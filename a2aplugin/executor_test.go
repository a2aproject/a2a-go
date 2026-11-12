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

package a2aplugin

import (
	"context"
	"errors"
	"iter"
	"reflect"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/internal/testutil"
)

type afterExecuteCall struct {
	ctx    context.Context
	result a2a.SendMessageResult
	err    error
}

func newAgent(events func(execCtx *a2asrv.ExecutorContext) []a2a.Event) a2asrv.AgentExecutor {
	return a2asrv.AgentExecutorFunc(func(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
		return func(yield func(a2a.Event, error) bool) {
			for _, event := range events(execCtx) {
				if !yield(event, nil) {
					return
				}
			}
		}
	})
}

func newUserMessage() *a2a.Message {
	return a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hi"))
}

func waitAfterExecute(t *testing.T, calls <-chan afterExecuteCall) afterExecuteCall {
	t.Helper()
	select {
	case call := <-calls:
		return call
	case <-time.After(time.Second):
		t.Fatal("AfterExecute() was not called")
		return afterExecuteCall{}
	}
}

type cleaningWrapper struct {
	a2asrv.AgentExecutor
	calls chan<- string
}

func (w *cleaningWrapper) Cleanup(context.Context, *a2asrv.ExecutorContext, a2a.SendMessageResult, error) {
	w.calls <- "wrapper cleanup"
}

func TestServer_NoExecutionHooksReturnsNext(t *testing.T) {
	t.Parallel()
	next := newAgent(func(*a2asrv.ExecutorContext) []a2a.Event { return nil })

	got := Server("noop", ServerHooks{}).WrapExecutor(next)

	if reflect.ValueOf(got).Pointer() != reflect.ValueOf(next).Pointer() {
		t.Fatal("WrapExecutor() returned a wrapper, want next unchanged")
	}
}

func TestServer_ExecutionHooks(t *testing.T) {
	t.Parallel()
	var agentCtxValue any
	agent := a2asrv.AgentExecutorFunc(func(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
		agentCtxValue = ctx.Value(ctxKey{})
		return func(yield func(a2a.Event, error) bool) {
			if !yield(a2a.NewSubmittedTask(execCtx, execCtx.Message), nil) {
				return
			}
			yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCompleted, nil), nil)
		}
	})
	var events []string
	afterCalls := make(chan afterExecuteCall, 1)
	plugin := Server("hooks", ServerHooks{
		BeforeExecute: func(ctx context.Context, _ *a2asrv.ExecutorContext) (context.Context, error) {
			return context.WithValue(ctx, ctxKey{}, "before"), nil
		},
		OnExecuteEvent: func(ctx context.Context, _ *a2asrv.ExecutorContext, event a2a.Event) (a2a.Event, error) {
			events = append(events, reflect.TypeOf(event).String()+":"+ctx.Value(ctxKey{}).(string))
			return event, nil
		},
		AfterExecute: func(ctx context.Context, _ *a2asrv.ExecutorContext, result a2a.SendMessageResult, err error) {
			afterCalls <- afterExecuteCall{ctx: ctx, result: result, err: err}
		},
	})
	handler := a2asrv.NewHandler(agent, a2asrv.WithPlugins(plugin))

	if _, err := handler.SendMessage(t.Context(), &a2a.SendMessageRequest{Message: newUserMessage()}); err != nil {
		t.Fatalf("handler.SendMessage() error = %v, want nil", err)
	}
	after := waitAfterExecute(t, afterCalls)

	if agentCtxValue != "before" {
		t.Fatalf("agent ctx value = %v, want %q", agentCtxValue, "before")
	}
	wantEvents := []string{"*a2a.Task:before", "*a2a.TaskStatusUpdateEvent:before"}
	if diff := cmp.Diff(wantEvents, events); diff != "" {
		t.Fatalf("OnExecuteEvent() wrong events (-want +got) diff = %s", diff)
	}
	if after.err != nil {
		t.Fatalf("AfterExecute() err = %v, want nil", after.err)
	}
	if task, ok := after.result.(*a2a.Task); !ok || task.Status.State != a2a.TaskStateCompleted {
		t.Fatalf("AfterExecute() result = %v, want a completed task", after.result)
	}
	if got := after.ctx.Value(ctxKey{}); got != "before" {
		t.Fatalf("AfterExecute() ctx value = %v, want %q", got, "before")
	}
	if err := after.ctx.Err(); err != nil {
		t.Fatalf("AfterExecute() ctx.Err() = %v, want nil", err)
	}
}

