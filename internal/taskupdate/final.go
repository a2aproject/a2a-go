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

package taskupdate

import (
	"github.com/a2aproject/a2a-go/a2a"
)

// IsFinal returns true if event must terminate a valid execution event sequence.
func IsFinal(event a2a.Event) bool {
	if _, ok := event.(*a2a.Message); ok {
		return true
	}

	var state a2a.TaskState
	switch v := event.(type) {
	case *a2a.TaskStatusUpdateEvent:
		state = v.Status.State
	case *a2a.Task:
		state = v.Status.State
	default:
		return false
	}

	return state.Terminal() || state == a2a.TaskStateInputRequired
}
