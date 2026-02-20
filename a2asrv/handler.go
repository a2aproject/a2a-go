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
	"github.com/a2aproject/a2a-go/a2asrv/taskstore"
	"github.com/a2aproject/a2a-go/a2asrv/workqueue"
	"github.com/a2aproject/a2a-go/internal/taskexec"
)

// RequestHandler defines a transport-agnostic interface for handling incoming A2A requests.
type RequestHandler interface {
	// GetTask handles the 'tasks/get' protocol method.
	GetTask(context.Context, *a2a.GetTaskRequest) (*a2a.Task, error)

	// ListTasks handles the 'tasks/list' protocol method.
	ListTasks(context.Context, *a2a.ListTasksRequest) (*a2a.ListTasksResponse, error)

	// CancelTask handles the 'tasks/cancel' protocol method.
	CancelTask(context.Context, *a2a.CancelTaskRequest) (*a2a.Task, error)

	// SendMessage handles the 'message/send' protocol method (non-streaming).
	SendMessage(context.Context, *a2a.SendMessageRequest) (a2a.SendMessageResult, error)

	// SubscribeToTask handles the `tasks/resubscribe` protocol method.
	SubscribeToTask(context.Context, *a2a.SubscribeToTaskRequest) iter.Seq2[a2a.Event, error]

	// SendStreamingMessage handles the 'message/stream' protocol method (streaming).
	SendStreamingMessage(context.Context, *a2a.SendMessageRequest) iter.Seq2[a2a.Event, error]

	// GetTaskPushConfig handles the `tasks/pushNotificationConfig/get` protocol method.
	GetTaskPushConfig(context.Context, *a2a.GetTaskPushConfigRequest) (*a2a.TaskPushConfig, error)

	// ListTaskPushConfig handles the `tasks/pushNotificationConfig/list` protocol method.
	ListTaskPushConfig(context.Context, *a2a.ListTaskPushConfigRequest) ([]*a2a.TaskPushConfig, error)

	// CreateTaskPushConfig handles the `tasks/pushNotificationConfig/set` protocol method.
	CreateTaskPushConfig(context.Context, *a2a.CreateTaskPushConfigRequest) (*a2a.TaskPushConfig, error)

	// DeleteTaskPushConfig handles the `tasks/pushNotificationConfig/delete` protocol method.
	DeleteTaskPushConfig(context.Context, *a2a.DeleteTaskPushConfigRequest) error

	// GetAgentCard returns an extended a2a.AgentCard if configured.
	GetExtendedAgentCard(context.Context, *a2a.GetExtendedAgentCardRequest) (*a2a.AgentCard, error)
}

// Implements a2asrv.RequestHandler.
type defaultRequestHandler struct {
	agentExecutor AgentExecutor
	execManager   taskexec.Manager
	panicHandler  taskexec.PanicHandlerFn

	pushSender        push.Sender
	queueManager      eventqueue.Manager
	concurrencyConfig limiter.ConcurrencyConfig

	pushConfigStore        push.ConfigStore
	taskStore              taskstore.Store
	workQueue              workqueue.Queue
	reqContextInterceptors []ExecutorContextInterceptor

	authenticatedCardProducer ExtendedAgentCardProducer
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

// WithExecutionPanicHandler allows to set a custom handler for panics occurred during execution.
func WithExecutionPanicHandler(handler func(r any) error) RequestHandlerOption {
	return func(ih *InterceptedHandler, h *defaultRequestHandler) {
		h.panicHandler = handler
	}
}

// WithConcurrencyConfig allows to set limits on the number of concurrent executions.
func WithConcurrencyConfig(config limiter.ConcurrencyConfig) RequestHandlerOption {
	return func(ih *InterceptedHandler, h *defaultRequestHandler) {
		h.concurrencyConfig = config
	}
}

// WithPushNotifications adds support for push notifications. If dependencies are not provided
// push-related methods will be returning a2a.ErrPushNotificationNotSupported,
func WithPushNotifications(store push.ConfigStore, sender push.Sender) RequestHandlerOption {
	return func(ih *InterceptedHandler, h *defaultRequestHandler) {
		h.pushConfigStore = store
		h.pushSender = sender
	}
}

// WithTaskStore overrides TaskStore with a custom implementation. If not provided,
// default to an in-memory implementation.
func WithTaskStore(store taskstore.Store) RequestHandlerOption {
	return func(ih *InterceptedHandler, h *defaultRequestHandler) {
		h.taskStore = store
	}
}

// ClusterConfig groups the necessary dependencies for A2A cluster mode operation.
type ClusterConfig struct {
	QueueManager eventqueue.Manager
	WorkQueue    workqueue.Queue
	TaskStore    taskstore.Store
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
			PanicHandler:      h.panicHandler,
		})
	} else {
		if h.queueManager == nil {
			h.queueManager = eventqueue.NewInMemoryManager()
		}
		if h.taskStore == nil {
			h.taskStore = taskstore.NewInMemory(&taskstore.InMemoryStoreConfig{
				Authenticator: NewTaskStoreAuthenticator(),
			})
			execFactory.taskStore = h.taskStore
		}
		h.execManager = taskexec.NewLocalManager(taskexec.LocalManagerConfig{
			QueueManager:      h.queueManager,
			ConcurrencyConfig: h.concurrencyConfig,
			Factory:           execFactory,
			PanicHandler:      h.panicHandler,
		})
	}

	return ih
}

