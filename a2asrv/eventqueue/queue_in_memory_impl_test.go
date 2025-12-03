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

type eventVersionPair struct {
	event   a2a.Event
	version a2a.TaskVersion
}

func newUnversioned(event a2a.Event) *eventVersionPair {
	return &eventVersionPair{event: event, version: a2a.TaskVersionMissing}
}

func mustCreate(t *testing.T, qm Manager, tid a2a.TaskID) Queue {
	t.Helper()
	q, err := qm.GetOrCreate(t.Context(), tid)
	if err != nil {
		t.Fatalf("qm.GetOrCreate() error = %v", err)
	}
	return q
}

func mustWrite(t *testing.T, q Queue, messages ...*eventVersionPair) {
	t.Helper()
	for i, msg := range messages {
		if err := q.WriteVersioned(t.Context(), msg.event, msg.version); err != nil {
			t.Fatalf("q.Write() error = %v at %d", err, i)
		}
	}
}

func mustRead(t *testing.T, q Queue) (a2a.Event, a2a.TaskVersion) {
	t.Helper()
	result, version, err := q.Read(t.Context())
	if err != nil {
		t.Fatalf("q.Read() error = %v", err)
	}
	return result, version
}

func newTestManager(t *testing.T, opts ...MemManagerOption) Manager {
	qm := NewInMemoryManager(opts...)
	t.Cleanup(func() {
		manager := qm.(*inMemoryManager)
		var ids []a2a.TaskID
		for tid := range manager.brokers {
			ids = append(ids, tid)
		}
		for _, tid := range ids {
			if err := qm.Destroy(t.Context(), tid); err != nil {
				t.Fatalf("qm.Destroy() error = %v", err)
			}
		}
	})
	return qm
}

func TestInMemoryQueue_WriteRead(t *testing.T) {
	t.Parallel()
	qm := newTestManager(t)

	tid := a2a.NewTaskID()
	writeQueue, readQueue := mustCreate(t, qm, tid), mustCreate(t, qm, tid)

	want := &eventVersionPair{event: &a2a.Message{ID: "test-event"}, version: a2a.TaskVersionInt(1)}
	mustWrite(t, writeQueue, want)
	got, gotVersion := mustRead(t, readQueue)
	if !reflect.DeepEqual(got, want.event) {
		t.Errorf("Read() got = %v, want %v", got, want)
	}
	if gotVersion != a2a.TaskVersionInt(1) {
		t.Errorf("Read() got version = %v, want %v", gotVersion, a2a.TaskVersionInt(1))
	}
}

func TestInMemoryQueue_DrainAfterDestroy(t *testing.T) {
	t.Parallel()
	qm := newTestManager(t)
	ctx, tid := t.Context(), a2a.NewTaskID()

	writeQueue, readQueue := mustCreate(t, qm, tid), mustCreate(t, qm, tid)
	want := []*eventVersionPair{
		{event: &a2a.Message{ID: "test-event"}, version: a2a.TaskVersionInt(1)},
		{event: &a2a.Message{ID: "test-event2"}, version: a2a.TaskVersionInt(2)},
	}

	mustWrite(t, writeQueue, want...)

	if err := qm.Destroy(ctx, tid); err != nil {
		t.Fatalf("qm.Destroy() error = %v", err)
	}

	var got []*eventVersionPair
	for {
		event, versions, err := readQueue.Read(ctx)
		if errors.Is(err, ErrQueueClosed) {
			break
		}
		if err != nil {
			t.Fatalf("Read() error = %v", err)
		}
		got = append(got, &eventVersionPair{event: event, version: versions})
	}
	if len(got) != len(want) {
		t.Fatalf("Read() got = %v, want %v", got, want)
	}
	for i, w := range want {
		if !reflect.DeepEqual(got[i].event, w.event) {
			t.Errorf("Read() got = %v, want %v", got, want)
		}
		if got[i].version != w.version {
			t.Errorf("Read() got version = %v, want %v", got[i].version, w.version)
		}
	}
}

