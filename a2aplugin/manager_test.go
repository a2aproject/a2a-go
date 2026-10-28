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

package a2aplugin_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aplugin"
	"github.com/a2aproject/a2a-go/v2/a2aplugin/core"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/a2asrv/push"
)

var errAfterHook = errors.New("after hook error is swallowed")

// recordingPlugin implements every hook and records its invocations in a shared log, which lets
// tests assert ordering between plugins. After and observer hooks always fail, so every test
// also checks that their errors are swallowed.
type recordingPlugin struct {
	name string
	log  *[]string

	beforeServerErr    error
	beforeClientErr    error
	beforeExecutionErr error
	beforePushErr      error
}

func newRecordingPlugin(name string, log *[]string) *recordingPlugin {
	return &recordingPlugin{name: name, log: log}
}

func (p *recordingPlugin) record(hook string, err error) {
	entry := p.name + ":" + hook
	if err != nil {
		entry += "(err=" + err.Error() + ")"
	}
	*p.log = append(*p.log, entry)
}

func (p *recordingPlugin) Name() string { return p.name }

func (p *recordingPlugin) BeforeHandlerRun(ctx context.Context, _ *a2asrv.ServerRequest) (context.Context, error) {
	p.record("before-handler", nil)
	return ctx, p.beforeServerErr
}

func (p *recordingPlugin) AfterHandlerRun(ctx context.Context, resp *a2asrv.ServerResponse) (context.Context, error) {
	p.record("after-handler", resp.Err)
	return ctx, errAfterHook
}

func (p *recordingPlugin) BeforeClientRequest(ctx context.Context, _ *a2aclient.ClientRequest) (context.Context, error) {
	p.record("before-client", nil)
	return ctx, p.beforeClientErr
}

func (p *recordingPlugin) AfterClientRequest(ctx context.Context, resp *a2aclient.ClientResponse) (context.Context, error) {
	p.record("after-client", resp.Err)
	return ctx, errAfterHook
}

func (p *recordingPlugin) BeforeExecution(ctx context.Context, _ *a2asrv.ExecutorContext) (context.Context, error) {
	p.record("before-execution", nil)
	return ctx, p.beforeExecutionErr
}

func (p *recordingPlugin) OnExecutionEvent(context.Context, *a2asrv.ExecutorContext, a2a.Event) error {
	p.record("execution-event", nil)
	return errAfterHook
}

func (p *recordingPlugin) AfterExecution(_ context.Context, _ *a2asrv.ExecutorContext, cause error) error {
	p.record("after-execution", cause)
	return errAfterHook
}

func (p *recordingPlugin) OnTaskEvent(context.Context, *a2asrv.TaskEventInfo) error {
	p.record("task-event", nil)
	return errAfterHook
}

func (p *recordingPlugin) BeforePushSend(ctx context.Context, _ *push.Delivery) (context.Context, error) {
	p.record("before-push", nil)
	return ctx, p.beforePushErr
}

func (p *recordingPlugin) OnPushSent(_ context.Context, r *push.DeliveryResult) error {
	p.record("push-sent", r.Err)
	return errAfterHook
}

type passthroughPlugin struct{ a2aplugin.Passthrough }

type bareServerPlugin struct{ name string }

func (p bareServerPlugin) Name() string { return p.name }

func (p bareServerPlugin) BeforeHandlerRun(ctx context.Context, _ *a2asrv.ServerRequest) (context.Context, error) {
	return ctx, nil
}

func (p bareServerPlugin) AfterHandlerRun(ctx context.Context, _ *a2asrv.ServerResponse) (context.Context, error) {
	return ctx, nil
}

type bareClientPlugin struct{ name string }

func (p bareClientPlugin) Name() string { return p.name }

func (p bareClientPlugin) BeforeClientRequest(ctx context.Context, _ *a2aclient.ClientRequest) (context.Context, error) {
	return ctx, nil
}

func (p bareClientPlugin) AfterClientRequest(ctx context.Context, _ *a2aclient.ClientResponse) (context.Context, error) {
	return ctx, nil
}

type hookKinds struct {
	Server, Client, Execution, Task, Push bool
}

func hooksOf(m a2aplugin.Manager) hookKinds {
	return hookKinds{
		Server:    m.HasHooks(core.ServerHooks),
		Client:    m.HasHooks(core.ClientHooks),
		Execution: m.HasHooks(core.ExecutionHooks),
		Task:      m.HasHooks(core.TaskHooks),
		Push:      m.HasHooks(core.PushHooks),
	}
}

