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
	"github.com/a2aproject/a2a-go/v2/a2aplugin/core"
)

type recordingClientPlugin struct {
	name      string
	log       *[]string
	beforeErr error
	beforeFn  func(ctx context.Context, req *ClientRequest) context.Context
}

var _ ClientPlugin = (*recordingClientPlugin)(nil)

func (p *recordingClientPlugin) Name() string { return p.name }

func (p *recordingClientPlugin) BeforeClientRequest(ctx context.Context, req *ClientRequest) (context.Context, error) {
	*p.log = append(*p.log, p.name+":before:"+req.Method)
	if p.beforeErr != nil {
		return ctx, p.beforeErr
	}
	if p.beforeFn != nil {
		ctx = p.beforeFn(ctx, req)
	}
	return ctx, nil
}

func (p *recordingClientPlugin) AfterClientRequest(ctx context.Context, resp *ClientResponse) (context.Context, error) {
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

type noClientHooksPlugin struct {
	recordingClientPlugin
}

func (*noClientHooksPlugin) HasClientHooks() bool { return false }

type namedOnlyPlugin struct{}

func (namedOnlyPlugin) Name() string { return "named-only" }

func newPluginTestClient(transport Transport, plugin ClientPlugin, interceptors ...CallInterceptor) *Client {
	return &Client{transport: transport, plugins: plugin, interceptors: interceptors}
}

func newSendMessageRequest() *a2a.SendMessageRequest {
	return &a2a.SendMessageRequest{Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hi"))}
}

func TestWithPlugins(t *testing.T) {
	t.Parallel()

	existing := &recordingClientPlugin{name: "existing"}
	replacement := &recordingClientPlugin{name: "replacement"}

	tests := []struct {
		name   string
		option ClientPlugin
		want   ClientPlugin
	}{
		{name: "installs plugin", option: replacement, want: replacement},
		{name: "nil is no-op", option: nil, want: existing},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := NewFactory(WithPlugins(existing), WithPlugins(tc.option))
			if f.plugins != tc.want {
				t.Fatalf("NewFactory(WithPlugins()) plugins = %v, want %v", f.plugins, tc.want)
			}
		})
	}
}

func TestWithAdditionalOptionsKeepsPlugins(t *testing.T) {
	t.Parallel()

	plugin := &recordingClientPlugin{name: "p"}
	f := WithAdditionalOptions(NewFactory(WithPlugins(plugin)))
	if f.plugins != plugin {
		t.Fatalf("WithAdditionalOptions() plugins = %v, want %v", f.plugins, plugin)
	}
}

func TestFactoryUsesGlobalPlugin(t *testing.T) {
	t.Cleanup(func() { core.SetGlobal(namedOnlyPlugin{}) })

	global := &recordingClientPlugin{name: "global"}
	explicit := &recordingClientPlugin{name: "explicit"}
	core.SetGlobal(global)

	tests := []struct {
		name string
		opts []FactoryOption
		want ClientPlugin
	}{
		{name: "global is the default", want: global},
		{name: "WithPlugins overrides global", opts: []FactoryOption{WithPlugins(explicit)}, want: explicit},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := append([]FactoryOption{
				WithTransport(a2a.TransportProtocolJSONRPC, TransportFactoryFn(func(context.Context, *a2a.AgentCard, *a2a.AgentInterface) (Transport, error) {
					return unimplementedTransport{}, nil
				})),
			}, tc.opts...)
			iface := a2a.NewAgentInterface("https://agent.com", a2a.TransportProtocolJSONRPC)

			client, err := NewFromEndpoints(t.Context(), []*a2a.AgentInterface{iface}, opts...)
			if err != nil {
				t.Fatalf("NewFromEndpoints() error = %v", err)
			}
			if client.plugins != tc.want {
				t.Fatalf("NewFromEndpoints() client.plugins = %v, want %v", client.plugins, tc.want)
			}
		})
	}
}

