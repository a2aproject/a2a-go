# Issue 124: Streaming and Artifact Updates

## Summary
Developers often struggle with the correct pattern for streaming long-running or incremental responses in A2A. A common misconception is to use multiple `Message` events, which are intended to be atomic and terminal for a turn. The correct pattern involves using `TaskArtifactUpdateEvent` for content chunks and `TaskStatusUpdateEvent` for lifecycle transitions.

## Analysis
The current `examples/helloworld` implementation uses a simple single-message response. While this is appropriate for a basic example, it doesn't provide a reference for:
- Implementing an `AgentExecutor` that emits events over time.
- Correctly utilizing `Artifact` types for incremental content.
- Handling the event stream on the client side without premature cancellation.

Community feedback in issue #124 specifically requested an end-to-end example of the streaming flow.

## Proposed Solutions

### Option 1: Dedicated Streaming Example (Recommended)
Add a new, self-contained project in `examples/streaming`.
- **Server:** A "Typing Agent" that accepts a prompt and returns the response chunk-by-chunk using `TaskArtifactUpdateEvent` with delays.
- **Client:** A CLI tool that uses `SendStreamingMessage` and prints tokens to `stdout` in real-time.

### Option 2: Multi-mode Hello World
Enhance `examples/helloworld` to support both single-message and streaming responses, toggled via command-line flags or input content.

### Option 3: Documentation Recipe
Add a dedicated guide in `docs/recipes/streaming.md` containing the code snippets and theoretical explanation.

## Recommendation
**Option 1** is recommended. It provides the clearest and most isolated learning environment for this specific protocol pattern, matching the existing modular approach of the repository (e.g., `clustermode`, `inputrequired`).
