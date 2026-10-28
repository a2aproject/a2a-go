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
	"slices"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aplugin/core"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/a2asrv/push"
	"github.com/a2aproject/a2a-go/v2/log"
)

// Manager runs a group of registered plugins. It implements every plugin hook
// interface, so Managers can be nested.
// [core.HooksReporter] lets call sites skip plugin work when no registered
// plugin implements the hooks they would call.
type Manager interface {
	CompletePlugin
	a2asrv.ExecutionPlugin
	a2asrv.TaskObserverPlugin
	push.Plugin
	core.HooksReporter
}

// SetGlobal installs m as the process-wide plugin [Manager]. SDK components
// capture [Global] at construction time, so SetGlobal must be called once at
// startup, before those components are built. Panics if m is nil.
func SetGlobal(m Manager) {
	if m == nil {
		panic("a2aplugin: SetGlobal called with a nil Manager")
	}
	core.SetGlobal(m)
}

// Global returns the process-wide plugin [Manager]. If [SetGlobal] has not
// been called it returns an empty Sequential manager.
func Global() Manager {
	if m, ok := core.Global().(Manager); ok {
		return m
	}
	return emptyGlobal
}

var emptyGlobal Manager = Sequential()

// SequentialManager runs the registered plugins one after another. Before*
// hooks run in registration order and After* hooks run in reverse order.
type SequentialManager struct {
	plugins   []Plugin
	server    []a2asrv.ServerPlugin
	client    []a2aclient.ClientPlugin
	execution []a2asrv.ExecutionPlugin
	task      []a2asrv.TaskObserverPlugin
	pushes    []push.Plugin
}

// Sequential returns a [Manager] that runs the given plugins sequentially.
func Sequential(plugins ...Plugin) *SequentialManager {
	return &SequentialManager{
		plugins:   slices.Clone(plugins),
		server:    selectPlugins[a2asrv.ServerPlugin](plugins, core.ServerHooks),
		client:    selectPlugins[a2aclient.ClientPlugin](plugins, core.ClientHooks),
		execution: selectPlugins[a2asrv.ExecutionPlugin](plugins, core.ExecutionHooks),
		task:      selectPlugins[a2asrv.TaskObserverPlugin](plugins, core.TaskHooks),
		pushes:    selectPlugins[push.Plugin](plugins, core.PushHooks),
	}
}

var _ Manager = (*SequentialManager)(nil)

// Name implements [Plugin].
func (*SequentialManager) Name() string { return "sequential" }

// Plugins returns the registered plugins in registration order.
func (m *SequentialManager) Plugins() []Plugin { return slices.Clone(m.plugins) }

// HasHooks implements [core.HooksReporter].
func (m *SequentialManager) HasHooks(kind core.HookKind) bool {
	switch kind {
	case core.ServerHooks:
		return len(m.server) > 0
	case core.ClientHooks:
		return len(m.client) > 0
	case core.ExecutionHooks:
		return len(m.execution) > 0
	case core.TaskHooks:
		return len(m.task) > 0
	case core.PushHooks:
		return len(m.pushes) > 0
	}
	return false
}

// BeforeHandlerRun implements [a2asrv.ServerPlugin]. If a hook returns an error,
// AfterHandlerRun is invoked on plugins that already ran, in reverse order,
// with an [a2asrv.ServerResponse] carrying the error.
func (m *SequentialManager) BeforeHandlerRun(ctx context.Context, req *a2asrv.ServerRequest) (context.Context, error) {
	return runBefore(ctx, m.server, "BeforeHandlerRun",
		func(ctx context.Context, p a2asrv.ServerPlugin) (context.Context, error) {
			return p.BeforeHandlerRun(ctx, req)
		},
		func(ran []a2asrv.ServerPlugin, err error) {
			resp := &a2asrv.ServerResponse{Method: req.Method, CallContext: req.CallContext, Err: err, IsFinal: true}
			runAfterHandlerRun(ctx, ran, resp)
		},
	)
}

