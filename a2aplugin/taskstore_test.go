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

package a2aplugin

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"
	"github.com/a2aproject/a2a-go/v2/internal/testutil"
)

func newTask(id a2a.TaskID) *a2a.Task {
	return &a2a.Task{ID: id, Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}}
}

func TestServer_NoTaskStoreHooksReturnsNext(t *testing.T) {
	t.Parallel()
	next := testutil.NewTestTaskStore()

	got := Server("noop", ServerHooks{}).WrapTaskStore(next)

	if reflect.ValueOf(got).Pointer() != reflect.ValueOf(next).Pointer() {
		t.Fatal("WrapTaskStore() returned a wrapper, want next unchanged")
	}
}

func TestServer_BeforeTaskWriteOnCreate(t *testing.T) {
	t.Parallel()
	task := newTask("task-1")
	var gotWrite TaskWrite
	store := testutil.NewTestTaskStore()
	var seenCtxValue any
	store.CreateFunc = func(ctx context.Context, task *a2a.Task) (taskstore.TaskVersion, error) {
		seenCtxValue = ctx.Value(ctxKey{})
		return taskstore.TaskVersion(1), nil
	}
	plugin := Server("before-create", ServerHooks{
		BeforeTaskWrite: func(ctx context.Context, w TaskWrite) (context.Context, error) {
			gotWrite = w
			return context.WithValue(ctx, ctxKey{}, "before"), nil
		},
	})

	wrapped := plugin.WrapTaskStore(store)
	if _, err := wrapped.Create(t.Context(), task); err != nil {
		t.Fatalf("wrapped.Create() error = %v, want nil", err)
	}

	wantWrite := TaskWrite{Task: task}
	if diff := cmp.Diff(wantWrite, gotWrite); diff != "" {
		t.Fatalf("BeforeTaskWrite() wrong write (-want +got) diff = %s", diff)
	}
	if seenCtxValue != "before" {
		t.Fatalf("store.Create() ctx value = %v, want %q", seenCtxValue, "before")
	}
}

func TestServer_BeforeTaskWriteOnUpdate(t *testing.T) {
	t.Parallel()
	task := newTask("task-1")
	prev := newTask("task-1")
	event := a2a.NewStatusUpdateEvent(a2a.TaskInfo{TaskID: task.ID}, a2a.TaskStateWorking, nil)
	var gotWrite TaskWrite
	store := testutil.NewTestTaskStore()
	store.UpdateFunc = func(ctx context.Context, req *taskstore.UpdateRequest) (taskstore.TaskVersion, error) {
		return taskstore.TaskVersion(2), nil
	}
	plugin := Server("before-update", ServerHooks{
		BeforeTaskWrite: func(ctx context.Context, w TaskWrite) (context.Context, error) {
			gotWrite = w
			return ctx, nil
		},
	})

	wrapped := plugin.WrapTaskStore(store)
	req := &taskstore.UpdateRequest{Task: task, Event: event, PrevTask: prev, PrevVersion: taskstore.TaskVersion(1)}
	if _, err := wrapped.Update(t.Context(), req); err != nil {
		t.Fatalf("wrapped.Update() error = %v, want nil", err)
	}

	wantWrite := TaskWrite{Task: task, Event: event, PrevTask: prev}
	if diff := cmp.Diff(wantWrite, gotWrite, cmpopts.IgnoreUnexported(a2a.Task{})); diff != "" {
		t.Fatalf("BeforeTaskWrite() wrong write (-want +got) diff = %s", diff)
	}
}

