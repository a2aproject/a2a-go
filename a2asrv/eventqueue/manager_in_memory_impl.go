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
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

const defaultQueueBufferSize = 32

// Match the pull-based reader's inactivity window so both built-in queue
// implementations use the same default bound for an inactive subscriber.
const defaultSubscriberTimeout = defaultInactivityTimeout

// MemManagerOption is a functional option for configuring an in-memory event manager.
type MemManagerOption func(*inMemoryManager)

// WithQueueBufferSize configures the size of the in-memory event queue buffer.
func WithQueueBufferSize(size int) MemManagerOption {
	return func(manager *inMemoryManager) {
		manager.bufferSize = size
	}
}

// WithSubscriberTimeout configures how long an in-memory subscriber may remain
// unable to accept a broadcast before it is closed and removed from the broker.
// The default is five minutes. A non-positive duration disables automatic
// subscriber removal.
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
// A message dispatcher goroutine is started when the first queue for a task ID is created.
// All the queues returned for the task ID before Destroy() is called are attached to the same goroutine. Each goroutine must use its own Queue.
// Destroy() stops the goroutine and closes all the queues. If queues were buffered consumers are allowed to drain them.
//
// Queue.Write() returns when a message is put to all the open queues associated with the task.
// A subscriber that remains unable to accept a message for the configured subscriber timeout is
// removed and its Reader returns ErrQueueClosed after draining buffered messages. Queue.Read()
// blocks until a message is received through another queue or until close.
// Queue.Read() will not receive a message sent using Write() call on the same queue.
// Queue.Close() unregisters a queue from further broadcasts and allows buffered messages to drain.
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
	return m.createReadWriter(ctx, taskID)
}

func (m *inMemoryManager) CreateWriter(ctx context.Context, taskID a2a.TaskID) (Writer, error) {
	return m.createReadWriter(ctx, taskID)
}

func (m *inMemoryManager) createReadWriter(ctx context.Context, taskID a2a.TaskID) (*inMemoryQueue, error) {
	m.mu.Lock()
	broker, ok := m.brokers[taskID]
	if !ok {
		broker = newInMemoryEventBroker(m.bufferSize, m.subscriberTimeout)
		m.brokers[taskID] = broker
	}
	m.mu.Unlock()
	return broker.connect(ctx)
}

func (m *inMemoryManager) Destroy(ctx context.Context, taskID a2a.TaskID) error {
	m.mu.Lock()
	broker, ok := m.brokers[taskID]
	if !ok {
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()
	err := broker.destroy(ctx)
	m.mu.Lock()
	if current, ok := m.brokers[taskID]; ok && current == broker {
		delete(m.brokers, taskID)
	}
	m.mu.Unlock()
	return err
}
