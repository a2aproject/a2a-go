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

// Package main demonstrates multi-agent task delegation using the A2A protocol.
//
// This example shows the core A2A multi-agent pattern:
//
//  1. A coordinator agent receives user requests and delegates work to
//     specialist agents based on the task content.
//  2. Specialist agents (translator, summarizer) each run as independent
//     A2A servers with their own AgentCards.
//  3. The coordinator uses the a2aclient SDK to discover specialist agents
//     by resolving their AgentCards and sends tasks to them.
//  4. Results from specialists are collected and combined into a single
//     response for the user.
//
// This pattern is fundamental to A2A: agents discover each other through
// AgentCards and communicate using the standard protocol, regardless of
// their internal implementation.
//
// Run with:
//
//	go run .
//
// The example starts three servers (translator, summarizer, coordinator)
// and sends a test request through the coordinator.
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

// --- Specialist Agent: Translator ---

// translatorExecutor simulates a translation agent.
// In a real system, this would call an LLM or translation API.
type translatorExecutor struct{}

var _ a2asrv.AgentExecutor = (*translatorExecutor)(nil)

func (*translatorExecutor) Execute(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		inputText := extractText(execCtx.Message)

		// Simulate translation work.
		translated := fmt.Sprintf("[Translated to French] %s -> Bonjour le monde!", inputText)

		response := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart(translated))
		yield(response, nil)
	}
}

func (*translatorExecutor) Cancel(_ context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCanceled, nil), nil)
	}
}

// --- Specialist Agent: Summarizer ---

// summarizerExecutor simulates a text summarization agent.
type summarizerExecutor struct{}

var _ a2asrv.AgentExecutor = (*summarizerExecutor)(nil)

func (*summarizerExecutor) Execute(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		inputText := extractText(execCtx.Message)

		// Simulate summarization work.
		wordCount := len(strings.Fields(inputText))
		summary := fmt.Sprintf("[Summary] Input had %d words. Key point: %s",
			wordCount, truncate(inputText, 50))

		response := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart(summary))
		yield(response, nil)
	}
}

func (*summarizerExecutor) Cancel(_ context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCanceled, nil), nil)
	}
}

// --- Coordinator Agent ---

// coordinatorExecutor receives user requests and delegates to specialist agents.
// It demonstrates how an A2A agent can act as a client to other A2A agents.
type coordinatorExecutor struct {
	// specialistURLs maps specialist names to their base URLs.
	// In production, these would be discovered through a registry or catalog.
	specialistURLs map[string]string
}

var _ a2asrv.AgentExecutor = (*coordinatorExecutor)(nil)

func (c *coordinatorExecutor) Execute(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		inputText := extractText(execCtx.Message)
		log.Printf("[Coordinator] Received: %q", inputText)

		// Step 1: Submit the task.
		if !yield(a2a.NewSubmittedTask(execCtx, execCtx.Message), nil) {
			return
		}

		// Step 2: Signal that we're working.
		if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateWorking, nil), nil) {
			return
		}

		// Step 3: Delegate to specialist agents and collect results.
		var results []string
		for name, baseURL := range c.specialistURLs {
			log.Printf("[Coordinator] Delegating to %s at %s", name, baseURL)

			result, err := c.delegateToSpecialist(ctx, baseURL, inputText)
			if err != nil {
				log.Printf("[Coordinator] %s failed: %v", name, err)
				results = append(results, fmt.Sprintf("%s: (error: %v)", name, err))
				continue
			}
			results = append(results, fmt.Sprintf("%s: %s", name, result))
		}

		// Step 4: Combine results into a single artifact.
		combined := fmt.Sprintf("Multi-agent results for %q:\n\n%s",
			truncate(inputText, 40), strings.Join(results, "\n"))

		event := a2a.NewArtifactEvent(execCtx, a2a.NewTextPart(combined))
		event.Artifact.Name = "combined-results"
		event.Artifact.Description = "Combined output from specialist agents"
		event.LastChunk = true
		if !yield(event, nil) {
			return
		}

		// Step 5: Mark complete.
		completedMsg := a2a.NewMessageForTask(
			a2a.MessageRoleAgent, execCtx,
			a2a.NewTextPart(fmt.Sprintf("Processed by %d specialists.", len(c.specialistURLs))),
		)
		yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCompleted, completedMsg), nil)
	}
}

