package taskexec

import (
	"context"

	"github.com/a2aproject/a2a-go/a2a"
)

// subscription encapsulates the logic of subscribing a channel to Execution events and canceling the subscription.
type subscription struct {
	events    chan a2a.Event
	execution *Execution
}

// newSubscription tries to subscribe a channel to Execution events. If the Execution ends,
// an "empty" subscription is returned.
func newSubscription(ctx context.Context, e *Execution) (*subscription, error) {
	ch := make(chan a2a.Event)

	select {
	case <-ctx.Done():
		return nil, ctx.Err()

	case e.subscribeChan <- ch:
		return &subscription{events: ch, execution: e}, nil

	case <-e.result.done:
		return &subscription{}, nil
	}
}

// cancel unsubscribe events channel from Execution events. If the Execution ends,
// the operation is a no-op.
func (s *subscription) cancel(ctx context.Context) error {
	if s.events == nil || s.execution == nil {
		return nil
	}

	select {
	case <-ctx.Done():
		return ctx.Err()

	case s.execution.unsubscribeChan <- s.events:
		return nil

	case <-s.execution.result.done:
		// Execution goroutine was stopped disposing all subscriptions.
		return nil
	}
}
