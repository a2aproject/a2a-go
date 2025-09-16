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

import "github.com/a2aproject/a2a-go/a2a"

// NewSubmittedTask is a utility for converting a Message sent by the user to a new Task
// the Agent will be working on.
func NewSubmittedTask(msg *a2a.Message) *a2a.Task {
	history := make([]*a2a.Message, 1)
	history[0] = msg

	contextID := msg.ContextID
	if contextID == "" {
		contextID = a2a.NewContextID()
	}

	return &a2a.Task{
		ID:        a2a.NewTaskID(),
		ContextID: contextID,
		Status:    a2a.TaskStatus{State: a2a.TaskStateSubmitted},
		History:   history,
	}
}
