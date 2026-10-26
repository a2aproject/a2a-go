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

	// HasServerHooks reports whether any registered plugin implements [a2asrv.ServerPlugin].
	HasServerHooks() bool
	// HasClientHooks reports whether any registered plugin implements [a2aclient.ClientPlugin].
	HasClientHooks() bool
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
	plugins []Plugin
	server  []a2asrv.ServerPlugin
	client  []a2aclient.ClientPlugin
}

// Sequential returns a [Manager] that runs the given plugins sequentially.
func Sequential(plugins ...Plugin) *SequentialManager {
	m := &SequentialManager{plugins: slices.Clone(plugins)}
	for _, p := range plugins {
		if sp, ok := p.(a2asrv.ServerPlugin); ok && hasServerHooks(sp) {
			m.server = append(m.server, sp)
		}
		if cp, ok := p.(a2aclient.ClientPlugin); ok && hasClientHooks(cp) {
			m.client = append(m.client, cp)
		}
	}
	return m
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

// BeforeHandlerRun implements [a2asrv.ServerPlugin]. If a hook returns an error,
// AfterHandlerRun is invoked on plugins that already ran, in reverse order,
// with an [a2asrv.ServerResponse] carrying the error.
func (m *SequentialManager) BeforeHandlerRun(ctx context.Context, req *a2asrv.ServerRequest) (context.Context, error) {
	var ran []a2asrv.ServerPlugin
	for _, p := range m.server {
		next, err := p.BeforeHandlerRun(ctx, req)
		if err != nil {
			resp := &a2asrv.ServerResponse{
				Method:      req.Method,
				CallContext: req.CallContext,
				Err:         err,
				IsFinal:     true,
			}
			unwindAfterHandlerRun(ctx, ran, resp)
			return ctx, err
		}
		ctx = next
		ran = append(ran, p)
	}
	return ctx, nil
}

// AfterHandlerRun implements [a2asrv.ServerPlugin]. Errors are logged and swallowed.
func (m *SequentialManager) AfterHandlerRun(ctx context.Context, resp *a2asrv.ServerResponse) (context.Context, error) {
	for _, p := range slices.Backward(m.server) {
		next, err := p.AfterHandlerRun(ctx, resp)
		if err != nil {
			log.Error(ctx, "a2aplugin: AfterHandlerRun failed", err, "plugin", p.Name())
			continue
		}
		ctx = next
	}
	return ctx, nil
}

// BeforeClientRequest implements [a2aclient.ClientPlugin]. See [SequentialManager.BeforeHandlerRun]
// for the ordering and error semantics.
func (m *SequentialManager) BeforeClientRequest(ctx context.Context, req *a2aclient.ClientRequest) (context.Context, error) {
	var ran []a2aclient.ClientPlugin
	for _, p := range m.client {
		next, err := p.BeforeClientRequest(ctx, req)
		if err != nil {
			resp := &a2aclient.ClientResponse{
				Method:        req.Method,
				BaseURL:       req.BaseURL,
				ServiceParams: req.ServiceParams,
				Card:          req.Card,
				Err:           err,
				IsFinal:       true,
			}
			unwindAfterClientRequest(ctx, ran, resp)
			return ctx, err
		}
		ctx = next
		ran = append(ran, p)
	}
	return ctx, nil
}

// AfterClientRequest implements [a2aclient.ClientPlugin]. Errors are logged and swallowed.
func (m *SequentialManager) AfterClientRequest(ctx context.Context, resp *a2aclient.ClientResponse) (context.Context, error) {
	for _, p := range slices.Backward(m.client) {
		next, err := p.AfterClientRequest(ctx, resp)
		if err != nil {
			log.Error(ctx, "a2aplugin: AfterClientRequest failed", err, "plugin", p.Name())
			continue
		}
		ctx = next
	}
	return ctx, nil
}

func unwindAfterHandlerRun(ctx context.Context, ran []a2asrv.ServerPlugin, resp *a2asrv.ServerResponse) {
	for _, p := range slices.Backward(ran) {
		if _, err := p.AfterHandlerRun(ctx, resp); err != nil {
			log.Error(ctx, "a2aplugin: AfterHandlerRun failed during unwind", err, "plugin", p.Name())
		}
	}
}

func unwindAfterClientRequest(ctx context.Context, ran []a2aclient.ClientPlugin, resp *a2aclient.ClientResponse) {
	for _, p := range slices.Backward(ran) {
		if _, err := p.AfterClientRequest(ctx, resp); err != nil {
			log.Error(ctx, "a2aplugin: AfterClientRequest failed during unwind", err, "plugin", p.Name())
		}
	}
}

// hasServerHooks returns true if the plugin should receive server-hook dispatch.
// Nested Managers contribute only when their own HasServerHooks is true.
func hasServerHooks(sp a2asrv.ServerPlugin) bool {
	if m, ok := sp.(Manager); ok {
		return m.HasServerHooks()
	}
	return true
}

// hasClientHooks mirrors [hasServerHooks] for client hooks.
func hasClientHooks(cp a2aclient.ClientPlugin) bool {
	if m, ok := cp.(Manager); ok {
		return m.HasClientHooks()
	}
	return true
}