func TestPluginUnaryHooks(t *testing.T) {
	t.Parallel()

	rejected := errors.New("rejected")
	transportErr := errors.New("boom")

	tests := []struct {
		name         string
		beforeErr    error
		transportErr error
		wantErr      error
		wantLog      []string
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
			name:         "transport error reaches final after",
			transportErr: transportErr,
			wantErr:      transportErr,
			wantLog:      []string{"p:before:GetTask", "p:after-final(err=boom)"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var log []string
			called := false
			transport := &testTransport{
				GetTaskFn: func(context.Context, ServiceParams, *a2a.GetTaskRequest) (*a2a.Task, error) {
					called = true
					return &a2a.Task{}, tc.transportErr
				},
			}
			plugin := &recordingClientPlugin{name: "p", log: &log, beforeErr: tc.beforeErr}
			client := newPluginTestClient(transport, plugin)

			_, err := client.GetTask(t.Context(), &a2a.GetTaskRequest{ID: "t1"})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("client.GetTask() error = %v, want %v", err, tc.wantErr)
			}
			if tc.beforeErr != nil && called {
				t.Fatalf("client.GetTask() called the transport despite plugin rejection")
			}
			if diff := cmp.Diff(tc.wantLog, log); diff != "" {
				t.Fatalf("client.GetTask() wrong plugin log (-want +got) diff = %s", diff)
			}
		})
	}
}

func TestPluginModifiesServiceParamsAndContext(t *testing.T) {
	t.Parallel()

	type ctxKey struct{}
	var log []string
	plugin := &recordingClientPlugin{
		name: "p",
		log:  &log,
		beforeFn: func(ctx context.Context, req *ClientRequest) context.Context {
			req.ServiceParams.Append("x-tenant", "acme")
			return context.WithValue(ctx, ctxKey{}, "set-by-plugin")
		},
	}
	interceptor := &testInterceptor{}

	var gotParams ServiceParams
	var gotCtxValue string
	transport := &testTransport{
		GetTaskFn: func(ctx context.Context, params ServiceParams, _ *a2a.GetTaskRequest) (*a2a.Task, error) {
			gotParams = params
			gotCtxValue, _ = ctx.Value(ctxKey{}).(string)
			return &a2a.Task{}, nil
		},
	}
	client := newPluginTestClient(transport, plugin, interceptor)

	if _, err := client.GetTask(t.Context(), &a2a.GetTaskRequest{ID: "t1"}); err != nil {
		t.Fatalf("client.GetTask() error = %v", err)
	}
	if diff := cmp.Diff([]string{"acme"}, gotParams.Get("x-tenant")); diff != "" {
		t.Fatalf("transport service params wrong (-want +got) diff = %s", diff)
	}
	if diff := cmp.Diff([]string{"acme"}, interceptor.lastReq.ServiceParams.Get("x-tenant")); diff != "" {
		t.Fatalf("interceptor service params wrong (-want +got) diff = %s", diff)
	}
	if gotCtxValue != "set-by-plugin" {
		t.Fatalf("transport ctx value = %q, want %q", gotCtxValue, "set-by-plugin")
	}
}

func TestPluginPayloadTypeChangeFailsCall(t *testing.T) {
	t.Parallel()

	var log []string
	plugin := &recordingClientPlugin{
		name: "p",
		log:  &log,
		beforeFn: func(ctx context.Context, req *ClientRequest) context.Context {
			req.Payload = &a2a.ListTasksRequest{}
			return ctx
		},
	}
	client := newPluginTestClient(&testTransport{}, plugin)

	_, err := client.GetTask(t.Context(), &a2a.GetTaskRequest{ID: "t1"})
	if err == nil {
		t.Fatalf("client.GetTask() error = nil, want payload type error")
	}
	want := []string{"p:before:GetTask", "p:after-final(err=" + err.Error() + ")"}
	if diff := cmp.Diff(want, log); diff != "" {
		t.Fatalf("client.GetTask() wrong plugin log (-want +got) diff = %s", diff)
	}
}

