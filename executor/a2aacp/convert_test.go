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
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/internal/acp"
	"github.com/google/go-cmp/cmp"
)

func planAsStatus(execCtx *a2asrv.ExecutorContext) UpdateConverter {
	return UpdateConverterFunc(func(_ context.Context, u Update, events []a2a.Event) ([]a2a.Event, error) {
		if u.Type != "plan" {
			return events, nil
		}
		var plan struct {
			Entries []any `json:"entries"`
		}
		if err := json.Unmarshal(u.Raw, &plan); err != nil {
			return nil, err
		}
		msg := a2a.NewMessageForTask(a2a.MessageRoleAgent, execCtx, a2a.NewDataPart(map[string]any{"plan": plan.Entries}))
		return append(events, a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateWorking, msg)), nil
	})
}

func TestExecutor_UpdateConverter(t *testing.T) {
	t.Parallel()
	h := &fakeHarness{onPrompt: func(s *promptScript) {
		s.update(map[string]any{"sessionUpdate": "agent_thought_chunk", "content": map[string]any{"type": "text", "text": "Thinking"}})
		s.update(map[string]any{"sessionUpdate": "agent_thought_chunk", "content": map[string]any{"type": "text", "text": " hard"}})
		s.text("m1", "Hello")
		s.update(map[string]any{"sessionUpdate": "plan", "entries": []any{map[string]any{"content": "step 1", "status": "pending"}}})
		s.end(acp.StopEndTurn)
	}}
	e := newTestExecutor(t, h, Config{
		UpdateConverter: func(_ context.Context, execCtx *a2asrv.ExecutorContext) UpdateConverter {
			return ChainUpdateConverters(ThoughtsAsArtifact(execCtx, "thoughts"), planAsStatus(execCtx))
		},
	})

	got := run(t, execute(e, newExecCtx("ctx", a2a.NewTextPart("hi"))))

	want := []eventSummary{
		submitted,
		working,
		{Kind: "artifact", Text: "Thinking"},
		{Kind: "artifact", Text: " hard", Append: true},
		{Kind: "artifact", Text: "Hello"},
		{Kind: "status", State: a2a.TaskStateWorking, Data: []any{map[string]any{"plan": []any{map[string]any{"content": "step 1", "status": "pending"}}}}},
		completed,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("Executor.Execute() wrong result (-want +got) diff = %s", diff)
	}
}

func TestExecutor_UpdateConverterReceivesRawUpdateAndDefaults(t *testing.T) {
	t.Parallel()
	h := &fakeHarness{onPrompt: func(s *promptScript) {
		s.update(map[string]any{"sessionUpdate": acp.UpdateToolCallUpdate, "toolCallId": "t1", "title": "ls", "status": "completed", "rawOutput": "a.txt"})
		s.end(acp.StopEndTurn)
	}}
	type call struct {
		Type      string
		RawOutput string
		Defaults  int
	}
	var calls []call
	e := newTestExecutor(t, h, Config{
		UpdateConverter: func(context.Context, *a2asrv.ExecutorContext) UpdateConverter {
			return UpdateConverterFunc(func(_ context.Context, u Update, events []a2a.Event) ([]a2a.Event, error) {
				var raw struct {
					RawOutput string `json:"rawOutput"`
				}
				if err := json.Unmarshal(u.Raw, &raw); err != nil {
					return nil, err
				}
				calls = append(calls, call{Type: u.Type, RawOutput: raw.RawOutput, Defaults: len(events)})
				return events, nil
			})
		},
	})

	run(t, execute(e, newExecCtx("ctx", a2a.NewTextPart("list files"))))

	want := []call{{Type: acp.UpdateToolCallUpdate, RawOutput: "a.txt", Defaults: 1}}
	if diff := cmp.Diff(want, calls); diff != "" {
		t.Fatalf("UpdateConverter.Convert() calls wrong result (-want +got) diff = %s", diff)
	}
}

func TestExecutor_UpdateConverterErrorFailsTask(t *testing.T) {
	t.Parallel()
	scriptDone := make(chan struct{})
	h := &fakeHarness{}
	h.onPrompt = func(s *promptScript) {
		defer close(scriptDone)
		s.text("m1", "Hello")
		waitFor(t, func() bool {
			_, _, canceled, _ := s.agent.snapshot()
			return len(canceled) > 0
		})
		s.end(acp.StopCancelled)
	}
	e := newTestExecutor(t, h, Config{
		UpdateConverter: func(context.Context, *a2asrv.ExecutorContext) UpdateConverter {
			return UpdateConverterFunc(func(context.Context, Update, []a2a.Event) ([]a2a.Event, error) {
				return nil, errors.New("boom")
			})
		},
	})

	got := run(t, execute(e, newExecCtx("ctx", a2a.NewTextPart("hi"))))

	if diff := cmp.Diff([]eventSummary{submitted, working, failedWith("converting acp update: boom")}, got); diff != "" {
		t.Fatalf("Executor.Execute() wrong result (-want +got) diff = %s", diff)
	}
	<-scriptDone
}

func TestExecutor_ConvertPart(t *testing.T) {
	t.Parallel()
	linkBlock := json.RawMessage(`{"type":"resource_link","uri":"https://example.com/spec","name":"spec"}`)
	tests := []struct {
		name        string
		convert     func(context.Context, *a2asrv.ExecutorContext, *a2a.Part) (json.RawMessage, bool, error)
		want        []eventSummary
		wantPrompts [][]acp.ContentBlock
	}{
		{
			name: "converted and built-in parts",
			convert: func(_ context.Context, _ *a2asrv.ExecutorContext, p *a2a.Part) (json.RawMessage, bool, error) {
				if _, ok := p.Content.(a2a.Data); ok {
					return linkBlock, true, nil
				}
				return nil, false, nil
			},
			want: []eventSummary{submitted, working, completed},
			wantPrompts: [][]acp.ContentBlock{{
				{Type: "text", Text: "read this"},
				{Type: "resource_link", URI: "https://example.com/spec", Name: "spec"},
			}},
		},
		{
			name: "error",
			convert: func(context.Context, *a2asrv.ExecutorContext, *a2a.Part) (json.RawMessage, bool, error) {
				return nil, false, errors.New("unsupported")
			},
			want: []eventSummary{submitted, failedWith("converting part 0: unsupported")},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := &fakeHarness{onPrompt: func(s *promptScript) { s.end(acp.StopEndTurn) }}
			e := newTestExecutor(t, h, Config{ConvertPart: tc.convert})

			got := run(t, execute(e, newExecCtx("ctx", a2a.NewTextPart("read this"), a2a.NewDataPart(map[string]any{"spec": true}))))

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("Executor.Execute() wrong result (-want +got) diff = %s", diff)
			}
			_, _, _, prompts := h.agent(0).snapshot()
			var gotPrompts [][]acp.ContentBlock
			for _, p := range prompts {
				gotPrompts = append(gotPrompts, p.Prompt)
			}
			if diff := cmp.Diff(tc.wantPrompts, gotPrompts); diff != "" {
				t.Fatalf("session/prompt content wrong result (-want +got) diff = %s", diff)
			}
		})
	}
}
