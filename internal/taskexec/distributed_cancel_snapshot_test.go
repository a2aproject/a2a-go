// Copyright 2026 The A2A Authors
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

package taskexec_test

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/a2asrv/eventqueue"
	"github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"
	"github.com/a2aproject/a2a-go/v2/a2asrv/workqueue"
	"github.com/a2aproject/a2a-go/v2/internal/testutil"
	"github.com/a2aproject/a2a-go/v2/internal/testutil/testexecutor"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

type cancelSnapshotContextKey struct{}

type cancelSnapshotResult struct {
	task *a2a.Task
	err  error
}

type cancelSnapshotRuntime struct {
	handler        a2asrv.RequestHandler
	store          *testutil.TestTaskStore
	seed           *a2a.Task
	snapshotRead   <-chan struct{}
	workerEntered  <-chan struct{}
	workerFinished <-chan struct{}
	release        func()
	writes         *atomic.Int32
}

// newCancelSnapshotRuntime 复用SDK实际Handler、内存存储和执行器，只用channel固定取消worker的先后顺序。
func newCancelSnapshotRuntime(t *testing.T, initial, final a2a.TaskState, fullTask, fast, replayOld bool) *cancelSnapshotRuntime {
	t.Helper()
	seed := &a2a.Task{ID: a2a.NewTaskID(), ContextID: a2a.NewContextID(), Status: a2a.TaskStatus{State: initial}, History: []*a2a.Message{a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("retained message"))}}
	seed.Artifacts = []*a2a.Artifact{a2a.NewArtifactEvent(seed, a2a.NewTextPart("retained artifact")).Artifact}
	store := testutil.NewTestTaskStore().WithTasks(t, seed)
	snapshotRead, entered, finished, release := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	var reads, writes atomic.Int32
	store.GetFunc = func(ctx context.Context, id a2a.TaskID) (*taskstore.StoredTask, error) {
		value, err := store.InMemory.Get(ctx, id)
		// Cancel首次读取用于准入，第二次读取初始快照；worker不继承测试私有标记。
		if ctx.Value(cancelSnapshotContextKey{}) == true && reads.Add(1) == 2 {
			close(snapshotRead)
		}
		return value, err
	}
	agent := testexecutor.NewCanceler()
	agent.CancelFn = func(_ context.Context, exec *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
		return func(yield func(a2a.Event, error) bool) {
			if fullTask {
				task := *exec.StoredTask
				task.Status.State = final
				yield(&task, nil)
				return
			}
			yield(a2a.NewStatusUpdateEvent(exec, final, nil), nil)
		}
	}
	base := workqueue.NewInMemory(nil)
	queue := testutil.NewTestWorkQueue()
	var worker workqueue.HandlerFn
	run := func(ctx context.Context, p *workqueue.Payload) (a2a.SendMessageResult, error) {
		close(entered)
		if !fast {
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		defer close(finished)
		return worker(ctx, p)
	}
	queue.RegisterHandlerFunc = func(cfg workqueue.HandlerConfig, fn workqueue.HandlerFn) { worker = fn; base.RegisterHandler(cfg, run) }
	queue.WriteFunc = func(ctx context.Context, p *workqueue.Payload) (a2a.TaskID, error) {
		writes.Add(1)
		if fast {
			// 固定快worker在Write回执前完成；上下文与SDK内存队列一样从后台开始。
			if _, err := run(context.Background(), p); err != nil {
				t.Logf("worker.Handle() error = %v", err)
			}
			return p.TaskID, nil
		}
		return base.Write(ctx, p)
	}
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	qm := testutil.NewTestQueueManager()
	if replayOld {
		stored, err := store.InMemory.Get(t.Context(), seed.ID)
		if err != nil {
			t.Fatalf("store.Get() error = %v, want nil", err)
		}
		qm.CreateReaderFunc = func(ctx context.Context, id a2a.TaskID) (eventqueue.Reader, error) {
			reader, err := qm.Manager.CreateReader(ctx, id)
			if err != nil {
				return nil, err
			}
			observed := false
			replay := testutil.NewTestEventQueue()
			replay.CloseFunc = reader.Close
			replay.ReadFunc = func(ctx context.Context) (*eventqueue.Message, error) {
				if !observed {
					observed = true
					// reader在取消排队前建立；重放的旧版本不能当成本次worker的拒绝结果。
					var event a2a.Event = seed
					if !fullTask {
						event = a2a.NewStatusUpdateEvent(seed, initial, nil)
					}
					return &eventqueue.Message{Event: event, TaskVersion: stored.Version}, nil
				}
				return reader.Read(ctx)
			}
			return replay, nil
		}
	}
	handler := a2asrv.NewHandler(agent, a2asrv.WithClusterMode(a2asrv.ClusterConfig{TaskStore: store, QueueManager: qm, WorkQueue: queue}))
	t.Cleanup(func() {
		unblock()
		if writes.Load() == 0 {
			return
		}
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Error("worker.Handle() did not finish")
		}
	})
	return &cancelSnapshotRuntime{handler: handler, store: store, seed: seed, snapshotRead: snapshotRead, workerEntered: entered, workerFinished: finished, release: unblock, writes: &writes}
}

