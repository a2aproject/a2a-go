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
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/google/go-cmp/cmp"
)

var (
	submitted = eventSummary{Kind: "task", State: a2a.TaskStateSubmitted}
	working   = eventSummary{Kind: "status", State: a2a.TaskStateWorking}
	completed = eventSummary{Kind: "status", State: a2a.TaskStateCompleted}
	canceled  = eventSummary{Kind: "status", State: a2a.TaskStateCanceled}
)

var rmToolCall = toolCall{ToolCallID: "t1", Title: "bash", Kind: "execute", RawInput: map[string]any{"command": "rm x.txt"}}

func rmInputRequired(preamble string) eventSummary {
	return eventSummary{
		Kind:  "status",
		State: a2a.TaskStateInputRequired,
		Text:  preamble + "Permission required: bash (rm x.txt).\nReply with one of: once, always, reject.",
		Data: []any{map[string]any{hitlDataKey: map[string]any{
			"id":     "t1",
			"type":   "permission",
			"title":  "bash",
			"detail": map[string]any{"tool": "execute", "input": map[string]any{"command": "rm x.txt"}},
			"options": []any{
				map[string]any{"id": "once", "label": "Allow"},
				map[string]any{"id": "always", "label": "Always Allow"},
				map[string]any{"id": "reject", "label": "Reject"},
			},
		}}},
	}
}

func toolProgress(text, status string) eventSummary {
	return eventSummary{
		Kind:  "status",
		State: a2a.TaskStateWorking,
		Text:  text,
		Data:  []any{map[string]any{"toolCall": map[string]any{"id": "t1", "title": "Read file", "kind": "read", "status": status}}},
	}
}

func failedWith(text string) eventSummary {
	return eventSummary{Kind: "status", State: a2a.TaskStateFailed, Text: text}
}

func TestExecutor_StreamsTurn(t *testing.T) {
	t.Parallel()
	h := &fakeHarness{onPrompt: func(s *promptScript) {
		s.text("m1", "Hello")
		s.text("m1", " world")
		s.update(map[string]any{"sessionUpdate": "agent_thought_chunk", "content": map[string]any{"type": "text", "text": "hmm"}})
		s.update(map[string]any{"sessionUpdate": updateToolCall, "toolCallId": "t1", "title": "Read file", "kind": "read", "status": "pending"})
		s.tool("t1", toolStatusInProgress)
		s.tool("t1", toolStatusInProgress)
		s.tool("t1", toolStatusCompleted)
		s.text("m2", "Done")
		s.end(stopEndTurn)
	}}
	e := newTestExecutor(t, h, Config{})

	got := run(t, execute(e, newExecCtx("ctx", a2a.NewTextPart("hi"))))

	want := []eventSummary{
		submitted,
		working,
		{Kind: "artifact", Text: "Hello"},
		{Kind: "artifact", Text: " world", Append: true},
		toolProgress("Running Read file", toolStatusInProgress),
		toolProgress("Finished Read file", toolStatusCompleted),
		{Kind: "artifact", Text: "\n\nDone", Append: true},
		completed,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("Executor.Execute() wrong result (-want +got) diff = %s", diff)
	}
}

func TestExecutor_StopReasons(t *testing.T) {
	t.Parallel()
	tests := []struct {
		reason stopReason
		want   eventSummary
	}{
		{reason: stopEndTurn, want: completed},
		{reason: stopCancelled, want: canceled},
		{reason: stopRefusal, want: failedWith("the agent refused to continue")},
		{reason: stopMaxTokens, want: failedWith("the agent reached its token limit")},
		{reason: stopMaxTurnRequests, want: failedWith("the agent reached its request limit for the turn")},
	}
	for _, tc := range tests {
		t.Run(string(tc.reason), func(t *testing.T) {
			t.Parallel()
			h := &fakeHarness{onPrompt: func(s *promptScript) { s.end(tc.reason) }}
			e := newTestExecutor(t, h, Config{})

			got := run(t, execute(e, newExecCtx("ctx", a2a.NewTextPart("hi"))))

			if diff := cmp.Diff([]eventSummary{submitted, working, tc.want}, got); diff != "" {
				t.Fatalf("Executor.Execute() wrong result (-want +got) diff = %s", diff)
			}
		})
	}
}

