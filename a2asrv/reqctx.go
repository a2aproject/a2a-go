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

package a2asrv

import (
	"context"

	"github.com/a2aproject/a2a-go/a2a"
)

// RequestContextBuilder defines an extension point for constructing request contexts
// that contain the information needed by AgentExecutor implementations to process incoming requests.
type RequestContextBuilder interface {
	// Build constructs a RequestContext from the provided parameters.
	Build(ctx context.Context, p *a2a.MessageSendParams, t *a2a.Task) *RequestContext
}

// RequestContext provides information about an incoming A2A request to AgentExecutor.
type RequestContext struct {
	// Request which triggered the execution.
	Request *a2a.MessageSendParams
	// TaskID is an ID of the task or a newly generated UUIDv4 in case Message did not reference any Task.
	TaskID a2a.TaskID
	// Task is present if request message specified a TaskID.
	Task *a2a.Task
	// RelatedTasks can be present when Message includes Task references and RequestContextBuilder is configured to load them.
	RelatedTasks []*a2a.Task
	// ContextID is a server-generated identifier for maintaining context across multiple related tasks or interactions. Matches the Task ContextID.
	ContextID string
}
