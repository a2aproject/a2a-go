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
	"encoding/base64"
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
		_ = mustSaveVersioned(t, store, task, a2a.TaskVersionMissing)
	}
}

func mustSaveVersioned(t *testing.T, store *Mem, task *a2a.Task, prev a2a.TaskVersion) a2a.TaskVersion {
	t.Helper()
	version, err := store.Save(t.Context(), task, task, prev)
	if err != nil {
		t.Fatalf("Save() failed: %v", err)
	}
	return version
}

func mustGet(t *testing.T, store *Mem, id a2a.TaskID) *a2a.Task {
	t.Helper()
	got, _, err := store.Get(t.Context(), id)
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

	_, _, err := store.Get(t.Context(), a2a.TaskID("invalid"))
	if !errors.Is(err, a2a.ErrTaskNotFound) {
		t.Fatalf("Unexpected error: got = %v, want ErrTaskNotFound", err)
	}
}

func getAuthInfo(ctx context.Context) (UserName, bool) {
	return "testName", true
}

var startTime = time.Date(2025, 12, 4, 15, 50, 0, 0, time.UTC)

func newTimeProvider(startTime time.Time, offsets []int64) func() time.Time {
	return func() time.Time {
		current, rest := offsets[0], offsets[1:]
		offsets = rest
		return startTime.Add(time.Duration(current) * time.Second)
	}
}

func newIncreasingTimeProvider(startTime time.Time) func() time.Time {
	timeOffsetIndex := 0
	return func() time.Time {
		timeOffsetIndex++
		return startTime.Add(time.Duration(timeOffsetIndex) * time.Second)
	}
}

func TestInMemoryTaskStore_List_NoAuth(t *testing.T) {
	store := NewMem()
	_, err := store.List(t.Context(), &a2a.ListTasksRequest{})
	if !errors.Is(err, a2a.ErrUnauthenticated) {
		t.Fatalf("Unexpected error: got = %v, want ErrUnauthenticated", err)
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

func createPageToken(updatedTime time.Time, taskID a2a.TaskID) string {
	timeStrNano := updatedTime.Format(time.RFC3339Nano)
	return base64.URLEncoding.EncodeToString([]byte(fmt.Sprintf("%s_%s", timeStrNano, taskID)))
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
			name:         "PageSize filter",
			request:      &a2a.ListTasksRequest{PageSize: 2},
			givenTasks:   []*a2a.Task{{ID: id1}, {ID: id2}, {ID: id3}},
			wantResponse: &a2a.ListTasksResponse{Tasks: []*a2a.Task{{ID: id3}, {ID: id2}}},
		},
		{
			name:         "PageToken filter",
			request:      &a2a.ListTasksRequest{PageSize: 1, PageToken: createPageToken(startTime.Add(3*time.Second), id3)},
			givenTasks:   []*a2a.Task{{ID: id1}, {ID: id2}, {ID: id3}},
			wantResponse: &a2a.ListTasksResponse{Tasks: []*a2a.Task{{ID: id2}}},
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
			store := NewMem(WithAuthenticator(getAuthInfo), WithTimeProvider(newIncreasingTimeProvider(startTime)))
			mustSave(t, store, tc.givenTasks...)

			listResponse, err := store.List(t.Context(), tc.request)
			if err != nil {
				t.Fatalf("Unexpected error: got = %v, want nil", err)
			}
			if diff := cmp.Diff(listResponse.Tasks, tc.wantResponse.Tasks); diff != "" {
				t.Fatalf("Tasks mismatch (+got -want):\n%s", diff)
			}
		})
	}
}

