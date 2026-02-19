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
	"fmt"
	"strings"
	"testing"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/internal/taskstore"
	"github.com/a2aproject/a2a-go/internal/utils"
	"github.com/google/go-cmp/cmp"
)

func newTestTask() *VersionedTask {
	return &VersionedTask{
		Task:    &a2a.Task{ID: a2a.NewTaskID(), ContextID: a2a.NewContextID()},
		Version: a2a.TaskVersionMissing,
	}
}

func getText(m *a2a.Message) string {
	return m.Parts[0].(a2a.TextPart).Text
}

type testSaver struct {
	saved      *a2a.Task
	version    a2a.TaskVersion
	versionSet bool
	fail       error
	failOnce   error
}

var _ Saver = (*testSaver)(nil)

func (s *testSaver) Get(ctx context.Context, taskID a2a.TaskID) (*a2a.Task, a2a.TaskVersion, error) {
	return s.saved, s.version, nil
}

func (s *testSaver) Save(ctx context.Context, task *a2a.Task, event a2a.Event, prev a2a.TaskVersion) (a2a.TaskVersion, error) {
	if s.failOnce != nil {
		err := s.failOnce
		s.failOnce = nil
		return a2a.TaskVersionMissing, err
	}
	if s.fail != nil {
		return a2a.TaskVersionMissing, s.fail
	}
	if s.versionSet {
		if prev != a2a.TaskVersionMissing && prev != s.version {
			return a2a.TaskVersionMissing, fmt.Errorf("version mismatch: prev=%v, current=%v", prev, s.version)
		}
	}
	s.version = s.version + 1
	s.saved = task
	return s.version, nil
}

func makeTextParts(texts ...string) a2a.ContentParts {
	result := make(a2a.ContentParts, len(texts))
	for i, text := range texts {
		result[i] = a2a.TextPart{Text: text}
	}
	return result
}

func TestManager_TaskSaved(t *testing.T) {
	saver := &testSaver{}
	task := newTestTask()
	m := NewManager(saver, task)

	newState := a2a.TaskStateCanceled
	updated := &a2a.Task{
		ID:        m.lastSaved.Task.ID,
		ContextID: m.lastSaved.Task.ContextID,
		Status:    a2a.TaskStatus{State: newState},
	}
	result, err := m.Process(t.Context(), updated)
	if err != nil {
		t.Fatalf("m.Process() failed to save task: %v", err)
	}

	if updated != saver.saved {
		t.Fatalf("task not saved: got = %v, want = %v", saver.saved, updated)
	}
	if updated != result.Task {
		t.Fatalf("manager task not updated: got = %v, want = %v", result, updated)
	}
	if result.Task.Status.State != newState {
		t.Fatalf("task state not updated: got = %v, want = %v", result.Task.Status.State, newState)
	}
}

func TestManager_SaverError(t *testing.T) {
	saver := &testSaver{}
	m := NewManager(saver, newTestTask())

	wantErr := errors.New("saver failed")
	saver.fail = wantErr
	if _, err := m.Process(t.Context(), m.lastSaved.Task); !errors.Is(err, wantErr) {
		t.Fatalf("m.Process() = %v, want %v", err, wantErr)
	}
}

func TestManager_StatusUpdate_StateChanges(t *testing.T) {
	saver := &testSaver{}
	m := NewManager(saver, newTestTask())
	m.lastSaved.Task.Status = a2a.TaskStatus{State: a2a.TaskStateSubmitted}

	states := []a2a.TaskState{a2a.TaskStateWorking, a2a.TaskStateCompleted}
	for _, state := range states {
		event := a2a.NewStatusUpdateEvent(m.lastSaved.Task, state, nil)

		versioned, err := m.Process(t.Context(), event)
		if err != nil {
			t.Fatalf("m.Process() failed to set state %q: %v", state, err)
		}
		if versioned.Task.Status.State != state {
			t.Fatalf("task state not updated: got = %v, want = %v", state, versioned.Task.Status.State)
		}
	}
}

