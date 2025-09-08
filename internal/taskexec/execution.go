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
)

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

func (e *Execution) GetEvents(ctx context.Context) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		eventChan, err := e.Subscribe(ctx)
		if err != nil {
			yield(nil, fmt.Errorf("failed to subscribe to execution events: %w", err))
			return
		}

		stopped := false
		defer func() {
			err := e.Unsubscribe(ctx, eventChan)
			// TODO(yarolegovich): else log
			if !stopped {
				yield(nil, err)
			}
		}()

		for {
			select {
			case <-ctx.Done():
				stopped = true
				yield(nil, err)
				return

			case event, ok := <-eventChan:
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

func (e *Execution) Result(ctx context.Context) (a2a.SendMessageResult, error) {
	return e.result.wait(ctx)
}

func (e *Execution) Subscribe(ctx context.Context) (chan a2a.Event, error) {
	ch := make(chan a2a.Event)

	select {
	case <-ctx.Done():
		return nil, ctx.Err()

	case e.subscribeChan <- ch:
		return ch, nil

	case <-e.result.done:
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

	case <-e.result.done:
		return nil
	}
}

func (e *Execution) processEvents(ctx context.Context, queue eventqueue.Queue) (a2a.SendMessageResult, error) {
	subscribers := make(map[chan a2a.Event]any)

	defer func() {
		for sub := range subscribers {
			close(sub)
		}
	}()

	eventChan, errorChan := make(chan a2a.Event), make(chan error)
	go readQueueToChannels(ctx, queue, eventChan, errorChan)

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
