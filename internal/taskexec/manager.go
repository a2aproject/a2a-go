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
	"golang.org/x/sync/errgroup"
)

var (
	ErrExecutionInProgress   = errors.New("task execution is already in progress")
	ErrCancelationInProgress = errors.New("task cancelation is in progress")
)

// Manager provides an API for executing and canceling tasks in a way that ensures
// concurrent calls don't interfere with one another in unexpected ways.
// The following guarantees are provided:
//   - If a Task is being canceled, a concurrent Execution can't be started.
//   - If a Task is being canceled, a concurrent cancelation will await the existing cancelation.
//   - If a Task is being executed, a concurrent cancelation will have the same result as the execution.
//   - If a Task is being executed, a concurrent execution will be rejected.
//
// Both cancelations and executions are started in detached context and run until completion.
type Manager struct {
	queueManager eventqueue.Manager

	mu           sync.Mutex
	executions   map[a2a.TaskID]*Execution
	cancelations map[a2a.TaskID]*cancelation
}

func NewManager(queueManager eventqueue.Manager) *Manager {
	return &Manager{
		queueManager: queueManager,
		executions:   make(map[a2a.TaskID]*Execution),
		cancelations: make(map[a2a.TaskID]*cancelation),
	}
}

// GetExecution can be used to resubscribe to events which are being produced by agentExecution.
func (m *Manager) GetExecution(taskID a2a.TaskID) (*Execution, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	execution, ok := m.executions[taskID]
	return execution, ok
}

// Execute starts an AgentExecutor in a separate goroutine with a detached context.
// There can only be a single active execution per TaskID.
func (m *Manager) Execute(ctx context.Context, tid a2a.TaskID, executor Executor) (*Execution, error) {
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

	execution := newExecution(tid, executor)
	m.executions[tid] = execution

	detachedCtx := context.WithoutCancel(ctx)

	go m.handleExecution(detachedCtx, execution)

	return execution, nil
}

// Cancel uses Canceler to finish execution and waits for it to finish.
// If there's a cancelation in progress we wait for its result instead of starting a new attempt.
// If there's an active Execution Canceler will be writing to the same result queue. Consumers
// subscribed to the Execution will receive a Task cancelation Event.
// If there's no active Execution Canceler is responsible for processing Task events.
func (m *Manager) Cancel(ctx context.Context, tid a2a.TaskID, canceler Canceler) (*a2a.Task, error) {
	m.mu.Lock()
	execution := m.executions[tid]
	cancel, cancelInProgress := m.cancelations[tid]

	if cancel == nil {
		cancel = newCancelation(tid, canceler)
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

// Uses an errogroup to start two goroutines.
// Execution is started in on of them. Another is processing events until a result or error
// is returned.
// The returned value is set as Execution result.
func (m *Manager) handleExecution(ctx context.Context, execution *Execution) {
	defer func() {
		m.mu.Lock()
		delete(m.executions, execution.tid)
		m.mu.Unlock()
	}()

	queue, err := m.queueManager.GetOrCreate(ctx, execution.tid)
	if err != nil {
		execution.result.reject(fmt.Errorf("queue creation failed: %w", err))
		return
	}
	defer m.destroyQueue(ctx, execution.tid)

	group, ctx := errgroup.WithContext(ctx)

	group.Go(func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("task execution panic: %v", r)
			}
		}()
		err = execution.controller.Execute(ctx, queue)
		return
	})

	handleEvents(ctx, group, execution.result, func(context.Context) (a2a.SendMessageResult, error) {
		return execution.processEvents(ctx, queue)
	})
}

// Uses an errogroup to start two goroutines.
// Cancelation is started in on of them. Another is processing events until a result or error
// is returned.
// The returned value is set as Cancelation result.
func (m *Manager) handleCancel(ctx context.Context, cancel *cancelation) {
	defer func() {
		m.mu.Lock()
		delete(m.cancelations, cancel.tid)
		m.mu.Unlock()
	}()

	queue, err := m.queueManager.GetOrCreate(ctx, cancel.tid)
	if err != nil {
		cancel.result.reject(fmt.Errorf("queue creation failed: %w", err))
		return
	}
	defer m.destroyQueue(ctx, cancel.tid)

	group, ctx := errgroup.WithContext(ctx)

	group.Go(func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("task cancelation panic: %v", r)
			}
		}()
		err = cancel.canceler.Cancel(ctx, queue)
		return
	})

	handleEvents(ctx, group, cancel.result, func(context.Context) (a2a.SendMessageResult, error) {
		return cancel.processEvents(ctx, queue)
	})
}

// Sends a cancelation request on the queue which is being used by an active execution.
// Then waits for the execution to complete and resolves cancelation to the same result.
func (m *Manager) handleCancelWithConcurrentRun(ctx context.Context, cancel *cancelation, run *Execution) {
	defer func() {
		if r := recover(); r != nil {
			cancel.result.reject(fmt.Errorf("task cancelation panic: %v", r))
		}
	}()

	defer func() {
		m.mu.Lock()
		delete(m.cancelations, cancel.tid)
		m.mu.Unlock()
	}()

	// TODO(yarolegovich): better handling for concurrent Execute() and Cancel() calls.
	// Currently we try to send a cancelation signal on the same queue which active execution uses for events.
	// This means a cancelation will fail if the concurrent execution fails or resolves to a
	// non-terminal state (eg. input-required) before receiving the cancelation signal.
	// In this case our cancel will resolve to ErrTaskNotCancelable. It would probably be more
	// correct to restart the cancelation as if there was no concurrent execution at the moment of Cancel call.
	if queue, ok := m.queueManager.Get(ctx, cancel.tid); ok {
		if err := cancel.canceler.Cancel(ctx, queue); err != nil {
			cancel.result.reject(err)
			return
		}
	}

	result, err := run.Result(ctx)
	if err != nil {
		cancel.result.reject(err)
		return
	}

	cancel.result.resolve(result)
}

type eventHandlerFn func(context.Context) (a2a.SendMessageResult, error)

// Event producer is supposed to be started by the caller.
// Starts an event-processor goroutine in the provided error group and consumes events
// until a result is returned.
// The result is on the provided promise once group.Wait() returns.
func handleEvents(ctx context.Context, group *errgroup.Group, r *promise, handler eventHandlerFn) {
	var result *a2a.SendMessageResult
	group.Go(func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("task event consumer panic: %v", r)
			}
		}()
		localResult, err := handler(ctx)
		result = &localResult
		return
	})

	if err := group.Wait(); err != nil {
		r.reject(err)
		return
	}

	if result == nil {
		r.reject(fmt.Errorf("bug: no error returned, but result unset"))
		return
	}

	r.resolve(*result)
}

func (m *Manager) destroyQueue(ctx context.Context, tid a2a.TaskID) {
	// TODO(yarolegovich): log if destroy fails
	// TODO(yarolegovich): consider not destroying queues until a Task reaches terminal state
	_ = m.queueManager.Destroy(ctx, tid)
}
