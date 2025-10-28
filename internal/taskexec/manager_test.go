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

package taskexec

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
)

func newManager() *Manager {
	qm := eventqueue.NewInMemoryManager()
	return NewManager(qm)
}

type testProcessor struct {
	callCount         atomic.Int32
	nextEventTerminal bool
	processErr        error

	contextCanceled bool
	block           chan struct{}
}

func (e *testProcessor) Process(ctx context.Context, event a2a.Event) (*a2a.SendMessageResult, error) {
	e.callCount.Add(1)

	if e.block != nil {
		select {
		case <-e.block:
		case <-ctx.Done():
			e.contextCanceled = true
			return nil, ctx.Err()
		}
	}

	if e.processErr != nil {
		return nil, e.processErr
	}

	if e.nextEventTerminal {
		result := event.(a2a.SendMessageResult)
		return &result, nil
	}

	return nil, nil
}

type testExecutor struct {
	*testProcessor

	executeCalled   chan struct{}
	executeErr      error
	queue           eventqueue.Queue
	contextCanceled bool
	block           chan struct{}
}

func newExecutor() *testExecutor {
	return &testExecutor{executeCalled: make(chan struct{}), testProcessor: &testProcessor{}}
}

func (e *testExecutor) Execute(ctx context.Context, queue eventqueue.Queue) error {
	e.queue = queue
	close(e.executeCalled)

	if e.block != nil {
		select {
		case <-e.block:
		case <-ctx.Done():
			e.contextCanceled = true
			return ctx.Err()
		}
	}

	return e.executeErr
}

type testCanceler struct {
	*testProcessor

	cancelCalled    chan struct{}
	cancelErr       error
	queue           eventqueue.Queue
	contextCanceled bool
	block           chan struct{}
}

func newCanceler() *testCanceler {
	return &testCanceler{cancelCalled: make(chan struct{}), testProcessor: &testProcessor{}}
}

func (c *testCanceler) Cancel(ctx context.Context, queue eventqueue.Queue) error {
	c.queue = queue
	close(c.cancelCalled)

	if c.block != nil {
		select {
		case <-c.block:
		case <-ctx.Done():
			c.contextCanceled = true
			return ctx.Err()
		}
	}

	return c.cancelErr
}

func (e *testExecutor) mustWrite(t *testing.T, event a2a.Event) {
	t.Helper()
	if err := e.queue.Write(t.Context(), event); err != nil {
		t.Fatalf("queue Write() failed: %v", err)
	}
}

func (e *testCanceler) mustWrite(t *testing.T, event a2a.Event) {
	t.Helper()
	if err := e.queue.Write(t.Context(), event); err != nil {
		t.Fatalf("queue Write() failed: %v", err)
	}
}

func TestManager_Execute(t *testing.T) {
	t.Parallel()
	ctx, tid, manager := t.Context(), a2a.NewTaskID(), newManager()

	executor := newExecutor()
	executor.nextEventTerminal = true
	execution, subscription, err := manager.Execute(ctx, tid, executor)
	subscription.cancel()
	if err != nil {
		t.Fatalf("Execute() failed: %v", err)
	}

	<-executor.executeCalled
	want := &a2a.Task{ID: tid}
	executor.mustWrite(t, want)

	if got, err := execution.Result(ctx); err != nil || got != want {
		t.Fatalf("execution.Result() = (%v, %v), want %v", got, err, want)
	}
}

func TestManager_EventProcessingFailureFailsExecution(t *testing.T) {
	t.Parallel()
	ctx, tid, manager := t.Context(), a2a.NewTaskID(), newManager()

	executor := newExecutor()
	executor.processErr = errors.New("test error")
	execution, subscription, err := manager.Execute(ctx, tid, executor)
	subscription.cancel()
	if err != nil {
		t.Fatalf("manager.Execute() failed: %v", err)
	}

	<-executor.executeCalled
	executor.mustWrite(t, &a2a.Task{ID: tid})

	if _, err = execution.Result(ctx); !errors.Is(err, executor.processErr) {
		t.Fatalf("execution.Result() failed with %v, want %v", err, executor.processErr)
	}
}

