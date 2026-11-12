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

import "testing"

type ctxKey struct{}

func TestServer_Name(t *testing.T) {
	t.Parallel()
	if got := Server("audit", ServerHooks{}).Name(); got != "audit" {
		t.Fatalf("Server().Name() = %q, want %q", got, "audit")
	}
}