func TestManager_StatusUpdate_CurrentStatusBecomesHistory(t *testing.T) {
	saver := &testSaver{}
	m := NewManager(saver, newTestTask())

	var lastResult *VersionedTask
	messages := []string{"hello", "world", "foo", "bar"}
	for i, msg := range messages {
		event := a2a.NewStatusUpdateEvent(m.lastSaved.Task, a2a.TaskStateWorking, a2a.NewMessage(a2a.MessageRoleAgent, a2a.TextPart{Text: msg}))

		versioned, err := m.Process(t.Context(), event)
		if err != nil {
			t.Fatalf("m.Process() failed to set status %d-th time: %v", i, err)
		}
		lastResult = versioned
	}

	status := getText(lastResult.Task.Status.Message)
	if status != messages[len(messages)-1] {
		t.Fatalf("wrong status text: got = %q, want = %q", status, messages[len(messages)-1])
	}
	if len(lastResult.Task.History) != len(messages)-1 {
		t.Fatalf("wrong history length: got = %d, want = %d", len(lastResult.Task.History), len(messages)-1)
	}
	for i, msg := range lastResult.Task.History {
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
		event := a2a.NewStatusUpdateEvent(m.lastSaved.Task, a2a.TaskStateWorking, nil)
		event.Metadata = metadata

		result, err := m.Process(t.Context(), event)
		if err != nil {
			t.Fatalf("m.Process() failed to set %d-th metadata: %v", i, err)
		}
		lastResult = result.Task
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

func TestManager_ArtifactUpdates(t *testing.T) {
	ctxid, tid, aid := a2a.NewContextID(), a2a.NewTaskID(), a2a.NewArtifactID()

	testCases := []struct {
		name    string
		events  []*a2a.TaskArtifactUpdateEvent
		want    []*a2a.Artifact
		wantErr bool
	}{
		{
			name: "create an artifact",
			events: []*a2a.TaskArtifactUpdateEvent{
				{
					TaskID: tid, ContextID: ctxid,
					Artifact: &a2a.Artifact{ID: aid, Parts: makeTextParts("Hello")},
				},
			},
			want: []*a2a.Artifact{{ID: aid, Parts: makeTextParts("Hello")}},
		},
		{
			name: "create multiple artifacts",
			events: []*a2a.TaskArtifactUpdateEvent{
				{
					TaskID: tid, ContextID: ctxid,
					Artifact: &a2a.Artifact{ID: aid, Parts: makeTextParts("Hello")},
				},
				{
					TaskID: tid, ContextID: ctxid,
					Artifact: &a2a.Artifact{ID: aid + "2", Parts: makeTextParts("World")},
				},
			},
			want: []*a2a.Artifact{
				{ID: aid, Parts: makeTextParts("Hello")},
				{ID: aid + "2", Parts: makeTextParts("World")},
			},
		},
		{
			name: "replace existing artifact",
			events: []*a2a.TaskArtifactUpdateEvent{
				{
					TaskID: tid, ContextID: ctxid,
					Artifact: &a2a.Artifact{ID: aid, Parts: makeTextParts("Hello")},
				},
				{
					TaskID: tid, ContextID: ctxid,
					Artifact: &a2a.Artifact{ID: aid, Parts: makeTextParts("World")},
				},
			},
			want: []*a2a.Artifact{{ID: aid, Parts: makeTextParts("World")}},
		},
		{
			name: "update existing artifact",
			events: []*a2a.TaskArtifactUpdateEvent{
				{
					TaskID: tid, ContextID: ctxid,
					Artifact: &a2a.Artifact{ID: aid, Parts: makeTextParts("Hello")},
				},
				{
					Append: true,
					TaskID: tid, ContextID: ctxid,
					Artifact: &a2a.Artifact{ID: aid, Parts: makeTextParts(", world!")},
				},
			},
			want: []*a2a.Artifact{{ID: aid, Parts: makeTextParts("Hello", ", world!")}},
		},
		{
			name: "update artifact metadata",
			events: []*a2a.TaskArtifactUpdateEvent{
				{
					TaskID: tid, ContextID: ctxid,
					Artifact: &a2a.Artifact{ID: aid},
				},
				{
					Append: true,
					TaskID: tid, ContextID: ctxid,
					Artifact: &a2a.Artifact{ID: aid, Metadata: map[string]any{"foo": "bar"}},
				},
			},
			want: []*a2a.Artifact{{ID: aid, Metadata: map[string]any{"foo": "bar"}}},
		},
		{
			name: "artifact updates metadata merged",
			events: []*a2a.TaskArtifactUpdateEvent{
				{
					TaskID: tid, ContextID: ctxid,
					Artifact: &a2a.Artifact{ID: aid, Metadata: map[string]any{"hello": "world", "1": "2"}},
				},
				{
					Append: true,
					TaskID: tid, ContextID: ctxid,
					Artifact: &a2a.Artifact{ID: aid, Metadata: map[string]any{"foo": "bar", "1": "3"}},
				},
			},
			want: []*a2a.Artifact{{ID: aid, Metadata: map[string]any{"hello": "world", "foo": "bar", "1": "3"}}},
		},
		{
			name: "multiple parts in an update",
			events: []*a2a.TaskArtifactUpdateEvent{
				{
					TaskID: tid, ContextID: ctxid,
					Artifact: &a2a.Artifact{ID: aid, Parts: a2a.ContentParts{
						a2a.TextPart{Text: "1"},
						a2a.TextPart{Text: "2"},
					}},
				},
				{
					Append: true,
					TaskID: tid, ContextID: ctxid,
					Artifact: &a2a.Artifact{ID: aid, Parts: a2a.ContentParts{
						a2a.FilePart{File: a2a.FileURI{URI: "ftp://..."}},
						a2a.DataPart{Data: map[string]any{"meta": 42}},
					}},
				},
			},
			want: []*a2a.Artifact{{ID: aid, Parts: a2a.ContentParts{
				a2a.TextPart{Text: "1"},
				a2a.TextPart{Text: "2"},
				a2a.FilePart{File: a2a.FileURI{URI: "ftp://..."}},
				a2a.DataPart{Data: map[string]any{"meta": 42}},
			}}},
		},
		{
			name: "multiple artifact updates",
			events: []*a2a.TaskArtifactUpdateEvent{
				{
					TaskID: tid, ContextID: ctxid,
					Artifact: &a2a.Artifact{ID: aid, Parts: makeTextParts("Hello")},
				},
				{
					Append: true,
					TaskID: tid, ContextID: ctxid,
					Artifact: &a2a.Artifact{ID: aid, Parts: makeTextParts(", world!")},
				},
				{
					Append: true,
					TaskID: tid, ContextID: ctxid,
					Artifact: &a2a.Artifact{ID: aid, Parts: makeTextParts("42")},
				},
			},
			want: []*a2a.Artifact{{ID: aid, Parts: makeTextParts("Hello", ", world!", "42")}},
		},
		{
			name: "interleaved artifact updates",
			events: []*a2a.TaskArtifactUpdateEvent{
				{
					TaskID: tid, ContextID: ctxid,
					Artifact: &a2a.Artifact{ID: aid, Parts: makeTextParts("Hello")},
				},
				{
					TaskID: tid, ContextID: ctxid,
					Artifact: &a2a.Artifact{ID: aid + "2", Parts: makeTextParts("Foo")},
				},
				{
					Append: true,
					TaskID: tid, ContextID: ctxid,
					Artifact: &a2a.Artifact{ID: aid, Parts: makeTextParts(", world!")},
				},
				{
					Append: true,
					TaskID: tid, ContextID: ctxid,
					Artifact: &a2a.Artifact{ID: aid + "2", Parts: makeTextParts("Bar")},
				},
			},
			want: []*a2a.Artifact{
				{ID: aid, Parts: makeTextParts("Hello", ", world!")},
				{ID: aid + "2", Parts: makeTextParts("Foo", "Bar")},
			},
		},
		{
			name: "fail on update of non-existent Artifact",
			events: []*a2a.TaskArtifactUpdateEvent{
				{
					Append: true,
					TaskID: tid, ContextID: ctxid,
					Artifact: &a2a.Artifact{Parts: makeTextParts("Hello")},
				},
			},
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			saver := &testSaver{}
			task := &a2a.Task{ID: tid, ContextID: ctxid}
			m := NewManager(saver, &VersionedTask{Task: task, Version: a2a.TaskVersionMissing})

			var gotErr error
			var lastResult *VersionedTask
			for _, ev := range tc.events {
				result, err := m.Process(t.Context(), ev)
				if err != nil {
					gotErr = err
					break
				}
				if lastResult != nil && !result.Version.After(lastResult.Version) {
					t.Fatalf("event.version <= prevEvent.version, want increasing, got %v, want %v", result.Version, lastResult.Version)
				}
				lastResult = result
			}
			if tc.wantErr != (gotErr != nil) {
				t.Errorf("error = %v, want error = %v", gotErr, tc.wantErr)
			}

			var saved []*a2a.Artifact
			if saver.saved != nil {
				saved = saver.saved.Artifacts
			}
			var got []*a2a.Artifact
			if lastResult != nil {
				got = lastResult.Task.Artifacts
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("wrong result (+got,-want)\ngot = %v\nwant = %v\ndiff=%s", got, tc.want, diff)
			}
			if diff := cmp.Diff(tc.want, saved); diff != "" {
				t.Errorf("wrong artifacts saved (+got,-want)\ngot = %v\nwant = %v\ndiff=%s", saved, tc.want, diff)
			}
		})
	}
}

func TestManager_IDValidationFailure(t *testing.T) {
	versioned := newTestTask()
	task := versioned.Task
	m := NewManager(&testSaver{}, versioned)

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
		&a2a.TaskArtifactUpdateEvent{TaskID: task.ID, ContextID: task.ContextID + "1"},
		&a2a.TaskArtifactUpdateEvent{TaskID: "", ContextID: task.ContextID},
		&a2a.TaskArtifactUpdateEvent{TaskID: task.ID, ContextID: ""},
	}

	for i, event := range testCases {
		if _, err := m.Process(t.Context(), event); err == nil {
			t.Fatalf("want ID validation to fail for %d-th event: %+v", i, event)
		}
	}
}

