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
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2aplugin"
)

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

func TestSetGlobalPanicsOnNil(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Errorf("SetGlobal(nil) did not panic, want panic")
		}
	}()
	a2aplugin.SetGlobal(nil)
}
