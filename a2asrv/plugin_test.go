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

package a2asrv

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv/push"
	"github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"
	"github.com/a2aproject/a2a-go/v2/internal/testutil"
)

type mockPlugin struct {
	PassthroughPlugin
	wrapCallFn     func(next CallHandler) CallHandler
	wrapExecutorFn func(next AgentExecutor) AgentExecutor
	wrapStoreFn    func(next taskstore.Store) taskstore.Store
	wrapPushFn     func(next push.DeliveryHandler) push.DeliveryHandler
}

func (p *mockPlugin) Name() string { return "mock" }

func (p *mockPlugin) WrapCall(next CallHandler) CallHandler {
	if p.wrapCallFn != nil {
		return p.wrapCallFn(next)
	}
	return next
}

func (p *mockPlugin) WrapExecutor(next AgentExecutor) AgentExecutor {
	if p.wrapExecutorFn != nil {
		return p.wrapExecutorFn(next)
	}
	return next
}

func (p *mockPlugin) WrapTaskStore(next taskstore.Store) taskstore.Store {
	if p.wrapStoreFn != nil {
		return p.wrapStoreFn(next)
	}
	return next
}

func (p *mockPlugin) WrapPushDelivery(next push.DeliveryHandler) push.DeliveryHandler {
	if p.wrapPushFn != nil {
		return p.wrapPushFn(next)
	}
	return next
}

type callRecorder struct {
	mu    sync.Mutex
	calls []string
}

func (r *callRecorder) add(call string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, call)
}

func (r *callRecorder) get() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.calls)
}

type recordingStore struct {
	taskstore.Store
	name     string
	recorder *callRecorder
}

func (s *recordingStore) Create(ctx context.Context, task *a2a.Task) (taskstore.TaskVersion, error) {
	s.recorder.add(s.name + "-create")
	return s.Store.Create(ctx, task)
}

func (s *recordingStore) Update(ctx context.Context, req *taskstore.UpdateRequest) (taskstore.TaskVersion, error) {
	s.recorder.add(s.name + "-update")
	return s.Store.Update(ctx, req)
}

func (s *recordingStore) Get(ctx context.Context, taskID a2a.TaskID) (*taskstore.StoredTask, error) {
	s.recorder.add(s.name + "-get")
	return s.Store.Get(ctx, taskID)
}

func (s *recordingStore) List(ctx context.Context, req *a2a.ListTasksRequest) (*a2a.ListTasksResponse, error) {
	s.recorder.add(s.name + "-list")
	return s.Store.List(ctx, req)
}

func recordingStorePlugin(name string, recorder *callRecorder) Plugin {
	return &mockPlugin{wrapStoreFn: func(next taskstore.Store) taskstore.Store {
		return &recordingStore{Store: next, name: name, recorder: recorder}
	}}
}

type recordingExecutor struct {
	AgentExecutor
	name  string
	calls chan<- string
}

func (e *recordingExecutor) Execute(ctx context.Context, execCtx *ExecutorContext) iter.Seq2[a2a.Event, error] {
	e.calls <- e.name + "-execute"
	return e.AgentExecutor.Execute(ctx, execCtx)
}

type cleaningExecutor struct {
	recordingExecutor
}

func (e *cleaningExecutor) Cleanup(context.Context, *ExecutorContext, a2a.SendMessageResult, error) {
	e.calls <- e.name + "-cleanup"
}

func recordingExecutorPlugin(name string, calls chan<- string) Plugin {
	return &mockPlugin{wrapExecutorFn: func(next AgentExecutor) AgentExecutor {
		return &recordingExecutor{AgentExecutor: next, name: name, calls: calls}
	}}
}

func cleaningExecutorPlugin(name string, calls chan<- string) Plugin {
	return &mockPlugin{wrapExecutorFn: func(next AgentExecutor) AgentExecutor {
		return &cleaningExecutor{recordingExecutor{AgentExecutor: next, name: name, calls: calls}}
	}}
}

