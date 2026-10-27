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
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

// recordingPlugin records every hook invocation it receives in a shared log
// prefixed with its name, which lets tests assert ordering between plugins and
// across before/after hooks.
type recordingPlugin struct {
	name string
	log  *[]string

	beforeServerErr    error
	afterServerErr     error
	beforeClientErr    error
	afterClientErr     error
	beforeExecutionErr error
	afterExecutionErr  error
}

func newRecordingPlugin(name string, log *[]string) *recordingPlugin {
	return &recordingPlugin{name: name, log: log}
}

func (p *recordingPlugin) Name() string { return p.name }

func (p *recordingPlugin) BeforeHandlerRun(ctx context.Context, _ *a2asrv.ServerRequest) (context.Context, error) {
	*p.log = append(*p.log, p.name+":before-handler")
	return ctx, p.beforeServerErr
}

func (p *recordingPlugin) AfterHandlerRun(ctx context.Context, resp *a2asrv.ServerResponse) (context.Context, error) {
	entry := p.name + ":after-handler"
	if resp.Err != nil {
		entry += "(err=" + resp.Err.Error() + ")"
	}
	*p.log = append(*p.log, entry)
	return ctx, p.afterServerErr
}

func (p *recordingPlugin) BeforeClientRequest(ctx context.Context, _ *a2aclient.ClientRequest) (context.Context, error) {
	*p.log = append(*p.log, p.name+":before-client")
	return ctx, p.beforeClientErr
}

func (p *recordingPlugin) AfterClientRequest(ctx context.Context, resp *a2aclient.ClientResponse) (context.Context, error) {
	entry := p.name + ":after-client"
	if resp.Err != nil {
		entry += "(err=" + resp.Err.Error() + ")"
	}
	*p.log = append(*p.log, entry)
	return ctx, p.afterClientErr
}

func (p *recordingPlugin) BeforeExecution(ctx context.Context, _ *a2asrv.ExecutorContext) (context.Context, error) {
	*p.log = append(*p.log, p.name+":before-execution")
	return ctx, p.beforeExecutionErr
}

func (p *recordingPlugin) OnExecutionEvent(context.Context, *a2asrv.ExecutorContext, a2a.Event) error {
	*p.log = append(*p.log, p.name+":execution-event")
	return errors.New("event hook error is swallowed")
}

func (p *recordingPlugin) AfterExecution(_ context.Context, _ *a2asrv.ExecutorContext, cause error) error {
	entry := p.name + ":after-execution"
	if cause != nil {
		entry += "(err=" + cause.Error() + ")"
	}
	*p.log = append(*p.log, entry)
	return p.afterExecutionErr
}

func (p *recordingPlugin) OnTaskEvent(context.Context, *a2asrv.TaskEventInfo) error {
	*p.log = append(*p.log, p.name+":task-event")
	return errors.New("task hook error is swallowed")
}

// bareExecutionPlugin implements only ExecutionPlugin.
type bareExecutionPlugin struct{ name string }

func (p bareExecutionPlugin) Name() string { return p.name }

func (p bareExecutionPlugin) BeforeExecution(ctx context.Context, _ *a2asrv.ExecutorContext) (context.Context, error) {
	return ctx, nil
}

func (p bareExecutionPlugin) OnExecutionEvent(context.Context, *a2asrv.ExecutorContext, a2a.Event) error {
	return nil
}

func (p bareExecutionPlugin) AfterExecution(context.Context, *a2asrv.ExecutorContext, error) error {
	return nil
}

// serverOnlyPlugin satisfies [a2asrv.ServerPlugin] but not ClientPlugin.
type serverOnlyPlugin struct {
	a2aplugin.Passthrough
	name string
}

func (p serverOnlyPlugin) Name() string { return p.name }

func (p serverOnlyPlugin) BeforeClientRequest(context.Context, *a2aclient.ClientRequest) (context.Context, error) {
	panic("serverOnlyPlugin should not be treated as a ClientPlugin by Sequential")
}

// bareServerPlugin implements ServerPlugin without embedding Passthrough, so
// it does not accidentally satisfy ClientPlugin.
type bareServerPlugin struct{ name string }

func (p bareServerPlugin) Name() string { return p.name }

func (p bareServerPlugin) BeforeHandlerRun(ctx context.Context, _ *a2asrv.ServerRequest) (context.Context, error) {
	return ctx, nil
}

