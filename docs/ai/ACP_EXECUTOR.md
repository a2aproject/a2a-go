# ACP Executor (`integration/executor/a2aacp`)

> **Staleness warning**: if anything described here contradicts observed behavior, re-read
> the source code. This document may be out of date.

`a2aacp.Executor` implements `a2asrv.AgentExecutor` on top of an agent speaking the
[Agent Client Protocol](https://agentclientprotocol.com) v1 over stdio (`opencode acp`,
`claude-agent-acp`, `codex-acp`, `gemini --experimental-acp`).

## Layout

| Package | Contents |
| :--- | :--- |
| `integration/executor/a2aacp` | the executor: turns (`turn.go`), keyed agent lifecycle and `Shutdown` (`agent.go`), ACP request → `Question` / structured input request translation, typed-reply matching (`questions.go`), `AutoAnswerer` / `ToolRules` / `MetadataSettings` (`hooks.go`), `Converter` and per-task output streams (`convert.go`), `Agent` / `AgentFunc` / `Process` / `WithCleanup` (`process.go`), `SessionStore` default |
| `a2aext/structuredinput` | the Structured Input extension the executor asks questions with: `Request`, `NewRequestEvent`, `Processor`, `Validator`; clients use `PendingRequest` / `NewResponse` |
| `internal/acp` | hand-rolled ACP client: protocol types (`schema.go`), JSON-RPC 2.0 over NDJSON (`Conn`), typed `Client` with a `Handler` for agent→client requests |
| `internal/subprocess` | starts a child process and exposes its stdio as an `io.ReadWriteCloser` |

Everything dropped in conversion is logged: unsupported prompt parts and malformed agent content
(warn), unmapped session updates and content types, unsupported notifications and unmatched
responses (debug). `ConvertUpdate` logs the updates it drops, so a custom `Converter` which
delegates to it keeps the logging.

`internal/acp` covers only what the executor uses. `Client` answers unsupported agent requests
("method not found") and malformed ones ("invalid params") itself; `executor.agentHandler`
routes the rest to turns.

The API is `NewExecutor(ctx, agent Agent, cfg *Config)` (nil `cfg` is valid) and `Executor`
(with `Shutdown` and `Close`). Design rules, agreed in review:

- Hooks are interfaces when the package ships an implementation (`Agent`: `Process`,
  `AgentFunc`; `AutoAnswerer`: `ToolRules`, `AutoAnswerFunc`; `Converter`: `ConvertUpdate`,
  `ConverterFunc`; `SessionStore`), and plain funcs otherwise (`Config.Session`,
  `Config.Settings`, `Config.Prompt`, `PoolConfig.Key`).
- Config durations: zero means the default, negative disables (`duration` in `executor.go`).
- "Turn" (one `session/prompt`) is internal; public docs speak of tasks and messages.
- Not exposed on purpose: question outcome callbacks (a question that times out or is abandoned
  is discovered when someone replies: the task fails with `errQuestionGone`), a configurable
  invalid-reply policy, prompt-part converters, agent profiles.

## Mapping

| A2A | ACP |
| :--- | :--- |
| Executor | one agent process (key `""`) or, with `Config.Pool`, one per `PoolConfig.Key` (default: per context); started by `Agent.Start(ctx, key)`, restarted lazily on the next turn if it exited |
| Context | session (`SessionStore`, which records the session ID and the agent key); `session/load` restores it in a new process if `loadSession` is advertised, otherwise `session/new`. Both take `cwd` and `mcpServers` from `Config.Session` (`sessionParams`: relative `cwd` joined to `Executor.dir`, the default, which is `Process.Dir` for a `Process` and the current directory otherwise; nil servers sent as `[]`) |
| Task / message | one `session/prompt` turn, or several when replies interrupt it |
| `response` artifact (`ResponseArtifact`) | `agent_message_chunk` content (text, image/audio as raw parts, resource links), appended for the whole task; see Conversion |
| working status + `toolCall` data part | `tool_call` / `tool_call_update` status changes (`in_progress`, `completed`, `failed`) |
| input-required + structured input request (id = `toolCallId`) + `toolCall` data part | `session/request_permission` not decided by `Config.AutoAnswer` (e.g. `ToolRules`) |
| input-required + structured input request (id = `elicitation-N`) | `elicitation/create` in form mode (URL mode is refused) |
| `Config.Settings` (e.g. `MetadataSettings` reading `{"acpSettings": {id: value}}`) | `session/set_config_option`, or `session/set_mode` / `session/set_model` for `mode` / `model` when the agent has no config options |
| completed / canceled / failed | prompt `stopReason` `end_turn` / `cancelled` / anything else, or an RPC error |
| CancelTask | `session/cancel` + `cancelled` outcome for every pending permission request |

