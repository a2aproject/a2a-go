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
	"sync/atomic"
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

func TestInMemoryManager_CreateDuringDestroyUsesReplacementBroker(t *testing.T) {
	t.Parallel()
	manager := newTestManager(t)
	tid := a2a.NewTaskID()
	reader, err := manager.CreateReader(t.Context(), tid)
	if err != nil {
		t.Fatalf("manager.CreateReader() error = %v, want nil", err)
	}
	queue := reader.(*inMemoryQueue)
	queue.dispatchMu.Lock()

	destroyDone := make(chan error, 1)
	go func() {
		destroyDone <- manager.Destroy(t.Context(), tid)
	}()
	select {
	case <-queue.broker.destroySignal:
	case <-time.After(time.Second):
		queue.dispatchMu.Unlock()
		t.Fatal("manager.Destroy() did not start broker shutdown")
	}

	writer, createErr := manager.CreateWriter(t.Context(), tid)
	queue.dispatchMu.Unlock()
	if err := <-destroyDone; err != nil {
		t.Fatalf("manager.Destroy() error = %v, want nil", err)
	}
	if createErr != nil {
		t.Fatalf("manager.CreateWriter() during Destroy error = %v, want nil", createErr)
	}
	if err := writer.Write(t.Context(), &Message{Event: &a2a.Message{ID: "replacement"}}); err != nil {
		t.Fatalf("writer.Write() error = %v, want nil", err)
	}
}

func TestInMemoryManager_CreateRetriesBrokerRemovedDuringConnect(t *testing.T) {
	t.Parallel()
	manager := newTestManager(t)
	tid := a2a.NewTaskID()
	reader, err := manager.CreateReader(t.Context(), tid)
	if err != nil {
		t.Fatalf("manager.CreateReader() error = %v, want nil", err)
	}
	queue := reader.(*inMemoryQueue)
	broker := queue.broker
	queue.dispatchMu.Lock()

	unregisterAccepted := make(chan struct{})
	go func() {
		broker.unregisterChan <- queue
		close(unregisterAccepted)
	}()
	select {
	case <-unregisterAccepted:
	case <-time.After(time.Second):
		queue.dispatchMu.Unlock()
		t.Fatal("broker did not start unregistering the existing reader")
	}

	type createResult struct {
		writer Writer
		err    error
	}
	createDone := make(chan createResult, 1)
	go func() {
		writer, err := manager.CreateWriter(t.Context(), tid)
		createDone <- createResult{writer: writer, err: err}
	}()
	deadline := time.Now().Add(time.Second)
	for broker.connectionRefs.Load() != 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := broker.connectionRefs.Load(); got != 2 {
		queue.dispatchMu.Unlock()
		t.Fatalf("broker connection references = %d, want 2", got)
	}

	destroyDone := make(chan error, 1)
	go func() {
		destroyDone <- manager.Destroy(t.Context(), tid)
	}()
	select {
	case <-broker.destroySignal:
	case <-time.After(time.Second):
		queue.dispatchMu.Unlock()
		t.Fatal("manager.Destroy() did not start broker shutdown")
	}
	result := <-createDone
	queue.dispatchMu.Unlock()
	if err := <-destroyDone; err != nil {
		t.Fatalf("manager.Destroy() error = %v, want nil", err)
	}
	if result.err != nil {
		t.Fatalf("manager.CreateWriter() error = %v, want nil", result.err)
	}
	if err := result.writer.Write(t.Context(), &Message{Event: &a2a.Message{ID: "replacement"}}); err != nil {
		t.Fatalf("writer.Write() error = %v, want nil", err)
	}
}

type destroyAndReplaceContext struct {
	context.Context
	manager *inMemoryManager
	taskID  a2a.TaskID
	calls   atomic.Int32
	err     error
}

func (c *destroyAndReplaceContext) Err() error {
	switch c.calls.Add(1) {
	case 2:
		c.err = c.manager.Destroy(context.Background(), c.taskID)
	case 3:
		stale := newInMemoryEventBroker(c.manager.bufferSize, c.manager.subscriberTimeout)
		if err := stale.destroy(context.Background()); err != nil {
			c.err = err
			break
		}
		c.manager.mu.Lock()
		c.manager.brokers[c.taskID] = stale
		c.manager.mu.Unlock()
	}
	return c.Context.Err()
}

