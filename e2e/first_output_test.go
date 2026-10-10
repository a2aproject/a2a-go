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

package e2e_test

import (
	"context"
	"errors"
	"iter"
	"testing"
	"testing/synctest"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/a2asrv/eventqueue"
	"github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"
	"github.com/a2aproject/a2a-go/v2/a2asrv/workqueue"
	"github.com/a2aproject/a2a-go/v2/internal/testutil"
	"github.com/a2aproject/a2a-go/v2/internal/testutil/testexecutor"
)

func matchesArtifact(event a2a.Event) bool {
	_, ok := event.(*a2a.TaskArtifactUpdateEvent)
	return ok
}

func TestAgentFirstOutputTimeout(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"blocking", "streaming", "cluster worker"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			tests := []struct {
				name     string
				timeout  time.Duration
				output   bool
				anyEvent bool
				noTask   bool
				finish   time.Duration
				want     a2a.TaskState
				wantErr  error
			}{
				{name: "working updates do not satisfy output", timeout: time.Second, finish: 2 * time.Second, want: a2a.TaskStateFailed, wantErr: a2asrv.ErrAgentFirstOutputTimeout},
				{name: "matching output disarms timeout", timeout: time.Second, output: true, finish: 2 * time.Second, want: a2a.TaskStateCompleted},
				{name: "nil matcher accepts submitted task", timeout: time.Second, anyEvent: true, finish: 2 * time.Second, want: a2a.TaskStateCompleted},
				{name: "zero disables timeout", finish: 2 * time.Second, want: a2a.TaskStateCompleted},
				{name: "negative disables timeout", timeout: -time.Second, finish: 2 * time.Second, want: a2a.TaskStateCompleted},
				{name: "completion without matching output", timeout: time.Second, finish: 100 * time.Millisecond, want: a2a.TaskStateCompleted},
				{name: "timeout before task creation", timeout: time.Second, noTask: true, wantErr: a2asrv.ErrAgentFirstOutputTimeout},
			}
			for _, tc := range tests {
				t.Run(tc.name, func(t *testing.T) {
					t.Parallel()
					synctest.Test(t, func(t *testing.T) {
						causes := make(chan error, 1)
						cleaned := make(chan struct{})
						executor := &testexecutor.TestAgentExecutor{
							CleanupFn: func(context.Context, *a2asrv.ExecutorContext, a2a.SendMessageResult, error) { close(cleaned) },
							ExecuteFn: func(ctx context.Context, ec *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
								return func(yield func(a2a.Event, error) bool) {
									defer func() { causes <- context.Cause(ctx) }()
									if tc.noTask {
										<-ctx.Done()
										yield(nil, context.Cause(ctx))
										return
									}
									if !yield(a2a.NewSubmittedTask(ec, ec.Message), nil) {
										return
									}
									if tc.output && !yield(a2a.NewArtifactEvent(ec, a2a.NewTextPart("hello")), nil) {
										return
									}
									ticker := time.NewTicker(100 * time.Millisecond)
									defer ticker.Stop()
									finish := time.NewTimer(tc.finish)
									defer finish.Stop()
									for {
										select {
										case <-ctx.Done():
											yield(nil, context.Cause(ctx))
											return
										case <-finish.C:
											yield(a2a.NewStatusUpdateEvent(ec, a2a.TaskStateCompleted, nil), nil)
											return
										case <-ticker.C:
											if !yield(a2a.NewStatusUpdateEvent(ec, a2a.TaskStateWorking, nil), nil) {
												return
											}
										}
									}
								}
							},
						}
						matcher := matchesArtifact
						if tc.anyEvent {
							matcher = nil
						}
						store := taskstore.NewInMemory(nil)
						options := []a2asrv.RequestHandlerOption{
							a2asrv.WithTaskStore(store),
							a2asrv.WithAgentFirstOutputTimeout(tc.timeout, matcher),
						}
						queue := testutil.NewTestWorkQueue()
						queueManager := eventqueue.NewInMemoryManager()
						if mode == "cluster worker" {
							options = append(options, a2asrv.WithClusterMode(a2asrv.ClusterConfig{
								QueueManager: queueManager, WorkQueue: queue, TaskStore: store,
							}))
						}
						handler := a2asrv.NewHandler(executor, options...)
						req := &a2a.SendMessageRequest{Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hello"))}
						var result a2a.SendMessageResult
						var err error
						switch mode {
						case "cluster worker":
							tid := a2a.NewTaskID()
							result, err = queue.HandlerFn(t.Context(), &workqueue.Payload{Type: workqueue.PayloadTypeExecute, TaskID: tid, ExecuteRequest: req})
							if destroyErr := queueManager.Destroy(t.Context(), tid); destroyErr != nil {
								t.Fatalf("queueManager.Destroy() error = %v, want nil", destroyErr)
							}
						case "streaming":
							for event, streamErr := range handler.SendStreamingMessage(t.Context(), req) {
								if streamErr != nil {
									err = streamErr
									break
								}
								if task, ok := event.(*a2a.Task); ok {
									result = task
								}
								if status, ok := event.(*a2a.TaskStatusUpdateEvent); ok && status.Status.State.Terminal() {
									result, err = handler.GetTask(t.Context(), &a2a.GetTaskRequest{ID: status.TaskID})
								}
							}
						default:
							result, err = handler.SendMessage(t.Context(), req)
						}
						<-cleaned
						if tc.noTask {
							if !errors.Is(err, tc.wantErr) {
								t.Fatalf("execution error = %v, want %v", err, tc.wantErr)
							}
						} else {
							if err != nil {
								t.Fatalf("execution error = %v, want nil", err)
							}
							task, ok := result.(*a2a.Task)
							if !ok || task.Status.State != tc.want {
								t.Fatalf("execution result = %v, want task state %v", result, tc.want)
							}
							stored, err := store.Get(t.Context(), task.ID)
							if err != nil || stored.Task.Status.State != tc.want {
								t.Fatalf("store.Get() = %v, %v, want task state %v", stored, err, tc.want)
							}
						}
						if cause := <-causes; tc.wantErr != nil && !errors.Is(cause, tc.wantErr) {
							t.Fatalf("context.Cause() = %v, want %v", cause, tc.wantErr)
						}
					})
				})
			}
		})
	}
}

