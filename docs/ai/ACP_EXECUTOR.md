# ACP Executor (`executor/a2aacp`)

> **Staleness warning**: if anything described here contradicts observed behavior, re-read
> the source code. This document may be out of date.

`a2aacp.Executor` implements `a2asrv.AgentExecutor` on top of an agent speaking the
[Agent Client Protocol](https://agentclientprotocol.com) v1 over stdio (`opencode acp`,
`claude-agent-acp`, `codex-acp`, `gemini --experimental-acp`).

## Layout

| Package | Contents |
| :--- | :--- |
| `executor/a2aacp` | the executor: turns (`turn.go`), keyed agent lifecycle and `Drain` (`agent.go`), ACP request → question translation and tool rules (`questions.go`), hook types and helpers (`hooks.go`), update converters (`convert.go`), process utilities and profiles (`process.go`), `SessionStore` default |
| `executor/a2aacp/hitl` | public question/answer format: `Question`, `Answer`, `FromStatus`/`FromMessage`, `NewStatusMessage`, `ParseAnswer`, `NewAnswerMessage`, `Question.MatchOption`/`Fields`/`AcceptForm`; used by the executor and by clients |
| `internal/acp` | hand-rolled ACP client: protocol types (`schema.go`), JSON-RPC 2.0 over NDJSON (`Conn`), typed `Client` with a `Handler` for agent→client requests |
| `internal/subprocess` | starts a child process and exposes its stdio as an `io.ReadWriteCloser` |

Everything dropped in conversion is logged: unsupported prompt parts and malformed agent content
(warn), unmapped session updates and content types, unsupported notifications and unmatched
responses (debug). Unmapped session updates are logged only when no `UpdateConverter` is set.

`internal/acp` covers only what the executor uses. `Client` answers unsupported agent requests
("method not found") and malformed ones ("invalid params") itself; `executor.agentHandler`
routes the rest to turns.

The exported API is `Executor` (with `Close` and `Drain`), `NewExecutor`, `Config`,
`SessionStore`/`Session`, `SessionParams`, `ErrNotOwner`, `Update`/`UpdateConverter`, and
helpers consumers would otherwise rewrite: `StartProcess`/`Process`,
`WithCleanup`, `ProfileCommand` (to build `Config.Start`), `MetadataConfig` (a `TurnConfig`),
`InterruptUnrecognized` (an `OnInvalidReply`), `ThoughtsAsArtifact` / `ChainUpdateConverters` /
`UpdateConverterFunc`. Extension points are `Config` callbacks: `Start`, `AgentKey`, `Session`,
`AutoAnswer`, `OnInvalidReply`, `OnQuestionDone`, `TurnConfig`, `Prompt`, `ConvertPart`,
`UpdateConverter`. Fields cover the common cases: `AllowTools`/`DenyTools`, `CancelTimeout`,
`StartTimeout`, `IdleTimeout`, `MaxAgents`, `BusyContext`, `QuestionTimeout`, `SessionConfig`.

## Mapping

| A2A | ACP |
| :--- | :--- |
| Executor | one agent process per key (`Config.AgentKey`, default one key `""`; `Config.Start(ctx, key)`, or `Command`/`Profile` via `StartProcess`), restarted lazily on the next turn if it exited |
| Context | session (`SessionStore`, which records the session ID and the agent key); `session/load` restores it in a new process if `loadSession` is advertised, otherwise `session/new`. Both take `cwd` and `mcpServers` from `Config.Session` (`sessionParams`: relative `cwd` joined to `Config.Dir`, the default; nil servers sent as `[]`) |
| Task / message | one `session/prompt` turn |
| `response` artifact | `agent_message_chunk` text, appended; new `messageId` or a tool call in between adds a blank line |
| working status + `toolCall` data part | `tool_call` / `tool_call_update` status changes (`in_progress`, `completed`, `failed`) |
| input-required + `hitl` data part (`type: permission`) | `session/request_permission` not decided by `Config.AutoAnswer` / `AllowTools` / `DenyTools` |
| input-required + `hitl` data part (`type: form`, `schema`) | `elicitation/create` in form mode (URL mode is refused) |
| `Config.SessionConfig`, `Config.TurnConfig` (e.g. `MetadataConfig` reading `{"acpConfig": {id: value}}`) | `session/set_config_option`, or `session/set_mode` / `session/set_model` for `mode` / `model` when the agent has no config options |
| completed / canceled / failed | prompt `stopReason` `end_turn` / `cancelled` / anything else, or an RPC error |
| CancelTask | `session/cancel` + `cancelled` outcome for every pending permission request |

