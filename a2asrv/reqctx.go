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
	"errors"
	"fmt"
	"github.com/a2aproject/a2a-go/a2a"
	"github.com/google/uuid"
)

// RequestContextBuilder defines an extension point for constructing request contexts
// that contain the information needed by AgentExecutor implementations to process incoming requests.
type RequestContextBuilder interface {
	// Build constructs a RequestContext from the provided parameters.
	Build(ctx context.Context, p a2a.MessageSendParams, t *a2a.Task) (RequestContext, error)
}

// RequestContext provides information about an incoming A2A request to AgentExecutor.
type RequestContext struct {
	// Request which triggered the execution.
	Request a2a.MessageSendParams
	// TaskID is an ID of the task or a newly generated UUIDv4 in case Message did not reference any Task.
	TaskID a2a.TaskID
	// Task is present if request message specified a TaskID.
	Task *a2a.Task
	// RelatedTasks can be present when Message includes Task references and RequestContextBuilder is configured to load them.
	RelatedTasks []a2a.Task
	// ContextID is a server-generated identifier for maintaining context across multiple related tasks or interactions. Matches the Task ContextID.
	ContextID string
}

// SimpleRequestContextBuilder Builds request context and populates referred tasks.
type SimpleRequestContextBuilder struct {
	store                       TaskStore
	shouldPopulateReferredTasks bool
}

func NewSimpleRequestContextBuilder(store TaskStore, shouldPopulateReferredTasks bool) *SimpleRequestContextBuilder {
	return &SimpleRequestContextBuilder{
		store:                       store,
		shouldPopulateReferredTasks: shouldPopulateReferredTasks,
	}
}

func (s *SimpleRequestContextBuilder) Build(ctx context.Context, p a2a.MessageSendParams, t *a2a.Task) (RequestContext, error) {
	relatedTasks := make([]a2a.Task, 0)
	var err error
	if s.store != nil && s.shouldPopulateReferredTasks {
		for _, taskId := range p.Message.ReferenceTasks {
			task, e := s.store.Get(ctx, taskId)
			if e != nil {
				e = fmt.Errorf("failed to get referenced task: %w, taskid: %v", e, taskId)
				err = errors.Join(err, e)
				continue
			}
			if IsTerminalStates(task.Status.State) {
				e = fmt.Errorf("referenced task (ID: %s) is in terminal state (%s)", taskId, task.Status.State)
				err = errors.Join(err, e)
				continue
			}
			relatedTasks = append(relatedTasks, task)
		}
	}
	if err != nil {
		return RequestContext{}, err
	}

	return NewRequestContext(p, t, relatedTasks)
}

func NewRequestContext(
	request a2a.MessageSendParams,
	task *a2a.Task,
	relatedTasks []a2a.Task,
) (RequestContext, error) {
	var (
		tid a2a.TaskID
		cid string
	)

	if request.Message.TaskID != nil {
		tid = *request.Message.TaskID
		if task != nil && task.ID != tid {
			return RequestContext{}, errors.New("bad task id")
		}
	} else {
		tid = a2a.TaskID(uuid.NewString())
	}

	if request.Message.ContextID != nil {
		cid = *request.Message.ContextID
		if task != nil && task.ContextID != cid {
			return RequestContext{}, errors.New("bad task id")
		}
	} else {
		cid = uuid.NewString()
	}

	request.Message.TaskID = &tid
	request.Message.ContextID = &cid

	if task == nil {
		return RequestContext{Task: nil, ContextID: cid, TaskID: tid, RelatedTasks: relatedTasks}, nil
	}
	return RequestContext{Request: request, TaskID: tid, ContextID: cid, Task: task, RelatedTasks: relatedTasks}, nil
}

func IsTerminalStates(state a2a.TaskState) bool {
	return state == a2a.TaskStateCanceled ||
		state == a2a.TaskStateRejected ||
		state == a2a.TaskStateFailed ||
		state == a2a.TaskStateCompleted ||
		state == a2a.TaskStateUnknown ||
		state == a2a.TaskStateInputRequired
}
