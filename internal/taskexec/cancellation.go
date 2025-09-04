package taskexec

import (
	"context"
	"fmt"
	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
)

type cancellation struct {
	canceller      Canceller
	queue          eventqueue.Queue
	concurrentExec *Execution
	result         *promise
}

func newCancellation(controller Canceller, queue eventqueue.Queue) *cancellation {
	return &cancellation{
		canceller: controller,
		queue:     queue,
		result:    newPromise(),
	}
}

func newConcurrentCancellation(controller Canceller, execution *Execution) *cancellation {
	return &cancellation{
		canceller:      controller,
		queue:          execution.queue,
		result:         newPromise(),
		concurrentExec: execution,
	}
}

func (c *cancellation) start(ctx context.Context) error {
	return c.canceller.Cancel(ctx, c.queue)
}

func (c *cancellation) wait(ctx context.Context) (*a2a.Task, error) {
	result, err := c.result.wait(ctx)

	if err != nil {
		return nil, fmt.Errorf("execution failed: %w", err)
	}

	task, ok := result.(*a2a.Task)
	if !ok { // a2a.Message was the result of the execution
		return nil, a2a.ErrTaskNotCancelable
	}

	if task.Status.State != a2a.TaskStateCanceled {
		return nil, a2a.ErrTaskNotCancelable
	}

	return task, nil
}

func (c *cancellation) processEvents(ctx context.Context) (a2a.SendMessageResult, error) {
	eventChan := make(chan a2a.Event)
	errorChan := make(chan error)
	go readQueueToChannels(ctx, c.queue, eventChan, errorChan)

	for {
		select {
		case event := <-eventChan:
			res, err := c.canceller.Process(ctx, event)

			if err != nil {
				return nil, err
			}

			if res != nil {
				return *res, nil
			}

		case <-ctx.Done():
			return nil, ctx.Err()

		case err := <-errorChan:
			return nil, err
		}
	}
}
