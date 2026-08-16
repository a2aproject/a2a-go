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

package a2aevent_test

import (
	"strings"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aevent"
	"github.com/a2aproject/a2a-go/v2/internal/utils"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestApplyUpdate(t *testing.T) {
	t.Parallel()
	tip, ti := newTestTaskInfo()

	testCases := []struct {
		name           string
		base           *a2a.Task
		event          a2a.Event
		want           *a2a.Task
		wantErrContain string
	}{
		{
			name:           "message cannot be applied",
			base:           newTask(tip, a2a.TaskStateWorking),
			event:          a2a.NewMessageForTask(a2a.MessageRoleAgent, tip, a2a.NewTextPart("hi")),
			wantErrContain: "message cannot be applied",
		},
		{
			name:           "completed task rejects updates",
			base:           newTask(tip, a2a.TaskStateCompleted),
			event:          a2a.NewStatusUpdateEvent(tip, a2a.TaskStateWorking, nil),
			wantErrContain: "state updates are not allowed",
		},
		{
			name:           "canceled task rejects updates",
			base:           newTask(tip, a2a.TaskStateCanceled),
			event:          a2a.NewStatusUpdateEvent(tip, a2a.TaskStateWorking, nil),
			wantErrContain: "state updates are not allowed",
		},
		{
			name:           "failed task rejects updates",
			base:           newTask(tip, a2a.TaskStateFailed),
			event:          a2a.NewStatusUpdateEvent(tip, a2a.TaskStateWorking, nil),
			wantErrContain: "state updates are not allowed",
		},
		{
			name:           "rejected task rejects updates",
			base:           newTask(tip, a2a.TaskStateRejected),
			event:          a2a.NewStatusUpdateEvent(tip, a2a.TaskStateWorking, nil),
			wantErrContain: "state updates are not allowed",
		},
		{
			name: "task ID mismatch",
			base: newTask(tip, a2a.TaskStateWorking),
			event: &a2a.TaskStatusUpdateEvent{
				TaskID: ti.TaskID + "++", ContextID: ti.ContextID,
				Status: a2a.TaskStatus{State: a2a.TaskStateWorking},
			},
			wantErrContain: "task IDs don't match",
		},
		{
			name: "context ID mismatch",
			base: newTask(tip, a2a.TaskStateWorking),
			event: &a2a.TaskStatusUpdateEvent{
				TaskID: ti.TaskID, ContextID: ti.ContextID + "++",
				Status: a2a.TaskStatus{State: a2a.TaskStateWorking},
			},
			wantErrContain: "context IDs don't match",
		},
		{
			name:  "task event replaces state entirely",
			base:  newTask(tip, a2a.TaskStateWorking, &a2a.Artifact{ID: "a1", Parts: makeTextParts("Hello")}),
			event: newTask(tip, a2a.TaskStateInputRequired, &a2a.Artifact{ID: "a2", Parts: makeTextParts("World!")}),
			want:  newTask(tip, a2a.TaskStateInputRequired, &a2a.Artifact{ID: "a2", Parts: makeTextParts("World!")}),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			before, err := utils.DeepCopy(tc.base)
			if err != nil {
				t.Fatalf("utils.DeepCopy() error = %v, want nil", err)
			}

			got, err := a2aevent.ApplyUpdate(tc.base, tc.event)
			if tc.wantErrContain != "" {
				if err == nil {
					t.Fatalf("a2aevent.ApplyUpdate() error = nil, want error")
				}
				if !strings.Contains(err.Error(), tc.wantErrContain) {
					t.Fatalf("a2aevent.ApplyUpdate() error = %v, want msg containing %q", err, tc.wantErrContain)
				}
				if diff := cmp.Diff(tc.base, before); diff != "" { // input modified
					t.Fatalf("input task was mutated (-before +after) diff = %s", diff)
				}
				return
			}

			if err != nil {
				t.Fatalf("a2aevent.ApplyUpdate() error = %v, want nil", err)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("a2aevent.ApplyUpdate() wrong result (-want +got) diff = %s", diff)
			}
			if diff := cmp.Diff(before, tc.base); diff != "" { // input modified
				t.Fatalf("input task was mutated (-before +after) diff = %s", diff)
			}
		})
	}
}