func TestSequentialHasHooks(t *testing.T) {
	t.Parallel()

	all := hookKinds{Server: true, Client: true, Execution: true, Task: true, Push: true}
	tests := []struct {
		name    string
		plugins []a2aplugin.Plugin
		want    hookKinds
	}{
		{name: "no plugins", want: hookKinds{}},
		{name: "server-only plugin", plugins: []a2aplugin.Plugin{bareServerPlugin{"s"}}, want: hookKinds{Server: true}},
		{name: "client-only plugin", plugins: []a2aplugin.Plugin{bareClientPlugin{"c"}}, want: hookKinds{Client: true}},
		{name: "server and client plugins", plugins: []a2aplugin.Plugin{bareServerPlugin{"s"}, bareClientPlugin{"c"}}, want: hookKinds{Server: true, Client: true}},
		{name: "Passthrough embedding", plugins: []a2aplugin.Plugin{passthroughPlugin{}}, want: all},
		{name: "nested empty manager", plugins: []a2aplugin.Plugin{a2aplugin.Sequential()}, want: hookKinds{}},
		{name: "nested server manager", plugins: []a2aplugin.Plugin{a2aplugin.Sequential(bareServerPlugin{"s"})}, want: hookKinds{Server: true}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if diff := cmp.Diff(tc.want, hooksOf(a2aplugin.Sequential(tc.plugins...))); diff != "" {
				t.Fatalf("Sequential(...) wrong Has*Hooks (-want +got) diff = %s", diff)
			}
		})
	}
}

func TestSequentialHooks(t *testing.T) {
	t.Parallel()

	rejected := errors.New("rejected")
	hooks := []struct {
		name string
		// failBefore makes the plugin's Before hook fail. It is nil for hook types without one.
		failBefore  func(p *recordingPlugin)
		run         func(ctx context.Context, m a2aplugin.Manager) error
		wantSuccess []string
		wantFailure []string
	}{
		{
			name:       "server",
			failBefore: func(p *recordingPlugin) { p.beforeServerErr = rejected },
			run: func(ctx context.Context, m a2aplugin.Manager) error {
				if _, err := m.BeforeHandlerRun(ctx, &a2asrv.ServerRequest{}); err != nil {
					return err
				}
				_, err := m.AfterHandlerRun(ctx, &a2asrv.ServerResponse{})
				return err
			},
			wantSuccess: []string{"a:before-handler", "b:before-handler", "b:after-handler", "a:after-handler"},
			wantFailure: []string{"a:before-handler", "b:before-handler", "a:after-handler(err=rejected)"},
		},
		{
			name:       "client",
			failBefore: func(p *recordingPlugin) { p.beforeClientErr = rejected },
			run: func(ctx context.Context, m a2aplugin.Manager) error {
				if _, err := m.BeforeClientRequest(ctx, &a2aclient.ClientRequest{}); err != nil {
					return err
				}
				_, err := m.AfterClientRequest(ctx, &a2aclient.ClientResponse{})
				return err
			},
			wantSuccess: []string{"a:before-client", "b:before-client", "b:after-client", "a:after-client"},
			wantFailure: []string{"a:before-client", "b:before-client", "a:after-client(err=rejected)"},
		},
		{
			name:       "execution",
			failBefore: func(p *recordingPlugin) { p.beforeExecutionErr = rejected },
			run: func(ctx context.Context, m a2aplugin.Manager) error {
				execCtx := &a2asrv.ExecutorContext{}
				if _, err := m.BeforeExecution(ctx, execCtx); err != nil {
					return err
				}
				if err := m.OnExecutionEvent(ctx, execCtx, &a2a.Task{}); err != nil {
					return err
				}
				return m.AfterExecution(ctx, execCtx, nil)
			},
			wantSuccess: []string{
				"a:before-execution", "b:before-execution",
				"a:execution-event", "b:execution-event",
				"b:after-execution", "a:after-execution",
			},
			wantFailure: []string{"a:before-execution", "b:before-execution", "a:after-execution(err=rejected)"},
		},
		{
			name: "task",
			run: func(ctx context.Context, m a2aplugin.Manager) error {
				return m.OnTaskEvent(ctx, &a2asrv.TaskEventInfo{})
			},
			wantSuccess: []string{"a:task-event", "b:task-event"},
		},
		{
			name:       "push",
			failBefore: func(p *recordingPlugin) { p.beforePushErr = rejected },
			run: func(ctx context.Context, m a2aplugin.Manager) error {
				delivery := &push.Delivery{}
				if _, err := m.BeforePushSend(ctx, delivery); err != nil {
					return err
				}
				return m.OnPushSent(ctx, &push.DeliveryResult{Delivery: delivery})
			},
			wantSuccess: []string{"a:before-push", "b:before-push", "a:push-sent", "b:push-sent"},
			wantFailure: []string{"a:before-push", "b:before-push", "a:push-sent(err=rejected)"},
		},
	}

	for _, hook := range hooks {
		for _, failing := range []bool{false, true} {
			if failing && hook.failBefore == nil {
				continue
			}
			name := hook.name + " success"
			want, wantErr := hook.wantSuccess, error(nil)
			if failing {
				name = hook.name + " before error unwinds earlier plugins"
				want, wantErr = hook.wantFailure, rejected
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				var log []string
				a, b, c := newRecordingPlugin("a", &log), newRecordingPlugin("b", &log), newRecordingPlugin("c", &log)
				plugins := []a2aplugin.Plugin{a, b}
				if failing {
					hook.failBefore(b)
					plugins = append(plugins, c)
				}

				if err := hook.run(t.Context(), a2aplugin.Sequential(plugins...)); !errors.Is(err, wantErr) {
					t.Fatalf("hook error = %v, want %v", err, wantErr)
				}
				if diff := cmp.Diff(want, log); diff != "" {
					t.Fatalf("hooks wrong log (-want +got) diff = %s", diff)
				}
			})
		}
	}
}

