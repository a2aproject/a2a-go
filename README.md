# A2A Go SDK

[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
[![Nightly Check](https://github.com/a2aproject/a2a-go/actions/workflows/nightly.yaml/badge.svg)](https://github.com/a2aproject/a2a-go/actions/workflows/nightly.yaml)
[![Go Doc](https://img.shields.io/badge/Go%20Package-Doc-blue.svg)](https://pkg.go.dev/github.com/a2aproject/a2a-go/v2)
[![Ask DeepWiki](https://deepwiki.com/badge.svg)](https://deepwiki.com/a2aproject/a2a-go)

<!-- markdownlint-disable no-inline-html -->

<div align="center">
   <img src="https://raw.githubusercontent.com/a2aproject/A2A/refs/heads/main/docs/assets/a2a-logo-black.svg" width="256" alt="A2A Logo"/>
   <h3>
      A Go library for running agentic applications as A2A Servers, following the <a href="https://a2a-protocol.org">Agent2Agent (A2A) Protocol</a>.
   </h3>
</div>

<!-- markdownlint-enable no-inline-html -->

---

## ✨ Features

- **A2A Protocol Compliance:** Build agentic applications that adhere to the Agent2Agent (A2A) **v1.0 Protocol Specification**.
- **Client & Server SDKs:** High-level APIs for both serving agentic functionality (`a2asrv`) and consuming it (`a2aclient`).
- **Multi-Transport Support:** Protocol bindings for gRPC, REST, and JSON-RPC.
- **Extensible & Pluggable:** Extension points for bringing your own transport implementations, authentication middlewares, messaging and database backends.

> **Note:** The SDK version is distinct from the A2A specification version. The supported protocol version is exported in the codebase as `a2a.Version`

---

## 🚀 Getting Started

Requires Go `1.25.0` or newer:

```bash
go get github.com/a2aproject/a2a-go/v2
```

Visit [**pkg.go**](https://pkg.go.dev/github.com/a2aproject/a2a-go/v2) for a full documentation.

## 💡 Examples

For a simple example refer to the [helloworld](./examples/helloworld) example. 

### Server

For a full documentation visit [**pkg.go.dev/a2asrv**](https://pkg.go.dev/github.com/a2aproject/a2a-go/v2/a2asrv).

1. Create a transport-agnostic A2A request handler:

    ```go
    var options []a2asrv.RequestHandlerOption = newCustomOptions()
    var agentExecutor a2asrv.AgentExecutor = newCustomAgentExecutor()
    requestHandler := a2asrv.NewHandler(agentExecutor, options...)
    ```

2. Wrap the handler into a transport implementation:

    ```go
    grpcHandler := a2agrpc.NewHandler(requestHandler)
    
    // or

    jsonrpcHandler := a2asrv.NewJSONRPCHandler(requestHandler)

    // or

    restHandler := a2asrv.NewRESTHandler(requestHandler)
    ```

3. Register handler with a server, for example:

    ```go
    import "google.golang.org/grpc"
    ...
    server := grpc.NewServer()
    grpcHandler.RegisterWith(server)
    err := server.Serve(listener)

    // or

    http.Handle("/", restOrJSONRPCHandler)
    err := http.ListenAndServe(":8080", nil)
    ```

### Agent first-output timeout

Use a separate first-output budget when progress events can arrive before useful
output. The matcher defines output for your application; this example waits for
a non-empty text artifact:

```go
firstText := func(event a2a.Event) bool {
    update, ok := event.(*a2a.TaskArtifactUpdateEvent)
    if !ok || update.Artifact == nil {
        return false
    }
    for _, part := range update.Artifact.Parts {
        if part != nil && part.Text() != "" {
            return true
        }
    }
    return false
}
requestHandler := a2asrv.NewHandler(agentExecutor,
    a2asrv.WithAgentFirstOutputTimeout(10*time.Second, firstText),
    a2asrv.WithAgentInactivityTimeout(30*time.Second),
)
```

The first-output timer starts when execution starts, after executor setup, and
stops permanently when a matching event is successfully written to the internal
event pipe. Other events do not reset it. A nil matcher accepts any first event;
a non-positive duration disables the feature. The independent inactivity timer
still resets on every successful event write and may expire first.

This applies to both blocking and streaming sends in local and cluster modes.
Task subscriptions and cancellation executions do not start a first-output timer.
It excludes admission, work-queue waiting, executor setup, and delivery to the
client. Matchers must return promptly, not mutate events, and support concurrent
executions. Executors must honor context cancellation; the timeout cause is
detectable with `errors.Is(context.Cause(ctx), a2asrv.ErrAgentFirstOutputTimeout)`.

An optional live smoke test exercises JSON-RPC/SSE with an OpenAI-compatible
streaming model. Configure `OPENAI_BASE_URL`, `OPENAI_API_KEY`, and `MODEL_NAME`
through your environment, then run:

```sh
A2A_RUN_MODEL_SMOKE=1 go test -race ./e2e \
  -run '^TestAgentFirstOutputTimeout_ModelSmoke$' -v -count=1 -timeout=120s
```

The test makes two real model requests: one forwards model text and keeps the A2A
stream open beyond the 30-second budget; the other deliberately withholds the
response body while emitting progress, then verifies timeout cancellation and a
persisted failed task. The provider must support streaming chat completions and
produce text within 30 seconds. Normal tests do not call an external model.
Provider-specific request fields can be supplied as a JSON object through
`A2A_MODEL_EXTRA_BODY`, for example `'{"thinking":{"type":"disabled"}}'` for a
provider that supports disabling reasoning.

### Client 

For a full documentation visit [**pkg.go.dev/a2aclient**](https://pkg.go.dev/github.com/a2aproject/a2a-go/v2/a2aclient).

1. Resolve an `AgentCard` to get an information about how an agent is exposed.

    ```go
    card, err := agentcard.DefaultResolver.Resolve(ctx)
    ```

2. Create a transport-agnostic client from the `AgentCard`:

    ```go
    var options a2aclient.FactoryOption = newCustomClientOptions()
	client, err := a2aclient.NewFromCard(ctx, card, options...)
    ```

3. The connection is now open and can be used to send requests to a server:

    ```go
    msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("..."))
    resp, err := client.SendMessage(ctx, &a2a.SendMessageRequest{Message: msg})
    ```

---

## 🔧 CLI

A companion command-line tool for working with A2A agents - send messages, inspect tasks, resolve agent cards, or setup simple a2a servers - lives in its own repository: [**a2aproject/a2a-cli**](https://github.com/a2aproject/a2a-cli).

```bash
# Install
go install github.com/a2aproject/a2a-cli@latest

# Discover an agent
a2a discover https://agent.example.com

# Send a message
a2a send https://agent.example.com "Hello, what can you do?"

# Expose a local script as an A2A agent
a2a serve --exec "./my-script.sh" --port 8080
```

See the [a2a-cli README](https://github.com/a2aproject/a2a-cli#readme) or run `a2a help` for the full command reference.

---

## 🌐 More Examples

You can find a variety of more detailed examples in the [a2a-samples](https://github.com/a2aproject/a2a-samples) repository.

---

## 🤝 Contributing

Contributions are welcome! Please see the [CONTRIBUTING.md](CONTRIBUTING.md) file for guidelines on how to get involved.

Before starting work on a new feature or significant change, please open an issue to discuss your proposed approach with the maintainers. This helps ensure your contribution aligns with the project's goals and prevents duplicated effort or wasted work.

---

## 📄 License

This project is licensed under the Apache 2.0 License. See the [LICENSE](LICENSE) file for more details.
