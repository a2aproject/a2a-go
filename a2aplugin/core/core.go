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
