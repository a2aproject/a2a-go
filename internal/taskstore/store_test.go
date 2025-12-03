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
	"testing"

	"github.com/a2aproject/a2a-go/a2a"
)

func mustSave(t *testing.T, store *Mem, task *a2a.Task) {
	t.Helper()
	if err := store.Save(t.Context(), task); err != nil {
		t.Fatalf("Save() failed: %v", err)
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
		t.Fatalf("Unexpected error: got = %v, wanted ErrTaskNotFound", err)
	}
}

func getUserName(ctx context.Context) (string, bool) {
	return "testName", true
}

func TestInMemoryTaskStore_List_NoAuth(t *testing.T) {
	store := NewMem()
	_, err := store.List(t.Context(), &a2a.ListTasksRequest{})
	if err == nil {
		t.Fatalf("Unexpected error: got no error, wanted ErrAuthFailed")
	} else if !errors.Is(err, a2a.ErrAuthFailed) {
		t.Fatalf("Unexpected error: got = %v, wanted ErrAuthFailed", err)
	}
}

func TestInMemoryTaskStore_List_Basic(t *testing.T) {
	store := NewMem(WithAuthInfoProviderFn(getUserName))

	// Call List before saving any tasks
	emptyListResponse, err := store.List(t.Context(), &a2a.ListTasksRequest{})
	if err != nil {
		t.Fatalf("Unexpected error: got = %v, wanted nil", err)
	}
	if len(emptyListResponse.Tasks) != 0 {
		t.Fatalf("Unexpected list length: got = %v, wanted 0", len(emptyListResponse.Tasks))
	}

	task1 := &a2a.Task{ID: a2a.NewTaskID()}
	task2 := &a2a.Task{ID: a2a.NewTaskID()}
	task3 := &a2a.Task{ID: a2a.NewTaskID()}
	mustSave(t, store, task1)
	mustSave(t, store, task2)
	mustSave(t, store, task3)

	listResponse, err := store.List(t.Context(), &a2a.ListTasksRequest{})

	if err != nil {
		t.Fatalf("Unexpected error: got = %v, wanted nil", err)
	}
	if len(listResponse.Tasks) != 3 {
		t.Fatalf("Unexpected list length: got = %v, wanted 3", len(listResponse.Tasks))
	}
	if listResponse.Tasks[0].ID != task1.ID {
		t.Fatalf("Unexpected task ID: got = %v, wanted %v", listResponse.Tasks[0].ID, task1.ID)
	}
	if listResponse.Tasks[1].ID != task2.ID {
		t.Fatalf("Unexpected task ID: got = %v, wanted %v", listResponse.Tasks[1].ID, task2.ID)
	}
	if listResponse.Tasks[2].ID != task3.ID {
		t.Fatalf("Unexpected task ID: got = %v, wanted %v", listResponse.Tasks[2].ID, task3.ID)
	}
}

