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

	"github.com/a2aproject/a2a-go/a2a"
)

const defaultQueueBufferSize = 32

type MemManagerOption func(*inMemoryManager)

func WithQueueBufferSize(size int) MemManagerOption {
	return func(manager *inMemoryManager) {
		manager.bufferSize = size
	}
}

// inMemoryManager implements Manager interface.
type inMemoryManager struct {
	mu      sync.Mutex
	brokers map[a2a.TaskID]*inMemoryEventBroker

	bufferSize int
}

var _ Manager = (*inMemoryManager)(nil)

// NewInMemoryManager creates a new in-memory eventqueue manager.
// A message dispatcher goroutine is started when the first queue for a task ID is created.
// All the queues returned for the task ID before Destroy() is called are attached to the same goroutine. Each goroutine must use its own Queue.
// Destroy() stops the goroutine and closes all the queues. If queues were buffered consumers are allowed to drain them.
//
// Queue.Write() returns when a message is put to all the open queues associated with the task.
// Queue.Read() blocks until a message is received through another queue or until close.
// Queue.Read() will not receive a message sent using Write() call on the same queue.
// Queue.Close() unregisters a queue from further broadcasts.
// Queue.Close() may partially drain the queue, so Read() behavior after is Close() is undefined.
func NewInMemoryManager(options ...MemManagerOption) Manager {
	manager := &inMemoryManager{
		brokers:    make(map[a2a.TaskID]*inMemoryEventBroker),
		bufferSize: defaultQueueBufferSize,
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
	defer m.mu.Unlock()
	broker, ok := m.brokers[taskID]
	if !ok {
		broker = newInMemoryEventBroker(m.bufferSize)
		m.brokers[taskID] = broker
	}
	return broker.connect()
}

func (m *inMemoryManager) Destroy(ctx context.Context, taskID a2a.TaskID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	broker, ok := m.brokers[taskID]
	if !ok {
		return nil
	}
	broker.destroy() // responsiveness expected, as we're holding the mutex
	delete(m.brokers, taskID)
	return nil
}
