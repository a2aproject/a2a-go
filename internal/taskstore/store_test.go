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
	"encoding/gob"
	"errors"
	"fmt"
	"testing"

	"github.com/a2aproject/a2a-go/a2a"
)

type customMeta struct{ Val int }

func mustSave(t *testing.T, store *Mem, task *a2a.Task) {
	t.Helper()
	if err := store.Save(t.Context(), task); err != nil {
		t.Fatalf("Save() error: %v", err)
	}
}

func mustGet(t *testing.T, store *Mem, id a2a.TaskID) *a2a.Task {
	t.Helper()
	got, err := store.Get(t.Context(), id)
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	return got
}

func TestInMemoryTaskStore_GetSaved(t *testing.T) {
	store := NewMem()
	metaKey := "key"

	meta := customMeta{Val: 42}
	task := &a2a.Task{ID: a2a.NewTaskID(), ContextID: "id"}
	task.Metadata = map[string]any{metaKey: meta}
	gob.Register(customMeta{})
	mustSave(t, store, task)

	got := mustGet(t, store, task.ID)
	if task.ContextID != got.ContextID {
		t.Fatalf("Data mismatch: got = %v, want = %v", got, task)
	}
	if meta != got.Metadata[metaKey] {
		t.Fatalf("Metadata mismatch: got = %v, want = %v", got, task)
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
		Artifacts: []*a2a.Artifact{&a2a.Artifact{Name: "foo"}},
		Metadata:  make(map[string]any),
	}
	mustSave(t, store, task)

	task.Status = a2a.TaskStatus{State: a2a.TaskStateCompleted}
	task.Artifacts[0] = &a2a.Artifact{Name: "bar"}
	task.Metadata[metaKey] = fmt.Sprintf("%v", task.Metadata["new"]) + "-modified"

	got := mustGet(t, store, task.ID)
	if task.Status.State == got.Status.State {
		t.Fatalf("Unexpected status change. got = %v, want = %v", got.Status, task.Status)
	}
	if task.Artifacts[0].Name == got.Artifacts[0].Name {
		t.Fatalf("Unexpected artifact change. got = %v, want = %v", got.Artifacts, task.Artifacts)
	}
	if task.Metadata[metaKey] == got.Metadata[metaKey] {
		t.Fatalf("Unexpected metadata change. got = %v, want = %v", got.Metadata, task.Metadata)
	}
}

func TestInMemoryTaskStore_TaskNotFound(t *testing.T) {
	store := NewMem()

	_, err := store.Get(t.Context(), a2a.TaskID("invalid"))
	if !errors.Is(err, a2a.ErrTaskNotFound) {
		t.Fatalf("Unexpected error: got: %v, wanted ErrTaskNotFound", err)
	}
}
