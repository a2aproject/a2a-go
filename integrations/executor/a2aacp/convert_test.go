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

type convertedEvent struct {
	Kind          string
	Artifact      string
	Text          string
	Data          []any
	Append        bool
	Metadata      map[string]any
	EventMetadata map[string]any
}

// runConverted executes the task and summarizes the events emitted for agent updates.
func runConverted(t *testing.T, e *Executor, execCtx *a2asrv.ExecutorContext) []convertedEvent {
	t.Helper()
	var got []convertedEvent
	for ev, err := range e.Execute(t.Context(), execCtx) {
		if err != nil {
			t.Fatalf("Executor.Execute() error = %v", err)
		}
		switch v := ev.(type) {
		case *a2a.TaskArtifactUpdateEvent:
			c := convertedEvent{Kind: "artifact", Artifact: v.Artifact.Name, Append: v.Append, Metadata: v.Artifact.Metadata, EventMetadata: v.Metadata}
			c.Text, c.Data = partsSummary(v.Artifact.Parts)
			got = append(got, c)
		case *a2a.TaskStatusUpdateEvent:
			if v.Status.State != a2a.TaskStateWorking || v.Status.Message == nil {
				continue
			}
			c := convertedEvent{Kind: "status", Metadata: v.Status.Message.Metadata, EventMetadata: v.Metadata}
			c.Text, c.Data = partsSummary(v.Status.Message.Parts)
			got = append(got, c)
		}
	}
	return got
}

func thoughtsAndPlan(ctx context.Context, execCtx *a2asrv.ExecutorContext, u Update) (*ConverterResult, error) {
	switch u.Type {
	case "agent_thought_chunk":
		var chunk struct {
			Content acp.ContentBlock `json:"content"`
		}
		if err := json.Unmarshal(u.Raw, &chunk); err != nil {
			return nil, err
		}
		return &ConverterResult{Artifact: "thoughts", Parts: []*a2a.Part{a2a.NewTextPart(chunk.Content.Text)}}, nil
	case "plan":
		var plan struct {
			Entries []any `json:"entries"`
		}
		if err := json.Unmarshal(u.Raw, &plan); err != nil {
			return nil, err
		}
		return &ConverterResult{
			Parts:         []*a2a.Part{a2a.NewDataPart(map[string]any{"plan": plan.Entries})},
			Metadata:      map[string]any{"kind": "plan"},
			EventMetadata: map[string]any{"source": "acp"},
		}, nil
	}
	return ConvertUpdate(ctx, execCtx, u)
}

func TestExecutor_Converter(t *testing.T) {
	t.Parallel()
	h := &fakeHarness{onPrompt: func(s *promptScript) {
		s.update(map[string]any{"sessionUpdate": "agent_thought_chunk", "content": map[string]any{"type": "text", "text": "Thinking"}})
		s.update(map[string]any{"sessionUpdate": "agent_thought_chunk", "content": map[string]any{"type": "text", "text": " hard"}})
		s.text("m1", "Hello")
		s.update(map[string]any{"sessionUpdate": "agent_thought_chunk", "content": map[string]any{"type": "text", "text": "Done"}})
		s.update(map[string]any{"sessionUpdate": "plan", "entries": []any{map[string]any{"content": "step 1", "status": "pending"}}})
		s.end(acp.StopEndTurn)
	}}
	e := newTestExecutor(t, h, Config{Converter: ConverterFunc(thoughtsAndPlan)})

	got := runConverted(t, e, newExecCtx("ctx", a2a.NewTextPart("hi")))

	want := []convertedEvent{
		{Kind: "artifact", Artifact: "thoughts", Text: "Thinking"},
		{Kind: "artifact", Text: " hard", Append: true},
		{Kind: "artifact", Artifact: ResponseArtifact, Text: "Hello"},
		{Kind: "artifact", Text: "\n\nDone", Append: true},
		{
			Kind:          "status",
			Data:          []any{map[string]any{"plan": []any{map[string]any{"content": "step 1", "status": "pending"}}}},
			Metadata:      map[string]any{"kind": "plan"},
			EventMetadata: map[string]any{"source": "acp"},
		},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("Executor.Execute() wrong result (-want +got) diff = %s", diff)
	}
}

