package taskexec

import (
	"context"
	"fmt"
	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
)

type cancellation struct {
	tid       a2a.TaskID
	canceller Canceller
	result    *promise
}

func newCancellation(tid a2a.TaskID, controller Canceller) *cancellation {
	return &cancellation{
		tid:       tid,
		canceller: controller,
		result:    newPromise(),
	}
}

func (c *cancellation) wait(ctx context.Context) (*a2a.Task, error) {
	result, err := c.result.wait(ctx)

	if err != nil {
		return nil, fmt.Errorf("cancellation failed: %w", err)
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

func (c *cancellation) processEvents(ctx context.Context, queue eventqueue.Queue) (a2a.SendMessageResult, error) {
	eventChan := make(chan a2a.Event)
	errorChan := make(chan error)
	go readQueueToChannels(ctx, queue, eventChan, errorChan)

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