func TestManager_ExecuteFailureFailsExecution(t *testing.T) {
	t.Parallel()
	ctx, tid, manager := t.Context(), a2a.NewTaskID(), newManager()

	executor := newExecutor()
	executor.executeErr = errors.New("test error")
	execution, subscription, err := manager.Execute(ctx, tid, executor)
	subscription.cancel()
	if err != nil {
		t.Fatalf("manager.Execute() failed: %v", err)
	}

	if _, err = execution.Result(ctx); !errors.Is(err, executor.executeErr) {
		t.Fatalf("execution.Result() = %v, want %v", err, executor.executeErr)
	}
}

func TestManager_ExecuteFailureCancelsProcessingContext(t *testing.T) {
	t.Parallel()
	ctx, tid, manager := t.Context(), a2a.NewTaskID(), newManager()

	executor := newExecutor()
	executor.executeErr = errors.New("test error")
	executor.block = make(chan struct{})
	executor.testProcessor.block = make(chan struct{})
	execution, subscription, err := manager.Execute(ctx, tid, executor)
	subscription.cancel()
	if err != nil {
		t.Fatalf("manager.Execute() failed: %v", err)
	}

	<-executor.executeCalled
	executor.mustWrite(t, &a2a.Task{ID: tid})
	for executor.testProcessor.callCount.Load() == 0 {
		time.Sleep(1 * time.Millisecond)
	}
	close(executor.block)
	_, _ = execution.Result(ctx)

	if !executor.testProcessor.contextCanceled {
		t.Fatalf("expected processing context to be canceled")
	}
}

func TestManager_ProcessingFailureCancelsExecuteContext(t *testing.T) {
	t.Parallel()
	ctx, tid, manager := t.Context(), a2a.NewTaskID(), newManager()

	executor := newExecutor()
	executor.block = make(chan struct{})
	executor.processErr = errors.New("test error")
	execution, subscription, err := manager.Execute(ctx, tid, executor)
	subscription.cancel()
	if err != nil {
		t.Fatalf("manager.Execute() failed: %v", err)
	}

	<-executor.executeCalled
	executor.mustWrite(t, &a2a.Task{ID: tid})
	_, _ = execution.Result(ctx)

	if !executor.contextCanceled {
		t.Fatalf("expected processing context to be canceled")
	}
}

func TestManager_FanOutExecutionEvents(t *testing.T) {
	t.Parallel()
	ctx, tid, manager := t.Context(), a2a.NewTaskID(), newManager()

	executor := newExecutor()
	execution, subscription, err := manager.Execute(ctx, tid, executor)
	subscription.cancel()
	if err != nil {
		t.Fatalf("manager.Execute() failed: %v", err)
	}
	<-executor.executeCalled

	// subscribe ${consumerCount} consumers to execution events
	consumerCount := 3
	var waitSubscribed sync.WaitGroup
	var waitStopped sync.WaitGroup
	var waitConsumed sync.WaitGroup
	var mu sync.Mutex
	consumed := map[int][]a2a.Event{}
	for consumerI := range consumerCount {
		waitSubscribed.Add(1)
		waitStopped.Add(1)
		go func() {
			defer waitStopped.Done()

			sub, _ := newSubscription(t.Context(), execution)
			waitSubscribed.Done()

			for event := range sub.Events(ctx) {
				mu.Lock()
				consumed[consumerI] = append(consumed[consumerI], event)
				mu.Unlock()
				waitConsumed.Done()
			}
		}()
	}
	waitSubscribed.Wait()

	// for each produced event wait for all consumers to consume it
	states := []a2a.TaskState{a2a.TaskStateSubmitted, a2a.TaskStateWorking, a2a.TaskStateCompleted}
	for i, state := range states {
		waitConsumed.Add(consumerCount)
		executor.nextEventTerminal = i == len(states)-1
		executor.mustWrite(t, &a2a.Task{ID: tid, Status: a2a.TaskStatus{State: state}})
		waitConsumed.Wait()
	}

	for i, list := range consumed {
		if len(list) != len(states) {
			t.Fatalf("got %d events for consumer %d, want %d", len(list), i, len(states))
		}
		for eventI, event := range list {
			state := event.(*a2a.Task).Status.State
			if state != states[eventI] {
				t.Fatalf("got %v event state for consumer %d, want %v", state, i, states[eventI])
			}
		}
	}

	waitStopped.Wait()
}

