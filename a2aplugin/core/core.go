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

// Package core defines the base plugin interface and the process-wide plugin registry
// shared by [github.com/a2aproject/a2a-go/v2/a2asrv], [github.com/a2aproject/a2a-go/v2/a2aclient]
// and [github.com/a2aproject/a2a-go/v2/a2aplugin]. Most users should use the a2aplugin package instead.
package core

import "sync/atomic"

// Plugin is the common base of every plugin.
type Plugin interface {
	// Name returns a short identifier used in logs and metrics.
	Name() string
}

var global atomic.Pointer[Plugin]

// SetGlobal installs p as the process-wide plugin. Panics if p is nil.
func SetGlobal(p Plugin) {
	if p == nil {
		panic("core: SetGlobal called with a nil Plugin")
	}
	global.Store(&p)
}

// Global returns the process-wide plugin or nil if [SetGlobal] has not been called.
func Global() Plugin {
	if p := global.Load(); p != nil {
		return *p
	}
	return nil
}

// GlobalAs returns the process-wide plugin if it implements P, or the zero value of P otherwise.
func GlobalAs[P any]() P {
	p, _ := Global().(P)
	return p
}

// HookKind identifies a group of plugin hooks.
type HookKind int

const (
	// ServerHooks are the hooks of a2asrv.ServerPlugin.
	ServerHooks HookKind = iota
	// ClientHooks are the hooks of a2aclient.ClientPlugin.
	ClientHooks
	// ExecutionHooks are the hooks of a2asrv.ExecutionPlugin.
	ExecutionHooks
	// TaskHooks are the hooks of a2asrv.TaskObserverPlugin.
	TaskHooks
	// PushHooks are the hooks of push.Plugin.
	PushHooks
)

// HooksReporter is implemented by plugins which can report whether they have hooks of
// a given kind, for example a plugin manager none of whose plugins implement them.
type HooksReporter interface {
	// HasHooks reports whether the plugin has hooks of the given kind.
	HasHooks(kind HookKind) bool
}

// HasHooks reports whether hooks of the given kind should be dispatched to p. It returns
// false for a nil p, p.HasHooks(kind) if p implements [HooksReporter], and true otherwise.
func HasHooks(p Plugin, kind HookKind) bool {
	if p == nil {
		return false
	}
	if r, ok := p.(HooksReporter); ok {
		return r.HasHooks(kind)
	}
	return true
}