func (p bareServerPlugin) AfterHandlerRun(ctx context.Context, _ *a2asrv.ServerResponse) (context.Context, error) {
	return ctx, nil
}

// bareClientPlugin mirrors [bareServerPlugin] for ClientPlugin.
type bareClientPlugin struct{ name string }

func (p bareClientPlugin) Name() string { return p.name }

func (p bareClientPlugin) BeforeClientRequest(ctx context.Context, _ *a2aclient.ClientRequest) (context.Context, error) {
	return ctx, nil
}

func (p bareClientPlugin) AfterClientRequest(ctx context.Context, _ *a2aclient.ClientResponse) (context.Context, error) {
	return ctx, nil
}

func TestSequentialHasHooks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		plugins        []a2aplugin.Plugin
		wantServerHook bool
		wantClientHook bool
	}{
		{
			name:           "no plugins",
			plugins:        nil,
			wantServerHook: false,
			wantClientHook: false,
		},
		{
			name:           "server-only plugin",
			plugins:        []a2aplugin.Plugin{bareServerPlugin{name: "s"}},
			wantServerHook: true,
			wantClientHook: false,
		},
		{
			name:           "client-only plugin",
			plugins:        []a2aplugin.Plugin{bareClientPlugin{name: "c"}},
			wantServerHook: false,
			wantClientHook: true,
		},
		{
			name:           "complete plugin via Passthrough embedding",
			plugins:        []a2aplugin.Plugin{serverOnlyPlugin{name: "cp"}},
			wantServerHook: true,
			wantClientHook: true,
		},
		{
			name: "mix of server and client plugins",
			plugins: []a2aplugin.Plugin{
				bareServerPlugin{name: "s"},
				bareClientPlugin{name: "c"},
			},
			wantServerHook: true,
			wantClientHook: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := a2aplugin.Sequential(tc.plugins...)

			if got := m.HasServerHooks(); got != tc.wantServerHook {
				t.Errorf("Sequential(...).HasServerHooks() = %v, want %v", got, tc.wantServerHook)
			}
			if got := m.HasClientHooks(); got != tc.wantClientHook {
				t.Errorf("Sequential(...).HasClientHooks() = %v, want %v", got, tc.wantClientHook)
			}
		})
	}
}

func TestSequentialNestedManagerShortCircuit(t *testing.T) {
	t.Parallel()

	empty := a2aplugin.Sequential()
	outer := a2aplugin.Sequential(empty)

	if outer.HasServerHooks() {
		t.Errorf("outer.HasServerHooks() = true, want false (nested empty manager must not contribute)")
	}
	if outer.HasClientHooks() {
		t.Errorf("outer.HasClientHooks() = true, want false (nested empty manager must not contribute)")
	}

	withServer := a2aplugin.Sequential(bareServerPlugin{name: "inner"})
	outerWithServer := a2aplugin.Sequential(withServer)
	if !outerWithServer.HasServerHooks() {
		t.Errorf("outerWithServer.HasServerHooks() = false, want true (nested manager with server plugin must contribute)")
	}
	if outerWithServer.HasClientHooks() {
		t.Errorf("outerWithServer.HasClientHooks() = true, want false (nested manager has no client plugin)")
	}
}

func TestSequentialBeforeHandlerRunOrder(t *testing.T) {
	t.Parallel()

	var log []string
	a := newRecordingPlugin("a", &log)
	b := newRecordingPlugin("b", &log)
	c := newRecordingPlugin("c", &log)

	m := a2aplugin.Sequential(a, b, c)

	ctx, err := m.BeforeHandlerRun(t.Context(), &a2asrv.ServerRequest{Method: "SendMessage"})
	if err != nil {
		t.Fatalf("BeforeHandlerRun() error = %v, want nil", err)
	}
	if ctx == nil {
		t.Fatalf("BeforeHandlerRun() returned nil context, want non-nil")
	}

	want := []string{"a:before-handler", "b:before-handler", "c:before-handler"}
	if diff := cmp.Diff(want, log); diff != "" {
		t.Errorf("BeforeHandlerRun() wrong invocation log (-want +got) diff = %s", diff)
	}
}

