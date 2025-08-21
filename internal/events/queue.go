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

package events

import (
	"context"
	"fmt"
	"sync"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv/events"
)

const DEFAULT_MAX_QUEUE_SIZE = 1024

// Implements events.EventQueueManager
type InMemoryQueueManager struct {
	mu     sync.Mutex
	queues map[a2a.TaskID]events.EventQueue
}

// Implements events.EventQueue
type InMemoryEventQueue struct {
	mu     sync.Mutex
	events chan a2a.Event
	closed bool
}

func NewInMemoryQueueManager() events.EventQueueManager {
	return &InMemoryQueueManager{
		queues: make(map[a2a.TaskID]events.EventQueue),
	}
}

func (m *InMemoryQueueManager) GetOrCreate(ctx context.Context, taskId a2a.TaskID) (events.EventQueue, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.queues[taskId]; !ok {
		queue, err := newEventQueue()
		if err != nil {
			return nil, fmt.Errorf("failed to create queue: %w", err)
		}
		m.queues[taskId] = queue
	}
	return m.queues[taskId], nil
}

func (m *InMemoryQueueManager) Destroy(ctx context.Context, taskId a2a.TaskID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.queues[taskId]; !ok {
		return fmt.Errorf("queue for taskId: %s does not exist", taskId)
	}
	queue := m.queues[taskId]
	queue.Close() // todo: care about the error
	delete(m.queues, taskId)
	return nil
}

func newEventQueue() (*InMemoryEventQueue, error) {
	return &InMemoryEventQueue{
		events: make(chan a2a.Event, DEFAULT_MAX_QUEUE_SIZE),
	}, nil
}

func (q *InMemoryEventQueue) Write(ctx context.Context, event a2a.Event) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return fmt.Errorf("queue is closed")
	}
	select {
	case q.events <- event:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		return fmt.Errorf("queue is full")
	}
}

func (q *InMemoryEventQueue) Read(ctx context.Context) (a2a.Event, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return nil, fmt.Errorf("queue is closed")
	}
	if len(q.events) == 0 {
		return nil, fmt.Errorf("queue is empty")
	}
	select {
	case event := <-q.events:
		return event, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (q *InMemoryEventQueue) Close() error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return nil
	}
	q.closed = true
	close(q.events)

	return nil
}
