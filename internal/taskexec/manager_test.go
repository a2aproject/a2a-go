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
	"github.com/a2aproject/a2a-go/a2asrv/workqueue"
	"github.com/a2aproject/a2a-go/internal/testutil"
	"github.com/google/go-cmp/cmp"
)

type testFactory struct {
	CreateExecutorFn func(context.Context, a2a.TaskID, *a2a.MessageSendParams) (Executor, Processor, error)
	CreateCancelerFn func(context.Context, *a2a.TaskIDParams) (Canceler, Processor, error)
}

var _ Factory = (*testFactory)(nil)

func (f *testFactory) CreateExecutor(ctx context.Context, tid a2a.TaskID, params *a2a.MessageSendParams) (Executor, Processor, error) {
	if f.CreateExecutorFn != nil {
		return f.CreateExecutorFn(ctx, tid, params)
	}
	return nil, nil, fmt.Errorf("not implemented")
}

func (f *testFactory) CreateCanceler(ctx context.Context, params *a2a.TaskIDParams) (Canceler, Processor, error) {
	if f.CreateCancelerFn != nil {
		return f.CreateCancelerFn(ctx, params)
	}
	return nil, nil, fmt.Errorf("not implemented")
}

func newStaticFactory(executor *testExecutor, canceler *testCanceler) Factory {
	return &testFactory{
		CreateExecutorFn: func(context.Context, a2a.TaskID, *a2a.MessageSendParams) (Executor, Processor, error) {
			if executor == nil {
				return nil, nil, fmt.Errorf("executor was not provided")
			}
			return executor, executor, nil
		},
		CreateCancelerFn: func(context.Context, *a2a.TaskIDParams) (Canceler, Processor, error) {
			if canceler == nil {
				return nil, nil, fmt.Errorf("canceler was not provided")
			}
			return canceler, canceler, nil
		},
	}
}

func newStaticExecutorManager(executor *testExecutor, canceler *testCanceler) Manager {
	return NewLocalManager(LocalManagerConfig{
		Factory: newStaticFactory(executor, canceler),
	})
}

type testProcessor struct {
	callCount         atomic.Int32
	nextEventTerminal bool
	processErr        error

	contextCanceled bool
	block           chan struct{}

	processErrorResult a2a.SendMessageResult
	processErrorErr    error
}

func (e *testProcessor) Process(ctx context.Context, event a2a.Event) (*ProcessorResult, error) {
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
		return &ProcessorResult{ExecutionResult: event.(a2a.SendMessageResult)}, nil
	}

	return &ProcessorResult{}, nil
}

func (e *testProcessor) ProcessError(ctx context.Context, err error) (a2a.SendMessageResult, error) {
	if e.processErrorResult == nil && e.processErrorErr == nil {
		return nil, err
	}
	return e.processErrorResult, e.processErrorErr
}