func TestManager_CancelActiveExecution(t *testing.T) {
	t.Parallel()
	ctx, tid, manager := t.Context(), a2a.NewTaskID(), newManager()

	executor := newExecutor()
	executor.nextEventTerminal = true
	execution, subscription, err := manager.Execute(ctx, tid, executor)
	subscription.cancel()
	if err != nil {
		t.Fatalf("manager.Execute() failed: %v", err)
	}
	<-executor.executeCalled

	canceler := newCanceler()
	want := &a2a.Task{ID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateCanceled}}
	go func() {
		<-canceler.cancelCalled
		canceler.mustWrite(t, want)
	}()

	task, err := manager.Cancel(ctx, tid, canceler)
	if err != nil || task != want {
		t.Fatalf("manager.Cancel() = (%v, %v), want %v", task, err, want)
	}

	execResult, err := execution.Result(ctx)
	if err != nil || execResult != want {
		t.Fatalf("execution.Result = (%v, %v), want %v", execResult, err, want)
	}
}

func TestManager_EventsEmptyAfterExecutionFinished(t *testing.T) {
	t.Parallel()
	ctx, tid, manager := t.Context(), a2a.NewTaskID(), newManager()

	executor := newExecutor()
	executor.nextEventTerminal = true
	execution, subscription, err := manager.Execute(ctx, tid, executor)
	subscription.cancel()
	if err != nil {
		t.Fatalf("manager.Execute() failed: %v", err)
	}

	<-executor.executeCalled
	want := &a2a.Task{ID: tid}
	executor.mustWrite(t, want)

	if got, err := execution.Result(ctx); err != nil || got != want {
		t.Fatalf("execution.Result() = (%v, %v), want %v", got, err, want)
	}

	eventCount := 0
	for v, err := range execution.Events(ctx) {
		fmt.Println(v, err)
		eventCount++
	}
	if eventCount != 0 {
		t.Fatalf("got %d events after execution finished, want 0", eventCount)
	}
}

func TestManager_CancelWithoutActiveExecution(t *testing.T) {
	t.Parallel()
	ctx, tid, manager := t.Context(), a2a.NewTaskID(), newManager()

	canceler := newCanceler()
	canceler.nextEventTerminal = true
	want := &a2a.Task{ID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateCanceled}}
	go func() {
		<-canceler.cancelCalled
		canceler.mustWrite(t, want)
	}()

	task, err := manager.Cancel(ctx, tid, canceler)
	if err != nil || task != want {
		t.Fatalf("manager.Cancel() = (%v, %v), want %v", task, err, want)
	}
}

func TestManager_ConcurrentExecutionCompletesBeforeCancel(t *testing.T) {
	t.Parallel()
	ctx, tid, manager := t.Context(), a2a.NewTaskID(), newManager()

	executor := newExecutor()
	executor.nextEventTerminal = true
	execution, subscription, err := manager.Execute(ctx, tid, executor)
	subscription.cancel()
	if err != nil {
		t.Fatalf("manager.Execute() failed: %v", err)
	}
	<-executor.executeCalled

	canceler := newCanceler()
	canceler.block = make(chan struct{})
	cancelErr := make(chan error)
	go func() {
		task, err := manager.Cancel(ctx, tid, canceler)
		if task != nil || err == nil {
			t.Errorf("manager.Cancel() = %v, expected to fail", task)
		}
		cancelErr <- err
	}()
	<-canceler.cancelCalled

	executor.mustWrite(t, &a2a.Task{ID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}})
	_, _ = execution.Result(ctx)
	close(canceler.block)

	if got := <-cancelErr; !errors.Is(got, a2a.ErrTaskNotCancelable) {
		t.Fatalf("manager.Cancel() = %v, want %v", got, a2a.ErrTaskNotCancelable)
	}
}

func TestManager_ConcurrentCancelationsResolveToTheSameResult(t *testing.T) {
	t.Parallel()
	ctx, tid, manager := t.Context(), a2a.NewTaskID(), newManager()

	var wg sync.WaitGroup
	wg.Add(2)
	results := make(chan *a2a.Task, 2)

	canceler1 := newCanceler()
	canceler1.nextEventTerminal = true
	canceler1.block = make(chan struct{})
	go func() {
		task, err := manager.Cancel(ctx, tid, canceler1)
		if err != nil {
			t.Errorf("manager.Cancel() failed: %v", err)
		}
		results <- task
		wg.Done()
	}()
	<-canceler1.cancelCalled

	canceler2 := newCanceler()
	canceler2.cancelErr = errors.New("test error") // this should never be returned
	ready := make(chan struct{})
	go func() {
		close(ready)
		task, err := manager.Cancel(ctx, tid, canceler2)
		if err != nil {
			t.Errorf("manager.Cancel() failed: %v", err)
		}
		results <- task
		wg.Done()
	}()
	<-ready

	close(canceler1.block)
	want := &a2a.Task{ID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateCanceled}}
	canceler1.mustWrite(t, want)
	wg.Wait()

	t1, t2 := <-results, <-results
	if t1 != want || t2 != want {
		t.Fatalf("got cancelation results [%v, %v], want both to be %v, ", t1, t2, want)
	}
}

