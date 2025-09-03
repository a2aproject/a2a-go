package task

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
	ErrTaskExecutionInProgress = errors.New("task execution is already in progress")
	ErrTaskExecutionNotFound   = errors.New("task execution not found")
)

// execution terminates when either a non-nil result or a non-nil error is returned.
// the terminal value becomes the result of the execution.
type handleEventFn func(context.Context, a2a.Event) (*a2a.SendMessageResult, error)

type ExecutableFn func(context.Context) error
type CancelExecutionFn func(context.Context, *Execution) error

type Executor struct {
	handleEventFn handleEventFn

	mu         sync.Mutex
	executions map[a2a.TaskID]*Execution
}

func NewExecutor(eventHandler handleEventFn) *Executor {
	return &Executor{
		handleEventFn: eventHandler,
		executions:    make(map[a2a.TaskID]*Execution),
	}
}

// GetExecution can be used to resubscribe to events which are being produced by agentExecution.
func (e *Executor) GetExecution(taskID a2a.TaskID) (*Execution, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	execution, ok := e.executions[taskID]
	return execution, ok
}

// Execute starts an AgentExecutor in a separate goroutine with a detached context.
// There can only be a single active execution per TaskID.
func (e *Executor) Execute(ctx context.Context, id a2a.TaskID, queue eventqueue.Queue, execFn ExecutableFn) (*Execution, error) {

	e.mu.Lock()
	defer e.mu.Unlock()

	// TODO(yarolegovich): handle idempotency once spec establishes the key.
	if _, ok := e.executions[id]; ok {
		return nil, ErrTaskExecutionInProgress
	}

	execution := newExecution(queue)
	e.executions[id] = execution

	detachedCtx := context.WithoutCancel(ctx)

	go e.startExecution(detachedCtx, id, execution, execFn)

	return execution, nil
}

// Cancel requests AgentExecutor to stop the execution and waits for it to finish.
func (e *Executor) Cancel(ctx context.Context, taskID a2a.TaskID, cancelFn CancelExecutionFn) (*a2a.Task, error) {
	e.mu.Lock()
	execution, ok := e.executions[taskID]
	e.mu.Unlock()

	if !ok {
		return nil, ErrTaskExecutionNotFound
	}

	if err := cancelFn(ctx, execution); err != nil {
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

func (e *Executor) startExecution(ctx context.Context, id a2a.TaskID, execution *Execution, execFn ExecutableFn) {
	defer func() {
		e.mu.Lock()
		delete(e.executions, id)
		e.mu.Unlock()
	}()

	group, ctx := errgroup.WithContext(ctx)

	var result *a2a.SendMessageResult
	group.Go(func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("event consumer panic: %v", r)
			}
		}()
		localResult, err := e.handleExecutionEvents(ctx, execution)
		result = &localResult
		return
	})

	group.Go(func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("agent executor panic: %v", r)
			}
		}()
		err = execFn(ctx)
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

func (e *Executor) handleExecutionEvents(ctx context.Context, execution *Execution) (a2a.SendMessageResult, error) {
	subscribers := make(map[chan a2a.Event]any)

	defer func() {
		for sub := range subscribers {
			close(sub)
		}
	}()

	eventChan := make(chan a2a.Event)
	errorChan := make(chan error)

	go readQueueToChannels(ctx, execution.queue, eventChan, errorChan)

	for {
		select {
		case event := <-eventChan:
			res, err := e.handleEventFn(ctx, event)

			if err != nil {
				return nil, err
			}

			for subscriber := range subscribers {
				subscriber <- event
			}

			if res != nil {
				return *res, nil
			}

		case <-ctx.Done():
			return nil, ctx.Err()

		case err := <-errorChan:
			return nil, err

		case s := <-execution.subscribeChan:
			subscribers[s] = struct{}{}

		case s := <-execution.unsubscribeChan:
			delete(subscribers, s)
		}
	}
}

func readQueueToChannels(ctx context.Context, queue eventqueue.Reader, eventChan chan a2a.Event, errorChan chan error) {
	for {
		event, err := queue.Read(ctx)
		if err != nil {
			select {
			case errorChan <- err:
			case <-ctx.Done():
			}
			return
		}

		select {
		case eventChan <- event:
		case <-ctx.Done():
			return
		}
	}
}