func newRecordingAgent(calls chan<- string) *mockAgentExecutor {
	return &mockAgentExecutor{
		ExecuteFunc: func(context.Context, *ExecutorContext) iter.Seq2[a2a.Event, error] {
			calls <- "agent-execute"
			return func(yield func(a2a.Event, error) bool) {
				yield(a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("done")), nil)
			}
		},
		CleanupFn: func(context.Context, a2a.SendMessageResult, error) {
			calls <- "agent-cleanup"
		},
	}
}

func collectCallsUntil(t *testing.T, calls <-chan string, last string) []string {
	t.Helper()
	var got []string
	for {
		select {
		case call := <-calls:
			got = append(got, call)
			if call == last {
				return got
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for %q, got calls %v", last, got)
		}
	}
}

func newPluginHandler(h RequestHandler, interceptors []CallInterceptor, options ...RequestHandlerOption) *InterceptedHandler {
	ih := &InterceptedHandler{Handler: h, Interceptors: interceptors}
	for _, option := range options {
		option(ih, nil)
	}
	return ih
}

func TestWithPlugins_Ordering(t *testing.T) {
	t.Parallel()
	var calls []string
	recordingPlugin := func(name string) Plugin {
		return &mockPlugin{wrapCallFn: func(next CallHandler) CallHandler {
			return func(ctx context.Context, callCtx *CallContext, req Request) iter.Seq2[any, error] {
				return func(yield func(any, error) bool) {
					calls = append(calls, name+"-before")
					defer func() { calls = append(calls, name+"-after") }()
					for v, err := range next(ctx, callCtx, req) {
						if !yield(v, err) {
							return
						}
					}
				}
			}
		}}
	}
	interceptor := &mockInterceptor{
		beforeFn: func(ctx context.Context, _ *CallContext, _ *Request) (context.Context, any, error) {
			calls = append(calls, "interceptor-before")
			return ctx, nil, nil
		},
		afterFn: func(context.Context, *CallContext, *Response) error {
			calls = append(calls, "interceptor-after")
			return nil
		},
	}
	mockHandler := &mockHandler{GetTaskFn: func(context.Context, *a2a.GetTaskRequest) (*a2a.Task, error) {
		calls = append(calls, "handler")
		return &a2a.Task{}, nil
	}}
	handler := newPluginHandler(mockHandler, []CallInterceptor{interceptor},
		WithPlugins(recordingPlugin("p1"), recordingPlugin("p2")),
		WithPlugins(recordingPlugin("p3")),
	)

	if _, err := handler.GetTask(t.Context(), &a2a.GetTaskRequest{}); err != nil {
		t.Fatalf("handler.GetTask() error = %v, want nil", err)
	}

	want := []string{
		"p1-before", "p2-before", "p3-before",
		"interceptor-before", "handler", "interceptor-after",
		"p3-after", "p2-after", "p1-after",
	}
	if diff := cmp.Diff(want, calls); diff != "" {
		t.Fatalf("handler.GetTask() wrong call order (-want +got) diff = %s", diff)
	}
}

func TestWithPlugins_Retry(t *testing.T) {
	t.Parallel()
	retryPlugin := &mockPlugin{wrapCallFn: func(next CallHandler) CallHandler {
		return func(ctx context.Context, callCtx *CallContext, req Request) iter.Seq2[any, error] {
			return func(yield func(any, error) bool) {
				for v, err := range next(ctx, callCtx, req) {
					if err == nil {
						yield(v, nil)
						return
					}
				}
				for v, err := range next(ctx, callCtx, req) {
					yield(v, err)
					return
				}
			}
		}
	}}
	attempts := 0
	wantTask := &a2a.Task{ID: "task"}
	mockHandler := &mockHandler{GetTaskFn: func(context.Context, *a2a.GetTaskRequest) (*a2a.Task, error) {
		attempts++
		if attempts == 1 {
			return nil, errors.New("transient failure")
		}
		return wantTask, nil
	}}
	handler := newPluginHandler(mockHandler, nil, WithPlugins(retryPlugin))

	got, err := handler.GetTask(t.Context(), &a2a.GetTaskRequest{})
	if err != nil {
		t.Fatalf("handler.GetTask() error = %v, want nil", err)
	}
	if got != wantTask {
		t.Fatalf("handler.GetTask() = %v, want %v", got, wantTask)
	}
	if attempts != 2 {
		t.Fatalf("handler.GetTask() attempts = %d, want 2", attempts)
	}
}

func TestWithPlugins_EarlyAnswer(t *testing.T) {
	t.Parallel()
	earlyTask := &a2a.Task{ID: "early"}
	earlyPlugin := &mockPlugin{wrapCallFn: func(next CallHandler) CallHandler {
		return func(context.Context, *CallContext, Request) iter.Seq2[any, error] {
			return func(yield func(any, error) bool) {
				yield(earlyTask, nil)
			}
		}
	}}
	interceptorCalled := false
	interceptor := &mockInterceptor{
		beforeFn: func(ctx context.Context, _ *CallContext, _ *Request) (context.Context, any, error) {
			interceptorCalled = true
			return ctx, nil, nil
		},
	}
	mockHandler := &mockHandler{}
	handler := newPluginHandler(mockHandler, []CallInterceptor{interceptor}, WithPlugins(earlyPlugin))

	got, err := handler.GetTask(t.Context(), &a2a.GetTaskRequest{})
	if err != nil {
		t.Fatalf("handler.GetTask() error = %v, want nil", err)
	}
	if got != earlyTask {
		t.Fatalf("handler.GetTask() = %v, want %v", got, earlyTask)
	}
	if interceptorCalled {
		t.Fatal("interceptor called, want plugin early answer")
	}
	if mockHandler.lastCallContext != nil {
		t.Fatal("handler called, want plugin early answer")
	}
}

func TestWithPlugins_Streaming(t *testing.T) {
	t.Parallel()
	var seen []string
	streamPlugin := &mockPlugin{wrapCallFn: func(next CallHandler) CallHandler {
		return func(ctx context.Context, callCtx *CallContext, req Request) iter.Seq2[any, error] {
			return func(yield func(any, error) bool) {
				defer func() { seen = append(seen, "end") }()
				for v, err := range next(ctx, callCtx, req) {
					seen = append(seen, v.(*a2a.Message).ID)
					if !yield(v, err) {
						return
					}
				}
			}
		}
	}}
	mockHandler := &mockHandler{SendStreamingMessageFn: func(context.Context, *a2a.SendMessageRequest) iter.Seq2[a2a.Event, error] {
		return func(yield func(a2a.Event, error) bool) {
			for _, id := range []string{"1", "2", "3"} {
				if !yield(&a2a.Message{ID: id}, nil) {
					return
				}
			}
		}
	}}
	handler := newPluginHandler(mockHandler, nil, WithPlugins(streamPlugin))

	for _, err := range handler.SendStreamingMessage(t.Context(), &a2a.SendMessageRequest{}) {
		if err != nil {
			t.Fatalf("handler.SendStreamingMessage() error = %v, want nil", err)
		}
	}

	want := []string{"1", "2", "3", "end"}
	if diff := cmp.Diff(want, seen); diff != "" {
		t.Fatalf("WrapCall() wrong observed values (-want +got) diff = %s", diff)
	}
}

func TestWithPlugins_WrapExecutor(t *testing.T) {
	t.Parallel()
	calls := make(chan string, 10)
	interceptor := interceptExecCtxFn(func(ctx context.Context, _ *ExecutorContext) (context.Context, error) {
		calls <- "interceptor"
		return ctx, nil
	})
	handler := NewHandler(newRecordingAgent(calls),
		WithExecutorContextInterceptor(interceptor),
		WithPlugins(recordingExecutorPlugin("p1", calls), recordingExecutorPlugin("p2", calls)),
	)

	msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hi"))
	if _, err := handler.SendMessage(t.Context(), &a2a.SendMessageRequest{Message: msg}); err != nil {
		t.Fatalf("handler.SendMessage() error = %v, want nil", err)
	}

	want := []string{"interceptor", "p1-execute", "p2-execute", "agent-execute", "agent-cleanup"}
	if diff := cmp.Diff(want, collectCallsUntil(t, calls, "agent-cleanup")); diff != "" {
		t.Fatalf("handler.SendMessage() wrong call order (-want +got) diff = %s", diff)
	}
}

func TestWithPlugins_ExecutorCleanup(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name    string
		plugins func(calls chan<- string) []Plugin
		want    []string
	}{
		{
			name: "wrapper without Cleanup",
			plugins: func(calls chan<- string) []Plugin {
				return []Plugin{recordingExecutorPlugin("p1", calls)}
			},
			want: []string{"agent-cleanup"},
		},
		{
			name: "passthrough plugin",
			plugins: func(chan<- string) []Plugin {
				return []Plugin{&mockPlugin{}}
			},
			want: []string{"agent-cleanup"},
		},
		{
			name: "wrappers with Cleanup run innermost first",
			plugins: func(calls chan<- string) []Plugin {
				return []Plugin{cleaningExecutorPlugin("p1", calls), cleaningExecutorPlugin("p2", calls)}
			},
			want: []string{"agent-cleanup", "p2-cleanup", "p1-cleanup"},
		},
		{
			name: "mixed wrappers",
			plugins: func(calls chan<- string) []Plugin {
				return []Plugin{
					cleaningExecutorPlugin("p1", calls),
					&mockPlugin{},
					recordingExecutorPlugin("p3", calls),
					cleaningExecutorPlugin("p4", calls),
				}
			},
			want: []string{"agent-cleanup", "p4-cleanup", "p1-cleanup"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			calls := make(chan string, 10)
			plugins := append([]Plugin{cleaningExecutorPlugin("end", calls)}, tc.plugins(calls)...)
			handler := NewHandler(newRecordingAgent(calls), WithPlugins(plugins...))

			msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hi"))
			if _, err := handler.SendMessage(t.Context(), &a2a.SendMessageRequest{Message: msg}); err != nil {
				t.Fatalf("handler.SendMessage() error = %v, want nil", err)
			}

			var got []string
			for _, call := range collectCallsUntil(t, calls, "end-cleanup") {
				if strings.HasSuffix(call, "-cleanup") && call != "end-cleanup" {
					got = append(got, call)
				}
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("Cleanup() wrong call order (-want +got) diff = %s", diff)
			}
		})
	}
}

