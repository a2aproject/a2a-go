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
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
)

func TestInMemoryQueue_WriteRead(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	q := NewInMemoryQueue(3)
	defer func() {
		if err := q.Close(); err != nil {
			t.Fatalf("failed to close event queue: %v", err)
		}
	}()
	want := &a2a.Message{ID: "test-event"}
	if err := q.Write(ctx, want); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	got, err := q.Read(ctx)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Read() got = %v, want %v", got, want)
	}
}

func TestInMemoryQueue_WriteCloseRead(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	q := NewInMemoryQueue(3)
	want := []*a2a.Message{
		{ID: "test-event"},
		{ID: "test-event2"},
	}
	for _, w := range want {
		if err := q.Write(ctx, w); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
	}
	if err := q.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	var got []a2a.Event
	typedQ := q.(*inMemoryQueue)
	for range len(typedQ.events) {
		event, err := q.Read(ctx)
		if err != nil {
			t.Fatalf("Read() error = %v", err)
		}
		got = append(got, event)
	}
	if len(got) != len(want) {
		t.Fatalf("Read() got = %v, want %v", got, want)
	}
	for i, w := range want {
		if !reflect.DeepEqual(got[i], w) {
			t.Errorf("Read() got = %v, want %v", got, want)
		}
	}
}

func TestInMemoryQueue_ReadEmpty(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	q := NewInMemoryQueue(3)
	completed := make(chan struct{})

	go func() {
		_, err := q.Read(ctx)
		if err != nil {
			t.Errorf("Read() error = %v", err)
			return
		}
		close(completed)
	}()

	select {
	case <-completed:
		t.Fatal("method should be blocking")
	case <-time.After(20 * time.Millisecond):
		// unblock blocked code by writing to queue
		err := q.Write(ctx, &a2a.Message{ID: "test"})
		if err != nil {
			t.Fatalf("Write() error = %v", err)
		}
	}
}

func TestInMemoryQueue_WriteFull(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	q := NewInMemoryQueue(1)
	completed := make(chan struct{})

	if err := q.Write(ctx, &a2a.Message{ID: "1"}); err != nil {
		t.Fatalf("Write() failed unexpectedly: %v", err)
	}

	go func() {
		err := q.Write(ctx, &a2a.Message{ID: "2"})
		if err != nil {
			t.Errorf("Write() error = %v", err)
			return
		}
		close(completed)
	}()

	select {
	case <-completed:
		t.Fatal("method should be blocking")
	case <-time.After(20 * time.Millisecond):
		// unblock blocked code by realising queue buffer
		_, err := q.Read(ctx)
		if err != nil {
			t.Fatalf("Read() error = %v", err)
		}
	}
}

func TestInMemoryQueue_Close(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	q := NewInMemoryQueue(3)

	if err := q.Close(); err != nil {
		t.Fatalf("failed to close event queue: %v", err)
	}

	// Writing to a closed queue should fail
	err := q.Write(ctx, &a2a.Message{ID: "test"})
	if err == nil {
		t.Error("Write() to closed queue should have returned an error, but got nil")
	}
	wantErr := ErrQueueClosed
	if !errors.Is(err, wantErr) {
		t.Errorf("Write() error = %v, want %v", err, wantErr)
	}

	// Reading from a closed queue should fail
	_, err = q.Read(ctx)
	if err == nil {
		t.Error("Read() from closed queue should have returned an error, but got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("Read() error = %v, want %v", err, wantErr)
	}

	// Closing again should be a no-op and not panic
	if err := q.Close(); err != nil {
		t.Fatalf("failed to close event queue: %v", err)
	}
}

func TestInMemoryQueue_WriteWithCanceledContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	q := NewInMemoryQueue(1)

	// Fill the queue
	if err := q.Write(ctx, &a2a.Message{ID: "1"}); err != nil {
		t.Fatalf("Write() failed unexpectedly: %v", err)
	}

	cancel()

	err := q.Write(ctx, &a2a.Message{ID: "2"})
	if err == nil {
		t.Error("Write() with canceled context should have returned an error, but got nil")
	}
	if err != context.Canceled {
		t.Errorf("Write() error = %v, want %v", err, context.Canceled)
	}
}

func TestInMemoryQueue_BlockedWriteOnFullQueueThenClose(t *testing.T) {
	t.Parallel()
	q := NewInMemoryQueue(1)
	ctx := t.Context()
	completed1 := make(chan struct{})
	completed2 := make(chan struct{})
	event := &a2a.Message{ID: "test"}

	// Fill the queue
	if err := q.Write(ctx, &a2a.Message{ID: "1"}); err != nil {
		t.Fatalf("Write() failed unexpectedly: %v", err)
	}

	go func() {
		ctx1 := t.Context()
		err := q.Write(ctx1, event) // blocks on trying to write to a full channel
		if !errors.Is(err, ErrQueueClosed) {
			t.Errorf("Write1() error = %v, want %v", err, ErrQueueClosed)
			return
		}
		close(completed1)
	}()

	go func() {
		ctx2 := t.Context()
		err := q.Write(ctx2, event) // blocks on semaphore
		if !errors.Is(err, ErrQueueClosed) {
			t.Errorf("Write2() error = %v, want %v", err, ErrQueueClosed)
			return
		}
		close(completed2)
	}()

	select {
	case <-completed1:
		t.Fatal("method should be blocking")
	case <-completed2:
		t.Fatal("method should be blocking")
	case <-time.After(20 * time.Millisecond):
		// unblock blocked code by closing queue
		err := q.Close()
		if err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}
}
