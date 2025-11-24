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

type Execution interface {
	Events(ctx context.Context) iter.Seq2[a2a.Event, error]

	Result(ctx context.Context) (a2a.SendMessageResult, error)
}

type subscriptionEvent struct {
	event    a2a.Event
	terminal bool
}

type subscriberChan chan subscriptionEvent

// Execution represents an agent invocation in a context of the referenced task.
// If the invocation was finished Result() will resolve immediately, otherwise it will block.
type localExecution struct {
	tid    a2a.TaskID
	params *a2a.MessageSendParams

	subscribers     map[subscriberChan]any
	subscribeChan   chan subscriberChan
	unsubscribeChan chan subscriberChan

	result *promise
}

// Not exported, because Executions are created by Executor.
func newLocalExecution(tid a2a.TaskID, params *a2a.MessageSendParams) *localExecution {
	return &localExecution{
		tid:    tid,
		params: params,

		subscribers:     make(map[subscriberChan]any),
		subscribeChan:   make(chan subscriberChan),
		unsubscribeChan: make(chan subscriberChan),

		result: newPromise(),
	}
}

// Events subscribes to the events an agent is producing during an active Execution.
// If the Execution was finished the sequence will be empty.
func (e *localExecution) Events(ctx context.Context) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		subscription, err := newLocalSubscription(ctx, e)
		if err != nil {
			yield(nil, fmt.Errorf("failed to subscribe to execution events: %w", err))
			return
		}
		for event, err := range subscription.Events(ctx) {
			if err != nil {
				yield(nil, err)
				return
			}
			if !yield(event, nil) {
				return
			}
		}
	}
}

// Result resolves immediately for the finished Execution or blocks until it is complete.
func (e *localExecution) Result(ctx context.Context) (a2a.SendMessageResult, error) {
	return e.result.wait(ctx)
}

func (e *localExecution) processEvents(ctx context.Context, queue eventqueue.Queue, processor Processor) (a2a.SendMessageResult, error) {
	defer func() {
		for sub := range e.subscribers {
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
			res, err := processor.Process(ctx, event)
			if err != nil {
				return nil, err
			}

			// terminal is required because some of the events might not be reported through a subscription channel.
			// for example, if context gets canceled controller might return Task in failed state, but it won't
			// reach subscribed event consumers, because we can't use context in select with channel write.
			// if subscriber doesn't receive a terminal events but their channel gets closed they must treat execution result
			// as the final event.
			terminal := res != nil
			subEvent := subscriptionEvent{event: event, terminal: terminal}
			for subscriber := range e.subscribers {
				select {
				case subscriber <- subEvent:
				case <-ctx.Done():
					log.Info(ctx, "execution context canceled during subscriber notification attempt", "cause", context.Cause(ctx))
					return processor.ProcessError(ctx, context.Cause(ctx))
				}
			}

			if terminal {
				return *res, nil
			}

		case <-ctx.Done():
			log.Info(ctx, "execution context canceled", "cause", context.Cause(ctx))
			return processor.ProcessError(ctx, context.Cause(ctx))

		case err := <-errorChan:
			log.Info(ctx, "error reading from queue", "error", err)
			return processor.ProcessError(ctx, err)

		case s := <-e.subscribeChan:
			e.subscribers[s] = struct{}{}

		case s := <-e.unsubscribeChan:
			delete(e.subscribers, s)
		}
	}
}
