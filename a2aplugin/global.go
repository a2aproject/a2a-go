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
	"sync/atomic"
)

var globalManager atomic.Pointer[Manager]

// SetGlobal installs m as the process-wide plugin [Manager]. SDK components
// capture [Global] at construction time, so SetGlobal must be called once at
// startup, before those components are built. Panics if m is nil.
func SetGlobal(m Manager) {
	if m == nil {
		panic("a2aplugin: SetGlobal called with a nil Manager")
	}
	globalManager.Store(&m)
}

// Global returns the process-wide plugin [Manager]. If [SetGlobal] has not
// been called it returns an empty Sequential manager.
func Global() Manager {
	if p := globalManager.Load(); p != nil {
		return *p
	}
	return emptyGlobal
}

var emptyGlobal Manager = Sequential()
