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

func NewExecutionManager(queueManager eventqueue.Manager) *Manager {
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
func (m *Manager) Execute(ctx context.Context, id a2a.TaskID, executor Executor) (*Execution, error) {
	queue, err := m.queueManager.GetOrCreate(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("queue creation failed: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// TODO(yarolegovich): handle idempotency once spec establishes the key.
	// We can return an execution in progress here and decide whether to tap into it or not
	// on the caller side.
	if _, ok := m.executions[id]; ok {
		return nil, ErrExecutionInProgress
	}

	if _, ok := m.cancellations[id]; ok {
		return nil, ErrCancellationInProgress
	}

	execution := newExecution(executor, queue)
	m.executions[id] = execution

	detachedCtx := context.WithoutCancel(ctx)

	go m.handleExecution(detachedCtx, id, execution)

	return execution, nil
}

// Cancel requests AgentExecutor to stop the execution and waits for it to finish.
func (m *Manager) Cancel(ctx context.Context, taskID a2a.TaskID, canceller Canceller) (*a2a.Task, error) {
	queue, err := m.queueManager.GetOrCreate(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("queue creation failed: %w", err)
	}

	m.mu.Lock()
	execution := m.executions[taskID]
	cancellation, cancelInProgress := m.cancellations[taskID]

	if cancellation == nil {
		if execution != nil {
			cancellation = newConcurrentCancellation(canceller, execution)
		} else {
			cancellation = newCancellation(canceller, queue)
		}
		m.cancellations[taskID] = cancellation
	}
	m.mu.Unlock()

	if cancelInProgress {
		return cancellation.wait(ctx)
	}

	detachedCtx := context.WithoutCancel(ctx)

	go m.handleCancellation(detachedCtx, taskID, cancellation)

	return cancellation.wait(ctx)
}

func (m *Manager) handleExecution(ctx context.Context, id a2a.TaskID, execution *Execution) {
	defer func() {
		m.mu.Lock()
		delete(m.executions, id)
		m.mu.Unlock()
	}()

	group, ctx := errgroup.WithContext(ctx)

	group.Go(func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("task execution panic: %v", r)
			}
		}()
		err = execution.start(ctx)
		return
	})

	m.handleEvents(ctx, group, execution.result, func(ctx context.Context) (a2a.SendMessageResult, error) {
		return execution.processEvents(ctx)
	})
}

func (m *Manager) handleCancellation(ctx context.Context, id a2a.TaskID, cancellation *cancellation) {
	defer func() {
		m.mu.Lock()
		delete(m.cancellations, id)
		m.mu.Unlock()
	}()

	group, ctx := errgroup.WithContext(ctx)

	group.Go(func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("task cancellation panic: %v", r)
			}
		}()
		err = cancellation.start(ctx)
		return
	})

	// If a concurrent execution is running, there is an event processor goroutine.
	// Cancellation taps into the result set by it.
	if cancellation.concurrentExec == nil {
		m.handleEvents(ctx, group, cancellation.result, func(ctx context.Context) (a2a.SendMessageResult, error) {
			return cancellation.processEvents(ctx)
		})
		return
	}

	if err := group.Wait(); err != nil {
		cancellation.result.reject(err)
		return
	}

	result, err := cancellation.concurrentExec.Result(ctx)
	if err != nil {
		cancellation.result.reject(err)
		return
	}

	cancellation.result.resolve(result)
}

func (m *Manager) handleEvents(ctx context.Context, group *errgroup.Group, r *promise, processor func(ctx context.Context) (a2a.SendMessageResult, error)) {
	var result *a2a.SendMessageResult
	group.Go(func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("task event consumer panic: %v", r)
			}
		}()
		localResult, err := processor(ctx)
		result = &localResult
		return
	})

	if err := group.Wait(); err != nil {
		r.reject(err)
		return
	}

	if result == nil {
		err := fmt.Errorf("bug: no error returned, but result unset")
		r.reject(err)
		return
	}

	r.resolve(*result)
}