The client advertises no `fs` or `terminal` capability, so agents use their own tools, and only
form elicitation. Other agent→client requests get "method not found".

`Config.Session` runs only when a session is created or loaded, for the message that caused it:
ACP fixes `cwd` and MCP servers for the session's lifetime in a process, so caller-specific values
(e.g. credentials in MCP headers) would apply to everyone in the context.

`Config.SessionConfig` is applied when a session is created or loaded in the current agent
process; `Config.TurnConfig` is applied on every turn and overrides it. Callers can't change
settings unless `TurnConfig` lets them: metadata is ignored by default, because some agents have
modes which skip permission requests (e.g. `bypassPermissions`). `MetadataConfig(patterns...)`
fails a turn that sets an ID matching none of the glob patterns. The executor tracks the
settable IDs per session from `session/new` / `session/load` responses and `config_option_update`
notifications (`agent.sessions`).

## Turn lifecycle (`executor.go`, `turn.go`)

- `startTurn` takes a per-context lock (`lockContext`), so tasks in one context run one at a
  time in arrival order; a queued task stays `submitted`. The lock is released by the goroutine
  that runs `session/prompt`, when the agent responds, not when `Execute` returns, so a turn
  waiting for an answer holds it for up to `QuestionTimeout`. With `Config.BusyContext =
  BusyReject` a new task fails with `busyError` (naming the holder, `ctxLock.holder`) instead of
  waiting; an interrupting reply always waits, since it waits for its own canceled turn.
- From `startTurn` until the turn is registered in `Executor.turns`, the task is in
  `Executor.waiting` with a cancel func. `Cancel` calls it with `errTaskCanceled`; `Execute`
  then returns without an event, since `Cancel` emits `canceled`. Registration checks the cause
  under `Executor.mu`, so a canceled task never starts a prompt. In local mode the a2asrv also
  cancels the producer context, but in cluster mode `Cancel` may be the only signal.
- `Config.Prompt` replaces the message parts sent as the prompt; the task history keeps the
  caller's message, since `Execute` yields the submitted task from `execCtx.Message`.
- The connection's read goroutine routes `session/update` and `session/request_permission` by
  agent and session ID (`Executor.routes`) into the turn's unbounded `queue`, so it never blocks on a turn
  nobody consumes. The prompt goroutine pushes the prompt result into the same queue after all
  notifications that preceded it, so ordering is preserved.
- `pump` drains the queue. On an unanswered permission request it stores `turn.pending`, yields
  input-required and returns; the turn (and the ACP request) stays open. The next `Execute`
  for the same task finds the turn in `Executor.turns`, parses the reply (`parseAnswer`), responds
  to the ACP request and resumes pumping. A reply `hitl.ParseAnswer` rejects goes to
  `Config.OnInvalidReply`: `ReplyAskAgain` (default) re-asks without touching the agent;
  `ReplyInterrupt` (`interrupt`) rejects the question, cancels the turn, waits for the context
  lock and starts a new turn with the reply under the same task ID.
- `turn.mu` guards `pending` and `canceled`, because `Cancel` runs concurrently with `Execute`
  or while no `Execute` is running. `cancelTurn` is idempotent; permission requests that arrive
  after cancelation are answered `cancelled` directly in `handle`.