// AfterHandlerRun implements [a2asrv.ServerPlugin]. Errors are logged and swallowed.
func (m *SequentialManager) AfterHandlerRun(ctx context.Context, resp *a2asrv.ServerResponse) (context.Context, error) {
	return runAfterHandlerRun(ctx, m.server, resp), nil
}

// BeforeClientRequest implements [a2aclient.ClientPlugin]. See [SequentialManager.BeforeHandlerRun]
// for the ordering and error semantics.
func (m *SequentialManager) BeforeClientRequest(ctx context.Context, req *a2aclient.ClientRequest) (context.Context, error) {
	return runBefore(ctx, m.client, "BeforeClientRequest",
		func(ctx context.Context, p a2aclient.ClientPlugin) (context.Context, error) {
			return p.BeforeClientRequest(ctx, req)
		},
		func(ran []a2aclient.ClientPlugin, err error) {
			resp := &a2aclient.ClientResponse{
				Method:        req.Method,
				BaseURL:       req.BaseURL,
				ServiceParams: req.ServiceParams,
				Card:          req.Card,
				Err:           err,
				IsFinal:       true,
			}
			runAfterClientRequest(ctx, ran, resp)
		},
	)
}

// AfterClientRequest implements [a2aclient.ClientPlugin]. Errors are logged and swallowed.
func (m *SequentialManager) AfterClientRequest(ctx context.Context, resp *a2aclient.ClientResponse) (context.Context, error) {
	return runAfterClientRequest(ctx, m.client, resp), nil
}

// BeforeExecution implements [a2asrv.ExecutionPlugin]. If a hook returns an error,
// AfterExecution is invoked on plugins that already ran, in reverse order, with the error.
func (m *SequentialManager) BeforeExecution(ctx context.Context, execCtx *a2asrv.ExecutorContext) (context.Context, error) {
	return runBefore(ctx, m.execution, "BeforeExecution",
		func(ctx context.Context, p a2asrv.ExecutionPlugin) (context.Context, error) {
			return p.BeforeExecution(ctx, execCtx)
		},
		func(ran []a2asrv.ExecutionPlugin, err error) {
			runAfterExecution(ctx, ran, execCtx, err)
		},
	)
}

// OnExecutionEvent implements [a2asrv.ExecutionPlugin]. Hooks run in registration order.
// Errors are logged and swallowed.
func (m *SequentialManager) OnExecutionEvent(ctx context.Context, execCtx *a2asrv.ExecutorContext, event a2a.Event) error {
	runObservers(ctx, m.execution, "OnExecutionEvent", func(p a2asrv.ExecutionPlugin) error {
		return p.OnExecutionEvent(ctx, execCtx, event)
	})
	return nil
}

// AfterExecution implements [a2asrv.ExecutionPlugin]. Errors are logged and swallowed.
func (m *SequentialManager) AfterExecution(ctx context.Context, execCtx *a2asrv.ExecutorContext, cause error) error {
	runAfterExecution(ctx, m.execution, execCtx, cause)
	return nil
}

// OnTaskEvent implements [a2asrv.TaskObserverPlugin]. Hooks run in registration order.
// Errors are logged and swallowed.
func (m *SequentialManager) OnTaskEvent(ctx context.Context, info *a2asrv.TaskEventInfo) error {
	runObservers(ctx, m.task, "OnTaskEvent", func(p a2asrv.TaskObserverPlugin) error {
		return p.OnTaskEvent(ctx, info)
	})
	return nil
}

// BeforePushSend implements [push.Plugin]. If a hook returns an error, OnPushSent is invoked
// on plugins that already ran with a [push.DeliveryResult] carrying the error.
func (m *SequentialManager) BeforePushSend(ctx context.Context, delivery *push.Delivery) (context.Context, error) {
	return runBefore(ctx, m.pushes, "BeforePushSend",
		func(ctx context.Context, p push.Plugin) (context.Context, error) {
			return p.BeforePushSend(ctx, delivery)
		},
		func(ran []push.Plugin, err error) {
			runOnPushSent(ctx, ran, &push.DeliveryResult{Delivery: delivery, Err: err})
		},
	)
}