The client advertises no `fs` or `terminal` capability, so agents use their own tools, and only
form elicitation. Other agent→client requests get "method not found".

`Config.Session` runs only when a session is created or loaded, for the message that caused it:
ACP fixes `cwd` and MCP servers for the session's lifetime in a process, so caller-specific values
(e.g. credentials in MCP headers) would apply to everyone in the context.

`Config.Settings` is applied before every turn; static defaults are a func returning a constant
map. Callers can't change settings unless `Settings` lets them: metadata is ignored by default,
because some agents have modes which skip permission requests (e.g. `bypassPermissions`).
`MetadataSettings(patterns...)` fails a message that sets an ID matching none of the `*` glob
patterns. The executor tracks the
settable IDs per session from `session/new` / `session/load` responses and `config_option_update`
notifications (`agent.sessions`).

## Turn lifecycle (`executor.go`, `turn.go`)

- `startTurn` takes a per-context lock (`lockContext`), so tasks in one context run one at a
  time in arrival order; a queued task stays `submitted`. The lock is released by the goroutine
  that runs `session/prompt`, when the agent responds, not when `Execute` returns, so a turn
  waiting for an answer holds it for up to `QuestionTimeout`. With `Config.RejectBusyContext` a
  new task fails with `busyError` (naming the holder, `ctxLock.holder`) instead of waiting; an
  interrupting reply always waits, since it waits for its own canceled turn.
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
  to the ACP request and resumes pumping (`parseReply`, see Questions). A reply which isn't an
  answer at all interrupts (`interrupt`): it rejects the question, cancels the turn, waits
  for the context lock and starts a new turn with the reply under the same task ID, carrying over
  `turn.out` so artifacts continue. An answer with invalid values re-asks with the reason,
  without touching the agent.
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
- Timed-out and abandoned questions produce no task event (an executor can only emit from
  `Execute` / `Cancel`), so the task stays input-required until the next reply fails it.
- `cancelTurn` stops the agent (`stopAgent`) if the turn hasn't ended within `CancelTimeout`
  (default 1m), failing all its turns; a canceled turn whose prompt fails reports `canceled`.

## Agent lifecycle (`agent.go`)

- Agents are keyed. `Executor.agentKey` (`PoolConfig.Key`, default the context ID; constant `""`
  without a pool) picks the key of a context with no stored session; afterwards the key stored
  with the session (`Session.AgentKey`) wins, since the session lives in that agent's storage. A
  changing key result therefore never splits a context. Without a pool, `NewExecutor` starts the
  `""` agent eagerly to surface config errors; pooled agents start on their first turn.
- `Executor.agents` maps keys to `agentSlot`s. `slot.startMu` serializes starting and stopping
  that key's agent, so at most one process per key exists (a sandbox with a fixed name can be
  reused). Lock order: `slot.startMu` before `Executor.mu`, never two `startMu`s except via
  `TryLock` in `makeRoom`.
- `acquireAgent` increments `slot.refs` and returns a release func. Turn setup holds a ref and
  the prompt goroutine releases it when the prompt ends, so a turn waiting for an answer keeps
  its agent. At zero refs, `PoolConfig.IdleTimeout` (default 10m) arms `slot.idle`; `stopIfIdle` re-checks refs
  under `Executor.mu` before closing. A slot is removed only after its agent closed, so a new
  start of the key waits on `startMu` instead of racing a closing process.
- `ensureAgent` runs `Agent.Start` + `initialize` within `StartTimeout`; a failed start refuses
  further starts of the key for a backoff (1s doubling to 1m) and reports `startErr`. Errors
  wrapping `ErrNotOwner` (another node owns the key) skip the backoff.
- `makeRoom` enforces `PoolConfig.MaxAgents`: it stops the least recently used idle agent or fails
  the turn with "all N agents are busy".
- Turns are routed by `routeKey{agent, sessionID}`: processes of different keys can hand out the
  same session IDs.
- `stopAgent` (used by `CancelTimeout`) closes the agent only if it is still the running one of
  its key. `Close` stops all agents.
- `Shutdown` sets `draining`: `startTurn` fails new turns with `errDraining` (so do queued turns
  once they get the context lock, in `acquireAgent`), while answers to pending questions still
  resume their turns. `drained` closes when `turns` and `waiting` are empty
  (`checkDrainedLocked`, called where either shrinks). If ctx ends first, waiting turns get
  `errDraining`, running turns are canceled, and `Close` stops the agents.
