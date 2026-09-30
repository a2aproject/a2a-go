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

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aevent"
	"github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"
	"github.com/a2aproject/a2a-go/v2/internal/testutil"
	"github.com/a2aproject/a2a-go/v2/internal/utils"
	"github.com/google/go-cmp/cmp"
)

func newSubmittedTask() *a2a.Task {
	return &a2a.Task{
		ID:        a2a.NewTaskID(),
		ContextID: a2a.NewContextID(),
		Status:    a2a.TaskStatus{State: a2a.TaskStateSubmitted},
	}
}

func getText(m *a2a.Message) string {
	return m.Parts[0].Text()
}

func makeTextParts(texts ...string) a2a.ContentParts {
	result := make(a2a.ContentParts, len(texts))
	for i, text := range texts {
		result[i] = a2a.NewTextPart(text)
	}
	return result
}

func newUpdater(t *testing.T, task *a2a.Task) (*Manager, *testutil.TestTaskStore) {
	t.Helper()
	saver := testutil.NewTestTaskStore()
	m, err := NewManager(saver, task.TaskInfo(), nil, taskstore.NewFullUpdateMaterializer(a2aevent.ApplyShallowUpdate))
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	return m, saver
}

func newUpdaterWithStoredTask(t *testing.T, task *a2a.Task) (*Manager, *testutil.TestTaskStore) {
	t.Helper()
	saver := testutil.NewTestTaskStore().WithTasks(t, task)
	m, err := NewManager(saver, task.TaskInfo(), saver.MustGet(t, task.ID), taskstore.NewFullUpdateMaterializer(a2aevent.ApplyShallowUpdate))
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	return m, saver
}

func mustProcess(t *testing.T, m *Manager, e a2a.Event) taskstore.TaskVersion {
	t.Helper()
	version, err := m.Process(t.Context(), e)
	if err != nil {
		t.Fatalf("m.Process() failed to save task: %v", err)
	}
	return version
}

func TestManager_TaskSaved(t *testing.T) {
	ctx := t.Context()

	task := newSubmittedTask()
	m, saver := newUpdater(t, task)
	if _, err := saver.Get(ctx, task.ID); !errors.Is(err, a2a.ErrTaskNotFound) {
		t.Fatalf("saver.Get() error = %v, want %v", err, a2a.ErrTaskNotFound)
	}

	version := mustProcess(t, m, task)

	stored := saver.MustGet(t, task.ID)
	if diff := cmp.Diff(stored.Task, task); diff != "" {
		t.Fatalf("wrong saved task state (-want +got):\n%s", diff)
	}
	if stored.Version != version {
		t.Fatalf("got wrong version %v, want %v", version, stored.Version)
	}
}

func TestManager_TaskImmutableAfterSave(t *testing.T) {
	task := newSubmittedTask()
	m, _ := newUpdater(t, task)

	_ = mustProcess(t, m, task)
	_ = mustProcess(t, m, a2a.NewArtifactEvent(task, a2a.NewTextPart("foo")))
	event := a2a.NewStatusUpdateEvent(task, a2a.TaskStateWorking, nil)
	event.Metadata = map[string]any{"foo": "bar"}
	_ = mustProcess(t, m, event)

	if task.Status.State != a2a.TaskStateSubmitted {
		t.Fatalf("task state = %v, want %v", task.Status.State, a2a.TaskStateSubmitted)
	}
	if len(task.Artifacts) != 0 {
		t.Fatalf("task artifact length = %d, want empty", len(task.Artifacts))
	}
	if len(task.Metadata) != 0 {
		t.Fatalf("task metadata length = %d, want empty", len(task.Metadata))
	}
}

func TestManager_StatusUpdateImmutableAfterSave(t *testing.T) {
	key := "foo"
	task := newSubmittedTask()
	m, _ := newUpdaterWithStoredTask(t, task)

	event := a2a.NewStatusUpdateEvent(task, a2a.TaskStateWorking, nil)
	event.Metadata = map[string]any{key: "bar"}
	_ = mustProcess(t, m, event)

	event2 := a2a.NewStatusUpdateEvent(task, a2a.TaskStateWorking, nil)
	event2.Metadata = map[string]any{key: "baz"}
	_ = mustProcess(t, m, event2)

	if v := event.Metadata[key]; v != "bar" {
		t.Fatalf("event.Metadata changed got %q, want %q", v, "bar")
	}
}

