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

package events

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
)

func TestInMemoryEventQueue_WriteRead(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	q := NewInMemoryQueue(3)
	defer func() {
		if err := q.Close(); err != nil {
			t.Fatalf("failed to close event queue: %v", err)
		}
	}()

	// write wanted event
	want := &a2a.Message{ID: "test-event"}
	if err := q.Write(ctx, want); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	// retrieve written event
	got, err := q.Read(ctx)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}

	// validate written event
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Read() got = %v, want %v", got, want)
	}
}

func TestInMemoryEventQueue_WriteCloseRead(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	q := NewInMemoryQueue(3)
	want := []*a2a.Message{
		{ID: "test-event"},
		{ID: "test-event2"},
	}
	// write wanted events
	for _, w := range want {
		if err := q.Write(ctx, w); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
	}

	// close queue
	if err := q.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// retrieve written events
	var got []a2a.Event
	for range len(q.events) {
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

func TestInMemoryEventQueue_ReadEmpty(t *testing.T) {
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
	case <-time.After(100 * time.Millisecond):
		// unblock blocked code by writing to queue
		err := q.Write(ctx, &a2a.Message{ID: "test"})
		if err != nil {
			t.Fatalf("Write() error = %v", err)
		}
	}
}

func TestInMemoryEventQueue_WriteFull(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	q := NewInMemoryQueue(1)
	completed := make(chan struct{})

	// Fill the queue
	if err := q.Write(ctx, &a2a.Message{ID: "1"}); err != nil {
		t.Fatalf("Write() failed unexpectedly: %v", err)
	}

	go func() {
		// Try to write to the full queue
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
	case <-time.After(100 * time.Millisecond):
		// unblock blocked code by realising queue buffer
		_, err := q.Read(ctx)
		if err != nil {
			t.Fatalf("Read() error = %v", err)
		}
	}
}

func TestInMemoryEventQueue_Close(t *testing.T) {
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
	wantErr := a2a.ErrQueueClosed
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

func TestInMemoryEventQueue_WriteWithCanceledContext(t *testing.T) {
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

func TestInMemoryQueueManager_GetOrCreate(t *testing.T) {
	t.Parallel()
	m := NewInMemoryQueueManager()
	taskID := a2a.TaskID("task-1")
	ctx := t.Context()

	// First call should create a queue
	q1, err := m.GetOrCreate(ctx, taskID)
	if err != nil {
		t.Fatalf("GetOrCreate() failed on first call: %v", err)
	}
	if q1 == nil {
		t.Fatal("GetOrCreate() returned a nil queue on first call")
	}

	// Second call should return the same queue
	q2, err := m.GetOrCreate(ctx, taskID)
	if err != nil {
		t.Fatalf("GetOrCreate() failed on second call: %v", err)
	}
	if q1 != q2 {
		t.Errorf("GetOrCreate() should return the same queue instance for the same task ID")
	}
}

func TestInMemoryQueueManager_Destroy(t *testing.T) {
	t.Parallel()
	m := NewInMemoryQueueManager()
	taskID := a2a.TaskID("task-1")
	ctx := t.Context()

	// Destroying a non-existent queue should return an error
	err := m.Destroy(ctx, taskID)
	if err == nil {
		t.Error("Destroy() on non-existent queue should have returned an error, but got nil")
	}
	wantErr := fmt.Sprintf("queue for taskId: %s does not exist", taskID)
	if err.Error() != wantErr {
		t.Errorf("Destroy() error = %v, want %v", err, wantErr)
	}

	// Create a queue
	q, err := m.GetOrCreate(ctx, taskID)
	if err != nil {
		t.Fatalf("GetOrCreate() failed: %v", err)
	}

	sameQ, err := m.GetOrCreate(ctx, taskID)
	if err != nil {
		t.Fatalf("GetOrCreate() failed: %v", err)
	}

	// Destroy the existing queue - q & sameQ
	if err := m.Destroy(ctx, taskID); err != nil {
		t.Fatalf("Destroy() failed: %v", err)
	}

	// Verify the queue is closed
	err = q.Write(ctx, &a2a.Message{ID: "test"})
	if err == nil || !errors.Is(err, a2a.ErrQueueClosed) {
		t.Errorf("Queue should be closed after manager destroys it, but Write() returned %v", err)
	}

	// Verify the queue is removed by creating a new queue with same taskID
	q2, err := m.GetOrCreate(ctx, taskID)
	if err != nil {
		t.Fatalf("GetOrCreate() failed after manager destroyed the queue: %v", err)
	}
	if q != sameQ {
		t.Fatalf("sameQ and q should be the same instance, but they are different")
	}
	if q == q2 {
		t.Fatalf("Destroyed queue should be removed from the manager, but it still exists")
	}
}

func TestInMemoryQueueManager_ConcurrentCreation(t *testing.T) {
	t.Parallel()
	m := NewInMemoryQueueManager()
	ctx := t.Context()
	var wg sync.WaitGroup
	numGoroutines := 100
	numTaskIDs := 10
	created := make(chan struct {
		queue  Queue
		taskId a2a.TaskID
	}, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			taskID := a2a.TaskID(fmt.Sprintf("task-%d", i%numTaskIDs))
			q, err := m.GetOrCreate(ctx, taskID)
			if err != nil {
				t.Errorf("Concurrent GetOrCreate() failed: %v", err)
				return
			}
			if q == nil {
				t.Error("Concurrent GetOrCreate() returned nil queue")
				return
			}
			created <- struct {
				queue  Queue
				taskId a2a.TaskID
			}{queue: q, taskId: taskID}
		}(i)
	}

	wg.Wait()
	close(created)

	for got := range created {
		existingQ, err := m.GetOrCreate(ctx, got.taskId)
		if err != nil {
			t.Errorf("GetOrCreate() failed after concurrent creation: %v", err)
		}
		if existingQ != got.queue {
			t.Fatalf("GetOrCreate() should return the same queue instance for the same task ID, but got different queues")
		}
	}

	imqm := m.(*inMemoryQueueManager)
	if len(imqm.queues) != numTaskIDs {
		t.Fatalf("Expected %d queues to be created, but got %d", numTaskIDs, len(imqm.queues))
	}
}
