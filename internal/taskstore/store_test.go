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

package taskstore

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/google/go-cmp/cmp"
)

func mustSave(t *testing.T, store *Mem, tasks ...*a2a.Task) {
	t.Helper()
	for _, task := range tasks {
		if err := store.Save(t.Context(), task); err != nil {
			t.Fatalf("Save() failed: %v", err)
		}
	}
}

func mustGet(t *testing.T, store *Mem, id a2a.TaskID) *a2a.Task {
	t.Helper()
	got, err := store.Get(t.Context(), id)
	if err != nil {
		t.Fatalf("Get() failed: %v", err)
	}
	return got
}

func TestInMemoryTaskStore_GetSaved(t *testing.T) {
	store := NewMem()

	meta := map[string]any{"k1": 42, "k2": []any{1, 2, 3}}
	task := &a2a.Task{ID: a2a.NewTaskID(), ContextID: "id", Metadata: meta}
	mustSave(t, store, task)

	got := mustGet(t, store, task.ID)
	if task.ContextID != got.ContextID {
		t.Fatalf("Data mismatch: got = %v, want = %v", got, task)
	}
	if !reflect.DeepEqual(meta, got.Metadata) {
		t.Fatalf("Metadata mismatch: got = %v, want = %v", got.Metadata, meta)
	}
}

func TestInMemoryTaskStore_GetUpdated(t *testing.T) {
	store := NewMem()

	task := &a2a.Task{ID: a2a.NewTaskID(), ContextID: "id"}
	mustSave(t, store, task)

	task.ContextID = "id2"
	mustSave(t, store, task)

	got := mustGet(t, store, task.ID)
	if task.ContextID != got.ContextID {
		t.Fatalf("Data mismatch: got = %v, want = %v", task, got)
	}
}

func TestInMemoryTaskStore_StoredImmutability(t *testing.T) {
	store := NewMem()
	metaKey := "key"

	task := &a2a.Task{
		ID:        a2a.NewTaskID(),
		Status:    a2a.TaskStatus{State: a2a.TaskStateWorking},
		Artifacts: []*a2a.Artifact{{Name: "foo"}},
		Metadata:  make(map[string]any),
	}
	mustSave(t, store, task)

	task.Status = a2a.TaskStatus{State: a2a.TaskStateCompleted}
	task.Artifacts[0] = &a2a.Artifact{Name: "bar"}
	task.Metadata[metaKey] = fmt.Sprintf("%v", task.Metadata["new"]) + "-modified"

	got := mustGet(t, store, task.ID)
	if task.Status.State == got.Status.State {
		t.Fatalf("Unexpected status change: got = %v, want = TaskStateWorking", got.Status)
	}
	if task.Artifacts[0].Name == got.Artifacts[0].Name {
		t.Fatalf("Unexpected artifact change: got = %v, want = []*a2a.Artifact{{Name: foo}}", got.Artifacts)
	}
	if task.Metadata[metaKey] == got.Metadata[metaKey] {
		t.Fatalf("Unexpected metadata change: got = %v, want = empty map[string]any", got.Metadata)
	}
}

func TestInMemoryTaskStore_TaskNotFound(t *testing.T) {
	store := NewMem()

	_, err := store.Get(t.Context(), a2a.TaskID("invalid"))
	if !errors.Is(err, a2a.ErrTaskNotFound) {
		t.Fatalf("Unexpected error: got = %v, want ErrTaskNotFound", err)
	}
}

func getAuthInfo(ctx context.Context) (UserName, bool) {
	return "testName", true
}

var timeOffsetIndex int
var startTime = time.Date(2025, 12, 4, 15, 50, 0, 0, time.UTC)

func setFixedTime() time.Time {
	timeOffsetIndex++
	return startTime.Add(time.Duration(timeOffsetIndex) * time.Second)
}

func TestInMemoryTaskStore_List_NoAuth(t *testing.T) {
	store := NewMem()
	_, err := store.List(t.Context(), &a2a.ListTasksRequest{})
	if !errors.Is(err, a2a.ErrAuthFailed) {
		t.Fatalf("Unexpected error: got = %v, want ErrAuthFailed", err)
	}
}

