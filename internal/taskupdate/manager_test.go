// Copyright 20\d\d The A2A Authors
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
		ID:        m.Task.ID,
		ContextID: m.Task.ContextID,
		Status:    a2a.TaskStatus{State: newState},
	}
	task.ID = m.Task.ID
	task.ContextID = m.Task.ContextID
	if err := m.Process(t.Context(), updated); err != nil {
		t.Fatalf("failed to save task: %v", err)
	}

	if updated != saver.saved {
		t.Fatalf("task not saved, want: %v, got: %v", updated, saver.saved)
	}
	if updated != m.Task {
		t.Fatalf("manager task not updated, want: %v, got: %v", updated, m.Task)
	}
	if m.Task.Status.State != newState {
		t.Fatalf("task state not updated, want: %v, got: %v", newState, m.Task.Status.State)
	}
}

func TestManager_SaverError(t *testing.T) {
	saver := &testSaver{}
	m := NewManager(saver, newTestTask())

	wantErr := errors.New("saver failed")
	saver.fail = wantErr
	if err := m.Process(t.Context(), m.Task); !errors.Is(err, wantErr) {
		t.Fatalf("want Process() to fail with %v, got %v", wantErr, err)
	}
}

func TestManager_StatusUpdate_StateChanges(t *testing.T) {
	saver := &testSaver{}
	m := NewManager(saver, newTestTask())
	m.Task.Status = a2a.TaskStatus{State: a2a.TaskStateSubmitted}

	states := []a2a.TaskState{a2a.TaskStateWorking, a2a.TaskStateCompleted}
	for _, state := range states {
		event := newStatusUpdate(m.Task)
		event.Status.State = state

		if err := m.Process(t.Context(), event); err != nil {
			t.Fatalf("Process() failed to set state %s: %v", state, err)
		}
		if m.Task.Status.State != state {
			t.Fatalf("task state not updated, want: %v, got: %v", state, m.Task.Status.State)
		}
	}
}

func TestManager_StatusUpdate_CurrentStatusBecomesHistory(t *testing.T) {
	saver := &testSaver{}
	m := NewManager(saver, newTestTask())

	messages := []string{"hello", "world", "foo", "bar"}
	for i, msg := range messages {
		event := newStatusUpdate(m.Task)
		textPart := a2a.TextPart{Text: msg}
		event.Status.Message = a2a.NewMessage(a2a.MessageRoleAgent, textPart)

		if err := m.Process(t.Context(), event); err != nil {
			t.Fatalf("Process() failed to set status %d-th time: %v", i, err)
		}
	}

	status := getText(m.Task.Status.Message)
	if status != messages[len(messages)-1] {
		t.Fatalf("want %s status text, got %s", messages[len(messages)-1], status)
	}
	if len(m.Task.History) != len(messages)-1 {
		t.Fatalf("want %d history messages, got %d", len(messages)-1, len(m.Task.History))
	}
	for i, msg := range m.Task.History {
		if getText(msg) != messages[i] {
			t.Fatalf("wanted %s history text, got %s", messages[i], getText(msg))
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

	for i, metadata := range updates {
		event := newStatusUpdate(m.Task)
		event.Metadata = metadata

		if err := m.Process(t.Context(), event); err != nil {
			t.Fatalf("Process() failed to set %d-th metadata: %v", i, err)
		}
	}

	got := m.Task.Metadata
	want := map[string]any{"foo": "bar2", "one": "two", "hello": "world"}
	if len(got) != len(want) {
		t.Fatalf("want %d metadata keys, got %d", len(want), len(got))
	}
	for k, v := range got {
		if v != want[k] {
			t.Fatalf("want %s=%s metadata keys, got %s=%s", k, want[k], k, v)
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
		if err := m.Process(t.Context(), event); err == nil {
			t.Fatalf("expected ID validation to fail")
		}
	}
}
