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
	"iter"
	"log/slog"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
	"github.com/a2aproject/a2a-go/a2asrv/limiter"
	"github.com/a2aproject/a2a-go/a2asrv/push"
	"github.com/a2aproject/a2a-go/a2asrv/workqueue"
	"github.com/a2aproject/a2a-go/internal/taskexec"
	"github.com/a2aproject/a2a-go/internal/taskstore"
)

// RequestHandler defines a transport-agnostic interface for handling incoming A2A requests.
type RequestHandler interface {
	// OnGetTask handles the 'tasks/get' protocol method.
	OnGetTask(ctx context.Context, query *a2a.TaskQueryParams) (*a2a.Task, error)

	// OnListTasks handles the 'tasks/list' protocol method.
	OnListTasks(ctx context.Context, req *a2a.ListTasksRequest) (*a2a.ListTasksResponse, error)

	// OnCancelTask handles the 'tasks/cancel' protocol method.
	OnCancelTask(ctx context.Context, id *a2a.TaskIDParams) (*a2a.Task, error)

	// OnSendMessage handles the 'message/send' protocol method (non-streaming).
	OnSendMessage(ctx context.Context, message *a2a.MessageSendParams) (a2a.SendMessageResult, error)

	// OnResubscribeToTask handles the `tasks/resubscribe` protocol method.
	OnResubscribeToTask(ctx context.Context, id *a2a.TaskIDParams) iter.Seq2[a2a.Event, error]

	// OnSendMessageStream handles the 'message/stream' protocol method (streaming).
	OnSendMessageStream(ctx context.Context, message *a2a.MessageSendParams) iter.Seq2[a2a.Event, error]

	// OnGetTaskPushConfig handles the `tasks/pushNotificationConfig/get` protocol method.
	OnGetTaskPushConfig(ctx context.Context, params *a2a.GetTaskPushConfigParams) (*a2a.TaskPushConfig, error)

	// OnListTaskPushConfig handles the `tasks/pushNotificationConfig/list` protocol method.
	OnListTaskPushConfig(ctx context.Context, params *a2a.ListTaskPushConfigParams) ([]*a2a.TaskPushConfig, error)

	// OnSetTaskPushConfig handles the `tasks/pushNotificationConfig/set` protocol method.
	OnSetTaskPushConfig(ctx context.Context, params *a2a.TaskPushConfig) (*a2a.TaskPushConfig, error)

	// OnDeleteTaskPushConfig handles the `tasks/pushNotificationConfig/delete` protocol method.
	OnDeleteTaskPushConfig(ctx context.Context, params *a2a.DeleteTaskPushConfigParams) error

	// GetAgentCard returns an extended a2a.AgentCard if configured.
	OnGetExtendedAgentCard(ctx context.Context) (*a2a.AgentCard, error)
}

// Implements a2asrv.RequestHandler.
type defaultRequestHandler struct {
	agentExecutor AgentExecutor
	execManager   taskexec.Manager

	pushSender        PushSender
	queueManager      eventqueue.Manager
	concurrencyConfig limiter.ConcurrencyConfig

	pushConfigStore        PushConfigStore
	taskStore              TaskStore
	workQueue              workqueue.Queue
	reqContextInterceptors []RequestContextInterceptor

	authenticatedCardProducer AgentCardProducer
}

var _ RequestHandler = (*defaultRequestHandler)(nil)

// RequestHandlerOption can be used to customize the default [RequestHandler] implementation behavior.
type RequestHandlerOption func(*InterceptedHandler, *defaultRequestHandler)

// WithLogger sets a custom logger. Request scoped parameters will be attached to this logger
// on method invocations. Any injected dependency will be able to access the logger using
// [github.com/a2aproject/a2a-go/log] package-level functions.
// If not provided, defaults to slog.Default().
func WithLogger(logger *slog.Logger) RequestHandlerOption {
	return func(ih *InterceptedHandler, h *defaultRequestHandler) {
		ih.Logger = logger
	}
}

// WithEventQueueManager overrides eventqueue.Manager with custom implementation
func WithEventQueueManager(manager eventqueue.Manager) RequestHandlerOption {
	return func(ih *InterceptedHandler, h *defaultRequestHandler) {
		h.queueManager = manager
	}
}

// WithConcurrencyConfig allows to set limits on the number of concurrent executions.
func WithConcurrencyConfig(config limiter.ConcurrencyConfig) RequestHandlerOption {
	return func(ih *InterceptedHandler, h *defaultRequestHandler) {
		h.concurrencyConfig = config
	}
}

