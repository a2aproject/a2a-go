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

package a2aacp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

const testTimeout = 5 * time.Second

// fakeHarness starts in-process ACP agents connected over pipes. Every
// session/prompt runs onPrompt in its own goroutine.
type fakeHarness struct {
	t        *testing.T
	caps     agentCapabilities
	state    sessionState
	onPrompt func(*promptScript)

	mu     sync.Mutex
	agents []*fakeAgent
}

type fakeAgent struct {
	h    *fakeHarness
	conn *conn

	mu         sync.Mutex
	nextID     int
	created    []string
	loaded     []string
	prompts    []promptRequest
	canceled   []string
	settings   []string
	mcpServers []json.RawMessage
	crashed    bool
}

func (h *fakeHarness) start(context.Context) (io.ReadWriteCloser, error) {
	clientR, agentW := io.Pipe()
	agentR, clientW := io.Pipe()
	f := &fakeAgent{h: h}
	f.conn = newConn(pipeRWC{r: agentR, w: agentW}, f.handle)
	f.conn.start()
	h.mu.Lock()
	h.agents = append(h.agents, f)
	h.mu.Unlock()
	return pipeRWC{r: clientR, w: clientW}, nil
}

func (h *fakeHarness) agent(i int) *fakeAgent {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.agents[i]
}

func (h *fakeHarness) agentCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.agents)
}

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

func (f *fakeAgent) handle(m *rpcMessage) {
	t := f.h.t
	f.mu.Lock()
	defer f.mu.Unlock()
	var result any
	switch m.Method {
	case methodInitialize:
		result = initializeResponse{ProtocolVersion: protocolVersion, AgentCapabilities: f.h.caps}
	case methodSessionNew:
		var req newSessionRequest
		mustUnmarshal(t, m.Params, &req)
		f.mcpServers = req.MCPServers
		f.nextID++
		id := fmt.Sprintf("sess-%d", f.nextID)
		f.created = append(f.created, id)
		result = newSessionResponse{SessionID: id, sessionState: f.h.state}
	case methodSessionLoad:
		var req loadSessionRequest
		mustUnmarshal(t, m.Params, &req)
		f.loaded = append(f.loaded, req.SessionID)
		result = f.h.state
	case methodSetConfigOption:
		var req setConfigOptionRequest
		mustUnmarshal(t, m.Params, &req)
		f.settings = append(f.settings, fmt.Sprintf("%s %s=%v %s", m.Method, req.ConfigID, req.Value, req.Type))
		result = setConfigOptionResponse{ConfigOptions: f.h.state.ConfigOptions}
	case methodSetMode:
		var req setModeRequest
		mustUnmarshal(t, m.Params, &req)
		f.settings = append(f.settings, fmt.Sprintf("%s %s", m.Method, req.ModeID))
	case methodSetModel:
		var req setModelRequest
		mustUnmarshal(t, m.Params, &req)
		f.settings = append(f.settings, fmt.Sprintf("%s %s", m.Method, req.ModelID))
	case methodSessionPrompt:
		var req promptRequest
		mustUnmarshal(t, m.Params, &req)
		f.prompts = append(f.prompts, req)
		go f.h.onPrompt(&promptScript{t: t, agent: f, id: m.ID, req: req})
		return
	case methodSessionCancel:
		var n cancelNotification
		mustUnmarshal(t, m.Params, &n)
		f.canceled = append(f.canceled, n.SessionID)
		return
	default:
		t.Errorf("fake agent got unexpected method %q", m.Method)
		return
	}
	if err := f.conn.respond(m.ID, result); err != nil {
		t.Errorf("fake agent respond() error = %v", err)
	}
}

func (f *fakeAgent) snapshot() (created, loaded, canceled []string, prompts []promptRequest) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.created, f.loaded, f.canceled, f.prompts
}

func (f *fakeAgent) recordedSettings() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.settings
}

func (f *fakeAgent) isCrashed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.crashed
}

func (f *fakeAgent) crash() {
	f.mu.Lock()
	f.crashed = true
	f.mu.Unlock()
	if err := f.conn.Close(); err != nil {
		f.h.t.Errorf("fake agent Close() error = %v", err)
	}
}

func mustUnmarshal(t *testing.T, data json.RawMessage, v any) {
	t.Helper()
	if err := json.Unmarshal(data, v); err != nil {
		t.Errorf("json.Unmarshal(%s) error = %v", data, err)
	}
}

// promptScript lets a test play the agent during one prompt turn.
type promptScript struct {
	t     *testing.T
	agent *fakeAgent
	id    json.RawMessage
	req   promptRequest
}

func (s *promptScript) promptText() string {
	var texts []string
	for _, b := range s.req.Prompt {
		texts = append(texts, b.Text)
	}
	return strings.Join(texts, " ")
}

func (s *promptScript) update(u map[string]any) {
	err := s.agent.conn.notify(methodSessionUpdate, map[string]any{"sessionId": s.req.SessionID, "update": u})
	if err != nil {
		s.t.Errorf("fake agent notify() error = %v", err)
	}
}

func (s *promptScript) text(messageID, text string) {
	s.update(map[string]any{
		"sessionUpdate": updateAgentMessageChunk,
		"messageId":     messageID,
		"content":       map[string]any{"type": "text", "text": text},
	})
}

func (s *promptScript) tool(id, status string) {
	s.update(map[string]any{
		"sessionUpdate": updateToolCallUpdate,
		"toolCallId":    id,
		"status":        status,
	})
}