func TestApplyUpdate_ArtifactUpdate(t *testing.T) {
	t.Parallel()

	tip, ti := newTestTaskInfo()

	testCases := []struct {
		name    string
		base    *a2a.Task
		event   *a2a.TaskArtifactUpdateEvent
		want    *a2a.Task
		wantErr bool
	}{
		{
			name: "new artifact",
			base: newTask(ti, a2a.TaskStateWorking),
			event: &a2a.TaskArtifactUpdateEvent{
				TaskID: ti.TaskID, ContextID: ti.ContextID,
				Artifact: &a2a.Artifact{ID: "a1", Parts: makeTextParts("Hello")},
			},
			want: newTask(tip, a2a.TaskStateWorking, &a2a.Artifact{ID: "a1", Parts: makeTextParts("Hello")}),
		},
		{
			name: "append new artifacts",
			base: newTask(tip, a2a.TaskStateWorking, &a2a.Artifact{ID: "a1", Parts: makeTextParts("Hello")}),
			event: &a2a.TaskArtifactUpdateEvent{
				TaskID: ti.TaskID, ContextID: ti.ContextID,
				Artifact: &a2a.Artifact{ID: "a2", Parts: makeTextParts("World")},
			},
			want: newTask(
				tip,
				a2a.TaskStateWorking,
				&a2a.Artifact{ID: "a1", Parts: makeTextParts("Hello")},
				&a2a.Artifact{ID: "a2", Parts: makeTextParts("World")},
			),
		},
		{
			name: "replace artifact",
			base: newTask(tip, a2a.TaskStateWorking, &a2a.Artifact{ID: "a1", Parts: makeTextParts("Hello")}),
			event: &a2a.TaskArtifactUpdateEvent{
				TaskID: ti.TaskID, ContextID: ti.ContextID,
				Artifact: &a2a.Artifact{ID: "a1", Parts: makeTextParts("World")},
			},
			want: newTask(tip, a2a.TaskStateWorking, &a2a.Artifact{ID: "a1", Parts: makeTextParts("World")}),
		},
		{
			name: "append parts",
			base: newTask(tip, a2a.TaskStateWorking, &a2a.Artifact{ID: "a1", Parts: makeTextParts("Hello")}),
			event: &a2a.TaskArtifactUpdateEvent{
				Append: true,
				TaskID: ti.TaskID, ContextID: ti.ContextID,
				Artifact: &a2a.Artifact{ID: "a1", Parts: makeTextParts(", world!")},
			},
			want: newTask(tip, a2a.TaskStateWorking, &a2a.Artifact{ID: "a1", Parts: makeTextParts("Hello", ", world!")}),
		},
		{
			name: "append metadata set",
			base: newTask(tip, a2a.TaskStateWorking, &a2a.Artifact{ID: "a1", Parts: makeTextParts("Hel")}),
			event: &a2a.TaskArtifactUpdateEvent{
				Append: true,
				TaskID: ti.TaskID, ContextID: ti.ContextID,
				Artifact: &a2a.Artifact{ID: "a1", Parts: makeTextParts("lo"), Metadata: map[string]any{"b": "2"}},
			},
			want: newTask(tip, a2a.TaskStateWorking, &a2a.Artifact{ID: "a1", Parts: makeTextParts("Hel", "lo"), Metadata: map[string]any{"b": "2"}}),
		},
		{
			name: "append metadata update",
			base: newTask(tip, a2a.TaskStateWorking, &a2a.Artifact{
				ID: "a1", Parts: makeTextParts("Hel"), Metadata: map[string]any{"a": "1", "shared": "old"},
			}),
			event: &a2a.TaskArtifactUpdateEvent{
				Append: true,
				TaskID: ti.TaskID, ContextID: ti.ContextID,
				Artifact: &a2a.Artifact{ID: "a1", Parts: makeTextParts("lo"), Metadata: map[string]any{"b": "2", "shared": "new"}},
			},
			want: newTask(tip, a2a.TaskStateWorking, &a2a.Artifact{
				ID: "a1", Parts: makeTextParts("Hel", "lo"),
				Metadata: map[string]any{"a": "1", "b": "2", "shared": "new"},
			}),
		},
		{
			name: "append to non-existent artifact fails",
			base: newTask(tip, a2a.TaskStateWorking),
			event: &a2a.TaskArtifactUpdateEvent{
				Append: true,
				TaskID: ti.TaskID, ContextID: ti.ContextID,
				Artifact: &a2a.Artifact{ID: "a1", Parts: makeTextParts("Hello")},
			},
			wantErr: true,
		},
		{
			name: "empty artifact fails",
			base: newTask(tip, a2a.TaskStateWorking),
			event: &a2a.TaskArtifactUpdateEvent{
				TaskID: ti.TaskID, ContextID: ti.ContextID,
				Artifact: &a2a.Artifact{ID: "a1"},
			},
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			before, err := utils.DeepCopy(tc.base)
			if err != nil {
				t.Fatalf("utils.DeepCopy() error = %v, want nil", err)
			}

			got, err := a2aevent.ApplyUpdate(tc.base, tc.event)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("a2aevent.ApplyUpdate() error = nil, want error")
				}
				if diff := cmp.Diff(before, tc.base); diff != "" { // input modified
					t.Fatalf("input task was mutated (-before +after) diff = %s", diff)
				}
				return
			}
			if err != nil {
				t.Fatalf("a2aevent.ApplyUpdate() error = %v, want nil", err)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("a2aevent.ApplyUpdate() wrong result (-want +got) diff = %s", diff)
			}
			if diff := cmp.Diff(before, tc.base); diff != "" { // input modified
				t.Fatalf("input task was mutated (-before +after) diff = %s", diff)
			}
		})
	}
}

