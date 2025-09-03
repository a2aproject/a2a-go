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
	ErrExecutionInProgress = errors.New("task execution is already in progress")
	ErrExecutionNotFound   = errors.New("task execution not found")
)

type Manager struct {
	queueManager eventqueue.Manager

	mu         sync.Mutex
	executions map[a2a.TaskID]*Execution
}

func NewExecutionManager(queueManager eventqueue.Manager) *Manager {
	return &Manager{
		queueManager: queueManager,
		executions:   make(map[a2a.TaskID]*Execution),
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
func (m *Manager) Execute(ctx context.Context, id a2a.TaskID, controller Controller) (*Execution, error) {
	queue, err := m.queueManager.GetOrCreate(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("queue creation failed: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// TODO(yarolegovich): handle idempotency once spec establishes the key.
	if _, ok := m.executions[id]; ok {
		return nil, ErrExecutionInProgress
	}

	execution := newExecution(controller, queue)
	m.executions[id] = execution

	detachedCtx := context.WithoutCancel(ctx)

	go m.startExecution(detachedCtx, id, execution)

	return execution, nil
}

// Cancel requests AgentExecutor to stop the execution and waits for it to finish.
func (m *Manager) Cancel(ctx context.Context, taskID a2a.TaskID) (*a2a.Task, error) {
	m.mu.Lock()
	execution, ok := m.executions[taskID]
	m.mu.Unlock()

	if !ok {
		return nil, ErrExecutionNotFound
	}

	if err := execution.cancel(ctx); err != nil {
		return nil, fmt.Errorf("failed to cancel task: %w", err)
	}

	select {
	case <-execution.done:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	if execution.err != nil {
		return nil, fmt.Errorf("execution failed: %w", execution.err)
	}

	task, ok := execution.result.(*a2a.Task)
	if !ok { // a2a.Message was the result of the execution
		return nil, a2a.ErrTaskNotCancelable
	}

	if task.Status.State != a2a.TaskStateCanceled {
		return nil, a2a.ErrTaskNotCancelable
	}

	return task, nil
}

func (m *Manager) startExecution(ctx context.Context, id a2a.TaskID, execution *Execution) {
	defer func() {
		m.mu.Lock()
		delete(m.executions, id)
		m.mu.Unlock()
	}()

	group, ctx := errgroup.WithContext(ctx)

	var result *a2a.SendMessageResult
	group.Go(func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("event consumer panic: %v", r)
			}
		}()
		localResult, err := execution.processEvents(ctx)
		result = &localResult
		return
	})

	group.Go(func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("agent executor panic: %v", r)
			}
		}()
		err = execution.start(ctx)
		return
	})

	if err := group.Wait(); err != nil {
		execution.err = err
		close(execution.done)
		return
	}

	if result == nil {
		execution.err = fmt.Errorf("bug: no error returned, but result unset")
		close(execution.done)
		return
	}

	execution.result = *result
	close(execution.done)
}