// GetTask implements RequestHandler.
func (h *defaultRequestHandler) GetTask(ctx context.Context, req *a2a.GetTaskRequest) (*a2a.Task, error) {
	taskID := req.ID
	if taskID == "" {
		return nil, fmt.Errorf("missing TaskID: %w", a2a.ErrInvalidParams)
	}

	storedTask, err := h.taskStore.Get(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("failed to get task: %w", err)
	}

	task := storedTask.Task
	if req.HistoryLength != nil {
		historyLength := *req.HistoryLength

		if historyLength <= 0 {
			task.History = []*a2a.Message{}
		} else if historyLength < len(task.History) {
			task.History = task.History[len(task.History)-historyLength:]
		}
	}

	return task, nil
}

// ListTasks implements RequestHandler.
func (h *defaultRequestHandler) ListTasks(ctx context.Context, req *a2a.ListTasksRequest) (*a2a.ListTasksResponse, error) {
	listResponse, err := h.taskStore.List(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to list tasks: %w", err)
	}
	return listResponse, nil
}

// CancelTask implements RequestHandler.
func (h *defaultRequestHandler) CancelTask(ctx context.Context, req *a2a.CancelTaskRequest) (*a2a.Task, error) {
	if req == nil {
		return nil, a2a.ErrInvalidParams
	}

	response, err := h.execManager.Cancel(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to cancel: %w", err)
	}
	return response, nil
}

// SendMessage implements RequestHandler.
func (h *defaultRequestHandler) SendMessage(ctx context.Context, req *a2a.SendMessageRequest) (a2a.SendMessageResult, error) {
	subscription, err := h.handleSendMessage(ctx, req)
	if err != nil {
		return nil, err
	}

	var lastEvent a2a.Event
	for event, err := range subscription.Events(ctx) {
		if err != nil {
			return nil, err
		}

		if taskID, interrupt := shouldInterruptNonStreaming(req, event); interrupt {
			storedTask, err := h.taskStore.Get(ctx, taskID)
			if err != nil {
				return nil, fmt.Errorf("failed to load task on event processing interrupt: %w", err)
			}
			return storedTask.Task, nil
		}
		lastEvent = event
	}

	if res, ok := lastEvent.(a2a.SendMessageResult); ok {
		return res, nil
	}

	task, err := h.taskStore.Get(ctx, lastEvent.TaskInfo().TaskID)
	if err != nil {
		return nil, fmt.Errorf("failed to load result after execution finished: %w", err)
	}
	return task.Task, nil
}

// SendStreamingMessage implements RequestHandler.
func (h *defaultRequestHandler) SendStreamingMessage(ctx context.Context, req *a2a.SendMessageRequest) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		subscription, err := h.handleSendMessage(ctx, req)
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

// SubscribeToTask implements RequestHandler.
func (h *defaultRequestHandler) SubscribeToTask(ctx context.Context, req *a2a.SubscribeToTaskRequest) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		if req == nil {
			yield(nil, a2a.ErrInvalidParams)
			return
		}

		subscription, err := h.execManager.Resubscribe(ctx, req.ID)
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

