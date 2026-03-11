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

package e2e

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2aclient"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
	"github.com/a2aproject/a2a-go/internal/testutil"
	"github.com/a2aproject/a2a-go/internal/testutil/testexecutor"
)

func TestConcurrentCancellation_ExecutionResolvesToCanceledTask(t *testing.T) {
	ctx := t.Context()

	executionErrCauseChan := make(chan error, 1)
	executor := &testexecutor.TestAgentExecutor{}
	// Execution will be creating task artifacts until a task is canceled. Cancelation will be detected using a failed task store update
	executor.ExecuteFn = func(ctx context.Context, reqCtx *a2asrv.RequestContext, q eventqueue.Queue) error {
		if err := q.Write(ctx, a2a.NewSubmittedTask(reqCtx, reqCtx.Message)); err != nil {
			return err
		}
		for ctx.Err() == nil {
			if err := q.Write(ctx, a2a.NewArtifactEvent(reqCtx, a2a.TextPart{Text: "work..."})); err != nil {
				return err
			}
			time.Sleep(5 * time.Millisecond)
		}
		executionErrCauseChan <- context.Cause(ctx)
		return ctx.Err()
	}
	executionCleanupResultChan := make(chan a2a.SendMessageResult, 1)
	executor.CleanupFn = func(ctx context.Context, reqCtx *a2asrv.RequestContext, result a2a.SendMessageResult, err error) {
		executionCleanupResultChan <- result
	}

	// This code will run on a different server
	canceler := &testexecutor.TestAgentExecutor{}
	canceler.CancelFn = func(ctx context.Context, reqCtx *a2asrv.RequestContext, q eventqueue.Queue) error {
		event := a2a.NewStatusUpdateEvent(reqCtx.StoredTask, a2a.TaskStateCanceled, nil)
		event.Final = true
		return q.Write(ctx, event)
	}
	cancelationCleanupResultChan := make(chan a2a.SendMessageResult, 1)
	canceler.CleanupFn = func(ctx context.Context, reqCtx *a2asrv.RequestContext, result a2a.SendMessageResult, err error) {
		cancelationCleanupResultChan <- result
	}

	// The store is shared by two server
	store := testutil.NewTestTaskStore()
	client1 := startTestServer(t, executor, store)
	client2 := startTestServer(t, canceler, store)

	// Send message streaming in a detached goroutine piping events to a channel
	executionEvents := make(chan a2a.Event, 1)
	go func() {
		defer close(executionEvents)
		msg := &a2a.MessageSendParams{Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: "Work"})}
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

	canceledTask, err := client2.CancelTask(ctx, &a2a.TaskIDParams{ID: task.ID})
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
	if !errors.Is(gotErrCause, a2a.ErrConcurrentTaskModification) {
		t.Fatalf("execution error cause = %v, want %v", gotErrCause, a2a.ErrConcurrentTaskModification)
	}

	for i, ch := range []chan a2a.SendMessageResult{executionCleanupResultChan, cancelationCleanupResultChan} {
		gotCleanupResult := <-ch
		if task, ok := gotCleanupResult.(*a2a.Task); ok {
			if task.Status.State != a2a.TaskStateCanceled {
				t.Fatalf("execution cleanup result at %d wrong state = %v, want %v", i, task.Status.State, a2a.TaskStateCanceled)
			}
		} else {
			t.Fatalf("execution cleanup result at %d is not a task, got %T", i, gotCleanupResult)
		}
	}
}

func startTestServer(t *testing.T, executor a2asrv.AgentExecutor, store a2asrv.TaskStore) *a2aclient.Client {
	handler := a2asrv.NewHandler(executor, a2asrv.WithTaskStore(store))
	server := httptest.NewServer(a2asrv.NewJSONRPCHandler(handler))
	t.Cleanup(server.Close)
	client := mustCreateClient(t, newAgentCard(server.URL))
	return client
}