func TestSequentialAfterHandlerRunReverseOrder(t *testing.T) {
	t.Parallel()

	var log []string
	a := newRecordingPlugin("a", &log)
	b := newRecordingPlugin("b", &log)
	c := newRecordingPlugin("c", &log)

	m := a2aplugin.Sequential(a, b, c)

	if _, err := m.AfterHandlerRun(t.Context(), &a2asrv.ServerResponse{Method: "SendMessage", IsFinal: true}); err != nil {
		t.Fatalf("AfterHandlerRun() error = %v, want nil", err)
	}

	want := []string{"c:after-handler", "b:after-handler", "a:after-handler"}
	if diff := cmp.Diff(want, log); diff != "" {
		t.Errorf("AfterHandlerRun() wrong invocation log (-want +got) diff = %s", diff)
	}
}

func TestSequentialBeforeHandlerRunUnwindsOnError(t *testing.T) {
	t.Parallel()

	var log []string
	a := newRecordingPlugin("a", &log)
	b := newRecordingPlugin("b", &log)
	b.beforeServerErr = errors.New("rejected by b")
	c := newRecordingPlugin("c", &log)

	m := a2aplugin.Sequential(a, b, c)

	_, err := m.BeforeHandlerRun(t.Context(), &a2asrv.ServerRequest{Method: "SendMessage"})
	if err == nil || err.Error() != "rejected by b" {
		t.Fatalf("BeforeHandlerRun() error = %v, want %q", err, "rejected by b")
	}

	want := []string{
		"a:before-handler",
		"b:before-handler",
		"a:after-handler(err=rejected by b)",
	}
	if diff := cmp.Diff(want, log); diff != "" {
		t.Errorf("BeforeHandlerRun() wrong invocation log (-want +got) diff = %s", diff)
	}
}

func TestSequentialAfterHandlerRunSwallowsErrors(t *testing.T) {
	t.Parallel()

	var log []string
	a := newRecordingPlugin("a", &log)
	a.afterServerErr = errors.New("boom-a")
	b := newRecordingPlugin("b", &log)
	b.afterServerErr = errors.New("boom-b")

	m := a2aplugin.Sequential(a, b)

	if _, err := m.AfterHandlerRun(t.Context(), &a2asrv.ServerResponse{Method: "SendMessage", IsFinal: true}); err != nil {
		t.Fatalf("AfterHandlerRun() error = %v, want nil (errors are logged and swallowed)", err)
	}

	want := []string{"b:after-handler", "a:after-handler"}
	if diff := cmp.Diff(want, log); diff != "" {
		t.Errorf("AfterHandlerRun() wrong invocation log (-want +got) diff = %s", diff)
	}
}

