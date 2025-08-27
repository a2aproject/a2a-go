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
	"fmt"
	"sync"
	"testing"

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
	want := a2a.Message{MessageID: "test-event"}
	if err := q.Write(ctx, want); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	// retrieve written event
	got, err := q.Read(ctx)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}

	// validtae written event
	if got.(a2a.Message).MessageID != want.MessageID {
		t.Errorf("Read() got = %v, want %v", got, want)
	}
}

func TestInMemoryEventQueue_WriteCloseRead(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	q := NewInMemoryQueue(3)

	want := []a2a.Message{
		{MessageID: "test-event"},
		{MessageID: "test-event2"},
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
	var got []a2a.Message
	for range len(q.events) {
		event, err := q.Read(ctx)
		if err != nil {
			t.Fatalf("Read() error = %v", err)
		}
		got = append(got, event.(a2a.Message))
	}

	if len(got) != len(want) {
		t.Fatalf("Read() got = %v, want %v", got, want)
	}

	for i, w := range want {
		if got[i].MessageID != w.MessageID {
			t.Errorf("Read() got = %v, want %v", got, want)
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
	err := q.Write(ctx, a2a.Message{MessageID: "test"})
	if err == nil {
		t.Error("Write() to closed queue should have returned an error, but got nil")
	}
	wantErr := a2a.ErrQueueClosed.Error()
	if err.Error() != wantErr {
		t.Errorf("Write() error = %v, want %v", err.Error(), wantErr)
	}

	// Reading from a closed queue should fail
	_, err = q.Read(ctx)
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
	if err == nil || err.Error() != a2a.ErrQueueClosed.Error() {
		t.Errorf("Queue should be closed after manager destroys it, but Write() returned %v", err)
	}

	// Verify the queue is removed from the manager
	imqm := m.(*inMemoryQueueManager)
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
	ctx := t.Context()
	var wg sync.WaitGroup
	numGoroutines := 100
	numTaskIDs := 10

	queues := make(map[a2a.TaskID]*inMemoryEventQueue)
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
			typedQ := q.(*inMemoryEventQueue) // Type assertion
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

	imqm := m.(*inMemoryQueueManager)
	imqm.mu.Lock()
	defer imqm.mu.Unlock()
	if len(imqm.queues) != numTaskIDs {
		t.Errorf("Expected %d queues to be created, but got %d", numTaskIDs, len(imqm.queues))
	}
}