func TestManager_ArtifactImmutableAfterSave(t *testing.T) {
	task := newSubmittedTask()
	m, _ := newUpdaterWithStoredTask(t, task)

	event := a2a.NewArtifactEvent(task, a2a.NewTextPart("hello"))
	_ = mustProcess(t, m, event)

	event2 := a2a.NewArtifactUpdateEvent(task, event.Artifact.ID, a2a.NewTextPart("world"))
	event2.Artifact.Metadata = map[string]any{"foo": "bar"}
	_ = mustProcess(t, m, event2)

	if l := len(event.Artifact.Parts); l != 1 {
		t.Fatalf("len(event.Artifact.Parts) = %d, want 1", l)
	}
	if l := len(event.Artifact.Metadata); l != 0 {
		t.Fatalf("len(event.Artifact.Parts) = %d, want 0", l)
	}
}

func TestManager_SaverError(t *testing.T) {
	task := newSubmittedTask()

	m, saver := newUpdater(t, task)
	wantCreateErr := errors.New("create failed")
	saver.CreateFunc = func(ctx context.Context, task *a2a.Task) (taskstore.TaskVersion, error) {
		return taskstore.TaskVersionMissing, wantCreateErr
	}
	if _, err := m.Process(t.Context(), task); !errors.Is(err, wantCreateErr) {
		t.Fatalf("m.Process() = %v, want %v", err, wantCreateErr)
	}

	m, saver = newUpdaterWithStoredTask(t, task)
	wantUpdateErr := errors.New("update failed")
	saver.UpdateFunc = func(ctx context.Context, req *taskstore.UpdateRequest) (taskstore.TaskVersion, error) {
		return taskstore.TaskVersionMissing, wantUpdateErr
	}
	if _, err := m.Process(t.Context(), task); !errors.Is(err, wantUpdateErr) {
		t.Fatalf("m.Process() = %v, want %v", err, wantUpdateErr)
	}
}

func TestManager_StatusUpdate_StateChanges(t *testing.T) {
	task := newSubmittedTask()
	m, saver := newUpdaterWithStoredTask(t, task)

	states := []a2a.TaskState{a2a.TaskStateWorking, a2a.TaskStateCompleted}
	for _, state := range states {
		_ = mustProcess(t, m, a2a.NewStatusUpdateEvent(task, state, nil))
		stored := saver.MustGet(t, task.ID)
		if stored.Task.Status.State != state {
			t.Fatalf("task state not updated: got = %v, want = %v", state, stored.Task.Status.State)
		}
	}
}

func TestManager_StatusUpdate_CurrentStatusBecomesHistory(t *testing.T) {
	task := newSubmittedTask()
	m, saver := newUpdaterWithStoredTask(t, task)

	messages := []string{"hello", "world", "foo", "bar"}
	for _, msg := range messages {
		event := a2a.NewStatusUpdateEvent(task, a2a.TaskStateWorking, a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart(msg)))
		_ = mustProcess(t, m, event)
	}

	stored := saver.MustGet(t, task.ID)
	status := getText(stored.Task.Status.Message)
	if status != messages[len(messages)-1] {
		t.Fatalf("wrong status text: got = %q, want = %q", status, messages[len(messages)-1])
	}
	if len(stored.Task.History) != len(messages)-1 {
		t.Fatalf("wrong history length: got = %d, want = %d", len(stored.Task.History), len(messages)-1)
	}
	for i, msg := range stored.Task.History {
		if getText(msg) != messages[i] {
			t.Fatalf("wrong history text: got = %q, want = %q", getText(msg), messages[i])
		}
	}
}