func TestSequentialBeforeHandlerRunPropagatesContext(t *testing.T) {
	t.Parallel()

	type ctxKey struct{ k string }
	key := ctxKey{"stage"}

	a := &ctxMutatingPlugin{name: "a", key: key, value: "a-wrote"}
	b := &ctxMutatingPlugin{name: "b", key: key, want: "a-wrote", value: "b-wrote"}

	m := a2aplugin.Sequential(a, b)
	ctx, err := m.BeforeHandlerRun(t.Context(), &a2asrv.ServerRequest{Method: "SendMessage"})
	if err != nil {
		t.Fatalf("BeforeHandlerRun() error = %v, want nil", err)
	}
	if got, _ := ctx.Value(key).(string); got != "b-wrote" {
		t.Errorf("BeforeHandlerRun() ctx[%v] = %q, want %q", key, got, "b-wrote")
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

func TestSequentialBeforeClientRequestOrderAndUnwind(t *testing.T) {
	t.Parallel()

	var log []string
	a := newRecordingPlugin("a", &log)
	b := newRecordingPlugin("b", &log)
	b.beforeClientErr = errors.New("rejected by b")
	c := newRecordingPlugin("c", &log)

	m := a2aplugin.Sequential(a, b, c)

	_, err := m.BeforeClientRequest(t.Context(), &a2aclient.ClientRequest{Method: "SendMessage"})
	if err == nil || err.Error() != "rejected by b" {
		t.Fatalf("BeforeClientRequest() error = %v, want %q", err, "rejected by b")
	}

	want := []string{
		"a:before-client",
		"b:before-client",
		"a:after-client(err=rejected by b)",
	}
	if diff := cmp.Diff(want, log); diff != "" {
		t.Errorf("BeforeClientRequest() wrong invocation log (-want +got) diff = %s", diff)
	}
}

func TestSequentialAfterClientRequestReverseOrder(t *testing.T) {
	t.Parallel()

	var log []string
	a := newRecordingPlugin("a", &log)
	b := newRecordingPlugin("b", &log)
	c := newRecordingPlugin("c", &log)

	m := a2aplugin.Sequential(a, b, c)

	if _, err := m.AfterClientRequest(t.Context(), &a2aclient.ClientResponse{Method: "SendMessage", IsFinal: true}); err != nil {
		t.Fatalf("AfterClientRequest() error = %v, want nil", err)
	}

	want := []string{"c:after-client", "b:after-client", "a:after-client"}
	if diff := cmp.Diff(want, log); diff != "" {
		t.Errorf("AfterClientRequest() wrong invocation log (-want +got) diff = %s", diff)
	}
}

func TestSequentialPluginsReturnsCopy(t *testing.T) {
	t.Parallel()

	a := bareServerPlugin{name: "a"}
	m := a2aplugin.Sequential(a)

	got := m.Plugins()
	if len(got) != 1 || got[0].Name() != "a" {
		t.Fatalf("Sequential(a).Plugins() = %v, want [a]", got)
	}

	got[0] = bareServerPlugin{name: "mutated"}
	again := m.Plugins()
	if again[0].Name() != "a" {
		t.Errorf("Sequential(a).Plugins() returned a shared slice; got[0] mutation leaked: %v", again)
	}
}

func TestSequentialServerHookSkipsClientOnlyPlugins(t *testing.T) {
	t.Parallel()

	var log []string
	server := newRecordingPlugin("server", &log)
	m := a2aplugin.Sequential(bareClientPlugin{name: "client-only"}, server)

	if _, err := m.BeforeHandlerRun(t.Context(), &a2asrv.ServerRequest{Method: "GetTask"}); err != nil {
		t.Fatalf("BeforeHandlerRun() error = %v, want nil", err)
	}

	want := []string{"server:before-handler"}
	if diff := cmp.Diff(want, log); diff != "" {
		t.Errorf("BeforeHandlerRun() wrong invocation log (-want +got) diff = %s", diff)
	}
}

func TestSequentialWithRequestHandlerUnwindsOnRejection(t *testing.T) {
	t.Parallel()

	var log []string
	a := newRecordingPlugin("a", &log)
	b := newRecordingPlugin("b", &log)
	b.beforeServerErr = errors.New("rejected by b")
	handler := a2asrv.NewHandler(nil, a2asrv.WithPlugins(a2aplugin.Sequential(a, b)))

	if _, err := handler.GetTask(t.Context(), &a2a.GetTaskRequest{ID: "x"}); !errors.Is(err, b.beforeServerErr) {
		t.Fatalf("GetTask() error = %v, want %v", err, b.beforeServerErr)
	}

	want := []string{"a:before-handler", "b:before-handler", "a:after-handler(err=rejected by b)"}
	if diff := cmp.Diff(want, log); diff != "" {
		t.Fatalf("GetTask() wrong plugin log (-want +got) diff = %s", diff)
	}
}

func TestGlobalDefaultsToEmptyManager(t *testing.T) {
	m := a2aplugin.Global()
	if m == nil {
		t.Fatalf("Global() = nil, want a non-nil empty manager")
	}
	if m.HasServerHooks() {
		t.Errorf("Global().HasServerHooks() = true, want false (empty default)")
	}
	if m.HasClientHooks() {
		t.Errorf("Global().HasClientHooks() = true, want false (empty default)")
	}
}

func TestSetGlobalInstallsManager(t *testing.T) {
	t.Cleanup(func() { a2aplugin.SetGlobal(a2aplugin.Sequential()) })

	installed := a2aplugin.Sequential(bareServerPlugin{name: "s"})
	a2aplugin.SetGlobal(installed)

	got := a2aplugin.Global()
	if !got.HasServerHooks() {
		t.Errorf("Global().HasServerHooks() = false, want true after SetGlobal with a server plugin")
	}
}

func TestSetGlobalReplacesPreviousManager(t *testing.T) {
	t.Cleanup(func() { a2aplugin.SetGlobal(a2aplugin.Sequential()) })

	a2aplugin.SetGlobal(a2aplugin.Sequential(bareServerPlugin{name: "s"}))
	a2aplugin.SetGlobal(a2aplugin.Sequential(bareClientPlugin{name: "c"}))

	got := a2aplugin.Global()
	if got.HasServerHooks() {
		t.Errorf("Global().HasServerHooks() = true, want false after replacement with a client-only manager")
	}
	if !got.HasClientHooks() {
		t.Errorf("Global().HasClientHooks() = false, want true after replacement with a client-only manager")
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
			t.Errorf("SetGlobal(nil) did not panic, want panic")
		}
	}()
	a2aplugin.SetGlobal(nil)
}

func TestSequentialHasExecutionHooks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		plugins []a2aplugin.Plugin
		want    bool
	}{
		{name: "no plugins", want: false},
		{name: "server-only plugin", plugins: []a2aplugin.Plugin{bareServerPlugin{name: "s"}}, want: false},
		{name: "execution-only plugin", plugins: []a2aplugin.Plugin{bareExecutionPlugin{name: "e"}}, want: true},
		{name: "Passthrough embedding", plugins: []a2aplugin.Plugin{serverOnlyPlugin{name: "p"}}, want: true},
		{name: "nested empty manager", plugins: []a2aplugin.Plugin{a2aplugin.Sequential()}, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := a2aplugin.Sequential(tc.plugins...).HasExecutionHooks(); got != tc.want {
				t.Fatalf("Sequential(...).HasExecutionHooks() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSequentialExecutionHooks(t *testing.T) {
	t.Parallel()

	var log []string
	a := newRecordingPlugin("a", &log)
	b := newRecordingPlugin("b", &log)
	b.afterExecutionErr = errors.New("after error is swallowed")
	m := a2aplugin.Sequential(a, b)
	execCtx := &a2asrv.ExecutorContext{}
	cause := errors.New("agent failed")

	if _, err := m.BeforeExecution(t.Context(), execCtx); err != nil {
		t.Fatalf("BeforeExecution() error = %v", err)
	}
	if err := m.OnExecutionEvent(t.Context(), execCtx, &a2a.Task{}); err != nil {
		t.Fatalf("OnExecutionEvent() error = %v", err)
	}
	if err := m.AfterExecution(t.Context(), execCtx, cause); err != nil {
		t.Fatalf("AfterExecution() error = %v", err)
	}

	want := []string{
		"a:before-execution",
		"b:before-execution",
		"a:execution-event",
		"b:execution-event",
		"b:after-execution(err=agent failed)",
		"a:after-execution(err=agent failed)",
	}
	if diff := cmp.Diff(want, log); diff != "" {
		t.Fatalf("Sequential execution hooks wrong log (-want +got) diff = %s", diff)
	}
}

func TestSequentialBeforeExecutionUnwindsOnError(t *testing.T) {
	t.Parallel()

	var log []string
	a := newRecordingPlugin("a", &log)
	b := newRecordingPlugin("b", &log)
	b.beforeExecutionErr = errors.New("rejected by b")
	c := newRecordingPlugin("c", &log)

	_, err := a2aplugin.Sequential(a, b, c).BeforeExecution(t.Context(), &a2asrv.ExecutorContext{})
	if !errors.Is(err, b.beforeExecutionErr) {
		t.Fatalf("BeforeExecution() error = %v, want %v", err, b.beforeExecutionErr)
	}

	want := []string{"a:before-execution", "b:before-execution", "a:after-execution(err=rejected by b)"}
	if diff := cmp.Diff(want, log); diff != "" {
		t.Fatalf("BeforeExecution() wrong log (-want +got) diff = %s", diff)
	}
}

func TestSequentialTaskHooks(t *testing.T) {
	t.Parallel()

	var log []string
	m := a2aplugin.Sequential(
		newRecordingPlugin("a", &log),
		bareServerPlugin{name: "server-only"},
		newRecordingPlugin("b", &log),
	)

	if !m.HasTaskHooks() {
		t.Fatalf("Sequential(...).HasTaskHooks() = false, want true")
	}
	if err := m.OnTaskEvent(t.Context(), &a2asrv.TaskEventInfo{}); err != nil {
		t.Fatalf("OnTaskEvent() error = %v, want nil", err)
	}
	if diff := cmp.Diff([]string{"a:task-event", "b:task-event"}, log); diff != "" {
		t.Fatalf("OnTaskEvent() wrong log (-want +got) diff = %s", diff)
	}
	if a2aplugin.Sequential(bareServerPlugin{name: "s"}).HasTaskHooks() {
		t.Fatalf("Sequential(server-only).HasTaskHooks() = true, want false")
	}
}
