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
	"github.com/a2aproject/a2a-go/v2/log"
)

// Manager runs a group of registered plugins. It implements every plugin hook
// interface, so Managers can be nested.
// The Has*Hooks methods let call sites skip plugin work when no registered
// plugin implements the hooks they would call.
type Manager interface {
	CompletePlugin
	a2asrv.ExecutionPlugin

	// HasServerHooks reports whether any registered plugin implements [a2asrv.ServerPlugin].
	HasServerHooks() bool
	// HasClientHooks reports whether any registered plugin implements [a2aclient.ClientPlugin].
	HasClientHooks() bool
	// HasExecutionHooks reports whether any registered plugin implements [a2asrv.ExecutionPlugin].
	HasExecutionHooks() bool
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
}

// Sequential returns a [Manager] that runs the given plugins sequentially.
func Sequential(plugins ...Plugin) *SequentialManager {
	return &SequentialManager{
		plugins:   slices.Clone(plugins),
		server:    selectPlugins[a2asrv.ServerPlugin](plugins, Manager.HasServerHooks),
		client:    selectPlugins[a2aclient.ClientPlugin](plugins, Manager.HasClientHooks),
		execution: selectPlugins[a2asrv.ExecutionPlugin](plugins, Manager.HasExecutionHooks),
	}
}

var _ Manager = (*SequentialManager)(nil)

// Name implements [Plugin].
func (*SequentialManager) Name() string { return "sequential" }

// Plugins returns the registered plugins in registration order.
func (m *SequentialManager) Plugins() []Plugin { return slices.Clone(m.plugins) }

// HasServerHooks implements [Manager].
func (m *SequentialManager) HasServerHooks() bool { return len(m.server) > 0 }

// HasClientHooks implements [Manager].
func (m *SequentialManager) HasClientHooks() bool { return len(m.client) > 0 }

// HasExecutionHooks implements [Manager].
func (m *SequentialManager) HasExecutionHooks() bool { return len(m.execution) > 0 }

// BeforeHandlerRun implements [a2asrv.ServerPlugin]. If a hook returns an error,
// AfterHandlerRun is invoked on plugins that already ran, in reverse order,
// with an [a2asrv.ServerResponse] carrying the error.
func (m *SequentialManager) BeforeHandlerRun(ctx context.Context, req *a2asrv.ServerRequest) (context.Context, error) {
	return runBefore(ctx, m.server,
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
	return runBefore(ctx, m.client,
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
	return runBefore(ctx, m.execution,
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
	for _, p := range m.execution {
		if err := p.OnExecutionEvent(ctx, execCtx, event); err != nil {
			log.Error(ctx, "a2aplugin: OnExecutionEvent failed", err, "plugin", p.Name())
		}
	}
	return nil
}

// AfterExecution implements [a2asrv.ExecutionPlugin]. Errors are logged and swallowed.
func (m *SequentialManager) AfterExecution(ctx context.Context, execCtx *a2asrv.ExecutorContext, cause error) error {
	runAfterExecution(ctx, m.execution, execCtx, cause)
	return nil
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
// to the next plugin. If a hook fails, unwind is called with the plugins that already ran
// and the error is returned.
func runBefore[P Plugin](
	ctx context.Context, plugins []P,
	hook func(context.Context, P) (context.Context, error),
	unwind func(ran []P, err error),
) (context.Context, error) {
	for i, p := range plugins {
		next, err := hook(ctx, p)
		if err != nil {
			unwind(plugins[:i], err)
			return ctx, err
		}
		ctx = next
	}
	return ctx, nil
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
// included only when hasHooks reports that they have hooks of that kind.
func selectPlugins[P Plugin](plugins []Plugin, hasHooks func(Manager) bool) []P {
	var selected []P
	for _, p := range plugins {
		if typed, ok := p.(P); ok && core.HasHooks(typed, hasHooks) {
			selected = append(selected, typed)
		}
	}
	return selected
}