func TestManager_StatusUpdate_MetadataUpdated(t *testing.T) {
	task := newSubmittedTask()
	m, saver := newUpdaterWithStoredTask(t, task)

	updates := []map[string]any{
		{"foo": "bar"},
		{"foo": "bar2", "hello": "world"},
		{"one": "two"},
	}

	for _, metadata := range updates {
		event := a2a.NewStatusUpdateEvent(task, a2a.TaskStateWorking, nil)
		event.Metadata = metadata
		_ = mustProcess(t, m, event)
	}

	stored := saver.MustGet(t, task.ID)
	got := stored.Task.Metadata
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
					Artifact: &a2a.Artifact{ID: aid, Parts: makeTextParts("Hel")},
				},
				{
					Append: true,
					TaskID: tid, ContextID: ctxid,
					Artifact: &a2a.Artifact{ID: aid, Parts: makeTextParts("lo"), Metadata: map[string]any{"foo": "bar"}},
				},
			},
			want: []*a2a.Artifact{{ID: aid, Parts: makeTextParts("Hel", "lo"), Metadata: map[string]any{"foo": "bar"}}},
		},
		{
			name: "artifact updates metadata merged",
			events: []*a2a.TaskArtifactUpdateEvent{
				{
					TaskID: tid, ContextID: ctxid,
					Artifact: &a2a.Artifact{
						ID: aid, Parts: makeTextParts("Hel"),
						Metadata: map[string]any{"hello": "world", "1": "2"},
					},
				},
				{
					Append: true,
					TaskID: tid, ContextID: ctxid,
					Artifact: &a2a.Artifact{
						ID: aid, Parts: makeTextParts("lo"),
						Metadata: map[string]any{"foo": "bar", "1": "3"},
					},
				},
			},
			want: []*a2a.Artifact{{
				ID: aid, Parts: makeTextParts("Hel", "lo"),
				Metadata: map[string]any{"hello": "world", "foo": "bar", "1": "3"},
			}},
		},
		{
			name: "multiple parts in an update",
			events: []*a2a.TaskArtifactUpdateEvent{
				{
					TaskID: tid, ContextID: ctxid,
					Artifact: &a2a.Artifact{ID: aid, Parts: a2a.ContentParts{
						a2a.NewTextPart("1"),
						a2a.NewTextPart("2"),
					}},
				},
				{
					Append: true,
					TaskID: tid, ContextID: ctxid,
					Artifact: &a2a.Artifact{ID: aid, Parts: a2a.ContentParts{
						a2a.NewFileURLPart(a2a.URL("ftp://..."), ""),
						a2a.NewDataPart(map[string]any{"meta": 42}),
					}},
				},
			},
			want: []*a2a.Artifact{{ID: aid, Parts: a2a.ContentParts{
				a2a.NewTextPart("1"),
				a2a.NewTextPart("2"),
				a2a.NewFileURLPart(a2a.URL("ftp://..."), ""),
				a2a.NewDataPart(map[string]any{"meta": 42}),
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
			task := &a2a.Task{ID: tid, ContextID: ctxid}
			m, saver := newUpdaterWithStoredTask(t, task)

			var gotErr error
			var lastVersion taskstore.TaskVersion
			for _, ev := range tc.events {
				version, err := m.Process(t.Context(), ev)
				if err != nil {
					gotErr = err
					break
				}
				if !version.After(lastVersion) {
					t.Fatalf("event.version <= prevEvent.version, want increasing, got %v, want %v", version, lastVersion)
				}
				lastVersion = version
			}
			if tc.wantErr != (gotErr != nil) {
				t.Errorf("error = %v, want error = %v", gotErr, tc.wantErr)
			}
			if tc.wantErr {
				return
			}

			got := saver.MustGet(t, task.ID).Task.Artifacts
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("wrong artifacts saved (-want +got)\ngot = %v\nwant = %v\ndiff=%s", got, tc.want, diff)
			}
		})
	}
}

