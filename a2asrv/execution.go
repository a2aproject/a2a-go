package a2asrv

import (
	"context"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv/events"
)

type agentExecution struct {
	queue events.Queue

	subscribeChan   chan chan a2a.Event
	unsubscribeChan chan chan a2a.Event

	// done channel gets closed once result or err field is set
	done   chan any
	result a2a.SendMessageResult
	err    error
}

func newAgentExecution(queue events.Queue) *agentExecution {
	return &agentExecution{
		queue: queue,

		subscribeChan:   make(chan chan a2a.Event),
		unsubscribeChan: make(chan chan a2a.Event),
		done:            make(chan any),
	}
}

func (e *agentExecution) wait(ctx context.Context) (a2a.SendMessageResult, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-e.done:
		return e.result, e.err
	}
}

func (e *agentExecution) subscribe(ctx context.Context) (chan a2a.Event, error) {
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

func (e *agentExecution) unsubscribe(ctx context.Context, ch chan a2a.Event) error {
	select {
	case <-ctx.Done():
		return ctx.Err()

	case e.unsubscribeChan <- ch:
		return nil

	case <-e.done:
		return nil
	}
}