func TestInMemoryTaskStore_List_Basic(t *testing.T) {
	store := NewMem(WithAuthenticator(getAuthInfo))

	// Call List before saving any tasks
	emptyListResponse, err := store.List(t.Context(), &a2a.ListTasksRequest{})
	if err != nil {
		t.Fatalf("Unexpected error: got = %v, want nil", err)
	}
	if len(emptyListResponse.Tasks) != 0 {
		t.Fatalf("Unexpected list length: got = %v, want 0", len(emptyListResponse.Tasks))
	}

	taskCount := 3
	tasks := make([]*a2a.Task, taskCount)
	for i := range taskCount {
		tasks[i] = &a2a.Task{ID: a2a.NewTaskID()}
	}
	mustSave(t, store, tasks...)

	listResponse, err := store.List(t.Context(), &a2a.ListTasksRequest{})

	if err != nil {
		t.Fatalf("Unexpected error: got = %v, want nil", err)
	}

	slices.Reverse(tasks)
	for i := range taskCount {
		if listResponse.Tasks[i].ID != tasks[i].ID {
			t.Fatalf("Unexpected task ID: got = %v, want %v", listResponse.Tasks[i].ID, tasks[i].ID)
		}
	}
}

func TestInMemoryTaskStore_List_StoredImmutability(t *testing.T) {
	store := NewMem(WithAuthenticator(getAuthInfo))
	task1 := &a2a.Task{
		ID:        a2a.NewTaskID(),
		ContextID: "id1",
		Status:    a2a.TaskStatus{State: a2a.TaskStateWorking},
		Artifacts: []*a2a.Artifact{{Name: "foo"}},
	}
	task2 := &a2a.Task{
		ID:        a2a.NewTaskID(),
		ContextID: "id2",
		Status:    a2a.TaskStatus{State: a2a.TaskStateWorking},
		Artifacts: []*a2a.Artifact{{Name: "bar"}},
	}
	task3 := &a2a.Task{
		ID:        a2a.NewTaskID(),
		ContextID: "id3",
		Status:    a2a.TaskStatus{State: a2a.TaskStateWorking},
		Artifacts: []*a2a.Artifact{{Name: "baz"}},
	}
	mustSave(t, store, task1, task2, task3)
	listResponse, err := store.List(t.Context(), &a2a.ListTasksRequest{
		IncludeArtifacts: true,
	})

	if err != nil {
		t.Fatalf("Unexpected error: got = %v, want nil", err)
	}

	listResponse.Tasks[0].ContextID = "modified-context-id"
	listResponse.Tasks[1].Status.State = a2a.TaskStateCompleted
	listResponse.Tasks[2].Artifacts[0].Name = "modified-artifact-name"

	newListResponse, err := store.List(t.Context(), &a2a.ListTasksRequest{
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("Unexpected error: got = %v, want nil", err)
	}
	if len(newListResponse.Tasks) != 3 {
		t.Fatalf("Unexpected list length: got = %v, want 3", len(newListResponse.Tasks))
	}
	if newListResponse.Tasks[0].ContextID != task3.ContextID {
		t.Fatalf("Unexpected task ID: got = %v, want %v", newListResponse.Tasks[2].ContextID, task1.ContextID)
	}
	if newListResponse.Tasks[1].Status.State != task2.Status.State {
		t.Fatalf("Unexpected task ID: got = %v, want %v", newListResponse.Tasks[1].Status.State, task2.Status.State)
	}
	if newListResponse.Tasks[2].Artifacts[0].Name != task1.Artifacts[0].Name {
		t.Fatalf("Unexpected task ID: got = %v, want %v", newListResponse.Tasks[2].Artifacts[0].Name, task1.Artifacts[0].Name)
	}
}

func TestInMemoryTaskStore_List_WithFilters(t *testing.T) {
	id1, id2, id3 := a2a.NewTaskID(), a2a.NewTaskID(), a2a.NewTaskID()
	cutoffTime := startTime.Add(2 * time.Second)
	testCases := []struct {
		name         string
		request      *a2a.ListTasksRequest
		givenTasks   []*a2a.Task
		wantResponse *a2a.ListTasksResponse
	}{
		{
			name:         "ContextID filter",
			request:      &a2a.ListTasksRequest{ContextID: "id1"},
			givenTasks:   []*a2a.Task{{ID: id1, ContextID: "id1"}, {ID: id2, ContextID: "id2"}},
			wantResponse: &a2a.ListTasksResponse{Tasks: []*a2a.Task{{ID: id1, ContextID: "id1"}}},
		},
		{
			name:         "Status filter",
			request:      &a2a.ListTasksRequest{Status: a2a.TaskStateCanceled},
			givenTasks:   []*a2a.Task{{ID: id1, Status: a2a.TaskStatus{State: a2a.TaskStateCanceled}}, {ID: id2, Status: a2a.TaskStatus{State: a2a.TaskStateWorking}}},
			wantResponse: &a2a.ListTasksResponse{Tasks: []*a2a.Task{{ID: id1, Status: a2a.TaskStatus{State: a2a.TaskStateCanceled}}}},
		},
		{
			name:         "LastUpdatedAfter filter",
			request:      &a2a.ListTasksRequest{LastUpdatedAfter: &cutoffTime},
			givenTasks:   []*a2a.Task{{ID: id1}, {ID: id2}, {ID: id3}},
			wantResponse: &a2a.ListTasksResponse{Tasks: []*a2a.Task{{ID: id3}, {ID: id2}}},
		},
		{
			name:         "HistoryLength filter",
			request:      &a2a.ListTasksRequest{HistoryLength: 2},
			givenTasks:   []*a2a.Task{{ID: id1, History: []*a2a.Message{{ID: "messageId1"}, {ID: "messageId2"}, {ID: "messageId3"}}}, {ID: id2, History: []*a2a.Message{{ID: "messageId4"}, {ID: "messageId5"}}}},
			wantResponse: &a2a.ListTasksResponse{Tasks: []*a2a.Task{{ID: id2, History: []*a2a.Message{{ID: "messageId4"}, {ID: "messageId5"}}}, {ID: id1, History: []*a2a.Message{{ID: "messageId2"}, {ID: "messageId3"}}}}},
		},
		{
			name:         "IncludeArtifacts true filter",
			request:      &a2a.ListTasksRequest{IncludeArtifacts: true},
			givenTasks:   []*a2a.Task{{ID: id1, Artifacts: []*a2a.Artifact{{Name: "foo"}}}, {ID: id2, Artifacts: []*a2a.Artifact{{Name: "bar"}}}, {ID: id3, Artifacts: []*a2a.Artifact{{Name: "baz"}}}},
			wantResponse: &a2a.ListTasksResponse{Tasks: []*a2a.Task{{ID: id3, Artifacts: []*a2a.Artifact{{Name: "baz"}}}, {ID: id2, Artifacts: []*a2a.Artifact{{Name: "bar"}}}, {ID: id1, Artifacts: []*a2a.Artifact{{Name: "foo"}}}}},
		},
		{
			name:         "IncludeArtifacts false filter",
			request:      &a2a.ListTasksRequest{IncludeArtifacts: false},
			givenTasks:   []*a2a.Task{{ID: id1, Artifacts: []*a2a.Artifact{{Name: "foo"}}}, {ID: id2, Artifacts: []*a2a.Artifact{{Name: "bar"}}}, {ID: id3, Artifacts: []*a2a.Artifact{{Name: "baz"}}}},
			wantResponse: &a2a.ListTasksResponse{Tasks: []*a2a.Task{{ID: id3}, {ID: id2}, {ID: id1}}},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			timeOffsetIndex = 0
			store := NewMem(WithAuthenticator(getAuthInfo), WithTimeProvider(setFixedTime))
			mustSave(t, store, tc.givenTasks...)

			listResponse, err := store.List(t.Context(), tc.request)

			if err != nil {
				t.Fatalf("Unexpected error: got = %v, want nil", err)
			}
			for i := range listResponse.Tasks {
				if diff := cmp.Diff(listResponse.Tasks[i], tc.wantResponse.Tasks[i]); diff != "" {
					t.Fatalf("Tasks mismatch (+got -want):\n%s", diff)
				}
			}
		})
	}
}