func TestManager_SetTaskFailedAfterInvalidUpdate(t *testing.T) {
	seedTask := newTestTask()
	invalidMeta := map[string]any{"invalid": func() {}}

	testCases := []struct {
		name          string
		invalidUpdate a2a.Event
	}{
		{
			name: "task update",
			invalidUpdate: &a2a.Task{
				ID:        seedTask.Task.ID,
				ContextID: seedTask.Task.ContextID,
				Metadata:  invalidMeta,
			},
		},
		{
			name: "artifact update",
			invalidUpdate: &a2a.TaskArtifactUpdateEvent{
				TaskID:    seedTask.Task.ID,
				ContextID: seedTask.Task.ContextID,
				Artifact: &a2a.Artifact{
					ID:       a2a.NewArtifactID(),
					Metadata: invalidMeta,
				},
			},
		},
		{
			name: "task status update",
			invalidUpdate: &a2a.TaskStatusUpdateEvent{
				TaskID:    seedTask.Task.ID,
				ContextID: seedTask.Task.ContextID,
				Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
				Metadata:  invalidMeta,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			store := taskstore.NewMem()

			m := NewManager(store, seedTask)
			_, err := m.Process(ctx, tc.invalidUpdate)
			if err == nil {
				t.Fatalf("m.Process() error = nil, expected serialization failure")
			}

			versioned, err := m.SetTaskFailed(ctx, tc.invalidUpdate, err)
			if err != nil {
				t.Fatalf("m.SetTaskFailed() error = %v, want nil", err)
			}
			if versioned.Task.Status.State != a2a.TaskStateFailed {
				t.Errorf("task.Status.State = %q, want %q", versioned.Task.Status.State, a2a.TaskStateFailed)
			}
		})
	}
}