func TestInMemoryTaskStore_List_Pagination(t *testing.T) {
	id1, id2, id3, id4, id5 := a2a.NewTaskID(), a2a.NewTaskID(), a2a.NewTaskID(), a2a.NewTaskID(), a2a.NewTaskID()
	testCases := []struct {
		name               string
		pageSize           int
		lastUpdatedOffsets []int64
		givenTasks         []*a2a.Task
		result             []*a2a.Task
		wantCalls          int
	}{
		{
			name:       "All tasks with incomplete last page",
			pageSize:   2,
			givenTasks: []*a2a.Task{{ID: id1}, {ID: id2}, {ID: id3}, {ID: id4}, {ID: id5}},
			result:     []*a2a.Task{{ID: id5}, {ID: id4}, {ID: id3}, {ID: id2}, {ID: id1}},
			wantCalls:  3,
		},
		{
			name:       "All tasks with complete last page",
			pageSize:   2,
			givenTasks: []*a2a.Task{{ID: id1}, {ID: id2}, {ID: id3}, {ID: id4}},
			result:     []*a2a.Task{{ID: id4}, {ID: id3}, {ID: id2}, {ID: id1}},
			wantCalls:  2,
		},
		{
			name:       "Page Size greater than number of tasks",
			pageSize:   10,
			givenTasks: []*a2a.Task{{ID: id1}, {ID: id2}, {ID: id3}, {ID: id4}, {ID: id5}},
			result:     []*a2a.Task{{ID: id5}, {ID: id4}, {ID: id3}, {ID: id2}, {ID: id1}},
			wantCalls:  1,
		},
		{
			name:       "Empty list",
			pageSize:   3,
			givenTasks: []*a2a.Task{},
			result:     []*a2a.Task{},
			wantCalls:  1,
		},
		{
			name:               "Same lastUpdated",
			pageSize:           2,
			lastUpdatedOffsets: []int64{0, 0, 0},
			givenTasks:         []*a2a.Task{{ID: id1}, {ID: id2}, {ID: id3}},
			result:             []*a2a.Task{{ID: id3}, {ID: id2}, {ID: id1}},
			wantCalls:          2,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			timeProvider := newIncreasingTimeProvider(startTime)
			if tc.lastUpdatedOffsets != nil {
				timeProvider = newTimeProvider(startTime, tc.lastUpdatedOffsets)
			}

			store := NewMem(WithAuthenticator(getAuthInfo), WithTimeProvider(timeProvider))
			mustSave(t, store, tc.givenTasks...)

			result := []*a2a.Task{}
			actualCalls := 0
			var pageToken string
			for {
				listResponse, err := store.List(t.Context(), &a2a.ListTasksRequest{PageSize: tc.pageSize, PageToken: pageToken})
				if err != nil {
					t.Fatalf("Unexpected error: got = %v, want nil", err)
				}
				result = append(result, listResponse.Tasks...)
				actualCalls++
				pageToken = listResponse.NextPageToken
				if pageToken == "" {
					break
				}
			}
			if diff := cmp.Diff(result, tc.result); diff != "" {
				t.Fatalf("Tasks mismatch (+got -want):\n%s", diff)
			}
			if actualCalls != tc.wantCalls {
				t.Fatalf("Unexpected number of calls: got = %v, want %v", actualCalls, tc.wantCalls)
			}
		})
	}
}

func TestInMemoryTaskStore_VersionIncrements(t *testing.T) {
	store := NewMem()

	task := &a2a.Task{ID: a2a.NewTaskID(), ContextID: "id"}
	v1 := mustSaveVersioned(t, store, task, a2a.TaskVersionMissing)

	task.ContextID = "id2"
	v2 := mustSaveVersioned(t, store, task, v1)

	if !v2.After(v1) {
		t.Fatalf("got v1 > v2: v1 = %v, v2 = %v", v1, v2)
	}
}

func TestInMemoryTaskStore_ConcurrentVersionIncrements(t *testing.T) {
	store := NewMem()

	task := &a2a.Task{ID: a2a.NewTaskID(), ContextID: "id"}

	goroutines := 100

	versionChan := make(chan a2a.TaskVersion, goroutines)
	for range goroutines {
		go func() {
			versionChan <- mustSaveVersioned(t, store, task, a2a.TaskVersionMissing)
		}()
	}
	var versions []a2a.TaskVersion
	for range goroutines {
		versions = append(versions, <-versionChan)
	}

	for i := 0; i < len(versions); i++ {
		for j := i + 1; j < len(versions); j++ {
			if !(versions[i].After(versions[j]) || versions[j].After(versions[i])) {
				t.Fatalf("got v1 <= v2 and v2 <= v1 meaning v1 == v2, want strict ordering: v1 = %v, v2 = %v", versions[i], versions[j])
			}
		}
	}
}

func TestInMemoryTaskStore_ConcurrentTaskModification(t *testing.T) {
	store := NewMem()

	task := &a2a.Task{ID: a2a.NewTaskID(), ContextID: "id"}
	v1 := mustSaveVersioned(t, store, task, a2a.TaskVersionMissing)

	task.ContextID = "id2"
	_ = mustSaveVersioned(t, store, task, v1)

	task.ContextID = "id3"
	if _, err := store.Save(t.Context(), task, task, v1); !errors.Is(err, a2a.ErrConcurrentTaskModification) {
		t.Fatal("Save() succeeded, wanted concurrent modification error")
	}
}