func TestExecutor_AgentContentStreamsIntoResponse(t *testing.T) {
	t.Parallel()
	h := &fakeHarness{onPrompt: func(s *promptScript) {
		s.text("m1", "Look")
		s.update(map[string]any{"sessionUpdate": acp.UpdateAgentMessageChunk, "messageId": "m1", "content": map[string]any{"type": "image", "data": "cG5n", "mimeType": "image/png"}})
		s.update(map[string]any{"sessionUpdate": acp.UpdateAgentMessageChunk, "messageId": "m1", "content": map[string]any{"type": "resource_link", "uri": "https://example.com/a.txt", "name": "a.txt"}})
		s.end(acp.StopEndTurn)
	}}
	e := newTestExecutor(t, h, Config{})

	got := runConverted(t, e, newExecCtx("ctx", a2a.NewTextPart("hi")))

	want := []convertedEvent{
		{Kind: "artifact", Artifact: ResponseArtifact, Text: "Look"},
		{Kind: "artifact", Data: []any{`raw image/png "png"`}, Append: true},
		{Kind: "artifact", Data: []any{"url https://example.com/a.txt"}, Append: true},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("Executor.Execute() wrong result (-want +got) diff = %s", diff)
	}
}

func TestExecutor_ConverterSkipsEvents(t *testing.T) {
	t.Parallel()
	h := &fakeHarness{onPrompt: func(s *promptScript) {
		s.text("m1", "secret")
		s.text("m1", "Hello")
		s.end(acp.StopEndTurn)
	}}
	e := newTestExecutor(t, h, Config{Converter: ConverterFunc(func(ctx context.Context, execCtx *a2asrv.ExecutorContext, u Update) (*ConverterResult, error) {
		res, err := ConvertUpdate(ctx, execCtx, u)
		if err != nil || res == nil || res.Parts[0].Text() == "secret" {
			return nil, err
		}
		return res, nil
	})})

	got := runConverted(t, e, newExecCtx("ctx", a2a.NewTextPart("hi")))

	if diff := cmp.Diff([]convertedEvent{{Kind: "artifact", Artifact: ResponseArtifact, Text: "Hello"}}, got); diff != "" {
		t.Fatalf("Executor.Execute() wrong result (-want +got) diff = %s", diff)
	}
}

func TestExecutor_ConverterReceivesRawUpdateWithMergedToolCall(t *testing.T) {
	t.Parallel()
	h := &fakeHarness{onPrompt: func(s *promptScript) {
		s.update(map[string]any{"sessionUpdate": acp.UpdateToolCall, "toolCallId": "t1", "title": "Read file", "kind": "read", "status": "pending"})
		s.update(map[string]any{"sessionUpdate": acp.UpdateToolCallUpdate, "toolCallId": "t1", "status": "completed", "rawOutput": "a.txt"})
		s.end(acp.StopEndTurn)
	}}
	var rawOutputs []string
	e := newTestExecutor(t, h, Config{Converter: ConverterFunc(func(ctx context.Context, execCtx *a2asrv.ExecutorContext, u Update) (*ConverterResult, error) {
		var raw struct {
			RawOutput string `json:"rawOutput"`
		}
		if err := json.Unmarshal(u.Raw, &raw); err != nil {
			return nil, err
		}
		rawOutputs = append(rawOutputs, raw.RawOutput)
		return ConvertUpdate(ctx, execCtx, u)
	})})

	got := runConverted(t, e, newExecCtx("ctx", a2a.NewTextPart("list files")))

	want := []convertedEvent{{
		Kind: "status",
		Text: "Finished Read file",
		Data: []any{map[string]any{"toolCall": map[string]any{"id": "t1", "title": "Read file", "kind": "read", "status": "completed"}}},
	}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("Executor.Execute() wrong result (-want +got) diff = %s", diff)
	}
	if diff := cmp.Diff([]string{"", "a.txt"}, rawOutputs); diff != "" {
		t.Fatalf("Update.Raw rawOutput wrong result (-want +got) diff = %s", diff)
	}
}

func TestExecutor_ConverterErrorFailsTask(t *testing.T) {
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
	e := newTestExecutor(t, h, Config{Converter: ConverterFunc(func(context.Context, *a2asrv.ExecutorContext, Update) (*ConverterResult, error) {
		return nil, errors.New("boom")
	})})

	got := run(t, execute(e, newExecCtx("ctx", a2a.NewTextPart("hi"))))

	if diff := cmp.Diff([]eventSummary{submitted, working, failedWith("converting acp update: boom")}, got); diff != "" {
		t.Fatalf("Executor.Execute() wrong result (-want +got) diff = %s", diff)
	}
	<-scriptDone
}