func TestManager_CancelatioStatusUpdate_RetryOnConcurrentModification(t *testing.T) {
	testCases := []struct {
		name           string
		initialState   VersionedTask
		updateMetadata map[string]any
		firstUpdateErr error
		getResult      *a2a.Task
		wantResult     *VersionedTask
		wantErrContain string
	}{
		{
			name: "concurrent update and task is non-terminal - retry succeeds",
			initialState: VersionedTask{
				Task:    &a2a.Task{Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}},
				Version: 1,
			},
			updateMetadata: map[string]any{"hello": "world"},
			firstUpdateErr: a2a.ErrConcurrentTaskModification,
			getResult: &a2a.Task{
				Status:   a2a.TaskStatus{State: a2a.TaskStateWorking},
				Metadata: map[string]any{"foo": "bar"},
			},
			wantResult: &VersionedTask{
				Task: &a2a.Task{
					Status:   a2a.TaskStatus{State: a2a.TaskStateCanceled},
					Metadata: map[string]any{"foo": "bar", "hello": "world"},
				},
				Version: 3,
			},
		},
		{
			name: "not concurrent update error - cancel fails",
			initialState: VersionedTask{
				Task:    &a2a.Task{Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}},
				Version: 1,
			},
			firstUpdateErr: errors.New("db error"),
			getResult: &a2a.Task{
				Status: a2a.TaskStatus{State: a2a.TaskStateWorking},
			},
			wantErrContain: "db error",
		},
		{
			name: "concurrent update and task is canceled - task returned as result",
			initialState: VersionedTask{
				Task:    &a2a.Task{Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}},
				Version: 1,
			},
			firstUpdateErr: a2a.ErrConcurrentTaskModification,
			getResult: &a2a.Task{
				Status: a2a.TaskStatus{State: a2a.TaskStateCanceled},
			},
			wantResult: &VersionedTask{
				Task:    &a2a.Task{Status: a2a.TaskStatus{State: a2a.TaskStateCanceled}},
				Version: 2,
			},
		},
		{
			name: "concurrent update and task in terminal state - fail",
			initialState: VersionedTask{
				Task:    &a2a.Task{Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}},
				Version: 1,
			},
			firstUpdateErr: a2a.ErrConcurrentTaskModification,
			getResult: &a2a.Task{
				Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
			},
			wantErrContain: fmt.Sprintf("task moved to %q before it could be cancelled", a2a.TaskStateCompleted),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			saver := &testSaver{}
			base := newTestTask()

			task := &VersionedTask{Task: base.Task, Version: tc.initialState.Version}
			task.Task.Status = tc.initialState.Task.Status

			saver.saved = task.Task
			saver.version = task.Version
			saver.versionSet = true
			saver.failOnce = tc.firstUpdateErr

			m := NewManager(saver, task)

			if tc.getResult != nil {
				updated, _ := utils.DeepCopy(task.Task)
				updated.Status = tc.getResult.Status
				saver.saved = updated
				saver.version = 2
			}

			event := a2a.NewStatusUpdateEvent(m.lastSaved.Task, a2a.TaskStateCanceled, nil)
			event.Metadata = tc.updateMetadata

			versioned, err := m.Process(t.Context(), event)
			if tc.wantErrContain != "" {
				if err == nil {
					t.Fatalf("m.Process() expected error, got nil")
				}
				if !strings.Contains(err.Error(), tc.wantErrContain) {
					t.Fatalf("got error %q, want contain %q", err.Error(), tc.wantErrContain)
				}
				return
			}
			if err != nil {
				t.Fatalf("m.Process() unexpected error: %v", err)
			}

			if tc.wantResult != nil {
				if versioned.Version != tc.wantResult.Version {
					t.Errorf("got version %d, want %d", versioned.Version, tc.wantResult.Version)
				}
				if versioned.Task.Status.State != tc.wantResult.Task.Status.State {
					t.Errorf("got state %q, want %q", versioned.Task.Status.State, tc.wantResult.Task.Status.State)
				}
			}
		})
	}
}
