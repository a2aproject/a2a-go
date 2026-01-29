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

	"github.com/google/uuid"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/log"
)

// InterceptedHandler implements [RequestHandler]. It can be used to attach call interceptors and initialize
// call context for every method of the wrapped handler.
type InterceptedHandler struct {
	// Handler is responsible for the actual processing of every call.
	Handler RequestHandler
	// Interceptors is a list of call interceptors which will be applied before and after each call.
	Interceptors []CallInterceptor
	// Logger is the logger which will be accessible from request scope context using log package
	// methods. Defaults to slog.Default() if not set.
	Logger *slog.Logger
}

var _ RequestHandler = (*InterceptedHandler)(nil)

func (h *InterceptedHandler) OnGetTask(ctx context.Context, query *a2a.TaskQueryParams) (*a2a.Task, error) {
	var task *a2a.Task
	ctx, callCtx := withMethodCallContext(ctx, "OnGetTask")
	if query != nil {
		ctx = h.withLoggerContext(ctx, slog.String("task_id", string(query.ID)))
	}
	ctx, interceptedQuery, earlyResponse, err := interceptBefore(ctx, h, callCtx, query, task)
	if err != nil || earlyResponse != nil {
		return earlyResponse, err
	}
	response, err := h.Handler.OnGetTask(ctx, interceptedQuery)
	return interceptAfter(ctx, h.Interceptors, callCtx, response, err)
}

func (h *InterceptedHandler) OnCancelTask(ctx context.Context, params *a2a.TaskIDParams) (*a2a.Task, error) {
	var task *a2a.Task
	ctx, callCtx := withMethodCallContext(ctx, "OnCancelTask")
	if params != nil {
		ctx = h.withLoggerContext(ctx, slog.String("task_id", string(params.ID)))
	}
	ctx, interceptedParams, earlyResponse, err := interceptBefore(ctx, h, callCtx, params, task)
	if err != nil || earlyResponse != nil {
		return earlyResponse, err
	}
	response, err := h.Handler.OnCancelTask(ctx, interceptedParams)
	return interceptAfter(ctx, h.Interceptors, callCtx, response, err)
}

func (h *InterceptedHandler) OnSendMessage(ctx context.Context, params *a2a.MessageSendParams) (a2a.SendMessageResult, error) {
	var result a2a.SendMessageResult
	ctx, callCtx := withMethodCallContext(ctx, "OnSendMessage")
	if params != nil && params.Message != nil {
		msg := params.Message
		ctx = h.withLoggerContext(
			ctx,
			slog.String("message_id", msg.ID),
			slog.String("task_id", string(msg.TaskID)),
			slog.String("context_id", msg.ContextID),
		)
	} else {
		ctx = h.withLoggerContext(ctx)
	}
	ctx, interceptedParams, earlyResponse, err := interceptBefore(ctx, h, callCtx, params, result)
	if err != nil || earlyResponse != nil {
		return earlyResponse, err
	}
	response, err := h.Handler.OnSendMessage(ctx, interceptedParams)
	return interceptAfter(ctx, h.Interceptors, callCtx, response, err)
}

func (h *InterceptedHandler) OnSendMessageStream(ctx context.Context, params *a2a.MessageSendParams) iter.Seq2[a2a.Event, error] {
	var result a2a.SendMessageResult
	return func(yield func(a2a.Event, error) bool) {
		ctx, callCtx := withMethodCallContext(ctx, "OnSendMessageStream")
		if params != nil && params.Message != nil {
			msg := params.Message
			ctx = h.withLoggerContext(
				ctx,
				slog.String("message_id", msg.ID),
				slog.String("task_id", string(msg.TaskID)),
				slog.String("context_id", msg.ContextID),
			)
		} else {
			ctx = h.withLoggerContext(ctx)
		}
		ctx, interceptedParams, earlyResponse, err := interceptBefore(ctx, h, callCtx, params, result)
		if err != nil || earlyResponse != nil {
			yield(earlyResponse, err)
			return
		}
		for event, err := range h.Handler.OnSendMessageStream(ctx, interceptedParams) {
			interceptedEvent, errOverride := interceptAfter(ctx, h.Interceptors, callCtx, event, err)
			if errOverride != nil {
				yield(nil, errOverride)
				return
			}
			if !yield(interceptedEvent, nil) {
				return
			}
		}
	}
}