var testPermissionOptions = []permissionOption{
	{OptionID: "opt-allow", Name: "Allow", Kind: permissionAllowOnce},
	{OptionID: "opt-always", Name: "Always Allow", Kind: permissionAllowAlways},
	{OptionID: "opt-reject", Name: "Reject", Kind: permissionRejectOnce},
}

func (s *promptScript) askPermission(tc toolCall) permissionOutcome {
	var resp requestPermissionResponse
	req := requestPermissionRequest{SessionID: s.req.SessionID, ToolCall: tc, Options: testPermissionOptions}
	if err := s.agent.conn.call(context.Background(), methodRequestPermission, req, &resp); err != nil && !s.agent.isCrashed() {
		s.t.Errorf("fake agent request_permission error = %v", err)
	}
	return resp.Outcome
}

func (s *promptScript) elicit(message string, schema map[string]any) createElicitationResponse {
	var resp createElicitationResponse
	req := createElicitationRequest{Mode: "form", SessionID: s.req.SessionID, Message: message, RequestedSchema: schema}
	if err := s.agent.conn.call(context.Background(), methodElicitationCreate, req, &resp); err != nil {
		s.t.Errorf("fake agent elicitation/create error = %v", err)
	}
	return resp
}

func (s *promptScript) end(reason stopReason) {
	if err := s.agent.conn.respond(s.id, promptResponse{StopReason: reason}); err != nil {
		s.t.Errorf("fake agent respond() error = %v", err)
	}
}

func newTestExecutor(t *testing.T, h *fakeHarness, cfg Config) *Executor {
	t.Helper()
	h.t = t
	cfg.start = h.start
	cfg.Dir = t.TempDir()
	e, err := NewExecutor(t.Context(), cfg)
	if err != nil {
		t.Fatalf("NewExecutor() error = %v", err)
	}
	t.Cleanup(func() {
		if err := e.Close(); err != nil {
			t.Errorf("Executor.Close() error = %v", err)
		}
	})
	return e
}

func newExecCtx(contextID string, parts ...*a2a.Part) *a2asrv.ExecutorContext {
	return &a2asrv.ExecutorContext{
		TaskID:    a2a.NewTaskID(),
		ContextID: contextID,
		Message:   a2a.NewMessage(a2a.MessageRoleUser, parts...),
	}
}

// followUp is the context of a message the caller sends to an existing task.
func followUp(prev *a2asrv.ExecutorContext, parts ...*a2a.Part) *a2asrv.ExecutorContext {
	return &a2asrv.ExecutorContext{
		TaskID:     prev.TaskID,
		ContextID:  prev.ContextID,
		Message:    a2a.NewMessageForTask(a2a.MessageRoleUser, prev, parts...),
		StoredTask: &a2a.Task{ID: prev.TaskID, ContextID: prev.ContextID, Status: a2a.TaskStatus{State: a2a.TaskStateInputRequired}},
	}
}

// eventSummary is the part of an event tests compare, without generated IDs and timestamps.
type eventSummary struct {
	Kind   string
	State  a2a.TaskState
	Text   string
	Data   []any
	Append bool
}

func summarize(ev a2a.Event) eventSummary {
	switch v := ev.(type) {
	case *a2a.Task:
		return eventSummary{Kind: "task", State: v.Status.State}
	case *a2a.TaskStatusUpdateEvent:
		s := eventSummary{Kind: "status", State: v.Status.State}
		if v.Status.Message != nil {
			s.Text, s.Data = partsSummary(v.Status.Message.Parts)
		}
		return s
	case *a2a.TaskArtifactUpdateEvent:
		s := eventSummary{Kind: "artifact", Append: v.Append}
		s.Text, s.Data = partsSummary(v.Artifact.Parts)
		return s
	}
	return eventSummary{Kind: fmt.Sprintf("%T", ev)}
}

func partsSummary(parts []*a2a.Part) (string, []any) {
	var texts []string
	var data []any
	for _, p := range parts {
		switch c := p.Content.(type) {
		case a2a.Text:
			texts = append(texts, string(c))
		case a2a.Data:
			data = append(data, c.Value)
		case a2a.Raw:
			data = append(data, fmt.Sprintf("raw %s %q", p.MediaType, c))
		case a2a.URL:
			data = append(data, fmt.Sprintf("url %s", c))
		}
	}
	return strings.Join(texts, ""), data
}

// run executes the task until it pauses or finishes and returns the summary of its events.
func run(t *testing.T, seq func(context.Context) func(func(a2a.Event, error) bool)) []eventSummary {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	defer cancel()
	var got []eventSummary
	for ev, err := range seq(ctx) {
		if err != nil {
			t.Fatalf("Executor.Execute() error = %v", err)
		}
		got = append(got, summarize(ev))
	}
	if ctx.Err() != nil {
		t.Fatalf("Executor.Execute() timed out, got events %+v", got)
	}
	return got
}

func execute(e *Executor, execCtx *a2asrv.ExecutorContext) func(context.Context) func(func(a2a.Event, error) bool) {
	return func(ctx context.Context) func(func(a2a.Event, error) bool) {
		return e.Execute(ctx, execCtx)
	}
}

func cancelTask(e *Executor, execCtx *a2asrv.ExecutorContext) func(context.Context) func(func(a2a.Event, error) bool) {
	return func(ctx context.Context) func(func(a2a.Event, error) bool) {
		return e.Cancel(ctx, execCtx)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(testTimeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met before timeout")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
