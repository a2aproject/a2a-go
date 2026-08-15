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
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/go-cmp/cmp"
)

func TestInMemoryManager(t *testing.T) {
	ctx, tid := t.Context(), a2a.NewTaskID()

	manager := NewInMemoryManager()
	reader, err := manager.CreateReader(ctx, tid)
	if err != nil {
		t.Fatalf("manager.CreateReader() error = %v", err)
	}
	writer, err := manager.CreateWriter(ctx, tid)
	if err != nil {
		t.Fatalf("manager.CreateWriter() error = %v", err)
	}
	wantEvent := a2a.NewMessage(a2a.MessageRoleUser)
	doneChan := make(chan struct{})
	go func() {
		if err := writer.Write(ctx, &Message{Event: wantEvent}); err != nil {
			t.Errorf("writer.Write() error = %v", err)
		}
		close(doneChan)
	}()
	got, err := reader.Read(ctx)
	if err != nil {
		t.Fatalf("reader.Read() error = %v", err)
	}
	if diff := cmp.Diff(wantEvent, got.Event); diff != "" {
		t.Fatalf("reader.Read() wrong result (-want +got) diff = %s", diff)
	}
	<-doneChan
	if err := manager.Destroy(ctx, tid); err != nil {
		t.Fatalf("manager.Destroy() error = %v", err)
	}
	if err := writer.Write(ctx, &Message{Event: wantEvent}); !errors.Is(err, ErrQueueClosed) {
		t.Errorf("writer.Write() error = %v, want %v", err, ErrQueueClosed)
	}
}

func TestInMemoryManager_ConcurrentCreation(t *testing.T) {
	type taskQueue struct {
		reader Reader
		taskID a2a.TaskID
	}

	t.Parallel()
	m := NewInMemoryManager()
	ctx := t.Context()
	var wg sync.WaitGroup
	numGoroutines, numTaskIDs := 100, 10

	created := make(chan taskQueue, numGoroutines)
	for i := range numGoroutines {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			taskID := a2a.TaskID(fmt.Sprintf("task-%d", i%numTaskIDs))
			q, err := m.CreateReader(ctx, taskID)
			if err != nil {
				t.Errorf("Concurrent GetOrCreate() failed: %v", err)
				return
			}
			created <- taskQueue{reader: q, taskID: taskID}
		}(i)
	}

	wg.Wait()
	close(created)

	// group all queues created concurrently by task ID
	createdMap := map[a2a.TaskID][]Reader{}
	for got := range created {
		createdMap[got.taskID] = append(createdMap[got.taskID], got.reader)
	}

	// for every task ID check that if we write a message using a queue, all the created queues will receive it
	for tid, queues := range createdMap {
		writeQueue, err := m.CreateWriter(ctx, tid)
		if err != nil {
			t.Errorf("CreateWriter() failed after concurrent creation: %v", err)
		}
		want := &a2a.Message{ID: a2a.NewMessageID()}
		if err := writeQueue.Write(ctx, &Message{Event: want}); err != nil {
			t.Fatalf("writeQueue.Write() error = %v", err)
		}
		for _, readQueue := range queues {
			got, err := readQueue.Read(ctx)
			if err != nil {
				t.Fatalf("readQueue.Read() error = %v", err)
			}
			if diff := cmp.Diff(want, got.Event); diff != "" {
				t.Fatalf("readQueue.Read() wrong result (-want +got) diff = %s", diff)
			}
		}
	}

	imqm := m.(*inMemoryManager)
	if len(imqm.brokers) != numTaskIDs {
		t.Fatalf("Expected %d queues to be created, but got %d", numTaskIDs, len(imqm.brokers))
	}
}

func TestInMemoryManager_DestroyHonorsContext(t *testing.T) {
	t.Parallel()
	m := NewInMemoryManager(WithQueueBufferSize(0))
	tid := a2a.NewTaskID()
	_, writer := mustCreateReadWriter(t, m, tid)
	manager := m.(*inMemoryManager)
	manager.mu.Lock()
	broker := manager.brokers[tid]
	manager.mu.Unlock()
	writeDone := make(chan error, 1)
	go func() {
		writeDone <- writer.Write(t.Context(), &Message{Event: &a2a.Message{ID: "blocked"}})
	}()
	select {
	case err := <-writeDone:
		t.Fatalf("Write() returned %v, want it to remain blocked", err)
	case <-time.After(20 * time.Millisecond):
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := m.Destroy(ctx, tid); !errors.Is(err, context.Canceled) {
		t.Fatalf("Destroy() error = %v, want %v", err, context.Canceled)
	}
	select {
	case <-writeDone:
	case <-time.After(time.Second):
		t.Fatal("Write() did not unblock after canceled Destroy() requested broker shutdown")
	}
	select {
	case <-broker.destroyed:
	case <-time.After(time.Second):
		t.Fatal("broker did not finish shutting down after canceled Destroy()")
	}
	reader, err := m.CreateReader(t.Context(), tid)
	if err != nil {
		t.Fatalf("CreateReader() after canceled Destroy() error = %v, want nil", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("replacement reader Close() error = %v, want nil", err)
	}
	if err := m.Destroy(t.Context(), tid); err != nil {
		t.Fatalf("second Destroy() error = %v, want nil", err)
	}
}
