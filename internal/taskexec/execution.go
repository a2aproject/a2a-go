package taskexec

import (
	"context"
	"fmt"
	"iter"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
)

type Controller interface {
	// Start starts publishing events to the queue. Called in a separate goroutine.
	Start(context.Context, eventqueue.Queue) error

	// Process is called in response to events produced by the running Execution.
	// Execution finishes when either a non-nil result or a non-nil error is returned.
	// the terminal value becomes the result of the execution.
	// Called in a separate goroutine.
	Process(context.Context, a2a.Event) (*a2a.SendMessageResult, error)

	// Cancel attempts to end the running Execution. Expected to produce a Task update
	// event with cancelled state.
	Cancel(context.Context, eventqueue.Queue) error
}

type Execution struct {
	controller Controller
	queue      eventqueue.Queue

	subscribeChan   chan chan a2a.Event
	unsubscribeChan chan chan a2a.Event

	// done channel gets closed once result or err field is set
	done   chan any
	result a2a.SendMessageResult
	err    error
}

// Not exported, because Executions are created by Executor.
func newExecution(controller Controller, queue eventqueue.Queue) *Execution {
	return &Execution{
		controller: controller,
		queue:      queue,

		subscribeChan:   make(chan chan a2a.Event),
		unsubscribeChan: make(chan chan a2a.Event),
		done:            make(chan any),
	}
}

func (e *Execution) GetEvents(ctx context.Context) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		eventChan, err := e.Subscribe(ctx)
		if err != nil {
			yield(nil, fmt.Errorf("failed to subscribe to execution result: %w", err))
			return
		}
		defer e.Unsubscribe(ctx, eventChan)

		for {
			select {
			case <-ctx.Done():
				yield(nil, err)
				return

			case event, ok := <-eventChan:
				if !ok {
					return
				}
				if !yield(event, nil) {
					return
				}
			}
		}
	}
}

func (e *Execution) Result(ctx context.Context) (a2a.SendMessageResult, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-e.done:
		return e.result, e.err
	}
}

func (e *Execution) Subscribe(ctx context.Context) (chan a2a.Event, error) {
	ch := make(chan a2a.Event)

	select {
	case <-ctx.Done():
		return nil, ctx.Err()

	case e.subscribeChan <- ch:
		return ch, nil

	case <-e.done:
		close(ch)
		return ch, nil
	}
}

func (e *Execution) Unsubscribe(ctx context.Context, ch chan a2a.Event) error {
	select {
	case <-ctx.Done():
		return ctx.Err()

	case e.unsubscribeChan <- ch:
		return nil

	case <-e.done:
		return nil
	}
}

func (e *Execution) start(ctx context.Context) error {
	return e.controller.Start(ctx, e.queue)
}

func (e *Execution) cancel(ctx context.Context) error {
	return e.controller.Cancel(ctx, e.queue)
}

func (e *Execution) processEvents(ctx context.Context) (a2a.SendMessageResult, error) {
	subscribers := make(map[chan a2a.Event]any)

	defer func() {
		for sub := range subscribers {
			close(sub)
		}
	}()

	eventChan := make(chan a2a.Event)
	errorChan := make(chan error)

	go readQueueToChannels(ctx, e.queue, eventChan, errorChan)

	for {
		select {
		case event := <-eventChan:
			res, err := e.controller.Process(ctx, event)

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

		case s := <-e.subscribeChan:
			subscribers[s] = struct{}{}

		case s := <-e.unsubscribeChan:
			delete(subscribers, s)
		}
	}
}
