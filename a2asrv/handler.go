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
	"iter"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
	"github.com/a2aproject/a2a-go/internal/taskexec"
	"github.com/a2aproject/a2a-go/internal/taskstore"
)

var ErrUnimplemented = errors.New("unimplemented")

// RequestHandler defines a transport-agnostic interface for handling incoming A2A requests.
type RequestHandler interface {
	// OnGetTask handles the 'tasks/get' protocol method.
	OnGetTask(ctx context.Context, query *a2a.TaskQueryParams) (*a2a.Task, error)

	// OnCancelTask handles the 'tasks/cancel' protocol method.
	OnCancelTask(ctx context.Context, id *a2a.TaskIDParams) (*a2a.Task, error)

	// OnSendMessage handles the 'message/send' protocol method (non-streaming).
	OnSendMessage(ctx context.Context, message *a2a.MessageSendParams) (a2a.SendMessageResult, error)

	// OnResubscribeToTask handles the `tasks/resubscribe` protocol method.
	OnResubscribeToTask(ctx context.Context, id *a2a.TaskIDParams) iter.Seq2[a2a.Event, error]

	// OnMessageSendStream handles the 'message/stream' protocol method (streaming).
	OnSendMessageStream(ctx context.Context, message *a2a.MessageSendParams) iter.Seq2[a2a.Event, error]

	// OnGetTaskPushNotificationConfig handles the `tasks/pushNotificationConfig/get` protocol method.
	OnGetTaskPushConfig(ctx context.Context, params *a2a.GetTaskPushConfigParams) (*a2a.TaskPushConfig, error)

	// OnListTaskPushNotificationConfig handles the `tasks/pushNotificationConfig/list` protocol method.
	OnListTaskPushConfig(ctx context.Context, params *a2a.ListTaskPushConfigParams) ([]*a2a.TaskPushConfig, error)

	// OnSetTaskPushConfig handles the `tasks/pushNotificationConfig/set` protocol method.
	OnSetTaskPushConfig(ctx context.Context, params *a2a.TaskPushConfig) (*a2a.TaskPushConfig, error)

	// OnDeleteTaskPushNotificationConfig handles the `tasks/pushNotificationConfig/delete` protocol method.
	OnDeleteTaskPushConfig(ctx context.Context, params *a2a.DeleteTaskPushConfigParams) error
}

// Implements a2asrv.RequestHandler
type defaultRequestHandler struct {
	agentExecutor AgentExecutor
	execManager   *taskexec.Manager

	pushNotifier PushNotifier
	queueManager eventqueue.Manager

	pushConfigStore       PushConfigStore
	taskStore             TaskStore
	reqContextInterceptor RequestContextInterceptor
}

type RequestHandlerOption func(*defaultRequestHandler)

// WithTaskStore overrides TaskStore with custom implementation
func WithTaskStore(store TaskStore) RequestHandlerOption {
	return func(h *defaultRequestHandler) {
		h.taskStore = store
	}
}

// WithEventQueueManager overrides eventqueue.Manager with custom implementation
func WithEventQueueManager(manager eventqueue.Manager) RequestHandlerOption {
	return func(h *defaultRequestHandler) {
		h.queueManager = manager
	}
}

// WithPushConfigStore overrides default PushConfigStore with custom implementation
func WithPushConfigStore(store PushConfigStore) RequestHandlerOption {
	return func(h *defaultRequestHandler) {
		h.pushConfigStore = store
	}
}

// WithPushNotifier overrides default PushNotifier with custom implementation
func WithPushNotifier(notifier PushNotifier) RequestHandlerOption {
	return func(h *defaultRequestHandler) {
		h.pushNotifier = notifier
	}
}

// WithRequestContextInterceptor overrides default RequestContextInterceptor with custom implementation
func WithRequestContextInterceptor(interceptor RequestContextInterceptor) RequestHandlerOption {
	return func(h *defaultRequestHandler) {
		h.reqContextInterceptor = interceptor
	}
}

