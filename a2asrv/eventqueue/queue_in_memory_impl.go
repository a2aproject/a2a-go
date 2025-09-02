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

	"github.com/a2aproject/a2a-go/a2a"
)

const defaultMaxQueueSize = 1024

// Implements Queue interface
type inMemoryQueue struct {
	// struct{}{} needs to be written to a semaphore before interacting with events
	// to limit the number inflight of goroutines that are writing to it.
	semaphore chan struct{}
	// Channel to keep all events related to a specific task.
	events chan a2a.Event
	// Indicates that the queue has been closed but still can be drained by Read().
	closed bool
	// struct{}{} needs to be written to close before trying to acquire a semaphore for closing events.
	close chan struct{}
}

// NewInMemoryQueue creates a new queue of desired size
func NewInMemoryQueue(size int) Queue {
	return &inMemoryQueue{
		semaphore: make(chan struct{}, 1),
		// todo: explore dynamically growing implementations (with a max-cap) to avoid preallocating a large buffered channel
		// examples:
		// https://github.com/modelcontextprotocol/go-sdk/blob/a76bae3a11c008d59488083185d05a74b86f429c/mcp/transport.go#L305
		// https://github.com/golang/net/blob/master/quic/queue.go
		events: make(chan a2a.Event, size),
		close:  make(chan struct{}, 1),
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
		return ErrQueueClosed
	}

	select {
	case q.events <- event:
		return nil
	case <-q.close:
		close(q.events)
		q.closed = true
		return ErrQueueClosed
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (q *inMemoryQueue) Read(ctx context.Context) (a2a.Event, error) {
	// q.closed is not checked so that the readers can drain the queue.
	select {
	case event, ok := <-q.events:
		if !ok {
			return nil, ErrQueueClosed
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