func TestManager_IDValidationFailure(t *testing.T) {
	task := newSubmittedTask()
	m, _ := newUpdater(t, task)

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

func TestManager_InvalidAgentResponse(t *testing.T) {
	taskID, contextID := a2a.NewTaskID(), a2a.NewContextID()
	taskInfo := a2a.TaskInfo{TaskID: taskID, ContextID: contextID}
	testCases := []struct {
		name            string
		storedTask      bool
		storedTaskState a2a.TaskState
		event           a2a.Event
		wantErrContain  string
	}{
		{
			name:           "artifact update before task snapshot",
			storedTask:     false,
			event:          a2a.NewArtifactEvent(taskInfo, a2a.NewTextPart("hi")),
			wantErrContain: "first event must be a Task or a message",
		},
		{
			name:           "status update before task snapshot",
			storedTask:     false,
			event:          a2a.NewStatusUpdateEvent(taskInfo, a2a.TaskStateSubmitted, nil),
			wantErrContain: "first event must be a Task or a message",
		},
		{
			name:           "artifact with empty part",
			storedTask:     true,
			event:          a2a.NewArtifactEvent(taskInfo),
			wantErrContain: "artifact cannot be empty",
		},
		{
			name:           "message in the task lifecycle",
			storedTask:     true,
			event:          a2a.NewMessageForTask(a2a.MessageRoleAgent, taskInfo),
			wantErrContain: "message not allowed after task was stored",
		},
		{
			name:            "completed task update not allowed",
			storedTask:      true,
			storedTaskState: a2a.TaskStateCompleted,
			event:           a2a.NewArtifactEvent(taskInfo, a2a.NewTextPart("hi")),
			wantErrContain:  fmt.Sprintf("%q task state updates are not allowed", a2a.TaskStateCompleted),
		},
		{
			name:            "canceled task update not allowed",
			storedTask:      true,
			storedTaskState: a2a.TaskStateCanceled,
			event:           a2a.NewArtifactEvent(taskInfo, a2a.NewTextPart("hi")),
			wantErrContain:  fmt.Sprintf("%q task state updates are not allowed", a2a.TaskStateCanceled),
		},
		{
			name:            "failed task update not allowed",
			storedTask:      true,
			storedTaskState: a2a.TaskStateFailed,
			event:           a2a.NewArtifactEvent(taskInfo, a2a.NewTextPart("hi")),
			wantErrContain:  fmt.Sprintf("%q task state updates are not allowed", a2a.TaskStateFailed),
		},
		{
			name:            "rejected task update not allowed",
			storedTask:      true,
			storedTaskState: a2a.TaskStateRejected,
			event:           a2a.NewArtifactEvent(taskInfo, a2a.NewTextPart("hi")),
			wantErrContain:  fmt.Sprintf("%q task state updates are not allowed", a2a.TaskStateRejected),
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			task := &a2a.Task{ID: taskID, ContextID: contextID, Status: a2a.TaskStatus{State: tc.storedTaskState}}
			var manager *Manager
			if tc.storedTask {
				manager, _ = newUpdaterWithStoredTask(t, task)
			} else {
				manager, _ = newUpdater(t, task)
			}
			_, err := manager.Process(t.Context(), tc.event)
			if err == nil {
				t.Fatal("manager.Process() error = nil, want non-nil")
			}
			if !errors.Is(err, a2a.ErrInvalidAgentResponse) {
				t.Fatalf("manager.Process() error = %q, want %q", err, a2a.ErrInvalidAgentResponse)
			}
			if !strings.Contains(err.Error(), tc.wantErrContain) {
				t.Fatalf("manager.Process() error = %q, want to contain %q", err.Error(), tc.wantErrContain)
			}
		})
	}
}

func TestManager_SetTaskFailedAfterInvalidUpdate(t *testing.T) {
	seedTask := newSubmittedTask()
	invalidMeta := map[string]any{"invalid": func() {}}

	testCases := []struct {
		name          string
		invalidUpdate a2a.Event
	}{
		{
			name: "task update",
			invalidUpdate: &a2a.Task{
				ID:        seedTask.ID,
				ContextID: seedTask.ContextID,
				Metadata:  invalidMeta,
			},
		},
		{
			name: "artifact update",
			invalidUpdate: &a2a.TaskArtifactUpdateEvent{
				TaskID:    seedTask.ID,
				ContextID: seedTask.ContextID,
				Artifact: &a2a.Artifact{
					ID:       a2a.NewArtifactID(),
					Parts:    []*a2a.Part{a2a.NewTextPart("hi")},
					Metadata: invalidMeta,
				},
			},
		},
		{
			name: "task status update",
			invalidUpdate: &a2a.TaskStatusUpdateEvent{
				TaskID:    seedTask.ID,
				ContextID: seedTask.ContextID,
				Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
				Metadata:  invalidMeta,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()

			m, store := newUpdaterWithStoredTask(t, seedTask)

			_, err := m.Process(ctx, tc.invalidUpdate)
			if err == nil {
				t.Fatalf("m.Process() error = nil, expected serialization failure")
			}
			if err := m.SetTaskFailed(ctx); err != nil {
				t.Fatalf("m.SetTaskFailed() error = %v, want nil", err)
			}

			stored := store.MustGet(t, seedTask.ID)
			if stored.Task.Status.State != a2a.TaskStateFailed {
				t.Errorf("task.Status.State = %q, want %q", stored.Task.Status.State, a2a.TaskStateFailed)
			}
		})
	}
}

