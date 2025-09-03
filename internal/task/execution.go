package task

import (
	"context"
	"fmt"
	"iter"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
)

type Execution struct {
	queue eventqueue.Queue

	subscribeChan   chan chan a2a.Event
	unsubscribeChan chan chan a2a.Event

	// done channel gets closed once result or err field is set
	done   chan any
	result a2a.SendMessageResult
	err    error
}

// Not exported, because Executions are created by Executor.
func newExecution(queue eventqueue.Queue) *Execution {
	return &Execution{
		queue: queue,

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