func TestInMemoryManager_CreateRetriesUntilStableBroker(t *testing.T) {
	t.Parallel()
	manager := newTestManager(t).(*inMemoryManager)
	taskID := a2a.NewTaskID()
	ctx := &destroyAndReplaceContext{Context: t.Context(), manager: manager, taskID: taskID}

	writer, err := manager.CreateWriter(ctx, taskID)
	if err != nil {
		t.Fatalf("manager.CreateWriter() error = %v, want nil", err)
	}
	if ctx.err != nil {
		t.Fatalf("broker replacement setup error = %v", ctx.err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close() error = %v, want nil", err)
	}
}

func TestInMemoryManager_CanceledCreationDoesNotRetainBroker(t *testing.T) {
	tests := []struct {
		name   string
		create func(Manager, context.Context, a2a.TaskID) (any, error)
	}{
		{
			name: "CreateReader",
			create: func(manager Manager, ctx context.Context, taskID a2a.TaskID) (any, error) {
				return manager.CreateReader(ctx, taskID)
			},
		},
		{
			name: "CreateWriter",
			create: func(manager Manager, ctx context.Context, taskID a2a.TaskID) (any, error) {
				return manager.CreateWriter(ctx, taskID)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			manager := NewInMemoryManager()
			taskID := a2a.NewTaskID()
			ctx, cancel := context.WithCancel(t.Context())
			cancel()

			queue, err := test.create(manager, ctx, taskID)
			if queue != nil || !errors.Is(err, context.Canceled) {
				t.Fatalf("manager.%s() = (%v, %v), want (nil, %v)", test.name, queue, err, context.Canceled)
			}

			inMemory := manager.(*inMemoryManager)
			inMemory.mu.Lock()
			_, exists := inMemory.brokers[taskID]
			inMemory.mu.Unlock()
			if exists {
				t.Fatalf("manager.%s() retained a broker after the canceled call", test.name)
			}
		})
	}
}

type cancelOnSecondErrContext struct {
	context.Context
	cancel context.CancelFunc
	calls  atomic.Int32
}

func (c *cancelOnSecondErrContext) Err() error {
	if c.calls.Add(1) == 2 {
		c.cancel()
	}
	return c.Context.Err()
}

func TestInMemoryManager_CancellationDuringCreationDoesNotRetainBroker(t *testing.T) {
	t.Parallel()
	manager := NewInMemoryManager()
	taskID := a2a.NewTaskID()
	base, cancel := context.WithCancel(t.Context())
	ctx := &cancelOnSecondErrContext{Context: base, cancel: cancel}

	reader, err := manager.CreateReader(ctx, taskID)
	if reader != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("manager.CreateReader() = (%v, %v), want (nil, %v)", reader, err, context.Canceled)
	}

	inMemory := manager.(*inMemoryManager)
	inMemory.mu.Lock()
	_, exists := inMemory.brokers[taskID]
	inMemory.mu.Unlock()
	if exists {
		t.Fatal("manager.CreateReader() retained a broker after cancellation during creation")
	}
}

type cancelOnDoneContext struct {
	context.Context
	cancel context.CancelFunc
	once   sync.Once
}

func (c *cancelOnDoneContext) Done() <-chan struct{} {
	c.once.Do(c.cancel)
	return c.Context.Done()
}

func TestInMemoryManager_CancellationAfterRegistrationDoesNotReturnQueue(t *testing.T) {
	t.Parallel()
	for i := range 100 {
		manager := NewInMemoryManager()
		taskID := a2a.TaskID(fmt.Sprintf("cancel-after-register-%d", i))
		base, cancel := context.WithCancel(t.Context())
		ctx := &cancelOnDoneContext{Context: base, cancel: cancel}

		reader, err := manager.CreateReader(ctx, taskID)
		if reader != nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("manager.CreateReader() at iteration %d = (%v, %v), want (nil, %v)", i, reader, err, context.Canceled)
		}

		inMemory := manager.(*inMemoryManager)
		inMemory.mu.Lock()
		_, exists := inMemory.brokers[taskID]
		inMemory.mu.Unlock()
		if exists {
			t.Fatalf("manager.CreateReader() at iteration %d retained a broker after cancellation", i)
		}
	}
}