func TestManager_PartialMaterializer_TerminalStateUpdateNotAllowed(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name          string
		terminalEvent func(task *a2a.Task) a2a.Event
	}{
		{
			name: "status update",
			terminalEvent: func(task *a2a.Task) a2a.Event {
				return a2a.NewStatusUpdateEvent(task, a2a.TaskStateCompleted, nil)
			},
		},
		{
			name: "task snapshot",
			terminalEvent: func(task *a2a.Task) a2a.Event {
				return &a2a.Task{ID: task.ID, ContextID: task.ContextID, Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}}
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			task := newSubmittedTask()
			store := testutil.NewTestTaskStore()
			m, err := NewManager(store, task.TaskInfo(), nil, taskstore.NewNoOpUpdateMaterializer())
			if err != nil {
				t.Fatalf("NewManager() error = %v", err)
			}
			mustProcess(t, m, task)
			mustProcess(t, m, tc.terminalEvent(task))

			_, err = m.Process(t.Context(), a2a.NewStatusUpdateEvent(task, a2a.TaskStateWorking, nil))
			if !errors.Is(err, a2a.ErrInvalidAgentResponse) {
				t.Fatalf("m.Process() error = %v, want %v", err, a2a.ErrInvalidAgentResponse)
			}
		})
	}
}

func TestManager_PartialMaterializer_SetTaskFailedUsesTaskInfo(t *testing.T) {
	t.Parallel()
	task := newSubmittedTask()
	store := testutil.NewTestTaskStore()
	var gotEvent a2a.Event
	store.UpdateFunc = func(ctx context.Context, req *taskstore.UpdateRequest) (taskstore.TaskVersion, error) {
		gotEvent = req.Event
		return req.PrevVersion + 1, nil
	}
	emptyTask := func(*a2a.Task, a2a.Event) (*a2a.Task, error) { return &a2a.Task{}, nil }
	m, err := NewManager(store, task.TaskInfo(), nil, taskstore.NewPartialUpdateMaterializer(emptyTask))
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	mustProcess(t, m, task)
	mustProcess(t, m, a2a.NewStatusUpdateEvent(task, a2a.TaskStateWorking, nil))

	if err := m.SetTaskFailed(t.Context()); err != nil {
		t.Fatalf("m.SetTaskFailed() error = %v", err)
	}

	event, ok := gotEvent.(*a2a.TaskStatusUpdateEvent)
	if !ok {
		t.Fatalf("store.Update() event = %T, want *a2a.TaskStatusUpdateEvent", gotEvent)
	}
	if diff := cmp.Diff(task.TaskInfo(), event.TaskInfo()); diff != "" {
		t.Fatalf("m.SetTaskFailed() wrong event task info (-want +got) diff = %s", diff)
	}
	if event.Status.State != a2a.TaskStateFailed {
		t.Fatalf("m.SetTaskFailed() event state = %q, want %q", event.Status.State, a2a.TaskStateFailed)
	}
}