// delegateToSpecialist creates a client connection to a specialist agent,
// sends the user's text, and returns the specialist's response.
func (c *coordinatorExecutor) delegateToSpecialist(ctx context.Context, baseURL, text string) (string, error) {
	// 1. Resolve the specialist's AgentCard.
	// This is how A2A agents discover each other's capabilities.
	card, err := agentcard.DefaultResolver.Resolve(ctx, baseURL)
	if err != nil {
		return "", fmt.Errorf("resolve agent card: %w", err)
	}

	// 2. Create a client for the specialist.
	client, err := a2aclient.NewFromCard(ctx, card)
	if err != nil {
		return "", fmt.Errorf("create client: %w", err)
	}
	defer func() { _ = client.Destroy() }()

	// 3. Send the message and get the result.
	msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart(text))
	resp, err := client.SendMessage(ctx, &a2a.SendMessageRequest{Message: msg})
	if err != nil {
		return "", fmt.Errorf("send message: %w", err)
	}

	// 4. Extract the text response.
	switch v := resp.(type) {
	case *a2a.Message:
		if len(v.Parts) > 0 {
			return v.Parts[0].Text(), nil
		}
		return "(empty message)", nil
	case *a2a.Task:
		// If the specialist returned a task, check for artifacts.
		if len(v.Artifacts) > 0 && len(v.Artifacts[0].Parts) > 0 {
			return v.Artifacts[0].Parts[0].Text(), nil
		}
		return fmt.Sprintf("(task %s in state %s)", v.ID, v.Status.State), nil
	default:
		return "(unexpected response type)", nil
	}
}

func (c *coordinatorExecutor) Cancel(_ context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCanceled, nil), nil)
	}
}

// --- Server Setup ---

func startSpecialistServer(name string, port int, executor a2asrv.AgentExecutor, ready chan<- struct{}) {
	addr := fmt.Sprintf("http://127.0.0.1:%d", port)
	agentCard := &a2a.AgentCard{
		Name:        name,
		Description: fmt.Sprintf("A specialist %s agent", strings.ToLower(name)),
		Version:     "1.0.0",
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(addr+"/invoke", a2a.TransportProtocolJSONRPC),
		},
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
		Capabilities:       a2a.AgentCapabilities{Streaming: true},
		Skills: []a2a.AgentSkill{
			{
				ID:          strings.ToLower(name),
				Name:        name,
				Description: fmt.Sprintf("Performs %s tasks", strings.ToLower(name)),
				Tags:        []string{strings.ToLower(name)},
			},
		},
	}

	requestHandler := a2asrv.NewHandler(executor)

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Fatalf("Failed to bind %s to port %d: %v", name, port, err)
	}
	log.Printf("[%s] Listening on 127.0.0.1:%d", name, port)

	mux := http.NewServeMux()
	mux.Handle("/invoke", a2asrv.NewJSONRPCHandler(requestHandler))
	mux.Handle(a2asrv.WellKnownAgentCardPath, a2asrv.NewStaticAgentCardHandler(agentCard))

	close(ready)
	if err := http.Serve(listener, mux); err != nil {
		log.Fatalf("[%s] Server stopped: %v", name, err)
	}
}

// --- Helpers ---

