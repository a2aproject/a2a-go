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

package acp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

type pipeRWC struct {
	r *io.PipeReader
	w *io.PipeWriter
}

func (p pipeRWC) Read(b []byte) (int, error)  { return p.r.Read(b) }
func (p pipeRWC) Write(b []byte) (int, error) { return p.w.Write(b) }
func (p pipeRWC) Close() error {
	if err := p.w.Close(); err != nil {
		return err
	}
	return p.r.Close()
}

// recordingHandler passes what the client receives to channels.
type recordingHandler struct {
	updates     chan *SessionNotification
	permissions chan json.RawMessage
	forms       chan *CreateElicitationRequest
}

func (h *recordingHandler) SessionUpdate(n *SessionNotification) { h.updates <- n }
func (h *recordingHandler) RequestPermission(id json.RawMessage, _ *RequestPermissionRequest) {
	h.permissions <- id
}
func (h *recordingHandler) CreateElicitation(_ json.RawMessage, req *CreateElicitationRequest) {
	h.forms <- req
}

// newTestClient connects a client to an agent played by agentHandle. Incoming
// agent messages are passed to agentHandle on the agent's read goroutine.
func newTestClient(t *testing.T, agentHandle func(agent *Conn, m *Message)) (*Client, *Conn, *recordingHandler) {
	t.Helper()
	clientR, agentW := io.Pipe()
	agentR, clientW := io.Pipe()
	h := &recordingHandler{
		updates:     make(chan *SessionNotification, 1),
		permissions: make(chan json.RawMessage, 1),
		forms:       make(chan *CreateElicitationRequest, 1),
	}
	client := NewClient(pipeRWC{r: clientR, w: clientW}, h)
	var agent *Conn
	agent = NewConn(pipeRWC{r: agentR, w: agentW}, func(m *Message) { agentHandle(agent, m) })
	agent.Start()
	client.Start()
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Errorf("client.Close() error = %v", err)
		}
		if err := agent.Close(); err != nil {
			t.Errorf("agent.Close() error = %v", err)
		}
	})
	return client, agent, h
}