func TestInMemoryQueue_ReadEmpty(t *testing.T) {
	t.Parallel()
	qm := newTestManager(t)
	tid := a2a.NewTaskID()

	writeQueue, readQueue := mustCreate(t, qm, tid), mustCreate(t, qm, tid)
	completed := make(chan struct{})

	go func() {
		mustRead(t, readQueue)
		close(completed)
	}()

	select {
	case <-completed:
		t.Fatal("method should be blocking")
	case <-time.After(15 * time.Millisecond):
		// unblock blocked code by writing to queue
		mustWrite(t, writeQueue, newUnversioned(&a2a.Message{ID: "test"}))
	}
	<-completed
}

func TestInMemoryQueue_WriteFull(t *testing.T) {
	t.Parallel()
	qm := newTestManager(t, WithQueueBufferSize(1))
	tid := a2a.NewTaskID()

	writeQueue, readQueue := mustCreate(t, qm, tid), mustCreate(t, qm, tid)
	completed := make(chan struct{})

	mustWrite(t, writeQueue, newUnversioned(&a2a.Message{ID: "1"}))
	go func() {
		mustWrite(t, writeQueue, newUnversioned(&a2a.Message{ID: "2"}))
		close(completed)
	}()

	select {
	case <-completed:
		t.Fatal("method should be blocking")
	case <-time.After(15 * time.Millisecond):
		// unblock blocked code by realising queue buffer
		mustRead(t, readQueue)
	}
	<-completed
}

func TestInMemoryQueue_WriteWithNoSubscribersDoesNotBlock(t *testing.T) {
	t.Parallel()
	qm := newTestManager(t, WithQueueBufferSize(0))
	tid := a2a.NewTaskID()

	writeQueue := mustCreate(t, qm, tid)
	mustWrite(t, writeQueue, newUnversioned(&a2a.Message{ID: "test"}))
}

func TestInMemoryQueue_CloseUnsubscribesFromEvents(t *testing.T) {
	t.Parallel()
	qm := newTestManager(t, WithQueueBufferSize(0))
	ctx, tid := t.Context(), a2a.NewTaskID()

	writeQueue, readQueue := mustCreate(t, qm, tid), mustCreate(t, qm, tid)

	if err := readQueue.Close(); err != nil {
		t.Fatalf("failed to close event queue: %v", err)
	}

	if err := writeQueue.Write(ctx, &a2a.Message{ID: "test"}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	event, version, err := readQueue.Read(ctx)
	if !errors.Is(err, ErrQueueClosed) {
		t.Fatalf("readQueue() = (%v, %v, %v), want %v", event, version, err, ErrQueueClosed)
	}
}

func TestInMemoryQueue_WriteWithCanceledContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())

	qm := newTestManager(t, WithQueueBufferSize(1))

	tid := a2a.NewTaskID()
	writeQueue, _ := mustCreate(t, qm, tid), mustCreate(t, qm, tid)

	// Fill the queue
	mustWrite(t, writeQueue, newUnversioned(&a2a.Message{ID: "1"}))
	cancel()

	err := writeQueue.Write(ctx, &a2a.Message{ID: "2"})
	if err == nil {
		t.Error("Write() with canceled context should have returned an error, but got nil")
	}
	if err != context.Canceled {
		t.Errorf("Write() error = %v, want %v", err, context.Canceled)
	}
}

func TestInMemoryQueue_BlockedWriteOnFullQueueThenDestroy(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	completed := make(chan struct{})

	qm := newTestManager(t, WithQueueBufferSize(1))

	tid := a2a.NewTaskID()
	writeQueue, _ := mustCreate(t, qm, tid), mustCreate(t, qm, tid)

	event := &a2a.Message{ID: "test"}

	// Fill the queue
	mustWrite(t, writeQueue, newUnversioned(&a2a.Message{ID: "1"}))

	go func() {
		err := writeQueue.Write(t.Context(), event)
		if !errors.Is(err, ErrQueueClosed) {
			t.Errorf("Write() error = %v, want %v", err, ErrQueueClosed)
			return
		}
		close(completed)
	}()

	select {
	case <-completed:
		t.Fatal("method should be blocking")
	case <-time.After(20 * time.Millisecond):
		// unblock blocked code by closing queue
		err := qm.Destroy(ctx, tid)
		if err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}
	<-completed
}