// waitCancelSnapshotSignal 用实际阶段信号同步测试，不用sleep推测取消或持久化时序。
func waitCancelSnapshotSignal(t *testing.T, ctx context.Context, ch <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-ch:
	case <-ctx.Done():
		t.Fatalf("%s error = %v, want completion", name, ctx.Err())
	}
}

// TestDistributedCancelSnapshotWaitsForWorker 验证初始非终态快照不能冒充本次取消结果。
func TestDistributedCancelSnapshotWaitsForWorker(t *testing.T) {
	t.Parallel()
	for _, state := range []a2a.TaskState{a2a.TaskStateInputRequired, a2a.TaskStateWorking, a2a.TaskStateAuthRequired} {
		t.Run(string(state), func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			runtime := newCancelSnapshotRuntime(t, state, a2a.TaskStateCanceled, false, false, false)
			before, err := runtime.handler.GetTask(ctx, &a2a.GetTaskRequest{ID: runtime.seed.ID})
			if err != nil {
				t.Fatalf("handler.GetTask() error = %v, want nil", err)
			}
			if diff := cmp.Diff(runtime.seed, before); diff != "" {
				t.Fatalf("handler.GetTask() wrong result (-want +got) diff = %s", diff)
			}
			done := make(chan cancelSnapshotResult, 1)
			go func() {
				task, err := runtime.handler.CancelTask(context.WithValue(ctx, cancelSnapshotContextKey{}, true), &a2a.CancelTaskRequest{ID: runtime.seed.ID})
				done <- cancelSnapshotResult{task, err}
			}()
			waitCancelSnapshotSignal(t, ctx, runtime.workerEntered, "cancel worker entry")
			waitCancelSnapshotSignal(t, ctx, runtime.snapshotRead, "cancel snapshot read")
			// 取消快照已经读取，但实际worker尚未处理；Get必须仍返回完整原任务。
			pending, err := runtime.handler.GetTask(ctx, &a2a.GetTaskRequest{ID: runtime.seed.ID})
			if err != nil {
				t.Fatalf("handler.GetTask() error = %v, want nil", err)
			}
			if diff := cmp.Diff(runtime.seed, pending); diff != "" {
				t.Fatalf("handler.GetTask() wrong result (-want +got) diff = %s", diff)
			}
			runtime.release()
			var got cancelSnapshotResult
			select {
			case got = <-done:
			case <-ctx.Done():
				t.Fatalf("handler.CancelTask() error = %v, want completion", ctx.Err())
			}
			waitCancelSnapshotSignal(t, ctx, runtime.workerFinished, "cancel worker completion")
			stored, err := runtime.store.Get(ctx, runtime.seed.ID)
			if err != nil {
				t.Fatalf("store.Get() error = %v, want nil", err)
			}
			if got.err != nil {
				t.Fatalf("handler.CancelTask() error = %v, want nil (persisted state %s)", got.err, stored.Task.Status.State)
			}
			if stored.Task.Status.State != a2a.TaskStateCanceled {
				t.Fatalf("store.Get() state = %v, want %v", stored.Task.Status.State, a2a.TaskStateCanceled)
			}
			if diff := cmp.Diff(stored.Task, got.task); diff != "" {
				t.Fatalf("handler.CancelTask() wrong result (-want +got) diff = %s", diff)
			}
			if diff := cmp.Diff(runtime.seed.History, got.task.History); diff != "" {
				t.Fatalf("handler.CancelTask() history wrong result (-want +got) diff = %s", diff)
			}
			if diff := cmp.Diff(runtime.seed.Artifacts, got.task.Artifacts); diff != "" {
				t.Fatalf("handler.CancelTask() artifacts wrong result (-want +got) diff = %s", diff)
			}
			if got := runtime.writes.Load(); got != 1 {
				t.Fatalf("queue.Write() count = %v, want 1", got)
			}
		})
	}
}

