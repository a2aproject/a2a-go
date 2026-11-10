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
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
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

func TestConn_ResponseBeforePeerExitIsDelivered(t *testing.T) {
	t.Parallel()
	for range 200 {
		client, _, _ := newTestClient(t, func(agent *Conn, m *Message) {
			if err := agent.Respond(m.ID, PromptResponse{StopReason: StopEndTurn}); err != nil {
				t.Errorf("agent.Respond() error = %v", err)
			}
			if err := agent.Close(); err != nil {
				t.Errorf("agent.Close() error = %v", err)
			}
		})

		resp, err := client.Prompt(testContext(t), PromptRequest{SessionID: "s1"})

		if err != nil {
			t.Fatalf("client.Prompt() error = %v, want nil", err)
		}
		if resp.StopReason != StopEndTurn {
			t.Fatalf("client.Prompt() = %v, want %v", resp.StopReason, StopEndTurn)
		}
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

// newRawAgent connects a client to an agent played by the test through raw
// lines: it reads what the client writes and writes agent output verbatim.
func newRawAgent(t *testing.T) (*Client, *bufio.Reader, io.Writer) {
	t.Helper()
	clientR, agentW := io.Pipe()
	agentR, clientW := io.Pipe()
	client := NewClient(pipeRWC{r: clientR, w: clientW}, &recordingHandler{})
	client.Start()
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Errorf("client.Close() error = %v", err)
		}
		if err := agentW.Close(); err != nil {
			t.Errorf("agentW.Close() error = %v", err)
		}
		if err := agentR.Close(); err != nil {
			t.Errorf("agentR.Close() error = %v", err)
		}
	})
	return client, bufio.NewReader(agentR), agentW
}

// ioTimeout is generous because race-enabled tests decode long lines slowly.
const ioTimeout = 30 * time.Second

func readLine(t *testing.T, r *bufio.Reader) []byte {
	t.Helper()
	type result struct {
		line []byte
		err  error
	}
	results := make(chan result, 1)
	go func() {
		line, err := r.ReadBytes('\n')
		results <- result{line, err}
	}()
	select {
	case got := <-results:
		if got.err != nil {
			t.Fatalf("ReadBytes() error = %v", got.err)
		}
		return got.line
	case <-time.After(ioTimeout):
		t.Fatal("ReadBytes() did not return, want a message from the client")
		return nil
	}
}

func readRequestID(t *testing.T, r *bufio.Reader) string {
	t.Helper()
	line := readLine(t, r)
	var m Message
	if err := json.Unmarshal(line, &m); err != nil {
		t.Fatalf("json.Unmarshal(%s) error = %v", line, err)
	}
	return string(m.ID)
}

func writeString(t *testing.T, w io.Writer, s string) {
	t.Helper()
	errs := make(chan error, 1)
	go func() {
		_, err := io.WriteString(w, s)
		errs <- err
	}()
	select {
	case err := <-errs:
		if err != nil {
			t.Fatalf("WriteString() error = %v", err)
		}
	case <-time.After(ioTimeout):
		t.Fatal("WriteString() did not return, want the client to read agent output")
	}
}

func TestConn_SkipsOutputWhichIsNotJSONRPC(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		output string
	}{
		{name: "plain text", output: "npm WARN config production Use `--omit=dev` instead.\n"},
		{name: "JSON log", output: `{"level":"info","msg":"agent started"}` + "\n"},
		{name: "JSON value", output: "[1,2,3]\n42\nnull\n"},
		{name: "truncated JSON", output: `{"jsonrpc":"2.0","id":` + "\n"},
		{name: "mistyped notification method", output: `{"jsonrpc":"2.0","method":5,"params":{}}` + "\n"},
		{name: "blank lines", output: "\n\r\n  \n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			client, r, w := newRawAgent(t)
			type result struct {
				resp *PromptResponse
				err  error
			}
			results := make(chan result, 1)
			go func() {
				resp, err := client.Prompt(testContext(t), PromptRequest{SessionID: "s1"})
				results <- result{resp, err}
			}()
			id := readRequestID(t, r)

			writeString(t, w, tc.output)
			writeString(t, w, `{"jsonrpc":"2.0","id":`+id+`,"result":{"stopReason":"end_turn"}}`+"\r\n")

			got := <-results
			if got.err != nil {
				t.Fatalf("client.Prompt() error = %v, want nil", got.err)
			}
			if got.resp.StopReason != StopEndTurn {
				t.Fatalf("client.Prompt() = %v, want %v", got.resp.StopReason, StopEndTurn)
			}
		})
	}
}

func TestConn_RejectsRequestWithMistypedMethod(t *testing.T) {
	t.Parallel()
	_, r, w := newRawAgent(t)

	writeString(t, w, `{"jsonrpc":"2.0","id":7,"method":{"name":"session/request_permission"}}`+"\n")

	line := readLine(t, r)
	var got Message
	if err := json.Unmarshal(line, &got); err != nil {
		t.Fatalf("json.Unmarshal(%s) error = %v", line, err)
	}
	if diff := cmp.Diff(Message{ID: json.RawMessage("7"), Error: &Error{Code: CodeInvalidRequest, Message: "method must be a non-empty string"}}, got); diff != "" {
		t.Fatalf("client response wrong result (-want +got) diff = %s", diff)
	}
}

func TestConn_MalformedResponseFailsOnlyItsCall(t *testing.T) {
	t.Parallel()
	client, r, w := newRawAgent(t)
	ctx := testContext(t)
	errs := make(chan error, 1)
	go func() {
		_, err := client.Prompt(ctx, PromptRequest{SessionID: "s1"})
		errs <- err
	}()
	id := readRequestID(t, r)

	writeString(t, w, `{"jsonrpc":"2.0","id":`+id+`,"error":"rate limited"}`+"\n")

	if err := <-errs; err == nil || !strings.Contains(err.Error(), "decoding session/prompt error") {
		t.Fatalf("client.Prompt() error = %v, want decoding session/prompt error", err)
	}
	sessions := make(chan error, 1)
	go func() {
		_, err := client.NewSession(ctx, NewSessionRequest{})
		sessions <- err
	}()
	id = readRequestID(t, r)
	writeString(t, w, `{"jsonrpc":"2.0","id":`+id+`,"result":{"sessionId":"s2"}}`+"\n")
	if err := <-sessions; err != nil {
		t.Fatalf("client.NewSession() error = %v, want nil", err)
	}
}

func TestConn_ReadsLongLines(t *testing.T) {
	t.Parallel()
	client, r, w := newRawAgent(t)
	type result struct {
		resp *NewSessionResponse
		err  error
	}
	results := make(chan result, 1)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	go func() {
		resp, err := client.NewSession(ctx, NewSessionRequest{})
		results <- result{resp, err}
	}()
	id := readRequestID(t, r)
	sessionID := strings.Repeat("s", 20<<20)

	writeString(t, w, `{"jsonrpc":"2.0","id":`+id+`,"result":{"sessionId":"`+sessionID+`"}}`+"\r\n")

	got := <-results
	if got.err != nil {
		t.Fatalf("client.NewSession() error = %v, want nil", got.err)
	}
	if len(got.resp.SessionID) != len(sessionID) {
		t.Fatalf("client.NewSession() session ID length = %d, want %d", len(got.resp.SessionID), len(sessionID))
	}
}
