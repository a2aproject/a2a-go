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
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/a2aproject/a2a-go/a2a"
)

func TestInMemoryManager_GetOrCreate(t *testing.T) {
	t.Parallel()
	m := NewInMemoryManager()
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

func TestInMemoryManager_DestroyExisting(t *testing.T) {
	t.Parallel()
	m := NewInMemoryManager()
	taskID := a2a.TaskID("task-1")
	ctx := t.Context()
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
	err = q.Write(ctx, &a2a.Message{ID: "test"})
	if err == nil || !errors.Is(err, ErrQueueClosed) {
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

func TestInMemoryManager_DestroyNonExistent(t *testing.T) {
	t.Parallel()
	m := NewInMemoryManager()
	taskID := a2a.TaskID("task-1")
	ctx := t.Context()

	wantErr := fmt.Sprintf("queue for taskId: %s does not exist", taskID)
	err := m.Destroy(ctx, taskID)
	if err == nil {
		t.Error("Destroy() on non-existent queue should have returned an error, but got nil")
	}
	if err.Error() != wantErr {
		t.Errorf("Destroy() error = %v, want %v", err, wantErr)
	}
}

func TestInMemoryManager_ConcurrentCreation(t *testing.T) {
	t.Parallel()
	m := NewInMemoryManager()
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

	imqm := m.(*inMemoryManager)
	if len(imqm.queues) != numTaskIDs {
		t.Fatalf("Expected %d queues to be created, but got %d", numTaskIDs, len(imqm.queues))
	}
}
