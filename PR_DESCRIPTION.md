# PR Title
feat: add streaming example (issue #124)

# PR Description
## Description

This PR adds a dedicated example in `examples/streaming` that demonstrates the correct pattern for implementing streaming agents using the A2A protocol. 

This addresses the confusion raised in issue #124 regarding how to implement time-delayed, chunked responses versus atomic messages.

## Key Features

### Server (`examples/streaming/server`)
- **Typewriter Simulation**: An agent that streams a text response chunk-by-chunk with simulated processing delays.
- **Correct Event Usage**: 
  - Emits `TaskStateSubmitted` and `TaskStateWorking` for lifecycle management.
  - Uses `TaskArtifactUpdateEvent` with `Append: true` for content streaming.
  - Terminates with `TaskStateCompleted` and a final message.
- **Graceful Shutdown**: Properly handles SIGINT/SIGTERM to close the listener and cleanup resources.
- **Whitespace Preservation**: Uses rune-based iteration to ensure spaces and newlines are streamed accurately.

### Client (`examples/streaming/client`)
- **Real-time Consumption**: Uses `client.SendStreamingMessage` to consume the event stream.
- **UI Handling**: Demonstrates how to differentiate between content updates (printing to stdout) and status updates (logging on new lines) to avoid output tearing and provide a smooth "live" effect.

## Fixes Issue
- Closes #124
