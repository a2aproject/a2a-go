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
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"
	"github.com/google/go-cmp/cmp"
)

type eventVersionPair struct {
	event   a2a.Event
	version taskstore.TaskVersion
}

func newUnversioned(event a2a.Event) *eventVersionPair {
	return &eventVersionPair{event: event, version: taskstore.TaskVersionMissing}
}

func mustCreateReadWriter(t *testing.T, qm Manager, tid a2a.TaskID) (Reader, Writer) {
	t.Helper()
	r, err := qm.CreateReader(t.Context(), tid)
	if err != nil {
		t.Fatalf("qm.CreateReader() error = %v", err)
	}
	w, err := qm.CreateWriter(t.Context(), tid)
	if err != nil {
		t.Fatalf("qm.CreateWriter() error = %v", err)
	}
	return r, w
}

func mustWrite(t *testing.T, q Writer, messages ...*eventVersionPair) {
	t.Helper()
	for i, msg := range messages {
		if err := q.Write(t.Context(), &Message{Event: msg.event, TaskVersion: msg.version}); err != nil {
			t.Fatalf("q.Write() error = %v at %d", err, i)
		}
	}
}

func mustRead(t *testing.T, q Reader) (a2a.Event, taskstore.TaskVersion) {
	t.Helper()
	result, err := q.Read(t.Context())
	if err != nil {
		t.Fatalf("q.Read() error = %v", err)
	}
	return result.Event, result.TaskVersion
}

