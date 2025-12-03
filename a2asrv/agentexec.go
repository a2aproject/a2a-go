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
	"github.com/a2aproject/a2a-go/internal/taskexec"
	"github.com/a2aproject/a2a-go/internal/taskupdate"
)

// AgentExecutor implementations translate agent outputs to A2A events.
// The provided [RequestContext] should be used as a [a2a.TaskInfoProvider] argument for [a2a.Event]-s constructor functions.
// For streaming responses [a2a.TaskArtifactUpdatEvent]-s should be used.
// A2A server stops processing events after one of these events:
//   - An [a2a.Message] with any payload.
//   - An [a2a.TaskStatusUpdateEvent] with Final field set to true.
//   - An [a2a.Task] with a [a2a.TaskState] for which Terminal() method returns true.
//
// The following code can be used as a streaming implementation template with generateOutputs and toParts missing:
//
//	func Execute(ctx context.Context, reqCtx *RequestContext, queue eventqueue.Queue) error {
//		if reqCtx.StoredTask == nil {
//			event := a2a.NewStatusUpdateEvent(reqCtx, a2a.TaskStateSubmitted, nil)
//			if err := queue.Write(ctx, event); err != nil {
//				return fmt.Errorf("failed to write state submitted: %w", err)
//			}
//		}
//
//		// perform setup
//
//		event := a2a.NewStatusUpdateEvent(reqCtx, a2a.TaskStateWorking, nil)
//		if err := queue.Write(ctx, event); err != nil {
//			return fmt.Errorf("failed to write state working: %w", err)
//		}
//
//		var artifactID a2a.ArtifactID
//		for output, err := range generateOutputs() {
//			if err != nil {
//				event := a2a.NewStatusUpdateEvent(reqCtx, a2a.TaskStateFailed, toErrorMessage(err))
//				if err := queue.Write(ctx, event); err != nil {
//					return fmt.Errorf("failed to write state failed: %w", err)
//				}
//			}
//
//			parts := toParts(output)
//			var event *a2a.TaskArtifactUpdateEvent
//			if artifactID == "" {
//				event = a2a.NewArtifactEvent(reqCtx, parts...)
//				artifactID = event.Artifact.ID
//			} else {
//				event = a2a.NewArtifactUpdateEvent(reqCtx, artifactID, parts...)
//			}
//
//			if err := queue.Write(ctx, event); err != nil {
//				return fmt.Errorf("failed to write artifact update: %w", err)
//			}
//		}
//
//		event = a2a.NewStatusUpdateEvent(reqCtx, a2a.TaskStateCompleted, nil)
//		event.Final = true
//		if err := queue.Write(ctx, event); err != nil {
//			return fmt.Errorf("failed to write state working: %w", err)
//		}
//
//		return nil
//	}
type AgentExecutor interface {
	// Execute invokes the agent passing information about the request which triggered execution,
	// translates agent outputs to A2A events and writes them to the event queue.
	// Every invocation runs in a dedicated goroutine.
	//
	// Failures should generally be reported by writing events carrying the cancelation information
	// and task state. An error should be returned in special cases like a failure to write an event.
	Execute(ctx context.Context, reqCtx *RequestContext, queue eventqueue.Queue) error

	// Cancel is called when a client requests the agent to stop working on a task.
	// The simplest implementation can write a cancelation event to the queue and let
	// it be processed by the A2A server. If the events gets applied during an active execution the execution
	// Context gets canceled.
	//
	// An an error should be returned if the cancelation request cannot be processed or a queue write failed.
	Cancel(ctx context.Context, reqCtx *RequestContext, queue eventqueue.Queue) error
}

type factory struct {
	taskStore       TaskStore
	pushSender      PushSender
	pushConfigStore PushConfigStore
	agent           AgentExecutor
	interceptors    []RequestContextInterceptor
}

var _ taskexec.Factory = (*factory)(nil)

