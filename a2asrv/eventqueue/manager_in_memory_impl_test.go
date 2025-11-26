// Copyright 2025 The A2A Authors
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

package eventqueue

import (
	"testing"

	"github.com/a2aproject/a2a-go/a2a"
)

func TestInMemoryManager(t *testing.T) {
	ctx, tid := t.Context(), a2a.NewTaskID()

	manager := NewInMemoryManager()
	if _, ok := manager.Get(ctx, tid); ok {
		t.Fatal("manager.Get() ok = true before a queue was created, want false")
	}
	if _, err := manager.GetOrCreate(ctx, tid); err != nil {
		t.Fatalf("anager.GetOrCreate() error = %v", err)
	}
	if _, ok := manager.Get(ctx, tid); !ok {
		t.Fatal("manager.Get() ok = false after a queue was created, want true")
	}
	if err := manager.Destroy(ctx, tid); err != nil {
		t.Fatalf("manager.Destroy() error = %v", err)
	}
	if _, ok := manager.Get(ctx, tid); ok {
		t.Fatal("manager.Get() ok = true after a queue was destroyed, want false")
	}
}
