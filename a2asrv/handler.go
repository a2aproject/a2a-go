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
	"log/slog"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
	"github.com/a2aproject/a2a-go/internal/taskexec"
	"github.com/a2aproject/a2a-go/internal/taskstore"
	"github.com/a2aproject/a2a-go/log"
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
	logger                    *slog.Logger
}

type RequestHandlerOption func(*defaultRequestHandler)

// WithTaskStore overrides TaskStore with custom implementation
func WithTaskStore(store TaskStore) RequestHandlerOption {
	return func(h *defaultRequestHandler) {
		h.taskStore = store
	}
}

// WithLogger sets a custom [slog.Logger]. Request scoped parameters will be attached to this logger
// on method invocations. Any injected dependency will be able to access the logger using either
// github.com/a2aproject/a2a-go/log package-level functions.
// If not provided, defaults to slog.Default().
func WithLogger(logger *slog.Logger) RequestHandlerOption {
	return func(h *defaultRequestHandler) {
		h.logger = logger
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
		logger:        slog.Default(),
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
		processor:    newProcessor(),
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
	// TODO(yarolegovich): attach request context values logger.With("task_id", taskID, ...) and add it to other methods
	ctx = log.WithLogger(ctx, h.logger)
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
		processor:       newProcessor(),
		agent:           h.agentExecutor,
		taskStore:       h.taskStore,
		pushConfigStore: h.pushConfigStore,
		taskID:          taskID,
		params:          params,
		interceptors:    h.reqContextInterceptors,
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