func TestExecutor_PermissionAnsweredByCaller(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		answer *a2a.Part
		want   string
	}{
		{name: "option id", answer: a2a.NewTextPart("once"), want: "opt-allow"},
		{name: "yes", answer: a2a.NewTextPart("Yes"), want: "opt-allow"},
		{name: "no", answer: a2a.NewTextPart("no"), want: "opt-reject"},
		{name: "label", answer: a2a.NewTextPart("Always Allow"), want: "opt-always"},
		{name: "hitl data", answer: a2a.NewDataPart(map[string]any{hitlDataKey: map[string]any{"id": "t1", "answer": "always"}}), want: "opt-always"},
		{name: "answer data", answer: a2a.NewDataPart(map[string]any{"answer": "reject"}), want: "opt-reject"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			outcomes := make(chan permissionOutcome, 1)
			h := &fakeHarness{onPrompt: func(s *promptScript) {
				outcomes <- s.askPermission(rmToolCall)
				s.end(stopEndTurn)
			}}
			e := newTestExecutor(t, h, Config{})
			execCtx := newExecCtx("ctx", a2a.NewTextPart("delete x.txt"))

			got := run(t, execute(e, execCtx))
			if diff := cmp.Diff([]eventSummary{submitted, working, rmInputRequired("")}, got); diff != "" {
				t.Fatalf("Executor.Execute() wrong result (-want +got) diff = %s", diff)
			}

			got = run(t, execute(e, followUp(execCtx, tc.answer)))
			if diff := cmp.Diff([]eventSummary{working, completed}, got); diff != "" {
				t.Fatalf("Executor.Execute() answer wrong result (-want +got) diff = %s", diff)
			}
			if diff := cmp.Diff(permissionOutcome{Outcome: "selected", OptionID: tc.want}, <-outcomes); diff != "" {
				t.Fatalf("permission outcome wrong result (-want +got) diff = %s", diff)
			}
		})
	}
}

func TestExecutor_PermissionUsesReportedToolInput(t *testing.T) {
	t.Parallel()
	h := &fakeHarness{onPrompt: func(s *promptScript) {
		s.update(map[string]any{
			"sessionUpdate": updateToolCall, "toolCallId": "t1", "title": "bash", "kind": "execute",
			"status": "pending", "rawInput": map[string]any{"command": "rm x.txt"},
		})
		s.askPermission(toolCall{ToolCallID: "t1", Title: "bash", Kind: "execute", RawInput: map[string]any{}})
		s.end(stopEndTurn)
	}}
	e := newTestExecutor(t, h, Config{})
	execCtx := newExecCtx("ctx", a2a.NewTextPart("delete x.txt"))

	got := run(t, execute(e, execCtx))

	if diff := cmp.Diff([]eventSummary{submitted, working, rmInputRequired("")}, got); diff != "" {
		t.Fatalf("Executor.Execute() wrong result (-want +got) diff = %s", diff)
	}
	run(t, execute(e, followUp(execCtx, a2a.NewTextPart("reject"))))
}

func TestExecutor_PermissionReaskedOnUnclearReply(t *testing.T) {
	t.Parallel()
	outcomes := make(chan permissionOutcome, 1)
	h := &fakeHarness{onPrompt: func(s *promptScript) {
		outcomes <- s.askPermission(rmToolCall)
		s.end(stopEndTurn)
	}}
	e := newTestExecutor(t, h, Config{})
	execCtx := newExecCtx("ctx", a2a.NewTextPart("delete x.txt"))
	run(t, execute(e, execCtx))

	got := run(t, execute(e, followUp(execCtx, a2a.NewTextPart("maybe later"))))
	if diff := cmp.Diff([]eventSummary{rmInputRequired("I didn't understand the reply. ")}, got); diff != "" {
		t.Fatalf("Executor.Execute() wrong result (-want +got) diff = %s", diff)
	}

	got = run(t, execute(e, followUp(execCtx, a2a.NewTextPart("yes"))))
	if diff := cmp.Diff([]eventSummary{working, completed}, got); diff != "" {
		t.Fatalf("Executor.Execute() wrong result (-want +got) diff = %s", diff)
	}
	if got, want := <-outcomes, "opt-allow"; got.OptionID != want {
		t.Fatalf("permission outcome = %v, want %v", got.OptionID, want)
	}
}