func (f *factory) CreateExecutor(ctx context.Context, tid a2a.TaskID, params *a2a.MessageSendParams) (taskexec.Executor, taskexec.Processor, error) {
	execCtx, err := f.loadExecutionContext(ctx, tid, params)
	if err != nil {
		return nil, nil, err
	}

	if params.Config != nil && params.Config.PushConfig != nil {
		if f.pushConfigStore == nil || f.pushSender == nil {
			return nil, nil, a2a.ErrPushNotificationNotSupported
		}
		if _, err := f.pushConfigStore.Save(ctx, tid, params.Config.PushConfig); err != nil {
			return nil, nil, fmt.Errorf("failed to save %v: %w", params.Config.PushConfig, err)
		}
	}

	executor := &executor{agent: f.agent, reqCtx: execCtx.reqCtx, interceptors: f.interceptors}
	processor := newProcessor(
		taskupdate.NewManager(f.taskStore, execCtx.task),
		f.pushConfigStore,
		f.pushSender,
		execCtx.reqCtx,
	)
	return executor, processor, nil
}

type executionContext struct {
	reqCtx *RequestContext
	task   *taskupdate.VersionedTask
}

// loadExecutionContext returns the information necessary for creating agent executor and agent event processor.
func (f *factory) loadExecutionContext(ctx context.Context, tid a2a.TaskID, params *a2a.MessageSendParams) (*executionContext, error) {
	msg := params.Message

	if msg.TaskID == "" {
		contextID := msg.ContextID
		if contextID == "" {
			contextID = a2a.NewContextID()
		}
		reqCtx := &RequestContext{
			Message:   msg,
			TaskID:    tid,
			ContextID: contextID,
			Metadata:  msg.Metadata,
		}
		return &executionContext{
			reqCtx: reqCtx,
			task: &taskupdate.VersionedTask{
				Task:    a2a.NewSubmittedTask(reqCtx, msg),
				Version: a2a.TaskVersionMissing,
			},
		}, nil
	}

	if msg.TaskID != tid {
		return nil, fmt.Errorf("bug: message task id different from executor task id")
	}

	storedTask, lastVersion, err := f.taskStore.Get(ctx, msg.TaskID)
	if err != nil {
		return nil, fmt.Errorf("task loading failed: %w", err)
	}
	if storedTask == nil {
		return nil, a2a.ErrTaskNotFound
	}

	if msg.ContextID != "" && msg.ContextID != storedTask.ContextID {
		return nil, fmt.Errorf("message contextID different from task contextID: %w", a2a.ErrInvalidParams)
	}

	if storedTask.Status.State.Terminal() {
		return nil, fmt.Errorf("task in a terminal state %q: %w", storedTask.Status.State, a2a.ErrInvalidParams)
	}

	storedTask.History = append(storedTask.History, msg)
	lastVersion, err = f.taskStore.Save(ctx, storedTask, nil, lastVersion)
	if err != nil {
		return nil, fmt.Errorf("task message history update failed: %w", err)
	}

	return &executionContext{
		task: &taskupdate.VersionedTask{
			Task:    storedTask,
			Version: lastVersion,
		},
		reqCtx: &RequestContext{
			Message:    msg,
			StoredTask: storedTask,
			TaskID:     storedTask.ID,
			ContextID:  storedTask.ContextID,
			Metadata:   msg.Metadata,
		},
	}, nil
}

func (f *factory) CreateCanceler(ctx context.Context, params *a2a.TaskIDParams) (taskexec.Canceler, taskexec.Processor, error) {
	task, version, err := f.taskStore.Get(ctx, params.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load a task: %w", err)
	}

	if task.Status.State.Terminal() && task.Status.State != a2a.TaskStateCanceled {
		return nil, nil, fmt.Errorf("task in non-cancelable state %s: %w", task.Status.State, a2a.ErrTaskNotCancelable)
	}

	reqCtx := &RequestContext{
		TaskID:     task.ID,
		StoredTask: task,
		ContextID:  task.ContextID,
		Metadata:   params.Metadata,
	}

	canceler := &canceler{agent: f.agent, reqCtx: reqCtx, task: task, interceptors: f.interceptors}
	updateManager := taskupdate.NewManager(f.taskStore, &taskupdate.VersionedTask{Task: task, Version: version})
	processor := newProcessor(updateManager, f.pushConfigStore, f.pushSender, reqCtx)
	return canceler, processor, nil
}

type executor struct {
	agent        AgentExecutor
	reqCtx       *RequestContext
	interceptors []RequestContextInterceptor
}