func TestWithPlugins_PassthroughExecutorFunc(t *testing.T) {
	t.Parallel()
	agent := AgentExecutorFunc(func(context.Context, *ExecutorContext) iter.Seq2[a2a.Event, error] {
		return func(yield func(a2a.Event, error) bool) {
			yield(a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("done")), nil)
		}
	})
	handler := NewHandler(agent, WithPlugins(&mockPlugin{}))

	msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hi"))
	if _, err := handler.SendMessage(t.Context(), &a2a.SendMessageRequest{Message: msg}); err != nil {
		t.Fatalf("handler.SendMessage() error = %v, want nil", err)
	}
}

func TestWithPlugins_WrapTaskStore(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name    string
		options []RequestHandlerOption
	}{
		{name: "default store"},
		{name: "custom store", options: []RequestHandlerOption{WithTaskStore(testutil.NewTestTaskStore())}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			recorder := &callRecorder{}
			agent := AgentExecutorFunc(func(_ context.Context, execCtx *ExecutorContext) iter.Seq2[a2a.Event, error] {
				return func(yield func(a2a.Event, error) bool) {
					if !yield(a2a.NewSubmittedTask(execCtx, execCtx.Message), nil) {
						return
					}
					yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCompleted, nil), nil)
				}
			})
			options := append(tc.options, WithPlugins(recordingStorePlugin("p1", recorder), recordingStorePlugin("p2", recorder)))
			handler := NewHandler(agent, options...)

			msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hi"))
			result, err := handler.SendMessage(t.Context(), &a2a.SendMessageRequest{Message: msg})
			if err != nil {
				t.Fatalf("handler.SendMessage() error = %v, want nil", err)
			}
			if _, err := handler.GetTask(t.Context(), &a2a.GetTaskRequest{ID: result.TaskInfo().TaskID}); err != nil {
				t.Fatalf("handler.GetTask() error = %v, want nil", err)
			}

			want := []string{
				"p1-create", "p2-create", "p1-update", "p2-update",
				"p1-get", "p2-get", // SendMessage loads the final task state
				"p1-get", "p2-get",
			}
			if diff := cmp.Diff(want, recorder.get()); diff != "" {
				t.Fatalf("WrapTaskStore() wrong store calls (-want +got) diff = %s", diff)
			}
		})
	}
}

