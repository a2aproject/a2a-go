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
	"fmt"
	"sync"
	"testing"

	"github.com/a2aproject/a2a-go/a2a"
)

func TestInMemoryEventQueue_WriteRead(t *testing.T) {
	t.Parallel()
	q, err := newEventQueue()
	if err != nil {
		t.Fatalf("failed to create event queue: %v", err)
	}
	defer func() {
		if err := q.Close(); err != nil {
			t.Fatalf("failed to close event queue: %v", err)
		}
	}()

	want := a2a.Message{MessageID: "test-event"}
	if err := q.Write(context.Background(), want); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	got, err := q.Read(context.Background())
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}

	if got.(a2a.Message).MessageID != want.MessageID {
		t.Errorf("Read() got = %v, want %v", got, want)
	}
}

func TestInMemoryEventQueue_ReadEmpty(t *testing.T) {
	t.Parallel()
	q, err := newEventQueue()
	if err != nil {
		t.Fatalf("failed to create event queue: %v", err)
	}
	defer func() {
		if err := q.Close(); err != nil {
			t.Fatalf("failed to close event queue: %v", err)
		}
	}()

	_, err = q.Read(context.Background())
	if err == nil {
		t.Error("Read() on empty queue should have returned an error, but got nil")
	}
	wantErr := "queue is empty"
	if err.Error() != wantErr {
		t.Errorf("Read() error = %v, want %v", err.Error(), wantErr)
	}
}

func TestInMemoryEventQueue_WriteFull(t *testing.T) {
	t.Parallel()
	// This test depends on the non-blocking nature of Write.
	// We create a queue with size 1 to test the full condition.
	q := &InMemoryEventQueue{
		events: make(chan a2a.Event, 1),
	}
	defer func() {
		if err := q.Close(); err != nil {
			t.Fatalf("failed to close event queue: %v", err)
		}
	}()

	// Fill the queue
	if err := q.Write(context.Background(), a2a.Message{MessageID: "1"}); err != nil {
		t.Fatalf("Write() failed unexpectedly: %v", err)
	}

	// Try to write to the full queue
	err := q.Write(context.Background(), a2a.Message{MessageID: "2"})
	if err == nil {
		t.Error("Write() to full queue should have returned an error, but got nil")
	}
	wantErr := "queue is full"
	if err.Error() != wantErr {
		t.Errorf("Write() error = %v, want %v", err.Error(), wantErr)
	}
}

func TestInMemoryEventQueue_Close(t *testing.T) {
	t.Parallel()
	q, err := newEventQueue()
	if err != nil {
		t.Fatalf("failed to create event queue: %v", err)
	}

	if err := q.Close(); err != nil {
		t.Fatalf("failed to close event queue: %v", err)
	}

	// Writing to a closed queue should fail
	err = q.Write(context.Background(), a2a.Message{MessageID: "test"})
	if err == nil {
		t.Error("Write() to closed queue should have returned an error, but got nil")
	}
	wantErr := "queue is closed"
	if err.Error() != wantErr {
		t.Errorf("Write() error = %v, want %v", err.Error(), wantErr)
	}

	// Reading from a closed queue should fail
	_, err = q.Read(context.Background())
	if err == nil {
		t.Error("Read() from closed queue should have returned an error, but got nil")
	}
	if err.Error() != wantErr {
		t.Errorf("Read() error = %v, want %v", err.Error(), wantErr)
	}

	// Closing again should be a no-op and not panic
	if err := q.Close(); err != nil {
		t.Fatalf("failed to close event queue: %v", err)
	}
}

func TestInMemoryEventQueue_WriteWithCanceledContext(t *testing.T) {
	t.Parallel()
	// To test context cancellation on Write, the queue must be full.
	q := &InMemoryEventQueue{
		events: make(chan a2a.Event, 1),
	}
	defer q.Close()

	// Fill the queue
	if err := q.Write(context.Background(), a2a.Message{MessageID: "1"}); err != nil {
		t.Fatalf("Write() failed unexpectedly: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel the context immediately

	err := q.Write(ctx, a2a.Message{MessageID: "2"})
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
	ctx := context.Background()

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
	ctx := context.Background()

	// Destroying a non-existent queue should return an error
	err := m.Destroy(ctx, taskID)
	if err == nil {
		t.Error("Destroy() on non-existent queue should have returned an error, but got nil")
	}
	wantErr := fmt.Sprintf("queue for taskId: %s does not exist", taskID)
	if err.Error() != wantErr {
		t.Errorf("Destroy() error = %v, want %v", err.Error(), wantErr)
	}

	// Create a queue
	q, err := m.GetOrCreate(ctx, taskID)
	if err != nil {
		t.Fatalf("GetOrCreate() failed: %v", err)
	}

	// Destroy the existing queue
	if err := m.Destroy(ctx, taskID); err != nil {
		t.Fatalf("Destroy() failed: %v", err)
	}

	// Verify the queue is closed
	err = q.Write(ctx, a2a.Message{MessageID: "test"})
	if err == nil || err.Error() != "queue is closed" {
		t.Errorf("Queue should be closed after manager destroys it, but Write() returned %v", err)
	}

	// Verify the queue is removed from the manager
	imqm := m.(*InMemoryQueueManager)
	imqm.mu.Lock()
	_, exists := imqm.queues[taskID]
	imqm.mu.Unlock()
	if exists {
		t.Error("Queue should be removed from manager after Destroy(), but it still exists")
	}
}

func TestInMemoryQueueManager_Concurrency(t *testing.T) {
	t.Parallel()
	m := NewInMemoryQueueManager()
	ctx := context.Background()
	var wg sync.WaitGroup
	numGoroutines := 100
	numTaskIDs := 10

	queues := make(map[a2a.TaskID]*InMemoryEventQueue)
	var mu sync.Mutex

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
			typedQ := q.(*InMemoryEventQueue) // Type assertion
			mu.Lock()
			if existingQ, ok := queues[taskID]; ok {
				if existingQ != typedQ {
					t.Errorf("Concurrent GetOrCreate() returned different queue instances for the same task ID")
				}
			} else {
				queues[taskID] = typedQ
			}
			mu.Unlock()
		}(i)
	}

	wg.Wait()

	imqm := m.(*InMemoryQueueManager)
	imqm.mu.Lock()
	defer imqm.mu.Unlock()
	if len(imqm.queues) != numTaskIDs {
		t.Errorf("Expected %d queues to be created, but got %d", numTaskIDs, len(imqm.queues))
	}
}