type testExecutor struct {
	*testProcessor

	executeCalled   chan struct{}
	executeErr      error
	queue           eventqueue.Queue
	contextCanceled bool
	block           chan struct{}
	emitTask        *a2a.Task
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

	if e.emitTask != nil {
		if err := queue.Write(ctx, e.emitTask); err != nil {
			return err
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
	emitTask        *a2a.Task
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

	if c.emitTask != nil {
		if err := queue.Write(ctx, c.emitTask); err != nil {
			return err
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

func consumeEvents(t *testing.T, sub Subscription) (chan []a2a.Event, chan error) {
	consumedEventsChan := make(chan []a2a.Event, 1)
	terminalErrChan := make(chan error, 1)
	go func() {
		consumedEvents := []a2a.Event{}
		var terminalErr error
		for ev, err := range sub.Events(t.Context()) {
			if err != nil {
				terminalErr = err
			} else {
				consumedEvents = append(consumedEvents, ev)
			}
		}

		consumedEventsChan <- consumedEvents
		if terminalErr != nil {
			terminalErrChan <- terminalErr
		} else {
			close(terminalErrChan)
		}
	}()
	return consumedEventsChan, terminalErrChan
}

type testWorkQueueMessage struct {
	payload *workqueue.Payload
}

var _ workqueue.Message = (*testWorkQueueMessage)(nil)

func (m *testWorkQueueMessage) Payload() *workqueue.Payload {
	return m.payload
}

func (m *testWorkQueueMessage) Complete(ctx context.Context) error {
	return nil
}

func (m *testWorkQueueMessage) Return(ctx context.Context, err error) error {
	return nil
}

type testWorkQueue struct {
	payloadChan chan *workqueue.Payload
}

var _ workqueue.ReadWriter = (*testWorkQueue)(nil)

func (q *testWorkQueue) Write(ctx context.Context, payload *workqueue.Payload) (a2a.TaskID, error) {
	select {
	case q.payloadChan <- payload:
		return payload.TaskID, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (q *testWorkQueue) Read(ctx context.Context) (workqueue.Message, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case msg := <-q.payloadChan:
		return &testWorkQueueMessage{payload: msg}, nil
	}
}

func (q *testWorkQueue) Close() error {
	return nil
}

func newStaticClusterManager(executor *testExecutor, canceler *testCanceler, taskStore TaskStore) Manager {
	config := &DistributedManagerConfig{
		WorkQueue:    workqueue.NewPullQueue(&testWorkQueue{payloadChan: make(chan *workqueue.Payload)}, nil),
		QueueManager: eventqueue.NewInMemoryManager(),
		Factory:      newStaticFactory(executor, canceler),
		TaskStore:    taskStore,
	}
	return NewDistributedManager(config)
}

type clusterMode bool

func (m clusterMode) newStaticManager(executor *testExecutor, canceler *testCanceler) (Manager, *testutil.TestTaskStore) {
	taskStore := testutil.NewTestTaskStore()
	if m {
		return newStaticClusterManager(executor, canceler, taskStore), taskStore
	}
	return newStaticExecutorManager(executor, canceler), taskStore
}

func (m clusterMode) newTestName(name string) string {
	if m {
		return name + " (cluster)"
	}
	return name
}

func TestManager_Execute(t *testing.T) {
	for _, clusterMode := range []clusterMode{false, true} {
		t.Run(clusterMode.newTestName("execute"), func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()

			executor := newExecutor()
			manager, _ := clusterMode.newStaticManager(executor, nil)
			executor.nextEventTerminal = true
			subscription, err := manager.Execute(ctx, &a2a.MessageSendParams{
				Message: a2a.NewMessage(a2a.MessageRoleUser),
			})
			subEventsChan, subErrChan := consumeEvents(t, subscription)
			if err != nil {
				t.Fatalf("Execute() failed: %v", err)
			}

			<-executor.executeCalled
			want := &a2a.Task{ID: subscription.TaskID(), Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}}
			executor.mustWrite(t, want)

			subEvents, subErr := <-subEventsChan, <-subErrChan
			if subErr != nil {
				t.Fatalf("subscription error = %v, want nil", subErr)
			}
			if diff := cmp.Diff([]a2a.Event{want}, subEvents); diff != "" {
				t.Fatalf("subscription events incorrect (+got,-want) diff = %s", diff)
			}
		})
	}
}

func TestManager_EventProcessingFailureFailsExecution(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	executor := newExecutor()
	manager := newStaticExecutorManager(executor, nil)
	executor.processErr = errors.New("test error")
	subscription, err := manager.Execute(ctx, &a2a.MessageSendParams{})
	subEventsChan, subErrChan := consumeEvents(t, subscription)
	if err != nil {
		t.Fatalf("manager.Execute() failed: %v", err)
	}

	<-executor.executeCalled
	executor.mustWrite(t, &a2a.Task{ID: subscription.TaskID()})

	subEvents, subErr := <-subEventsChan, <-subErrChan
	if !errors.Is(subErr, executor.processErr) {
		t.Fatalf("subscription error = %v, want %v", subErr, executor.processErr)
	}
	if diff := cmp.Diff([]a2a.Event{}, subEvents); diff != "" {
		t.Fatalf("subscription events incorrect (+got,-want) diff = %s", diff)
	}
}

func TestManager_ExecuteFailureFailsExecution(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	executor := newExecutor()
	manager := newStaticExecutorManager(executor, nil)
	executor.executeErr = errors.New("test error")
	subscription, err := manager.Execute(ctx, &a2a.MessageSendParams{})
	subEventsChan, subErrChan := consumeEvents(t, subscription)
	if err != nil {
		t.Fatalf("manager.Execute() failed: %v", err)
	}

	subEvents, subErr := <-subEventsChan, <-subErrChan
	if !errors.Is(subErr, executor.executeErr) {
		t.Fatalf("subscription error = %v, want %v", subErr, executor.executeErr)
	}
	if diff := cmp.Diff([]a2a.Event{}, subEvents); diff != "" {
		t.Fatalf("subscription events incorrect (+got,-want) diff = %s", diff)
	}
}

func TestManager_ExecuteFailureCancelsProcessingContext(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	executor := newExecutor()
	manager := newStaticExecutorManager(executor, nil)
	executor.executeErr = errors.New("test error")
	executor.block = make(chan struct{})
	executor.testProcessor.block = make(chan struct{})
	subscription, err := manager.Execute(ctx, &a2a.MessageSendParams{})
	executionResult, _ := consumeEvents(t, subscription)
	if err != nil {
		t.Fatalf("manager.Execute() failed: %v", err)
	}

	<-executor.executeCalled
	executor.mustWrite(t, &a2a.Task{ID: subscription.TaskID()})
	for executor.testProcessor.callCount.Load() == 0 {
		time.Sleep(1 * time.Millisecond)
	}
	close(executor.block)
	<-executionResult

	if !executor.testProcessor.contextCanceled {
		t.Fatalf("expected processing context to be canceled")
	}
}

func TestManager_ProcessingFailureCancelsExecuteContext(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	executor := newExecutor()
	manager := newStaticExecutorManager(executor, nil)
	executor.block = make(chan struct{})
	executor.processErr = errors.New("test error")
	subscription, err := manager.Execute(ctx, &a2a.MessageSendParams{})
	executionResult, _ := consumeEvents(t, subscription)
	if err != nil {
		t.Fatalf("manager.Execute() failed: %v", err)
	}

	<-executor.executeCalled
	executor.mustWrite(t, &a2a.Task{ID: subscription.TaskID()})
	<-executionResult

	if !executor.contextCanceled {
		t.Fatalf("expected processing context to be canceled")
	}
}

func TestManager_ExecuteErrorOverwriteByProcessorResult(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	wantResult := &a2a.Task{Status: a2a.TaskStatus{State: a2a.TaskStateFailed}}
	executor := newExecutor()
	manager := newStaticExecutorManager(executor, nil)
	executor.block = make(chan struct{})
	executor.executeErr = errors.New("test error!")
	executor.processErrorResult = wantResult

	subscription, err := manager.Execute(ctx, &a2a.MessageSendParams{})
	subEventsChan, subErrChan := consumeEvents(t, subscription)
	if err != nil {
		t.Fatalf("manager.Execute() failed: %v", err)
	}

	<-executor.executeCalled
	close(executor.block)
	if subErr := <-subErrChan; subErr != nil {
		t.Fatalf("subscription error = %v, want nil", subErr)
	}
	subEvents := <-subEventsChan
	if diff := cmp.Diff([]a2a.Event{wantResult}, subEvents); diff != "" {
		t.Fatalf("subscription events incorrect (+got,-want) diff = %s", diff)
	}
}

func TestManager_FanOutExecutionEvents(t *testing.T) {
	for _, clusterMode := range []clusterMode{false, true} {
		t.Run(clusterMode.newTestName("fan-out"), func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()

			executor := newExecutor()
			manager, taskStore := clusterMode.newStaticManager(executor, nil)
			subscription, err := manager.Execute(ctx, &a2a.MessageSendParams{
				Message: a2a.NewMessage(a2a.MessageRoleUser),
			})
			_ = taskStore.WithTasks(t, &a2a.Task{ID: subscription.TaskID()})
			_, _ = consumeEvents(t, subscription)
			if err != nil {
				t.Fatalf("manager.Execute() failed: %v", err)
			}
			<-executor.executeCalled

			// subscribe ${consumerCount} consumers to execution events
			consumerCount := 3
			var waitSubscribed sync.WaitGroup
			waitSubscribed.Add(consumerCount)

			var waitStopped sync.WaitGroup
			waitStopped.Add(consumerCount)

			var waitConsumed sync.WaitGroup
			if clusterMode { // task snapshot emitted for each consumer
				waitConsumed.Add(consumerCount)
			}
			var mu sync.Mutex
			consumed := map[int][]a2a.Event{}
			for consumerI := range consumerCount {
				go func() {
					defer waitStopped.Done()
					sub, err := manager.Resubscribe(t.Context(), subscription.TaskID())
					if err != nil {
						t.Errorf("manager.Resubscribe() error = %v", err)
						return
					}
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
				executor.mustWrite(t, &a2a.Task{ID: subscription.TaskID(), Status: a2a.TaskStatus{State: state}})
				waitConsumed.Wait()
			}

			for i, list := range consumed {
				if clusterMode { // skip task snapshot to clusterMode subscription
					list = list[1:]
				}
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
		})
	}
}

func TestManager_CancelActiveExecution(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	executor, canceler := newExecutor(), newCanceler()
	manager := newStaticExecutorManager(executor, canceler)
	executor.nextEventTerminal = true
	subscription, err := manager.Execute(ctx, &a2a.MessageSendParams{})
	executionResult, executionErr := consumeEvents(t, subscription)
	if err != nil {
		t.Fatalf("manager.Execute() failed: %v", err)
	}
	<-executor.executeCalled

	want := &a2a.Task{ID: subscription.TaskID(), Status: a2a.TaskStatus{State: a2a.TaskStateCanceled}}
	go func() {
		<-canceler.cancelCalled
		canceler.mustWrite(t, want)
	}()

	task, err := manager.Cancel(ctx, &a2a.TaskIDParams{ID: subscription.TaskID()})
	if err != nil || task != want {
		t.Fatalf("manager.Cancel() = (%v, %v), want %v", task, err, want)
	}

	execResult, err := <-executionResult, <-executionErr
	if err != nil || execResult[len(execResult)-1] != want {
		t.Fatalf("execution.Result = (%v, %v), want %v", execResult, err, want)
	}
}

func TestManager_CancelWithoutActiveExecution(t *testing.T) {
	for _, clusterMode := range []clusterMode{false, true} {
		t.Run(clusterMode.newTestName("cancel"), func(t *testing.T) {
			t.Parallel()
			ctx, tid := t.Context(), a2a.NewTaskID()

			canceler := newCanceler()
			manager, taskStore := clusterMode.newStaticManager(nil, canceler)
			taskStore.WithTasks(t, &a2a.Task{ID: tid})
			canceler.nextEventTerminal = true

			want := &a2a.Task{ID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateCanceled}}
			go func() {
				<-canceler.cancelCalled
				canceler.mustWrite(t, want)
			}()

			task, err := manager.Cancel(ctx, &a2a.TaskIDParams{ID: tid})
			if err != nil || task != want {
				t.Fatalf("manager.Cancel() = (%v, %v), want %v", task, err, want)
			}
		})
	}
}

func TestManager_ConcurrentExecutionCompletesBeforeCancel(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	executor, canceler := newExecutor(), newCanceler()
	manager := newStaticExecutorManager(executor, canceler)
	executor.nextEventTerminal = true
	subscription, err := manager.Execute(ctx, &a2a.MessageSendParams{})
	_, _ = consumeEvents(t, subscription)
	if err != nil {
		t.Fatalf("manager.Execute() failed: %v", err)
	}
	<-executor.executeCalled

	canceler.block = make(chan struct{})
	cancelErr := make(chan error)
	go func() {
		task, err := manager.Cancel(ctx, &a2a.TaskIDParams{ID: subscription.TaskID()})
		if task != nil || err == nil {
			t.Errorf("manager.Cancel() = %v, expected to fail", task)
		}
		cancelErr <- err
	}()
	<-canceler.cancelCalled

	executor.mustWrite(t, &a2a.Task{ID: subscription.TaskID(), Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}})
	close(canceler.block)

	if got := <-cancelErr; !errors.Is(got, a2a.ErrTaskNotCancelable) {
		t.Fatalf("manager.Cancel() = %v, want %v", got, a2a.ErrTaskNotCancelable)
	}
}

func TestManager_ConcurrentCancelationsResolveToTheSameResult(t *testing.T) {
	t.Parallel()
	ctx, tid := t.Context(), a2a.NewTaskID()

	canceler1 := newCanceler()
	canceler1.nextEventTerminal = true
	canceler1.block = make(chan struct{})

	canceler2 := newCanceler()
	canceler2.cancelErr = errors.New("test error") // this should never be returned

	var callCount atomic.Int32
	manager := NewLocalManager(LocalManagerConfig{
		Factory: &testFactory{
			CreateCancelerFn: func(context.Context, *a2a.TaskIDParams) (Canceler, Processor, error) {
				if callCount.CompareAndSwap(0, 1) {
					return canceler1, canceler1, nil
				} else {
					return canceler2, canceler2, nil
				}
			},
		},
	})

	var wg sync.WaitGroup
	wg.Add(2)
	results := make(chan *a2a.Task, 2)

	go func() {
		task, err := manager.Cancel(ctx, &a2a.TaskIDParams{})
		if err != nil {
			t.Errorf("manager.Cancel() failed: %v", err)
		}
		results <- task
		wg.Done()
	}()
	<-canceler1.cancelCalled

	ready := make(chan struct{})
	go func() {
		close(ready)
		task, err := manager.Cancel(ctx, &a2a.TaskIDParams{})
		if err != nil {
			t.Errorf("manager.Cancel() failed: %v", err)
		}
		results <- task
		wg.Done()
	}()
	<-ready
	time.Sleep(10 * time.Millisecond)

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
	ctx, tid := t.Context(), a2a.NewTaskID()

	canceler := newCanceler()
	manager := newStaticExecutorManager(nil, canceler)
	canceler.block = make(chan struct{})
	canceler.cancelErr = errors.New("test error")
	done := make(chan struct{})
	go func() {
		_, _ = manager.Cancel(ctx, &a2a.TaskIDParams{ID: tid})
		close(done)
	}()
	<-canceler.cancelCalled

	subscription, err := manager.Execute(ctx, &a2a.MessageSendParams{
		Message: a2a.NewMessageForTask(a2a.MessageRoleUser, &a2a.Task{ID: tid}),
	})
	if subscription != nil || !errors.Is(err, ErrCancelationInProgress) {
		t.Fatalf("manager.Execute() = (%v, %v), want %v", subscription, err, ErrCancelationInProgress)
	}

	close(canceler.block)
	<-done
}

func TestManager_CanExecuteAfterCancelFailed(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	// First cancelation fails
	canceler := newCanceler()
	canceler.cancelErr = errors.New("test error")

	executor := newExecutor()
	executor.nextEventTerminal = true

	manager := newStaticExecutorManager(executor, canceler)

	if task, err := manager.Cancel(ctx, &a2a.TaskIDParams{}); err == nil {
		t.Fatalf("manager.Cancel() = %v, want error", task)
	}

	subscription, err := manager.Execute(ctx, &a2a.MessageSendParams{})
	_, executionErr := consumeEvents(t, subscription)
	if err != nil {
		t.Fatalf("manager.Execute() failed with %v", err)
	}

	<-executor.executeCalled
	executor.mustWrite(t, &a2a.Task{ID: subscription.TaskID()})

	if err := <-executionErr; err != nil {
		t.Fatalf("execution.Result() wailed with %v", err)
	}
}

func TestManager_CanCancelAfterCancelFailed(t *testing.T) {
	ctx, tid := t.Context(), a2a.NewTaskID()

	// First cancelation fails
	canceler1 := newCanceler()
	canceler1.cancelErr = errors.New("test error")

	// Second cancelation succeeds
	canceler2 := newCanceler()
	canceler2.nextEventTerminal = true
	go func() {
		<-canceler2.cancelCalled
		canceler2.mustWrite(t, &a2a.Task{ID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateCanceled}})
	}()

	callCount := 0
	manager := NewLocalManager(LocalManagerConfig{
		Factory: &testFactory{
			CreateCancelerFn: func(context.Context, *a2a.TaskIDParams) (Canceler, Processor, error) {
				callCount++
				if callCount == 1 {
					return canceler1, canceler1, nil
				} else {
					return canceler2, canceler2, nil
				}
			},
		},
	})

	if task, err := manager.Cancel(ctx, &a2a.TaskIDParams{}); err == nil {
		t.Fatalf("manager.Cancel() = %v, want error", task)
	}

	if _, err := manager.Cancel(ctx, &a2a.TaskIDParams{}); err != nil {
		t.Errorf("manager.Cancel() failed with %v", err)
	}
}

func TestManager_GetExecution(t *testing.T) {
	ctx := t.Context()

	executor := newExecutor()
	manager := newStaticExecutorManager(executor, nil)
	executor.nextEventTerminal = true
	subscription, err := manager.Execute(ctx, &a2a.MessageSendParams{})
	executionResult, _ := consumeEvents(t, subscription)
	if err != nil {
		t.Fatalf("manager.Execute() failed: %v", err)
	}

	tid := subscription.TaskID()
	if _, err := manager.Resubscribe(ctx, tid); err != nil {
		t.Fatal("manager.Resubscribe() failed")
	}

	<-executor.executeCalled
	executor.mustWrite(t, &a2a.Task{ID: tid})
	<-executionResult

	if _, err := manager.Resubscribe(ctx, tid); err == nil {
		t.Fatal("manager.Resubscribe() succeeded for finished execution")
	}
}