func (h *InterceptedHandler) OnResubscribeToTask(ctx context.Context, params *a2a.TaskIDParams) iter.Seq2[a2a.Event, error] {
	var result a2a.SendMessageResult
	return func(yield func(a2a.Event, error) bool) {
		ctx, callCtx := withMethodCallContext(ctx, "OnResubscribeToTask")
		if params != nil {
			ctx = h.withLoggerContext(ctx, slog.String("task_id", string(params.ID)))
		}
		ctx, interceptedParams, earlyResponse, err := interceptBefore(ctx, h, callCtx, params, result)
		if err != nil || earlyResponse != nil {
			yield(earlyResponse, err)
			return
		}
		for event, err := range h.Handler.OnResubscribeToTask(ctx, interceptedParams) {
			interceptedEvent, errOverride := interceptAfter(ctx, h.Interceptors, callCtx, event, err)
			if errOverride != nil {
				yield(nil, errOverride)
				return
			}
			if !yield(interceptedEvent, nil) {
				return
			}
		}
	}
}

func (h *InterceptedHandler) OnGetTaskPushConfig(ctx context.Context, params *a2a.GetTaskPushConfigParams) (*a2a.TaskPushConfig, error) {
	var taskPushConfig *a2a.TaskPushConfig
	ctx, callCtx := withMethodCallContext(ctx, "OnGetTaskPushConfig")
	if params != nil {
		ctx = h.withLoggerContext(ctx, slog.String("task_id", string(params.TaskID)))
	}
	ctx, interceptedParams, earlyResponse, err := interceptBefore(ctx, h, callCtx, params, taskPushConfig)
	if err != nil || earlyResponse != nil {
		return earlyResponse, err
	}
	response, err := h.Handler.OnGetTaskPushConfig(ctx, interceptedParams)
	return interceptAfter(ctx, h.Interceptors, callCtx, response, err)
}

func (h *InterceptedHandler) OnListTaskPushConfig(ctx context.Context, params *a2a.ListTaskPushConfigParams) ([]*a2a.TaskPushConfig, error) {
	var taskPushConfigs []*a2a.TaskPushConfig
	ctx, callCtx := withMethodCallContext(ctx, "OnListTaskPushConfig")
	if params != nil {
		ctx = h.withLoggerContext(ctx, slog.String("task_id", string(params.TaskID)))
	}
	ctx, interceptedParams, earlyResponse, err := interceptBefore(ctx, h, callCtx, params, taskPushConfigs)
	if err != nil || earlyResponse != nil {
		return earlyResponse, err
	}
	response, err := h.Handler.OnListTaskPushConfig(ctx, interceptedParams)
	return interceptAfter(ctx, h.Interceptors, callCtx, response, err)
}

func (h *InterceptedHandler) OnSetTaskPushConfig(ctx context.Context, params *a2a.TaskPushConfig) (*a2a.TaskPushConfig, error) {
	var taskPushConfig *a2a.TaskPushConfig
	ctx, callCtx := withMethodCallContext(ctx, "OnSetTaskPushConfig")
	if params != nil {
		ctx = h.withLoggerContext(ctx, slog.String("task_id", string(params.TaskID)))
	}
	ctx, interceptedParams, earlyResponse, err := interceptBefore(ctx, h, callCtx, params, taskPushConfig)
	if err != nil || earlyResponse != nil {
		return earlyResponse, err
	}
	response, err := h.Handler.OnSetTaskPushConfig(ctx, interceptedParams)
	return interceptAfter(ctx, h.Interceptors, callCtx, response, err)
}

