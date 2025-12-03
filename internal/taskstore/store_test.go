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
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/a2aproject/a2a-go/a2a"
)

func mustSave(t *testing.T, store *Mem, task *a2a.Task) {
	_ = mustSaveVersioned(t, store, task, a2a.TaskVersionMissing)
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
		t.Fatalf("Unexpected status change: got = %v, want = %v", got.Status, task.Status)
	}
	if task.Artifacts[0].Name == got.Artifacts[0].Name {
		t.Fatalf("Unexpected artifact change: got = %v, want = %v", got.Artifacts, task.Artifacts)
	}
	if task.Metadata[metaKey] == got.Metadata[metaKey] {
		t.Fatalf("Unexpected metadata change: got = %v, want = %v", got.Metadata, task.Metadata)
	}
}

func TestInMemoryTaskStore_TaskNotFound(t *testing.T) {
	store := NewMem()

	_, _, err := store.Get(t.Context(), a2a.TaskID("invalid"))
	if !errors.Is(err, a2a.ErrTaskNotFound) {
		t.Fatalf("Unexpected error: got = %v, wanted ErrTaskNotFound", err)
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
	if _, err := store.Save(t.Context(), task, task, v1); err == nil {
		t.Fatal("Save() succeeded, wanted concurrent modification error")
	}
}