func TestAgentFirstOutputTimeout_CancellationExecution(t *testing.T) {
	t.Parallel()
	for _, cluster := range []bool{false, true} {
		synctest.Test(t, func(t *testing.T) {
			store := taskstore.NewInMemory(nil)
			seed := &a2a.Task{ID: a2a.NewTaskID(), ContextID: a2a.NewContextID()}
			task := a2a.NewSubmittedTask(seed, a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("work")))
			if _, err := store.Create(t.Context(), task); err != nil {
				t.Fatalf("store.Create() error = %v, want nil", err)
			}
			executor := &testexecutor.TestAgentExecutor{
				CancelFn: func(ctx context.Context, ec *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
					return func(yield func(a2a.Event, error) bool) {
						time.Sleep(2 * time.Second)
						if ctx.Err() != nil {
							yield(nil, context.Cause(ctx))
							return
						}
						yield(a2a.NewStatusUpdateEvent(ec, a2a.TaskStateCanceled, nil), nil)
					}
				},
			}
			options := []a2asrv.RequestHandlerOption{a2asrv.WithTaskStore(store), a2asrv.WithAgentFirstOutputTimeout(time.Second, matchesArtifact)}
			queue := testutil.NewTestWorkQueue()
			qm := eventqueue.NewInMemoryManager()
			if cluster {
				options = append(options, a2asrv.WithClusterMode(a2asrv.ClusterConfig{TaskStore: store, QueueManager: qm, WorkQueue: queue}))
			}
			handler := a2asrv.NewHandler(executor, options...)
			req := &a2a.CancelTaskRequest{ID: task.ID}
			var result a2a.SendMessageResult
			var err error
			if cluster {
				result, err = queue.HandlerFn(t.Context(), &workqueue.Payload{Type: workqueue.PayloadTypeCancel, TaskID: task.ID, CancelRequest: req})
				if err := qm.Destroy(t.Context(), task.ID); err != nil {
					t.Fatalf("queueManager.Destroy() error = %v, want nil", err)
				}
			} else {
				result, err = handler.CancelTask(t.Context(), req)
			}
			if err != nil {
				t.Fatalf("CancelTask() error = %v, want nil", err)
			}
			if got, ok := result.(*a2a.Task); !ok || got.Status.State != a2a.TaskStateCanceled {
				t.Fatalf("CancelTask() = %v, want canceled task", result)
			}
		})
	}
}

