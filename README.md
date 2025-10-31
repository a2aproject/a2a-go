# A2A Go SDK

[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
[![Nightly Check](https://github.com/a2aproject/a2a-go/actions/workflows/nightly.yaml/badge.svg)](https://github.com/a2aproject/a2a-go/actions/workflows/nightly.yaml)
[![Go Doc](https://img.shields.io/badge/Go%20Package-Doc-blue.svg)](https://pkg.go.dev/github.com/a2aproject/a2a-go)
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

- **A2A Protocol Compliant:** Build agentic applications that adhere to the Agent2Agent (A2A) Protocol.
- **Extensible:** Easily add support for different communication protocols and database backends.

---

## 🚀 Getting Started

Requires Go `1.24.4` or newer:

```bash
go get github.com/a2aproject/a2a-go@0.3.0
```

Visit [**pkg.go**](https://pkg.go.dev/github.com/a2aproject/a2a-go) for a full documentation.

## Examples

For a simple example refer to [gRPC helloworld](./examples/grpc/helloworld). 

### Server

For a full documentation visit [**pkg.go.dev/a2asrv**](https://pkg.go.dev/github.com/a2aproject/a2a-go/a2asrv).

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
    ```

3. Register handler with a server, for example:

    ```go
    import "google.golang.org/grpc"
    ...
    server := grpc.NewServer()
    grpcHandler.RegisterWith(server)
    err := server.Serve(listener)
    ```

### Client 

For a full documentation visit [**pkg.go.dev/a2aclient**](https://pkg.go.dev/github.com/a2aproject/a2a-go/a2aclient).

1. Resolve an `AgentCard` to get an information about how an agent is exposed.

    ```go
    cardResolver := agentcard.Resolver{BaseURL: *cardURL}
    card, err := cardResolver.Resolve(ctx)
    ```

2. Create a transport-agnostic client from the `AgentCard`:

    ```go
    var options a2aclient.FactoryOption = newCustomClientOptions()
	client, err := a2aclient.NewFromCard(ctx, card, options...)
    ```

3. The connection is now open and can be used to send requests to a server:

    ```go
    msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: "..."})
    resp, err := client.SendMessage(ctx, &a2a.MessageSendParams{Message: msg})
    ```

---

## 🌐 More Examples

You can find a variety of more detailed examples in the [a2a-samples](https://github.com/a2aproject/a2a-samples) repository.

---

## 🤝 Contributing

Contributions are welcome! Please see the [CONTRIBUTING.md](CONTRIBUTING.md) file for guidelines on how to get involved.

---

## 📄 License

This project is licensed under the Apache 2.0 License. See the [LICENSE](LICENSE) file for more details.
