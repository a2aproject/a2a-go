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

package core

import "testing"

type namedPlugin string

func (p namedPlugin) Name() string { return string(p) }

func TestGlobal(t *testing.T) {
	if got := Global(); got != nil {
		t.Fatalf("Global() = %v, want nil before SetGlobal", got)
	}

	SetGlobal(namedPlugin("first"))
	SetGlobal(namedPlugin("second"))

	if got := Global(); got != namedPlugin("second") {
		t.Fatalf("Global() = %v, want %v", got, namedPlugin("second"))
	}
}

func TestSetGlobalPanicsOnNil(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatalf("SetGlobal(nil) did not panic")
		}
	}()
	SetGlobal(nil)
}

type describedPlugin struct{ namedPlugin }

func (describedPlugin) Description() string { return "described" }

type describer interface{ Description() string }

func TestGlobalAs(t *testing.T) {
	SetGlobal(namedPlugin("plain"))
	if got := GlobalAs[describer](); got != nil {
		t.Fatalf("GlobalAs[describer]() = %v, want nil for a plugin without Description", got)
	}

	SetGlobal(describedPlugin{"described"})
	if got := GlobalAs[describer](); got == nil {
		t.Fatalf("GlobalAs[describer]() = nil, want the global plugin")
	}
}

type hooksReporter interface{ HasTestHooks() bool }

type reportingPlugin struct {
	namedPlugin
	has bool
}

func (p reportingPlugin) HasTestHooks() bool { return p.has }

func TestHasHooks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		plugin Plugin
		want   bool
	}{
		{name: "nil plugin", plugin: nil, want: false},
		{name: "plugin without reporter", plugin: namedPlugin("p"), want: true},
		{name: "reporter with hooks", plugin: reportingPlugin{has: true}, want: true},
		{name: "reporter without hooks", plugin: reportingPlugin{has: false}, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := HasHooks(tc.plugin, hooksReporter.HasTestHooks); got != tc.want {
				t.Fatalf("HasHooks() = %v, want %v", got, tc.want)
			}
		})
	}
}