func TestWithPlugins_WrapPushDelivery(t *testing.T) {
	t.Parallel()
	recorder := &callRecorder{}
	var gotHeaders []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Values("X-Plugin")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	headerPlugin := func(name string) Plugin {
		return &mockPlugin{wrapPushFn: func(next push.DeliveryHandler) push.DeliveryHandler {
			return func(ctx context.Context, d *push.Delivery) (*push.DeliveryResult, error) {
				recorder.add(name + "-before")
				d.Headers.Add("X-Plugin", name)
				result, err := next(ctx, d)
				recorder.add(fmt.Sprintf("%s-after-%d", name, result.StatusCode))
				return result, err
			}
		}}
	}
	taskSeed := &a2a.Task{ID: a2a.NewTaskID(), ContextID: a2a.NewContextID()}
	agent := newEventReplayAgent([]a2a.Event{newFinalTaskStatusUpdate(taskSeed, a2a.TaskStateCompleted, "done")}, nil)
	sender := push.NewHTTPPushSender(&push.HTTPSenderConfig{AllowPrivateNetworks: true})
	handler := NewHandler(agent,
		WithTaskStore(testutil.NewTestTaskStore().WithTasks(t, taskSeed)),
		WithPushNotifications(push.NewInMemoryStore(), sender),
		WithPlugins(headerPlugin("p1"), headerPlugin("p2")),
	)

	input := &a2a.SendMessageRequest{
		Message: newUserMessage(taskSeed, "work"),
		Config:  &a2a.SendMessageConfig{PushConfig: &a2a.PushConfig{URL: server.URL}},
	}
	if _, err := handler.SendMessage(t.Context(), input); err != nil {
		t.Fatalf("handler.SendMessage() error = %v, want nil", err)
	}

	wantCalls := []string{"p1-before", "p2-before", "p2-after-200", "p1-after-200"}
	if diff := cmp.Diff(wantCalls, recorder.get()); diff != "" {
		t.Fatalf("WrapPushDelivery() wrong call order (-want +got) diff = %s", diff)
	}
	if diff := cmp.Diff([]string{"p1", "p2"}, gotHeaders); diff != "" {
		t.Fatalf("X-Plugin header wrong values (-want +got) diff = %s", diff)
	}
}

