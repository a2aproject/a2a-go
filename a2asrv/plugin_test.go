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
	"iter"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

type mockPlugin struct {
	PassthroughPlugin
	wrapCallFn func(next CallHandler) CallHandler
}

func (p *mockPlugin) Name() string { return "mock" }

func (p *mockPlugin) WrapCall(next CallHandler) CallHandler {
	if p.wrapCallFn != nil {
		return p.wrapCallFn(next)
	}
	return next
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
