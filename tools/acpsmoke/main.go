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

// Command acpsmoke runs a scripted conversation against a real ACP agent
// served by the a2aacp executor over A2A JSON-RPC: a text turn, an approved
// and a denied tool call, a follow-up in the same context and a cancel.
// The agent must be configured to ask before running shell commands.
// See tools/acp_smoke.sh.
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
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/integrations/executor/a2aacp"
)

func main() {
	command := flag.String("cmd", "", "agent command line (required), e.g. \"opencode acp\"")
	dir := flag.String("dir", "", "workspace directory (required)")
	verbose := flag.Bool("v", false, "print agent stderr")
	timeout := flag.Duration("timeout", 3*time.Minute, "timeout of each step")
	flag.Parse()

	if *dir == "" || *command == "" {
		log.Fatal("-cmd and -dir are required")
	}
	agent := a2aacp.Process{Argv: strings.Fields(*command), Dir: *dir}
	if *verbose {
		agent.Stderr = os.Stderr
	}
	if err := run(agent, *timeout); err != nil {
		log.Fatal(err)
	}
}

func run(agent a2aacp.Process, timeout time.Duration) error {
	ctx := context.Background()
	dir := agent.Dir
	executor, err := a2aacp.NewExecutor(ctx, agent, &a2aacp.Config{Settings: a2aacp.MetadataSettings("mode")})
	if err != nil {
		return err
	}
	defer func() {
		if err := executor.Close(); err != nil {
			log.Printf("closing executor: %v", err)
		}
	}()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	server := &http.Server{Handler: a2asrv.NewJSONRPCHandler(a2asrv.NewHandler(executor))}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("serving: %v", err)
		}
	}()
	defer func() {
		if err := server.Close(); err != nil {
			log.Printf("closing server: %v", err)
		}
	}()

	card := &a2a.AgentCard{
		SupportedInterfaces: []*a2a.AgentInterface{a2a.NewAgentInterface("http://"+listener.Addr().String(), a2a.TransportProtocolJSONRPC)},
		Capabilities:        a2a.AgentCapabilities{Streaming: true},
	}
	client, err := a2aclient.NewFromCard(ctx, card)
	if err != nil {
		return err
	}

	s := &smoke{client: client, dir: dir, contextID: a2a.NewContextID(), timeout: timeout}
	steps := []struct {
		name string
		fn   func(context.Context) error
	}{
		{"text turn", s.textTurn},
		{"session mode via metadata", s.sessionMode},
		{"approve tool call", s.approve},
		{"follow-up in context", s.followUp},
		{"deny tool call", s.deny},
		{"cancel while input-required", s.cancel},
	}
	var failed int
	for _, step := range steps {
		fmt.Printf("=== %s\n", step.name)
		stepCtx, cancel := context.WithTimeout(ctx, timeout)
		err := step.fn(stepCtx)
		cancel()
		if err != nil {
			failed++
			fmt.Printf("--- FAIL: %s: %v\n", step.name, err)
			continue
		}
		fmt.Printf("--- PASS: %s\n", step.name)
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d steps failed", failed, len(steps))
	}
	return nil
}

type smoke struct {
	client    *a2aclient.Client
	dir       string
	contextID string
	timeout   time.Duration
}

type result struct {
	task  *a2a.Task
	state a2a.TaskState
	text  string
}

// send streams a message and returns the task state after the stream ends.
func (s *smoke) send(ctx context.Context, taskID a2a.TaskID, text string) (*result, error) {
	return s.sendWithMetadata(ctx, taskID, text, nil)
}

func (s *smoke) sendWithMetadata(ctx context.Context, taskID a2a.TaskID, text string, metadata map[string]any) (*result, error) {
	msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart(text))
	msg.ContextID = s.contextID
	msg.TaskID = taskID
	msg.Metadata = metadata
	fmt.Printf("> %s\n", text)

	res := &result{}
	var sb strings.Builder
	for ev, err := range s.client.SendStreamingMessage(ctx, &a2a.SendMessageRequest{Message: msg}) {
		if err != nil {
			return nil, err
		}
		switch v := ev.(type) {
		case *a2a.Task:
			res.task, res.state = v, v.Status.State
		case *a2a.TaskStatusUpdateEvent:
			res.state = v.Status.State
			if v.Status.Message != nil {
				fmt.Printf("  [%s] %s\n", v.Status.State, partsText(v.Status.Message.Parts))
			}
			if res.task == nil {
				res.task = &a2a.Task{ID: v.TaskID, ContextID: v.ContextID}
			}
		case *a2a.TaskArtifactUpdateEvent:
			sb.WriteString(partsText(v.Artifact.Parts))
		}
	}
	res.text = sb.String()
	if res.text != "" {
		fmt.Printf("< %s\n", res.text)
	}
	fmt.Printf("  = %s\n", res.state)
	return res, nil
}

