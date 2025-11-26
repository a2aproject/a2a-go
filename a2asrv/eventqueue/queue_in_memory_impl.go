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

package eventqueue

import (
	"context"
	"fmt"

	"github.com/a2aproject/a2a-go/a2a"
)

type broadcast struct {
	sender     *inMemoryQueue // sender does not receive its own broadcasts
	payload    a2a.Event
	dispatched chan struct{} // closed after all registered Queues-s received the broadcast.
}

// inMemoryEventBroker manages a goroutine which acts as an event bus for registeted Queue-s.
type inMemoryEventBroker struct {
	registered     map[*inMemoryQueue]any
	destroySignal  chan struct{} // used to request broker destruction
	destroyed      chan struct{} // closed after broker is destroyed
	registerChan   chan *inMemoryQueue
	unregisterChan chan *inMemoryQueue
	broadcastChan  chan *broadcast

	queueBufferSize int
}

func newInMemoryEventBroker(queueBufferSize int) *inMemoryEventBroker {
	broker := &inMemoryEventBroker{
		registered:      make(map[*inMemoryQueue]any),
		registerChan:    make(chan *inMemoryQueue),
		unregisterChan:  make(chan *inMemoryQueue),
		broadcastChan:   make(chan *broadcast),
		destroySignal:   make(chan struct{}),
		destroyed:       make(chan struct{}),
		queueBufferSize: queueBufferSize,
	}
	go func() {
		defer func() {
			for queue := range broker.registered {
				queue.destroy()
			}
			close(broker.destroyed)
		}()

		for {
			select {
			case b := <-broker.broadcastChan:
				for queue := range broker.registered {
					if queue == b.sender {
						continue
					}

					select {
					case queue.eventsChan <- b.payload:
					case <-broker.destroySignal:
						return
					}
				}
				close(b.dispatched)

			case sub := <-broker.registerChan:
				broker.registered[sub] = struct{}{}

			case sub := <-broker.unregisterChan:
				if _, ok := broker.registered[sub]; ok {
					delete(broker.registered, sub)
					sub.destroy()
				}

			case <-broker.destroySignal:
				return
			}
		}
	}()
	return broker
}

func (b *inMemoryEventBroker) connect() (*inMemoryQueue, error) {
	conn := &inMemoryQueue{
		broker:     b,
		closedChan: make(chan struct{}),
		eventsChan: make(chan a2a.Event, b.queueBufferSize),
	}
	select {
	case <-b.destroyed:
		return nil, fmt.Errorf("broker destroyed")
	case b.registerChan <- conn:
		return conn, nil
	}
}

func (b *inMemoryEventBroker) destroy() {
	select {
	case <-b.destroyed: // already destroyed
		return
	case b.destroySignal <- struct{}{}: // send destroy signal
	}
	<-b.destroyed
}

// inMemoryQueue implements Queue interface.
type inMemoryQueue struct {
	broker     *inMemoryEventBroker
	closedChan chan struct{}
	eventsChan chan a2a.Event
	closed     bool
}

func (q *inMemoryQueue) Write(ctx context.Context, event a2a.Event) error {
	if q.closed {
		return ErrQueueClosed
	}

	broadcast := &broadcast{sender: q, payload: event, dispatched: make(chan struct{})}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-q.closedChan:
		return ErrQueueClosed
	case q.broker.broadcastChan <- broadcast:
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-q.closedChan:
		return ErrQueueClosed
	case <-broadcast.dispatched:
		// all subscribers received the broadcast
	}
	return nil
}

func (q *inMemoryQueue) Read(ctx context.Context) (a2a.Event, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case event, ok := <-q.eventsChan: // allow to drain
		if !ok {
			return nil, ErrQueueClosed
		}
		return event, nil
	}
}

func (q *inMemoryQueue) Close() error {
	// loop to ensure we drain q.eventsChan, so that the broker is not blocked trying to send us a broadcast
	for {
		select {
		case _, ok := <-q.eventsChan:
			if !ok {
				return nil
			}
		case q.broker.unregisterChan <- q:
			return nil
		}
	}
}

func (q *inMemoryQueue) destroy() {
	q.closed = true
	close(q.eventsChan)
	close(q.closedChan)
}