func TestExecutor_PermissionAnsweredByToolRules(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		cfg  Config
		want string
	}{
		{name: "allow", cfg: Config{AllowTools: []string{"exec*"}}, want: "opt-allow"},
		{name: "deny wins", cfg: Config{AllowTools: []string{"*"}, DenyTools: []string{"execute"}}, want: "opt-reject"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			outcomes := make(chan permissionOutcome, 1)
			h := &fakeHarness{onPrompt: func(s *promptScript) {
				outcomes <- s.askPermission(rmToolCall)
				s.end(stopEndTurn)
			}}
			e := newTestExecutor(t, h, tc.cfg)

			got := run(t, execute(e, newExecCtx("ctx", a2a.NewTextPart("delete x.txt"))))

			if diff := cmp.Diff([]eventSummary{submitted, working, completed}, got); diff != "" {
				t.Fatalf("Executor.Execute() wrong result (-want +got) diff = %s", diff)
			}
			if got := <-outcomes; got.OptionID != tc.want {
				t.Fatalf("permission outcome = %v, want %v", got.OptionID, tc.want)
			}
		})
	}
}

func TestExecutor_CancelWhileInputRequired(t *testing.T) {
	t.Parallel()
	outcomes := make(chan permissionOutcome, 1)
	h := &fakeHarness{onPrompt: func(s *promptScript) {
		if s.promptText() == "next" {
			s.end(stopEndTurn)
			return
		}
		outcomes <- s.askPermission(rmToolCall)
		s.end(stopCancelled)
	}}
	e := newTestExecutor(t, h, Config{})
	execCtx := newExecCtx("ctx", a2a.NewTextPart("delete x.txt"))
	run(t, execute(e, execCtx))

	got := run(t, cancelTask(e, execCtx))

	if diff := cmp.Diff([]eventSummary{canceled}, got); diff != "" {
		t.Fatalf("Executor.Cancel() wrong result (-want +got) diff = %s", diff)
	}
	if diff := cmp.Diff(permissionOutcome{Outcome: "cancelled"}, <-outcomes); diff != "" {
		t.Fatalf("permission outcome wrong result (-want +got) diff = %s", diff)
	}
	_, _, gotCanceled, _ := h.agent(0).snapshot()
	if diff := cmp.Diff([]string{"sess-1"}, gotCanceled); diff != "" {
		t.Fatalf("session/cancel wrong sessions (-want +got) diff = %s", diff)
	}
	got = run(t, execute(e, newExecCtx("ctx", a2a.NewTextPart("next"))))
	if diff := cmp.Diff([]eventSummary{submitted, working, completed}, got); diff != "" {
		t.Fatalf("Executor.Execute() after cancel wrong result (-want +got) diff = %s", diff)
	}
}

func TestExecutor_CancelWhileWorking(t *testing.T) {
	t.Parallel()
	h := &fakeHarness{}
	h.onPrompt = func(s *promptScript) {
		s.text("m1", "Working on it")
		waitFor(t, func() bool {
			_, _, canceled, _ := s.agent.snapshot()
			return len(canceled) > 0
		})
		s.end(stopCancelled)
	}
	e := newTestExecutor(t, h, Config{})
	execCtx := newExecCtx("ctx", a2a.NewTextPart("hi"))

	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	defer cancel()
	var got []eventSummary
	for ev, err := range e.Execute(ctx, execCtx) {
		if err != nil {
			t.Fatalf("Executor.Execute() error = %v", err)
		}
		got = append(got, summarize(ev))
		if _, ok := ev.(*a2a.TaskArtifactUpdateEvent); ok {
			run(t, cancelTask(e, execCtx))
		}
	}

	want := []eventSummary{submitted, working, {Kind: "artifact", Text: "Working on it"}, canceled}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("Executor.Execute() wrong result (-want +got) diff = %s", diff)
	}
}

