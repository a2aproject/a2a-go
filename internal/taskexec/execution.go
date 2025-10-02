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
	"fmt"
	"iter"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
	"github.com/a2aproject/a2a-go/log"
)

// Execution represents an agent invocation in a context of the referenced task.
// If the invocation was finished Result() will resolve immediately, otherwise it will block.
type Execution struct {
	tid        a2a.TaskID
	controller Executor

	subscribeChan   chan chan a2a.Event
	unsubscribeChan chan chan a2a.Event

	result *promise
}

// Not exported, because Executions are created by Executor.
func newExecution(tid a2a.TaskID, controller Executor) *Execution {
	return &Execution{
		tid:        tid,
		controller: controller,

		subscribeChan:   make(chan chan a2a.Event),
		unsubscribeChan: make(chan chan a2a.Event),

		result: newPromise(),
	}
}

// Events subscribes to the events the agent is producing during an active Execution.
// If the Execution was finished the sequence will be empty.
func (e *Execution) Events(ctx context.Context) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		subscription, err := newSubscription(ctx, e)
		if err != nil {
			yield(nil, fmt.Errorf("failed to subscribe to execution events: %w", err))
			return
		}

		stopped := false
		defer func() {
			err := subscription.cancel(ctx)
			if !stopped {
				yield(nil, err)
			} else {
				log.Error(ctx, "failed to cancel a subscription", err)
			}
		}()

		for {
			select {
			case <-ctx.Done():
				stopped = true
				yield(nil, err)
				return

			case event, ok := <-subscription.events:
				if !ok {
					return
				}
				if !yield(event, nil) {
					stopped = true
					return
				}
			}
		}
	}
}

// Result resolves immediately for the finished Execution or blocks until it is complete.
func (e *Execution) Result(ctx context.Context) (a2a.SendMessageResult, error) {
	return e.result.wait(ctx)
}

func (e *Execution) processEvents(ctx context.Context, queue eventqueue.Queue) (a2a.SendMessageResult, error) {
	subscribers := make(map[chan a2a.Event]any)

	defer func() {
		for sub := range subscribers {
			close(sub)
		}
	}()

	eventChan, errorChan := make(chan a2a.Event), make(chan error)

	queueReadCtx, cancelCtx := context.WithCancel(ctx)
	defer cancelCtx()
	go readQueueToChannels(queueReadCtx, queue, eventChan, errorChan)

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