func TestWithPlugins_SkipPushDelivery(t *testing.T) {
	t.Parallel()
	skipPlugin := &mockPlugin{wrapPushFn: func(next push.DeliveryHandler) push.DeliveryHandler {
		return func(ctx context.Context, d *push.Delivery) (*push.DeliveryResult, error) {
			d.Skip = true
			return next(ctx, d)
		}
	}}
	taskSeed := &a2a.Task{ID: a2a.NewTaskID(), ContextID: a2a.NewContextID()}
	agent := newEventReplayAgent([]a2a.Event{newFinalTaskStatusUpdate(taskSeed, a2a.TaskStateCompleted, "done")}, nil)
	failingSender := push.NewHTTPPushSender(&push.HTTPSenderConfig{FailOnError: true})
	handler := NewHandler(agent,
		WithTaskStore(testutil.NewTestTaskStore().WithTasks(t, taskSeed)),
		WithPushNotifications(push.NewInMemoryStore(), failingSender),
		WithPlugins(skipPlugin),
	)

	input := &a2a.SendMessageRequest{
		Message: newUserMessage(taskSeed, "work"),
		Config:  &a2a.SendMessageConfig{PushConfig: &a2a.PushConfig{URL: "http://localhost:1"}},
	}
	result, err := handler.SendMessage(t.Context(), input)
	if err != nil {
		t.Fatalf("handler.SendMessage() error = %v, want nil", err)
	}
	if state := result.(*a2a.Task).Status.State; state != a2a.TaskStateCompleted {
		t.Fatalf("handler.SendMessage() state = %q, want %q", state, a2a.TaskStateCompleted)
	}
}
