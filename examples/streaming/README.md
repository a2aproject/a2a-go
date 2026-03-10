# Streaming Example

This example demonstrates how to implement a server that streams task results using `Artifact` updates, and a client that consumes these updates in real-time.

## Structure

*   `server/`: A simple HTTP server exposing an A2A agent that streams "typing" content.
*   `client/`: A CLI client that connects to the server, triggers a task, and prints the streamed output.

## Running the Example

### 1. Start the Server

```bash
go run ./examples/streaming/server/main.go
```

The server will start on port `9003`.

### 2. Run the Client

In a separate terminal, run:

```bash
go run ./examples/streaming/client/main.go
```

You should see the client connect, submit a task, and then receive streamed characters simulating a typing effect.
