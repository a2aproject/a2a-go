// Copyright 2026 The A2A Authors
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

package taskexec

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/internal/eventpipe"
)

const (
	firstOutputPending uint32 = iota
	firstOutputWritten
	firstOutputExpired
)

type firstOutputTracker struct {
	timeout time.Duration
	matcher func(a2a.Event) bool
	written chan struct{}
	state   atomic.Uint32
}

func newFirstOutputTracker(timeout time.Duration, matcher func(a2a.Event) bool) *firstOutputTracker {
	if timeout <= 0 {
		return nil
	}
	return &firstOutputTracker{timeout: timeout, matcher: matcher, written: make(chan struct{})}
}

func (t *firstOutputTracker) wait(ctx context.Context, timer *time.Timer) error {
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return context.Cause(ctx)
	case <-t.written:
		return nil
	case <-timer.C:
		// Resolve a simultaneous write and expiry once, even when both select cases are ready.
		if t.state.CompareAndSwap(firstOutputPending, firstOutputExpired) {
			return ErrAgentFirstOutputTimeout
		}
		return nil
	}
}

func newFirstOutputTrackingWriter(inner eventpipe.Writer, tracker *firstOutputTracker) eventpipe.Writer {
	if tracker == nil {
		return inner
	}
	return &firstOutputTrackingWriter{inner: inner, tracker: tracker}
}

type firstOutputTrackingWriter struct {
	inner   eventpipe.Writer
	tracker *firstOutputTracker
}

func (w *firstOutputTrackingWriter) Write(ctx context.Context, event a2a.Event) error {
	t := w.tracker
	// Inspect the event before publishing it to the concurrently running consumer.
	matches := t.state.Load() == firstOutputPending && (t.matcher == nil || t.matcher(event))
	if err := w.inner.Write(ctx, event); err != nil {
		return err
	}
	if matches && t.state.CompareAndSwap(firstOutputPending, firstOutputWritten) {
		close(t.written)
	}
	return nil
}