func TestPluginSkipped(t *testing.T) {
	t.Parallel()

	var log []string
	tests := []struct {
		name   string
		plugin ClientPlugin
	}{
		{name: "nil plugin", plugin: nil},
		{name: "plugin reports no client hooks", plugin: &noClientHooksPlugin{recordingClientPlugin{name: "p", log: &log}}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			transport := &testTransport{
				GetTaskFn: func(context.Context, ServiceParams, *a2a.GetTaskRequest) (*a2a.Task, error) {
					return &a2a.Task{}, nil
				},
			}
			client := newPluginTestClient(transport, tc.plugin)
			if _, err := client.GetTask(t.Context(), &a2a.GetTaskRequest{ID: "t1"}); err != nil {
				t.Fatalf("client.GetTask() error = %v", err)
			}
			if len(log) != 0 {
				t.Fatalf("client.GetTask() plugin log = %v, want empty", log)
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
			events:  makeEventSeq2([]a2a.Event{&a2a.Task{ID: "t1"}, &a2a.Task{ID: "t2"}}),
			wantLog: []string{"p:before:SendStreamingMessage", "p:after-event", "p:after-event", "p:after-final"},
		},
		{
			name:    "caller break still fires final",
			events:  makeEventSeq2([]a2a.Event{&a2a.Task{ID: "t1"}, &a2a.Task{ID: "t2"}, &a2a.Task{ID: "t3"}}),
			breakAt: 2,
			wantLog: []string{"p:before:SendStreamingMessage", "p:after-event", "p:after-event", "p:after-final"},
		},
		{
			name: "transport error reaches final",
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
			events:    makeEventSeq2([]a2a.Event{&a2a.Task{ID: "t1"}}),
			beforeErr: rejected,
			wantErr:   rejected,
			wantLog:   []string{"p:before:SendStreamingMessage"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var log []string
			transport := &testTransport{
				SendStreamingMessageFn: func(context.Context, ServiceParams, *a2a.SendMessageRequest) iter.Seq2[a2a.Event, error] {
					return tc.events
				},
			}
			client := newPluginTestClient(transport, &recordingClientPlugin{name: "p", log: &log, beforeErr: tc.beforeErr})

			var gotErr error
			seen := 0
			for _, err := range client.SendStreamingMessage(t.Context(), newSendMessageRequest()) {
				if err != nil {
					gotErr = err
				}
				seen++
				if seen == tc.breakAt {
					break
				}
			}
			if !errors.Is(gotErr, tc.wantErr) {
				t.Fatalf("client.SendStreamingMessage() error = %v, want %v", gotErr, tc.wantErr)
			}
			if diff := cmp.Diff(tc.wantLog, log); diff != "" {
				t.Fatalf("client.SendStreamingMessage() wrong plugin log (-want +got) diff = %s", diff)
			}
		})
	}
}

func TestPluginWrapsOutsideInterceptors(t *testing.T) {
	t.Parallel()

	var log []string
	interceptor := &testInterceptor{
		BeforeFn: func(ctx context.Context, _ *Request) (context.Context, any, error) {
			log = append(log, "ic:before")
			return ctx, nil, nil
		},
		AfterFn: func(context.Context, *Response) error {
			log = append(log, "ic:after")
			return nil
		},
	}
	transport := &testTransport{
		GetTaskFn: func(context.Context, ServiceParams, *a2a.GetTaskRequest) (*a2a.Task, error) {
			return &a2a.Task{}, nil
		},
	}
	client := newPluginTestClient(transport, &recordingClientPlugin{name: "pl", log: &log}, interceptor)

	if _, err := client.GetTask(t.Context(), &a2a.GetTaskRequest{ID: "t1"}); err != nil {
		t.Fatalf("client.GetTask() error = %v", err)
	}
	want := []string{"pl:before:GetTask", "ic:before", "ic:after", "pl:after-final"}
	if diff := cmp.Diff(want, log); diff != "" {
		t.Fatalf("client.GetTask() wrong ordering (-want +got) diff = %s", diff)
	}
}

func TestPluginOtherMethods(t *testing.T) {
	t.Parallel()

	transport := &testTransport{
		DeleteTaskPushConfigFn: func(context.Context, ServiceParams, *a2a.DeleteTaskPushConfigRequest) error {
			return nil
		},
		SubscribeToTaskFn: func(context.Context, ServiceParams, *a2a.SubscribeToTaskRequest) iter.Seq2[a2a.Event, error] {
			return makeEventSeq2([]a2a.Event{&a2a.Task{ID: "t1"}})
		},
	}
	tests := []struct {
		name string
		call func(c *Client) error
		want []string
	}{
		{
			name: "DeleteTaskPushConfig",
			call: func(c *Client) error {
				return c.DeleteTaskPushConfig(t.Context(), &a2a.DeleteTaskPushConfigRequest{TaskID: "t1"})
			},
			want: []string{"p:before:DeleteTaskPushConfig", "p:after-final"},
		},
		{
			name: "SubscribeToTask",
			call: func(c *Client) error {
				for _, err := range c.SubscribeToTask(t.Context(), &a2a.SubscribeToTaskRequest{ID: "t1"}) {
					if err != nil {
						return err
					}
				}
				return nil
			},
			want: []string{"p:before:SubscribeToTask", "p:after-event", "p:after-final"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var log []string
			client := newPluginTestClient(transport, &recordingClientPlugin{name: "p", log: &log})

			if err := tc.call(client); err != nil {
				t.Fatalf("client.%s() error = %v", tc.name, err)
			}
			if diff := cmp.Diff(tc.want, log); diff != "" {
				t.Fatalf("client.%s() wrong plugin log (-want +got) diff = %s", tc.name, diff)
			}
		})
	}
}
