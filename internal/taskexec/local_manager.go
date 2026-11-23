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
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv/eventqueue"
	"github.com/a2aproject/a2a-go/v2/a2asrv/limiter"
	"github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"
	"github.com/a2aproject/a2a-go/v2/internal/eventpipe"
	"github.com/a2aproject/a2a-go/v2/log"
)

var (
	// ErrExecutionInProgress is returned when a caller attempts to start an execution for
	// a Task concurrently with another execution.
	ErrExecutionInProgress = a2a.NewError(a2a.ErrUnsupportedOperation, "task execution is already in progress")
	// ErrCancelationInProgress is returned when a caller attempts to start an execution for
	// a Task concurrently with its cancelation.
	ErrCancelationInProgress = a2a.NewError(a2a.ErrUnsupportedOperation, "task cancelation is in progress")
)

// localManager provides an API for executing and canceling tasks in a way that ensures
// concurrent calls don't interfere with one another in unexpected ways.
// The following guarantees are provided:
//   - If a Task is being canceled, a concurrent Execution can't be started.
//   - If a Task is being canceled, a concurrent cancelation will await the existing cancelation.
//   - If a Task is being executed, a concurrent cancelation shares its events and observes its terminal result.
//   - If a Task is being executed, a concurrent execution will be rejected.
//
// Both cancelations and executions are started in detached context and run until completion.
// The type is suitable only for single-process execution management.
type localManager struct {
	queueManager      eventqueue.Manager
	factory           Factory
	store             taskstore.Store
	panicHandler      PanicHandlerFn
	inactivityTimeout time.Duration

	mu           sync.Mutex
	executions   map[a2a.TaskID]*localExecution
	cancelations map[a2a.TaskID]*cancelation
	limiter      *concurrencyLimiter
}

type cancelation struct {
	req    *a2a.CancelTaskRequest
	result *promise
}

type localExecution struct {
	tid    a2a.TaskID
	req    *a2a.SendMessageRequest
	result *promise

	pipe         *eventpipe.Local
	queueManager eventqueue.Manager
	store        taskstore.Store

	// exiting flag is set to true after we know the execution is about to finish.
	// a fast client might attempt to send a follow-up before the execution gets unregistered.
	exiting          atomic.Bool
	consumerDone     chan struct{}
	consumerDoneOnce sync.Once
}

var _ Manager = (*localManager)(nil)

// LocalManagerConfig contains in-process execution Manager configuration parameters.
type LocalManagerConfig struct {
	QueueManager      eventqueue.Manager
	ConcurrencyConfig limiter.ConcurrencyConfig
	Factory           Factory
	TaskStore         taskstore.Store
	PanicHandler      PanicHandlerFn
	// AgentInactivityTimeout, if positive, terminates an execution when the
	// agent's producer has not written any events to the pipe for the
	// configured duration. The terminating cause is [ErrAgentInactivityTimeout].
	// A value of 0 disables the watcher, preserving prior behavior.
	AgentInactivityTimeout time.Duration
}

// NewLocalManager is a [localManager] constructor function.
func NewLocalManager(cfg LocalManagerConfig) Manager {
	manager := &localManager{
		queueManager:      cfg.QueueManager,
		factory:           cfg.Factory,
		store:             cfg.TaskStore,
		panicHandler:      cfg.PanicHandler,
		inactivityTimeout: cfg.AgentInactivityTimeout,
		limiter:           newConcurrencyLimiter(cfg.ConcurrencyConfig),
		executions:        make(map[a2a.TaskID]*localExecution),
		cancelations:      make(map[a2a.TaskID]*cancelation),
	}
	if manager.queueManager == nil {
		manager.queueManager = eventqueue.NewInMemoryManager()
	}
	return manager
}

func newCancelation(req *a2a.CancelTaskRequest) *cancelation {
	return &cancelation{req: req, result: newPromise()}
}

func newLocalExecution(qm eventqueue.Manager, store taskstore.Store, tid a2a.TaskID, req *a2a.SendMessageRequest) *localExecution {
	return &localExecution{
		tid:          tid,
		req:          req,
		queueManager: qm,
		store:        store,
		pipe:         eventpipe.NewLocal(),
		result:       newPromise(),
		consumerDone: make(chan struct{}),
	}
}