func newTestManager(t *testing.T, opts ...MemManagerOption) Manager {
	qm := NewInMemoryManager(opts...)
	t.Cleanup(func() {
		manager := qm.(*inMemoryManager)
		manager.mu.Lock()
		var ids []a2a.TaskID
		for tid := range manager.brokers {
			ids = append(ids, tid)
		}
		manager.mu.Unlock()
		for _, tid := range ids {
			if err := qm.Destroy(context.Background(), tid); err != nil {
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
	readQueue, writeQueue := mustCreateReadWriter(t, qm, tid)

	want := &eventVersionPair{event: &a2a.Message{ID: "test-event"}, version: taskstore.TaskVersion(1)}
	mustWrite(t, writeQueue, want)
	got, gotVersion := mustRead(t, readQueue)
	if !reflect.DeepEqual(got, want.event) {
		t.Errorf("Read() got = %v, want %v", got, want)
	}
	if gotVersion != taskstore.TaskVersion(1) {
		t.Errorf("Read() got version = %v, want %v", gotVersion, taskstore.TaskVersion(1))
	}
}

func TestInMemoryQueue_DrainAfterDestroy(t *testing.T) {
	t.Parallel()
	qm := newTestManager(t)
	ctx, tid := t.Context(), a2a.NewTaskID()

	readQueue, writeQueue := mustCreateReadWriter(t, qm, tid)
	want := []*eventVersionPair{
		{event: &a2a.Message{ID: "test-event"}, version: taskstore.TaskVersion(1)},
		{event: &a2a.Message{ID: "test-event2"}, version: taskstore.TaskVersion(2)},
	}

	mustWrite(t, writeQueue, want...)

	if err := qm.Destroy(ctx, tid); err != nil {
		t.Fatalf("qm.Destroy() error = %v", err)
	}

	var got []*eventVersionPair
	for {
		msg, err := readQueue.Read(ctx)
		if errors.Is(err, ErrQueueClosed) {
			break
		}
		if err != nil {
			t.Fatalf("Read() error = %v", err)
		}
		got = append(got, &eventVersionPair{event: msg.Event, version: msg.TaskVersion})
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

	readQueue, writeQueue := mustCreateReadWriter(t, qm, tid)
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

	readQueue, writeQueue := mustCreateReadWriter(t, qm, tid)
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

	writer, err := qm.CreateWriter(t.Context(), tid)
	if err != nil {
		t.Fatalf("qm.CreateWriter() error = %v", err)
	}
	mustWrite(t, writer, newUnversioned(&a2a.Message{ID: "test"}))
}

func TestInMemoryQueue_CloseUnsubscribesFromEvents(t *testing.T) {
	t.Parallel()
	qm := newTestManager(t, WithQueueBufferSize(0))
	ctx, tid := t.Context(), a2a.NewTaskID()

	readQueue, writeQueue := mustCreateReadWriter(t, qm, tid)

	if err := readQueue.Close(); err != nil {
		t.Fatalf("failed to close event queue: %v", err)
	}

	if err := writeQueue.Write(ctx, &Message{Event: &a2a.Message{ID: "test"}}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	msg, err := readQueue.Read(ctx)
	if !errors.Is(err, ErrQueueClosed) {
		t.Fatalf("readQueue() = (%v, %v), want %v", msg, err, ErrQueueClosed)
	}
}

func TestInMemoryQueue_WriteWithCanceledContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())

	qm := newTestManager(t, WithQueueBufferSize(1))

	tid := a2a.NewTaskID()
	_, writeQueue := mustCreateReadWriter(t, qm, tid)

	// Fill the queue
	mustWrite(t, writeQueue, newUnversioned(&a2a.Message{ID: "1"}))
	cancel()

	err := writeQueue.Write(ctx, &Message{Event: &a2a.Message{ID: "2"}})
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
	_, writeQueue := mustCreateReadWriter(t, qm, tid)

	event := &a2a.Message{ID: "test"}

	// Fill the queue
	mustWrite(t, writeQueue, newUnversioned(&a2a.Message{ID: "1"}))

	go func() {
		err := writeQueue.Write(t.Context(), &Message{Event: event})
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

func TestInMemoryQueue_StalledSubscriberDoesNotBlockNewSubscriber(t *testing.T) {
	t.Parallel()
	qm := newTestManager(t, WithQueueBufferSize(1), WithSubscriberTimeout(0))
	tid := a2a.NewTaskID()
	stalled, writer := mustCreateReadWriter(t, qm, tid)
	if err := writer.Write(t.Context(), &Message{Event: &a2a.Message{ID: "buffered"}}); err != nil {
		t.Fatalf("initial Write() error = %v, want nil", err)
	}

	writeDone := make(chan error, 1)
	go func() {
		writeDone <- writer.Write(t.Context(), &Message{Event: &a2a.Message{ID: "stalled"}})
	}()
	select {
	case err := <-writeDone:
		t.Fatalf("Write() returned %v, want it to remain blocked on the stalled subscriber", err)
	case <-time.After(20 * time.Millisecond):
	}

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	createDone := make(chan struct {
		reader Reader
		err    error
	}, 1)
	go func() {
		healthy, err := qm.CreateReader(ctx, tid)
		createDone <- struct {
			reader Reader
			err    error
		}{healthy, err}
	}()
	var healthy Reader
	select {
	case result := <-createDone:
		if result.err != nil {
			t.Fatalf("CreateReader() error = %v, want a reader while another subscriber is stalled", result.err)
		}
		healthy = result.reader
	case <-time.After(time.Second):
		t.Fatal("CreateReader() did not return while another subscriber was stalled")
	}
	healthyWriter, err := qm.CreateWriter(ctx, tid)
	if err != nil {
		t.Fatalf("CreateWriter() error = %v, want a writer while another subscriber is stalled", err)
	}

	healthyWriteDone := make(chan error, 1)
	go func() {
		healthyWriteDone <- healthyWriter.Write(ctx, &Message{Event: &a2a.Message{ID: "healthy"}})
	}()
	msg, err := healthy.Read(ctx)
	if err != nil {
		t.Fatalf("healthy.Read() error = %v, want the healthy subscriber to receive its event", err)
	}
	if msg.Event.(*a2a.Message).ID != "healthy" {
		t.Fatalf("healthy.Read() event ID = %q, want %q", msg.Event.(*a2a.Message).ID, "healthy")
	}
	select {
	case err := <-healthyWriteDone:
		t.Fatalf("healthy Write() returned %v, want producer backpressure from the stalled subscriber", err)
	case <-time.After(20 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-healthyWriteDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("healthy Write() error = %v, want %v after cancellation", err, context.Canceled)
		}
	case <-time.After(time.Second):
		t.Fatal("healthy Write() did not unblock after cancellation")
	}

	if err := stalled.Close(); err != nil {
		t.Fatalf("stalled.Close() error = %v", err)
	}
	select {
	case err := <-writeDone:
		if err != nil {
			t.Fatalf("stalled Write() error = %v, want nil after stalled subscriber Close()", err)
		}
	case <-time.After(time.Second):
		t.Fatal("stalled Write() did not unblock after stalled subscriber Close()")
	}
}

func TestInMemoryQueue_StalledSubscriberDoesNotBlockClose(t *testing.T) {
	t.Parallel()
	qm := newTestManager(t, WithQueueBufferSize(0), WithSubscriberTimeout(0))
	tid := a2a.NewTaskID()
	stalled, writer := mustCreateReadWriter(t, qm, tid)

	writeDone := make(chan error, 1)
	go func() {
		writeDone <- writer.Write(t.Context(), &Message{Event: &a2a.Message{ID: "stalled"}})
	}()
	select {
	case err := <-writeDone:
		t.Fatalf("Write() returned %v, want it to remain blocked on the stalled subscriber", err)
	case <-time.After(20 * time.Millisecond):
	}

	closeDone := make(chan error, 1)
	go func() {
		closeDone <- stalled.Close()
	}()
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("stalled.Close() error = %v, want nil", err)
		}
	case <-time.After(time.Second):
		t.Fatal("stalled.Close() did not return while the broker had a pending delivery")
	}
	select {
	case err := <-writeDone:
		if err != nil {
			t.Fatalf("Write() error = %v, want nil after the only subscriber closed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Write() did not unblock after subscriber Close()")
	}
}

func TestInMemoryQueue_StalledSubscriberDoesNotBlockUnrelatedTask(t *testing.T) {
	t.Parallel()
	qm := newTestManager(t, WithQueueBufferSize(0), WithSubscriberTimeout(0))
	stalledTask := a2a.NewTaskID()
	_, writer := mustCreateReadWriter(t, qm, stalledTask)

	writeDone := make(chan error, 1)
	go func() {
		writeDone <- writer.Write(t.Context(), &Message{Event: &a2a.Message{ID: "stalled"}})
	}()
	select {
	case err := <-writeDone:
		t.Fatalf("Write() returned %v, want it to remain blocked on the stalled subscriber", err)
	case <-time.After(20 * time.Millisecond):
	}

	connectCtx, connectCancel := context.WithTimeout(t.Context(), time.Second)
	defer connectCancel()
	connectDone := make(chan error, 1)
	go func() {
		_, err := qm.CreateReader(connectCtx, stalledTask)
		connectDone <- err
	}()

	otherTask := a2a.NewTaskID()
	otherCtx, otherCancel := context.WithTimeout(t.Context(), time.Second)
	defer otherCancel()
	otherDone := make(chan error, 1)
	go func() {
		_, err := qm.CreateReader(otherCtx, otherTask)
		otherDone <- err
	}()
	select {
	case err := <-connectDone:
		if err != nil {
			t.Fatalf("CreateReader() error = %v, want nil", err)
		}
	case <-time.After(time.Second):
		t.Fatal("CreateReader() for the stalled task did not return")
	}
	select {
	case err := <-otherDone:
		if err != nil {
			t.Fatalf("CreateReader() for an unrelated task error = %v, want nil", err)
		}
	case <-time.After(time.Second):
		t.Fatal("CreateReader() for an unrelated task did not return while another task was stalled")
	}
	if err := qm.Destroy(otherCtx, otherTask); err != nil {
		t.Fatalf("qm.Destroy() for an unrelated task error = %v, want nil", err)
	}
	if err := qm.Destroy(t.Context(), stalledTask); err != nil {
		t.Fatalf("qm.Destroy() error = %v", err)
	}
	select {
	case <-writeDone:
	case <-time.After(time.Second):
		t.Fatal("stalled Write() did not unblock after Destroy()")
	}
}

func TestInMemoryQueue_StalledSubscriberIsDroppedAfterGracePeriod(t *testing.T) {
	t.Parallel()
	qm := newTestManager(t, WithQueueBufferSize(1), WithSubscriberTimeout(50*time.Millisecond))
	tid := a2a.NewTaskID()
	stalled, writer := mustCreateReadWriter(t, qm, tid)

	if err := writer.Write(t.Context(), &Message{Event: &a2a.Message{ID: "buffered"}}); err != nil {
		t.Fatalf("initial Write() error = %v, want nil", err)
	}

	writeDone := make(chan error, 1)
	go func() {
		writeDone <- writer.Write(t.Context(), &Message{Event: &a2a.Message{ID: "full"}})
	}()
	select {
	case err := <-writeDone:
		t.Fatalf("second Write() returned %v before the subscriber grace period elapsed", err)
	case <-time.After(20 * time.Millisecond):
	}
	select {
	case err := <-writeDone:
		if err != nil {
			t.Fatalf("second Write() error = %v, want nil after dropping the stalled subscriber", err)
		}
	case <-time.After(time.Second):
		t.Fatal("second Write() remained blocked beyond the subscriber grace period")
	}

	if _, err := stalled.Read(t.Context()); err != nil {
		t.Fatalf("stalled.Read() for buffered event error = %v, want nil", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if _, err := stalled.Read(ctx); !errors.Is(err, ErrQueueClosed) {
		t.Fatalf("stalled.Read() after drop error = %v, want %v", err, ErrQueueClosed)
	}

	replacement, err := qm.CreateReader(ctx, tid)
	if err != nil {
		t.Fatalf("CreateReader() after subscriber drop error = %v, want nil", err)
	}
	want := &Message{Event: &a2a.Message{ID: "after-resubscribe"}}
	if err := writer.Write(ctx, want); err != nil {
		t.Fatalf("Write() after resubscribe error = %v, want nil", err)
	}
	got, err := replacement.Read(ctx)
	if err != nil {
		t.Fatalf("replacement.Read() error = %v, want nil", err)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("replacement.Read() wrong result (-want +got) diff = %s", diff)
	}
}

func TestInMemoryQueue_PreservesMessageOrderAcrossSubscribers(t *testing.T) {
	t.Parallel()
	qm := newTestManager(t, WithQueueBufferSize(3), WithSubscriberTimeout(0))
	tid := a2a.NewTaskID()
	first, writer := mustCreateReadWriter(t, qm, tid)
	second, err := qm.CreateReader(t.Context(), tid)
	if err != nil {
		t.Fatalf("CreateReader() error = %v", err)
	}

	for i := range 3 {
		if err := writer.Write(t.Context(), &Message{Event: &a2a.Message{ID: fmt.Sprintf("event-%d", i)}}); err != nil {
			t.Fatalf("Write(%d) error = %v", i, err)
		}
	}
	for _, reader := range []Reader{first, second} {
		for i := range 3 {
			message, err := reader.Read(t.Context())
			if err != nil {
				t.Fatalf("Read(%d) error = %v", i, err)
			}
			want := fmt.Sprintf("event-%d", i)
			if got := message.Event.(*a2a.Message).ID; got != want {
				t.Fatalf("Read(%d) ID = %q, want %q", i, got, want)
			}
		}
	}
}

func TestInMemoryQueue_PreservesConcurrentBroadcastOrderAcrossSubscribers(t *testing.T) {
	t.Parallel()
	const broadcastCount = 32

	qm := newTestManager(t, WithQueueBufferSize(broadcastCount), WithSubscriberTimeout(0))
	tid := a2a.NewTaskID()
	first, firstWriter := mustCreateReadWriter(t, qm, tid)
	second, err := qm.CreateReader(t.Context(), tid)
	if err != nil {
		t.Fatalf("CreateReader() error = %v", err)
	}
	secondWriter, err := qm.CreateWriter(t.Context(), tid)
	if err != nil {
		t.Fatalf("CreateWriter() error = %v", err)
	}

	start := make(chan struct{})
	errs := make(chan error, broadcastCount)
	var writes sync.WaitGroup
	for i := range broadcastCount {
		writes.Go(func() {
			<-start
			writer := firstWriter
			if i%2 != 0 {
				writer = secondWriter
			}
			errs <- writer.Write(t.Context(), &Message{Event: &a2a.Message{ID: fmt.Sprintf("event-%d", i)}})
		})
	}
	close(start)
	writes.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("Write() error = %v, want nil", err)
		}
	}

	readIDs := func(reader Reader) []string {
		t.Helper()
		ids := make([]string, 0, broadcastCount)
		for range broadcastCount {
			message, err := reader.Read(t.Context())
			if err != nil {
				t.Fatalf("Read() error = %v, want nil", err)
			}
			ids = append(ids, message.Event.(*a2a.Message).ID)
		}
		return ids
	}
	firstIDs := readIDs(first)
	secondIDs := readIDs(second)
	if diff := cmp.Diff(firstIDs, secondIDs); diff != "" {
		t.Fatalf("subscriber broadcast order mismatch (-first +second) diff = %s", diff)
	}
}
