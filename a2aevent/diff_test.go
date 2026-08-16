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
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aevent"
	"github.com/a2aproject/a2a-go/v2/internal/utils"
	"github.com/google/go-cmp/cmp"
)

func TestRecover(t *testing.T) {
	t.Parallel()

	tip, ti := newTestTaskInfo()
	ts := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	m1 := &a2a.Message{ID: "m1", Role: a2a.MessageRoleAgent, Parts: makeTextParts("first")}

	testCases := []struct {
		name string
		prev *a2a.Task
		curr *a2a.Task
		want []a2a.Event
	}{
		{
			name: "no diff",
			prev: newTask(tip, a2a.TaskStateWorking, &a2a.Artifact{ID: "a1", Parts: makeTextParts("Hello")}),
			curr: newTask(tip, a2a.TaskStateWorking, &a2a.Artifact{ID: "a1", Parts: makeTextParts("Hello")}),
			want: nil,
		},
		{
			name: "removed artifact no",
			prev: newTask(tip, a2a.TaskStateWorking, &a2a.Artifact{ID: "a1", Parts: makeTextParts("Hello")}),
			curr: newTask(tip, a2a.TaskStateWorking),
			want: nil,
		},
		{
			name: "new artifact",
			prev: newTask(tip, a2a.TaskStateWorking),
			curr: newTask(tip, a2a.TaskStateWorking, &a2a.Artifact{ID: "a1", Parts: makeTextParts("Hello")}),
			want: []a2a.Event{
				&a2a.TaskArtifactUpdateEvent{
					ContextID: ti.ContextID, TaskID: ti.TaskID,
					Artifact: &a2a.Artifact{ID: "a1", Parts: makeTextParts("Hello")},
				},
			},
		},
		{
			name: "artifact new parts",
			prev: newTask(tip, a2a.TaskStateWorking, &a2a.Artifact{ID: "a1", Parts: makeTextParts("Hello")}),
			curr: newTask(tip, a2a.TaskStateWorking, &a2a.Artifact{ID: "a1", Parts: makeTextParts("Hello", ", world!")}),
			want: []a2a.Event{
				&a2a.TaskArtifactUpdateEvent{
					Append:    true,
					ContextID: ti.ContextID, TaskID: ti.TaskID,
					Artifact: &a2a.Artifact{ID: "a1", Parts: makeTextParts(", world!")},
				},
			},
		},
		{
			name: "artifact changed parts",
			prev: newTask(tip, a2a.TaskStateWorking, &a2a.Artifact{ID: "a1", Parts: makeTextParts("Hello")}),
			curr: newTask(tip, a2a.TaskStateWorking, &a2a.Artifact{ID: "a1", Parts: makeTextParts("Goodbye")}),
			want: []a2a.Event{
				&a2a.TaskArtifactUpdateEvent{
					ContextID: ti.ContextID, TaskID: ti.TaskID,
					Artifact: &a2a.Artifact{ID: "a1", Parts: makeTextParts("Goodbye")},
				},
			},
		},
		{
			name: "artifact part removed",
			prev: newTask(tip, a2a.TaskStateWorking, &a2a.Artifact{ID: "a1", Parts: makeTextParts("Hello", "World")}),
			curr: newTask(tip, a2a.TaskStateWorking, &a2a.Artifact{ID: "a1", Parts: makeTextParts("Hello")}),
			want: []a2a.Event{
				&a2a.TaskArtifactUpdateEvent{
					ContextID: ti.ContextID, TaskID: ti.TaskID,
					Artifact: &a2a.Artifact{ID: "a1", Parts: makeTextParts("Hello")},
				},
			},
		},
		{
			name: "new artifact metadata and parts",
			prev: newTask(tip, a2a.TaskStateWorking, &a2a.Artifact{ID: "a1", Parts: makeTextParts("Hello"), Metadata: map[string]any{"a": "1"}}),
			curr: newTask(tip, a2a.TaskStateWorking, &a2a.Artifact{ID: "a1", Parts: makeTextParts("Hello", "more"), Metadata: map[string]any{"a": "1", "b": "2"}}),
			want: []a2a.Event{
				&a2a.TaskArtifactUpdateEvent{
					Append:    true,
					ContextID: ti.ContextID, TaskID: ti.TaskID,
					Artifact: &a2a.Artifact{ID: "a1", Parts: makeTextParts("more"), Metadata: map[string]any{"b": "2"}},
				},
			},
		},
		{
			name: "new artifact metadata",
			prev: newTask(tip, a2a.TaskStateWorking, &a2a.Artifact{
				ID: "a1", Parts: makeTextParts("Hello"), Metadata: map[string]any{"a": "1"},
			}),
			curr: newTask(tip, a2a.TaskStateWorking, &a2a.Artifact{
				ID: "a1", Parts: makeTextParts("World"), Metadata: map[string]any{"a": "1", "b": "2"},
			}),
			want: []a2a.Event{
				&a2a.TaskArtifactUpdateEvent{
					ContextID: ti.ContextID, TaskID: ti.TaskID,
					Artifact: &a2a.Artifact{
						ID:       "a1",
						Parts:    makeTextParts("World"), // artifact update can't be empty
						Metadata: map[string]any{"b": "2"},
					},
				},
			},
		},
		{
			name: "new state",
			prev: newTask(tip, a2a.TaskStateWorking),
			curr: newTask(tip, a2a.TaskStateInputRequired),
			want: []a2a.Event{
				&a2a.TaskStatusUpdateEvent{
					ContextID: ti.ContextID, TaskID: ti.TaskID,
					Status: a2a.TaskStatus{State: a2a.TaskStateInputRequired},
				},
			},
		},
		{
			name: "new status message",
			prev: newTask(tip, a2a.TaskStateWorking),
			curr: &a2a.Task{ContextID: ti.ContextID, ID: ti.TaskID, Status: a2a.TaskStatus{State: a2a.TaskStateWorking, Message: m1}},
			want: []a2a.Event{
				&a2a.TaskStatusUpdateEvent{
					ContextID: ti.ContextID, TaskID: ti.TaskID,
					Status: a2a.TaskStatus{State: a2a.TaskStateWorking, Message: m1},
				},
			},
		},
		{
			name: "status timestamp change",
			prev: newTask(tip, a2a.TaskStateWorking),
			curr: &a2a.Task{ContextID: ti.ContextID, ID: ti.TaskID, Status: a2a.TaskStatus{State: a2a.TaskStateInputRequired, Timestamp: &ts}},
			want: []a2a.Event{
				&a2a.TaskStatusUpdateEvent{
					ContextID: ti.ContextID, TaskID: ti.TaskID,
					Status: a2a.TaskStatus{State: a2a.TaskStateInputRequired, Timestamp: &ts},
				},
			},
		},
		{
			name: "task metadata change",
			prev: &a2a.Task{
				ContextID: ti.ContextID, ID: ti.TaskID,
				Status:   a2a.TaskStatus{State: a2a.TaskStateWorking},
				Metadata: map[string]any{"a": "1", "shared": "old"},
			},
			curr: &a2a.Task{
				ContextID: ti.ContextID, ID: ti.TaskID,
				Status:   a2a.TaskStatus{State: a2a.TaskStateWorking},
				Metadata: map[string]any{"a": "1", "shared": "new", "b": "2"},
			},
			want: []a2a.Event{
				&a2a.TaskStatusUpdateEvent{
					ContextID: ti.ContextID, TaskID: ti.TaskID,
					Status:   a2a.TaskStatus{State: a2a.TaskStateWorking},
					Metadata: map[string]any{"shared": "new", "b": "2"},
				},
			},
		},
		{
			name: "artifact and status updates",
			prev: newTask(tip, a2a.TaskStateWorking, &a2a.Artifact{ID: "a1", Parts: makeTextParts("Hello")}),
			curr: &a2a.Task{
				ContextID: ti.ContextID, ID: ti.TaskID,
				Status: a2a.TaskStatus{State: a2a.TaskStateInputRequired},
				Artifacts: []*a2a.Artifact{
					{ID: "a1", Parts: makeTextParts("Hello", "World")},
					{ID: "a2", Parts: makeTextParts("New")},
				},
				Metadata: map[string]any{"k": "v"},
			},
			want: []a2a.Event{
				&a2a.TaskArtifactUpdateEvent{
					Append:    true,
					ContextID: ti.ContextID, TaskID: ti.TaskID,
					Artifact: &a2a.Artifact{ID: "a1", Parts: makeTextParts("World")},
				},
				&a2a.TaskArtifactUpdateEvent{
					ContextID: ti.ContextID, TaskID: ti.TaskID,
					Artifact: &a2a.Artifact{ID: "a2", Parts: makeTextParts("New")},
				},
				&a2a.TaskStatusUpdateEvent{
					ContextID: ti.ContextID, TaskID: ti.TaskID,
					Status:   a2a.TaskStatus{State: a2a.TaskStateInputRequired},
					Metadata: map[string]any{"k": "v"},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			prevBefore, err := utils.DeepCopy(tc.prev)
			if err != nil {
				t.Fatalf("utils.DeepCopy() error = %v, want nil", err)
			}
			currBefore, err := utils.DeepCopy(tc.curr)
			if err != nil {
				t.Fatalf("utils.DeepCopy() error = %v, want nil", err)
			}

			got := a2aevent.Recover(tc.prev, tc.curr)

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("a2aevent.Recover() wrong result (-want +got) diff = %s", diff)
			}
			if diff := cmp.Diff(prevBefore, tc.prev); diff != "" { // input modified
				t.Fatalf("input task was mutated (-before +after) diff = %s", diff)
			}
			if diff := cmp.Diff(currBefore, tc.curr); diff != "" { // input modified
				t.Fatalf("input task was mutated (-before +after) diff = %s", diff)
			}
		})
	}
}

