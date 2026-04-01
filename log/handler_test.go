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

package log

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/go-cmp/cmp"
)

func TestHandler_Handle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		attrs    []slog.Attr
		wantKeys map[string]any
	}{
		{
			name: "Task",
			attrs: []slog.Attr{
				slog.Any("task", &a2a.Task{
					ID:        "t-1",
					ContextID: "ctx-1",
					Status:    a2a.TaskStatus{State: a2a.TaskStateWorking},
					History:   []*a2a.Message{{ID: "m-1"}},
				}),
			},
			wantKeys: map[string]any{
				"task": map[string]any{
					"id":         "t-1",
					"context_id": "ctx-1",
					"state":      "TASK_STATE_WORKING",
				},
			},
		},
		{
			name: "Message without task_id",
			attrs: []slog.Attr{
				slog.Any("msg", &a2a.Message{
					ID:   "m-1",
					Role: a2a.MessageRoleUser,
					Parts: a2a.ContentParts{
						a2a.NewTextPart("hello"),
						a2a.NewTextPart("world"),
					},
				}),
			},
			wantKeys: map[string]any{
				"msg": map[string]any{
					"id":    "m-1",
					"role":  "ROLE_USER",
					"parts": float64(2),
				},
			},
		},
		{
			name: "Message with task_id",
			attrs: []slog.Attr{
				slog.Any("msg", &a2a.Message{
					ID:     "m-2",
					Role:   a2a.MessageRoleAgent,
					TaskID: "t-1",
					Parts:  a2a.ContentParts{a2a.NewTextPart("hi")},
				}),
			},
			wantKeys: map[string]any{
				"msg": map[string]any{
					"id":      "m-2",
					"role":    "ROLE_AGENT",
					"task_id": "t-1",
					"parts":   float64(1),
				},
			},
		},
		{
			name: "TaskStatusUpdateEvent",
			attrs: []slog.Attr{
				slog.Any("event", &a2a.TaskStatusUpdateEvent{
					TaskID:    "t-1",
					ContextID: "c-1",
					Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
				}),
			},
			wantKeys: map[string]any{
				"event": map[string]any{
					"task_id":    "t-1",
					"context_id": "c-1",
					"state":      "TASK_STATE_COMPLETED",
				},
			},
		},
		{
			name: "TaskArtifactUpdateEvent",
			attrs: []slog.Attr{
				slog.Any("event", &a2a.TaskArtifactUpdateEvent{
					TaskID:    "t-1",
					ContextID: "c-1",
					Artifact: &a2a.Artifact{
						ID:    "a-1",
						Parts: a2a.ContentParts{a2a.NewTextPart("data")},
					},
					Append:    true,
					LastChunk: true,
				}),
			},
			wantKeys: map[string]any{
				"event": map[string]any{
					"task_id":     "t-1",
					"artifact_id": "a-1",
					"append":      true,
					"last_chunk":  true,
					"context_id":  "c-1",
					"parts":       float64(1),
				},
			},
		},
		{
			name: "non-A2A type passes through unchanged",
			attrs: []slog.Attr{
				slog.String("key", "value"),
				slog.Int("count", 42),
			},
			wantKeys: map[string]any{
				"key":   "value",
				"count": float64(42),
			},
		},
		{
			name: "nil Task passes through",
			attrs: []slog.Attr{
				slog.Any("task", (*a2a.Task)(nil)),
			},
			wantKeys: map[string]any{
				"task": nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
			handler := AttachFormatter(inner, DefaultA2ATypeFormatter)
			logger := slog.New(handler)

			logger.Info("test", attrsToArgs(tt.attrs)...)

			var got map[string]any
			if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
				t.Fatalf("json.Unmarshal() error = %v, for %q", err, buf.String())
			}

			for k, want := range tt.wantKeys {
				gotVal, ok := got[k]
				if !ok {
					t.Fatalf("missing key %q in log output: %s", k, buf.String())
				}
				if diff := cmp.Diff(want, gotVal); diff != "" {
					t.Fatalf("key %q wrong result (+got,-want) diff = %s", k, diff)
				}
			}
		})
	}
}

