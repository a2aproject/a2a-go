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

package a2atest_test

import (
	"iter"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2atest"
	"github.com/google/go-cmp/cmp"
)

func eventsSeq(events ...a2a.Event) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		for _, ev := range events {
			if !yield(ev, nil) {
				return
			}
		}
	}
}

func TestCollectEvents(t *testing.T) {
	t.Parallel()

	want := []a2a.Event{
		&a2a.Task{ID: "t1", Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}},
		a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("hello")),
		&a2a.TaskStatusUpdateEvent{TaskID: "t1", Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}},
	}
	got, err := a2atest.CollectEvents(eventsSeq(want...))
	if err != nil {
		t.Fatalf("CollectEvents() error = %v", err)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("CollectEvents() mismatch (-want +got):\n%s", diff)
	}
}

func TestCollectEvents_Empty(t *testing.T) {
	t.Parallel()
	got, err := a2atest.CollectEvents(eventsSeq())
	if err != nil {
		t.Fatalf("CollectEvents() error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("CollectEvents() len = %d, want 0", len(got))
	}
}

func TestFindEvent(t *testing.T) {
	t.Parallel()

	events := []a2a.Event{
		&a2a.Task{ID: "t1", Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}},
		a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("hello")),
		&a2a.TaskStatusUpdateEvent{TaskID: "t1", Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}},
	}
	got, err := a2atest.FindEvent(eventsSeq(events...), func(ev a2a.Event) bool {
		_, ok := ev.(*a2a.TaskStatusUpdateEvent)
		return ok
	})
	if err != nil {
		t.Fatalf("FindEvent() error = %v", err)
	}
	if _, ok := got.(*a2a.TaskStatusUpdateEvent); !ok {
		t.Fatalf("FindEvent() = %T, want *a2a.TaskStatusUpdateEvent", got)
	}
}

func TestFindEvent_NotFound(t *testing.T) {
	t.Parallel()

	events := []a2a.Event{
		a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("hello")),
	}
	got, err := a2atest.FindEvent(eventsSeq(events...), func(ev a2a.Event) bool {
		return false
	})
	if err != nil {
		t.Fatalf("FindEvent() error = %v", err)
	}
	if got != nil {
		t.Fatalf("FindEvent() = %v, want nil", got)
	}
}

func TestIsTerminal(t *testing.T) {
	t.Parallel()
	for _, state := range []a2a.TaskState{
		a2a.TaskStateCompleted, a2a.TaskStateFailed,
		a2a.TaskStateCanceled, a2a.TaskStateRejected,
	} {
		if !a2atest.IsTerminal(state) {
			t.Errorf("IsTerminal(%v) = false, want true", state)
		}
	}
	for _, state := range []a2a.TaskState{
		a2a.TaskStateSubmitted, a2a.TaskStateWorking,
		a2a.TaskStateInputRequired, a2a.TaskStateUnspecified,
	} {
		if a2atest.IsTerminal(state) {
			t.Errorf("IsTerminal(%v) = true, want false", state)
		}
	}
}

func TestFinalTaskState(t *testing.T) {
	t.Parallel()

	events := []a2a.Event{
		&a2a.Task{ID: "t1", Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}},
		a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("working...")),
		&a2a.TaskStatusUpdateEvent{TaskID: "t1", Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}},
	}
	state, task, err := a2atest.FinalTaskState(eventsSeq(events...))
	if err != nil {
		t.Fatalf("FinalTaskState() error = %v", err)
	}
	if state != a2a.TaskStateCompleted {
		t.Fatalf("FinalTaskState() state = %v, want %v", state, a2a.TaskStateCompleted)
	}
	if task == nil {
		t.Fatal("FinalTaskState() task = nil, want non-nil")
	}
	if task.ID != "t1" {
		t.Fatalf("FinalTaskState() task.ID = %s, want t1", task.ID)
	}
}