type ctxMutatingPlugin struct {
	a2aplugin.Passthrough
	name  string
	key   any
	want  string
	value string
}

func (p *ctxMutatingPlugin) Name() string { return p.name }

func (p *ctxMutatingPlugin) BeforeHandlerRun(ctx context.Context, _ *a2asrv.ServerRequest) (context.Context, error) {
	if p.want != "" {
		got, _ := ctx.Value(p.key).(string)
		if got != p.want {
			return ctx, fmt.Errorf("plugin %q: ctx[%v] = %q, want %q", p.name, p.key, got, p.want)
		}
	}
	return context.WithValue(ctx, p.key, p.value), nil
}

func TestSequentialBeforeHandlerRunPropagatesContext(t *testing.T) {
	t.Parallel()

	type ctxKey struct{}
	a := &ctxMutatingPlugin{name: "a", key: ctxKey{}, value: "a-wrote"}
	b := &ctxMutatingPlugin{name: "b", key: ctxKey{}, want: "a-wrote", value: "b-wrote"}

	ctx, err := a2aplugin.Sequential(a, b).BeforeHandlerRun(t.Context(), &a2asrv.ServerRequest{})
	if err != nil {
		t.Fatalf("BeforeHandlerRun() error = %v, want nil", err)
	}
	if got, _ := ctx.Value(ctxKey{}).(string); got != "b-wrote" {
		t.Fatalf("BeforeHandlerRun() ctx value = %q, want %q", got, "b-wrote")
	}
}

func TestSequentialPluginsReturnsCopy(t *testing.T) {
	t.Parallel()

	m := a2aplugin.Sequential(bareServerPlugin{name: "a"})
	got := m.Plugins()
	got[0] = bareServerPlugin{name: "mutated"}

	if again := m.Plugins(); again[0].Name() != "a" {
		t.Fatalf("Sequential(a).Plugins() returned a shared slice: %v", again)
	}
}

func TestGlobalDefaultsToEmptyManager(t *testing.T) {
	if diff := cmp.Diff(hookKinds{}, hooksOf(a2aplugin.Global())); diff != "" {
		t.Fatalf("Global() wrong Has*Hooks (-want +got) diff = %s", diff)
	}
}

func TestSetGlobal(t *testing.T) {
	t.Cleanup(func() { a2aplugin.SetGlobal(a2aplugin.Sequential()) })

	a2aplugin.SetGlobal(a2aplugin.Sequential(bareServerPlugin{name: "s"}))
	a2aplugin.SetGlobal(a2aplugin.Sequential(bareClientPlugin{name: "c"}))

	if diff := cmp.Diff(hookKinds{Client: true}, hooksOf(a2aplugin.Global())); diff != "" {
		t.Fatalf("Global() after SetGlobal wrong Has*Hooks (-want +got) diff = %s", diff)
	}
}

func TestNewHandlerUsesGlobalManager(t *testing.T) {
	t.Cleanup(func() { a2aplugin.SetGlobal(a2aplugin.Sequential()) })

	var log []string
	a2aplugin.SetGlobal(a2aplugin.Sequential(newRecordingPlugin("global", &log)))
	handler := a2asrv.NewHandler(nil)

	_, err := handler.GetTask(t.Context(), &a2a.GetTaskRequest{ID: "missing"})
	if err == nil {
		t.Fatalf("GetTask() error = nil, want task not found")
	}

	want := []string{"global:before-handler", "global:after-handler(err=" + err.Error() + ")"}
	if diff := cmp.Diff(want, log); diff != "" {
		t.Fatalf("GetTask() wrong plugin log (-want +got) diff = %s", diff)
	}
}

func TestSetGlobalPanicsOnNil(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatalf("SetGlobal(nil) did not panic, want panic")
		}
	}()
	a2aplugin.SetGlobal(nil)
}