func TestExecutor_ContextsMapToSessions(t *testing.T) {
	t.Parallel()
	h := &fakeHarness{onPrompt: func(s *promptScript) { s.end(stopEndTurn) }}
	e := newTestExecutor(t, h, Config{})

	run(t, execute(e, newExecCtx("ctx-a", a2a.NewTextPart("one"))))
	run(t, execute(e, newExecCtx("ctx-a", a2a.NewTextPart("two"))))
	run(t, execute(e, newExecCtx("ctx-b", a2a.NewTextPart("three"))))

	created, _, _, prompts := h.agent(0).snapshot()
	if diff := cmp.Diff([]string{"sess-1", "sess-2"}, created); diff != "" {
		t.Fatalf("session/new wrong sessions (-want +got) diff = %s", diff)
	}
	var gotSessions []string
	for _, p := range prompts {
		gotSessions = append(gotSessions, p.SessionID)
	}
	if diff := cmp.Diff([]string{"sess-1", "sess-1", "sess-2"}, gotSessions); diff != "" {
		t.Fatalf("session/prompt wrong sessions (-want +got) diff = %s", diff)
	}
}

func TestExecutor_QueuesTurnsInContext(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	h := &fakeHarness{onPrompt: func(s *promptScript) {
		if s.promptText() == "first" {
			<-release
		}
		s.end(stopEndTurn)
	}}
	e := newTestExecutor(t, h, Config{})

	results := make(chan []eventSummary, 2)
	go func() { results <- run(t, execute(e, newExecCtx("ctx", a2a.NewTextPart("first")))) }()
	waitFor(t, func() bool { _, _, _, p := h.agent(0).snapshot(); return len(p) == 1 })
	go func() { results <- run(t, execute(e, newExecCtx("ctx", a2a.NewTextPart("second")))) }()

	time.Sleep(50 * time.Millisecond)
	if _, _, _, p := h.agent(0).snapshot(); len(p) != 1 {
		t.Fatalf("session/prompt count while first turn runs = %d, want 1", len(p))
	}
	close(release)
	for range 2 {
		if diff := cmp.Diff([]eventSummary{submitted, working, completed}, <-results); diff != "" {
			t.Fatalf("Executor.Execute() wrong result (-want +got) diff = %s", diff)
		}
	}
	_, _, _, prompts := h.agent(0).snapshot()
	var gotText []string
	for _, p := range prompts {
		gotText = append(gotText, p.Prompt[0].Text)
	}
	if diff := cmp.Diff([]string{"first", "second"}, gotText); diff != "" {
		t.Fatalf("session/prompt wrong order (-want +got) diff = %s", diff)
	}
}

func agentExited(e *Executor) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	select {
	case <-e.agent.conn.Done():
		return true
	default:
		return false
	}
}

func TestExecutor_RestartsAgent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		loadSession bool
		wantCreated []string
		wantLoaded  []string
	}{
		{name: "load session", loadSession: true, wantLoaded: []string{"sess-1"}},
		{name: "new session", loadSession: false, wantCreated: []string{"sess-1"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := &fakeHarness{caps: agentCapabilities{LoadSession: tc.loadSession}, onPrompt: func(s *promptScript) { s.end(stopEndTurn) }}
			e := newTestExecutor(t, h, Config{})
			run(t, execute(e, newExecCtx("ctx", a2a.NewTextPart("one"))))
			h.agent(0).crash()
			waitFor(t, func() bool { return agentExited(e) })

			got := run(t, execute(e, newExecCtx("ctx", a2a.NewTextPart("two"))))

			if diff := cmp.Diff([]eventSummary{submitted, working, completed}, got); diff != "" {
				t.Fatalf("Executor.Execute() wrong result (-want +got) diff = %s", diff)
			}
			if got := h.agentCount(); got != 2 {
				t.Fatalf("started agents = %d, want 2", got)
			}
			created, loaded, _, _ := h.agent(1).snapshot()
			if diff := cmp.Diff(tc.wantCreated, created); diff != "" {
				t.Fatalf("session/new wrong sessions (-want +got) diff = %s", diff)
			}
			if diff := cmp.Diff(tc.wantLoaded, loaded); diff != "" {
				t.Fatalf("session/load wrong sessions (-want +got) diff = %s", diff)
			}
		})
	}
}

