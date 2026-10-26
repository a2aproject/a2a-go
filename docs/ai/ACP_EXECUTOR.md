# ACP Executor (`executor/a2aacp`)

> **Staleness warning**: if anything described here contradicts observed behavior, re-read
> the source code. This document may be out of date.

`a2aacp.Executor` implements `a2asrv.AgentExecutor` on top of an agent speaking the
[Agent Client Protocol](https://agentclientprotocol.com) v1 over stdio (`opencode acp`,
`claude-agent-acp`, `codex-acp`, `gemini --experimental-acp`).

## Layout

| Package | Contents |
| :--- | :--- |
| `executor/a2aacp` | the executor: turns (`turn.go`), agent lifecycle (`agent.go`), ACP request → question translation and tool rules (`questions.go`), hook types and helpers (`hooks.go`), process utilities and profiles (`process.go`), `SessionStore` default |
| `executor/a2aacp/hitl` | public question/answer format: `Question`, `Answer`, `FromStatus`/`FromMessage`, `NewStatusMessage`, `ParseAnswer`, `NewAnswerMessage`, `Question.MatchOption`/`Fields`/`AcceptForm`; used by the executor and by clients |
| `internal/acp` | hand-rolled ACP client: protocol types (`schema.go`), JSON-RPC 2.0 over NDJSON (`Conn`), typed `Client` with a `Handler` for agent→client requests |
| `internal/subprocess` | starts a child process and exposes its stdio as an `io.ReadWriteCloser` |

`internal/acp` covers only what the executor uses. `Client` answers unsupported agent requests
("method not found") and malformed ones ("invalid params") itself; `executor.agentHandler`
routes the rest to turns.

The exported API is `Executor`, `NewExecutor`, `Config`, `SessionStore`, `SessionParams`, and
helpers consumers would otherwise rewrite: `StartProcess`/`Process`,
`WithCleanup`, `ProfileCommand` (to build `Config.Start`), `MetadataConfig` (a `TurnConfig`),
`InterruptUnrecognized` (an `OnInvalidReply`). Extension points are `Config` callbacks:
`Start`, `Session`, `AutoAnswer`, `OnInvalidReply`, `OnQuestionDone`, `TurnConfig`, `Prompt`. Fields cover
the common cases: `AllowTools`/`DenyTools`, `CancelTimeout`, `StartTimeout`,
`QuestionTimeout`, `SessionConfig`.

## Mapping

| A2A | ACP |
| :--- | :--- |
| Executor | one agent process (`Config.Start`, or `Command`/`Profile` via `StartProcess`), restarted lazily on the next turn if it exited |
| Context | session (`SessionStore`); `session/load` restores it in a new process if `loadSession` is advertised, otherwise `session/new`. Both take `cwd` and `mcpServers` from `Config.Session` (`sessionParams`: relative `cwd` joined to `Config.Dir`, the default; nil servers sent as `[]`) |
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
  that runs `session/prompt`, when the agent responds, not when `Execute` returns.
- `Config.Prompt` replaces the message parts sent as the prompt; the task history keeps the
  caller's message, since `Execute` yields the submitted task from `execCtx.Message`.
- The connection's read goroutine routes `session/update` and `session/request_permission` by
  session ID (`Executor.routes`) into the turn's unbounded `queue`, so it never blocks on a turn
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

- `startMu` serializes starting and stopping, so at most one agent process exists (a sandbox
  with a fixed name can be reused). `ensureAgent` runs `Config.Start` + `initialize` within
  `StartTimeout`; a failed start refuses further starts for a backoff (1s doubling to 1m) and
  reports `startErr`.
- `stopAgent` (used by `CancelTimeout`) closes the agent only if it is still the running one.

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
