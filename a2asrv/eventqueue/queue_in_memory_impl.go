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

	"github.com/a2aproject/a2a-go/a2a"
)

const defaultMaxQueueSize = 1024

type semaphore struct {
	tokens chan any
}

// Implements Queue interface
type inMemoryQueue struct {
	// semaphore plays the role of a mutex for events channel, but provides
	// acquireInContext method which unblocks if context.Context get canceled.
	// can be held for a long time if Write() blocks on trying to write to a full channel.
	semaphore *semaphore
	// events channel is the queue backing mechanism.
	events chan a2a.Event

	// closeMu is acquired by Close() for the whole duration of method execution.
	// If there are concurrent Close() calls the first one to acquire the mutex ensures the queue
	// is canceled and others wait for it to finish.
	// We do this to guarantee that no Writes are accepted after Close() exits.
	closeMu sync.Mutex
	// closed indicates that the queue has been closed but still can be drained by Read().
	// We use atomic.Bool because Write() reads closed state without acquiring closeMu.
	closed atomic.Bool
	// closeChan is closed by Close() to ensure Write() calls are not blocked on trying to write
	// to a full events channel, preventing Close() to close it.
	closeChan chan struct{}
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
		closeChan: make(chan struct{}),
	}
}

func (q *inMemoryQueue) Write(ctx context.Context, event a2a.Event) error {
	if err := q.semaphore.acquireWithContext(ctx); err != nil {
		return err
	}
	defer q.semaphore.release()

	if q.closed.Load() {
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

	if q.closed.Load() {
		return nil
	}
	q.closed.Store(true)

	// Ensure there's no Write() holding the semaphore blocked on trying to write to a full channel.
	close(q.closeChan)

	// It might be blocked here if there is a writer holding the semaphore.
	// But it's going to be unblocked by the signal we sent.
	q.semaphore.acquire()
	defer q.semaphore.release()

	close(q.events)

	return nil
}