func partsText(parts []*a2a.Part) string {
	var out []string
	for _, p := range parts {
		if t := p.Text(); t != "" {
			out = append(out, t)
		}
	}
	return strings.Join(out, " ")
}

func expectState(r *result, want ...a2a.TaskState) error {
	if slices.Contains(want, r.state) {
		return nil
	}
	return fmt.Errorf("task state = %s, want one of %v", r.state, want)
}

func (s *smoke) textTurn(ctx context.Context) error {
	r, err := s.send(ctx, "", "Reply with exactly the word pong and nothing else.")
	if err != nil {
		return err
	}
	if err := expectState(r, a2a.TaskStateCompleted); err != nil {
		return err
	}
	if !strings.Contains(strings.ToLower(r.text), "pong") {
		return fmt.Errorf("response %q does not contain pong", r.text)
	}
	return nil
}

func (s *smoke) sessionMode(ctx context.Context) error {
	metadata := map[string]any{a2aacp.SettingsMetadataKey: map[string]any{"mode": "build"}}
	r, err := s.sendWithMetadata(ctx, "", "Reply with exactly the word ok and nothing else.", metadata)
	if err != nil {
		return err
	}
	return expectState(r, a2a.TaskStateCompleted)
}

func (s *smoke) approve(ctx context.Context) error {
	r, err := s.send(ctx, "", "Use the bash tool to run exactly this command and nothing else: echo smoke > approved.txt")
	if err != nil {
		return err
	}
	if err := expectState(r, a2a.TaskStateInputRequired); err != nil {
		return err
	}
	if r, err = s.send(ctx, r.task.ID, "once"); err != nil {
		return err
	}
	if err := expectState(r, a2a.TaskStateCompleted); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(s.dir, "approved.txt")); err != nil {
		return fmt.Errorf("approved command did not run: %w", err)
	}
	return nil
}

func (s *smoke) followUp(ctx context.Context) error {
	r, err := s.send(ctx, "", "What is the name of the file you just created? Answer with the file name only.")
	if err != nil {
		return err
	}
	if err := expectState(r, a2a.TaskStateCompleted); err != nil {
		return err
	}
	if !strings.Contains(r.text, "approved.txt") {
		return fmt.Errorf("response %q does not mention approved.txt", r.text)
	}
	return nil
}

func (s *smoke) deny(ctx context.Context) error {
	r, err := s.send(ctx, "", "Use the bash tool to run exactly this command and nothing else: rm approved.txt")
	if err != nil {
		return err
	}
	if err := expectState(r, a2a.TaskStateInputRequired); err != nil {
		return err
	}
	if r, err = s.send(ctx, r.task.ID, "reject"); err != nil {
		return err
	}
	if err := expectState(r, a2a.TaskStateCompleted, a2a.TaskStateFailed); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(s.dir, "approved.txt")); err != nil {
		return fmt.Errorf("denied command ran: %w", err)
	}
	return nil
}

func (s *smoke) cancel(ctx context.Context) error {
	r, err := s.send(ctx, "", "Use the bash tool to run exactly this command and nothing else: sleep 60")
	if err != nil {
		return err
	}
	if err := expectState(r, a2a.TaskStateInputRequired); err != nil {
		return err
	}
	task, err := s.client.CancelTask(ctx, &a2a.CancelTaskRequest{ID: r.task.ID})
	if err != nil {
		return err
	}
	if task.Status.State != a2a.TaskStateCanceled {
		return fmt.Errorf("task state after cancel = %s, want %s", task.Status.State, a2a.TaskStateCanceled)
	}
	fmt.Printf("  [%s]\n", task.Status.State)
	return nil
}
