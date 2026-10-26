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
	"fmt"
	"maps"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aext/structuredinput"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/internal/acp"
	"github.com/google/go-cmp/cmp"
)

var (
	submitted = eventSummary{Kind: "task", State: a2a.TaskStateSubmitted}
	working   = eventSummary{Kind: "status", State: a2a.TaskStateWorking}
	completed = eventSummary{Kind: "status", State: a2a.TaskStateCompleted}
	canceled  = eventSummary{Kind: "status", State: a2a.TaskStateCanceled}
)

var rmToolCall = acp.ToolCall{ToolCallID: "t1", Title: "bash", Kind: "execute", RawInput: map[string]any{"command": "rm x.txt"}}

func rmInputRequired(preamble string) eventSummary {
	return eventSummary{
		Kind:  "status",
		State: a2a.TaskStateInputRequired,
		Text:  preamble + "Permission required: bash (rm x.txt).\nReply with one of: once, always, reject.",
		Data: []any{
			map[string]any{"toolCall": map[string]any{"id": "t1", "title": "bash", "kind": "execute", "input": map[string]any{"command": "rm x.txt"}}},
			map[string]any{
				"type":  "object",
				"title": "bash",
				"properties": map[string]any{"option": optionSchema(
					map[string]any{"const": "once", "title": "Allow"},
					map[string]any{"const": "always", "title": "Always Allow"},
					map[string]any{"const": "reject", "title": "Reject"},
				)},
				"required": []any{"option"},
			},
		},
	}
}

func validationError(msg string) map[string]any {
	return map[string]any{
		"valid":  false,
		"errors": []any{map[string]any{"error": "validating root: " + msg, "instanceLocation": "", "keywordLocation": ""}},
	}
}

func textAnswer(text string) func(*testing.T) *a2a.Part {
	return func(*testing.T) *a2a.Part { return a2a.NewTextPart(text) }
}

func optionSchema(options ...any) map[string]any {
	return map[string]any{"type": "string", "oneOf": options}
}

func responsePart(t *testing.T, id string, data any) *a2a.Part {
	t.Helper()
	part, err := structuredinput.NewResponsePart(id, data)
	if err != nil {
		t.Fatalf("structuredinput.NewResponsePart() error = %v", err)
	}
	return part
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
		s.update(map[string]any{"sessionUpdate": acp.UpdateToolCall, "toolCallId": "t1", "title": "Read file", "kind": "read", "status": "pending"})
		s.tool("t1", acp.ToolStatusInProgress)
		s.tool("t1", acp.ToolStatusInProgress)
		s.tool("t1", acp.ToolStatusCompleted)
		s.text("m2", "Done")
		s.end(acp.StopEndTurn)
	}}
	e := newTestExecutor(t, h, Config{})

	got := run(t, execute(e, newExecCtx("ctx", a2a.NewTextPart("hi"))))

	want := []eventSummary{
		submitted,
		working,
		{Kind: "artifact", Text: "Hello"},
		{Kind: "artifact", Text: " world", Append: true},
		toolProgress("Running Read file", acp.ToolStatusInProgress),
		toolProgress("Finished Read file", acp.ToolStatusCompleted),
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
		reason acp.StopReason
		want   eventSummary
	}{
		{reason: acp.StopEndTurn, want: completed},
		{reason: acp.StopCancelled, want: canceled},
		{reason: acp.StopRefusal, want: failedWith("the agent refused to continue")},
		{reason: acp.StopMaxTokens, want: failedWith("the agent reached its token limit")},
		{reason: acp.StopMaxTurnRequests, want: failedWith("the agent reached its request limit for the turn")},
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
		answer func(t *testing.T) *a2a.Part
		want   string
	}{
		{name: "option id", answer: textAnswer("once"), want: "opt-allow"},
		{name: "yes", answer: textAnswer("Yes"), want: "opt-allow"},
		{name: "no", answer: textAnswer("no"), want: "opt-reject"},
		{name: "label", answer: textAnswer("Always Allow"), want: "opt-always"},
		{
			name:   "structured input",
			answer: func(t *testing.T) *a2a.Part { return responsePart(t, "t1", Answer{Option: OptionAlways}) },
			want:   "opt-always",
		},
		{
			name:   "untagged data",
			answer: func(*testing.T) *a2a.Part { return a2a.NewDataPart(map[string]any{"option": "reject"}) },
			want:   "opt-reject",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			outcomes := make(chan acp.PermissionOutcome, 1)
			h := &fakeHarness{onPrompt: func(s *promptScript) {
				outcomes <- s.askPermission(rmToolCall)
				s.end(acp.StopEndTurn)
			}}
			e := newTestExecutor(t, h, Config{})
			execCtx := newExecCtx("ctx", a2a.NewTextPart("delete x.txt"))

			got := run(t, execute(e, execCtx))
			if diff := cmp.Diff([]eventSummary{submitted, working, rmInputRequired("")}, got); diff != "" {
				t.Fatalf("Executor.Execute() wrong result (-want +got) diff = %s", diff)
			}

			got = run(t, execute(e, followUp(execCtx, tc.answer(t))))
			if diff := cmp.Diff([]eventSummary{working, completed}, got); diff != "" {
				t.Fatalf("Executor.Execute() answer wrong result (-want +got) diff = %s", diff)
			}
			if diff := cmp.Diff(acp.PermissionOutcome{Outcome: "selected", OptionID: tc.want}, <-outcomes); diff != "" {
				t.Fatalf("permission outcome wrong result (-want +got) diff = %s", diff)
			}
		})
	}
}