- If `Execute`'s context ends or `yield` returns false, the turn is canceled in the agent.
- A pending question has a timer (`Config.QuestionTimeout`, default 24h). On expiry the question
  is rejected (permission) or declined (form) and the turn is canceled, because nobody would see
  what the agent does next. `turn.end` clears the pending question when the prompt returns, for
  example because the agent exited.
- A message on an input-required task with no pending question (expired, agent restarted,
  executor restarted) fails the task with `errQuestionGone`; it is never sent as a new prompt.
- `Config.OnQuestionDone` reports every question that was shown to the caller when it stops
  being pending: answered, timed out, canceled, interrupted, or abandoned (`turn.end` found it
  pending, e.g. the agent exited). The last two and timeouts produce no task event, so this is
  the only way for a consumer to withdraw buttons or notifications.
- `cancelTurn` with `Config.CancelTimeout` stops the agent (`stopAgent`) if the turn hasn't
  ended in time, failing all its turns; a canceled turn whose prompt fails reports `canceled`.

## Agent lifecycle (`agent.go`)

- Agents are keyed. `Config.AgentKey(ctx, contextID)` picks the key of a context with no stored
  session; afterwards the key stored with the session (`Session.AgentKey`) wins, since the
  session lives in that agent's storage. A changing `AgentKey` result therefore never splits a
  context. Without `AgentKey`, `NewExecutor` starts the `""` agent eagerly to surface config
  errors; keyed agents start on their first turn.
- `Executor.agents` maps keys to `agentSlot`s. `slot.startMu` serializes starting and stopping
  that key's agent, so at most one process per key exists (a sandbox with a fixed name can be
  reused). Lock order: `slot.startMu` before `Executor.mu`, never two `startMu`s except via
  `TryLock` in `makeRoom`.
- `acquireAgent` increments `slot.refs` and returns a release func. Turn setup holds a ref and
  the prompt goroutine releases it when the prompt ends, so a turn waiting for an answer keeps
  its agent. At zero refs, `Config.IdleTimeout` arms `slot.idle`; `stopIfIdle` re-checks refs
  under `Executor.mu` before closing. A slot is removed only after its agent closed, so a new
  start of the key waits on `startMu` instead of racing a closing process.
- `ensureAgent` runs `Config.Start` + `initialize` within `StartTimeout`; a failed start refuses
  further starts of the key for a backoff (1s doubling to 1m) and reports `startErr`. Errors
  wrapping `ErrNotOwner` (another node owns the key) skip the backoff.
- `makeRoom` enforces `Config.MaxAgents`: it stops the least recently used idle agent or fails
  the turn with "all N agents are busy".
- Turns are routed by `routeKey{agent, sessionID}`: processes of different keys can hand out the
  same session IDs.
- `stopAgent` (used by `CancelTimeout`) closes the agent only if it is still the running one of
  its key. `Close` stops all agents.
- `Drain` sets `draining`: `startTurn` fails new turns with `errDraining` (so do queued turns
  once they get the context lock, in `acquireAgent`), while answers to pending questions still
  resume their turns. `drained` closes when `turns` and `waiting` are empty
  (`checkDrainedLocked`, called where either shrinks). If ctx ends first, waiting turns get
  `errDraining`, running turns are canceled, and `Close` stops the agents.
- Cluster integration lives outside the executor: a push-queue router sends all payloads of a
  context (cancels included, via the task's context) to the node owning the key, `Start`
  claims the key's lease and restores its sandbox (returning `ErrNotOwner` if another node
  holds it), and the `WithCleanup` callback snapshots and releases it.

## Conversion hooks (`convert.go`)

- `Config.ConvertPart` runs per prompt part before the built-in mapping; a returned JSON block is
  sent as is (`acp.ContentBlock.Raw`, emitted by its `MarshalJSON`).
