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

type recordingServerPlugin struct {
	name      string
	log       *[]string
	beforeErr error
	ctxKey    any
	ctxValue  string
	gotReq    *ServerRequest
}

var _ ServerPlugin = (*recordingServerPlugin)(nil)

func (p *recordingServerPlugin) Name() string { return p.name }

func (p *recordingServerPlugin) BeforeHandlerRun(ctx context.Context, req *ServerRequest) (context.Context, error) {
	p.gotReq = req
	*p.log = append(*p.log, p.name+":before:"+req.Method)
	if p.beforeErr != nil {
		return ctx, p.beforeErr
	}
	if p.ctxKey != nil {
		ctx = context.WithValue(ctx, p.ctxKey, p.ctxValue)
	}
	return ctx, nil
}

func (p *recordingServerPlugin) AfterHandlerRun(ctx context.Context, resp *ServerResponse) (context.Context, error) {
	entry := p.name + ":after"
	switch {
	case resp.IsStreamEvent:
		entry += "-event"
	case resp.IsFinal:
		entry += "-final"
	}
	if resp.Err != nil {
		entry += "(err=" + resp.Err.Error() + ")"
	}
	*p.log = append(*p.log, entry)
	return ctx, nil
}

type noHooksPlugin struct {
	recordingServerPlugin
}

func (*noHooksPlugin) HasServerHooks() bool { return false }

type recordingInterceptor struct {
	PassthroughCallInterceptor
	name string
	log  *[]string
}

func (r *recordingInterceptor) Before(ctx context.Context, _ *CallContext, _ *Request) (context.Context, any, error) {
	*r.log = append(*r.log, r.name+":before")
	return ctx, nil, nil
}

func (r *recordingInterceptor) After(ctx context.Context, _ *CallContext, _ *Response) error {
	*r.log = append(*r.log, r.name+":after")
	return nil
}

func newPluginTestHandler(plugin ServerPlugin, inner RequestHandler, interceptors ...CallInterceptor) RequestHandler {
	return &InterceptedHandler{Handler: inner, Interceptors: interceptors, Plugins: plugin}
}

func newSendMessageRequest() *a2a.SendMessageRequest {
	return &a2a.SendMessageRequest{
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hi")),
	}
}

func yieldAll(events ...a2a.Event) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		for _, ev := range events {
			if !yield(ev, nil) {
				return
			}
		}
	}
}

func TestWithPlugins(t *testing.T) {
	t.Parallel()

	existing := &recordingServerPlugin{name: "existing"}
	replacement := &recordingServerPlugin{name: "replacement"}

	tests := []struct {
		name   string
		option ServerPlugin
		want   ServerPlugin
	}{
		{name: "installs plugin", option: replacement, want: replacement},
		{name: "nil is no-op", option: nil, want: existing},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ih := &InterceptedHandler{Plugins: existing}
			WithPlugins(tc.option)(ih, &defaultRequestHandler{})
			if ih.Plugins != tc.want {
				t.Errorf("WithPlugins() set Plugins = %v, want %v", ih.Plugins, tc.want)
			}
		})
	}
}

func TestPluginUnaryHooks(t *testing.T) {
	t.Parallel()

	rejected := errors.New("rejected")
	handlerErr := errors.New("boom")

	tests := []struct {
		name       string
		beforeErr  error
		handlerErr error
		wantErr    error
		wantLog    []string
	}{
		{
			name:    "success",
			wantLog: []string{"p:before:GetTask", "p:after-final"},
		},
		{
			name:      "before error rejects call",
			beforeErr: rejected,
			wantErr:   rejected,
			wantLog:   []string{"p:before:GetTask"},
		},
		{
			name:       "handler error reaches final after",
			handlerErr: handlerErr,
			wantErr:    handlerErr,
			wantLog:    []string{"p:before:GetTask", "p:after-final(err=boom)"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var log []string
			plugin := &recordingServerPlugin{name: "p", log: &log, beforeErr: tc.beforeErr}
			inner := &mockHandler{resultErr: tc.handlerErr}
			handler := newPluginTestHandler(plugin, inner)

			_, err := handler.GetTask(t.Context(), &a2a.GetTaskRequest{ID: "task-1"})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("GetTask() error = %v, want %v", err, tc.wantErr)
			}
			if tc.beforeErr != nil && inner.lastCallContext != nil {
				t.Fatalf("GetTask() invoked the handler despite plugin rejection")
			}
			if diff := cmp.Diff(tc.wantLog, log); diff != "" {
				t.Fatalf("GetTask() wrong plugin log (-want +got) diff = %s", diff)
			}
		})
	}
}

func TestPluginReceivesCallContext(t *testing.T) {
	t.Parallel()

	var log []string
	plugin := &recordingServerPlugin{name: "p", log: &log}
	handler := newPluginTestHandler(plugin, &mockHandler{})

	if _, err := handler.GetTask(t.Context(), &a2a.GetTaskRequest{ID: "x", Tenant: "acme"}); err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if plugin.gotReq.CallContext == nil {
		t.Fatalf("BeforeHandlerRun() got nil CallContext")
	}
	if got := plugin.gotReq.CallContext.Tenant(); got != "acme" {
		t.Fatalf("CallContext.Tenant() = %q, want %q", got, "acme")
	}
}