func TestExecutor_AgentExitFailsTurn(t *testing.T) {
	t.Parallel()
	h := &fakeHarness{onPrompt: func(s *promptScript) {
		s.text("m1", "Hi")
		s.agent.crash()
	}}
	e := newTestExecutor(t, h, Config{})

	got := run(t, execute(e, newExecCtx("ctx", a2a.NewTextPart("hi"))))

	want := []eventSummary{submitted, working, {Kind: "artifact", Text: "Hi"}, failedWith("acp agent error: acp agent exited")}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("Executor.Execute() wrong result (-want +got) diff = %s", diff)
	}
}

func TestExecutor_ConvertsPrompt(t *testing.T) {
	t.Parallel()
	pdf := a2a.NewRawPart([]byte("%PDF"))
	pdf.MediaType = "application/pdf"
	pdf.Filename = "doc.pdf"
	png := a2a.NewRawPart([]byte("png"))
	png.MediaType = "image/png"
	link := a2a.NewFileURLPart("https://example.com/a.txt", "text/plain")

	tests := []struct {
		name       string
		caps       promptCapabilities
		parts      []*a2a.Part
		wantPrompt []contentBlock
		wantEvents []eventSummary
	}{
		{
			name:       "text and data",
			parts:      []*a2a.Part{a2a.NewTextPart("hi"), a2a.NewDataPart(map[string]any{"a": 1})},
			wantPrompt: []contentBlock{{Type: "text", Text: "hi"}, {Type: "text", Text: `{"a":1}`}},
			wantEvents: []eventSummary{submitted, working, completed},
		},
		{
			name:       "image",
			caps:       promptCapabilities{Image: true},
			parts:      []*a2a.Part{png},
			wantPrompt: []contentBlock{{Type: "image", Data: "cG5n", MimeType: "image/png"}},
			wantEvents: []eventSummary{submitted, working, completed},
		},
		{
			name:       "url",
			parts:      []*a2a.Part{link},
			wantPrompt: []contentBlock{{Type: "resource_link", URI: "https://example.com/a.txt", Name: "https://example.com/a.txt", MimeType: "text/plain"}},
			wantEvents: []eventSummary{submitted, working, completed},
		},
		{
			name:  "embedded file",
			caps:  promptCapabilities{EmbeddedContext: true},
			parts: []*a2a.Part{pdf},
			wantPrompt: []contentBlock{{Type: "resource", Resource: &embeddedResource{
				URI: "attachment:doc.pdf", MimeType: "application/pdf", Blob: "JVBERg==",
			}}},
			wantEvents: []eventSummary{submitted, working, completed},
		},
		{
			name:       "unsupported file",
			parts:      []*a2a.Part{pdf},
			wantEvents: []eventSummary{submitted, failedWith("the agent does not accept application/pdf attachments")},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := &fakeHarness{caps: agentCapabilities{PromptCapabilities: tc.caps}, onPrompt: func(s *promptScript) { s.end(stopEndTurn) }}
			e := newTestExecutor(t, h, Config{})

			got := run(t, execute(e, newExecCtx("ctx", tc.parts...)))

			if diff := cmp.Diff(tc.wantEvents, got); diff != "" {
				t.Fatalf("Executor.Execute() wrong result (-want +got) diff = %s", diff)
			}
			_, _, _, prompts := h.agent(0).snapshot()
			var gotPrompt []contentBlock
			if len(prompts) > 0 {
				gotPrompt = prompts[0].Prompt
			}
			if diff := cmp.Diff(tc.wantPrompt, gotPrompt); diff != "" {
				t.Fatalf("session/prompt wrong content (-want +got) diff = %s", diff)
			}
		})
	}
}

func TestExecutor_RejectsUnsupportedAgentRequests(t *testing.T) {
	t.Parallel()
	errs := make(chan error, 1)
	h := &fakeHarness{onPrompt: func(s *promptScript) {
		errs <- s.agent.conn.call(context.Background(), "fs/read_text_file", map[string]any{"sessionId": s.req.SessionID, "path": "/etc/passwd"}, nil)
		s.end(stopEndTurn)
	}}
	e := newTestExecutor(t, h, Config{})

	run(t, execute(e, newExecCtx("ctx", a2a.NewTextPart("hi"))))

	var rpcErr *rpcError
	if err := <-errs; !errors.As(err, &rpcErr) || rpcErr.Code != codeMethodNotFound {
		t.Fatalf("fs/read_text_file error = %v, want code %d", err, codeMethodNotFound)
	}
}