- `Config.UpdateConverter` creates a per-turn `UpdateConverter` (stored in `turn.converter`).
  `turn.convert` computes the built-in events first (keeping `textStream` and tool state
  consistent), then passes `Update{Type, Raw}` and those events to the converter. `Raw` comes
  from `acp.SessionUpdate.UnmarshalJSON`, which keeps the undecoded JSON so fields the schema
  doesn't model (`rawOutput`, `entries`, …) reach converters. A converter error cancels the turn
  and fails the task.
- Questions and prompt results don't pass through converters.

## Permission requests

ACP's `request_permission.toolCall` is a partial update: opencode sends `rawInput: {}` there
after reporting the real input in an earlier `tool_call_update`. `turn.mergeTool` keeps the
latest known state per `toolCallId` and the question is built from the merged state.

Question option IDs are derived from ACP option kinds (`allow_once`→`once`, `allow_always`→
`always`, `reject_once`→`reject`, `reject_always`→`reject_always`) so `Rules` and callers use
stable IDs; `pendingPermission.optionIDs` maps them back to the agent's `optionId`.

`Executor.autoAnswer` consults `Config.AutoAnswer` first, then answers requests matched by
`DenyTools` with `reject`, then `reject_always`, then the `cancelled` outcome, so a denied tool
never reaches the caller. `AllowTools` selects `once`, then `always`; without either the caller
is asked. Form question IDs come from a per-executor counter (`elicitation-N`), because JSON-RPC
IDs repeat after an agent restart.

`Config.SessionConfig` is applied until it succeeds once per session and agent process
(`sessionInfo.configured`), so a turn that fails configuration doesn't lose it.

## Forms

`hitl` renders the schema as a field list and accepts a hitl/`answer` data part with an
object, a JSON object as text, a bare value when the form has one field, or decline/cancel.
Values are checked against `required`, `enum` / `oneOf` and converted to the field type; an
invalid reply re-asks with the reason.

## Local process (`internal/subprocess`, exported as `a2aacp.StartProcess`)

`subprocess.Start` owns the stdout pipe (`os.Pipe`) instead of using `StdoutPipe`, because `Cmd.Wait`
closes the latter when the process exits and could drop the agent's last messages. Stdout is
closed `outputGrace` after exit in case a child inherited it; reads then report `io.EOF`. A failed
write waits briefly for the reader to report why the agent stopped (`acp.Conn.writeErr`).

The agent runs in its own process group. `Close` closes stdin, sends SIGTERM to the group after
`closeGrace`, SIGKILL after another `closeGrace`, and always sends SIGTERM to the group once the
agent exited, so children such as MCP servers don't outlive it.

`acp.Conn.Call` checks for a delivered response before reporting `done`, because the reader
delivers a response and then fails when the agent exits right after responding.

The agent's exit is visible on `Client.Done` before the prompt goroutine ends the turn, so
`Executor.answer` checks `agent.exited()` and reports the question as gone instead of a write
error.

## Testing

- Executor tests run against an in-process fake agent over `io.Pipe` (`fake_agent_test.go`,
  agent side is an `acp.Conn`); each `session/prompt` runs a test-provided script in its own
  goroutine. Scripts that outlive the test's last `Execute` must be awaited, or they write to a
  closed pipe.
- Lifecycle tests (`agent_test.go`) use `fakeHarness.hangInitialize` for start timeouts and
  `fakeAgent.stopped` to observe the executor closing an agent.
- `hitl`, `internal/acp` and `internal/subprocess` have their own tests (pipes, and `sh`/`cat`/`sleep`).
- `tools/acp_smoke.sh [profile]` (or `ACP_CMD="cloudcode acp"`) runs a scripted conversation
  against a real agent through `a2asrv` + `a2aclient`. It sets `OPENCODE_PERMISSION` /
  `CLOUDCODE_PERMISSION` so opencode-based agents ask before running bash.

## Not implemented yet

- Per-profile permission bootstrap for non-opencode agents; URL-mode elicitation; reporting the
  available modes, models and config options to A2A callers.