func TestHandler_WithAttrs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	handler := AttachFormatter(inner, DefaultA2ATypeFormatter)

	task := &a2a.Task{
		ID:        "t-1",
		ContextID: "ctx-1",
		Status:    a2a.TaskStatus{State: a2a.TaskStateSubmitted},
	}
	withTask := handler.WithAttrs([]slog.Attr{slog.Any("task", task)})
	slog.New(withTask).Info("with-attrs test")

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, for %q", err, buf.String())
	}

	want := map[string]any{
		"id":         "t-1",
		"context_id": "ctx-1",
		"state":      "TASK_STATE_SUBMITTED",
	}
	gotTask, ok := got["task"]
	if !ok {
		t.Fatalf("missing key \"task\" in log output: %s", buf.String())
	}
	if diff := cmp.Diff(want, gotTask); diff != "" {
		t.Fatalf("Handler.WithAttrs() wrong result (+got,-want) diff = %s", diff)
	}
}

func TestHandler_WithGroup(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	handler := AttachFormatter(inner, DefaultA2ATypeFormatter)

	grouped := handler.WithGroup("a2a")
	slog.New(grouped).Info("grouped test",
		slog.Any("task", &a2a.Task{
			ID:        "t-1",
			ContextID: "ctx-1",
			Status:    a2a.TaskStatus{State: a2a.TaskStateWorking},
		}),
	)

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, for %q", err, buf.String())
	}

	group, ok := got["a2a"].(map[string]any)
	if !ok {
		t.Fatalf("missing group \"a2a\" in log output: %s", buf.String())
	}

	want := map[string]any{
		"id":         "t-1",
		"context_id": "ctx-1",
		"state":      "TASK_STATE_WORKING",
	}
	gotTask, ok := group["task"]
	if !ok {
		t.Fatalf("missing key \"task\" in group: %s", buf.String())
	}
	if diff := cmp.Diff(want, gotTask); diff != "" {
		t.Fatalf("Handler.WithGroup() wrong result (+got,-want) diff = %s", diff)
	}
}

func TestHandler_Enabled(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
	handler := AttachFormatter(inner, DefaultA2ATypeFormatter)

	if handler.Enabled(t.Context(), slog.LevelDebug) {
		t.Fatalf("Handler.Enabled(Debug) = true, want false")
	}
	if !handler.Enabled(t.Context(), slog.LevelWarn) {
		t.Fatalf("Handler.Enabled(Warn) = false, want true")
	}
}

func TestHandler_nestedGroupFormatting(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	handler := AttachFormatter(inner, DefaultA2ATypeFormatter)
	logger := slog.New(handler)

	logger.Info("nested",
		slog.Group("outer",
			slog.Any("task", &a2a.Task{
				ID:        "t-nested",
				ContextID: "ctx-nested",
				Status:    a2a.TaskStatus{State: a2a.TaskStateFailed},
			}),
		),
	)

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, for %q", err, buf.String())
	}

	outer, ok := got["outer"].(map[string]any)
	if !ok {
		t.Fatalf("missing group \"outer\" in log output: %s", buf.String())
	}

	want := map[string]any{
		"id":         "t-nested",
		"context_id": "ctx-nested",
		"state":      "TASK_STATE_FAILED",
	}
	gotTask, ok := outer["task"]
	if !ok {
		t.Fatalf("missing key \"task\" in outer group: %s", buf.String())
	}
	if diff := cmp.Diff(want, gotTask); diff != "" {
		t.Fatalf("nested group formatting wrong result (+got,-want) diff = %s", diff)
	}
}

type sensitiveValue struct{ raw string }

func TestHandler_customFormatter(t *testing.T) {
	t.Parallel()

	custom := func(val any) (slog.Value, bool) {
		if _, ok := val.(sensitiveValue); ok {
			return slog.StringValue("[REDACTED]"), true
		}
		return slog.Value{}, false
	}

	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	handler := AttachFormatter(inner, custom)
	logger := slog.New(handler)

	logger.Info("test", slog.Any("password", sensitiveValue{"s3cret"}), slog.String("user", "alice"))

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, for %q", err, buf.String())
	}

	if got["password"] != "[REDACTED]" {
		t.Fatalf("custom formatter: password = %v, want [REDACTED]", got["password"])
	}
	if got["user"] != "alice" {
		t.Fatalf("custom formatter: user = %v, want alice", got["user"])
	}
}

func attrsToArgs(attrs []slog.Attr) []any {
	args := make([]any, len(attrs))
	for i, a := range attrs {
		args[i] = a
	}
	return args
}
