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
	"testing"

	"github.com/a2aproject/a2a-go/a2a"
)

type testQueue struct {
	events chan a2a.Event
	err    chan error
}

func (q *testQueue) Read(ctx context.Context) (a2a.Event, error) {
	select {
	case e := <-q.events:
		return e, nil
	case err := <-q.err:
		return nil, err
	}
}

func newTestQueue() *testQueue {
	return &testQueue{events: make(chan a2a.Event), err: make(chan error)}
}

func makeUnbufferedChannels() (chan a2a.Event, chan error) {
	eventChan := make(chan a2a.Event)
	errorChan := make(chan error)
	return eventChan, errorChan
}

func TestReadQueueToChannels_WriteMultipleEvents(t *testing.T) {
	eventChan, errChan := makeUnbufferedChannels()
	queue := newTestQueue()

	ctx, cancelCtx := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		readQueueToChannels(ctx, queue, eventChan, errChan)
		close(done)
	}()
	read := []*a2a.Task{}
	send := []*a2a.Task{{ID: "1"}, {ID: "2"}, {ID: "3"}}
	for _, event := range send {
		queue.events <- event
		read = append(read, (<-eventChan).(*a2a.Task))
	}
	queue.err <- fmt.Errorf("failed")
	cancelCtx()
	<-done

	for i, sent := range send {
		if sent.ID != read[i].ID {
			t.Fatalf("expected ID at %d to be %s, got %s", i, sent.ID, read[i].ID)
		}
	}
}

func TestReadQueueToChannels_StopsWhenReadFails(t *testing.T) {
	eventChan, errChan := makeUnbufferedChannels()
	queue := newTestQueue()

	done := make(chan struct{})
	go func() {
		readQueueToChannels(t.Context(), queue, eventChan, errChan)
		close(done)
	}()

	wantErr := errors.New("read failure")
	queue.err <- wantErr
	gotErr := <-errChan
	<-done

	if !errors.Is(gotErr, wantErr) {
		t.Fatalf("expected read to fail with %v, got %v", wantErr, gotErr)
	}
}

func TestReadQueueToChannels_StopsEventWriteWhenContextCanceled(t *testing.T) {
	eventChan, errChan := makeUnbufferedChannels()
	queue := newTestQueue()

	ctx, cancelCtx := context.WithCancel(t.Context())

	done := make(chan struct{})
	go func() {
		readQueueToChannels(ctx, queue, eventChan, errChan)
		close(done)
	}()
	queue.events <- &a2a.Task{}
	cancelCtx()
	<-done

	// Expect readQueueToChannels exits without eventChan reader
}

func TestReadQueueToChannels_StopsErrorWriteWhenContextCanceled(t *testing.T) {
	eventChan, errChan := makeUnbufferedChannels()
	queue := newTestQueue()

	ctx, cancelCtx := context.WithCancel(t.Context())

	done := make(chan struct{})
	go func() {
		readQueueToChannels(ctx, queue, eventChan, errChan)
		close(done)
	}()
	queue.err <- errors.New("failed")
	cancelCtx()
	<-done

	// Expect readQueueToChannels exist without errChan reader
}

func TestRunProducerConsumer(t *testing.T) {
	panicFn := func() error { panic("panic") }
	msg := a2a.NewMessage(a2a.MessageRoleUser)

	testCases := []struct {
		name     string
		producer eventProducerFn
		consumer eventConsumerFn
		wantErr  bool
	}{
		{
			name:     "success",
			producer: func(ctx context.Context) error { return nil },
			consumer: func(ctx context.Context) (a2a.SendMessageResult, error) { return msg, nil },
		},
		{
			name:     "producer panic",
			producer: func(ctx context.Context) error { return panicFn() },
			consumer: func(ctx context.Context) (a2a.SendMessageResult, error) { return msg, nil },
			wantErr:  true,
		},
		{
			name:     "consumer panic",
			producer: func(ctx context.Context) error { return nil },
			consumer: func(ctx context.Context) (a2a.SendMessageResult, error) { return nil, panicFn() },
			wantErr:  true,
		},
		{
			name:     "producer error",
			producer: func(ctx context.Context) error { return fmt.Errorf("error") },
			consumer: func(ctx context.Context) (a2a.SendMessageResult, error) { return msg, nil },
			wantErr:  true,
		},
		{
			name:     "consumer error",
			producer: func(ctx context.Context) error { return nil },
			consumer: func(ctx context.Context) (a2a.SendMessageResult, error) { return nil, fmt.Errorf("error") },
			wantErr:  true,
		},
		{
			name:     "nil consumer result",
			producer: func(ctx context.Context) error { return nil },
			consumer: func(ctx context.Context) (a2a.SendMessageResult, error) { return nil, nil },
			wantErr:  true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := runProducerConsumer(t.Context(), tc.producer, tc.consumer)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got %v", result)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected result, got %v, %v", result, err)
			}
			if result == nil && err == nil {
				t.Fatalf("expected non-nil error when result is nil")
			}
		})
	}

}
