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

package eventpipe

import (
	"context"
	"sync/atomic"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv/eventqueue"
)

const defaultBufferSize = 256

// Reader is an interface for reading events.
type Reader interface {
	// Read dequeues an event or blocks if the queue is empty.
	Read(ctx context.Context) (a2a.Event, error)
}

// Writer is an interface for writing events.
type Writer interface {
	// Write enqueues an event or blocks if the queue is full.
	Write(ctx context.Context, event a2a.Event) error
}

type localOptions struct {
	bufferSize int
}

// LocalPipeOption is a functional option for configuring a local pipe.
type LocalPipeOption func(*localOptions)

// WithBufferSize configures the size of the event buffer for the local pipe.
func WithBufferSize(size int) LocalPipeOption {
	return func(opts *localOptions) {
		opts.bufferSize = size
	}
}

// Local represents a local event pipe with a reader and a writer.
type Local struct {
	Reader Reader
	Writer Writer

	closeWriter func()
	writer      *pipeWriter
}

type pipeEvent struct {
	event    a2a.Event
	dequeued chan struct{}
}

// NewLocal creates a new local event pipe.
func NewLocal(opts ...LocalPipeOption) *Local {
	options := &localOptions{bufferSize: defaultBufferSize}
	for _, opt := range opts {
		opt(options)
	}
	events := make(chan pipeEvent, options.bufferSize)

	writer := &pipeWriter{events: events, closeChan: make(chan struct{})}
	pipe := &Local{
		Writer:      writer,
		Reader:      &pipeReader{events: events},
		closeWriter: writer.close,
		writer:      writer,
	}
	return pipe
}

type pipeWriter struct {
	events chan pipeEvent

	closed    atomic.Bool
	closeChan chan struct{}
}

var _ Writer = (*pipeWriter)(nil)

func (w *pipeWriter) Write(ctx context.Context, event a2a.Event) error {
	return w.enqueue(ctx, pipeEvent{event: event})
}

func (w *pipeWriter) enqueue(ctx context.Context, event pipeEvent) error {
	if w.closed.Load() {
		return eventqueue.ErrQueueClosed
	}

	select {
	case w.events <- event:
		return nil
	case <-w.closeChan:
		return eventqueue.ErrQueueClosed
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *pipeWriter) close() {
	if w.closed.CompareAndSwap(false, true) {
		close(w.closeChan)
	}
}

type pipeReader struct {
	events chan pipeEvent
}

func (r *pipeReader) Read(ctx context.Context) (a2a.Event, error) {
	select { // readers are allowed to drain the channel after pipe is closed
	case event, ok := <-r.events:
		if !ok {
			return nil, eventqueue.ErrQueueClosed
		}
		if event.dequeued != nil {
			close(event.dequeued)
		}
		return event.event, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Close closes the local event pipe.
func (q *Local) Close() {
	q.closeWriter()
}

// WriteTracked enqueues an event without waiting for its consumption. The
// returned channel is closed if the event is read. A nil channel means the
// consumer had already stopped before the event could be enqueued.
func (q *Local) WriteTracked(ctx context.Context, event a2a.Event, consumerDone <-chan struct{}) (<-chan struct{}, error) {
	dequeued := make(chan struct{})
	if q.writer.closed.Load() {
		return nil, eventqueue.ErrQueueClosed
	}
	select {
	case q.writer.events <- pipeEvent{event: event, dequeued: dequeued}:
		return dequeued, nil
	case <-consumerDone:
		return nil, nil
	case <-q.writer.closeChan:
		return nil, eventqueue.ErrQueueClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