func TestNewExecutor_InvalidCommand(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		cfg  Config
		want string
	}{
		{name: "no command", cfg: Config{}, want: "one of Config.Command or Config.Profile is required"},
		{name: "unknown profile", cfg: Config{Profile: "unknown"}, want: `unknown profile "unknown"`},
		{name: "agent exits", cfg: Config{Command: []string{"true"}}, want: "initializing acp agent: acp agent exited"},
		{name: "agent prints non-json", cfg: Config{Command: []string{"echo", "hello"}}, want: "initializing acp agent: invalid character"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.cfg.Dir = t.TempDir()
			e, err := NewExecutor(t.Context(), tc.cfg)
			if err == nil {
				if err := e.Close(); err != nil {
					t.Errorf("Executor.Close() error = %v", err)
				}
				t.Fatalf("NewExecutor() error = nil, want %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("NewExecutor() error = %v, want %q", err, tc.want)
			}
		})
	}
}

var profileSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"name":  map[string]any{"type": "string", "description": "Your name"},
		"count": map[string]any{"type": "integer"},
		"color": map[string]any{"type": "string", "enum": []any{"red", "blue"}},
	},
	"required": []any{"name"},
}

var nameSchema = map[string]any{
	"type":       "object",
	"properties": map[string]any{"name": map[string]any{"type": "string"}},
}

func formInputRequired(preamble, text string, schema map[string]any) eventSummary {
	return eventSummary{
		Kind:  "status",
		State: a2a.TaskStateInputRequired,
		Text:  preamble + text,
		Data: []any{map[string]any{hitlDataKey: map[string]any{
			"id":    "elicitation-1",
			"type":  "form",
			"title": "Tell me about you",
			"options": []any{
				map[string]any{"id": "decline", "label": "Decline"},
				map[string]any{"id": "cancel", "label": "Cancel"},
			},
			"schema": schema,
		}}},
	}
}

const profileFormText = "Tell me about you\n- color (string) One of: red, blue.\n- count (integer)\n- name (string, required): Your name\n" +
	"Reply with a JSON object of the fields, or with decline or cancel."

func TestExecutor_FormElicitation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		schema map[string]any
		answer *a2a.Part
		want   createElicitationResponse
	}{
		{
			name:   "single field text",
			schema: nameSchema,
			answer: a2a.NewTextPart("Anna"),
			want:   createElicitationResponse{Action: elicitationAccept, Content: map[string]any{"name": "Anna"}},
		},
		{
			name:   "json text",
			schema: profileSchema,
			answer: a2a.NewTextPart(`{"name": "Anna", "count": "3", "color": "red"}`),
			want:   createElicitationResponse{Action: elicitationAccept, Content: map[string]any{"name": "Anna", "count": 3.0, "color": "red"}},
		},
		{
			name:   "hitl data",
			schema: profileSchema,
			answer: a2a.NewDataPart(map[string]any{hitlDataKey: map[string]any{"id": "elicitation-1", "answer": map[string]any{"name": "Bo"}}}),
			want:   createElicitationResponse{Action: elicitationAccept, Content: map[string]any{"name": "Bo"}},
		},
		{
			name:   "decline",
			schema: profileSchema,
			answer: a2a.NewTextPart("Decline"),
			want:   createElicitationResponse{Action: elicitationDecline},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			responses := make(chan createElicitationResponse, 1)
			h := &fakeHarness{onPrompt: func(s *promptScript) {
				responses <- s.elicit("Tell me about you", tc.schema)
				s.end(stopEndTurn)
			}}
			e := newTestExecutor(t, h, Config{})
			execCtx := newExecCtx("ctx", a2a.NewTextPart("hi"))

			got := run(t, execute(e, execCtx))
			if len(got) != 3 || got[2].State != a2a.TaskStateInputRequired {
				t.Fatalf("Executor.Execute() = %+v, want input-required", got)
			}

			got = run(t, execute(e, followUp(execCtx, tc.answer)))
			if diff := cmp.Diff([]eventSummary{working, completed}, got); diff != "" {
				t.Fatalf("Executor.Execute() answer wrong result (-want +got) diff = %s", diff)
			}
			if diff := cmp.Diff(tc.want, <-responses); diff != "" {
				t.Fatalf("elicitation response wrong result (-want +got) diff = %s", diff)
			}
		})
	}
}