func TestManager_NotAllowedToExecuteWhileCanceling(t *testing.T) {
	t.Parallel()
	ctx, tid, manager := t.Context(), a2a.NewTaskID(), newManager()

	canceler := newCanceler()
	canceler.block = make(chan struct{})
	canceler.cancelErr = errors.New("test error")
	done := make(chan struct{})
	go func() {
		_, _ = manager.Cancel(ctx, tid, canceler)
		close(done)
	}()
	<-canceler.cancelCalled

	execution, _, err := manager.Execute(ctx, tid, newExecutor())
	if execution != nil || !errors.Is(err, ErrCancelationInProgress) {
		t.Fatalf("manager.Execute() = (%v, %v), want %v", execution, err, ErrCancelationInProgress)
	}

	close(canceler.block)
	<-done
}

func TestManager_CanExecuteAfterCancelFailed(t *testing.T) {
	t.Parallel()
	ctx, tid, manager := t.Context(), a2a.NewTaskID(), newManager()

	// First cancelation fails
	canceler := newCanceler()
	canceler.cancelErr = errors.New("test error")
	if task, err := manager.Cancel(ctx, tid, canceler); err == nil {
		t.Fatalf("manager.Cancel() = %v, want error", task)
	}

	executor := newExecutor()
	executor.nextEventTerminal = true
	execution, subscription, err := manager.Execute(ctx, tid, executor)
	subscription.cancel()
	if err != nil {
		t.Fatalf("maanger.Execute() failed with %v", err)
	}

	<-executor.executeCalled
	executor.mustWrite(t, &a2a.Task{ID: tid})

	if _, err := execution.Result(ctx); err != nil {
		t.Fatalf("execution.Result() wailed with %v", err)
	}
}

func TestManager_CanCancelAfterCancelFailed(t *testing.T) {
	ctx, tid, manager := t.Context(), a2a.NewTaskID(), newManager()

	// First cancelation fails
	canceler := newCanceler()
	canceler.cancelErr = errors.New("test error")
	if task, err := manager.Cancel(ctx, tid, canceler); err == nil {
		t.Fatalf("manager.Cancel() = %v, want error", task)
	}

	// Second cancelation succeeds
	canceler = newCanceler()
	canceler.nextEventTerminal = true
	go func() {
		<-canceler.cancelCalled
		canceler.mustWrite(t, &a2a.Task{ID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateCanceled}})
	}()

	if _, err := manager.Cancel(ctx, tid, canceler); err != nil {
		t.Errorf("manager.Cancel() failed with %v", err)
	}
}

func TestManager_GetExecution(t *testing.T) {
	ctx, tid, manager := t.Context(), a2a.NewTaskID(), newManager()

	executor := newExecutor()
	executor.nextEventTerminal = true
	startedExecution, subscription, err := manager.Execute(ctx, tid, executor)
	subscription.cancel()
	if err != nil {
		t.Fatalf("manager.Execute() failed: %v", err)
	}

	execution, ok := manager.GetExecution(tid)
	if !ok || execution != startedExecution {
		t.Fatalf("manager.GetExecution() = (%v, %v), want %v", ok, execution, startedExecution)
	}

	execution, ok = manager.GetExecution(tid + "-2")
	if ok || execution != nil {
		t.Fatalf("manager.GetExecution(fakeID) = (%v, %v), want (nil, false)", ok, execution)
	}

	<-executor.executeCalled
	executor.mustWrite(t, &a2a.Task{ID: tid})
	_, _ = startedExecution.Result(ctx)

	execution, ok = manager.GetExecution(tid)
	if ok || execution != nil {
		t.Fatalf("manager.GetExecution(finishedID) = (%v, %v), want (nil, false)", ok, execution)
	}
}