// OnPushSent implements [push.Plugin]. Hooks run in registration order. Errors are logged and swallowed.
func (m *SequentialManager) OnPushSent(ctx context.Context, result *push.DeliveryResult) error {
	runOnPushSent(ctx, m.pushes, result)
	return nil
}

func runOnPushSent(ctx context.Context, plugins []push.Plugin, result *push.DeliveryResult) {
	runObservers(ctx, plugins, "OnPushSent", func(p push.Plugin) error {
		return p.OnPushSent(ctx, result)
	})
}

func runAfterHandlerRun(ctx context.Context, plugins []a2asrv.ServerPlugin, resp *a2asrv.ServerResponse) context.Context {
	return runAfter(ctx, plugins, "AfterHandlerRun", func(ctx context.Context, p a2asrv.ServerPlugin) (context.Context, error) {
		return p.AfterHandlerRun(ctx, resp)
	})
}

func runAfterClientRequest(ctx context.Context, plugins []a2aclient.ClientPlugin, resp *a2aclient.ClientResponse) context.Context {
	return runAfter(ctx, plugins, "AfterClientRequest", func(ctx context.Context, p a2aclient.ClientPlugin) (context.Context, error) {
		return p.AfterClientRequest(ctx, resp)
	})
}

func runAfterExecution(ctx context.Context, plugins []a2asrv.ExecutionPlugin, execCtx *a2asrv.ExecutorContext, cause error) {
	runAfter(ctx, plugins, "AfterExecution", func(ctx context.Context, p a2asrv.ExecutionPlugin) (context.Context, error) {
		return ctx, p.AfterExecution(ctx, execCtx, cause)
	})
}

// runBefore calls hook on plugins in registration order, passing the returned context
// to the next plugin. If a hook fails, the rejecting plugin is logged, unwind is called with
// the plugins that already ran and the error is returned.
func runBefore[P Plugin](
	ctx context.Context, plugins []P, hookName string,
	hook func(context.Context, P) (context.Context, error),
	unwind func(ran []P, err error),
) (context.Context, error) {
	for i, p := range plugins {
		next, err := hook(ctx, p)
		if err != nil {
			log.Info(ctx, "a2aplugin: "+hookName+" rejected the call", "plugin", p.Name(), "cause", err)
			unwind(plugins[:i], err)
			return ctx, err
		}
		ctx = next
	}
	return ctx, nil
}

// runObservers calls hook on plugins in registration order. Errors are logged and swallowed.
func runObservers[P Plugin](ctx context.Context, plugins []P, hookName string, hook func(P) error) {
	for _, p := range plugins {
		if err := hook(p); err != nil {
			log.Error(ctx, "a2aplugin: "+hookName+" failed", err, "plugin", p.Name())
		}
	}
}

// runAfter calls hook on plugins in reverse order, passing the returned context to the
// next plugin. Errors are logged and swallowed.
func runAfter[P Plugin](ctx context.Context, plugins []P, hookName string, hook func(context.Context, P) (context.Context, error)) context.Context {
	for _, p := range slices.Backward(plugins) {
		next, err := hook(ctx, p)
		if err != nil {
			log.Error(ctx, "a2aplugin: "+hookName+" failed", err, "plugin", p.Name())
			continue
		}
		ctx = next
	}
	return ctx
}

// selectPlugins returns the plugins implementing hook interface P. Nested Managers are
// included only when they report that they have hooks of the given kind.
func selectPlugins[P Plugin](plugins []Plugin, kind core.HookKind) []P {
	var selected []P
	for _, p := range plugins {
		if typed, ok := p.(P); ok && core.HasHooks(typed, kind) {
			selected = append(selected, typed)
		}
	}
	return selected
}