func TestServer_BeforeTaskWriteError(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("rejected")
	createCalled := false
	store := testutil.NewTestTaskStore()
	store.CreateFunc = func(context.Context, *a2a.Task) (taskstore.TaskVersion, error) {
		createCalled = true
		return taskstore.TaskVersionMissing, nil
	}
	var afterErr error
	var afterCtxValue any
	plugin := Server("reject", ServerHooks{
		BeforeTaskWrite: func(ctx context.Context, _ TaskWrite) (context.Context, error) {
			return context.WithValue(ctx, ctxKey{}, "before"), wantErr
		},
		AfterTaskWrite: func(ctx context.Context, _ TaskWrite, err error) {
			afterCtxValue = ctx.Value(ctxKey{})
			afterErr = err
		},
	})

	wrapped := plugin.WrapTaskStore(store)
	_, err := wrapped.Create(t.Context(), newTask("task-1"))

	if !errors.Is(err, wantErr) {
		t.Fatalf("wrapped.Create() error = %v, want %v", err, wantErr)
	}
	if createCalled {
		t.Fatal("store.Create() called, want BeforeTaskWrite to stop the write")
	}
	if !errors.Is(afterErr, wantErr) {
		t.Fatalf("AfterTaskWrite() err = %v, want %v", afterErr, wantErr)
	}
	if afterCtxValue != "before" {
		t.Fatalf("AfterTaskWrite() ctx value = %v, want %q", afterCtxValue, "before")
	}
}

func TestServer_AfterTaskWriteReceivesStoreError(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("store down")
	store := testutil.NewTestTaskStore().SetSaveError(wantErr)
	var gotErr error
	plugin := Server("after", ServerHooks{
		AfterTaskWrite: func(_ context.Context, _ TaskWrite, err error) {
			gotErr = err
		},
	})

	wrapped := plugin.WrapTaskStore(store)
	_, err := wrapped.Create(t.Context(), newTask("task-1"))

	if !errors.Is(err, wantErr) {
		t.Fatalf("wrapped.Create() error = %v, want %v", err, wantErr)
	}
	if !errors.Is(gotErr, wantErr) {
		t.Fatalf("AfterTaskWrite() err = %v, want %v", gotErr, wantErr)
	}
}

func TestServer_WrapTaskStoreOutsideHooks(t *testing.T) {
	t.Parallel()
	var calls []string
	store := testutil.NewTestTaskStore()
	store.CreateFunc = func(context.Context, *a2a.Task) (taskstore.TaskVersion, error) {
		calls = append(calls, "store")
		return taskstore.TaskVersion(1), nil
	}
	plugin := Server("order", ServerHooks{
		BeforeTaskWrite: func(ctx context.Context, _ TaskWrite) (context.Context, error) {
			calls = append(calls, "before")
			return ctx, nil
		},
		WrapTaskStore: func(next taskstore.Store) taskstore.Store {
			return &loggingStore{Store: next, name: "wrap", calls: &calls}
		},
	})

	wrapped := plugin.WrapTaskStore(store)
	if _, err := wrapped.Create(t.Context(), newTask("task-1")); err != nil {
		t.Fatalf("wrapped.Create() error = %v, want nil", err)
	}

	want := []string{"wrap", "before", "store"}
	if diff := cmp.Diff(want, calls); diff != "" {
		t.Fatalf("WrapTaskStore() wrong call order (-want +got) diff = %s", diff)
	}
}

func TestServer_TaskStoreHooksSkipReads(t *testing.T) {
	t.Parallel()
	store := testutil.NewTestTaskStore()
	plugin := Server("hooks", ServerHooks{
		BeforeTaskWrite: func(context.Context, TaskWrite) (context.Context, error) {
			t.Fatal("BeforeTaskWrite() called on a read")
			return nil, nil
		},
		AfterTaskWrite: func(context.Context, TaskWrite, error) {
			t.Fatal("AfterTaskWrite() called on a read")
		},
	})

	wrapped := plugin.WrapTaskStore(store)
	if _, err := wrapped.Get(t.Context(), "missing"); !errors.Is(err, a2a.ErrTaskNotFound) {
		t.Fatalf("wrapped.Get() error = %v, want %v", err, a2a.ErrTaskNotFound)
	}
}

type loggingStore struct {
	taskstore.Store
	name  string
	calls *[]string
}

func (s *loggingStore) Create(ctx context.Context, task *a2a.Task) (taskstore.TaskVersion, error) {
	*s.calls = append(*s.calls, s.name)
	return s.Store.Create(ctx, task)
}