func (h *defaultRequestHandler) handleSendMessage(ctx context.Context, req *a2a.SendMessageRequest) (taskexec.Subscription, error) {
	switch {
	case req == nil:
		return nil, fmt.Errorf("message send params is required: %w", a2a.ErrInvalidParams)
	case req.Message == nil:
		return nil, fmt.Errorf("message is required: %w", a2a.ErrInvalidParams)
	case req.Message.ID == "":
		return nil, fmt.Errorf("message ID is required: %w", a2a.ErrInvalidParams)
	case len(req.Message.Parts) == 0:
		return nil, fmt.Errorf("message parts is required: %w", a2a.ErrInvalidParams)
	case req.Message.Role == "":
		return nil, fmt.Errorf("message role is required: %w", a2a.ErrInvalidParams)
	}
	return h.execManager.Execute(ctx, req)
}

// GetTaskPushConfig implements RequestHandler.
func (h *defaultRequestHandler) GetTaskPushConfig(ctx context.Context, req *a2a.GetTaskPushConfigRequest) (*a2a.TaskPushConfig, error) {
	if h.pushConfigStore == nil || h.pushSender == nil {
		return nil, a2a.ErrPushNotificationNotSupported
	}
	config, err := h.pushConfigStore.Get(ctx, req.TaskID, req.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get push configs: %w", err)
	}
	if config != nil {
		return &a2a.TaskPushConfig{
			TaskID: req.TaskID,
			Config: *config,
		}, nil
	}
	return nil, push.ErrPushConfigNotFound
}

// ListTaskPushConfig implements RequestHandler.
func (h *defaultRequestHandler) ListTaskPushConfig(ctx context.Context, req *a2a.ListTaskPushConfigRequest) ([]*a2a.TaskPushConfig, error) {
	if h.pushConfigStore == nil || h.pushSender == nil {
		return nil, a2a.ErrPushNotificationNotSupported
	}
	configs, err := h.pushConfigStore.List(ctx, req.TaskID)
	if err != nil {
		return nil, fmt.Errorf("failed to list push configs: %w", err)
	}
	result := make([]*a2a.TaskPushConfig, len(configs))
	for i, config := range configs {
		result[i] = &a2a.TaskPushConfig{
			TaskID: req.TaskID,
			Config: *config,
		}
	}
	return result, nil
}

// CreateTaskPushConfig implements RequestHandler.
func (h *defaultRequestHandler) CreateTaskPushConfig(ctx context.Context, req *a2a.CreateTaskPushConfigRequest) (*a2a.TaskPushConfig, error) {
	if h.pushConfigStore == nil || h.pushSender == nil {
		return nil, a2a.ErrPushNotificationNotSupported
	}

	saved, err := h.pushConfigStore.Save(ctx, req.TaskID, &req.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to save push config: %w", err)
	}

	return &a2a.TaskPushConfig{
		TaskID: req.TaskID,
		ID:     saved.ID,
		Config: *saved,
	}, nil
}

// DeleteTaskPushConfig implements RequestHandler.
func (h *defaultRequestHandler) DeleteTaskPushConfig(ctx context.Context, req *a2a.DeleteTaskPushConfigRequest) error {
	if h.pushConfigStore == nil || h.pushSender == nil {
		return a2a.ErrPushNotificationNotSupported
	}
	return h.pushConfigStore.Delete(ctx, req.TaskID, req.ID)
}

// GetExtendedAgentCard implements RequestHandler.
func (h *defaultRequestHandler) GetExtendedAgentCard(ctx context.Context, req *a2a.GetExtendedAgentCardRequest) (*a2a.AgentCard, error) {
	if h.authenticatedCardProducer == nil {
		return nil, a2a.ErrExtendedCardNotConfigured
	}
	return h.authenticatedCardProducer.ExtendedCard(ctx, req)
}

func shouldInterruptNonStreaming(req *a2a.SendMessageRequest, event a2a.Event) (a2a.TaskID, bool) {
	// Non-blocking clients receive a result on the first task event, default Blocking to TRUE
	if req.Config != nil && req.Config.Blocking != nil && !(*req.Config.Blocking) {
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
