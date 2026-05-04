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

package taskexec

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"
)

func TestRunProducerConsumer(t *testing.T) {
	panicFn := func(str string) error { panic(str) }
	msg := a2a.NewMessage(a2a.MessageRoleUser)

	testCases := []struct {
		name         string
		producer     eventProducerFn
		consumer     eventConsumerFn
		panicHandler PanicHandlerFn
		wantErr      error
	}{
		{
			name:     "success",
			producer: func(ctx context.Context) error { return nil },
			consumer: func(ctx context.Context) (a2a.SendMessageResult, error) { return msg, nil },
		},
		{
			name:     "producer panic",
			producer: func(ctx context.Context) error { return panicFn("panic!") },
			consumer: func(ctx context.Context) (a2a.SendMessageResult, error) {
				<-ctx.Done()
				return nil, nil
			},
			wantErr: fmt.Errorf("event producer panic: panic!"),
		},
		{
			name:     "consumer panic",
			producer: func(ctx context.Context) error { return nil },
			consumer: func(ctx context.Context) (a2a.SendMessageResult, error) { return nil, panicFn("panic!") },
			wantErr:  fmt.Errorf("event consumer panic: panic!"),
		},
		{
			name:     "producer error",
			producer: func(ctx context.Context) error { return fmt.Errorf("error") },
			consumer: func(ctx context.Context) (a2a.SendMessageResult, error) {
				<-ctx.Done()
				return nil, nil
			},
			wantErr: fmt.Errorf("error"),
		},
		{
			name:     "producer error override by consumer result",
			producer: func(ctx context.Context) error { return fmt.Errorf("error") },
			consumer: func(ctx context.Context) (a2a.SendMessageResult, error) {
				<-ctx.Done()
				return &a2a.Task{Status: a2a.TaskStatus{State: a2a.TaskStateFailed}}, nil
			},
		},
		{
			name:     "consumer error",
			producer: func(ctx context.Context) error { return nil },
			consumer: func(ctx context.Context) (a2a.SendMessageResult, error) { return nil, fmt.Errorf("error") },
			wantErr:  fmt.Errorf("error"),
		},
		{
			name:     "nil consumer result",
			producer: func(ctx context.Context) error { return nil },
			consumer: func(ctx context.Context) (a2a.SendMessageResult, error) { return nil, nil },
			wantErr:  fmt.Errorf("bug: consumer stopped, but result unset: consumer stopped"),
		},
		{
			name: "producer context canceled on consumer non-nil result",
			producer: func(ctx context.Context) error {
				<-ctx.Done()
				return ctx.Err()
			},
			consumer: func(ctx context.Context) (a2a.SendMessageResult, error) { return msg, nil },
		},
		{
			name: "producer context canceled on consumer error result",
			producer: func(ctx context.Context) error {
				<-ctx.Done()
				return ctx.Err()
			},
			consumer: func(ctx context.Context) (a2a.SendMessageResult, error) { return nil, fmt.Errorf("error") },
			wantErr:  fmt.Errorf("error"),
		},
		{
			name:         "consumer panic custom handler",
			producer:     func(ctx context.Context) error { return nil },
			consumer:     func(ctx context.Context) (a2a.SendMessageResult, error) { return nil, panicFn("panic!") },
			panicHandler: func(err any) error { return fmt.Errorf("custom error") },
			wantErr:      fmt.Errorf("custom error"),
		},
		{
			name:     "producer panic custom handler",
			producer: func(ctx context.Context) error { return panicFn("panic!") },
			consumer: func(ctx context.Context) (a2a.SendMessageResult, error) {
				<-ctx.Done()
				return nil, nil
			},
			panicHandler: func(err any) error { return fmt.Errorf("custom error") },
			wantErr:      fmt.Errorf("custom error"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := runProducerConsumer(t.Context(), tc.producer, tc.consumer, nil, tc.panicHandler, nil)
			if tc.wantErr != nil && err == nil {
				t.Fatalf("expected error, got %v", result)
			}
			if tc.wantErr == nil && err != nil {
				t.Fatalf("expected result, got %v, %v", result, err)
			}
			if tc.wantErr != nil && !strings.Contains(err.Error(), tc.wantErr.Error()) {
				t.Fatalf("expected error = %s, got %s", tc.wantErr.Error(), err.Error())
			}
			if result == nil && err == nil {
				t.Fatalf("expected non-nil error when result is nil")
			}
		})
	}
}

func TestRunProducerConsumer_CausePropagation(t *testing.T) {
	consumerErr := taskstore.ErrConcurrentModification
	var gotProducerErr error
	_, _ = runProducerConsumer(t.Context(),
		func(ctx context.Context) error {
			<-ctx.Done()
			gotProducerErr = context.Cause(ctx)
			return nil
		},
		func(ctx context.Context) (a2a.SendMessageResult, error) {
			return nil, consumerErr
		},
		nil,
		nil,
		nil,
	)
	if gotProducerErr != consumerErr {
		t.Fatalf("expected producer error = %s, got %s", consumerErr, gotProducerErr)
	}
}

