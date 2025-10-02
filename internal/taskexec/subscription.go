// Copyright 2025 The A2A Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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
