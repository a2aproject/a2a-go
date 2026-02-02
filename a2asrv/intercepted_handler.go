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

type interceptBeforeResult[Req any, Resp any] struct {
	reqOverride   Req
	earlyResponse *Resp
	earlyErr      error
}

var _ RequestHandler = (*InterceptedHandler)(nil)

func (h *InterceptedHandler) OnGetTask(ctx context.Context, query *a2a.TaskQueryParams) (*a2a.Task, error) {
	ctx, callCtx := withMethodCallContext(ctx, "OnGetTask")
	if query != nil {
		ctx = h.withLoggerContext(ctx, slog.String("task_id", string(query.ID)))
	}
	ctx, res := interceptBefore[*a2a.TaskQueryParams, *a2a.Task](ctx, h, callCtx, query)
	if res.earlyErr != nil {
		return nil, res.earlyErr
	}
	if res.earlyResponse != nil {
		return *res.earlyResponse, nil
	}
	response, err := h.Handler.OnGetTask(ctx, res.reqOverride)
	return interceptAfter(ctx, h.Interceptors, callCtx, response, err)
}

func (h *InterceptedHandler) OnCancelTask(ctx context.Context, params *a2a.TaskIDParams) (*a2a.Task, error) {
	ctx, callCtx := withMethodCallContext(ctx, "OnCancelTask")
	if params != nil {
		ctx = h.withLoggerContext(ctx, slog.String("task_id", string(params.ID)))
	}
	ctx, res := interceptBefore[*a2a.TaskIDParams, *a2a.Task](ctx, h, callCtx, params)
	if res.earlyErr != nil {
		return nil, res.earlyErr
	}
	if res.earlyResponse != nil {
		return *res.earlyResponse, nil
	}
	response, err := h.Handler.OnCancelTask(ctx, res.reqOverride)
	return interceptAfter(ctx, h.Interceptors, callCtx, response, err)
}

func (h *InterceptedHandler) OnSendMessage(ctx context.Context, params *a2a.MessageSendParams) (a2a.SendMessageResult, error) {
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
	ctx, res := interceptBefore[*a2a.MessageSendParams, a2a.SendMessageResult](ctx, h, callCtx, params)
	if res.earlyErr != nil {
		return nil, res.earlyErr
	}
	if res.earlyResponse != nil {
		return *res.earlyResponse, nil
	}
	response, err := h.Handler.OnSendMessage(ctx, res.reqOverride)
	return interceptAfter(ctx, h.Interceptors, callCtx, response, err)
}