func (h *InterceptedHandler) OnDeleteTaskPushConfig(ctx context.Context, params *a2a.DeleteTaskPushConfigParams) error {
	var emptyResult struct{}
	ctx, callCtx := withMethodCallContext(ctx, "OnDeleteTaskPushConfig")
	if params != nil {
		ctx = h.withLoggerContext(ctx, slog.String("task_id", string(params.TaskID)))
	}
	ctx, interceptedParams, earlyResponse, err := interceptBefore(ctx, h, callCtx, params, emptyResult)
	if err != nil || earlyResponse != emptyResult {
		return err
	}
	err = h.Handler.OnDeleteTaskPushConfig(ctx, interceptedParams)
	var emptyResponse struct{}
	_, errOverride := interceptAfter(ctx, h.Interceptors, callCtx, emptyResponse, err)
	if errOverride != nil {
		return errOverride
	}
	return nil
}

func (h *InterceptedHandler) OnGetExtendedAgentCard(ctx context.Context) (*a2a.AgentCard, error) {
	var agentCard *a2a.AgentCard
	ctx, callCtx := withMethodCallContext(ctx, "OnGetExtendedAgentCard")
	ctx = h.withLoggerContext(ctx)

	var req struct{}
	ctx, _, earlyResponse, err := interceptBefore(ctx, h, callCtx, req, agentCard)
	if err != nil || earlyResponse != nil {
		return earlyResponse, err
	}
	response, err := h.Handler.OnGetExtendedAgentCard(ctx)
	return interceptAfter(ctx, h.Interceptors, callCtx, response, err)
}

func interceptBefore[Req any, Resp any](ctx context.Context, h *InterceptedHandler, callCtx *CallContext, payload Req, result Resp) (context.Context, Req, Resp, error) {
	request := &Request{Payload: payload}
	var zeroReq Req
	var zeroResp Resp

	for i, interceptor := range h.Interceptors {
		localCtx, result, err := interceptor.Before(ctx, callCtx, request)
		if err != nil || result != nil {
			typedResult := zeroResp
			if result != nil {
				typedResult = result.(Resp)
			}
			interceptors := h.Interceptors[:i+1]
			resp, err := interceptAfter(ctx, interceptors, callCtx, typedResult, err)
			return ctx, zeroReq, resp, err
		}
		ctx = localCtx
	}

	if request.Payload == nil {
		return ctx, zeroReq, zeroResp, nil
	}

	typed, ok := request.Payload.(Req)
	if !ok {
		return ctx, zeroReq, zeroResp, fmt.Errorf("payload type changed from %T to %T", payload, request.Payload)
	}

	return ctx, typed, zeroResp, nil
}

func interceptAfter[T any](ctx context.Context, interceptors []CallInterceptor, callCtx *CallContext, payload T, responseErr error) (T, error) {
	response := &Response{Payload: payload, Err: responseErr}

	var zero T
	for i := range len(interceptors) {
		interceptor := interceptors[len(interceptors)-i-1]
		if resp, err := interceptor.After(ctx, callCtx, response); err != nil {
			if resp == nil {
				return zero, err
			}
			return resp.(T), err
		}
	}

	if response.Payload == nil {
		return zero, response.Err
	}

	typed, ok := response.Payload.(T)
	if !ok {
		return zero, fmt.Errorf("payload type changed from %T to %T", payload, response.Payload)
	}

	return typed, response.Err
}

// withLoggerContext is a private utility function which attaches an slog.Logger with a2a-specific attributes
// to the provided context.
func (h *InterceptedHandler) withLoggerContext(ctx context.Context, attrs ...any) context.Context {
	logger := h.Logger
	if logger == nil {
		logger = log.LoggerFrom(ctx)
	}
	requestID := uuid.NewString()
	withAttrs := logger.WithGroup("a2a").With(attrs...).With(slog.String("request_id", requestID))
	return log.WithLogger(ctx, withAttrs)
}

// withMethodCallContext is a private utility function which modifies CallContext.method if a CallContext
// was passed by a transport implementation or initializes a new CallContext with the provided method.
func withMethodCallContext(ctx context.Context, method string) (context.Context, *CallContext) {
	callCtx, ok := CallContextFrom(ctx)
	if !ok {
		ctx, callCtx = WithCallContext(ctx, nil)
	}
	callCtx.method = method
	return ctx, callCtx
}