func TestServer_BeforeExecuteError(t *testing.T) {
	t.Parallel()
	agentCalled := false
	agent := a2asrv.AgentExecutorFunc(func(context.Context, *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
		agentCalled = true
		return func(func(a2a.Event, error) bool) {}
	})
	wantErr := errors.New("rejected")
	afterCalls := make(chan afterExecuteCall, 1)
	plugin := Server("reject", ServerHooks{
		BeforeExecute: func(ctx context.Context, _ *a2asrv.ExecutorContext) (context.Context, error) {
			return context.WithValue(ctx, ctxKey{}, "before"), wantErr
		},
		AfterExecute: func(ctx context.Context, _ *a2asrv.ExecutorContext, result a2a.SendMessageResult, err error) {
			afterCalls <- afterExecuteCall{ctx: ctx, result: result, err: err}
		},
	})
	handler := a2asrv.NewHandler(agent, a2asrv.WithPlugins(plugin))

	if _, err := handler.SendMessage(t.Context(), &a2a.SendMessageRequest{Message: newUserMessage()}); !errors.Is(err, wantErr) {
		t.Fatalf("handler.SendMessage() error = %v, want %v", err, wantErr)
	}
	after := waitAfterExecute(t, afterCalls)

	if agentCalled {
		t.Fatal("agent executed, want BeforeExecute to stop the execution")
	}
	if !errors.Is(after.err, wantErr) {
		t.Fatalf("AfterExecute() err = %v, want %v", after.err, wantErr)
	}
	if got := after.ctx.Value(ctxKey{}); got != "before" {
		t.Fatalf("AfterExecute() ctx value = %v, want %q", got, "before")
	}
}

func TestServer_BeforeExecutePanic(t *testing.T) {
	t.Parallel()
	agent := newAgent(func(*a2asrv.ExecutorContext) []a2a.Event { return nil })
	afterCalls := make(chan afterExecuteCall, 1)
	plugin := Server("panic", ServerHooks{
		BeforeExecute: func(context.Context, *a2asrv.ExecutorContext) (context.Context, error) {
			panic("before execute failed")
		},
		AfterExecute: func(ctx context.Context, _ *a2asrv.ExecutorContext, result a2a.SendMessageResult, err error) {
			afterCalls <- afterExecuteCall{ctx: ctx, result: result, err: err}
		},
	})
	handler := a2asrv.NewHandler(agent, a2asrv.WithPlugins(plugin))

	if _, err := handler.SendMessage(t.Context(), &a2a.SendMessageRequest{Message: newUserMessage()}); err == nil {
		t.Fatal("handler.SendMessage() error = nil, want the panic error")
	}
	after := waitAfterExecute(t, afterCalls)

	if after.err == nil {
		t.Fatal("AfterExecute() err = nil, want the panic error")
	}
}

func TestServer_OnExecuteEventReplacesEvent(t *testing.T) {
	t.Parallel()
	agent := newAgent(func(*a2asrv.ExecutorContext) []a2a.Event {
		return []a2a.Event{a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("original"))}
	})
	replaced := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("replaced"))
	plugin := Server("replace", ServerHooks{
		OnExecuteEvent: func(context.Context, *a2asrv.ExecutorContext, a2a.Event) (a2a.Event, error) {
			return replaced, nil
		},
	})
	handler := a2asrv.NewHandler(agent, a2asrv.WithPlugins(plugin))

	got, err := handler.SendMessage(t.Context(), &a2a.SendMessageRequest{Message: newUserMessage()})
	if err != nil {
		t.Fatalf("handler.SendMessage() error = %v, want nil", err)
	}

	if diff := cmp.Diff(a2a.SendMessageResult(replaced), got); diff != "" {
		t.Fatalf("handler.SendMessage() wrong result (-want +got) diff = %s", diff)
	}
}