func testContext(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestClient_Calls(t *testing.T) {
	t.Parallel()
	got := make(chan string, 2)
	client, _, _ := newTestClient(t, func(agent *Conn, m *Message) {
		got <- m.Method + " " + string(m.Params)
		if len(m.ID) == 0 {
			return
		}
		var result any = NewSessionResponse{SessionID: "s1", SessionState: SessionState{Modes: &struct{}{}}}
		if m.Method == MethodSessionPrompt {
			result = PromptResponse{StopReason: StopEndTurn}
		}
		if err := agent.Respond(m.ID, result); err != nil {
			t.Errorf("agent.Respond() error = %v", err)
		}
	})
	ctx := testContext(t)

	session, err := client.NewSession(ctx, NewSessionRequest{Cwd: "/w", MCPServers: []json.RawMessage{}})
	if err != nil {
		t.Fatalf("client.NewSession() error = %v", err)
	}
	if diff := cmp.Diff(&NewSessionResponse{SessionID: "s1", SessionState: SessionState{Modes: &struct{}{}}}, session); diff != "" {
		t.Fatalf("client.NewSession() wrong result (-want +got) diff = %s", diff)
	}
	if want := `session/new {"cwd":"/w","mcpServers":[]}`; <-got != want {
		t.Fatalf("agent received wrong session/new, want %s", want)
	}

	prompt, err := client.Prompt(ctx, PromptRequest{SessionID: "s1", Prompt: []ContentBlock{{Type: "text", Text: "hi"}}})
	if err != nil {
		t.Fatalf("client.Prompt() error = %v", err)
	}
	if prompt.StopReason != StopEndTurn {
		t.Fatalf("client.Prompt() = %v, want %v", prompt.StopReason, StopEndTurn)
	}
	<-got

	if err := client.Cancel("s1"); err != nil {
		t.Fatalf("client.Cancel() error = %v", err)
	}
	if got, want := <-got, `session/cancel {"sessionId":"s1"}`; got != want {
		t.Fatalf("agent received = %s, want %s", got, want)
	}
}

func TestClient_CallError(t *testing.T) {
	t.Parallel()
	client, _, _ := newTestClient(t, func(agent *Conn, m *Message) {
		if err := agent.RespondError(m.ID, &Error{Code: -32000, Message: "auth required"}); err != nil {
			t.Errorf("agent.RespondError() error = %v", err)
		}
	})

	_, err := client.NewSession(testContext(t), NewSessionRequest{})

	var rpcErr *Error
	if !errors.As(err, &rpcErr) || rpcErr.Message != "auth required" {
		t.Fatalf("client.NewSession() error = %v, want auth required", err)
	}
}

func TestClient_AgentRequests(t *testing.T) {
	t.Parallel()
	client, agent, h := newTestClient(t, func(*Conn, *Message) {})
	ctx := testContext(t)

	if err := agent.Notify(MethodSessionUpdate, SessionNotification{SessionID: "s1", Update: SessionUpdate{SessionUpdate: UpdateToolCall}}); err != nil {
		t.Fatalf("agent.Notify() error = %v", err)
	}
	if got := <-h.updates; got.SessionID != "s1" || got.Update.SessionUpdate != UpdateToolCall {
		t.Fatalf("Handler.SessionUpdate() got %+v", got)
	}

	outcomes := make(chan RequestPermissionResponse, 1)
	go func() {
		var resp RequestPermissionResponse
		if err := agent.Call(ctx, MethodRequestPermission, RequestPermissionRequest{SessionID: "s1"}, &resp); err != nil {
			t.Errorf("agent.Call(request_permission) error = %v", err)
		}
		outcomes <- resp
	}()
	if err := client.RespondPermission(<-h.permissions, PermissionOutcome{Outcome: OutcomeSelected, OptionID: "o1"}); err != nil {
		t.Fatalf("client.RespondPermission() error = %v", err)
	}
	if diff := cmp.Diff(RequestPermissionResponse{Outcome: PermissionOutcome{Outcome: OutcomeSelected, OptionID: "o1"}}, <-outcomes); diff != "" {
		t.Fatalf("permission response wrong result (-want +got) diff = %s", diff)
	}
}

func TestClient_RejectsUnsupportedRequests(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		method   string
		params   any
		wantCode int
	}{
		{name: "unknown method", method: "fs/read_text_file", params: map[string]any{"path": "/etc/passwd"}, wantCode: CodeMethodNotFound},
		{name: "malformed params", method: MethodRequestPermission, params: map[string]any{"options": "none"}, wantCode: CodeInvalidParams},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, agent, _ := newTestClient(t, func(*Conn, *Message) {})

			err := agent.Call(testContext(t), tc.method, tc.params, nil)

			var rpcErr *Error
			if !errors.As(err, &rpcErr) || rpcErr.Code != tc.wantCode {
				t.Fatalf("agent.Call(%s) error = %v, want code %d", tc.method, err, tc.wantCode)
			}
		})
	}
}

func TestConn_PeerExitFailsCalls(t *testing.T) {
	t.Parallel()
	exited := make(chan struct{})
	client, agent, _ := newTestClient(t, func(*Conn, *Message) { close(exited) })
	errs := make(chan error, 1)
	go func() {
		_, err := client.Prompt(testContext(t), PromptRequest{SessionID: "s1"})
		errs <- err
	}()
	<-exited

	if err := agent.Close(); err != nil {
		t.Fatalf("agent.Close() error = %v", err)
	}

	if err := <-errs; !errors.Is(err, ErrAgentExited) {
		t.Fatalf("client.Prompt() error = %v, want %v", err, ErrAgentExited)
	}
	<-client.Done()
}
