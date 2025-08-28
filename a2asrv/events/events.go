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
)

const defaultMaxQueueSize = 1024

// Reader defines the interface for reading events from a queue.
// A2A server stack reads events written by AgentExecutor.
type Reader interface {
	// Read dequeues an event or blocks if the queue is empty.
	Read(ctx context.Context) (a2a.Event, error)
}

// Writer defines the interface for writing events to a queue.
// AgentExecutor translates agent responses to Messages, Tasks or Task update events.
type Writer interface {
	// Write enqueues an event or blocks if a bounded queue is full.
	Write(ctx context.Context, event a2a.Event) error
}

// Queue defines the interface for publishing and consuming
// events generated during agent execution.
type Queue interface {
	Reader
	Writer

	// Close shuts down a connection to the queue.
	Close() error
}

// QueueManager manages event queues on a per-task basis.
// It provides lifecycle management for task-specific event queues,
// enabling multiple clients to attach to the same task's event stream.
type QueueManager interface {
	// GetOrCreate returns an existing queue if one exists, or creates a new one.
	GetOrCreate(ctx context.Context, taskId a2a.TaskID) (Queue, error)

	// Destroy closes the queue for the specified task and frees all associates resources.
	Destroy(ctx context.Context, taskId a2a.TaskID) error
}

// Implements EventQueueManager interface
type inMemoryQueueManager struct {
	mu     sync.Mutex
	queues map[a2a.TaskID]Queue
}

// Implements EventQueue interface
type inMemoryQueue struct {
	// An element needs to be written to a semaphore before writing to events or closing it.
	semaphore chan any
	// Channel to keep all events related to a specific task
	events chan a2a.Event
	// Indicates that the queue has been closed but still can be drained by Read()
	closed bool
	// An element needs to be written to close before trying to acquire a semaphore for closing events.
	close chan any
}

// NewInMemoryQueueManager creates a new queue manager
func NewInMemoryQueueManager() QueueManager {
	return &inMemoryQueueManager{
		queues: make(map[a2a.TaskID]Queue),
	}
}

func (m *inMemoryQueueManager) GetOrCreate(ctx context.Context, taskId a2a.TaskID) (Queue, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.queues[taskId]; !ok {
		queue := NewInMemoryQueue(defaultMaxQueueSize)
		m.queues[taskId] = queue
	}
	return m.queues[taskId], nil
}

func (m *inMemoryQueueManager) Destroy(ctx context.Context, taskId a2a.TaskID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.queues[taskId]; !ok {
		return fmt.Errorf("queue for taskId: %s does not exist", taskId)
	}
	queue := m.queues[taskId]
	_ = queue.Close() // in memory queue close never fails
	delete(m.queues, taskId)
	return nil
}

func NewInMemoryQueue(size int) *inMemoryQueue {
	return &inMemoryQueue{
		semaphore: make(chan any, 1),
		events:    make(chan a2a.Event, size),
		close:     make(chan any, 1),
	}
}

func (q *inMemoryQueue) Write(ctx context.Context, event a2a.Event) error {
	select {
	case q.semaphore <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-q.semaphore }()

	if q.closed {
		return a2a.ErrQueueClosed
	}

	select {
	case q.events <- event:
		return nil
	case <-q.close:
		close(q.events)
		q.closed = true
		return a2a.ErrQueueClosed
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (q *inMemoryQueue) Read(ctx context.Context) (a2a.Event, error) {
	// q.closed is not checked so that the readers can drain the queue.
	select {
	case event, ok := <-q.events:
		if !ok {
			return nil, a2a.ErrQueueClosed
		}
		return event, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (q *inMemoryQueue) Close() error {
	select {
	case q.close <- struct{}{}:
	default:
		// There's already a signal in the channel but q.closed
		// hasn't been set properly so we move on to close the queue
	}

	// It might be blocked here if there is a writer holding the semaphore.
	// But it's going to be unblocked by the signal we sent.
	q.semaphore <- struct{}{}
	defer func() { <-q.semaphore }()

	if !q.closed {
		close(q.events)
		q.closed = true
	}

	return nil
}