func (e *executor) Execute(ctx context.Context, q eventqueue.Queue) error {
	var err error
	for _, interceptor := range e.interceptors {
		ctx, err = interceptor.Intercept(ctx, e.reqCtx)
		if err != nil {
			return fmt.Errorf("interceptor failed: %w", err)
		}
	}
	return e.agent.Execute(ctx, e.reqCtx, q)
}

type canceler struct {
	agent        AgentExecutor
	task         *a2a.Task
	reqCtx       *RequestContext
	interceptors []RequestContextInterceptor
}

func (c *canceler) Cancel(ctx context.Context, q eventqueue.Queue) error {
	if c.task.Status.State == a2a.TaskStateCanceled {
		return q.Write(ctx, c.task)
	}

	var err error
	for _, interceptor := range c.interceptors {
		ctx, err = interceptor.Intercept(ctx, c.reqCtx)
		if err != nil {
			return fmt.Errorf("interceptor failed: %w", err)
		}
	}

	return c.agent.Cancel(ctx, c.reqCtx, q)
}

type processor struct {
	// Processor is running in event consumer goroutine, but request context loading
	// happens in event consumer goroutine. Once request context is loaded and validate the processor
	// gets initialized.
	updateManager   *taskupdate.Manager
	pushConfigStore PushConfigStore
	pushSender      PushSender
	reqCtx          *RequestContext

	processedCount int
}

func newProcessor(updateManager *taskupdate.Manager, store PushConfigStore, sender PushSender, reqCtx *RequestContext) *processor {
	return &processor{
		updateManager:   updateManager,
		pushConfigStore: store,
		pushSender:      sender,
		reqCtx:          reqCtx,
	}
}

// Process implements taskexec.Processor interface method.
// A (nil, nil) result means the processing should continue.
// A non-nill result becomes the result of the execution.
func (p *processor) Process(ctx context.Context, event a2a.Event) (*taskexec.ProcessorResult, error) {
	// TODO(yarolegovich): handle invalid event sequence where a Message is produced after a Task was created
	if msg, ok := event.(*a2a.Message); ok {
		return &taskexec.ProcessorResult{ExecutionResult: msg}, nil
	}

	versioned, err := p.updateManager.Process(ctx, event)
	if err != nil {
		return p.setTaskFailed(ctx, event, err)
	}

	task := versioned.Task
	if err := p.sendPushNotifications(ctx, task); err != nil {
		return p.setTaskFailed(ctx, event, err)
	}

	if task.Status.State == a2a.TaskStateUnknown {
		return nil, fmt.Errorf("unknown task state: %s", task.Status.State)
	}

	result := &taskexec.ProcessorResult{TaskVersion: versioned.Version}
	if taskupdate.IsFinal(event) {
		result.ExecutionResult = task
	}
	return result, nil
}

// ProcessError implements taskexec.ProcessError interface method.
// Here we can try handling producer or queue error by moving the task to failed state and making it the execution result.
func (p *processor) ProcessError(ctx context.Context, cause error) (a2a.SendMessageResult, error) {
	if p.reqCtx.StoredTask == nil && p.processedCount == 0 {
		// there was no task in the store, don't create one in failed state and allow to error to be propagated to the client
		return nil, cause
	}

	versioned, err := p.updateManager.SetTaskFailed(ctx, nil, cause)
	if err != nil {
		return nil, err
	}
	return versioned.Task, nil
}

func (p *processor) setTaskFailed(ctx context.Context, event a2a.Event, err error) (*taskexec.ProcessorResult, error) {
	versioned, err := p.updateManager.SetTaskFailed(ctx, event, err)
	if err != nil {
		return nil, err
	}
	return &taskexec.ProcessorResult{
		ExecutionResult: versioned.Task,
		TaskVersion:     versioned.Version,
	}, nil
}

func (p *processor) sendPushNotifications(ctx context.Context, task *a2a.Task) error {
	if p.pushSender == nil || p.pushConfigStore == nil {
		return nil
	}

	configs, err := p.pushConfigStore.List(ctx, task.ID)
	if err != nil {
		return err
	}

	// TODO(yarolegovich): consider dispatching in parallel with max concurrent calls cap
	for _, config := range configs {
		if err := p.pushSender.SendPush(ctx, config, task); err != nil {
			return err
		}
	}
	return nil
}
