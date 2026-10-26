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
