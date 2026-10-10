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
	"errors"
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/internal/eventpipe"
)

func TestRunProducerConsumer_FirstOutputTimeout(t *testing.T) {
	t.Parallel()
	writeErr := errors.New("write failed")
	tests := []struct {
		name       string
		timeout    time.Duration
		inactivity time.Duration
		progress   bool
		output     bool
		anyEvent   bool
		blockWrite bool
		writeErr   error
		wantErr    error
	}{
		{name: "no events", timeout: time.Second, wantErr: ErrAgentFirstOutputTimeout},
		{name: "progress cannot extend deadline", timeout: time.Second, progress: true, wantErr: ErrAgentFirstOutputTimeout},
		{name: "output disarms deadline", timeout: time.Second, progress: true, output: true},
		{name: "nil matcher accepts first event", timeout: time.Second, progress: true, anyEvent: true},
		{name: "zero disables", timeout: 0},
		{name: "negative disables", timeout: -time.Second},
		{name: "failed write does not satisfy deadline", timeout: time.Second, output: true, writeErr: writeErr, wantErr: ErrAgentFirstOutputTimeout},
		{name: "blocked matching write does not satisfy deadline", timeout: time.Second, output: true, blockWrite: true, writeErr: context.Canceled, wantErr: ErrAgentFirstOutputTimeout},
		{name: "inactivity after first output", timeout: time.Second, inactivity: time.Second, output: true, wantErr: ErrAgentInactivityTimeout},
		{name: "inactivity before first output", timeout: time.Second, inactivity: time.Second / 2, wantErr: ErrAgentInactivityTimeout},
		{name: "progress keeps inactivity alive", timeout: time.Second, inactivity: time.Second / 2, progress: true, wantErr: ErrAgentFirstOutputTimeout},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				matcher := func(event a2a.Event) bool {
					_, ok := event.(*a2a.Message)
					return ok
				}
				if tc.anyEvent {
					matcher = nil
				}
				first := newFirstOutputTracker(tc.timeout, matcher)
				idle := newInactivityTracker(tc.inactivity)
				var inner eventpipe.Writer = &fakeWriter{err: tc.writeErr}
				if tc.blockWrite {
					pipe := eventpipe.NewLocal(eventpipe.WithBufferSize(0))
					defer pipe.Close()
					inner = pipe.Writer
				}
				writer := newFirstOutputTrackingWriter(newActivityTrackingWriter(inner, idle), first)
				completed := make(chan struct{})
				var cause error
				_, err := runProducerConsumer(t.Context(), func(ctx context.Context) error {
					defer func() { cause = context.Cause(ctx) }()
					ticker := time.NewTicker(100 * time.Millisecond)
					defer ticker.Stop()
					for i := range 20 {
						select {
						case <-ctx.Done():
							return context.Cause(ctx)
						case <-ticker.C:
						}
						var event a2a.Event
						if tc.output && i == 0 {
							event = a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("hello"))
						} else if tc.progress {
							event = a2a.NewStatusUpdateEvent(&a2a.Task{}, a2a.TaskStateWorking, nil)
						}
						if event != nil {
							if err := writer.Write(ctx, event); !errors.Is(err, tc.writeErr) {
								return fmt.Errorf("writer.Write() error = %v, want %v", err, tc.writeErr)
							}
						}
					}
					close(completed)
					return nil
				}, func(ctx context.Context) (a2a.SendMessageResult, error) {
					select {
					case <-completed:
						return a2a.NewMessage(a2a.MessageRoleAgent), nil
					case <-ctx.Done():
						return nil, context.Cause(ctx)
					}
				}, nil, nil, idle, first)
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("runProducerConsumer() error = %v, want %v", err, tc.wantErr)
				}
				if tc.wantErr != nil && !errors.Is(cause, tc.wantErr) {
					t.Fatalf("context.Cause() = %v, want %v", cause, tc.wantErr)
				}
			})
		})
	}
}

func TestFirstOutputTrackingWriter_ConcurrentExpiry(t *testing.T) {
	t.Parallel()
	for _, early := range []bool{false, true} {
		synctest.Test(t, func(t *testing.T) {
			tracker := newFirstOutputTracker(time.Second, nil)
			writer := newFirstOutputTrackingWriter(&fakeWriter{}, tracker)
			timer := time.NewTimer(time.Second)
			result := make(chan error, 1)
			if early {
				if err := writer.Write(t.Context(), a2a.NewMessage(a2a.MessageRoleAgent)); err != nil {
					t.Fatalf("writer.Write() error = %v, want nil", err)
				}
				time.Sleep(2 * time.Second)
			}
			go func() { result <- tracker.wait(t.Context(), timer) }()
			if !early {
				time.Sleep(time.Second)
			}
			for range 2 {
				if err := writer.Write(t.Context(), a2a.NewMessage(a2a.MessageRoleAgent)); err != nil {
					t.Fatalf("writer.Write() error = %v, want nil", err)
				}
			}
			if err := <-result; (early && err != nil) || (!early && err != nil && !errors.Is(err, ErrAgentFirstOutputTimeout)) {
				t.Fatalf("tracker.wait() error = %v, want resolved output/expiry race", err)
			}
		})
	}
}

func TestRunProducerConsumer_FirstOutputCleanup(t *testing.T) {
	t.Parallel()
	failure := errors.New("executor failed")
	for _, outcome := range []string{"completion", "error", "cancellation", "matcher panic"} {
		t.Run(outcome, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancelCause(t.Context())
				defer cancel(nil)
				tracker := newFirstOutputTracker(time.Hour, func(a2a.Event) bool { panic("matcher failure") })
				writer := newFirstOutputTrackingWriter(&fakeWriter{}, tracker)
				_, err := runProducerConsumer(ctx, func(ctx context.Context) error {
					if outcome == "matcher panic" {
						return writer.Write(ctx, a2a.NewMessage(a2a.MessageRoleAgent))
					}
					<-ctx.Done()
					return context.Cause(ctx)
				}, func(ctx context.Context) (a2a.SendMessageResult, error) {
					switch outcome {
					case "completion":
						return a2a.NewMessage(a2a.MessageRoleAgent), nil
					case "error":
						return nil, failure
					case "cancellation":
						cancel(failure)
					}
					<-ctx.Done()
					return nil, context.Cause(ctx)
				}, nil, func(any) error { return failure }, nil, tracker)
				want := failure
				if outcome == "completion" {
					want = nil
				}
				if !errors.Is(err, want) {
					t.Fatalf("runProducerConsumer() error = %v, want %v", err, want)
				}
			})
		})
	}
}