func (m *localManager) Resubscribe(ctx context.Context, taskID a2a.TaskID) (Subscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	execution, ok := m.executions[taskID]
	if !ok {
		return nil, fmt.Errorf("no active execution")
	}
	queue, err := m.queueManager.CreateReader(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("no queue for active execution")
	}
	subscription := newLocalSubscription(execution, queue)
	subscription.startWithTask = true
	return subscription, nil
}

// Execute starts two goroutine in a detached context. One will invoke [Executor] for event generation and
// the other one will be processing events passed through an [eventqueue.Queue].
// There can only be a single active execution per TaskID.
func (m *localManager) Execute(ctx context.Context, req *a2a.SendMessageRequest) (Subscription, error) {
	var tid a2a.TaskID
	if req.Message == nil || len(req.Message.TaskID) == 0 {
		tid = a2a.NewTaskID()
	} else {
		tid = req.Message.TaskID
		// handle fast client sending a follow-up message before execution was unregistered
		if err := m.waitForExiting(ctx, tid); err != nil {
			return nil, err
		}
	}

	execution, err := m.createExecution(ctx, tid, req)
	if err != nil {
		return nil, fmt.Errorf("create execution failure: %w", err)
	}

	eventBroadcastQueue, err := m.queueManager.CreateWriter(ctx, tid)
	if err != nil {
		m.cleanupExecution(ctx, execution)
		return nil, fmt.Errorf("failed to create a queue: %w", err)
	}

	defaultSubReadQueue, err := m.queueManager.CreateReader(ctx, tid)
	if err != nil {
		m.cleanupExecution(ctx, execution)
		return nil, fmt.Errorf("failed to create a default subscription event queue: %w", err)
	}

	detachedCtx := context.WithoutCancel(ctx)

	go m.handleExecution(detachedCtx, execution, eventBroadcastQueue)

	return newLocalSubscription(execution, defaultSubReadQueue), nil
}

func (m *localManager) waitForExiting(ctx context.Context, tid a2a.TaskID) error {
	m.mu.Lock()
	exec, ok := m.executions[tid]
	m.mu.Unlock()
	if !ok {
		return nil
	}
	if !exec.exiting.Load() {
		return ErrExecutionInProgress
	}
	_, _ = exec.result.wait(ctx)
	return ctx.Err()
}

func (m *localManager) createExecution(ctx context.Context, tid a2a.TaskID, req *a2a.SendMessageRequest) (*localExecution, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// TODO(yarolegovich): handle idempotency once spec establishes the key. We can return
	// an execution in progress here and decide whether to tap it or not on the caller side.
	if _, ok := m.executions[tid]; ok {
		return nil, ErrExecutionInProgress
	}

	if _, ok := m.cancelations[tid]; ok {
		return nil, ErrCancelationInProgress
	}

	if err := m.limiter.acquireQuotaLocked(ctx); err != nil {
		return nil, fmt.Errorf("concurrency quota exceeded: %w", err)
	}

	execution := newLocalExecution(m.queueManager, m.store, tid, req)
	m.executions[tid] = execution

	return execution, nil
}

// Cancel uses [Canceler] to signal task cancelation and waits for it to take effect.
// If there's a cancelation in progress we wait for its result instead of starting a new one.
// If there's an active [execution] Canceler will be writing to the same result queue. Consumers
// subscribed to the Execution will receive a task cancelation event and handle it accordingly.
// If there's no active Execution Canceler will be processing task events.
func (m *localManager) Cancel(ctx context.Context, req *a2a.CancelTaskRequest) (*a2a.Task, error) {
	m.mu.Lock()
	tid := req.ID
	execution := m.executions[tid]
	cancel, cancelInProgress := m.cancelations[tid]

	if cancel == nil {
		cancel = newCancelation(req)
		m.cancelations[tid] = cancel
	}
	m.mu.Unlock()

	if !cancelInProgress {
		detachedCtx := context.WithoutCancel(ctx)
		if execution != nil {
			go m.handleCancelWithConcurrentRun(detachedCtx, cancel, execution)
		} else {
			go m.handleCancel(detachedCtx, cancel)
		}
	} else {
		log.Debug(ctx, "waiting for existing cancelation result")
	}

	result, err := cancel.result.wait(ctx)
	return convertToCancelationResult(ctx, result, err)
}

