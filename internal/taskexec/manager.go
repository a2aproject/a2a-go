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
	ErrExecutionInProgress    = errors.New("task execution is already in progress")
	ErrCancellationInProgress = errors.New("task cancellation is in progress")
)

// Manager provides an API for executing and canceling tasks in a way that ensures
// concurrent calls don't interfere with one another in unexpected ways.
// The following guarantees are provided:
//   - If a Task is being cancelled, a concurrent Execution can't be started.
//   - If a Task is being cancelled, a concurrent cancellation will await the existing cancellation.
//   - If a Task is being executed, a concurrent cancellation will have the same result as the execution.
//   - If a Task is being executed, a concurrent execution will be rejected.
//
// Both cancellations and executions are started in detached context and run until completion.
type Manager struct {
	queueManager eventqueue.Manager

	mu            sync.Mutex
	executions    map[a2a.TaskID]*Execution
	cancellations map[a2a.TaskID]*cancellation
}

func NewManager(queueManager eventqueue.Manager) *Manager {
	return &Manager{
		queueManager:  queueManager,
		executions:    make(map[a2a.TaskID]*Execution),
		cancellations: make(map[a2a.TaskID]*cancellation),
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

	// TODO(yarolegovich): handle idempotency once spec establishes the key.
	// We can return an execution in progress here and decide whether to tap into it or not
	// on the caller side.
	if _, ok := m.executions[tid]; ok {
		return nil, ErrExecutionInProgress
	}

	if _, ok := m.cancellations[tid]; ok {
		return nil, ErrCancellationInProgress
	}

	execution := newExecution(tid, executor)
	m.executions[tid] = execution

	detachedCtx := context.WithoutCancel(ctx)

	go m.handleExecution(detachedCtx, execution)

	return execution, nil
}

// Cancel requests AgentExecutor to stop the execution and waits for it to finish.
func (m *Manager) Cancel(ctx context.Context, tid a2a.TaskID, canceller Canceller) (*a2a.Task, error) {
	m.mu.Lock()
	execution := m.executions[tid]
	cancel, cancelInProgress := m.cancellations[tid]

	if cancel == nil {
		cancel = newCancellation(tid, canceller)
		m.cancellations[tid] = cancel
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

func (m *Manager) handleCancel(ctx context.Context, cancel *cancellation) {
	defer func() {
		m.mu.Lock()
		delete(m.cancellations, cancel.tid)
		m.mu.Unlock()

		_ = m.queueManager.Destroy(ctx, cancel.tid)
	}()

	queue, err := m.queueManager.GetOrCreate(ctx, cancel.tid)
	if err != nil {
		cancel.result.reject(fmt.Errorf("queue creation failed: %w", err))
		return
	}

	group, ctx := errgroup.WithContext(ctx)

	group.Go(func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("task cancellation panic: %v", r)
			}
		}()
		err = cancel.canceller.Cancel(ctx, queue)
		return
	})

	handleEvents(ctx, group, cancel.result, func(context.Context) (a2a.SendMessageResult, error) {
		return cancel.processEvents(ctx, queue)
	})
}

func (m *Manager) handleCancelWithConcurrentRun(ctx context.Context, cancel *cancellation, run *Execution) {
	defer func() {
		if r := recover(); r != nil {
			cancel.result.reject(fmt.Errorf("task cancellation panic: %v", r))
		}
	}()

	defer func() {
		m.mu.Lock()
		delete(m.cancellations, cancel.tid)
		m.mu.Unlock()
	}()

	queue, err := m.queueManager.GetOrCreate(ctx, cancel.tid)
	if err != nil {
		cancel.result.reject(fmt.Errorf("queue creation failed: %w", err))
		return
	}

	if err := cancel.canceller.Cancel(ctx, queue); err != nil {
		cancel.result.reject(err)
		return
	}

	result, err := run.Result(ctx)
	if err != nil {
		cancel.result.reject(err)
		return
	}

	cancel.result.resolve(result)
}

type eventHandlerFn func(context.Context) (a2a.SendMessageResult, error)

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
