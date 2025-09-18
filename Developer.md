# Developer Guide

This guide is for developers who want to contribute to the `a2a-go` project.

## Project Architecture

The `a2a-go` SDK is divided into several packages, each with a specific responsibility:

- **`a2a`**: This package contains the core types and constants of the A2A protocol. These types are transport-agnostic and are shared by both the client and server implementations.
- **`a2apb`**: This package contains the Protocol Buffers (protobuf) definitions for the A2A protocol. The gRPC service and message types are defined in this package.
- **`a2asrv`**: This package provides the server-side implementation of the A2A protocol. It includes the `Handler` which processes incoming A2A requests and the `AgentExecutor` interface which you implement to create your agent's logic.
- **`a2aclient`**: This package provides the client-side implementation of the A2A protocol. It allows you to interact with other A2A agents. **(Note: This package is not yet fully implemented)**.

The overall architecture is designed to be modular and extensible. The core protocol is decoupled from the transport layer, allowing you to use different transport protocols (e.g., gRPC, WebSockets) to carry A2A messages.

## Getting Started

To start developing `a2a-go`, you need to have Go installed on your system.

1. **Clone the repository:**

   ```bash
   git clone https://github.com/a2aproject/a2a-go.git
   cd a2a-go
   ```

2. **Install dependencies:**

   ```bash
   go mod download
   ```

## Building and Testing

To build the project, run the following command:

```bash
go build ./...
```

To run the tests, use the `go test` command:

```bash
go test ./...
```

### Protobuf Generation

If you make changes to the `.proto` files in the `a2apb` directory, you will need to regenerate the Go code. The `buf.gen.yaml` file defines the generation steps. You will need to have `buf` and the `protoc-gen-go` and `protoc-gen-go-grpc` plugins installed.

You can find instructions on how to install these tools on their respective websites.

Once you have the tools installed, you can regenerate the code by running the following command from the root of the repository:

```bash
buf generate
```

## Contribution Guidelines

We welcome contributions to the `a2a-go` project. If you would like to contribute, please follow these steps:

1. **Fork the repository.**
2. **Create a new branch for your feature or bug fix.**
3. **Make your changes and write tests.**
4. **Ensure that all tests pass.**
5. **Submit a pull request.**

When submitting a pull request, please provide a clear description of your changes and why they are needed.

## Adding a New Transport Protocol

The `a2a-go` SDK is designed to be transport-agnostic. To add a new transport protocol, you need to do the following:

1. **Implement the `Transport` interface from the `a2aclient` package.** This interface defines the methods for sending and receiving A2A messages over a specific transport.
2. **Implement the server-side logic for your transport.** This will typically involve creating a new server that listens for incoming connections and passes the A2A messages to the `a2asrv.Handler`.
3. **Create a new factory function for your transport.** This function will be used to create a new instance of your transport.

For an example of how to implement a transport, see the gRPC implementation in the `a2aclient` and `a2asrv` packages.

## Diagrams

Here are some diagrams to help you understand the architecture of the `a2a-go` project.

### A2A Interaction Sequence Diagram

This diagram shows a typical sequence of events for an A2A interaction.

```mermaid
sequenceDiagram
    participant Client
    participant A2A Server (gRPC)
    participant AgentExecutor

    Client->>A2A Server (gRPC): SendMessage(message)
    A2A Server (gRPC)->>A2A Server (gRPC): Create RequestContext
    A2A Server (gRPC)->>A2A Server (gRPC): Create EventQueue
    A2A Server (gRPC)->>AgentExecutor: Execute(context, reqCtx, queue)
    AgentExecutor->>AgentExecutor: Process message
    AgentExecutor->>A2A Server (gRPC): queue.Publish(event)
    A2A Server (gRPC)-->>Client: Stream of events
```

### Server-side Request Handling Flow

This diagram shows the flow of control when a request is handled by the server.

```mermaid
graph TD
    A[gRPC Request] --> B{a2asrv.Handler};
    B --> C{Authenticate Request};
    C --> D[Create RequestContext];
    D --> E[Create EventQueue];
    E --> F[Call AgentExecutor];
    F --> G[Agent Business Logic];
    G --> H[Publish Events];
    H --> I[Send Response];
    I --> J[gRPC Response];
```