- Cluster integration lives outside the executor: a push-queue router sends all payloads of a
  context (cancels included, via the task's context) to the node owning the key, `Agent.Start`
  claims the key's lease and restores its sandbox (returning `ErrNotOwner` if another node
  holds it), and the `WithCleanup` callback snapshots and releases it.

## Conversion (`convert.go`)

- `pump` passes every session update to `Executor.converter` (`Config.Converter`, default
  `ConvertUpdate`) as an `Update{Type, Raw}`. `Raw` comes from `acp.SessionUpdate.UnmarshalJSON`,
  which keeps the undecoded JSON so fields the schema doesn't model (`rawOutput`, `entries`, …)
  reach converters. Unexported fields carry the decoded update and, for tool calls, the merged
  state (`turn.update` runs `mergeTool` for every converter) when the status changed, which
  `ConvertUpdate` uses.
- A converter returns a `*ConverterResult{Artifact, Parts, Metadata, EventMetadata}` or nil to
  skip; it never builds events. `taskOutput.emit` does: the first result for an artifact name
  creates it, later ones append (artifact metadata merges on append in `a2aevent`); an empty
  `Artifact` sends a working status message. This keeps artifact IDs and appends consistent
  whatever the converter returns.
- `taskOutput` lives per task (`turn.out`, handed over by `interrupt`). A stream puts a blank
  line before its next text part when the agent `messageId` changes or a result for another
  target came in between (`artifactStream.needsBreak`, kept pending until a text part comes).
- A converter error cancels the turn and fails the task. Questions and prompt results don't pass
  through converters.

## Permission requests

ACP's `request_permission.toolCall` is a partial update: opencode sends `rawInput: {}` there
after reporting the real input in an earlier `tool_call_update`. `turn.mergeTool` keeps the
latest known state per `toolCallId` and the question is built from the merged state.

Question option IDs are derived from ACP option kinds (`allow_once`→`once`, `allow_always`→
`always`, `reject_once`→`reject`, `reject_always`→`reject_always`) so `ToolRules` and callers use
stable IDs; `pendingPermission.optionIDs` maps them back to the agent's `optionId`.

`Executor.autoAnswer` consults `Config.AutoAnswer`. An answer which fails the request schema
(`Executor.validate`) is logged and the caller is asked. `OptionCancel`, when not an offered
option, skips validation and answers with the `cancelled` outcome (`pendingQuestion.reply`). `ToolRules`
matches `*` globs (`matchGlob`, nothing else is special) against the tool kind: `Deny` answers
`reject`, then `reject_always`, then cancel, so a denied tool never reaches the caller; `Allow`
selects `once`, then `always`. Form question IDs come from a per-executor counter
(`elicitation-N`), because JSON-RPC IDs repeat after an agent restart.

## Questions (`questions.go`)

Questions use the Structured Input extension (`a2aext/structuredinput`); there is no custom
question format. `newPending` builds a `structuredinput.Request` whose schema is an object with
`option` (a string `oneOf` of `{const: id, title: label}`) and, for forms, `content` (the
agent's `requestedSchema`, converted to `jsonschema.Schema`), required by `if`/`then` when
`option` is `accept`. Forms offer `accept`, `decline`, `cancel`. The decoded payload is
`Answer{Option, Content}`; `Question{Request, Title, Options, Tool}` is what `AutoAnswerer`s see.

`pump` asks with `structuredinput.NewRequestEvent` (text for people from `pendingQuestion.text`,
plus a `toolCall` data part for permissions). `parseReply` runs `structuredinput.Processor`
(untagged data parts accepted): accepted → `Answer`; invalid, ID mismatch or several inputs →
`ProcessorResult.NewRetryEvent` with the reason (and a `validation_error` part when there are
validation errors); no structured input → `matchText` reads typed replies (option ID or label,
yes/no for permissions, a JSON object of form fields) and validates the result with the same
validator; anything else interrupts. Validation messages come from `jsonschema-go`;
`describe` only strips their `validating root: ` prefix.

## Local process (`internal/subprocess`, used by `a2aacp.Process`)

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
- `internal/acp` and `internal/subprocess` have their own tests (pipes, and `sh`/`cat`/`sleep`).
- `tools/acp_smoke.sh [opencode|claude|codex|gemini]` (or `ACP_CMD="cloudcode acp"`; the
  profile commands live in the script) runs a scripted conversation
  against a real agent through `a2asrv` + `a2aclient`. It sets `OPENCODE_PERMISSION` /
  `CLOUDCODE_PERMISSION` so opencode-based agents ask before running bash.

## Not implemented yet

- Per-profile permission bootstrap for non-opencode agents; URL-mode elicitation; reporting the
  available modes, models and config options to A2A callers.