func TestRunProducerConsumer_InactivityTimeout(t *testing.T) {
	t.Parallel()

	t.Run("producer stalls without writing events", func(t *testing.T) {
		t.Parallel()
		tracker := newInactivityTracker(20 * time.Millisecond)
		var producerCause error

		_, err := runProducerConsumer(
			t.Context(),
			func(ctx context.Context) error {
				<-ctx.Done()
				producerCause = context.Cause(ctx)
				return ctx.Err()
			},
			func(ctx context.Context) (a2a.SendMessageResult, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			},
			nil,
			nil,
			tracker,
		)
		if !errors.Is(err, ErrAgentInactivityTimeout) {
			t.Fatalf("runProducerConsumer() error = %v, want errors.Is(_, ErrAgentInactivityTimeout)", err)
		}
		if !errors.Is(producerCause, ErrAgentInactivityTimeout) {
			t.Fatalf("context.Cause(producerCtx) = %v, want errors.Is(_, ErrAgentInactivityTimeout)", producerCause)
		}
	})

	t.Run("activity signals reset the timer", func(t *testing.T) {
		t.Parallel()
		tracker := newInactivityTracker(20 * time.Millisecond)

		producerStarted := make(chan struct{})
		releaseProducer := make(chan struct{})
		_, err := runProducerConsumer(
			t.Context(),
			func(ctx context.Context) error {
				close(producerStarted)
				// Send activity signals at half the timeout interval, so the
				// watcher's timer keeps getting reset and never fires.
				ticker := time.NewTicker(10 * time.Millisecond)
				defer ticker.Stop()
				for {
					select {
					case <-releaseProducer:
						return nil
					case <-ctx.Done():
						return ctx.Err()
					case <-ticker.C:
						tracker.record()
					}
				}
			},
			func(ctx context.Context) (a2a.SendMessageResult, error) {
				<-producerStarted
				// Hold the consumer open long enough (4x the timeout) that,
				// without timer resets, the watcher would have fired
				// multiple times.
				time.Sleep(80 * time.Millisecond)
				close(releaseProducer)
				return a2a.NewMessage(a2a.MessageRoleUser), nil
			},
			nil,
			nil,
			tracker,
		)
		if err != nil {
			t.Fatalf("runProducerConsumer() error = %v, want nil (timer should have been reset)", err)
		}
	})

	t.Run("zero timeout does not start watcher", func(t *testing.T) {
		t.Parallel()
		// With timeout=0 newInactivityTracker returns nil, so the watcher
		// must not be started at all. We verify by running a long-lived
		// producer and asserting completion is driven only by the
		// consumer's result.
		_, err := runProducerConsumer(
			t.Context(),
			func(ctx context.Context) error {
				<-ctx.Done()
				return ctx.Err()
			},
			func(ctx context.Context) (a2a.SendMessageResult, error) {
				time.Sleep(5 * time.Millisecond)
				return a2a.NewMessage(a2a.MessageRoleUser), nil
			},
			nil,
			nil,
			newInactivityTracker(0),
		)
		if err != nil {
			t.Fatalf("runProducerConsumer() error = %v, want nil", err)
		}
	})
}

func TestActivityTrackingWriter(t *testing.T) {
	t.Parallel()

	t.Run("nil tracker returns inner writer unchanged", func(t *testing.T) {
		t.Parallel()
		inner := &fakeWriter{}
		got := newActivityTrackingWriter(inner, nil)
		if got != inner {
			t.Fatalf("newActivityTrackingWriter(_, nil) = %v, want inner writer", got)
		}
	})

	t.Run("successful write signals tracker", func(t *testing.T) {
		t.Parallel()
		inner := &fakeWriter{}
		tracker := newInactivityTracker(time.Hour) // any positive timeout
		w := newActivityTrackingWriter(inner, tracker)

		if err := w.Write(t.Context(), a2a.NewMessage(a2a.MessageRoleUser)); err != nil {
			t.Fatalf("Write() error = %v, want nil", err)
		}
		select {
		case <-tracker.writeRecorded:
		default:
			t.Fatalf("expected signal after successful Write")
		}
	})

	t.Run("failed write does not signal", func(t *testing.T) {
		t.Parallel()
		inner := &fakeWriter{err: errors.New("boom")}
		tracker := newInactivityTracker(time.Hour)
		w := newActivityTrackingWriter(inner, tracker)

		if err := w.Write(t.Context(), a2a.NewMessage(a2a.MessageRoleUser)); err == nil {
			t.Fatalf("Write() error = nil, want non-nil")
		}
		select {
		case <-tracker.writeRecorded:
			t.Fatalf("expected no signal after failed Write")
		default:
		}
	})

	t.Run("signal is non-blocking when channel is full", func(t *testing.T) {
		t.Parallel()
		inner := &fakeWriter{}
		tracker := newInactivityTracker(time.Hour)
		// Pre-fill the channel so a non-blocking send must drop the new signal.
		tracker.writeRecorded <- struct{}{}
		w := newActivityTrackingWriter(inner, tracker)

		// Should return without blocking even though the channel is full.
		done := make(chan error, 1)
		go func() {
			done <- w.Write(t.Context(), a2a.NewMessage(a2a.MessageRoleUser))
		}()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Write() error = %v, want nil", err)
			}
		case <-time.After(50 * time.Millisecond):
			t.Fatalf("Write() blocked when signal channel was full")
		}
	})
}

type fakeWriter struct {
	err error
}

func (w *fakeWriter) Write(_ context.Context, _ a2a.Event) error {
	return w.err
}