func TestApplyUpdate_StatusUpdate(t *testing.T) {
	t.Parallel()

	tip, ti := newTestTaskInfo()

	statusMessage := &a2a.Message{ID: "m1", Role: a2a.MessageRoleAgent, Parts: makeTextParts("previous")}
	newMessage := &a2a.Message{ID: "m2", Role: a2a.MessageRoleAgent, Parts: makeTextParts("current")}

	testCases := []struct {
		name  string
		base  *a2a.Task
		event *a2a.TaskStatusUpdateEvent
		want  *a2a.Task
	}{
		{
			name:  "state update",
			base:  newTask(tip, a2a.TaskStateWorking),
			event: a2a.NewStatusUpdateEvent(tip, a2a.TaskStateInputRequired, nil),
			want:  newTask(tip, a2a.TaskStateInputRequired),
		},
		{
			name: "message replaced with nil",
			base: &a2a.Task{
				ID: ti.TaskID, ContextID: ti.ContextID,
				Status: a2a.TaskStatus{State: a2a.TaskStateWorking, Message: statusMessage},
			},
			event: a2a.NewStatusUpdateEvent(tip, a2a.TaskStateWorking, nil),
			want: &a2a.Task{
				ID: ti.TaskID, ContextID: ti.ContextID,
				Status:  a2a.TaskStatus{State: a2a.TaskStateWorking},
				History: []*a2a.Message{statusMessage},
			},
		},
		{
			name: "message replaced with message",
			base: &a2a.Task{
				ID: ti.TaskID, ContextID: ti.ContextID,
				Status: a2a.TaskStatus{State: a2a.TaskStateWorking, Message: statusMessage},
			},
			event: a2a.NewStatusUpdateEvent(tip, a2a.TaskStateWorking, newMessage),
			want: &a2a.Task{
				ID: ti.TaskID, ContextID: ti.ContextID,
				Status:  a2a.TaskStatus{State: a2a.TaskStateWorking, Message: newMessage},
				History: []*a2a.Message{statusMessage},
			},
		},
		{
			name: "task metadata set",
			base: newTask(tip, a2a.TaskStateWorking),
			event: &a2a.TaskStatusUpdateEvent{
				TaskID: ti.TaskID, ContextID: ti.ContextID,
				Status:   a2a.TaskStatus{State: a2a.TaskStateWorking},
				Metadata: map[string]any{"b": "2"},
			},
			want: &a2a.Task{
				ID: ti.TaskID, ContextID: ti.ContextID,
				Status:   a2a.TaskStatus{State: a2a.TaskStateWorking},
				Metadata: map[string]any{"b": "2"},
			},
		},
		{
			name: "task metadata updated",
			base: &a2a.Task{
				ID: ti.TaskID, ContextID: ti.ContextID,
				Status:   a2a.TaskStatus{State: a2a.TaskStateWorking},
				Metadata: map[string]any{"a": "1", "shared": "old"},
			},
			event: &a2a.TaskStatusUpdateEvent{
				TaskID: ti.TaskID, ContextID: ti.ContextID,
				Status:   a2a.TaskStatus{State: a2a.TaskStateWorking},
				Metadata: map[string]any{"b": "2", "shared": "new"},
			},
			want: &a2a.Task{
				ID: ti.TaskID, ContextID: ti.ContextID,
				Status:   a2a.TaskStatus{State: a2a.TaskStateWorking},
				Metadata: map[string]any{"a": "1", "b": "2", "shared": "new"},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			before, err := utils.DeepCopy(tc.base)
			if err != nil {
				t.Fatalf("utils.DeepCopy() error = %v, want nil", err)
			}

			got, err := a2aevent.ApplyUpdate(tc.base, tc.event)
			if err != nil {
				t.Fatalf("a2aevent.ApplyUpdate() error = %v, want nil", err)
			}
			opts := []cmp.Option{cmpopts.IgnoreFields(a2a.TaskStatus{}, "Timestamp")}
			if diff := cmp.Diff(tc.want, got, opts...); diff != "" {
				t.Fatalf("a2aevent.ApplyUpdate() wrong result (-want +got) diff = %s", diff)
			}
			if diff := cmp.Diff(before, tc.base, opts...); diff != "" {
				t.Fatalf("input task was mutated (-before +after) diff = %s", diff)
			}
		})
	}
}

func makeTextParts(texts ...string) a2a.ContentParts {
	parts := make(a2a.ContentParts, len(texts))
	for i, text := range texts {
		parts[i] = a2a.NewTextPart(text)
	}
	return parts
}

func newTestTaskInfo() (a2a.TaskInfoProvider, a2a.TaskInfo) {
	tip := &a2a.Task{ID: a2a.NewTaskID(), ContextID: a2a.NewContextID()}
	return tip, tip.TaskInfo()
}

func newTask(tip a2a.TaskInfoProvider, state a2a.TaskState, artifacts ...*a2a.Artifact) *a2a.Task {
	return &a2a.Task{
		ID:        tip.TaskInfo().TaskID,
		ContextID: tip.TaskInfo().ContextID,
		Status:    a2a.TaskStatus{State: state},
		Artifacts: artifacts,
	}
}
