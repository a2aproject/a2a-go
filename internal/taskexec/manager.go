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

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
	"github.com/a2aproject/a2a-go/a2asrv/limiter"
	"github.com/a2aproject/a2a-go/log"
)

var (
	// ErrExecutionInProgress is returned when a caller attempts to start an execution for
	// a Task concurrently with another execution.
	ErrExecutionInProgress = errors.New("task execution is already in progress")
	// ErrCancelationInProgress is returned when a caller attempts to start an execution for
	// a Task concurrently with its cancelation.
	ErrCancelationInProgress = errors.New("task cancelation is in progress")
)

// LocalManager provides an API for executing and canceling tasks in a way that ensures
// concurrent calls don't interfere with one another in unexpected ways.
// The following guarantees are provided:
//   - If a Task is being canceled, a concurrent Execution can't be started.
//   - If a Task is being canceled, a concurrent cancelation will await the existing cancelation.
//   - If a Task is being executed, a concurrent cancelation will have the same result as the execution.
//   - If a Task is being executed, a concurrent execution will be rejected.
//
// Both cancelations and executions are started in detached context and run until completion.
type LocalManager struct {
	queueManager eventqueue.Manager
	factory      Factory

	mu           sync.Mutex
	executions   map[a2a.TaskID]*localExecution
	cancelations map[a2a.TaskID]*cancelation
	limiter      *concurrencyLimiter
}

// Config contains Manager configuration parameters.
type Config struct {
	QueueManager      eventqueue.Manager
	ConcurrencyConfig limiter.ConcurrencyConfig
	Factory           Factory
}

// NewLocalManager is a [LocalManager] constructor function.
func NewLocalManager(cfg Config) *LocalManager {
	manager := &LocalManager{
		queueManager: cfg.QueueManager,
		factory:      cfg.Factory,
		limiter:      newConcurrencyLimiter(cfg.ConcurrencyConfig),
		executions:   make(map[a2a.TaskID]*localExecution),
		cancelations: make(map[a2a.TaskID]*cancelation),
	}
	if manager.queueManager == nil {
		manager.queueManager = eventqueue.NewInMemoryManager()
	}
	return manager
}

// GetExecution is used to get a reference to an active [Execution]. The method can be used
// to resubscribe to execution events or wait for its completion.
func (m *LocalManager) GetExecution(taskID a2a.TaskID) (Execution, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	execution, ok := m.executions[taskID]
	return execution, ok
}

// Execute starts two goroutine in a detached context. One will invoke [Executor] for event generation and
// the other one will be processing events passed through an [eventqueue.Queue].
// There can only be a single active execution per TaskID.
func (m *LocalManager) Execute(ctx context.Context, tid a2a.TaskID, params *a2a.MessageSendParams) (Execution, Subscription, error) {
	execution, err := m.createExecution(ctx, tid, params)
	if err != nil {
		return nil, nil, err
	}

	eventBroadcastQueue, err := m.queueManager.GetOrCreate(ctx, tid)
	if err != nil {
		m.cleanupExecution(ctx, execution)
		return nil, nil, fmt.Errorf("failed to create a queue: %w", err)
	}

	defaultSubReadQueue, ok := m.queueManager.Get(ctx, tid)
	if !ok {
		m.cleanupExecution(ctx, execution)
		return nil, nil, fmt.Errorf("failed to create a default subscription event queue: %w", err)
	}

	detachedCtx := context.WithoutCancel(ctx)

	go m.handleExecution(detachedCtx, execution, eventBroadcastQueue)

	return execution, newLocalSubscription(execution, defaultSubReadQueue), nil
}

func (m *LocalManager) createExecution(ctx context.Context, tid a2a.TaskID, params *a2a.MessageSendParams) (*localExecution, error) {
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

	execution := newLocalExecution(m.queueManager, tid, params)
	m.executions[tid] = execution

	return execution, nil
}

