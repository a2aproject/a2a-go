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

package e2e_test

import (
	"context"
	"errors"
	"iter"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"
	"github.com/a2aproject/a2a-go/v2/internal/testutil"
	"github.com/a2aproject/a2a-go/v2/internal/testutil/testexecutor"
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

	// The store is shared by two server
	store := testutil.NewTestTaskStore()
	cancelClient := startTestServer(t, testexecutor.NewCanceler(), store)

	executionEvents, drainFn := sendMessageInBackground(t, startTestServer(t, executor, store))
	defer drainFn()
	taskEvent, ok := <-executionEvents
	if !ok {
		t.Fatalf("client.SendStreamingMessage() no task event")
	}
	task, ok := taskEvent.(*a2a.Task)
	if !ok {
		t.Fatalf("client.SendStreamingMessage() task event is not a task, got %T", taskEvent)
	}

	canceledTask, err := cancelClient.CancelTask(ctx, &a2a.CancelTaskRequest{ID: task.ID})
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

func TestConcurrentCancellationFailure_GetsCorrectError(t *testing.T) {
	ctx := t.Context()

	sharedStore := testutil.NewTestTaskStore()
	executor, execChannels := testexecutor.NewWithControlChannels()
	receivedEventsChan, drainFn := sendMessageInBackground(t, startTestServer(t, executor, sharedStore))
	defer drainFn()
	reqCtx := <-execChannels.ReqCtx
	execChannels.ExecEvent <- a2a.NewSubmittedTask(reqCtx, reqCtx.Message)
	<-receivedEventsChan

	cancelErrChan := make(chan error)
	canceler, cancelChannels := testexecutor.NewWithControlChannels()
	go func() {
		cancelClient := startTestServer(t, canceler, sharedStore)
		_, err := cancelClient.CancelTask(ctx, &a2a.CancelTaskRequest{ID: reqCtx.TaskID})
		cancelErrChan <- err
	}()
	<-cancelChannels.CancelCalled

	execChannels.ExecEvent <- a2a.NewStatusUpdateEvent(reqCtx, a2a.TaskStateCompleted, nil)
	<-receivedEventsChan

	cancelChannels.ContinueCancel <- struct{}{}

	gotErr := <-cancelErrChan
	if !errors.Is(gotErr, a2a.ErrTaskNotCancelable) {
		t.Fatalf("cancelClient.CancelTask() error = %v, want %v", gotErr, a2a.ErrTaskNotCancelable)
	}
}

func TestCancelCancelledTask(t *testing.T) {
	ctx := t.Context()

	sharedStore := testutil.NewTestTaskStore()
	executor, execChannels := testexecutor.NewWithControlChannels()
	receivedEventsChan, drainFn := sendMessageInBackground(t, startTestServer(t, executor, sharedStore))
	defer drainFn()
	reqCtx := <-execChannels.ReqCtx
	execChannels.ExecEvent <- a2a.NewSubmittedTask(reqCtx, reqCtx.Message)
	<-receivedEventsChan

	cancelClient1 := startTestServer(t, testexecutor.NewCanceler(), sharedStore)
	if _, err := cancelClient1.CancelTask(ctx, &a2a.CancelTaskRequest{ID: reqCtx.TaskID}); err != nil {
		t.Errorf("cancelClient1.CancelTask() error = %v", err)
	}

	execChannels.ExecEvent <- a2a.NewStatusUpdateEvent(reqCtx, a2a.TaskStateCompleted, nil)
	<-receivedEventsChan

	cancelClient2 := startTestServer(t, testexecutor.NewCanceler(), sharedStore)
	task, err := cancelClient2.CancelTask(ctx, &a2a.CancelTaskRequest{ID: reqCtx.TaskID})
	if err != nil {
		t.Fatalf("cancelClient2.CancelTask() error = %v", err)
	}
	if task.Status.State != a2a.TaskStateCanceled {
		t.Fatalf("cancelClient2.CancelTask() = %v, want cancelled task", task)
	}
}

func TestConcurrentCancellation_MultipleCancelCallsGetSameResult(t *testing.T) {
	ctx := t.Context()

	sharedStore := testutil.NewTestTaskStore()
	executor, execChannels := testexecutor.NewWithControlChannels()
	receivedEventsChan, drainFn := sendMessageInBackground(t, startTestServer(t, executor, sharedStore))
	defer drainFn()
	reqCtx := <-execChannels.ReqCtx
	execChannels.ExecEvent <- a2a.NewSubmittedTask(reqCtx, reqCtx.Message)
	<-receivedEventsChan

	concurrentCancelCount := 2
	var cancelChannels []*testexecutor.ControlChannels
	cancelResutlts := make(chan *a2a.Task, concurrentCancelCount)
	for range concurrentCancelCount {
		canceler, channels := testexecutor.NewWithControlChannels()
		cancelChannels = append(cancelChannels, channels)

		client := startTestServer(t, canceler, sharedStore)
		go func() {
			task, err := client.CancelTask(ctx, &a2a.CancelTaskRequest{ID: reqCtx.TaskID})
			if err != nil {
				t.Errorf("CancelTask() error = %v", err)
			}
			cancelResutlts <- task
		}()
	}
	for _, channels := range cancelChannels {
		<-channels.CancelCalled
	}
	for _, channels := range cancelChannels {
		channels.ContinueCancel <- struct{}{}
	}

	for range concurrentCancelCount {
		task := <-cancelResutlts
		if task == nil {
			t.Fatal("CancelTask() returned nil task")
		}
		if task.Status.State != a2a.TaskStateCanceled {
			t.Fatalf("CancelTask() status = %v, want canceled task", task.Status.State)
		}
	}

	execChannels.ExecEvent <- a2a.NewStatusUpdateEvent(reqCtx, a2a.TaskStateCompleted, nil)
	execResult := <-receivedEventsChan

	if task, ok := execResult.(*a2a.Task); ok {
		if task.Status.State != a2a.TaskStateCanceled {
			t.Fatalf("client.SendStreamingMessage() wrong state = %v, want %v", task.Status.State, a2a.TaskStateCanceled)
		}
	} else {
		t.Fatalf("client.SendStreamingMessage() task event is not a task, got %T", execResult)
	}
}

func startTestServer(t *testing.T, executor a2asrv.AgentExecutor, store taskstore.Store) *a2aclient.Client {
	handler := a2asrv.NewHandler(executor, a2asrv.WithTaskStore(store))
	server := httptest.NewServer(a2asrv.NewJSONRPCHandler(handler))
	t.Cleanup(server.Close)
	client := mustCreateClient(t, newAgentCard(server.URL))
	return client
}

func sendMessageInBackground(t *testing.T, client *a2aclient.Client) (<-chan a2a.Event, func()) {
	receivedEventsChan := make(chan a2a.Event, 1)
	go func() {
		defer close(receivedEventsChan)
		msg := &a2a.SendMessageRequest{Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("Work"))}
		for event, err := range client.SendStreamingMessage(t.Context(), msg) {
			if err != nil {
				t.Errorf("client.SendStreamingMessage() error = %v", err)
				return
			}
			receivedEventsChan <- event
		}
	}()
	return receivedEventsChan, func() {
		for range receivedEventsChan {
			// drain
		}
	}
}