func TestManager_CancelationStatusUpdate_RetryOnConcurrentModification(t *testing.T) {
	tid, ctxID := a2a.NewTaskID(), a2a.NewContextID()
	taskInfo := a2a.TaskInfo{TaskID: tid, ContextID: ctxID}
	testCases := []struct {
		name           string
		initialState   taskstore.StoredTask
		statusUpdate   *a2a.TaskStatusUpdateEvent
		firstUpdateErr error
		getResult      *a2a.Task
		wantResult     *taskstore.StoredTask
		wantErrContain string
	}{
		{
			name: "concurrent update and task is non-terminal - retry succeeds",
			initialState: taskstore.StoredTask{
				Task:    &a2a.Task{Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}},
				Version: 1,
			},
			statusUpdate: &a2a.TaskStatusUpdateEvent{
				TaskID: tid, ContextID: ctxID,
				Status:   a2a.TaskStatus{State: a2a.TaskStateCanceled},
				Metadata: map[string]any{"hello": "world"},
			},
			firstUpdateErr: taskstore.ErrConcurrentModification,
			getResult: &a2a.Task{
				Status:   a2a.TaskStatus{State: a2a.TaskStateWorking},
				Metadata: map[string]any{"foo": "bar"},
			},
			wantResult: &taskstore.StoredTask{
				Task: &a2a.Task{
					Status:   a2a.TaskStatus{State: a2a.TaskStateCanceled},
					Metadata: map[string]any{"foo": "bar", "hello": "world"},
				},
				Version: 3,
			},
		},
		{
			name:         "not concurrent update error - cancel fails",
			statusUpdate: a2a.NewStatusUpdateEvent(taskInfo, a2a.TaskStateCanceled, nil),
			initialState: taskstore.StoredTask{
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
			name:         "not cancelation - update fails",
			statusUpdate: a2a.NewStatusUpdateEvent(taskInfo, a2a.TaskStateWorking, nil),
			initialState: taskstore.StoredTask{
				Task:    &a2a.Task{Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}},
				Version: 1,
			},
			firstUpdateErr: taskstore.ErrConcurrentModification,
			wantErrContain: taskstore.ErrConcurrentModification.Error(),
		},
		{
			name:         "concurrent update and task is canceled - task returned as result",
			statusUpdate: a2a.NewStatusUpdateEvent(taskInfo, a2a.TaskStateCanceled, nil),
			initialState: taskstore.StoredTask{
				Task:    &a2a.Task{Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}},
				Version: 1,
			},
			firstUpdateErr: taskstore.ErrConcurrentModification,
			getResult: &a2a.Task{
				Status: a2a.TaskStatus{State: a2a.TaskStateCanceled},
			},
			wantResult: &taskstore.StoredTask{
				Task:    &a2a.Task{Status: a2a.TaskStatus{State: a2a.TaskStateCanceled}},
				Version: 2,
			},
		},
		{
			name:         "concurrent update and task in terminal state - fail",
			statusUpdate: a2a.NewStatusUpdateEvent(taskInfo, a2a.TaskStateCanceled, nil),
			initialState: taskstore.StoredTask{
				Task:    &a2a.Task{Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}},
				Version: 1,
			},
			firstUpdateErr: taskstore.ErrConcurrentModification,
			getResult: &a2a.Task{
				Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
			},
			wantErrContain: fmt.Sprintf("task moved to %q before it could be cancelled", a2a.TaskStateCompleted),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			task := *tc.initialState.Task
			task.ID = tid
			task.ContextID = ctxID

			m, saver := newUpdaterWithStoredTask(t, &task)

			if stored := saver.MustGet(t, task.ID); stored.Version != tc.initialState.Version {
				t.Fatalf("storedVersion = %v, want %v", stored.Version, tc.initialState.Version)
			}

			if tc.firstUpdateErr != nil {
				returned := false
				saver.UpdateFunc = func(ctx context.Context, req *taskstore.UpdateRequest) (taskstore.TaskVersion, error) {
					if returned {
						return saver.InMemory.Update(ctx, req)
					}
					returned = true
					if tc.getResult != nil {
						updated, _ := utils.DeepCopy(&task)
						updated.Status = tc.getResult.Status
						_, _ = saver.InMemory.Update(t.Context(), &taskstore.UpdateRequest{Task: updated})
					}
					return taskstore.TaskVersionMissing, tc.firstUpdateErr
				}
			}

			_, err := m.Process(t.Context(), tc.statusUpdate)
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
				stored := saver.MustGet(t, task.ID)
				if stored.Version != tc.wantResult.Version {
					t.Errorf("got version %d, want %d", stored.Version, tc.wantResult.Version)
				}
				if stored.Task.Status.State != tc.wantResult.Task.Status.State {
					t.Errorf("got state %q, want %q", stored.Task.Status.State, tc.wantResult.Task.Status.State)
				}
			}
		})
	}
}