func TestExecutor_PermissionUsesReportedToolInput(t *testing.T) {
	t.Parallel()
	h := &fakeHarness{onPrompt: func(s *promptScript) {
		s.update(map[string]any{
			"sessionUpdate": acp.UpdateToolCall, "toolCallId": "t1", "title": "bash", "kind": "execute",
			"status": "pending", "rawInput": map[string]any{"command": "rm x.txt"},
		})
		s.askPermission(acp.ToolCall{ToolCallID: "t1", Title: "bash", Kind: "execute", RawInput: map[string]any{}})
		s.end(acp.StopEndTurn)
	}}
	e := newTestExecutor(t, h, Config{})
	execCtx := newExecCtx("ctx", a2a.NewTextPart("delete x.txt"))

	got := run(t, execute(e, execCtx))

	if diff := cmp.Diff([]eventSummary{submitted, working, rmInputRequired("")}, got); diff != "" {
		t.Fatalf("Executor.Execute() wrong result (-want +got) diff = %s", diff)
	}
	run(t, execute(e, followUp(execCtx, a2a.NewTextPart("reject"))))
}

func TestExecutor_PermissionAnsweredByToolRules(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		cfg     Config
		options []acp.PermissionOption
		want    acp.PermissionOutcome
	}{
		{
			name: "allow",
			cfg:  Config{AutoAnswer: ToolRules{Allow: []string{"exec*"}}},
			want: acp.PermissionOutcome{Outcome: acp.OutcomeSelected, OptionID: "opt-allow"},
		},
		{
			name: "deny wins",
			cfg:  Config{AutoAnswer: ToolRules{Allow: []string{"*"}, Deny: []string{"execute"}}},
			want: acp.PermissionOutcome{Outcome: acp.OutcomeSelected, OptionID: "opt-reject"},
		},
		{
			name: "deny with only reject always",
			cfg:  Config{AutoAnswer: ToolRules{Deny: []string{"execute"}}},
			options: []acp.PermissionOption{
				{OptionID: "opt-allow", Name: "Allow", Kind: acp.PermissionAllowOnce},
				{OptionID: "opt-reject-always", Name: "Never", Kind: acp.PermissionRejectAlways},
			},
			want: acp.PermissionOutcome{Outcome: acp.OutcomeSelected, OptionID: "opt-reject-always"},
		},
		{
			name:    "deny without reject options",
			cfg:     Config{AutoAnswer: ToolRules{Deny: []string{"execute"}}},
			options: []acp.PermissionOption{{OptionID: "opt-allow", Name: "Allow", Kind: acp.PermissionAllowOnce}},
			want:    acp.PermissionOutcome{Outcome: acp.OutcomeCancelled},
		},
		{
			name:    "allow with only allow always",
			cfg:     Config{AutoAnswer: ToolRules{Allow: []string{"execute"}}},
			options: []acp.PermissionOption{{OptionID: "opt-always", Name: "Always", Kind: acp.PermissionAllowAlways}},
			want:    acp.PermissionOutcome{Outcome: acp.OutcomeSelected, OptionID: "opt-always"},
		},
		{
			name: "auto answer",
			cfg: Config{AutoAnswer: AutoAnswerFunc(func(_ context.Context, _ *a2asrv.ExecutorContext, q Question) (Answer, bool) {
				return Answer{Option: OptionAlways}, q.Tool.Input.(map[string]any)["command"] == "rm x.txt"
			})},
			want: acp.PermissionOutcome{Outcome: acp.OutcomeSelected, OptionID: "opt-always"},
		},
		{
			name: "auto answer falls back to rules",
			cfg: Config{AutoAnswer: AutoAnswerFunc(func(ctx context.Context, execCtx *a2asrv.ExecutorContext, q Question) (Answer, bool) {
				if q.Tool.Kind == "read" {
					return Answer{Option: OptionOnce}, true
				}
				return ToolRules{Deny: []string{"*"}}.AutoAnswer(ctx, execCtx, q)
			})},
			want: acp.PermissionOutcome{Outcome: acp.OutcomeSelected, OptionID: "opt-reject"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			options := tc.options
			if options == nil {
				options = testPermissionOptions
			}
			outcomes := make(chan acp.PermissionOutcome, 1)
			h := &fakeHarness{onPrompt: func(s *promptScript) {
				outcomes <- s.askPermissionWith(rmToolCall, options)
				s.end(acp.StopEndTurn)
			}}
			e := newTestExecutor(t, h, tc.cfg)

			got := run(t, execute(e, newExecCtx("ctx", a2a.NewTextPart("delete x.txt"))))

			if diff := cmp.Diff([]eventSummary{submitted, working, completed}, got); diff != "" {
				t.Fatalf("Executor.Execute() wrong result (-want +got) diff = %s", diff)
			}
			if diff := cmp.Diff(tc.want, <-outcomes); diff != "" {
				t.Fatalf("permission outcome wrong result (-want +got) diff = %s", diff)
			}
		})
	}
}

