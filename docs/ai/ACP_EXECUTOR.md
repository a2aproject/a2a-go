# ACP Executor (`executor/a2aacp`)

> **Staleness warning**: if anything described here contradicts observed behavior, re-read
> the source code. This document may be out of date.

`a2aacp.Executor` implements `a2asrv.AgentExecutor` on top of an agent speaking the
[Agent Client Protocol](https://agentclientprotocol.com) v1 over stdio (`opencode acp`,
`claude-agent-acp`, `codex-acp`, `gemini --experimental-acp`). The ACP client is hand-rolled
(`jsonrpc.go`, `schema.go`) and covers only what the executor uses.

The exported API is `Executor`, `NewExecutor`, `Config` and two interfaces: `SessionStore`
(context → session persistence) and `MCPServerProvider` (MCP servers per context, as raw ACP
`McpServer` JSON). Everything else, including profiles and tool rules, stays unexported and is
configured through `Config` fields.

## Mapping

| A2A | ACP |
| :--- | :--- |
| Executor | one agent process (`StartFunc`), restarted lazily on the next turn if it exited |
| Context | session (`SessionStore`); `session/load` restores it in a new process if `loadSession` is advertised, otherwise `session/new` |
| Task / message | one `session/prompt` turn |
| `response` artifact | `agent_message_chunk` text, appended; new `messageId` or a tool call in between adds a blank line |
| working status + `toolCall` data part | `tool_call` / `tool_call_update` status changes (`in_progress`, `completed`, `failed`) |
| input-required + `hitl` data part (`type: permission`) | `session/request_permission` not decided by `Config.AllowTools` / `DenyTools` |
| input-required + `hitl` data part (`type: form`, `schema`) | `elicitation/create` in form mode (URL mode is refused) |
| message metadata `{"acpConfig": {id: value}}`, `Config.SessionConfig` | `session/set_config_option`, or `session/set_mode` / `session/set_model` for `mode` / `model` when the agent has no config options |
| completed / canceled / failed | prompt `stopReason` `end_turn` / `cancelled` / anything else, or an RPC error |
| CancelTask | `session/cancel` + `cancelled` outcome for every pending permission request |

The client advertises no `fs` or `terminal` capability, so agents use their own tools, and only
form elicitation. Other agent→client requests get "method not found".

`Config.SessionConfig` is applied when a session is created or loaded in the current agent
process; message metadata is applied on every turn and overrides it. The executor tracks the
settable IDs per session from `session/new` / `session/load` responses and `config_option_update`
notifications (`agent.sessions`).

## Turn lifecycle (`executor.go`, `turn.go`)

- `startTurn` takes a per-context lock (`lockContext`), so tasks in one context run one at a
  time in arrival order; a queued task stays `submitted`. The lock is released by the goroutine
  that runs `session/prompt`, when the agent responds, not when `Execute` returns.
- The connection's read goroutine routes `session/update` and `session/request_permission` by
  session ID (`Executor.routes`) into the turn's unbounded `queue`, so it never blocks on a turn
  nobody consumes. The prompt goroutine pushes the prompt result into the same queue after all
  notifications that preceded it, so ordering is preserved.
- `pump` drains the queue. On an unanswered permission request it stores `turn.pending`, yields
  input-required and returns; the turn (and the ACP request) stays open. The next `Execute`
  for the same task finds the turn in `Executor.turns`, parses the reply (`parseAnswer`), responds
  to the ACP request and resumes pumping. An unclear reply re-asks without touching the agent.
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

## Permission requests

ACP's `request_permission.toolCall` is a partial update: opencode sends `rawInput: {}` there
after reporting the real input in an earlier `tool_call_update`. `turn.mergeTool` keeps the
latest known state per `toolCallId` and the question is built from the merged state.

Question option IDs are derived from ACP option kinds (`allow_once`→`once`, `allow_always`→
`always`, `reject_once`→`reject`, `reject_always`→`reject_always`) so `Rules` and callers use
stable IDs; `pendingPermission.optionIDs` maps them back to the agent's `optionId`.

## Forms

`questions.go` renders the schema as a field list and accepts a hitl/`answer` data part with an
object, a JSON object as text, a bare value when the form has one field, or decline/cancel.
Values are checked against `required`, `enum` / `oneOf` and converted to the field type; an
invalid reply re-asks with the reason.

## Local process

`localStart` owns the stdout pipe (`os.Pipe`) instead of using `StdoutPipe`, because `Cmd.Wait`
closes the latter when the process exits and could drop the agent's last messages. Stdout is
closed `outputGrace` after exit in case a child inherited it. A failed write waits briefly for
the reader to report why the agent stopped (`conn.writeErr`).

## Testing

- Unit tests run against an in-process fake agent over `io.Pipe` (`fake_agent_test.go`); each
  `session/prompt` runs a test-provided script in its own goroutine.
- `tools/acp_smoke.sh [profile]` (or `ACP_CMD="cloudcode acp"`) runs a scripted conversation
  against a real agent through `a2asrv` + `a2aclient`. It sets `OPENCODE_PERMISSION` /
  `CLOUDCODE_PERMISSION` so opencode-based agents ask before running bash.

## Not implemented yet

- Per-profile permission bootstrap for non-opencode agents; URL-mode elicitation; reporting the
  available modes, models and config options to A2A callers.
