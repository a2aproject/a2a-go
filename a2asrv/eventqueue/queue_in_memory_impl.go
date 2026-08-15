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
	"sync"
	"sync/atomic"
	"time"
)

type broadcast struct {
	ctx        context.Context
	sender     *inMemoryQueue // sender does not receive its own broadcasts
	payload    *Message
	dispatched chan struct{} // closed after all registered queues received the broadcast.
	pending    atomic.Int64
}

func (b *broadcast) setPending(count int) {
	if count == 0 {
		close(b.dispatched)
		return
	}
	b.pending.Store(int64(count))
}

func (b *broadcast) done() {
	if b.pending.Add(-1) == 0 {
		close(b.dispatched)
	}
}

// inMemoryEventBroker manages the control plane for the queues associated with
// one task. Data delivery is performed by per-queue workers so a full queue
// cannot prevent the broker from accepting registration or unregistration.
type inMemoryEventBroker struct {
	registered     map[*inMemoryQueue]any
	destroySignal  chan struct{} // used to request broker destruction
	destroyed      chan struct{} // closed after broker is destroyed
	registerChan   chan *inMemoryQueue
	unregisterChan chan *inMemoryQueue
	broadcastChan  chan *broadcast
	destroyOnce    sync.Once

	queueBufferSize   int
	subscriberTimeout time.Duration
}

func newInMemoryEventBroker(queueBufferSize int, subscriberTimeout time.Duration) *inMemoryEventBroker {
	broker := &inMemoryEventBroker{
		registered:        make(map[*inMemoryQueue]any),
		destroySignal:     make(chan struct{}),
		destroyed:         make(chan struct{}),
		registerChan:      make(chan *inMemoryQueue),
		unregisterChan:    make(chan *inMemoryQueue),
		broadcastChan:     make(chan *broadcast),
		queueBufferSize:   queueBufferSize,
		subscriberTimeout: subscriberTimeout,
	}
	go broker.run()
	return broker
}

func (b *inMemoryEventBroker) run() {
	defer func() {
		for queue := range b.registered {
			queue.destroy()
		}
		close(b.destroyed)
	}()

	for {
		select {
		case broadcast := <-b.broadcastChan:
			b.dispatch(broadcast)

		case queue := <-b.registerChan:
			b.registered[queue] = struct{}{}
			queue.startDispatch()

		case queue := <-b.unregisterChan:
			if _, ok := b.registered[queue]; ok {
				delete(b.registered, queue)
				queue.destroy()
			}

		case <-b.destroySignal:
			return
		}
	}
}

func (b *inMemoryEventBroker) dispatch(broadcast *broadcast) {
	recipients := 0
	for queue := range b.registered {
		if queue != broadcast.sender {
			recipients++
		}
	}
	broadcast.setPending(recipients)
	for queue := range b.registered {
		if queue != broadcast.sender {
			queue.dispatch(broadcast)
		}
	}
}

func (b *inMemoryEventBroker) connect(ctx context.Context) (*inMemoryQueue, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	queue := newInMemoryQueue(b)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-b.destroySignal:
		return nil, ErrQueueClosed
	case <-b.destroyed:
		return nil, ErrQueueClosed
	case b.registerChan <- queue:
		return queue, nil
	}
}

func (b *inMemoryEventBroker) requestUnregister(queue *inMemoryQueue) {
	select {
	case b.unregisterChan <- queue:
	case <-b.destroySignal:
	case <-b.destroyed:
	}
}

