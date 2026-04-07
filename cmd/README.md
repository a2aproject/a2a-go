# A2A CLI

A command-line interface for the Agent-to-Agent protocol. Link netcat for agents.

```bash
go install github.com/a2aproject/a2a-go/v2/cmd/a2a@latest
```

## Command Grammar

```
a2a <verb> [noun] <url> [positional-args] [flags] [global-flags]
```

The agent URL is always the first positional argument. Flags can appear in any position after the verb. Verbs that operate on a single resource type drop the noun (`send` always sends a message, `cancel` always cancels a task). Verbs that span multiple resource types require it (`get task`, `list tasks`).

## Global Flags

These apply to every client-mode command.

| Flag | Short | Description |
|---|---|---|
| `--output <fmt>` | `-o` | Output format: `text` (default), `json`. |
| `--transport <name>` | | Force transport: `rest`, `jsonrpc`, `grpc`. Default: auto-detect from card. |
| `--svc-param <k=v>` | | Service parameter (repeatable). The chosen transport defines how it's passed. Split on the first `=`. |
| `--auth <creds>` | | Shorthand for `--svc-param "Authorization=<creds>"`. |
| `--tenant <id>` | | Tenant identifier. Passed on every request. |
| `--timeout <dur>` | | Request timeout. Default `30s`. |
| `--verbose` | `-v` | Verbose output to stderr. |

---

## Client Commands

### `discover` - Agent Card Discovery

Fetch and display an agent card from its well-known URL.

```bash
a2a discover <url>
a2a discover <url> -o json
```
To fetch the extended card (if supported):

```bash
a2a discover <url> --extended --auth "Bearer <token>"
```
`discover` is a convenience alias for `get card <url>`.

### `send` - Send a Message

Send a message to an agent and print the response.

```bash
# Simple text message
a2a send <url> "Hello, what can you do?"

# Streaming response - events printed as they arrive
a2a send <url> --stream "Summarize this document"

# Fire-and-forget - get the task ID back immediately
a2a send <url> --immediate "Start a long analysis"

# Full structured message as JSON (the Message object)
a2a send <url> --json '{"role":"ROLE_USER","parts":[{"text":"analyze this"},{"fileUrl":"s3://..."}]}'

# Just the parts array
a2a send <url> --parts '[{"text":"analyze this"},{"fileUrl":"s3://..."}]'

# From file
a2a send <url> -f message.json

# Continue a conversation (same task)
a2a send <url> --task <task-id> "Follow-up question"

# Group under a context (new task, shared context)
a2a send <url> --context <context-id> "Related question"
```

| Flag | Description |
|---|---|
| `--stream` | Use `SendStreamingMessage`. Events are printed incrementally. |
| `--immediate` | Set `ReturnImmediately` in `SendMessageConfig`. |
| `--json <body>` | Raw JSON `Message` object. |
| `--parts <json>` | Raw JSON `parts` array. A `Message` is constructed with `ROLE_USER` and the given parts. |
| `-f <file>` | Read message from a JSON file. |
| `--task <id>` | Set `TaskID` on the message to continue an existing task. |
| `--context <id>` | Set `ContextID` on the message. |
| `--history <n>` | Request `n` history messages in the response. |

### `get task` - Get Task Details

```bash
a2a get task <url> <id>
a2a get task <url> <id> --history 10
a2a get task <url> <id> --with-artifacts -o json
```

| Flag | Description |
|---|---|
| `--history <n>` | Include up to `n` history messages. |
| `--with-artifacts` | Include artifacts in the response. |

### `list tasks` - List Tasks

```bash
a2a list tasks <url>
a2a list tasks <url> --context <ctx-id>
a2a list tasks <url> --status working
a2a list tasks <url> --limit 50
```

| Flag | Description |
|---|---|
| `--context <id>` | Filter by context ID. |
| `--status <state>` | Filter by task state. Accepts short forms: `submitted`, `working`, `completed`, `failed`, `canceled`, `rejected`, `input-required`, `auth-required`. |
| `--limit <n>` | Page size. |
| `--page-token <t>` | Pagination token from a previous response. |
| `--history <n>` | Include up to `n` history messages per task. |
| `--since <time>` | Only tasks with status updates after this timestamp (RFC 3339). |
| `--with-artifacts` | Include artifacts in the response. |

### `cancel` - Cancel a Task

```bash
a2a cancel <url> <task-id>
```