func TestServer_AfterExecuteSkipsCancelation(t *testing.T) {
	t.Parallel()
	task := &a2a.Task{ID: a2a.NewTaskID(), ContextID: a2a.NewContextID(), Status: a2a.TaskStatus{State: a2a.TaskStateInputRequired}}
	agent := newAgent(func(*a2asrv.ExecutorContext) []a2a.Event { return nil })
	afterCalls := make(chan afterExecuteCall, 1)
	plugin := Server("after", ServerHooks{
		AfterExecute: func(ctx context.Context, _ *a2asrv.ExecutorContext, result a2a.SendMessageResult, err error) {
			afterCalls <- afterExecuteCall{ctx: ctx, result: result, err: err}
		},
	})
	handler := a2asrv.NewHandler(agent,
		a2asrv.WithTaskStore(testutil.NewTestTaskStore().WithTasks(t, task)),
		a2asrv.WithPlugins(plugin),
	)

	if _, err := handler.CancelTask(t.Context(), &a2a.CancelTaskRequest{ID: task.ID}); err != nil {
		t.Fatalf("handler.CancelTask() error = %v, want nil", err)
	}

	if len(afterCalls) != 0 {
		t.Fatalf("AfterExecute() calls after cancelation = %d, want 0", len(afterCalls))
	}
}

func TestServer_WrapExecutorOutsideHooks(t *testing.T) {
	t.Parallel()
	calls := make(chan string, 10)
	agent := newAgent(func(*a2asrv.ExecutorContext) []a2a.Event {
		calls <- "agent"
		return []a2a.Event{a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("done"))}
	})
	plugin := Server("order", ServerHooks{
		BeforeExecute: func(ctx context.Context, _ *a2asrv.ExecutorContext) (context.Context, error) {
			calls <- "before"
			return ctx, nil
		},
		WrapExecutor: func(next a2asrv.AgentExecutor) a2asrv.AgentExecutor {
			return a2asrv.AgentExecutorFunc(func(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
				calls <- "wrap"
				return next.Execute(ctx, execCtx)
			})
		},
	})
	handler := a2asrv.NewHandler(agent, a2asrv.WithPlugins(plugin))

	if _, err := handler.SendMessage(t.Context(), &a2a.SendMessageRequest{Message: newUserMessage()}); err != nil {
		t.Fatalf("handler.SendMessage() error = %v, want nil", err)
	}
	close(calls)
	var got []string
	for call := range calls {
		got = append(got, call)
	}

	if diff := cmp.Diff([]string{"wrap", "before", "agent"}, got); diff != "" {
		t.Fatalf("WrapExecutor() wrong call order (-want +got) diff = %s", diff)
	}
}

func TestServer_AfterExecuteWithWrapExecutor(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name      string
		wrap      func(next a2asrv.AgentExecutor, calls chan<- string) a2asrv.AgentExecutor
		wantCalls []string
	}{
		{
			name: "wrapper without cleanup",
			wrap: func(next a2asrv.AgentExecutor, _ chan<- string) a2asrv.AgentExecutor {
				return &struct{ a2asrv.AgentExecutor }{next}
			},
			wantCalls: []string{"after execute"},
		},
		{
			name: "wrapper with cleanup",
			wrap: func(next a2asrv.AgentExecutor, calls chan<- string) a2asrv.AgentExecutor {
				return &cleaningWrapper{AgentExecutor: next, calls: calls}
			},
			wantCalls: []string{"after execute", "wrapper cleanup"},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			calls := make(chan string, 10)
			agent := newAgent(func(*a2asrv.ExecutorContext) []a2a.Event {
				return []a2a.Event{a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("done"))}
			})
			plugin := Server("wrap", ServerHooks{
				AfterExecute: func(context.Context, *a2asrv.ExecutorContext, a2a.SendMessageResult, error) {
					calls <- "after execute"
				},
				WrapExecutor: func(next a2asrv.AgentExecutor) a2asrv.AgentExecutor {
					return tc.wrap(next, calls)
				},
			})
			handler := a2asrv.NewHandler(agent, a2asrv.WithPlugins(plugin))

			if _, err := handler.SendMessage(t.Context(), &a2a.SendMessageRequest{Message: newUserMessage()}); err != nil {
				t.Fatalf("handler.SendMessage() error = %v, want nil", err)
			}
			var got []string
			for range tc.wantCalls {
				select {
				case call := <-calls:
					got = append(got, call)
				case <-time.After(time.Second):
					t.Fatalf("Cleanup() calls = %v, want %v", got, tc.wantCalls)
				}
			}

			if diff := cmp.Diff(tc.wantCalls, got); diff != "" {
				t.Fatalf("Cleanup() wrong call order (-want +got) diff = %s", diff)
			}
		})
	}
}
