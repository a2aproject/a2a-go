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
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

const defaultQueueBufferSize = 32

// Reuse the pull-based reader's five-minute inactivity window as the default
// grace period for an in-memory subscriber.
const defaultSubscriberTimeout = defaultInactivityTimeout

// MemManagerOption is a functional option for configuring an in-memory event manager.
type MemManagerOption func(*inMemoryManager)

// WithQueueBufferSize configures the size of the in-memory event queue buffer.
func WithQueueBufferSize(size int) MemManagerOption {
	return func(manager *inMemoryManager) {
		manager.bufferSize = size
	}
}

// WithSubscriberTimeout configures how long Writer.Write waits for each reader
// to accept an individual broadcast before that reader is closed and removed.
// The timer restarts for every broadcast. The default is five minutes. A
// non-positive duration disables automatic subscriber removal.
func WithSubscriberTimeout(timeout time.Duration) MemManagerOption {
	return func(manager *inMemoryManager) {
		manager.subscriberTimeout = timeout
	}
}

// inMemoryManager implements Manager interface.
type inMemoryManager struct {
	mu      sync.Mutex
	brokers map[a2a.TaskID]*inMemoryEventBroker

	bufferSize        int
	subscriberTimeout time.Duration
}

var _ Manager = (*inMemoryManager)(nil)

// NewInMemoryManager creates a new in-memory eventqueue manager.
// A broker goroutine is started when the first connection for a task ID is created.
// All connections returned for the task ID before Destroy() is called are attached to the same broker. Each connection must use its own Reader or Writer.
// Destroy() stops the broker and closes all connections. Buffered readers are allowed to drain their messages.
//
// Writer.Write() returns when a message is put to all the open readers associated with the task. Writer connections are publishers and are not broadcast subscribers.
// If a broadcast is accepted before its context is canceled, delivery continues; a later Write on the same writer waits for that broadcast to finish.
// A subscriber that remains unable to accept a message for the configured subscriber timeout is
// removed and its Reader returns ErrQueueClosed after draining buffered messages. Reader.Read()
// blocks until a message is received through another connection or until close.
// Reader.Read() will not receive a message sent using Write() call on the same connection.
// Reader.Close() unregisters a connection from further broadcasts and allows buffered messages to drain.
func NewInMemoryManager(options ...MemManagerOption) Manager {
	manager := &inMemoryManager{
		brokers:           make(map[a2a.TaskID]*inMemoryEventBroker),
		bufferSize:        defaultQueueBufferSize,
		subscriberTimeout: defaultSubscriberTimeout,
	}
	for _, opt := range options {
		opt(manager)
	}
	return manager
}

func (m *inMemoryManager) CreateReader(ctx context.Context, taskID a2a.TaskID) (Reader, error) {
	queue, err := m.createQueue(ctx, taskID, true)
	if err != nil {
		return nil, err
	}
	return queue, nil
}

func (m *inMemoryManager) CreateWriter(ctx context.Context, taskID a2a.TaskID) (Writer, error) {
	queue, err := m.createQueue(ctx, taskID, false)
	if err != nil {
		return nil, err
	}
	return queue, nil
}

func (m *inMemoryManager) createQueue(ctx context.Context, taskID a2a.TaskID, subscriber bool) (*inMemoryQueue, error) {
	for attempt := range 2 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		m.mu.Lock()
		broker, ok := m.brokers[taskID]
		if !ok {
			broker = newInMemoryEventBroker(m.bufferSize, m.subscriberTimeout)
			m.brokers[taskID] = broker
		}
		// Hold a reference before releasing the manager lock so a failed concurrent
		// connection cannot remove a broker that another caller is still joining.
		broker.connectionRefs.Add(1)
		m.mu.Unlock()

		queue, err := broker.connect(ctx, subscriber)
		m.mu.Lock()
		if err != nil {
			if queue == nil {
				broker.connectionRefs.Add(-1)
			} else {
				queue.drop()
				queue = nil
			}
		}
		current, exists := m.brokers[taskID]
		replaced := !exists || current != broker
		cleanup := err != nil && !replaced && broker.connectionRefs.Load() == 0
		if cleanup {
			delete(m.brokers, taskID)
			replaced = true
		}
		m.mu.Unlock()

		if queue != nil && replaced {
			queue.drop()
			queue = nil
			err = ErrQueueClosed
		}
		if cleanup {
			if destroyErr := broker.destroy(context.Background()); destroyErr != nil {
				return nil, fmt.Errorf("failed to clean up broker after connect failure: %w: %w", err, destroyErr)
			}
		}
		if err == nil {
			return queue, nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		if attempt == 0 && replaced && errors.Is(err, ErrQueueClosed) {
			continue
		}
		return nil, err
	}
	return nil, ErrQueueClosed
}

func (m *inMemoryManager) Destroy(ctx context.Context, taskID a2a.TaskID) error {
	m.mu.Lock()
	broker, ok := m.brokers[taskID]
	if !ok {
		m.mu.Unlock()
		return nil
	}
	delete(m.brokers, taskID)
	m.mu.Unlock()
	return broker.destroy(ctx)
}