func (m *localManager) cleanupExecution(ctx context.Context, execution *localExecution) {
	m.destroyQueue(ctx, execution.tid)
	execution.pipe.Close()

	m.mu.Lock()
	m.limiter.releaseQuotaLocked(ctx)
	delete(m.executions, execution.tid)
	execution.result.signalDone()
	m.mu.Unlock()
}

// Uses an errogroup to start two goroutines.
// Execution is started in one of them. Another is processing events until a result or error
// is returned.
// The returned value is set as Execution result.
func (m *localManager) handleExecution(ctx context.Context, execution *localExecution, eventBroadcast eventqueue.Writer) {
	defer m.cleanupExecution(ctx, execution)
	defer execution.consumerDoneOnce.Do(func() { close(execution.consumerDone) })

	executor, processor, cleaner, err := m.factory.CreateExecutor(ctx, execution.tid, execution.req)
	if err != nil {
		log.Error(ctx, "executor setup failed", err)
		execution.result.setError(fmt.Errorf("executor setup failed: %w", err))
		return
	}

	handler := &executionHandler{
		agentEvents:       execution.pipe.Reader,
		handledEventQueue: eventBroadcast,
		handleEventFn: func(ctx context.Context, e a2a.Event) (*ProcessorResult, error) {
			result, err := processor.Process(ctx, e)
			execution.exiting.Store(err != nil || result.ExecutionResult != nil)
			return result, err
		},
		handleErrorFn: processor.ProcessError,
	}
	tracker := newInactivityTracker(m.inactivityTimeout)
	producerWriter := newActivityTrackingWriter(execution.pipe.Writer, tracker)
	result, err := runProducerConsumer(
		ctx,
		func(ctx context.Context) error { return executor.Execute(ctx, producerWriter) },
		func(ctx context.Context) (a2a.SendMessageResult, error) {
			defer execution.consumerDoneOnce.Do(func() { close(execution.consumerDone) })
			return handler.processEvents(ctx)
		},
		nil,
		m.panicHandler,
		tracker,
	)

	cleaner.Cleanup(ctx, result, err)

	if err != nil {
		execution.result.setError(err)
		return
	}
	execution.result.setValue(result)
}

// Uses an errogroup to start two goroutines.
// Cancelation is started in on of them. Another is processing events until a result or error
// is returned.
// The returned value is set as Cancelation result.
func (m *localManager) handleCancel(ctx context.Context, cancel *cancelation) {
	defer func() {
		m.mu.Lock()
		delete(m.cancelations, cancel.req.ID)
		cancel.result.signalDone()
		m.mu.Unlock()
	}()

	canceler, processor, cleaner, err := m.factory.CreateCanceler(ctx, cancel.req)
	if err != nil {
		log.Error(ctx, "canceler setup failed", err)
		cancel.result.setError(fmt.Errorf("canceler setup failed: %w", err))
		return
	}

	pipe := eventpipe.NewLocal()
	defer pipe.Close()

	handler := &executionHandler{
		agentEvents:   pipe.Reader,
		handleEventFn: processor.Process,
		handleErrorFn: func(ctx context.Context, err error) (a2a.SendMessageResult, error) { return nil, err },
	}
	tracker := newInactivityTracker(m.inactivityTimeout)
	producerWriter := newActivityTrackingWriter(pipe.Writer, tracker)
	result, err := runProducerConsumer(
		ctx,
		func(ctx context.Context) error { return canceler.Cancel(ctx, producerWriter) },
		handler.processEvents,
		nil,
		m.panicHandler,
		tracker,
	)

	cleaner.Cleanup(ctx, result, err)

	if err != nil {
		cancel.result.setError(err)
		return
	}
	cancel.result.setValue(result)
}

func (m *localManager) cancelOnPipe(ctx context.Context, canceler Canceler, pipe eventpipe.Writer) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = m.cancelationPanicError(ctx, r)
		}
	}()
	return canceler.Cancel(ctx, pipe)
}

