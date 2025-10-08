// Copyright 2025 The A2A Authors
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

package taskupdate

import (
	"context"
	"errors"
	"testing"

	"github.com/a2aproject/a2a-go/a2a"
)

func newTestTask() *a2a.Task {
	return &a2a.Task{ID: a2a.NewTaskID(), ContextID: a2a.NewContextID()}
}

func newStatusUpdate(task *a2a.Task) *a2a.TaskStatusUpdateEvent {
	return &a2a.TaskStatusUpdateEvent{TaskID: task.ID, ContextID: task.ContextID}
}

func getText(m *a2a.Message) string {
	return m.Parts[0].(a2a.TextPart).Text
}

type testSaver struct {
	saved *a2a.Task
	fail  error
}

func (s *testSaver) Save(ctx context.Context, task *a2a.Task) error {
	if s.fail != nil {
		return s.fail
	}
	s.saved = task
	return nil
}

func TestManager_TaskSaved(t *testing.T) {
	saver := &testSaver{}
	task := &a2a.Task{ID: a2a.NewTaskID(), ContextID: a2a.NewContextID()}
	m := NewManager(saver, task)

	newState := a2a.TaskStateCanceled
	updated := &a2a.Task{
		ID:        m.task.ID,
		ContextID: m.task.ContextID,
		Status:    a2a.TaskStatus{State: newState},
	}
	result, err := m.Process(t.Context(), updated)
	if err != nil {
		t.Fatalf("m.Process() failed to save task: %v", err)
	}

	if updated != saver.saved {
		t.Fatalf("task not saved: got = %v, want = %v", saver.saved, updated)
	}
	if updated != result {
		t.Fatalf("manager task not updated: got = %v, want = %v", result, updated)
	}
	if result.Status.State != newState {
		t.Fatalf("task state not updated: got = %v, want = %v", result.Status.State, newState)
	}
}

func TestManager_SaverError(t *testing.T) {
	saver := &testSaver{}
	m := NewManager(saver, newTestTask())

	wantErr := errors.New("saver failed")
	saver.fail = wantErr
	if _, err := m.Process(t.Context(), m.task); !errors.Is(err, wantErr) {
		t.Fatalf("m.Process() = %v, want %v", err, wantErr)
	}
}

func TestManager_StatusUpdate_StateChanges(t *testing.T) {
	saver := &testSaver{}
	m := NewManager(saver, newTestTask())
	m.task.Status = a2a.TaskStatus{State: a2a.TaskStateSubmitted}

	states := []a2a.TaskState{a2a.TaskStateWorking, a2a.TaskStateCompleted}
	for _, state := range states {
		event := newStatusUpdate(m.task)
		event.Status.State = state

		task, err := m.Process(t.Context(), event)
		if err != nil {
			t.Fatalf("m.Process() failed to set state %q: %v", state, err)
		}
		if task.Status.State != state {
			t.Fatalf("task state not updated: got = %v, want = %v", state, task.Status.State)
		}
	}
}

func TestManager_StatusUpdate_CurrentStatusBecomesHistory(t *testing.T) {
	saver := &testSaver{}
	m := NewManager(saver, newTestTask())

	var lastResult *a2a.Task
	messages := []string{"hello", "world", "foo", "bar"}
	for i, msg := range messages {
		event := newStatusUpdate(m.task)
		textPart := a2a.TextPart{Text: msg}
		event.Status.Message = a2a.NewMessage(a2a.MessageRoleAgent, textPart)

		result, err := m.Process(t.Context(), event)
		if err != nil {
			t.Fatalf("m.Process() failed to set status %d-th time: %v", i, err)
		}
		lastResult = result
	}

	status := getText(lastResult.Status.Message)
	if status != messages[len(messages)-1] {
		t.Fatalf("wrong status text: got = %q, want = %q", status, messages[len(messages)-1])
	}
	if len(lastResult.History) != len(messages)-1 {
		t.Fatalf("wrong history length: got = %d, want = %d", len(lastResult.History), len(messages)-1)
	}
	for i, msg := range lastResult.History {
		if getText(msg) != messages[i] {
			t.Fatalf("wrong history text: got = %q, want = %q", getText(msg), messages[i])
		}
	}
}

func TestManager_StatusUpdate_MetadataUpdated(t *testing.T) {
	saver := &testSaver{}
	m := NewManager(saver, newTestTask())

	updates := []map[string]any{
		{"foo": "bar"},
		{"foo": "bar2", "hello": "world"},
		{"one": "two"},
	}

	var lastResult *a2a.Task
	for i, metadata := range updates {
		event := newStatusUpdate(m.task)
		event.Metadata = metadata

		result, err := m.Process(t.Context(), event)
		if err != nil {
			t.Fatalf("m.Process() failed to set %d-th metadata: %v", i, err)
		}
		lastResult = result
	}

	got := lastResult.Metadata
	want := map[string]any{"foo": "bar2", "one": "two", "hello": "world"}
	if len(got) != len(want) {
		t.Fatalf("wrong metadata size: got = %d, want = %d", len(got), len(want))
	}
	for k, v := range got {
		if v != want[k] {
			t.Fatalf("wrong metadata kv: got = %s=%s, want %s=%s", k, v, k, want[k])
		}
	}
}

func TestManager_IDValidationFailure(t *testing.T) {
	task := &a2a.Task{ID: a2a.NewTaskID(), ContextID: a2a.NewContextID()}
	m := NewManager(&testSaver{}, task)

	testCases := []a2a.Event{
		&a2a.Task{ID: task.ID + "1", ContextID: task.ContextID},
		&a2a.Task{ID: task.ID, ContextID: task.ContextID + "1"},
		&a2a.Task{ID: "", ContextID: task.ContextID},
		&a2a.Task{ID: task.ID, ContextID: ""},

		&a2a.TaskStatusUpdateEvent{TaskID: task.ID + "1", ContextID: task.ContextID},
		&a2a.TaskStatusUpdateEvent{TaskID: task.ID, ContextID: task.ContextID + "1"},
		&a2a.TaskStatusUpdateEvent{TaskID: "", ContextID: task.ContextID},
		&a2a.TaskStatusUpdateEvent{TaskID: task.ID, ContextID: ""},

		&a2a.TaskArtifactUpdateEvent{TaskID: task.ID + "1", ContextID: task.ContextID},
		&a2a.TaskArtifactUpdateEvent{TaskID: task.ID, ContextID: task.ContextID},
		&a2a.TaskArtifactUpdateEvent{TaskID: "", ContextID: task.ContextID},
		&a2a.TaskArtifactUpdateEvent{TaskID: task.ID, ContextID: ""},
	}

	for _, event := range testCases {
		if _, err := m.Process(t.Context(), event); err == nil {
			t.Fatalf("expected ID validation to fail")
		}
	}
}