func (b *inMemoryEventBroker) destroy(ctx context.Context) error {
	b.destroyOnce.Do(func() {
		close(b.destroySignal)
	})
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-b.destroyed:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// inMemoryQueue implements Queue interface.
type inMemoryQueue struct {
	broker     *inMemoryEventBroker
	closedChan chan struct{}
	eventsChan chan *Message

	dispatchMu     sync.Mutex
	dispatchCond   *sync.Cond
	dispatchQueue  []*broadcast
	dispatchDone   chan struct{}
	unregisterOnce sync.Once
	closed         bool
}

func newInMemoryQueue(broker *inMemoryEventBroker) *inMemoryQueue {
	queue := &inMemoryQueue{
		broker:       broker,
		closedChan:   make(chan struct{}),
		eventsChan:   make(chan *Message, broker.queueBufferSize),
		dispatchDone: make(chan struct{}),
	}
	queue.dispatchCond = sync.NewCond(&queue.dispatchMu)
	return queue
}

var _ Reader = (*inMemoryQueue)(nil)
var _ Writer = (*inMemoryQueue)(nil)

func (q *inMemoryQueue) Write(ctx context.Context, message *Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	broadcast := &broadcast{ctx: ctx, sender: q, payload: message, dispatched: make(chan struct{})}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-q.closedChan:
		return ErrQueueClosed
	case <-q.broker.destroySignal:
		return ErrQueueClosed
	case <-q.broker.destroyed:
		return ErrQueueClosed
	case q.broker.broadcastChan <- broadcast:
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-q.closedChan:
		return ErrQueueClosed
	case <-q.broker.destroySignal:
		return ErrQueueClosed
	case <-q.broker.destroyed:
		return ErrQueueClosed
	case <-broadcast.dispatched:
		select {
		case <-q.closedChan:
			return ErrQueueClosed
		case <-q.broker.destroySignal:
			return ErrQueueClosed
		default:
		}
		return nil
	}
}

func (q *inMemoryQueue) Read(ctx context.Context) (*Message, error) {
	select {
	case message, ok := <-q.eventsChan:
		if !ok {
			return nil, ErrQueueClosed
		}
		return message, nil
	default:
	}

	select {
	case message, ok := <-q.eventsChan:
		if !ok {
			return nil, ErrQueueClosed
		}
		return message, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (q *inMemoryQueue) Close() error {
	q.drop()
	return nil
}

func (q *inMemoryQueue) startDispatch() {
	go q.runDispatch()
}

func (q *inMemoryQueue) dispatch(broadcast *broadcast) {
	q.dispatchMu.Lock()
	if q.closed {
		q.dispatchMu.Unlock()
		broadcast.done()
		return
	}
	q.dispatchQueue = append(q.dispatchQueue, broadcast)
	q.dispatchCond.Signal()
	q.dispatchMu.Unlock()
}

func (q *inMemoryQueue) runDispatch() {
	defer func() {
		close(q.eventsChan)
		close(q.dispatchDone)
	}()
	for {
		q.dispatchMu.Lock()
		for len(q.dispatchQueue) == 0 && !q.closed {
			q.dispatchCond.Wait()
		}
		if q.closed {
			pending := q.dispatchQueue
			q.dispatchQueue = nil
			q.dispatchMu.Unlock()
			for _, broadcast := range pending {
				broadcast.done()
			}
			return
		}
		broadcast := q.dispatchQueue[0]
		if len(q.dispatchQueue) == 1 {
			q.dispatchQueue = nil
		} else {
			q.dispatchQueue[0] = nil
			q.dispatchQueue = q.dispatchQueue[1:]
		}
		q.dispatchMu.Unlock()

		timedOut := q.deliver(broadcast)
		if timedOut {
			q.drop()
		}
		broadcast.done()
	}
}

func (q *inMemoryQueue) deliver(broadcast *broadcast) bool {
	if err := broadcast.ctx.Err(); err != nil {
		return false
	}

	var timeout <-chan time.Time
	var timer *time.Timer
	if q.broker.subscriberTimeout > 0 {
		timer = time.NewTimer(q.broker.subscriberTimeout)
		timeout = timer.C
		defer timer.Stop()
	}

	select {
	case q.eventsChan <- broadcast.payload:
		return false
	case <-q.closedChan:
		return false
	case <-broadcast.ctx.Done():
		return false
	case <-timeout:
		return true
	}
}

func (q *inMemoryQueue) drop() {
	q.dispatchMu.Lock()
	if q.closed {
		q.dispatchMu.Unlock()
		return
	}
	q.closed = true
	close(q.closedChan)
	q.dispatchCond.Broadcast()
	q.dispatchMu.Unlock()

	q.unregisterOnce.Do(func() {
		go q.broker.requestUnregister(q)
	})
}

func (q *inMemoryQueue) destroy() {
	q.drop()
	<-q.dispatchDone
}