// ClusterConfig groups the necessary dependencies for A2A cluster mode operation.
type ClusterConfig struct {
	QueueManager eventqueue.Manager
	WorkQueue    workqueue.Queue
	TaskStore    TaskStore
}

// WithClusterMode is an experimental feature where work queue is used to distribute tasks across multiple instances.
func WithClusterMode(config ClusterConfig) RequestHandlerOption {
	return func(ih *InterceptedHandler, h *defaultRequestHandler) {
		h.workQueue = config.WorkQueue
		h.taskStore = config.TaskStore
		h.queueManager = config.QueueManager
	}
}

// NewHandler creates a new request handler.
func NewHandler(executor AgentExecutor, options ...RequestHandlerOption) RequestHandler {
	h := &defaultRequestHandler{agentExecutor: executor}
	ih := &InterceptedHandler{Handler: h, Logger: slog.Default()}

	for _, option := range options {
		option(ih, h)
	}

	execFactory := &factory{
		agent:           h.agentExecutor,
		taskStore:       h.taskStore,
		pushSender:      h.pushSender,
		pushConfigStore: h.pushConfigStore,
		interceptors:    h.reqContextInterceptors,
	}
	if h.workQueue != nil {
		if h.taskStore == nil || h.queueManager == nil {
			panic("TaskStore and QueueManager must be provided for cluster mode")
		}
		h.execManager = taskexec.NewDistributedManager(&taskexec.DistributedManagerConfig{
			WorkQueue:         h.workQueue,
			TaskStore:         h.taskStore,
			QueueManager:      h.queueManager,
			ConcurrencyConfig: h.concurrencyConfig,
			Factory:           execFactory,
		})
	} else {
		if h.queueManager == nil {
			h.queueManager = eventqueue.NewInMemoryManager()
		}
		if h.taskStore == nil {
			h.taskStore = taskstore.NewMem()
			execFactory.taskStore = h.taskStore
		}
		h.execManager = taskexec.NewLocalManager(taskexec.LocalManagerConfig{
			QueueManager:      h.queueManager,
			ConcurrencyConfig: h.concurrencyConfig,
			Factory:           execFactory,
		})
	}

	return ih
}

func (h *defaultRequestHandler) OnGetTask(ctx context.Context, query *a2a.TaskQueryParams) (*a2a.Task, error) {
	taskID := query.ID
	if taskID == "" {
		return nil, fmt.Errorf("missing TaskID: %w", a2a.ErrInvalidParams)
	}

	task, _, err := h.taskStore.Get(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("failed to get task: %w", err)
	}

	if query.HistoryLength != nil {
		historyLength := *query.HistoryLength

		if historyLength <= 0 {
			task.History = []*a2a.Message{}
		} else if historyLength < len(task.History) {
			task.History = task.History[len(task.History)-historyLength:]
		}
	}

	return task, nil
}

func (h *defaultRequestHandler) OnListTasks(ctx context.Context, req *a2a.ListTasksRequest) (*a2a.ListTasksResponse, error) {
	listResponse, err := h.taskStore.List(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to list tasks: %w", err)
	}
	return listResponse, nil
}

func (h *defaultRequestHandler) OnCancelTask(ctx context.Context, params *a2a.TaskIDParams) (*a2a.Task, error) {
	if params == nil {
		return nil, a2a.ErrInvalidParams
	}

	response, err := h.execManager.Cancel(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to cancel: %w", err)
	}
	return response, nil
}

func (h *defaultRequestHandler) OnSendMessage(ctx context.Context, params *a2a.MessageSendParams) (a2a.SendMessageResult, error) {
	subscription, err := h.handleSendMessage(ctx, params)
	if err != nil {
		return nil, err
	}

	var lastEvent a2a.Event
	for event, err := range subscription.Events(ctx) {
		if err != nil {
			return nil, err
		}

		if taskID, interrupt := shouldInterruptNonStreaming(params, event); interrupt {
			task, _, err := h.taskStore.Get(ctx, taskID)
			if err != nil {
				return nil, fmt.Errorf("failed to load task on event processing interrupt: %w", err)
			}
			return task, nil
		}
		lastEvent = event
	}

	if res, ok := lastEvent.(a2a.SendMessageResult); ok {
		return res, nil
	}

	task, _, err := h.taskStore.Get(ctx, lastEvent.TaskInfo().TaskID)
	if err != nil {
		return nil, fmt.Errorf("failed to load result after execution finished: %w", err)
	}
	return task, nil
}

