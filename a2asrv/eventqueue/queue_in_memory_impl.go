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

const defaultMaxQueueSize = 1024

type semaphore struct {
	tokens chan any
}

// Implements Queue interface
type inMemoryQueue struct {
	// struct{}{} needs to be written to a semaphore's tokens before interacting with events
	// to limit the number inflight of goroutines that are writing to it.
	semaphore *semaphore
	// Channel to keep all events related to a specific task.
	events chan a2a.Event

	closeMu sync.Mutex
	// Indicates that the queue has been closed but still can be drained by Read().
	closed bool
	// struct{}{} needs to be written to closeChan before trying to acquire a semaphore for closing events.
	closeChan chan any
}

func newSemaphore(count int) *semaphore {
	return &semaphore{tokens: make(chan any, count)}
}

func (s *semaphore) acquire() {
	s.tokens <- struct{}{}
}

func (s *semaphore) acquireWithContext(ctx context.Context) error {
	select {
	case s.tokens <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *semaphore) release() {
	<-s.tokens
}

// NewInMemoryQueue creates a new queue of desired size
func NewInMemoryQueue(size int) Queue {
	return &inMemoryQueue{
		// todo: consider using https://pkg.go.dev/golang.org/x/sync/semaphore instead
		semaphore: newSemaphore(1),
		// todo: explore dynamically growing implementations (with a max-cap) to avoid preallocating a large buffered channel
		// examples:
		// https://github.com/modelcontextprotocol/go-sdk/blob/a76bae3a11c008d59488083185d05a74b86f429c/mcp/transport.go#L305
		// https://github.com/golang/net/blob/master/quic/queue.go
		events:    make(chan a2a.Event, size),
		closeChan: make(chan any, 1),
	}
}

func (q *inMemoryQueue) Write(ctx context.Context, event a2a.Event) error {
	if err := q.semaphore.acquireWithContext(ctx); err != nil {
		return err
	}
	defer q.semaphore.release()

	if q.closed {
		return ErrQueueClosed
	}

	select {
	case q.events <- event:
		return nil
	case <-q.closeChan:
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
	q.closeMu.Lock()
	defer q.closeMu.Unlock()

	if q.closed {
		return nil
	}
	q.closed = true

	close(q.closeChan)

	// It might be blocked here if there is a writer holding the semaphore.
	// But it's going to be unblocked by the signal we sent.
	q.semaphore.acquire()
	defer q.semaphore.release()

	close(q.events)

	return nil
}