func (m *localManager) cancelationPanicError(ctx context.Context, r any) error {
	if m.panicHandler != nil {
		return m.panicHandler(r)
	}
	err := fmt.Errorf("task cancelation panicked: %v\n%s", r, debug.Stack())
	log.Warn(ctx, "cancelation panicked", "error", err)
	return err
}

type cancelHandoffWriter struct {
	run     *localExecution
	mu      sync.Mutex
	records []cancelHandoffRecord
}

type cancelHandoffRecord struct {
	event a2a.Event
	read  <-chan struct{}
}

func (w *cancelHandoffWriter) Write(ctx context.Context, event a2a.Event) error {
	read, err := w.run.pipe.WriteTracked(ctx, event, w.run.consumerDone)
	if errors.Is(err, eventqueue.ErrQueueClosed) {
		result, waitErr := w.run.result.wait(ctx)
		if waitErr != nil {
			return waitErr
		}
		task, ok := result.(*a2a.Task)
		if !ok || task.Status.State.Terminal() {
			return eventqueue.ErrQueueClosed
		}
		err = nil
	}
	if err != nil {
		return err
	}
	w.mu.Lock()
	w.records = append(w.records, cancelHandoffRecord{event: event, read: read})
	w.mu.Unlock()
	return nil
}

func (w *cancelHandoffWriter) pending() []a2a.Event {
	w.mu.Lock()
	defer w.mu.Unlock()
	var pending []a2a.Event
	for _, r := range w.records {
		if r.read != nil {
			select {
			case <-r.read:
				continue
			default:
			}
		}
		pending = append(pending, r.event)
	}
	return pending
}

func (m *localManager) processPendingCancelEvents(ctx context.Context, req *a2a.CancelTaskRequest, events []a2a.Event) (a2a.SendMessageResult, error) {
	processor, err := m.factory.CreateCancelProcessor(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("canceler processor setup failed: %w", err)
	}
	for _, event := range events {
		result, err := processor.Process(ctx, event)
		if err != nil {
			return nil, fmt.Errorf("processor error: %w", err)
		}
		if result.ExecutionResult != nil {
			return result.ExecutionResult, nil
		}
	}
	return nil, fmt.Errorf("cancelation finished without a final result: %w", a2a.ErrInvalidAgentResponse)
}

// Sends cancelation events to the active execution, then processes any unread events
// after a nonterminal execution result using a fresh task snapshot.
func (m *localManager) handleCancelWithConcurrentRun(ctx context.Context, cancel *cancelation, run *localExecution) {
	defer func() {
		if r := recover(); r != nil {
			cancel.result.setError(m.cancelationPanicError(ctx, r))
		}
		m.mu.Lock()
		delete(m.cancelations, cancel.req.ID)
		cancel.result.signalDone()
		m.mu.Unlock()
	}()

	canceler, _, cleaner, err := m.factory.CreateCanceler(ctx, cancel.req)
	if err != nil {
		log.Error(ctx, "task cancelation setup failed", err)
		cancel.result.setError(fmt.Errorf("setup failed: %w", err))
		return
	}

	writer := &cancelHandoffWriter{run: run}
	if err := m.cancelOnPipe(ctx, canceler, writer); err != nil {
		// The pipe is closed after the execution finishes. This happens when the agent ends the execution in
		// response to the cancelation before the canceler writes its events, so we resolve to the execution result.
		if !errors.Is(err, eventqueue.ErrQueueClosed) {
			cleaner.Cleanup(ctx, nil, err)
			cancel.result.setError(err)
			return
		}
		log.Debug(ctx, "execution finished before the cancelation was written")
	}

	log.Debug(ctx, "waiting for concurrent cancelation result")

	result, err := run.result.wait(ctx)
	if err == nil {
		if task, ok := result.(*a2a.Task); ok && !task.Status.State.Terminal() {
			if pending := writer.pending(); len(pending) > 0 {
				result, err = m.processPendingCancelEvents(ctx, cancel.req, pending)
			}
		}
	}

	cleaner.Cleanup(ctx, result, err)

	if err != nil {
		cancel.result.setError(err)
		return
	}
	cancel.result.setValue(result)
}

func (m *localManager) destroyQueue(ctx context.Context, tid a2a.TaskID) {
	if err := m.queueManager.Destroy(ctx, tid); err != nil {
		log.Error(ctx, "failed to destroy a queue", err)
	}
}