func extractText(msg *a2a.Message) string {
	if msg == nil || len(msg.Parts) == 0 {
		return ""
	}
	return msg.Parts[0].Text()
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// --- Main ---

var (
	translatorPort  = flag.Int("translator-port", 9010, "Port for the translator agent.")
	summarizerPort  = flag.Int("summarizer-port", 9011, "Port for the summarizer agent.")
	coordinatorPort = flag.Int("coordinator-port", 9012, "Port for the coordinator agent.")
)

func main() {
	flag.Parse()

	// 1. Start specialist agents.
	translatorReady := make(chan struct{})
	summarizerReady := make(chan struct{})

	go startSpecialistServer("Translator", *translatorPort, &translatorExecutor{}, translatorReady)
	go startSpecialistServer("Summarizer", *summarizerPort, &summarizerExecutor{}, summarizerReady)

	<-translatorReady
	<-summarizerReady

	// 2. Start the coordinator agent that knows about the specialists.
	coordinatorReady := make(chan struct{})
	coordinator := &coordinatorExecutor{
		specialistURLs: map[string]string{
			"Translator": fmt.Sprintf("http://127.0.0.1:%d", *translatorPort),
			"Summarizer": fmt.Sprintf("http://127.0.0.1:%d", *summarizerPort),
		},
	}

	go func() {
		addr := fmt.Sprintf("http://127.0.0.1:%d", *coordinatorPort)
		agentCard := &a2a.AgentCard{
			Name:        "Coordinator Agent",
			Description: "Delegates tasks to specialist agents and combines their results",
			Version:     "1.0.0",
			SupportedInterfaces: []*a2a.AgentInterface{
				a2a.NewAgentInterface(addr+"/invoke", a2a.TransportProtocolJSONRPC),
			},
			DefaultInputModes:  []string{"text"},
			DefaultOutputModes: []string{"text"},
			Capabilities:       a2a.AgentCapabilities{Streaming: true},
			Skills: []a2a.AgentSkill{
				{
					ID:          "coordinate",
					Name:        "Multi-Agent Coordinator",
					Description: "Sends user input to translation and summarization agents, then combines results.",
					Tags:        []string{"coordination", "multi-agent"},
					Examples:    []string{"process this text", "analyze and translate"},
				},
			},
		}

		requestHandler := a2asrv.NewHandler(coordinator)

		listener, err := net.Listen("tcp", fmt.Sprintf(":%d", *coordinatorPort))
		if err != nil {
			log.Fatalf("Failed to bind coordinator to port: %v", err)
		}
		log.Printf("[Coordinator] Listening on 127.0.0.1:%d", *coordinatorPort)

		mux := http.NewServeMux()
		mux.Handle("/invoke", a2asrv.NewJSONRPCHandler(requestHandler))
		mux.Handle(a2asrv.WellKnownAgentCardPath, a2asrv.NewStaticAgentCardHandler(agentCard))

		close(coordinatorReady)
		if err := http.Serve(listener, mux); err != nil {
			log.Fatalf("[Coordinator] Server stopped: %v", err)
		}
	}()

	<-coordinatorReady
	// Give the listener a moment to start accepting.
	time.Sleep(50 * time.Millisecond)

	// 3. Act as a user: send a request to the coordinator.
	log.Println("=== Sending request to coordinator ===")

	ctx := context.Background()
	coordURL := fmt.Sprintf("http://127.0.0.1:%d", *coordinatorPort)

	card, err := agentcard.DefaultResolver.Resolve(ctx, coordURL)
	if err != nil {
		log.Fatalf("Failed to resolve coordinator card: %v", err)
	}

	client, err := a2aclient.NewFromCard(ctx, card)
	if err != nil {
		log.Fatalf("Failed to create coordinator client: %v", err)
	}
	defer func() { _ = client.Destroy() }()

	msg := a2a.NewMessage(a2a.MessageRoleUser,
		a2a.NewTextPart("The A2A protocol enables seamless agent-to-agent communication across different platforms and implementations."),
	)

	resp, err := client.SendMessage(ctx, &a2a.SendMessageRequest{Message: msg})
	if err != nil {
		log.Fatalf("Failed to send message: %v", err)
	}

	// 4. Print the combined result.
	log.Println("=== Coordinator Response ===")
	switch v := resp.(type) {
	case *a2a.Task:
		log.Printf("Task ID: %s", v.ID)
		log.Printf("State:   %s", v.Status.State)
		for _, artifact := range v.Artifacts {
			log.Printf("Artifact %q:", artifact.Name)
			for _, part := range artifact.Parts {
				log.Println(part.Text())
			}
		}
	case *a2a.Message:
		for _, part := range v.Parts {
			log.Println(part.Text())
		}
	}
}
