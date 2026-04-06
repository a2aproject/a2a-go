// Copyright 2026 The A2A Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package main demonstrates streaming task results in the A2A protocol.
//
// This example shows the full streaming lifecycle:
//
//  1. The AgentExecutor emits a sequence of A2A events that model a long-running
//     task progressing through multiple states (submitted -> working -> completed).
//  2. Artifact content is streamed in chunks using TaskArtifactUpdateEvent, where
//     the first event creates the artifact and subsequent events append to it.
//  3. An embedded client consumes the stream by iterating over events and
//     printing each one as it arrives.
//
// This pattern is essential for agents that generate large outputs (code, documents,
// analysis) where waiting for the full result would be too slow.
//
// Run with:
//
//	go run .
//
// The example starts a server and immediately connects a client that streams a
// response, printing each event as it arrives.
package main

import (
	"context"
	"flag"
	"fmt"
	"iter"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aclient/agentcard"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

// --- Streaming Agent Executor ---

// streamingAgentExecutor demonstrates how to emit a sequence of A2A events
// that represent a long-running, multi-step task with streamed artifact output.
type streamingAgentExecutor struct{}

var _ a2asrv.AgentExecutor = (*streamingAgentExecutor)(nil)

func (*streamingAgentExecutor) Execute(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		// Step 1: Acknowledge the task by creating it in submitted state.
		// This is important for streaming because the client needs a task ID
		// to track progress and potentially resubscribe later.
		if !yield(a2a.NewSubmittedTask(execCtx, execCtx.Message), nil) {
			return
		}

		// Step 2: Signal that the agent has started working.
		// Clients can use this to show a "processing" indicator.
		workingMsg := a2a.NewMessageForTask(a2a.MessageRoleAgent, execCtx, a2a.NewTextPart("Generating story..."))
		if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateWorking, workingMsg), nil) {
			return
		}

		// Step 3: Stream artifact content in chunks.
		// The first NewArtifactEvent creates the artifact with a new ID.
		// Subsequent NewArtifactUpdateEvent calls append to that same artifact.
		// This models how an LLM generates tokens incrementally.
		storyChunks := []string{
			"Once upon a time, ",
			"in a digital realm, ",
			"two agents discovered ",
			"they could communicate ",
			"using the A2A protocol. ",
			"Together they solved problems ",
			"that neither could tackle alone.",
		}

		var artifactID a2a.ArtifactID
		for i, chunk := range storyChunks {
			// Simulate incremental generation delay.
			select {
			case <-ctx.Done():
				// Respect context cancellation (e.g., client disconnect).
				yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCanceled, nil), nil)
				return
			case <-time.After(100 * time.Millisecond):
			}

			var event *a2a.TaskArtifactUpdateEvent
			if i == 0 {
				// First chunk: create the artifact.
				event = a2a.NewArtifactEvent(execCtx, a2a.NewTextPart(chunk))
				event.Artifact.Name = "story"
				event.Artifact.Description = "A short story about A2A agents"
				artifactID = event.Artifact.ID
			} else {
				// Subsequent chunks: append to the existing artifact.
				event = a2a.NewArtifactUpdateEvent(execCtx, artifactID, a2a.NewTextPart(chunk))
			}

			// Mark the last chunk so clients know the artifact is complete.
			if i == len(storyChunks)-1 {
				event.LastChunk = true
			}

			if !yield(event, nil) {
				return
			}
		}

		// Step 4: Signal completion. This is a terminal state -- no more events
		// will be emitted and the task becomes immutable.
		completedMsg := a2a.NewMessageForTask(a2a.MessageRoleAgent, execCtx, a2a.NewTextPart("Story generation complete!"))
		yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCompleted, completedMsg), nil)
	}
}

func (*streamingAgentExecutor) Cancel(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCanceled, nil), nil)
	}
}

// --- Main ---

var port = flag.Int("port", 9003, "Port for the streaming A2A server.")

