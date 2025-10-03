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
	"fmt"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
	"github.com/a2aproject/a2a-go/internal/taskupdate"
)

type executor struct {
	*processor
	taskID          a2a.TaskID
	taskStore       TaskStore
	pushConfigStore PushConfigStore
	agent           AgentExecutor
	params          *a2a.MessageSendParams
}

func (e *executor) Execute(ctx context.Context, q eventqueue.Queue) error {
	reqCtx, err := e.loadExecRequestContext(ctx)
	if err != nil {
		return err
	}

	e.processor = &processor{updateManager: taskupdate.NewManager(e.taskStore, reqCtx.Task)}

	return e.agent.Execute(ctx, reqCtx, q)
}

func (e *executor) loadExecRequestContext(ctx context.Context) (*RequestContext, error) {
	params, msg := e.params, e.params.Message

	var task *a2a.Task
	if msg.TaskID == "" {
		task = taskupdate.NewSubmittedTask(e.taskID, msg)
	} else {
		localResult, err := e.taskStore.Get(ctx, msg.TaskID)
		if err != nil {
			return nil, fmt.Errorf("request context loading failed: %w", err)
		}

		if msg.ContextID != "" && msg.ContextID != localResult.ContextID {
			return nil, fmt.Errorf("message contextID different from task contextID: %w", a2a.ErrInvalidRequest)
		}

		if localResult.Status.State.Terminal() {
			return nil, fmt.Errorf("message referenced a task in a terminal state, state = %s: %w", task.Status.State, a2a.ErrInvalidRequest)
		}

		task = localResult
	}

	if params.Config != nil && params.Config.PushConfig != nil {
		if e.pushConfigStore == nil {
			return nil, a2a.ErrPushNotificationNotSupported
		}
		if err := e.pushConfigStore.Save(ctx, task.ID, params.Config.PushConfig); err != nil {
			return nil, fmt.Errorf("failed to save %v: %w", params.Config.PushConfig, err)
		}
	}

	return &RequestContext{
		Message:   params.Message,
		Task:      task,
		TaskID:    task.ID,
		ContextID: task.ContextID,
		Metadata:  params.Message.Metadata,
	}, nil
}

type canceler struct {
	*processor
	agent     AgentExecutor
	taskStore TaskStore
	taskID    a2a.TaskID
	params    *a2a.TaskIDParams
}

func (c *canceler) Cancel(ctx context.Context, q eventqueue.Queue) error {
	task, err := c.taskStore.Get(ctx, c.taskID)
	if err != nil {
		return fmt.Errorf("failed to load a task: %w", err)
	}

	if task.Status.State == a2a.TaskStateCanceled {
		return q.Write(ctx, task)
	}

	if task.Status.State.Terminal() {
		return fmt.Errorf("task in non-cancelable state %s: %w", task.Status.State, a2a.ErrTaskNotCancelable)
	}

	c.processor = &processor{updateManager: taskupdate.NewManager(c.taskStore, task)}

	reqCtx := &RequestContext{
		TaskID:    c.taskID,
		ContextID: task.ContextID,
		Task:      task,
		Metadata:  c.params.Metadata,
	}

	return c.agent.Cancel(ctx, reqCtx, q)
}

type processor struct {
	updateManager *taskupdate.Manager
}

func (p *processor) Process(ctx context.Context, event a2a.Event) (*a2a.SendMessageResult, error) {
	// TODO(yarolegovich): handle invalid event sequence where a Message is produced after a Task was created
	if msg, ok := event.(*a2a.Message); ok {
		var result a2a.SendMessageResult = msg
		return &result, nil
	}

	if err := p.updateManager.Process(ctx, event); err != nil {
		return nil, err
	}

	task := p.updateManager.Task

	// TODO(yarolegovich): handle pushes

	if _, ok := event.(*a2a.TaskArtifactUpdateEvent); ok {
		return nil, nil
	}

	if statusUpdate, ok := event.(*a2a.TaskStatusUpdateEvent); ok {
		if statusUpdate.Final {
			var result a2a.SendMessageResult = task
			return &result, nil
		}
		return nil, nil
	}

	if task.Status.State == a2a.TaskStateUnknown {
		return nil, fmt.Errorf("unknown task state: %s", task.Status.State)
	}

	if task.Status.State.Terminal() || task.Status.State == a2a.TaskStateInputRequired {
		var result a2a.SendMessageResult = task
		return &result, nil
	}

	return nil, nil
}