Prints the updated task status.

### `subscribe` - Subscribe to Task Events

```bash
a2a subscribe <url> <task-id>
```
Streams events to stdout until the task reaches a terminal state. Output format matches `send --stream`.

---

## Server Mode

`a2a serve` starts an A2A-compliant server backed by one of three modes.

### Common Server Flags

| Flag | Description |
|---|---|
| `--port <n>` | Listen port. Default `8080`. |
| `--host <addr>` | Bind address. Default `127.0.0.1`. |
| `--name <name>` | Agent name for the auto-generated card. |
| `--description <desc>` | Agent description. |
| `--transport <proto>` | Transport to serve: `rest` (default), `jsonrpc`, `grpc`. |
| `--protocol <ver>` | Protocol version: `latest` (default), `0.3`. When set to `0.3`, the server uses the compat transport layer (`a2agrpc/v0` for gRPC, `a2acompat/a2av0` for REST/JSON-RPC) to accept v0.3 clients. |
| `--card <file>` | Serve a custom agent card JSON instead of auto-generating. |
| `--card-compat` | Serve the agent card in a dual v0.3/v1.0 format so both old and new clients can discover the agent. |
| `--quiet` | Suppress traffic logging to stderr. |

### `--echo` - Echo Mode

```bash
a2a serve --echo
a2a serve --echo --port 9090 --name "Echo Agent"
```
Returns the user's message text back as an agent response. The "ping" for agents - useful for connectivity testing and client development.

### `--proxy` - Proxy Mode

Forward all requests to an upstream A2A agent. Logs traffic to stderr. Useful for debugging agent interactions, injecting service parameters, or acting as an authenticated gateway.

```bash
# Basic proxy with traffic logging
a2a serve --proxy https://upstream-agent.com

# Inject auth for an upstream that requires it
a2a serve --proxy https://upstream-agent.com --auth "<token>"

# Add tracing headers
a2a serve --proxy https://upstream-agent.com \
  --svc-param "X-Request-Source=a2a-proxy" \
  --svc-param "X-Trace-ID=debug-session-1"
```

The proxy creates an `a2aclient.Client` for the upstream agent and forwards each A2A operation. Service parameters specified via `--svc-param` are injected into every forwarded request using `a2aclient.AttachServiceParams`. The proxy's own agent card is derived from the upstream card with the local interface address substituted.

### `--exec` - Exec Mode

Run any command as an A2A agent. Message text goes to stdin, stdout becomes the response artifact. The subprocess does not need to know anything about A2A.
```bash
a2a serve --exec "python -u a2a_unaware_agent.py"
a2a serve --exec "./content-generator.sh"
```

#### Subprocess Interface

**stdin:** The first text part of the incoming A2A message.
**stdout:** Response content. Interpretation depends on whether `--chunk` is set (see below).
**stderr:** Logged by the CLI at debug level. On non-zero exit, stderr content is included in the failure status message.

**Exit code:**
- `0` → `TaskStateCompleted`
- Non-zero → `TaskStateFailed`

#### Output Modes

**Default (no `--chunk`):** The entire stdout is collected and emitted as a single text artifact when the process exits.
```
Status: working → [process runs] → Artifact (full output) → Status: completed
```

**With `--chunk=<delimiter>`:** stdout is read incrementally and split by the delimiter. Each piece is streamed as an artifact chunk event (`Append: true`) as soon as it's available. This enables streaming without requiring the subprocess to know about A2A's event model.

```bash
# Emit 3 chunks with 500ms delay - useful for verifying client streaming
a2a serve --exec "for i in 1 2 3; do echo \$i; sleep 0.5; done" --chunk=$'\n'

# Space-delimited chunks
a2a serve --exec "echo 'alpha beta gamma'" --chunk=' '

# Paragraph-level chunks
a2a serve --exec "cat essay.txt" --chunk=$'\n\n'
```

The event sequence with `--chunk`:

```
StatusUpdate:          working
ArtifactUpdateEvent:   {id: new, append: false, parts: ["1"]}
ArtifactUpdateEvent:   {id: same, append: true,  parts: ["2"]}
ArtifactUpdateEvent:   {id: same, append: true,  parts: ["3"], lastChunk: true}
StatusUpdate:          completed
```

---

## Output Formatting

All commands support `-o json` for machine-readable output and emits raw protocol objects.
Text mode is the default and is designed for human consumption in a terminal.