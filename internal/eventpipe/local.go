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
	"fmt"
	"sync/atomic"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
)

const defaultBufferSize = 1024

type localOptions struct {
	bufferSize int
}

type LocalPipeOption func(*localOptions)

func WithBufferSize(size int) LocalPipeOption {
	return func(opts *localOptions) {
		opts.bufferSize = size
	}
}

type Local struct {
	Reader eventqueue.Reader
	// TODO(yarolegovich): change to eventqueue.Writer when AgentExecutor interface is updated
	Writer eventqueue.Queue

	closeWriter func()
}

func NewLocal(opts ...LocalPipeOption) *Local {
	options := &localOptions{bufferSize: defaultBufferSize}
	for _, opt := range opts {
		opt(options)
	}
	events := make(chan a2a.Event, options.bufferSize)

	writer := &pipeWriter{events: events, closeChan: make(chan struct{})}
	pipe := &Local{
		Writer:      writer,
		Reader:      &pipeReader{events: events},
		closeWriter: writer.close,
	}
	return pipe
}

type pipeWriter struct {
	events chan a2a.Event

	closed    atomic.Bool
	closeChan chan struct{}
}

func (w *pipeWriter) Write(ctx context.Context, event a2a.Event) error {
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

func (w *pipeWriter) Read(ctx context.Context) (a2a.Event, error) {
	return nil, fmt.Errorf("only queue write is allowed")
}

func (w *pipeWriter) Close() error {
	return fmt.Errorf("only queue write is allowed")
}

func (w *pipeWriter) close() {
	if w.closed.CompareAndSwap(false, true) {
		close(w.closeChan)
	}
}

type pipeReader struct {
	events chan a2a.Event
}

func (r *pipeReader) Read(ctx context.Context) (a2a.Event, error) {
	select { // readers are allowed to drain the channel after pipe is closed
	case event, ok := <-r.events:
		if !ok {
			return nil, eventqueue.ErrQueueClosed
		}
		return event, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (q *Local) Close() {
	q.closeWriter()
}
