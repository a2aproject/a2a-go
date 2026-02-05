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

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
)

type streamingAgentExecutor struct{}

var _ a2asrv.AgentExecutor = (*streamingAgentExecutor)(nil)

func (*streamingAgentExecutor) Execute(ctx context.Context, reqCtx *a2asrv.RequestContext, q eventqueue.Queue) error {
	log.Printf("Executing request. TaskID: %s", reqCtx.TaskID)

	// 1. Initial State: Submitted
	if reqCtx.StoredTask == nil {
		if err := q.Write(ctx, a2a.NewStatusUpdateEvent(reqCtx, a2a.TaskStateSubmitted, nil)); err != nil {
			return fmt.Errorf("failed to write state submitted: %w", err)
		}
	}

	// 2. Transition to Working
	if err := q.Write(ctx, a2a.NewStatusUpdateEvent(reqCtx, a2a.TaskStateWorking, nil)); err != nil {
		return fmt.Errorf("failed to write state working: %w", err)
	}

	// 3. Stream content as Artifact chunks
	response := "Streaming responses allows agents to provide real-time feedback for long-running tasks.\n\nIt supports preserving whitespace, newlines, and structure."
	var artifactID a2a.ArtifactID

	// Simulate "typing" by chunks of runes (to preserve utf8 and whitespace)
	const chunkSize = 5
	runes := []rune(response)

	for i := 0; i < len(runes); i += chunkSize {
		end := i + chunkSize
		if end > len(runes) {
			end = len(runes)
		}
		chunk := string(runes[i:end])

		// Simulate processing delay (variable to feel more natural)
		select {
		case <-time.After(50 * time.Millisecond):
			// continue
		case <-ctx.Done():
			log.Printf("Task %s cancelled by client", reqCtx.TaskID)
			return ctx.Err()
		}

		var event *a2a.TaskArtifactUpdateEvent

		// First chunk creates the Artifact, subsequent chunks append to it.
		if artifactID == "" {
			event = a2a.NewArtifactEvent(reqCtx, a2a.TextPart{Text: chunk})
			artifactID = event.Artifact.ID
		} else {
			event = a2a.NewArtifactUpdateEvent(reqCtx, artifactID, a2a.TextPart{Text: chunk})
		}

		if err := q.Write(ctx, event); err != nil {
			return fmt.Errorf("failed to write artifact update: %w", err)
		}
	}

	// 4. Final State: Completed
	finalMsg := a2a.NewMessageForTask(a2a.MessageRoleAgent, reqCtx, a2a.TextPart{Text: "Stream finished."})
	compEvent := a2a.NewStatusUpdateEvent(reqCtx, a2a.TaskStateCompleted, finalMsg)
	compEvent.Final = true
	if err := q.Write(ctx, compEvent); err != nil {
		return fmt.Errorf("failed to write state completed: %w", err)
	}

	log.Printf("Task %s execution finished", reqCtx.TaskID)
	return nil
}

func (*streamingAgentExecutor) Cancel(ctx context.Context, reqCtx *a2asrv.RequestContext, q eventqueue.Queue) error {
	log.Printf("Cancellation requested for task %s", reqCtx.TaskID)
	cancelEvent := a2a.NewStatusUpdateEvent(reqCtx, a2a.TaskStateCanceled, nil)
	cancelEvent.Final = true
	return q.Write(ctx, cancelEvent)
}

var port = flag.Int("port", 9003, "Port for the A2A server to listen on.")

func main() {
	flag.Parse()

	// Handle graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	agentCard := &a2a.AgentCard{
		Name:               "Streaming Agent",
		Description:        "Demonstrates how to stream task results using Artifacts",
		URL:                fmt.Sprintf("http://127.0.0.1:%d/invoke", *port),
		PreferredTransport: a2a.TransportProtocolJSONRPC,
		Capabilities:       a2a.AgentCapabilities{Streaming: true},
	}

	requestHandler := a2asrv.NewHandler(&streamingAgentExecutor{})

	mux := http.NewServeMux()
	mux.Handle("/invoke", a2asrv.NewJSONRPCHandler(requestHandler))
	mux.Handle(a2asrv.WellKnownAgentCardPath, a2asrv.NewStaticAgentCardHandler(agentCard))

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", *port),
		Handler: mux,
		BaseContext: func(_ net.Listener) context.Context {
			return ctx
		},
	}

	go func() {
		log.Printf("Starting streaming example server on %s", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	<-ctx.Done()
	stop()
	log.Println("Shutting down server...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}
	log.Println("Server stopped gracefully")
}
