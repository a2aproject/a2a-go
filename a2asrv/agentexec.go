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
	pushNotifier    PushNotifier
	pushConfigStore PushConfigStore
	agent           AgentExecutor
	params          *a2a.MessageSendParams
	interceptors    []RequestContextInterceptor
}

func (e *executor) Execute(ctx context.Context, q eventqueue.Queue) error {
	reqCtx, err := e.loadExecRequestContext(ctx)
	if err != nil {
		return err
	}
	e.processor.init(taskupdate.NewManager(e.taskStore, reqCtx.Task))

	for _, interceptor := range e.interceptors {
		ctx, err = interceptor.Intercept(ctx, reqCtx)
		if err != nil {
			return fmt.Errorf("interceptor failed: %w", err)
		}
	}

	return e.agent.Execute(ctx, reqCtx, q)
}

func (e *executor) loadExecRequestContext(ctx context.Context) (*RequestContext, error) {
	params, msg := e.params, e.params.Message

	var task *a2a.Task
	if msg.TaskID == "" {
		task = taskupdate.NewSubmittedTask(e.taskID, msg)
	} else {
		storedTask, err := e.taskStore.Get(ctx, msg.TaskID)
		if err != nil {
			return nil, fmt.Errorf("task loading failed: %w", err)
		}
		if storedTask == nil {
			return nil, a2a.ErrTaskNotFound
		}

		if msg.ContextID != "" && msg.ContextID != storedTask.ContextID {
			return nil, fmt.Errorf("message contextID different from task contextID: %w", a2a.ErrInvalidRequest)
		}

		if storedTask.Status.State.Terminal() {
			return nil, fmt.Errorf("task in a terminal state %q: %w", storedTask.Status.State, a2a.ErrInvalidRequest)
		}

		storedTask.History = append(storedTask.History, msg)
		if err := e.taskStore.Save(ctx, storedTask); err != nil {
			return nil, fmt.Errorf("task message history update failed: %w", err)
		}

		task = storedTask
	}

	if params.Config != nil && params.Config.PushConfig != nil {
		if e.pushConfigStore == nil {
			return nil, a2a.ErrPushNotificationNotSupported
		}
		if _, err := e.pushConfigStore.Save(ctx, task.ID, params.Config.PushConfig); err != nil {
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
	agent        AgentExecutor
	taskStore    TaskStore
	params       *a2a.TaskIDParams
	interceptors []RequestContextInterceptor
}

func (c *canceler) Cancel(ctx context.Context, q eventqueue.Queue) error {
	task, err := c.taskStore.Get(ctx, c.params.ID)
	if err != nil {
		return fmt.Errorf("failed to load a task: %w", err)
	}
	c.processor.init(taskupdate.NewManager(c.taskStore, task))

	if task.Status.State == a2a.TaskStateCanceled {
		return q.Write(ctx, task)
	}

	if task.Status.State.Terminal() {
		return fmt.Errorf("task in non-cancelable state %s: %w", task.Status.State, a2a.ErrTaskNotCancelable)
	}

	reqCtx := &RequestContext{
		TaskID:    task.ID,
		Task:      task,
		ContextID: task.ContextID,
		Metadata:  c.params.Metadata,
	}

	for _, interceptor := range c.interceptors {
		ctx, err = interceptor.Intercept(ctx, reqCtx)
		if err != nil {
			return fmt.Errorf("interceptor failed: %w", err)
		}
	}

	return c.agent.Cancel(ctx, reqCtx, q)
}

type processor struct {
	// Processor is running in event consumer goroutine, but request context loading
	// happens in event consumer goroutine. Once request context is loaded and validate the processor
	// gets initialized.
	updateManager   *taskupdate.Manager
	pushConfigStore PushConfigStore
	pushNotifier    PushNotifier
}

func newProcessor(pushConfigStore PushConfigStore, pushNotifier PushNotifier) *processor {
	return &processor{
		pushConfigStore: pushConfigStore,
		pushNotifier:    pushNotifier,
	}
}

func (p *processor) init(um *taskupdate.Manager) {
	p.updateManager = um
}

// Process implements taskexec.Processor interface.
// A (nil, nil) result means the processing should continue.
// A non-nill result becomes the result of the execution.
func (p *processor) Process(ctx context.Context, event a2a.Event) (*a2a.SendMessageResult, error) {
	// TODO(yarolegovich): handle invalid event sequence where a Message is produced after a Task was created
	if msg, ok := event.(*a2a.Message); ok {
		var result a2a.SendMessageResult = msg
		return &result, nil
	}

	task, err := p.updateManager.Process(ctx, event)
	if err != nil {
		return nil, err
	}

	p.sendPushNotifications(ctx, task)

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

func (p *processor) sendPushNotifications(ctx context.Context, task *a2a.Task) {
	if p.pushNotifier != nil && p.pushConfigStore != nil {
		configs, _ := p.pushConfigStore.List(ctx, task.ID)
		// TODO(yarolegovich): log error from getting stored push configs
		// TODO(yarolegovich): consider dispatching in parallel with max concurrent calls cap
		for _, config := range configs {
			// TODO(yarolegovich): log error from sending a push
			_ = p.pushNotifier.SendPush(ctx, config, task)
		}
	}
}
