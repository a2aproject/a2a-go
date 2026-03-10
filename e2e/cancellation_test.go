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

package e2e_test

import (
	"context"
	"errors"
	"iter"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2aclient"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/a2aproject/a2a-go/a2asrv/taskstore"
	"github.com/a2aproject/a2a-go/internal/testutil"
	"github.com/a2aproject/a2a-go/internal/testutil/testexecutor"
)

func TestConcurrentCancellation_ExecutionResolvesToCanceledTask(t *testing.T) {
	ctx := t.Context()

	executionErrCauseChan := make(chan error, 1)
	executor := &testexecutor.TestAgentExecutor{}

	// Execution will be creating task artifacts until a task is canceled. Cancelation will be detected using a failed task store update
	executor.ExecuteFn = func(ctx context.Context, reqCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
		return func(yield func(a2a.Event, error) bool) {
			if !yield(a2a.NewSubmittedTask(reqCtx, reqCtx.Message), nil) {
				return
			}
			for ctx.Err() == nil {
				if !yield(a2a.NewArtifactEvent(reqCtx, a2a.NewTextPart("work...")), nil) {
					return
				}
				time.Sleep(5 * time.Millisecond)
			}
			executionErrCauseChan <- context.Cause(ctx)
			yield(nil, context.Cause(ctx))
		}
	}

	// This code will run on a different server
	executor.CancelFn = func(ctx context.Context, reqCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
		return func(yield func(a2a.Event, error) bool) {
			yield(a2a.NewStatusUpdateEvent(reqCtx.StoredTask, a2a.TaskStateCanceled, nil), nil)
		}
	}

	// The store is shared by two server
	store := testutil.NewTestTaskStore()
	client1 := startTestServer(t, executor, store)
	client2 := startTestServer(t, executor, store)

	// Send message streaming in a detached goroutine piping events to a channel
	executionEvents := make(chan a2a.Event, 1)
	go func() {
		defer close(executionEvents)
		msg := &a2a.SendMessageRequest{Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("Work"))}
		for event, err := range client1.SendStreamingMessage(ctx, msg) {
			if err != nil {
				t.Errorf("client.SendStreamingMessage() error = %v", err)
				return
			}
			executionEvents <- event
		}
	}()

	taskEvent, ok := <-executionEvents
	if !ok {
		t.Fatalf("client.SendStreamingMessage() no task event")
	}
	task, ok := taskEvent.(*a2a.Task)
	if !ok {
		t.Fatalf("client.SendStreamingMessage() task event is not a task, got %T", taskEvent)
	}

	canceledTask, err := client2.CancelTask(ctx, &a2a.CancelTaskRequest{ID: task.ID})
	if err != nil {
		t.Fatalf("client.CancelTask() error = %v", err)
	}
	if canceledTask.Status.State != a2a.TaskStateCanceled {
		t.Fatalf("client.CancelTask() wrong state = %v, want %v", canceledTask.Status.State, a2a.TaskStateCanceled)
	}

	var lastExecutionEvent a2a.Event
	for event := range executionEvents {
		lastExecutionEvent = event
	}
	if task, ok := lastExecutionEvent.(*a2a.Task); ok {
		if task.Status.State != a2a.TaskStateCanceled {
			t.Fatalf("client.SendStreamingMessage() wrong state = %v, want %v", task.Status.State, a2a.TaskStateCanceled)
		}
	} else {
		t.Fatalf("client.SendStreamingMessage() task event is not a task, got %T", lastExecutionEvent)
	}

	gotErrCause := <-executionErrCauseChan
	if !errors.Is(gotErrCause, taskstore.ErrConcurrentModification) {
		t.Fatalf("execution error cause = %v, want %v", gotErrCause, taskstore.ErrConcurrentModification)
	}
}

func startTestServer(t *testing.T, executor a2asrv.AgentExecutor, store taskstore.Store) *a2aclient.Client {
	handler := a2asrv.NewHandler(executor, a2asrv.WithTaskStore(store))
	server := httptest.NewServer(a2asrv.NewJSONRPCHandler(handler))
	t.Cleanup(server.Close)
	client := mustCreateClient(t, newAgentCard(server.URL))
	return client
}