func main() {
	flag.Parse()

	// Start the server in a goroutine and run the client in main.
	addr := fmt.Sprintf("http://127.0.0.1:%d", *port)
	serverReady := make(chan struct{})

	go startServer(*port, addr, serverReady)

	// Wait for the server to be ready.
	<-serverReady
	log.Println("Server ready, starting streaming client...")

	// Run the streaming client.
	if err := runStreamingClient(addr); err != nil {
		log.Fatalf("Client error: %v", err)
	}
}

func startServer(port int, addr string, ready chan<- struct{}) {
	requestHandler := a2asrv.NewHandler(&streamingAgentExecutor{})

	agentCard := &a2a.AgentCard{
		Name:        "Streaming Story Agent",
		Description: "An agent that streams a story one chunk at a time",
		Version:     "1.0.0",
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(addr+"/invoke", a2a.TransportProtocolJSONRPC),
		},
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
		// Streaming must be declared in capabilities for clients to use SendStreamingMessage.
		Capabilities: a2a.AgentCapabilities{Streaming: true},
		Skills: []a2a.AgentSkill{
			{
				ID:          "story",
				Name:        "Story Generator",
				Description: "Generates a short story streamed in chunks.",
				Tags:        []string{"streaming", "creative"},
				Examples:    []string{"tell me a story", "write something"},
			},
		},
	}

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Fatalf("Failed to bind to port: %v", err)
	}
	log.Printf("Starting streaming A2A server on 127.0.0.1:%d", port)

	mux := http.NewServeMux()
	mux.Handle("/invoke", a2asrv.NewJSONRPCHandler(requestHandler))
	mux.Handle(a2asrv.WellKnownAgentCardPath, a2asrv.NewStaticAgentCardHandler(agentCard))

	close(ready)
	if err := http.Serve(listener, mux); err != nil {
		log.Fatalf("Server stopped: %v", err)
	}
}

func runStreamingClient(baseURL string) error {
	ctx := context.Background()

	// 1. Resolve the agent card to discover capabilities and transport details.
	card, err := agentcard.DefaultResolver.Resolve(ctx, baseURL)
	if err != nil {
		return fmt.Errorf("failed to resolve agent card: %w", err)
	}

	log.Printf("Connected to: %s (streaming=%v)", card.Name, card.Capabilities.Streaming)

	// 2. Create a client from the resolved card.
	client, err := a2aclient.NewFromCard(ctx, card)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}
	defer func() { _ = client.Destroy() }()

	// 3. Send a streaming message and iterate over events as they arrive.
	msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("Tell me a story"))
	req := &a2a.SendMessageRequest{Message: msg}

	log.Println("--- Streaming events ---")

	var storyParts []string
	for event, err := range client.SendStreamingMessage(ctx, req) {
		if err != nil {
			return fmt.Errorf("stream error: %w", err)
		}

		// Handle each event type. In a real application you would update a UI,
		// forward events to a websocket, or accumulate results.
		switch v := event.(type) {
		case *a2a.Task:
			log.Printf("[Task]     id=%s state=%s", v.ID, v.Status.State)

		case *a2a.TaskStatusUpdateEvent:
			statusMsg := ""
			if v.Status.Message != nil && len(v.Status.Message.Parts) > 0 {
				statusMsg = v.Status.Message.Parts[0].Text()
			}
			log.Printf("[Status]   state=%s msg=%q", v.Status.State, statusMsg)

		case *a2a.TaskArtifactUpdateEvent:
			text := ""
			if len(v.Artifact.Parts) > 0 {
				text = v.Artifact.Parts[0].Text()
			}
			storyParts = append(storyParts, text)
			label := "chunk"
			if v.LastChunk {
				label = "final"
			}
			log.Printf("[Artifact] id=%s %s=%q append=%v", v.Artifact.ID, label, text, v.Append)

		case *a2a.Message:
			text := ""
			if len(v.Parts) > 0 {
				text = v.Parts[0].Text()
			}
			log.Printf("[Message]  role=%s text=%q", v.Role, text)
		}
	}

	// 4. Print the assembled result.
	log.Println("--- Complete story ---")
	log.Println(strings.Join(storyParts, ""))

	return nil
}
