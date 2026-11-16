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

package a2aclient

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
	name       string
	wrapCallFn func(next CallHandler) CallHandler
}

func (p *mockPlugin) Name() string { return p.name }

func (p *mockPlugin) WrapCall(next CallHandler) CallHandler {
	if p.wrapCallFn != nil {
		return p.wrapCallFn(next)
	}
	return next
}

func newPluginClient(transport Transport, interceptors []CallInterceptor, options ...FactoryOption) *Client {
	f := NewFactory(append([]FactoryOption{WithDefaultsDisabled()}, options...)...)
	return &Client{transport: transport, interceptors: interceptors, plugins: f.plugins}
}

func TestWithPlugins_Ordering(t *testing.T) {
	t.Parallel()
	var calls []string
	recordingPlugin := func(name string) Plugin {
		return &mockPlugin{name: name, wrapCallFn: func(next CallHandler) CallHandler {
			return func(ctx context.Context, req Request) iter.Seq2[any, error] {
				return func(yield func(any, error) bool) {
					calls = append(calls, name+"-before")
					defer func() { calls = append(calls, name+"-after") }()
					for v, err := range next(ctx, req) {
						if !yield(v, err) {
							return
						}
					}
				}
			}
		}}
	}
	interceptor := &testInterceptor{
		BeforeFn: func(ctx context.Context, _ *Request) (context.Context, any, error) {
			calls = append(calls, "interceptor-before")
			return ctx, nil, nil
		},
		AfterFn: func(context.Context, *Response) error {
			calls = append(calls, "interceptor-after")
			return nil
		},
	}
	transport := &testTransport{GetTaskFn: func(context.Context, ServiceParams, *a2a.GetTaskRequest) (*a2a.Task, error) {
		calls = append(calls, "transport")
		return &a2a.Task{}, nil
	}}
	client := newPluginClient(transport, []CallInterceptor{interceptor},
		WithPlugins(recordingPlugin("p1"), recordingPlugin("p2")),
		WithPlugins(recordingPlugin("p3")),
	)

	if _, err := client.GetTask(t.Context(), &a2a.GetTaskRequest{}); err != nil {
		t.Fatalf("client.GetTask() error = %v, want nil", err)
	}

	want := []string{
		"p1-before", "p2-before", "p3-before",
		"interceptor-before", "transport", "interceptor-after",
		"p3-after", "p2-after", "p1-after",
	}
	if diff := cmp.Diff(want, calls); diff != "" {
		t.Fatalf("client.GetTask() wrong call order (-want +got) diff = %s", diff)
	}
}

func TestWithPlugins_Retry(t *testing.T) {
	t.Parallel()
	retryPlugin := &mockPlugin{name: "retry", wrapCallFn: func(next CallHandler) CallHandler {
		return func(ctx context.Context, req Request) iter.Seq2[any, error] {
			return func(yield func(any, error) bool) {
				for v, err := range next(ctx, req) {
					if err == nil {
						yield(v, nil)
						return
					}
				}
				for v, err := range next(ctx, req) {
					yield(v, err)
					return
				}
			}
		}
	}}
	attempts := 0
	wantTask := &a2a.Task{ID: "task"}
	transport := &testTransport{GetTaskFn: func(context.Context, ServiceParams, *a2a.GetTaskRequest) (*a2a.Task, error) {
		attempts++
		if attempts == 1 {
			return nil, errors.New("transient failure")
		}
		return wantTask, nil
	}}
	client := newPluginClient(transport, nil, WithPlugins(retryPlugin))

	got, err := client.GetTask(t.Context(), &a2a.GetTaskRequest{})
	if err != nil {
		t.Fatalf("client.GetTask() error = %v, want nil", err)
	}
	if got != wantTask {
		t.Fatalf("client.GetTask() = %v, want %v", got, wantTask)
	}
	if attempts != 2 {
		t.Fatalf("client.GetTask() attempts = %d, want 2", attempts)
	}
}

func TestWithPlugins_EarlyAnswer(t *testing.T) {
	t.Parallel()
	earlyTask := &a2a.Task{ID: "early"}
	earlyPlugin := &mockPlugin{name: "early", wrapCallFn: func(next CallHandler) CallHandler {
		return func(context.Context, Request) iter.Seq2[any, error] {
			return func(yield func(any, error) bool) {
				yield(earlyTask, nil)
			}
		}
	}}
	interceptor := &testInterceptor{}
	transportCalled := false
	transport := &testTransport{GetTaskFn: func(context.Context, ServiceParams, *a2a.GetTaskRequest) (*a2a.Task, error) {
		transportCalled = true
		return &a2a.Task{}, nil
	}}
	client := newPluginClient(transport, []CallInterceptor{interceptor}, WithPlugins(earlyPlugin))

	got, err := client.GetTask(t.Context(), &a2a.GetTaskRequest{})
	if err != nil {
		t.Fatalf("client.GetTask() error = %v, want nil", err)
	}
	if got != earlyTask {
		t.Fatalf("client.GetTask() = %v, want %v", got, earlyTask)
	}
	if interceptor.lastReq != nil {
		t.Fatal("interceptor called, want plugin early answer")
	}
	if transportCalled {
		t.Fatal("transport called, want plugin early answer")
	}
}

func TestWithPlugins_Streaming(t *testing.T) {
	t.Parallel()
	var seen []string
	streamPlugin := &mockPlugin{name: "stream", wrapCallFn: func(next CallHandler) CallHandler {
		return func(ctx context.Context, req Request) iter.Seq2[any, error] {
			return func(yield func(any, error) bool) {
				defer func() { seen = append(seen, "end") }()
				for v, err := range next(ctx, req) {
					seen = append(seen, v.(*a2a.Message).ID)
					if !yield(v, err) {
						return
					}
				}
			}
		}
	}}
	transport := &testTransport{SendStreamingMessageFn: func(context.Context, ServiceParams, *a2a.SendMessageRequest) iter.Seq2[a2a.Event, error] {
		return makeEventSeq2([]a2a.Event{&a2a.Message{ID: "1"}, &a2a.Message{ID: "2"}, &a2a.Message{ID: "3"}})
	}}
	client := newPluginClient(transport, nil, WithPlugins(streamPlugin))

	for _, err := range client.SendStreamingMessage(t.Context(), &a2a.SendMessageRequest{Message: a2a.NewMessage(a2a.MessageRoleUser)}) {
		if err != nil {
			t.Fatalf("client.SendStreamingMessage() error = %v, want nil", err)
		}
	}

	want := []string{"1", "2", "3", "end"}
	if diff := cmp.Diff(want, seen); diff != "" {
		t.Fatalf("WrapCall() wrong observed values (-want +got) diff = %s", diff)
	}
}

func TestWithPlugins_WithAdditionalOptions(t *testing.T) {
	t.Parallel()
	p1, p2 := &mockPlugin{name: "p1"}, &mockPlugin{name: "p2"}
	f1 := NewFactory(WithPlugins(p1))
	f2 := WithAdditionalOptions(f1, WithPlugins(p2))

	if diff := cmp.Diff([]Plugin{p1, p2}, f2.plugins, cmp.AllowUnexported(mockPlugin{})); diff != "" {
		t.Fatalf("WithAdditionalOptions() wrong plugins (-want +got) diff = %s", diff)
	}
	if len(f1.plugins) != 1 {
		t.Fatalf("WithAdditionalOptions() modified an argument: len(f.plugins) = %d, want 1", len(f1.plugins))
	}
}