func TestInMemoryTaskStore_List_StoredImmutability(t *testing.T) {
	store := NewMem(WithAuthInfoProviderFn(getUserName))
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
	mustSave(t, store, task1)
	mustSave(t, store, task2)
	mustSave(t, store, task3)
	listResponse, err := store.List(t.Context(), &a2a.ListTasksRequest{
		IncludeArtifacts: true,
	})

	if err != nil {
		t.Fatalf("Unexpected error: got = %v, wanted nil", err)
	}

	listResponse.Tasks[0].ContextID = "modified-context-id"
	listResponse.Tasks[1].Status.State = a2a.TaskStateCompleted
	listResponse.Tasks[2].Artifacts[0].Name = "modified-artifact-name"

	newListResponse, err := store.List(t.Context(), &a2a.ListTasksRequest{
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("Unexpected error: got = %v, wanted nil", err)
	}
	if len(newListResponse.Tasks) != 3 {
		t.Fatalf("Unexpected list length: got = %v, wanted 3", len(newListResponse.Tasks))
	}
	if newListResponse.Tasks[0].ContextID != task1.ContextID {
		t.Fatalf("Unexpected task ID: got = %v, wanted %v", newListResponse.Tasks[0].ContextID, task1.ContextID)
	}
	if newListResponse.Tasks[1].ContextID != task2.ContextID {
		t.Fatalf("Unexpected task ID: got = %v, wanted %v", newListResponse.Tasks[1].ContextID, task2.ContextID)
	}
	if newListResponse.Tasks[2].ContextID != task3.ContextID {
		t.Fatalf("Unexpected task ID: got = %v, wanted %v", newListResponse.Tasks[2].ContextID, task3.ContextID)
	}

}

func TestInMemoryTaskStore_List_WithFilters(t *testing.T) {
	store := NewMem(WithAuthInfoProviderFn(getUserName))
	task1 := &a2a.Task{
		ID:        a2a.NewTaskID(),
		ContextID: "id1",
		Status:    a2a.TaskStatus{State: a2a.TaskStateWorking},
		Artifacts: []*a2a.Artifact{{Name: "foo"}},
		History: []*a2a.Message{
			{
				ContextID: "contextId1",
			},
			{
				ContextID: "contextId2",
			},
			{
				ContextID: "contextId3",
			},
		},
	}
	task2 := &a2a.Task{
		ID:        a2a.NewTaskID(),
		ContextID: "id2",
		Status:    a2a.TaskStatus{State: a2a.TaskStateCanceled},
		Artifacts: []*a2a.Artifact{{Name: "bar"}},
	}
	task3 := &a2a.Task{
		ID:        a2a.NewTaskID(),
		ContextID: "id3",
		Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
		Artifacts: []*a2a.Artifact{{Name: "baz"}},
	}
	mustSave(t, store, task1)
	mustSave(t, store, task2)
	mustSave(t, store, task3)

	list1Response, err := store.List(t.Context(), &a2a.ListTasksRequest{
		ContextID: "id1",
	})

	if err != nil {
		t.Fatalf("Unexpected error: got = %v, wanted nil", err)
	}
	if len(list1Response.Tasks) != 1 {
		t.Fatalf("Unexpected list length: got = %v, wanted 1", len(list1Response.Tasks))
	}
	for i := range list1Response.Tasks {
		if list1Response.Tasks[i].ContextID != task1.ContextID {
			t.Fatalf("Unexpected task ID: got = %v, wanted %v", list1Response.Tasks[i].ContextID, task1.ContextID)
		}
	}

	list2Response, err := store.List(t.Context(), &a2a.ListTasksRequest{
		Status: "canceled",
	})

	if err != nil {
		t.Fatalf("Unexpected error: got = %v, wanted nil", err)
	}
	if len(list2Response.Tasks) != 1 {
		t.Fatalf("Unexpected list length: got = %v, wanted 1", len(list2Response.Tasks))
	}
	for i := range list2Response.Tasks {
		if list2Response.Tasks[i].Status.State != task2.Status.State {
			t.Fatalf("Unexpected task ID: got = %v, wanted %v", list2Response.Tasks[i].Status.State, task2.Status.State)
		}
	}

	list3Response, err := store.List(t.Context(), &a2a.ListTasksRequest{
		HistoryLength: 2,
	})
	if err != nil {
		t.Fatalf("Unexpected error: got = %v, wanted nil", err)
	}
	for i := range list3Response.Tasks {
		if len(list3Response.Tasks[i].History) > 2 {
			t.Fatalf("Unexpected history length: got = %v, wanted maximum 2", len(list3Response.Tasks[i].History))
		}
	}

	var flag bool
	list4Response, err := store.List(t.Context(), &a2a.ListTasksRequest{
		IncludeArtifacts: flag,
	})
	if err != nil {
		t.Fatalf("Unexpected error: got = %v, wanted nil", err)
	}
	for i := range list4Response.Tasks {
		if flag {
			if list4Response.Tasks[i].Artifacts == nil {
				t.Fatalf("Unexpected artifacts: got = nil, wanted not nil")
			}
		} else {
			if list4Response.Tasks[i].Artifacts != nil {
				t.Fatalf("Unexpected artifacts: got = not nil, wanted nil")
			}
		}
	}
}