func (h *InterceptedHandler) OnSendMessageStream(ctx context.Context, params *a2a.MessageSendParams) iter.Seq2[a2a.Event, error] {
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
		ctx, res := interceptBefore[*a2a.MessageSendParams, a2a.SendMessageResult](ctx, h, callCtx, params)
		if res.earlyErr != nil {
			yield(nil, res.earlyErr)
			return
		}
		if res.earlyResponse != nil {
			yield(*res.earlyResponse, nil)
			return
		}
		for event, err := range h.Handler.OnSendMessageStream(ctx, res.reqOverride) {
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
	return func(yield func(a2a.Event, error) bool) {
		ctx, callCtx := withMethodCallContext(ctx, "OnResubscribeToTask")
		if params != nil {
			ctx = h.withLoggerContext(ctx, slog.String("task_id", string(params.ID)))
		}
		ctx, res := interceptBefore[*a2a.TaskIDParams, a2a.SendMessageResult](ctx, h, callCtx, params)
		if res.earlyErr != nil {
			yield(nil, res.earlyErr)
			return
		}
		if res.earlyResponse != nil {
			yield(*res.earlyResponse, nil)
			return
		}
		for event, err := range h.Handler.OnResubscribeToTask(ctx, res.reqOverride) {
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
	ctx, callCtx := withMethodCallContext(ctx, "OnGetTaskPushConfig")
	if params != nil {
		ctx = h.withLoggerContext(ctx, slog.String("task_id", string(params.TaskID)))
	}
	ctx, res := interceptBefore[*a2a.GetTaskPushConfigParams, *a2a.TaskPushConfig](ctx, h, callCtx, params)
	if res.earlyErr != nil {
		return nil, res.earlyErr
	}
	if res.earlyResponse != nil {
		return *res.earlyResponse, nil
	}
	response, err := h.Handler.OnGetTaskPushConfig(ctx, res.reqOverride)
	return interceptAfter(ctx, h.Interceptors, callCtx, response, err)
}

func (h *InterceptedHandler) OnListTaskPushConfig(ctx context.Context, params *a2a.ListTaskPushConfigParams) ([]*a2a.TaskPushConfig, error) {
	ctx, callCtx := withMethodCallContext(ctx, "OnListTaskPushConfig")
	if params != nil {
		ctx = h.withLoggerContext(ctx, slog.String("task_id", string(params.TaskID)))
	}
	ctx, res := interceptBefore[*a2a.ListTaskPushConfigParams, []*a2a.TaskPushConfig](ctx, h, callCtx, params)
	if res.earlyErr != nil {
		return nil, res.earlyErr
	}
	if res.earlyResponse != nil {
		return *res.earlyResponse, nil
	}
	response, err := h.Handler.OnListTaskPushConfig(ctx, res.reqOverride)
	return interceptAfter(ctx, h.Interceptors, callCtx, response, err)
}

func (h *InterceptedHandler) OnSetTaskPushConfig(ctx context.Context, params *a2a.TaskPushConfig) (*a2a.TaskPushConfig, error) {
	ctx, callCtx := withMethodCallContext(ctx, "OnSetTaskPushConfig")
	if params != nil {
		ctx = h.withLoggerContext(ctx, slog.String("task_id", string(params.TaskID)))
	}
	ctx, res := interceptBefore[*a2a.TaskPushConfig, *a2a.TaskPushConfig](ctx, h, callCtx, params)
	if res.earlyErr != nil {
		return nil, res.earlyErr
	}
	if res.earlyResponse != nil {
		return *res.earlyResponse, nil
	}
	response, err := h.Handler.OnSetTaskPushConfig(ctx, res.reqOverride)
	return interceptAfter(ctx, h.Interceptors, callCtx, response, err)
}

func (h *InterceptedHandler) OnDeleteTaskPushConfig(ctx context.Context, params *a2a.DeleteTaskPushConfigParams) error {
	ctx, callCtx := withMethodCallContext(ctx, "OnDeleteTaskPushConfig")
	if params != nil {
		ctx = h.withLoggerContext(ctx, slog.String("task_id", string(params.TaskID)))
	}
	ctx, res := interceptBefore[*a2a.DeleteTaskPushConfigParams, struct{}](ctx, h, callCtx, params)
	if res.earlyErr != nil {
		return res.earlyErr
	}
	if res.earlyResponse != nil {
		return nil
	}
	err := h.Handler.OnDeleteTaskPushConfig(ctx, res.reqOverride)
	var emptyResponse struct{}
	_, errOverride := interceptAfter(ctx, h.Interceptors, callCtx, emptyResponse, err)
	if errOverride != nil {
		return errOverride
	}
	return nil
}

func (h *InterceptedHandler) OnGetExtendedAgentCard(ctx context.Context) (*a2a.AgentCard, error) {
	ctx, callCtx := withMethodCallContext(ctx, "OnGetExtendedAgentCard")
	ctx = h.withLoggerContext(ctx)

	var req *struct{}
	ctx, res := interceptBefore[*struct{}, *a2a.AgentCard](ctx, h, callCtx, req)
	if res.earlyErr != nil {
		return nil, res.earlyErr
	}
	if res.earlyResponse != nil {
		return *res.earlyResponse, nil
	}
	response, err := h.Handler.OnGetExtendedAgentCard(ctx)
	return interceptAfter(ctx, h.Interceptors, callCtx, response, err)
}

func interceptBefore[Req any, Resp any](ctx context.Context, h *InterceptedHandler, callCtx *CallContext, payload Req) (context.Context, interceptBeforeResult[Req, Resp]) {
	request := &Request{Payload: payload}

	var zeroReq Req
	outcome := interceptBeforeResult[Req, Resp]{
		reqOverride:   zeroReq,
		earlyResponse: nil,
		earlyErr:      nil,
	}

	for i, interceptor := range h.Interceptors {
		localCtx, result, err := interceptor.Before(ctx, callCtx, request)
		if err != nil || result != nil {
			var typedResult Resp
			if result != nil {
				r, ok := result.(Resp)
				if !ok {
					outcome.earlyErr = fmt.Errorf("result type changed from %T to %T", result, typedResult)
					return ctx, outcome
				}
				typedResult = r
			}
			interceptors := h.Interceptors[:i+1]
			resp, err := interceptAfter(ctx, interceptors, callCtx, typedResult, err)
			outcome.earlyResponse = &resp
			outcome.earlyErr = err
			return ctx, outcome
		}
		ctx = localCtx
	}

	if request.Payload == nil {
		return ctx, outcome
	}

	typed, ok := request.Payload.(Req)
	if !ok {
		outcome.earlyErr = fmt.Errorf("payload type changed from %T to %T", payload, request.Payload)
		return ctx, outcome
	}

	outcome.reqOverride = typed
	return ctx, outcome
}

func interceptAfter[T any](ctx context.Context, interceptors []CallInterceptor, callCtx *CallContext, payload T, responseErr error) (T, error) {
	response := &Response{Payload: payload, Err: responseErr}

	var zero T
	for i := range len(interceptors) {
		interceptor := interceptors[len(interceptors)-i-1]
		if err := interceptor.After(ctx, callCtx, response); err != nil {
			return zero, err
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