func TestAgentFirstOutputTimeout_FollowUp(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		cleaned := make(chan struct{}, 2)
		executor := &testexecutor.TestAgentExecutor{
			CleanupFn: func(context.Context, *a2asrv.ExecutorContext, a2a.SendMessageResult, error) { cleaned <- struct{}{} },
			ExecuteFn: func(ctx context.Context, ec *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
				return func(yield func(a2a.Event, error) bool) {
					if ec.StoredTask == nil {
						if !yield(a2a.NewSubmittedTask(ec, ec.Message), nil) {
							return
						}
						if !yield(a2a.NewArtifactEvent(ec, a2a.NewTextPart("hello")), nil) {
							return
						}
						yield(a2a.NewStatusUpdateEvent(ec, a2a.TaskStateInputRequired, nil), nil)
						return
					}
					<-ctx.Done()
					yield(nil, context.Cause(ctx))
				}
			},
		}
		handler := a2asrv.NewHandler(executor, a2asrv.WithAgentFirstOutputTimeout(time.Second, matchesArtifact))
		message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("start"))
		result, err := handler.SendMessage(t.Context(), &a2a.SendMessageRequest{Message: message})
		if err != nil {
			t.Fatalf("SendMessage() error = %v, want nil", err)
		}
		<-cleaned
		first, ok := result.(*a2a.Task)
		if !ok || first.Status.State != a2a.TaskStateInputRequired {
			t.Fatalf("SendMessage() = %v, want input-required task", result)
		}
		followUp := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("continue"))
		followUp.TaskID = first.ID
		result, err = handler.SendMessage(t.Context(), &a2a.SendMessageRequest{Message: followUp})
		if err != nil {
			t.Fatalf("SendMessage() error = %v, want nil", err)
		}
		<-cleaned
		if got, ok := result.(*a2a.Task); !ok || got.Status.State != a2a.TaskStateFailed {
			t.Fatalf("SendMessage() = %v, want failed follow-up task", result)
		}
	})
}

func TestAgentFirstOutputTimeout_Resubscribe(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		executor := a2asrv.AgentExecutorFunc(func(ctx context.Context, ec *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
			return func(yield func(a2a.Event, error) bool) {
				if !yield(a2a.NewSubmittedTask(ec, ec.Message), nil) {
					return
				}
				<-ctx.Done()
				yield(nil, context.Cause(ctx))
			}
		})
		handler := a2asrv.NewHandler(executor, a2asrv.WithAgentFirstOutputTimeout(time.Second, matchesArtifact))
		start := time.Now()
		var taskID a2a.TaskID
		for event, err := range handler.SendStreamingMessage(t.Context(), &a2a.SendMessageRequest{Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("work"))}) {
			if err != nil {
				t.Fatalf("SendStreamingMessage() error = %v, want nil", err)
			}
			if task, ok := event.(*a2a.Task); ok {
				taskID = task.ID
			}
			break
		}
		time.Sleep(time.Second / 2)
		var state a2a.TaskState
		for event, err := range handler.SubscribeToTask(t.Context(), &a2a.SubscribeToTaskRequest{ID: taskID}) {
			if err != nil {
				t.Fatalf("SubscribeToTask() error = %v, want nil", err)
			}
			if task, ok := event.(*a2a.Task); ok {
				state = task.Status.State
			}
		}
		if elapsed := time.Since(start); state != a2a.TaskStateFailed || elapsed != time.Second {
			t.Fatalf("SubscribeToTask() state = %v, elapsed = %v, want failed at original deadline", state, elapsed)
		}
	})
}