// NewHandler creates a new request handler
func NewHandler(executor AgentExecutor, options ...RequestHandlerOption) RequestHandler {
	h := &defaultRequestHandler{
		agentExecutor:         executor,
		queueManager:          eventqueue.NewInMemoryManager(),
		taskStore:             taskstore.NewMem(),
		reqContextInterceptor: &DefaultRequestContextInterceptor{},
	}

	for _, option := range options {
		option(h)
	}

	h.execManager = taskexec.NewManager(h.queueManager)
	if interceptor, ok := h.reqContextInterceptor.(*DefaultRequestContextInterceptor); ok {
		interceptor.taskStore = h.taskStore
	}

	return h
}

func (h *defaultRequestHandler) OnGetTask(ctx context.Context, query *a2a.TaskQueryParams) (*a2a.Task, error) {
	return nil, ErrUnimplemented
}

// TODO(yarolegovich): add tests in https://github.com/a2aproject/a2a-go/issues/21
func (h *defaultRequestHandler) OnCancelTask(ctx context.Context, params *a2a.TaskIDParams) (*a2a.Task, error) {
	canceler := &canceler{
		agent:       h.agentExecutor,
		taskStore:   h.taskStore,
		taskID:      params.ID,
		interceptor: h.reqContextInterceptor,
		params:      params,
	}

	result, err := h.execManager.Cancel(ctx, params.ID, canceler)
	if err != nil {
		return nil, fmt.Errorf("failed to cancel: %w", err)
	}

	return result, nil
}

func (h *defaultRequestHandler) OnSendMessage(ctx context.Context, params *a2a.MessageSendParams) (a2a.SendMessageResult, error) {
	execution, err := h.handleSendMessage(ctx, params)
	if err != nil {
		return nil, err
	}

	for event, err := range execution.Events(ctx) {
		if err != nil {
			return nil, err
		}
		if shouldInterrupt(event) {
			return event.(a2a.SendMessageResult), nil
		}
	}

	return execution.Result(ctx)
}

func (h *defaultRequestHandler) OnSendMessageStream(ctx context.Context, params *a2a.MessageSendParams) iter.Seq2[a2a.Event, error] {
	execution, err := h.handleSendMessage(ctx, params)
	if err != nil {
		return func(yield func(a2a.Event, error) bool) {
			yield(nil, err)
		}
	}
	return execution.Events(ctx)
}

func (h *defaultRequestHandler) OnResubscribeToTask(ctx context.Context, params *a2a.TaskIDParams) iter.Seq2[a2a.Event, error] {
	exec, ok := h.execManager.GetExecution(params.ID)
	if !ok {
		return func(yield func(a2a.Event, error) bool) {
			yield(nil, a2a.ErrTaskNotFound)
		}
	}
	return exec.Events(ctx)
}

func (h *defaultRequestHandler) handleSendMessage(ctx context.Context, params *a2a.MessageSendParams) (*taskexec.Execution, error) {
	if params.Message == nil {
		return nil, fmt.Errorf("message is required: %w", a2a.ErrInvalidRequest)
	}

	var taskID a2a.TaskID
	if len(params.Message.TaskID) == 0 {
		taskID = a2a.NewTaskID()
	} else {
		taskID = params.Message.TaskID
	}

	return h.execManager.Execute(ctx, taskID, &executor{
		agent:           h.agentExecutor,
		taskStore:       h.taskStore,
		pushConfigStore: h.pushConfigStore,
		taskID:          taskID,
		params:          params,
		interceptor:     h.reqContextInterceptor,
	})
}

func (h *defaultRequestHandler) OnGetTaskPushConfig(ctx context.Context, params *a2a.GetTaskPushConfigParams) (*a2a.TaskPushConfig, error) {
	return nil, ErrUnimplemented
}

func (h *defaultRequestHandler) OnListTaskPushConfig(ctx context.Context, params *a2a.ListTaskPushConfigParams) ([]*a2a.TaskPushConfig, error) {
	return nil, ErrUnimplemented
}

func (h *defaultRequestHandler) OnSetTaskPushConfig(ctx context.Context, params *a2a.TaskPushConfig) (*a2a.TaskPushConfig, error) {
	return nil, ErrUnimplemented
}

func (h *defaultRequestHandler) OnDeleteTaskPushConfig(ctx context.Context, params *a2a.DeleteTaskPushConfigParams) error {
	return ErrUnimplemented
}

// TODO(yarolegovich): handle auth-required state
func shouldInterrupt(_ a2a.Event) bool {
	return false
}
