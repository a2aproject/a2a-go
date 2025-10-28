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

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
	"github.com/a2aproject/a2a-go/a2asrv/push"
	"github.com/a2aproject/a2a-go/internal/taskexec"
	"github.com/a2aproject/a2a-go/internal/taskstore"
)

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

	// GetAgentCard returns an extended [a2a.AgentCard] if configured.
	OnGetExtendedAgentCard(ctx context.Context) (*a2a.AgentCard, error)
}

// Implements a2asrv.RequestHandler
type defaultRequestHandler struct {
	agentExecutor AgentExecutor
	execManager   *taskexec.Manager

	pushNotifier PushNotifier
	queueManager eventqueue.Manager

	pushConfigStore        PushConfigStore
	taskStore              TaskStore
	reqContextInterceptors []RequestContextInterceptor

	authenticatedCardProducer AgentCardProducer
}

type RequestHandlerOption func(*defaultRequestHandler)

type HTTPPushConfig push.HTTPSenderConfig

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

// WithPushNotifications adds support for push notifications.
func WithPushNotifications(store PushConfigStore, notifier PushNotifier) RequestHandlerOption {
	return func(h *defaultRequestHandler) {
		h.pushConfigStore = store
		h.pushNotifier = notifier
	}
}

// WithHTTPPushSender overrides default PushNotifier with an HTTPPushSender configured with the provided config.
func WithHTTPPushSender(config *HTTPPushConfig) RequestHandlerOption {
	return func(h *defaultRequestHandler) {
		h.pushNotifier = push.NewHTTPPushSender((*push.HTTPSenderConfig)(config))
	}
}

// WithRequestContextInterceptor overrides default RequestContextInterceptor with custom implementation
func WithRequestContextInterceptor(interceptor RequestContextInterceptor) RequestHandlerOption {
	return func(h *defaultRequestHandler) {
		h.reqContextInterceptors = append(h.reqContextInterceptors, interceptor)
	}
}

// WithExtendedAgentCard sets a static extended authenticated agent card.
func WithExtendedAgentCard(card *a2a.AgentCard) RequestHandlerOption {
	return func(h *defaultRequestHandler) {
		h.authenticatedCardProducer = AgentCardProducerFn(func(ctx context.Context) (*a2a.AgentCard, error) {
			return card, nil
		})
	}
}

// WithExtendedAgentCardProducer sets a dynamic extended authenticated agent card producer.
func WithExtendedAgentCardProducer(cardProducer AgentCardProducer) RequestHandlerOption {
	return func(h *defaultRequestHandler) {
		h.authenticatedCardProducer = cardProducer
	}
}

// NewHandler creates a new request handler
func NewHandler(executor AgentExecutor, options ...RequestHandlerOption) RequestHandler {
	h := &defaultRequestHandler{
		agentExecutor: executor,
		queueManager:  eventqueue.NewInMemoryManager(),
		taskStore:     taskstore.NewMem(),
		// push notifications are not supported by default
	}

	for _, option := range options {
		option(h)
	}

	h.execManager = taskexec.NewManager(h.queueManager)

	return h
}

func (h *defaultRequestHandler) OnGetTask(ctx context.Context, query *a2a.TaskQueryParams) (*a2a.Task, error) {
	taskID := query.ID
	if taskID == "" {
		return nil, fmt.Errorf("missing TaskID: %w", a2a.ErrInvalidRequest)
	}

	task, err := h.taskStore.Get(ctx, taskID)
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

func (h *defaultRequestHandler) OnCancelTask(ctx context.Context, params *a2a.TaskIDParams) (*a2a.Task, error) {
	if params == nil {
		return nil, a2a.ErrInvalidRequest
	}

	canceler := &canceler{
		processor:    newProcessor(h.pushConfigStore, h.pushNotifier),
		agent:        h.agentExecutor,
		taskStore:    h.taskStore,
		params:       params,
		interceptors: h.reqContextInterceptors,
	}

	result, err := h.execManager.Cancel(ctx, params.ID, canceler)
	if err != nil {
		return nil, fmt.Errorf("failed to cancel: %w", err)
	}

	return result, nil
}

func (h *defaultRequestHandler) OnSendMessage(ctx context.Context, params *a2a.MessageSendParams) (a2a.SendMessageResult, error) {
	execution, subscription, err := h.handleSendMessage(ctx, params)
	if err != nil {
		return nil, err
	}

	for event, err := range subscription.Events(ctx) {
		if err != nil {
			return nil, err
		}
		if taskID, required := isAuthRequired(event); required {
			task, err := h.taskStore.Get(ctx, taskID)
			if err != nil {
				return nil, fmt.Errorf("failed to load task in auth-required state: %w", err)
			}
			return task, nil
		}
	}

	return execution.Result(ctx)
}

func (h *defaultRequestHandler) OnSendMessageStream(ctx context.Context, params *a2a.MessageSendParams) iter.Seq2[a2a.Event, error] {
	_, subscription, err := h.handleSendMessage(ctx, params)

	if err != nil {
		return func(yield func(a2a.Event, error) bool) {
			yield(nil, err)
		}
	}

	return subscription.Events(ctx)
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

func (h *defaultRequestHandler) handleSendMessage(ctx context.Context, params *a2a.MessageSendParams) (*taskexec.Execution, *taskexec.Subscription, error) {
	if params.Message == nil {
		return nil, nil, fmt.Errorf("message is required: %w", a2a.ErrInvalidRequest)
	}

	var taskID a2a.TaskID
	if len(params.Message.TaskID) == 0 {
		taskID = a2a.NewTaskID()
	} else {
		taskID = params.Message.TaskID
	}

	return h.execManager.Execute(ctx, taskID, &executor{
		processor:       newProcessor(h.pushConfigStore, h.pushNotifier),
		agent:           h.agentExecutor,
		taskStore:       h.taskStore,
		pushConfigStore: h.pushConfigStore,
		pushNotifier:    h.pushNotifier,
		taskID:          taskID,
		params:          params,
		interceptors:    h.reqContextInterceptors,
	})
}

func (h *defaultRequestHandler) OnGetTaskPushConfig(ctx context.Context, params *a2a.GetTaskPushConfigParams) (*a2a.TaskPushConfig, error) {
	if h.pushConfigStore == nil || h.pushNotifier == nil {
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
	if h.pushConfigStore == nil || h.pushNotifier == nil {
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
	if h.pushConfigStore == nil || h.pushNotifier == nil {
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
	if h.pushConfigStore == nil || h.pushNotifier == nil {
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

func isAuthRequired(event a2a.Event) (a2a.TaskID, bool) {
	switch v := event.(type) {
	case *a2a.Task:
		return v.ID, v.Status.State == a2a.TaskStateAuthRequired
	case *a2a.TaskStatusUpdateEvent:
		return v.TaskID, v.Status.State == a2a.TaskStateAuthRequired
	}
	return "", false
}