// TestDistributedCancelSnapshotPreservesRefusal 区分初始快照与真正worker的Task或Status拒绝事件。
func TestDistributedCancelSnapshotPreservesRefusal(t *testing.T) {
	t.Parallel()
	for _, fullTask := range []bool{true, false} {
		for _, fast := range []bool{true, false} {
			t.Run(fmt.Sprintf("task=%v/fast=%v", fullTask, fast), func(t *testing.T) {
				t.Parallel()
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				runtime := newCancelSnapshotRuntime(t, a2a.TaskStateInputRequired, a2a.TaskStateInputRequired, fullTask, fast, false)
				done := make(chan cancelSnapshotResult, 1)
				go func() {
					task, err := runtime.handler.CancelTask(context.WithValue(ctx, cancelSnapshotContextKey{}, true), &a2a.CancelTaskRequest{ID: runtime.seed.ID})
					done <- cancelSnapshotResult{task, err}
				}()
				waitCancelSnapshotSignal(t, ctx, runtime.workerEntered, "cancel worker entry")
				if !fast {
					waitCancelSnapshotSignal(t, ctx, runtime.snapshotRead, "cancel snapshot read")
					runtime.release()
				}
				var got cancelSnapshotResult
				select {
				case got = <-done:
				case <-ctx.Done():
					t.Fatalf("handler.CancelTask() error = %v, want worker refusal", ctx.Err())
				}
				waitCancelSnapshotSignal(t, ctx, runtime.workerFinished, "cancel worker completion")
				if ctx.Err() != nil {
					t.Fatalf("handler.CancelTask() error = %v, want refusal before request deadline", ctx.Err())
				}
				if !errors.Is(got.err, a2a.ErrTaskNotCancelable) {
					t.Fatalf("handler.CancelTask() error = %v, want %v", got.err, a2a.ErrTaskNotCancelable)
				}
				if got.task != nil {
					t.Fatalf("handler.CancelTask() = %v, want nil", got.task)
				}
				stored, err := runtime.store.Get(ctx, runtime.seed.ID)
				if err != nil {
					t.Fatalf("store.Get() error = %v, want nil", err)
				}
				if diff := cmp.Diff(runtime.seed, stored.Task, cmpopts.IgnoreFields(a2a.TaskStatus{}, "Timestamp")); diff != "" {
					t.Fatalf("store.Get() wrong result (-want +got) diff = %s", diff)
				}
			})
		}
	}
}

// TestDistributedCancelSnapshotIgnoresOlderEvents 验证旧Task和Status事件不会在真正取消事件之前误终止请求。
func TestDistributedCancelSnapshotIgnoresOlderEvents(t *testing.T) {
	t.Parallel()
	for _, fullTask := range []bool{true, false} {
		t.Run(fmt.Sprintf("task=%v", fullTask), func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			runtime := newCancelSnapshotRuntime(t, a2a.TaskStateInputRequired, a2a.TaskStateCanceled, fullTask, false, true)
			done := make(chan cancelSnapshotResult, 1)
			go func() {
				task, err := runtime.handler.CancelTask(context.WithValue(ctx, cancelSnapshotContextKey{}, true), &a2a.CancelTaskRequest{ID: runtime.seed.ID})
				done <- cancelSnapshotResult{task, err}
			}()
			waitCancelSnapshotSignal(t, ctx, runtime.workerEntered, "cancel worker entry")
			waitCancelSnapshotSignal(t, ctx, runtime.snapshotRead, "cancel snapshot read")
			runtime.release()
			var got cancelSnapshotResult
			select {
			case got = <-done:
			case <-ctx.Done():
				t.Fatalf("handler.CancelTask() error = %v, want completion", ctx.Err())
			}
			waitCancelSnapshotSignal(t, ctx, runtime.workerFinished, "cancel worker completion")
			if got.err != nil {
				t.Fatalf("handler.CancelTask() error = %v, want nil", got.err)
			}
			stored, err := runtime.store.Get(ctx, runtime.seed.ID)
			if err != nil {
				t.Fatalf("store.Get() error = %v, want nil", err)
			}
			if stored.Task.Status.State != a2a.TaskStateCanceled {
				t.Fatalf("store.Get() state = %v, want %v", stored.Task.Status.State, a2a.TaskStateCanceled)
			}
			if diff := cmp.Diff(stored.Task, got.task); diff != "" {
				t.Fatalf("handler.CancelTask() wrong result (-want +got) diff = %s", diff)
			}
		})
	}
}

// TestDistributedCancelSnapshotDeadline 验证取消请求会等待真实结果，并在预算耗尽时保持失败而非伪造成功。
func TestDistributedCancelSnapshotDeadline(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	runtime := newCancelSnapshotRuntime(t, a2a.TaskStateInputRequired, a2a.TaskStateCanceled, false, false, false)
	done := make(chan cancelSnapshotResult, 1)
	go func() {
		task, err := runtime.handler.CancelTask(context.WithValue(ctx, cancelSnapshotContextKey{}, true), &a2a.CancelTaskRequest{ID: runtime.seed.ID})
		done <- cancelSnapshotResult{task, err}
	}()
	waitCancelSnapshotSignal(t, ctx, runtime.workerEntered, "cancel worker entry")
	waitCancelSnapshotSignal(t, ctx, runtime.snapshotRead, "cancel snapshot read")
	var got cancelSnapshotResult
	select {
	case got = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("handler.CancelTask() did not honor request deadline")
	}
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("handler.CancelTask() context error = %v, want %v", ctx.Err(), context.DeadlineExceeded)
	}
	if got.err == nil || got.task != nil {
		t.Fatalf("handler.CancelTask() = (%v,%v), want failure", got.task, got.err)
	}
	stored, err := runtime.store.Get(t.Context(), runtime.seed.ID)
	if err != nil {
		t.Fatalf("store.Get() error = %v, want nil", err)
	}
	if diff := cmp.Diff(runtime.seed, stored.Task); diff != "" {
		t.Fatalf("store.Get() wrong result (-want +got) diff = %s", diff)
	}
	runtime.release()
}