// Cancel uses [Canceler] to signal task cancelation and waits for it to take effect.
// If there's a cancelation in progress we wait for its result instead of starting a new one.
// If there's an active [Execution] Canceler will be writing to the same result queue. Consumers
// subscribed to the Execution will receive a task cancelation event and handle it accordingly.
// If there's no active Execution Canceler will be processing task events.
func (m *LocalManager) Cancel(ctx context.Context, params *a2a.TaskIDParams) (*a2a.Task, error) {
	m.mu.Lock()
	tid := params.ID
	execution := m.executions[tid]
	cancel, cancelInProgress := m.cancelations[tid]

	if cancel == nil {
		cancel = newCancelation(params)
		m.cancelations[tid] = cancel
	}
	m.mu.Unlock()

	if cancelInProgress {
		return cancel.wait(ctx)
	}

	detachedCtx := context.WithoutCancel(ctx)

	if execution != nil {
		go m.handleCancelWithConcurrentRun(detachedCtx, cancel, execution)
	} else {
		go m.handleCancel(detachedCtx, cancel)
	}

	return cancel.wait(ctx)
}

func (m *LocalManager) cleanupExecution(ctx context.Context, execution *localExecution) {
	m.destroyQueue(ctx, execution.tid)

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
func (m *LocalManager) handleExecution(ctx context.Context, execution *localExecution, eventBroadcast eventqueue.Writer) {
	defer m.cleanupExecution(ctx, execution)

	executor, processor, err := m.factory.CreateExecutor(ctx, execution.tid, execution.params)
	if err != nil {
		execution.result.setError(fmt.Errorf("setup failed: %w", err))
		m.destroyQueue(ctx, execution.tid)
		return
	}

	result, err := runProducerConsumer(
		ctx,
		func(ctx context.Context) error { return executor.Execute(ctx, execution.pipe.Writer) },
		func(ctx context.Context) (a2a.SendMessageResult, error) {
			return execution.processEvents(ctx, processor, eventBroadcast)
		},
	)

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
func (m *LocalManager) handleCancel(ctx context.Context, cancel *cancelation) {
	defer func() {
		m.mu.Lock()
		delete(m.cancelations, cancel.params.ID)
		cancel.result.signalDone()
		m.mu.Unlock()
	}()

	canceler, processor, err := m.factory.CreateCanceler(ctx, cancel.params)
	if err != nil {
		cancel.result.setError(fmt.Errorf("setup failed: %w", err))
		return
	}

	result, err := runProducerConsumer(
		ctx,
		func(ctx context.Context) error { return canceler.Cancel(ctx, cancel.pipe.Writer) },
		func(ctx context.Context) (a2a.SendMessageResult, error) {
			return processEvents(ctx, cancel.pipe.Reader, processor)
		},
	)
	if err != nil {
		cancel.result.setError(err)
		return
	}
	cancel.result.setValue(result)
}

// Sends a cancelation request on the queue which is being used by an active execution.
// Then waits for the execution to complete and resolves cancelation to the same result.
func (m *LocalManager) handleCancelWithConcurrentRun(ctx context.Context, cancel *cancelation, run *localExecution) {
	defer func() {
		if r := recover(); r != nil {
			cancel.result.setError(fmt.Errorf("task cancelation panic: %v", r))
		}
	}()

	defer func() {
		m.mu.Lock()
		delete(m.cancelations, cancel.params.ID)
		cancel.result.signalDone()
		m.mu.Unlock()
	}()

	canceler, _, err := m.factory.CreateCanceler(ctx, cancel.params)
	if err != nil {
		cancel.result.setError(fmt.Errorf("setup failed: %w", err))
		return
	}

	// TODO(yarolegovich): better handling for concurrent Execute() and Cancel() calls.
	// Currently we try to send a cancelation signal on the same queue which active execution uses for events.
	// This means a cancelation will fail if the concurrent execution fails or resolves to a
	// non-terminal state (eg. input-required) before receiving the cancelation signal.
	// In this case our cancel will resolve to ErrTaskNotCancelable. It would probably be more
	// correct to restart the cancelation as if there was no concurrent execution at the moment of Cancel call.
	if err := canceler.Cancel(ctx, run.pipe.Writer); err != nil {
		cancel.result.setError(err)
		return
	}

	result, err := run.Result(ctx)
	if err != nil {
		cancel.result.setError(err)
		return
	}

	cancel.result.setValue(result)
}

func (m *LocalManager) destroyQueue(ctx context.Context, tid a2a.TaskID) {
	// TODO(yarolegovich): consider not destroying queues until a Task reaches terminal state
	if err := m.queueManager.Destroy(ctx, tid); err != nil {
		log.Error(ctx, "failed to destroy a queue", err)
	}
}