func TestExecutor_FormElicitationReaskedOnInvalidReply(t *testing.T) {
	t.Parallel()
	responses := make(chan createElicitationResponse, 1)
	h := &fakeHarness{onPrompt: func(s *promptScript) {
		responses <- s.elicit("Tell me about you", profileSchema)
		s.end(stopEndTurn)
	}}
	e := newTestExecutor(t, h, Config{})
	execCtx := newExecCtx("ctx", a2a.NewTextPart("hi"))

	got := run(t, execute(e, execCtx))
	if diff := cmp.Diff([]eventSummary{submitted, working, formInputRequired("", profileFormText, profileSchema)}, got); diff != "" {
		t.Fatalf("Executor.Execute() wrong result (-want +got) diff = %s", diff)
	}

	got = run(t, execute(e, followUp(execCtx, a2a.NewTextPart(`{"name": "Anna", "color": "green"}`))))
	want := formInputRequired(`I couldn't use the reply: "green" is not a valid choice for color. `, profileFormText, profileSchema)
	if diff := cmp.Diff([]eventSummary{want}, got); diff != "" {
		t.Fatalf("Executor.Execute() wrong result (-want +got) diff = %s", diff)
	}

	got = run(t, execute(e, followUp(execCtx, a2a.NewTextPart(`{"color": "blue"}`))))
	want = formInputRequired("I couldn't use the reply: the field name is required. ", profileFormText, profileSchema)
	if diff := cmp.Diff([]eventSummary{want}, got); diff != "" {
		t.Fatalf("Executor.Execute() wrong result (-want +got) diff = %s", diff)
	}

	run(t, execute(e, followUp(execCtx, a2a.NewTextPart(`{"name": "Anna"}`))))
	if diff := cmp.Diff(createElicitationResponse{Action: elicitationAccept, Content: map[string]any{"name": "Anna"}}, <-responses); diff != "" {
		t.Fatalf("elicitation response wrong result (-want +got) diff = %s", diff)
	}
}

func TestExecutor_QuestionTimeout(t *testing.T) {
	t.Parallel()
	outcomes := make(chan permissionOutcome, 1)
	h := &fakeHarness{onPrompt: func(s *promptScript) {
		outcomes <- s.askPermission(rmToolCall)
		s.end(stopCancelled)
	}}
	e := newTestExecutor(t, h, Config{QuestionTimeout: 10 * time.Millisecond})
	execCtx := newExecCtx("ctx", a2a.NewTextPart("delete x.txt"))
	run(t, execute(e, execCtx))

	if diff := cmp.Diff(permissionOutcome{Outcome: "selected", OptionID: "opt-reject"}, <-outcomes); diff != "" {
		t.Fatalf("permission outcome wrong result (-want +got) diff = %s", diff)
	}
	waitFor(t, func() bool { _, _, canceled, _ := h.agent(0).snapshot(); return len(canceled) == 1 })

	got := run(t, execute(e, followUp(execCtx, a2a.NewTextPart("once"))))
	if diff := cmp.Diff([]eventSummary{failedWith(errQuestionGone)}, got); diff != "" {
		t.Fatalf("Executor.Execute() wrong result (-want +got) diff = %s", diff)
	}
}

