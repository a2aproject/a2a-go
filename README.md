# a2a-go

[![Go Reference](https://pkg.go.dev/badge/github.com/a2aproject/a2a-go.svg)](https://pkg.go.dev/github.com/a2aproject/a2a-go)
[![Go Report Card](https://goreportcard.com/badge/github.com/a2aproject/a2a-go)](https://goreportcard.com/report/github.com/a2aproject/a2a-go)

`a2a-go` is the official Go implementation of the [A2A Protocol](https://github.com/a2aproject/a2a), a decentralized communication protocol for AI agents.

This SDK provides the tools and building blocks for creating A2A-compatible agents in Go. It includes a server implementation for handling A2A requests and a client for interacting with other agents.

## A2A Protocol

The Agent-to-Agent (A2A) protocol is designed to facilitate communication between autonomous AI agents. It defines a set of standard messages and interaction patterns that allow agents to discover each other, negotiate capabilities, and exchange information in a secure and decentralized manner.

Key features of the A2A protocol include:

- **Agent Discovery**: Agents can broadcast their capabilities and discover other agents on the network.
- **Skill-based Routing**: Messages are routed to agents based on the skills they offer.
- **Secure Communication**: The protocol supports end-to-end encryption and authentication.
- **Transport Agnostic**: A2A can be used over any transport protocol, such as gRPC, WebSockets, or HTTP.

## Project Status

This project is currently under active development.

- **Server (`a2asrv`)**: The server-side implementation is in progress. It provides the core components for building an A2A agent.
- **Client (`a2aclient`)**: The client-side implementation is **not yet complete**. The API is defined, but the methods are not implemented.

## Installation

To use `a2a-go` in your project, you can use `go get`:

```bash
go get github.com/a2aproject/a2a-go
```

## Usage

Here is a basic example of how to create an A2A agent using the `a2asrv` package.

First, you need to implement the `AgentExecutor` and `AgentCardProducer` interfaces.

```go
package main

import (
	"context"
	"fmt"
	"log"
	"net"

	"google.golang.org/grpc"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
	"github.com/a2aproject/a2a-go/a2apb"
)

// MyAgent is a custom implementation of an A2A agent.
type MyAgent struct{}

// Execute is called when the agent receives a message.
func (a *MyAgent) Execute(ctx context.Context, reqCtx a2asrv.RequestContext, queue eventqueue.Queue) error {
	fmt.Printf("Received message for task %s\n", reqCtx.TaskID())
	// Process the message and send events to the queue.
	return nil
}

// Cancel is called when a task is canceled.
func (a *MyAgent) Cancel(ctx context.Context, reqCtx a2asrv.RequestContext, queue eventqueue.Queue) error {
	fmt.Printf("Canceling task %s\n", reqCtx.TaskID())
	return nil
}

// Card returns the agent's public card.
func (a *MyAgent) Card() *a2a.AgentCard {
	return &a2a.AgentCard{
		ID:    "my-agent",
		Name:  "My Awesome Agent",
		// Add more details about the agent's capabilities here.
	}
}

func main() {
	// Create a new agent.
	agent := &MyAgent{}

	// Create a new gRPC server.
	grpcServer := grpc.NewServer()

	// Create a new A2A handler.
	handler := a2asrv.NewHandler(agent, agent, a2asrv.DefaultInMemoryManager())

	// Register the A2A service with the gRPC server.
	a2apb.RegisterA2AServiceServer(grpcServer, handler)

	// Start the gRPC server.
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	fmt.Println("gRPC server listening on :50051")
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
```

This example shows how to create a simple agent and expose it over gRPC. For more details on how to implement the agent logic, see the documentation for the `a2asrv` package.