func TestManager_SetTaskFailedAfterTerminalState(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name         string
		materializer taskstore.UpdateMaterializer
	}{
		{name: "full materializer", materializer: taskstore.NewFullUpdateMaterializer(a2aevent.ApplyShallowUpdate)},
		{name: "partial materializer", materializer: taskstore.NewNoOpUpdateMaterializer()},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			task := newSubmittedTask()
			store := testutil.NewTestTaskStore().WithTasks(t, task)
			m, err := NewManager(store, task.TaskInfo(), store.MustGet(t, task.ID), tc.materializer)
			if err != nil {
				t.Fatalf("NewManager() error = %v", err)
			}
			wantVersion := mustProcess(t, m, a2a.NewStatusUpdateEvent(task, a2a.TaskStateCompleted, nil))

			if err := m.SetTaskFailed(t.Context()); err == nil {
				t.Fatalf("m.SetTaskFailed() error = nil, want error")
			}

			stored := store.MustGet(t, task.ID)
			if stored.Version != wantVersion {
				t.Fatalf("store.Get() version = %v, want %v", stored.Version, wantVersion)
			}
		})
	}
}

func TestManager_UpdateRequestPrevTask(t *testing.T) {
	t.Parallel()
	task := newSubmittedTask()
	task.Metadata = map[string]any{"foo": "bar"}
	store := testutil.NewTestTaskStore().WithTasks(t, task)
	var requests []*taskstore.UpdateRequest
	store.UpdateFunc = func(ctx context.Context, req *taskstore.UpdateRequest) (taskstore.TaskVersion, error) {
		requests = append(requests, req)
		return store.InMemory.Update(ctx, req)
	}
	idOnly := func(task *a2a.Task, event a2a.Event) (*a2a.Task, error) {
		return &a2a.Task{ID: task.ID, ContextID: task.ContextID}, nil
	}
	m, err := NewManager(store, task.TaskInfo(), store.MustGet(t, task.ID), taskstore.NewPartialUpdateMaterializer(idOnly))
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	mustProcess(t, m, a2a.NewArtifactEvent(task, a2a.NewTextPart("foo")))
	mustProcess(t, m, a2a.NewStatusUpdateEvent(task, a2a.TaskStateWorking, nil))
	mustProcess(t, m, a2a.NewStatusUpdateEvent(task, a2a.TaskStateCompleted, nil))

	if len(requests) != 3 {
		t.Fatalf("store.Update() called %d times, want 3", len(requests))
	}
	if diff := cmp.Diff(task, requests[0].PrevTask); diff != "" {
		t.Fatalf("store.Update() wrong first PrevTask (-want +got) diff = %s", diff)
	}
	for i := 1; i < len(requests); i++ {
		if requests[i].PrevTask != requests[i-1].Task {
			t.Fatalf("store.Update() PrevTask = %v, want previous update Task %v", requests[i].PrevTask, requests[i-1].Task)
		}
	}
}

func TestManager_PartialMaterializer_CancelationRetryUsesStoredTask(t *testing.T) {
	t.Parallel()
	task := newSubmittedTask()
	store := testutil.NewTestTaskStore().WithTasks(t, task)
	var inputs []*a2a.Task
	idOnly := func(task *a2a.Task, event a2a.Event) (*a2a.Task, error) {
		inputs = append(inputs, task)
		return &a2a.Task{ID: task.ID, ContextID: task.ContextID}, nil
	}
	m, err := NewManager(store, task.TaskInfo(), store.MustGet(t, task.ID), taskstore.NewPartialUpdateMaterializer(idOnly))
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	concurrentTask := &a2a.Task{
		ID:        task.ID,
		ContextID: task.ContextID,
		Status:    a2a.TaskStatus{State: a2a.TaskStateWorking},
		Metadata:  map[string]any{"foo": "bar"},
	}
	conflicted := false
	store.UpdateFunc = func(ctx context.Context, req *taskstore.UpdateRequest) (taskstore.TaskVersion, error) {
		if conflicted {
			return store.InMemory.Update(ctx, req)
		}
		conflicted = true
		if _, err := store.InMemory.Update(ctx, &taskstore.UpdateRequest{Task: concurrentTask}); err != nil {
			return taskstore.TaskVersionMissing, err
		}
		return taskstore.TaskVersionMissing, taskstore.ErrConcurrentModification
	}

	mustProcess(t, m, a2a.NewStatusUpdateEvent(task, a2a.TaskStateCanceled, nil))

	wantInputs := []*a2a.Task{task, concurrentTask}
	if diff := cmp.Diff(wantInputs, inputs); diff != "" {
		t.Fatalf("materializer.ApplyUpdate() wrong input tasks (-want +got) diff = %s", diff)
	}
}