func TestRecover_ReappliedEventsReproduceState(t *testing.T) {
	t.Parallel()

	tip, ti := newTestTaskInfo()
	m1 := &a2a.Message{ID: "m1", Role: a2a.MessageRoleAgent, Parts: makeTextParts("first")}
	m2 := &a2a.Message{ID: "m2", Role: a2a.MessageRoleAgent, Parts: makeTextParts("second")}

	testCases := []struct {
		name string
		prev *a2a.Task
		curr *a2a.Task
	}{
		{
			name: "no changes",
			prev: newTask(tip, a2a.TaskStateWorking, &a2a.Artifact{ID: "a1", Parts: makeTextParts("Hello")}),
			curr: newTask(tip, a2a.TaskStateWorking, &a2a.Artifact{ID: "a1", Parts: makeTextParts("Hello")}),
		},
		{
			name: "all possible changes",
			prev: &a2a.Task{
				ID: ti.TaskID, ContextID: ti.ContextID,
				Status:   a2a.TaskStatus{State: a2a.TaskStateWorking},
				Metadata: map[string]any{"a": "1"},
			},
			curr: &a2a.Task{
				ID: ti.TaskID, ContextID: ti.ContextID,
				Status:    a2a.TaskStatus{State: a2a.TaskStateInputRequired},
				Artifacts: []*a2a.Artifact{{ID: "a1", Parts: makeTextParts("Hello")}},
				Metadata:  map[string]any{"a": "1", "b": "2"},
			},
		},
		{
			name: "append parts to an existing artifact",
			prev: newTask(tip, a2a.TaskStateWorking, &a2a.Artifact{ID: "a1", Parts: makeTextParts("Hello")}),
			curr: newTask(tip, a2a.TaskStateWorking, &a2a.Artifact{ID: "a1", Parts: makeTextParts("Hello", ", world!")}),
		},
		{
			name: "replace artifact parts",
			prev: newTask(tip, a2a.TaskStateWorking, &a2a.Artifact{ID: "a1", Parts: makeTextParts("Hello")}),
			curr: newTask(tip, a2a.TaskStateWorking, &a2a.Artifact{ID: "a1", Parts: makeTextParts("Goodbye")}),
		},
		{
			name: "status message move to history",
			prev: &a2a.Task{
				ID: ti.TaskID, ContextID: ti.ContextID,
				Status: a2a.TaskStatus{State: a2a.TaskStateWorking, Message: m1},
			},
			curr: &a2a.Task{
				ID: ti.TaskID, ContextID: ti.ContextID,
				Status:  a2a.TaskStatus{State: a2a.TaskStateWorking, Message: m2},
				History: []*a2a.Message{m1},
			},
		},
		{
			name: "multiple artifact and status updates",
			prev: newTask(tip, a2a.TaskStateWorking, &a2a.Artifact{ID: "a1", Parts: makeTextParts("Hello")}),
			curr: &a2a.Task{
				ID: ti.TaskID, ContextID: ti.ContextID,
				Status: a2a.TaskStatus{State: a2a.TaskStateInputRequired},
				Artifacts: []*a2a.Artifact{
					{ID: "a1", Parts: makeTextParts("Hello", "World")},
					{ID: "a2", Parts: makeTextParts("New")},
				},
				Metadata: map[string]any{"k": "v"},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			recovered := a2aevent.Recover(tc.prev, tc.curr)

			got, err := utils.DeepCopy(tc.prev)
			if err != nil {
				t.Fatalf("utils.DeepCopy() error = %v, want nil", err)
			}
			for _, event := range recovered {
				got, err = a2aevent.ApplyUpdate(got, event)
				if err != nil {
					t.Fatalf("a2aevent.ApplyUpdate() error = %v, want nil", err)
				}
			}
			if diff := cmp.Diff(tc.curr, got); diff != "" {
				t.Fatalf("re-applying a2aevent.Recover() events did not reproduce state (-want +got) diff = %s", diff)
			}
		})
	}
}