func TestExecutor_AgentExitWhileInputRequired(t *testing.T) {
	t.Parallel()
	h := &fakeHarness{onPrompt: func(s *promptScript) {
		s.askPermission(rmToolCall)
	}}
	e := newTestExecutor(t, h, Config{})
	execCtx := newExecCtx("ctx", a2a.NewTextPart("delete x.txt"))
	run(t, execute(e, execCtx))
	h.agent(0).crash()
	waitFor(t, func() bool { return agentExited(e) })

	got := run(t, execute(e, followUp(execCtx, a2a.NewTextPart("once"))))

	if diff := cmp.Diff([]eventSummary{failedWith(errQuestionGone)}, got); diff != "" {
		t.Fatalf("Executor.Execute() wrong result (-want +got) diff = %s", diff)
	}
	if got := h.agentCount(); got != 1 {
		t.Fatalf("started agents = %d, want 1", got)
	}
}

func withConfig(execCtx *a2asrv.ExecutorContext, settings map[string]any) *a2asrv.ExecutorContext {
	execCtx.Message.Metadata = map[string]any{configMetadataKey: settings}
	return execCtx
}

func TestExecutor_SessionConfig(t *testing.T) {
	t.Parallel()
	configOptions := sessionState{ConfigOptions: []sessionConfigOption{
		{ID: "model", Name: "Model", Type: "select"},
		{ID: "fast", Name: "Fast", Type: "boolean"},
	}}
	tests := []struct {
		name         string
		state        sessionState
		cfg          map[string]any
		messages     []map[string]any
		wantSettings []string
		wantLast     eventSummary
	}{
		{
			name:     "config options",
			state:    configOptions,
			cfg:      map[string]any{"model": "m1"},
			messages: []map[string]any{{"fast": true}, nil},
			wantSettings: []string{
				"session/set_config_option fast=true boolean",
				"session/set_config_option model=m1 ",
			},
			wantLast: completed,
		},
		{
			name:     "message overrides config",
			state:    configOptions,
			cfg:      map[string]any{"model": "m1"},
			messages: []map[string]any{{"model": "m2"}},
			wantSettings: []string{
				"session/set_config_option model=m2 ",
			},
			wantLast: completed,
		},
		{
			name:         "modes and models",
			state:        sessionState{Modes: &struct{}{}, Models: &struct{}{}},
			messages:     []map[string]any{{"mode": "plan", "model": "m1"}},
			wantSettings: []string{"session/set_mode plan", "session/set_model m1"},
			wantLast:     completed,
		},
		{
			name:     "unknown setting",
			state:    configOptions,
			messages: []map[string]any{{"temperature": 1}},
			wantLast: failedWith(`configuring acp session: the agent has no session setting "temperature"`),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := &fakeHarness{state: tc.state, onPrompt: func(s *promptScript) { s.end(stopEndTurn) }}
			e := newTestExecutor(t, h, Config{SessionConfig: tc.cfg})

			var got []eventSummary
			for _, settings := range tc.messages {
				got = run(t, execute(e, withConfig(newExecCtx("ctx", a2a.NewTextPart("hi")), settings)))
			}

			if diff := cmp.Diff(tc.wantLast, got[len(got)-1]); diff != "" {
				t.Fatalf("Executor.Execute() wrong last event (-want +got) diff = %s", diff)
			}
			if diff := cmp.Diff(tc.wantSettings, h.agent(0).recordedSettings()); diff != "" {
				t.Fatalf("session settings wrong result (-want +got) diff = %s", diff)
			}
		})
	}
}

type staticMCPServers []json.RawMessage

func (s staticMCPServers) MCPServers(context.Context, *a2asrv.ExecutorContext) ([]json.RawMessage, error) {
	return s, nil
}

func TestExecutor_MCPServers(t *testing.T) {
	t.Parallel()
	servers := staticMCPServers{json.RawMessage(`{"type":"http","name":"api","url":"https://example.com/mcp","headers":[]}`)}
	h := &fakeHarness{onPrompt: func(s *promptScript) { s.end(stopEndTurn) }}
	e := newTestExecutor(t, h, Config{MCPServers: servers})

	run(t, execute(e, newExecCtx("ctx", a2a.NewTextPart("hi"))))

	h.agent(0).mu.Lock()
	got := h.agent(0).mcpServers
	h.agent(0).mu.Unlock()
	if diff := cmp.Diff([]json.RawMessage(servers), got); diff != "" {
		t.Fatalf("session/new mcpServers wrong result (-want +got) diff = %s", diff)
	}
}