func TestExecutor_CancelWhileInputRequired(t *testing.T) {
	t.Parallel()
	outcomes := make(chan acp.PermissionOutcome, 1)
	h := &fakeHarness{onPrompt: func(s *promptScript) {
		if s.promptText() == "next" {
			s.end(acp.StopEndTurn)
			return
		}
		outcomes <- s.askPermission(rmToolCall)
		s.end(acp.StopCancelled)
	}}
	e := newTestExecutor(t, h, Config{})
	execCtx := newExecCtx("ctx", a2a.NewTextPart("delete x.txt"))
	run(t, execute(e, execCtx))

	got := run(t, cancelTask(e, execCtx))

	if diff := cmp.Diff([]eventSummary{canceled}, got); diff != "" {
		t.Fatalf("Executor.Cancel() wrong result (-want +got) diff = %s", diff)
	}
	if diff := cmp.Diff(acp.PermissionOutcome{Outcome: "cancelled"}, <-outcomes); diff != "" {
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
		s.end(acp.StopCancelled)
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
	h := &fakeHarness{onPrompt: func(s *promptScript) { s.end(acp.StopEndTurn) }}
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
		s.end(acp.StopEndTurn)
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

func TestExecutor_RejectsTurnInBusyContext(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	h := &fakeHarness{onPrompt: func(s *promptScript) {
		if s.promptText() == "first" {
			<-release
		}
		s.end(acp.StopEndTurn)
	}}
	e := newTestExecutor(t, h, Config{RejectBusyContext: true})

	first := newExecCtx("ctx", a2a.NewTextPart("first"))
	results := make(chan []eventSummary, 1)
	go func() { results <- run(t, execute(e, first)) }()
	waitFor(t, func() bool { _, _, _, p := h.agent(0).snapshot(); return len(p) == 1 })

	got := run(t, execute(e, newExecCtx("ctx", a2a.NewTextPart("second"))))

	want := []eventSummary{submitted, failedWith(fmt.Sprintf("the context is busy with task %s, send the message after it finishes", first.TaskID))}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("Executor.Execute() wrong result (-want +got) diff = %s", diff)
	}
	close(release)
	if diff := cmp.Diff([]eventSummary{submitted, working, completed}, <-results); diff != "" {
		t.Fatalf("Executor.Execute() first turn wrong result (-want +got) diff = %s", diff)
	}
	got = run(t, execute(e, newExecCtx("ctx", a2a.NewTextPart("third"))))
	if diff := cmp.Diff([]eventSummary{submitted, working, completed}, got); diff != "" {
		t.Fatalf("Executor.Execute() after first turn wrong result (-want +got) diff = %s", diff)
	}
}

func TestExecutor_CancelQueuedTurn(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	h := &fakeHarness{onPrompt: func(s *promptScript) {
		if s.promptText() == "first" {
			<-release
		}
		s.end(acp.StopEndTurn)
	}}
	e := newTestExecutor(t, h, Config{})

	firstResult := make(chan []eventSummary, 1)
	go func() { firstResult <- run(t, execute(e, newExecCtx("ctx", a2a.NewTextPart("first")))) }()
	waitFor(t, func() bool { _, _, _, p := h.agent(0).snapshot(); return len(p) == 1 })
	queued := newExecCtx("ctx", a2a.NewTextPart("second"))
	queuedResult := make(chan []eventSummary, 1)
	go func() { queuedResult <- run(t, execute(e, queued)) }()
	waitFor(t, func() bool { return e.isWaiting(queued.TaskID) })

	got := run(t, cancelTask(e, queued))

	if diff := cmp.Diff([]eventSummary{canceled}, got); diff != "" {
		t.Fatalf("Executor.Cancel() wrong result (-want +got) diff = %s", diff)
	}
	if diff := cmp.Diff([]eventSummary{submitted}, <-queuedResult); diff != "" {
		t.Fatalf("Executor.Execute() queued turn wrong result (-want +got) diff = %s", diff)
	}
	close(release)
	<-firstResult
	if _, _, _, p := h.agent(0).snapshot(); len(p) != 1 {
		t.Fatalf("session/prompt count = %d, want 1", len(p))
	}
}

func agentExited(e *Executor) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := e.agents[""]
	return s == nil || s.agent == nil || s.agent.exited()
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
			h := &fakeHarness{caps: acp.AgentCapabilities{LoadSession: tc.loadSession}, onPrompt: func(s *promptScript) { s.end(acp.StopEndTurn) }}
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
		caps       acp.PromptCapabilities
		parts      []*a2a.Part
		wantPrompt []acp.ContentBlock
		wantEvents []eventSummary
	}{
		{
			name:       "text and data",
			parts:      []*a2a.Part{a2a.NewTextPart("hi"), a2a.NewDataPart(map[string]any{"a": 1})},
			wantPrompt: []acp.ContentBlock{{Type: "text", Text: "hi"}, {Type: "text", Text: `{"a":1}`}},
			wantEvents: []eventSummary{submitted, working, completed},
		},
		{
			name:       "image",
			caps:       acp.PromptCapabilities{Image: true},
			parts:      []*a2a.Part{png},
			wantPrompt: []acp.ContentBlock{{Type: "image", Data: "cG5n", MimeType: "image/png"}},
			wantEvents: []eventSummary{submitted, working, completed},
		},
		{
			name:       "url",
			parts:      []*a2a.Part{link},
			wantPrompt: []acp.ContentBlock{{Type: "resource_link", URI: "https://example.com/a.txt", Name: "https://example.com/a.txt", MimeType: "text/plain"}},
			wantEvents: []eventSummary{submitted, working, completed},
		},
		{
			name:  "embedded file",
			caps:  acp.PromptCapabilities{EmbeddedContext: true},
			parts: []*a2a.Part{pdf},
			wantPrompt: []acp.ContentBlock{{Type: "resource", Resource: &acp.EmbeddedResource{
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
			h := &fakeHarness{caps: acp.AgentCapabilities{PromptCapabilities: tc.caps}, onPrompt: func(s *promptScript) { s.end(acp.StopEndTurn) }}
			e := newTestExecutor(t, h, Config{})

			got := run(t, execute(e, newExecCtx("ctx", tc.parts...)))

			if diff := cmp.Diff(tc.wantEvents, got); diff != "" {
				t.Fatalf("Executor.Execute() wrong result (-want +got) diff = %s", diff)
			}
			_, _, _, prompts := h.agent(0).snapshot()
			var gotPrompt []acp.ContentBlock
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
		errs <- s.agent.conn.Call(context.Background(), "fs/read_text_file", map[string]any{"sessionId": s.req.SessionID, "path": "/etc/passwd"}, nil)
		s.end(acp.StopEndTurn)
	}}
	e := newTestExecutor(t, h, Config{})

	run(t, execute(e, newExecCtx("ctx", a2a.NewTextPart("hi"))))

	var rpcErr *acp.Error
	if err := <-errs; !errors.As(err, &rpcErr) || rpcErr.Code != acp.CodeMethodNotFound {
		t.Fatalf("fs/read_text_file error = %v, want code %d", err, acp.CodeMethodNotFound)
	}
}

func TestNewExecutor_InvalidAgent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		agent Agent
		want  string
	}{
		{name: "no agent", agent: nil, want: "agent is required"},
		{name: "no command", agent: Process{}, want: "Process.Argv is empty"},
		{name: "agent exits", agent: Process{Argv: []string{"true"}}, want: "initializing acp agent: acp agent exited"},
		{name: "agent prints non-json", agent: Process{Argv: []string{"echo", "hello"}}, want: "initializing acp agent: invalid character"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if p, ok := tc.agent.(Process); ok {
				p.Dir = t.TempDir()
				tc.agent = p
			}
			e, err := NewExecutor(t.Context(), tc.agent, nil)
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

func formInputRequired(preamble, text string, fields map[string]any, validation ...any) eventSummary {
	return eventSummary{
		Kind:  "status",
		State: a2a.TaskStateInputRequired,
		Text:  preamble + text,
		Data: append([]any{map[string]any{
			"type":  "object",
			"title": "Tell me about you",
			"properties": map[string]any{
				"option": optionSchema(
					map[string]any{"const": "accept", "title": "Accept"},
					map[string]any{"const": "decline", "title": "Decline"},
					map[string]any{"const": "cancel", "title": "Cancel"},
				),
				"content": fields,
			},
			"required": []any{"option"},
			"if":       map[string]any{"properties": map[string]any{"option": map[string]any{"const": "accept"}}},
			"then":     map[string]any{"required": []any{"content"}},
		}}, validation...),
	}
}

const profileFormText = "Tell me about you\n- color (string) One of: red, blue.\n- count (integer)\n- name (string, required): Your name\n" +
	"Reply with a JSON object of the fields, or with decline or cancel."

func TestExecutor_FormElicitation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		schema map[string]any
		answer func(t *testing.T) *a2a.Part
		want   acp.CreateElicitationResponse
	}{
		{
			name:   "json text",
			schema: profileSchema,
			answer: textAnswer(`{"name": "Anna", "count": 3, "color": "red"}`),
			want:   acp.CreateElicitationResponse{Action: acp.ElicitationAccept, Content: map[string]any{"name": "Anna", "count": 3.0, "color": "red"}},
		},
		{
			name:   "structured input",
			schema: profileSchema,
			answer: func(t *testing.T) *a2a.Part {
				return responsePart(t, "elicitation-1", Answer{Option: OptionAccept, Content: map[string]any{"name": "Bo"}})
			},
			want: acp.CreateElicitationResponse{Action: acp.ElicitationAccept, Content: map[string]any{"name": "Bo"}},
		},
		{
			name:   "decline",
			schema: profileSchema,
			answer: textAnswer("Decline"),
			want:   acp.CreateElicitationResponse{Action: acp.ElicitationDecline},
		},
		{
			name:   "structured decline",
			schema: profileSchema,
			answer: func(t *testing.T) *a2a.Part { return responsePart(t, "elicitation-1", Answer{Option: OptionDecline}) },
			want:   acp.CreateElicitationResponse{Action: acp.ElicitationDecline},
		},
		{
			name:   "confirmation",
			schema: map[string]any{"type": "object"},
			answer: textAnswer("accept"),
			want:   acp.CreateElicitationResponse{Action: acp.ElicitationAccept},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			responses := make(chan acp.CreateElicitationResponse, 1)
			h := &fakeHarness{onPrompt: func(s *promptScript) {
				responses <- s.elicit("Tell me about you", tc.schema)
				s.end(acp.StopEndTurn)
			}}
			e := newTestExecutor(t, h, Config{})
			execCtx := newExecCtx("ctx", a2a.NewTextPart("hi"))

			got := run(t, execute(e, execCtx))
			if len(got) != 3 || got[2].State != a2a.TaskStateInputRequired {
				t.Fatalf("Executor.Execute() = %+v, want input-required", got)
			}

			got = run(t, execute(e, followUp(execCtx, tc.answer(t))))
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
	responses := make(chan acp.CreateElicitationResponse, 1)
	h := &fakeHarness{onPrompt: func(s *promptScript) {
		responses <- s.elicit("Tell me about you", profileSchema)
		s.end(acp.StopEndTurn)
	}}
	e := newTestExecutor(t, h, Config{})
	execCtx := newExecCtx("ctx", a2a.NewTextPart("hi"))

	got := run(t, execute(e, execCtx))
	if diff := cmp.Diff([]eventSummary{submitted, working, formInputRequired("", profileFormText, profileSchema)}, got); diff != "" {
		t.Fatalf("Executor.Execute() wrong result (-want +got) diff = %s", diff)
	}

	got = run(t, execute(e, followUp(execCtx, a2a.NewTextPart(`{"name": "Anna", "color": "green"}`))))
	want := formInputRequired("I couldn't use the reply: validating /properties/content: validating /properties/content/properties/color: enum: green does not equal any of: [red blue]. ", profileFormText, profileSchema, validationError("validating /properties/content: validating /properties/content/properties/color: enum: green does not equal any of: [red blue]"))
	if diff := cmp.Diff([]eventSummary{want}, got); diff != "" {
		t.Fatalf("Executor.Execute() wrong result (-want +got) diff = %s", diff)
	}

	got = run(t, execute(e, followUp(execCtx, a2a.NewTextPart(`{"color": "blue"}`))))
	want = formInputRequired("I couldn't use the reply: validating /properties/content: required: missing properties: [\"name\"]. ", profileFormText, profileSchema, validationError("validating /properties/content: required: missing properties: [\"name\"]"))
	if diff := cmp.Diff([]eventSummary{want}, got); diff != "" {
		t.Fatalf("Executor.Execute() wrong result (-want +got) diff = %s", diff)
	}

	run(t, execute(e, followUp(execCtx, a2a.NewTextPart(`{"name": "Anna"}`))))
	if diff := cmp.Diff(acp.CreateElicitationResponse{Action: acp.ElicitationAccept, Content: map[string]any{"name": "Anna"}}, <-responses); diff != "" {
		t.Fatalf("elicitation response wrong result (-want +got) diff = %s", diff)
	}
}

func TestExecutor_QuestionTimeout(t *testing.T) {
	t.Parallel()
	outcomes := make(chan acp.PermissionOutcome, 1)
	scriptDone := make(chan struct{})
	h := &fakeHarness{onPrompt: func(s *promptScript) {
		defer close(scriptDone)
		outcomes <- s.askPermission(rmToolCall)
		s.end(acp.StopCancelled)
	}}
	e := newTestExecutor(t, h, Config{QuestionTimeout: 10 * time.Millisecond})
	execCtx := newExecCtx("ctx", a2a.NewTextPart("delete x.txt"))
	run(t, execute(e, execCtx))

	if diff := cmp.Diff(acp.PermissionOutcome{Outcome: "selected", OptionID: "opt-reject"}, <-outcomes); diff != "" {
		t.Fatalf("permission outcome wrong result (-want +got) diff = %s", diff)
	}
	waitFor(t, func() bool { _, _, canceled, _ := h.agent(0).snapshot(); return len(canceled) == 1 })

	got := run(t, execute(e, followUp(execCtx, a2a.NewTextPart("once"))))
	if diff := cmp.Diff([]eventSummary{failedWith(errQuestionGone)}, got); diff != "" {
		t.Fatalf("Executor.Execute() wrong result (-want +got) diff = %s", diff)
	}
	<-scriptDone
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

func withSettings(execCtx *a2asrv.ExecutorContext, settings map[string]any) *a2asrv.ExecutorContext {
	execCtx.Message.Metadata = map[string]any{SettingsMetadataKey: settings}
	return execCtx
}

func TestExecutor_Settings(t *testing.T) {
	t.Parallel()
	configOptions := acp.SessionState{ConfigOptions: []acp.SessionConfigOption{
		{ID: "model", Name: "Model", Type: "select"},
		{ID: "fast", Name: "Fast", Type: "boolean"},
	}}
	modesAndModels := acp.SessionState{Modes: &struct{}{}, Models: &struct{}{}}
	tests := []struct {
		name         string
		state        acp.SessionState
		defaults     map[string]any
		allowed      []string
		messages     []map[string]any
		wantSettings []string
		wantLast     eventSummary
	}{
		{
			name:     "config options",
			state:    configOptions,
			defaults: map[string]any{"model": "m1"},
			allowed:  []string{"*"},
			messages: []map[string]any{{"fast": true}, nil},
			wantSettings: []string{
				"session/set_config_option fast=true boolean",
				"session/set_config_option model=m1 ",
				"session/set_config_option model=m1 ",
			},
			wantLast: completed,
		},
		{
			name:     "message overrides defaults",
			state:    configOptions,
			defaults: map[string]any{"model": "m1"},
			allowed:  []string{"*"},
			messages: []map[string]any{{"model": "m2"}},
			wantSettings: []string{
				"session/set_config_option model=m2 ",
			},
			wantLast: completed,
		},
		{
			name:         "modes and models",
			state:        modesAndModels,
			allowed:      []string{"*"},
			messages:     []map[string]any{{"mode": "plan", "model": "m1"}},
			wantSettings: []string{"session/set_mode plan", "session/set_model m1"},
			wantLast:     completed,
		},
		{
			name:     "unknown setting",
			state:    configOptions,
			allowed:  []string{"*"},
			messages: []map[string]any{{"temperature": 1}},
			wantLast: failedWith(`configuring acp session: the agent has no session setting "temperature"`),
		},
		{
			name:     "metadata ignored by default",
			state:    modesAndModels,
			messages: []map[string]any{{"mode": "bypassPermissions"}},
			wantLast: completed,
		},
		{
			name:     "setting not allowed",
			state:    modesAndModels,
			allowed:  []string{"model"},
			messages: []map[string]any{{"mode": "bypassPermissions", "model": "m1"}},
			wantLast: failedWith(`the session setting "mode" can't be changed in a message`),
		},
		{
			name:         "setting allowed",
			state:        modesAndModels,
			allowed:      []string{"mod*"},
			messages:     []map[string]any{{"model": "m1"}},
			wantSettings: []string{"session/set_model m1"},
			wantLast:     completed,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := &fakeHarness{state: tc.state, onPrompt: func(s *promptScript) { s.end(acp.StopEndTurn) }}
			var cfg Config
			if tc.allowed != nil {
				fromMetadata := MetadataSettings(tc.allowed...)
				cfg.Settings = func(ctx context.Context, execCtx *a2asrv.ExecutorContext) (map[string]any, error) {
					settings, err := fromMetadata(ctx, execCtx)
					if err != nil {
						return nil, err
					}
					merged := map[string]any{}
					maps.Copy(merged, tc.defaults)
					maps.Copy(merged, settings)
					return merged, nil
				}
			}
			e := newTestExecutor(t, h, cfg)

			var got []eventSummary
			for _, settings := range tc.messages {
				got = run(t, execute(e, withSettings(newExecCtx("ctx", a2a.NewTextPart("hi")), settings)))
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

func TestExecutor_SessionParams(t *testing.T) {
	t.Parallel()
	servers := []json.RawMessage{json.RawMessage(`{"type":"http","name":"api","url":"https://example.com/mcp","headers":[]}`)}
	absDir := t.TempDir()
	tests := []struct {
		name        string
		session     func(context.Context, *a2asrv.ExecutorContext) (SessionParams, error)
		wantCwd     func(dir string) string
		wantServers []json.RawMessage
		wantLast    eventSummary
	}{
		{
			name:        "defaults",
			wantCwd:     func(dir string) string { return dir },
			wantServers: []json.RawMessage{},
			wantLast:    completed,
		},
		{
			name: "relative cwd per context",
			session: func(_ context.Context, execCtx *a2asrv.ExecutorContext) (SessionParams, error) {
				return SessionParams{Cwd: filepath.Join("work", execCtx.ContextID), MCPServers: servers}, nil
			},
			wantCwd:     func(dir string) string { return filepath.Join(dir, "work", "ctx") },
			wantServers: servers,
			wantLast:    completed,
		},
		{
			name: "absolute cwd",
			session: func(context.Context, *a2asrv.ExecutorContext) (SessionParams, error) {
				return SessionParams{Cwd: absDir}, nil
			},
			wantCwd:     func(string) string { return absDir },
			wantServers: []json.RawMessage{},
			wantLast:    completed,
		},
		{
			name: "error",
			session: func(context.Context, *a2asrv.ExecutorContext) (SessionParams, error) {
				return SessionParams{}, errors.New("no workspace")
			},
			wantLast: failedWith("resolving session parameters: no workspace"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := &fakeHarness{onPrompt: func(s *promptScript) { s.end(acp.StopEndTurn) }}
			e := newTestExecutor(t, h, Config{Session: tc.session})

			got := run(t, execute(e, newExecCtx("ctx", a2a.NewTextPart("hi"))))

			if diff := cmp.Diff(tc.wantLast, got[len(got)-1]); diff != "" {
				t.Fatalf("Executor.Execute() wrong last event (-want +got) diff = %s", diff)
			}
			var wantCwds []string
			if tc.wantCwd != nil {
				wantCwds = []string{tc.wantCwd(e.dir)}
			}
			gotCwds, gotServers := h.agent(0).sessionParams()
			if diff := cmp.Diff(wantCwds, gotCwds); diff != "" {
				t.Fatalf("session/new cwd wrong result (-want +got) diff = %s", diff)
			}
			if diff := cmp.Diff(tc.wantServers, gotServers); diff != "" {
				t.Fatalf("session/new mcpServers wrong result (-want +got) diff = %s", diff)
			}
		})
	}
}

func TestExecutor_SessionParamsOnLoad(t *testing.T) {
	t.Parallel()
	h := &fakeHarness{caps: acp.AgentCapabilities{LoadSession: true}, onPrompt: func(s *promptScript) { s.end(acp.StopEndTurn) }}
	e := newTestExecutor(t, h, Config{Session: func(_ context.Context, execCtx *a2asrv.ExecutorContext) (SessionParams, error) {
		return SessionParams{Cwd: execCtx.ContextID}, nil
	}})
	run(t, execute(e, newExecCtx("ctx", a2a.NewTextPart("one"))))
	h.agent(0).crash()
	waitFor(t, func() bool { return agentExited(e) })

	run(t, execute(e, newExecCtx("ctx", a2a.NewTextPart("two"))))

	_, loaded, _, _ := h.agent(1).snapshot()
	gotCwds, _ := h.agent(1).sessionParams()
	if diff := cmp.Diff([]string{"sess-1"}, loaded); diff != "" {
		t.Fatalf("session/load wrong sessions (-want +got) diff = %s", diff)
	}
	if diff := cmp.Diff([]string{filepath.Join(e.dir, "ctx")}, gotCwds); diff != "" {
		t.Fatalf("session/load cwd wrong result (-want +got) diff = %s", diff)
	}
}

func TestExecutor_InvalidReply(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		reply       string
		wantEvents  []eventSummary
		wantOutcome acp.PermissionOutcome
		wantPrompts []string
	}{
		{
			name:        "unrecognized reply interrupts",
			reply:       "rename it instead",
			wantEvents:  []eventSummary{working, completed},
			wantOutcome: acp.PermissionOutcome{Outcome: acp.OutcomeSelected, OptionID: "opt-reject"},
			wantPrompts: []string{"delete x.txt", "rename it instead"},
		},
		{
			name:        "answer resumes",
			reply:       "yes",
			wantEvents:  []eventSummary{working, completed},
			wantOutcome: acp.PermissionOutcome{Outcome: acp.OutcomeSelected, OptionID: "opt-allow"},
			wantPrompts: []string{"delete x.txt"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			outcomes := make(chan acp.PermissionOutcome, 1)
			h := &fakeHarness{onPrompt: func(s *promptScript) {
				if s.promptText() != "delete x.txt" {
					s.end(acp.StopEndTurn)
					return
				}
				outcome := s.askPermission(rmToolCall)
				outcomes <- outcome
				if outcome.OptionID == "opt-reject" {
					waitFor(t, func() bool { _, _, canceled, _ := s.agent.snapshot(); return len(canceled) > 0 })
					s.end(acp.StopCancelled)
					return
				}
				s.end(acp.StopEndTurn)
			}}
			e := newTestExecutor(t, h, Config{})
			execCtx := newExecCtx("ctx", a2a.NewTextPart("delete x.txt"))
			run(t, execute(e, execCtx))

			got := run(t, execute(e, followUp(execCtx, a2a.NewTextPart(tc.reply))))

			if diff := cmp.Diff(tc.wantEvents, got); diff != "" {
				t.Fatalf("Executor.Execute() wrong result (-want +got) diff = %s", diff)
			}
			if diff := cmp.Diff(tc.wantOutcome, <-outcomes); diff != "" {
				t.Fatalf("permission outcome wrong result (-want +got) diff = %s", diff)
			}
			_, _, _, prompts := h.agent(0).snapshot()
			var gotPrompts []string
			for _, p := range prompts {
				gotPrompts = append(gotPrompts, p.Prompt[0].Text)
			}
			if diff := cmp.Diff(tc.wantPrompts, gotPrompts); diff != "" {
				t.Fatalf("session/prompt wrong prompts (-want +got) diff = %s", diff)
			}
		})
	}
}

func TestExecutor_InterruptContinuesResponseArtifact(t *testing.T) {
	t.Parallel()
	h := &fakeHarness{onPrompt: func(s *promptScript) {
		if s.promptText() != "delete x.txt" {
			s.text("m2", "Renamed")
			s.end(acp.StopEndTurn)
			return
		}
		s.text("m1", "Deleting")
		s.askPermission(rmToolCall)
		waitFor(t, func() bool { _, _, canceled, _ := s.agent.snapshot(); return len(canceled) > 0 })
		s.end(acp.StopCancelled)
	}}
	e := newTestExecutor(t, h, Config{})
	execCtx := newExecCtx("ctx", a2a.NewTextPart("delete x.txt"))
	run(t, execute(e, execCtx))

	got := run(t, execute(e, followUp(execCtx, a2a.NewTextPart("rename it instead"))))

	want := []eventSummary{working, {Kind: "artifact", Text: "\n\nRenamed", Append: true}, completed}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("Executor.Execute() wrong result (-want +got) diff = %s", diff)
	}
}

func TestExecutor_InvalidFormValueAskedAgainWhenInterrupting(t *testing.T) {
	t.Parallel()
	h := &fakeHarness{onPrompt: func(s *promptScript) {
		s.elicit("Tell me about you", profileSchema)
		s.end(acp.StopEndTurn)
	}}
	e := newTestExecutor(t, h, Config{})
	execCtx := newExecCtx("ctx", a2a.NewTextPart("hi"))
	run(t, execute(e, execCtx))

	got := run(t, execute(e, followUp(execCtx, a2a.NewTextPart(`{"color": "blue"}`))))

	want := formInputRequired("I couldn't use the reply: validating /properties/content: required: missing properties: [\"name\"]. ", profileFormText, profileSchema, validationError("validating /properties/content: required: missing properties: [\"name\"]"))
	if diff := cmp.Diff([]eventSummary{want}, got); diff != "" {
		t.Fatalf("Executor.Execute() wrong result (-want +got) diff = %s", diff)
	}
	run(t, execute(e, followUp(execCtx, a2a.NewTextPart(`{"name": "Anna"}`))))
}

func TestExecutor_Prompt(t *testing.T) {
	t.Parallel()
	h := &fakeHarness{onPrompt: func(s *promptScript) { s.end(acp.StopEndTurn) }}
	e := newTestExecutor(t, h, Config{Prompt: func(_ context.Context, execCtx *a2asrv.ExecutorContext) ([]*a2a.Part, error) {
		return append([]*a2a.Part{a2a.NewTextPart("[context]")}, execCtx.Message.Parts...), nil
	}})
	execCtx := newExecCtx("ctx", a2a.NewTextPart("hi"))

	var task *a2a.Task
	for ev, err := range e.Execute(t.Context(), execCtx) {
		if err != nil {
			t.Fatalf("Executor.Execute() error = %v", err)
		}
		if v, ok := ev.(*a2a.Task); ok {
			task = v
		}
	}

	_, _, _, prompts := h.agent(0).snapshot()
	want := []acp.ContentBlock{{Type: "text", Text: "[context]"}, {Type: "text", Text: "hi"}}
	if diff := cmp.Diff(want, prompts[0].Prompt); diff != "" {
		t.Fatalf("session/prompt wrong content (-want +got) diff = %s", diff)
	}
	if diff := cmp.Diff([]*a2a.Message{execCtx.Message}, task.History); diff != "" {
		t.Fatalf("Executor.Execute() task history wrong result (-want +got) diff = %s", diff)
	}
}

func TestExecutor_QuestionIsStructuredInputRequest(t *testing.T) {
	t.Parallel()
	h := &fakeHarness{onPrompt: func(s *promptScript) {
		s.askPermission(rmToolCall)
		s.end(acp.StopEndTurn)
	}}
	e := newTestExecutor(t, h, Config{})
	execCtx := newExecCtx("ctx", a2a.NewTextPart("delete x.txt"))

	var status *a2a.TaskStatusUpdateEvent
	for ev, err := range e.Execute(t.Context(), execCtx) {
		if err != nil {
			t.Fatalf("Executor.Execute() error = %v", err)
		}
		if v, ok := ev.(*a2a.TaskStatusUpdateEvent); ok {
			status = v
		}
	}
	req, err := structuredinput.PendingRequest(&a2a.Task{Status: status.Status})

	if err != nil {
		t.Fatalf("structuredinput.PendingRequest() error = %v", err)
	}
	if req == nil || req.ID != "t1" {
		t.Fatalf("structuredinput.PendingRequest() = %+v, want request t1", req)
	}
	if diff := cmp.Diff([]string{structuredinput.URI}, status.Status.Message.Extensions); diff != "" {
		t.Fatalf("input-required message extensions wrong result (-want +got) diff = %s", diff)
	}
	resp, err := structuredinput.NewResponse(execCtx, req, Answer{Option: OptionOnce})
	if err != nil {
		t.Fatalf("structuredinput.NewResponse() error = %v", err)
	}
	got := run(t, execute(e, followUp(execCtx, resp.Parts...)))
	if diff := cmp.Diff([]eventSummary{working, completed}, got); diff != "" {
		t.Fatalf("Executor.Execute() answer wrong result (-want +got) diff = %s", diff)
	}
}

func TestExecutor_UnusableAnswerAskedAgain(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		form   bool
		answer func(t *testing.T) *a2a.Part
		want   eventSummary
	}{
		{
			name:   "answer to another question",
			answer: func(t *testing.T) *a2a.Part { return responsePart(t, "t0", Answer{Option: OptionOnce}) },
			want:   rmInputRequired("I couldn't use the reply: it answers a different question. "),
		},
		{
			name:   "malformed json",
			form:   true,
			answer: textAnswer(`{"name": `),
			want:   formInputRequired("I couldn't use the reply: the reply is not a valid JSON object: unexpected end of JSON input. ", profileFormText, profileSchema),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := &fakeHarness{onPrompt: func(s *promptScript) {
				if tc.form {
					s.elicit("Tell me about you", profileSchema)
				} else {
					s.askPermission(rmToolCall)
				}
				s.end(acp.StopEndTurn)
			}}
			e := newTestExecutor(t, h, Config{})
			execCtx := newExecCtx("ctx", a2a.NewTextPart("hi"))
			run(t, execute(e, execCtx))

			got := run(t, execute(e, followUp(execCtx, tc.answer(t))))

			if diff := cmp.Diff([]eventSummary{tc.want}, got); diff != "" {
				t.Fatalf("Executor.Execute() wrong result (-want +got) diff = %s", diff)
			}
			run(t, execute(e, followUp(execCtx, a2a.NewTextPart("decline"))))
		})
	}
}