func (h *defaultRequestHandler) OnSendMessageStream(ctx context.Context, params *a2a.MessageSendParams) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		subscription, err := h.handleSendMessage(ctx, params)
		if err != nil {
			yield(nil, err)
			return
		}

		for ev, err := range subscription.Events(ctx) {
			if !yield(ev, err) {
				return
			}
		}
	}
}

func (h *defaultRequestHandler) OnResubscribeToTask(ctx context.Context, params *a2a.TaskIDParams) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		if params == nil {
			yield(nil, a2a.ErrInvalidParams)
			return
		}

		subscription, err := h.execManager.Resubscribe(ctx, params.ID)
		if err != nil {
			yield(nil, fmt.Errorf("%w: %w", a2a.ErrTaskNotFound, err))
			return
		}

		for ev, err := range subscription.Events(ctx) {
			if !yield(ev, err) {
				return
			}
		}
	}
}

func (h *defaultRequestHandler) handleSendMessage(ctx context.Context, params *a2a.MessageSendParams) (taskexec.Subscription, error) {
	if params == nil || params.Message == nil {
		return nil, fmt.Errorf("message is required: %w", a2a.ErrInvalidParams)
	}
	return h.execManager.Execute(ctx, params)
}

func (h *defaultRequestHandler) OnGetTaskPushConfig(ctx context.Context, params *a2a.GetTaskPushConfigParams) (*a2a.TaskPushConfig, error) {
	if h.pushConfigStore == nil || h.pushSender == nil {
		return nil, a2a.ErrPushNotificationNotSupported
	}
	config, err := h.pushConfigStore.Get(ctx, params.TaskID, params.ConfigID)
	if err != nil {
		return nil, fmt.Errorf("failed to get push configs: %w", err)
	}
	if config != nil {
		return &a2a.TaskPushConfig{
			TaskID: params.TaskID,
			Config: *config,
		}, nil
	}
	return nil, push.ErrPushConfigNotFound
}

func (h *defaultRequestHandler) OnListTaskPushConfig(ctx context.Context, params *a2a.ListTaskPushConfigParams) ([]*a2a.TaskPushConfig, error) {
	if h.pushConfigStore == nil || h.pushSender == nil {
		return nil, a2a.ErrPushNotificationNotSupported
	}
	configs, err := h.pushConfigStore.List(ctx, params.TaskID)
	if err != nil {
		return nil, fmt.Errorf("failed to list push configs: %w", err)
	}
	result := make([]*a2a.TaskPushConfig, len(configs))
	for i, config := range configs {
		result[i] = &a2a.TaskPushConfig{
			TaskID: params.TaskID,
			Config: *config,
		}
	}
	return result, nil
}

func (h *defaultRequestHandler) OnSetTaskPushConfig(ctx context.Context, params *a2a.TaskPushConfig) (*a2a.TaskPushConfig, error) {
	if h.pushConfigStore == nil || h.pushSender == nil {
		return nil, a2a.ErrPushNotificationNotSupported
	}
	saved, err := h.pushConfigStore.Save(ctx, params.TaskID, &params.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to save push config: %w", err)
	}
	return &a2a.TaskPushConfig{
		TaskID: params.TaskID,
		Config: *saved,
	}, nil
}

func (h *defaultRequestHandler) OnDeleteTaskPushConfig(ctx context.Context, params *a2a.DeleteTaskPushConfigParams) error {
	if h.pushConfigStore == nil || h.pushSender == nil {
		return a2a.ErrPushNotificationNotSupported
	}
	return h.pushConfigStore.Delete(ctx, params.TaskID, params.ConfigID)
}

func (h *defaultRequestHandler) OnGetExtendedAgentCard(ctx context.Context) (*a2a.AgentCard, error) {
	if h.authenticatedCardProducer == nil {
		return nil, a2a.ErrAuthenticatedExtendedCardNotConfigured
	}
	return h.authenticatedCardProducer.Card(ctx)
}

func shouldInterruptNonStreaming(params *a2a.MessageSendParams, event a2a.Event) (a2a.TaskID, bool) {
	// Non-blocking clients receive a result on the first task event, default Blocking to TRUE
	if params.Config != nil && params.Config.Blocking != nil && !(*params.Config.Blocking) {
		if _, ok := event.(*a2a.Message); ok {
			return "", false
		}
		taskInfo := event.TaskInfo()
		return taskInfo.TaskID, true
	}

	// Non-streaming clients need to be notified when auth is required
	switch v := event.(type) {
	case *a2a.Task:
		return v.ID, v.Status.State == a2a.TaskStateAuthRequired
	case *a2a.TaskStatusUpdateEvent:
		return v.TaskID, v.Status.State == a2a.TaskStateAuthRequired
	}

	return "", false
}