func TestPluginContextFlowsToHandler(t *testing.T) {
	t.Parallel()

	type ctxKey struct{}
	var log []string
	plugin := &recordingServerPlugin{name: "p", log: &log, ctxKey: ctxKey{}, ctxValue: "set-by-plugin"}

	var seen string
	inner := &mockHandler{
		GetTaskFn: func(ctx context.Context, _ *a2a.GetTaskRequest) (*a2a.Task, error) {
			seen, _ = ctx.Value(ctxKey{}).(string)
			return &a2a.Task{}, nil
		},
	}
	handler := newPluginTestHandler(plugin, inner)

	if _, err := handler.GetTask(t.Context(), &a2a.GetTaskRequest{ID: "x"}); err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if seen != "set-by-plugin" {
		t.Fatalf("handler ctx value = %q, want %q", seen, "set-by-plugin")
	}
}

func TestPluginSkipped(t *testing.T) {
	t.Parallel()

	var log []string
	tests := []struct {
		name   string
		plugin ServerPlugin
	}{
		{name: "nil plugin", plugin: nil},
		{name: "plugin reports no server hooks", plugin: &noHooksPlugin{recordingServerPlugin{name: "p", log: &log}}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			handler := newPluginTestHandler(tc.plugin, &mockHandler{})
			if _, err := handler.GetTask(t.Context(), &a2a.GetTaskRequest{ID: "x"}); err != nil {
				t.Fatalf("GetTask() error = %v", err)
			}
			if len(log) != 0 {
				t.Fatalf("GetTask() plugin log = %v, want empty", log)
			}
		})
	}
}

func TestPluginStreamingHooks(t *testing.T) {
	t.Parallel()

	streamErr := errors.New("stream boom")
	rejected := errors.New("rejected")

	tests := []struct {
		name      string
		events    iter.Seq2[a2a.Event, error]
		beforeErr error
		breakAt   int
		wantErr   error
		wantLog   []string
	}{
		{
			name:    "per-event and final",
			events:  yieldAll(&a2a.Task{ID: "t1"}, &a2a.Task{ID: "t2"}),
			wantLog: []string{"p:before:SendStreamingMessage", "p:after-event", "p:after-event", "p:after-final"},
		},
		{
			name:    "caller break still fires final",
			events:  yieldAll(&a2a.Task{ID: "t1"}, &a2a.Task{ID: "t2"}, &a2a.Task{ID: "t3"}),
			breakAt: 2,
			wantLog: []string{"p:before:SendStreamingMessage", "p:after-event", "p:after-event", "p:after-final"},
		},
		{
			name: "handler error reaches final",
			events: func(yield func(a2a.Event, error) bool) {
				if !yield(&a2a.Task{ID: "t1"}, nil) {
					return
				}
				yield(nil, streamErr)
			},
			wantErr: streamErr,
			wantLog: []string{
				"p:before:SendStreamingMessage",
				"p:after-event",
				"p:after-event(err=stream boom)",
				"p:after-final(err=stream boom)",
			},
		},
		{
			name:      "before error rejects call",
			events:    yieldAll(&a2a.Task{ID: "t1"}),
			beforeErr: rejected,
			wantErr:   rejected,
			wantLog:   []string{"p:before:SendStreamingMessage"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var log []string
			plugin := &recordingServerPlugin{name: "p", log: &log, beforeErr: tc.beforeErr}
			inner := &mockHandler{
				SendStreamingMessageFn: func(context.Context, *a2a.SendMessageRequest) iter.Seq2[a2a.Event, error] {
					return tc.events
				},
			}
			handler := newPluginTestHandler(plugin, inner)

			var gotErr error
			seen := 0
			for _, err := range handler.SendStreamingMessage(t.Context(), newSendMessageRequest()) {
				if err != nil {
					gotErr = err
				}
				seen++
				if seen == tc.breakAt {
					break
				}
			}
			if !errors.Is(gotErr, tc.wantErr) {
				t.Fatalf("SendStreamingMessage() error = %v, want %v", gotErr, tc.wantErr)
			}
			if diff := cmp.Diff(tc.wantLog, log); diff != "" {
				t.Fatalf("SendStreamingMessage() wrong plugin log (-want +got) diff = %s", diff)
			}
		})
	}
}

func TestPluginWrapsOutsideInterceptors(t *testing.T) {
	t.Parallel()

	var log []string
	plugin := &recordingServerPlugin{name: "pl", log: &log}
	interceptor := &recordingInterceptor{name: "ic", log: &log}
	handler := newPluginTestHandler(plugin, &mockHandler{}, interceptor)

	if _, err := handler.GetTask(t.Context(), &a2a.GetTaskRequest{ID: "x"}); err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}

	want := []string{"pl:before:GetTask", "ic:before", "ic:after", "pl:after-final"}
	if diff := cmp.Diff(want, log); diff != "" {
		t.Fatalf("GetTask() wrong ordering (-want +got) diff = %s", diff)
	}
}
