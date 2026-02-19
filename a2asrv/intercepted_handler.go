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

func (h *InterceptedHandler) GetTask(ctx context.Context, req *a2a.GetTaskRequest) (*a2a.Task, error) {
	ctx, callCtx := withMethodCallContext(ctx, "GetTask")
	if req != nil {
		ctx = h.withLoggerContext(ctx, slog.String("task_id", string(req.ID)))
	}
	return doCall(ctx, callCtx, h, req, h.Handler.GetTask)
}

func (h *InterceptedHandler) ListTasks(ctx context.Context, req *a2a.ListTasksRequest) (*a2a.ListTasksResponse, error) {
	ctx, callCtx := withMethodCallContext(ctx, "ListTasks")
	if req != nil {
		ctx = h.withLoggerContext(ctx)
	}
	return doCall(ctx, callCtx, h, req, h.Handler.ListTasks)
}

func (h *InterceptedHandler) CancelTask(ctx context.Context, req *a2a.CancelTaskRequest) (*a2a.Task, error) {
	ctx, callCtx := withMethodCallContext(ctx, "CancelTask")
	if req != nil {
		ctx = h.withLoggerContext(ctx, slog.String("task_id", string(req.ID)))
	}
	return doCall(ctx, callCtx, h, req, h.Handler.CancelTask)
}

func (h *InterceptedHandler) SendMessage(ctx context.Context, req *a2a.SendMessageRequest) (a2a.SendMessageResult, error) {
	ctx, callCtx := withMethodCallContext(ctx, "SendMessage")
	if req != nil && req.Message != nil {
		msg := req.Message
		ctx = h.withLoggerContext(
			ctx,
			slog.String("message_id", msg.ID),
			slog.String("task_id", string(msg.TaskID)),
			slog.String("context_id", msg.ContextID),
		)
	} else {
		ctx = h.withLoggerContext(ctx)
	}
	return doCall(ctx, callCtx, h, req, h.Handler.SendMessage)
}

func (h *InterceptedHandler) SendStreamingMessage(ctx context.Context, req *a2a.SendMessageRequest) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		ctx, callCtx := withMethodCallContext(ctx, "SendStreamingMessage")
		if req != nil && req.Message != nil {
			msg := req.Message
			ctx = h.withLoggerContext(
				ctx,
				slog.String("message_id", msg.ID),
				slog.String("task_id", string(msg.TaskID)),
				slog.String("context_id", msg.ContextID),
			)
		} else {
			ctx = h.withLoggerContext(ctx)
		}
		ctx, res := interceptBefore[*a2a.SendMessageRequest, a2a.SendMessageResult](ctx, h, callCtx, req)
		if res.earlyErr != nil {
			yield(nil, res.earlyErr)
			return
		}
		if res.earlyResponse != nil {
			yield(*res.earlyResponse, nil)
			return
		}
		for event, err := range h.Handler.SendStreamingMessage(ctx, res.reqOverride) {
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

func (h *InterceptedHandler) SubscribeToTask(ctx context.Context, req *a2a.SubscribeToTaskRequest) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		ctx, callCtx := withMethodCallContext(ctx, "SubscribeToTask")
		if req != nil {
			ctx = h.withLoggerContext(ctx, slog.String("task_id", string(req.ID)))
		}
		ctx, res := interceptBefore[*a2a.SubscribeToTaskRequest, a2a.SendMessageResult](ctx, h, callCtx, req)
		if res.earlyErr != nil {
			yield(nil, res.earlyErr)
			return
		}
		if res.earlyResponse != nil {
			yield(*res.earlyResponse, nil)
			return
		}
		for event, err := range h.Handler.SubscribeToTask(ctx, res.reqOverride) {
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

func (h *InterceptedHandler) GetTaskPushConfig(ctx context.Context, req *a2a.GetTaskPushConfigRequest) (*a2a.TaskPushConfig, error) {
	ctx, callCtx := withMethodCallContext(ctx, "GetTaskPushConfig")
	if req != nil {
		ctx = h.withLoggerContext(ctx, slog.String("task_id", string(req.TaskID)))
	}
	return doCall(ctx, callCtx, h, req, h.Handler.GetTaskPushConfig)
}

func (h *InterceptedHandler) ListTaskPushConfig(ctx context.Context, req *a2a.ListTaskPushConfigRequest) ([]*a2a.TaskPushConfig, error) {
	ctx, callCtx := withMethodCallContext(ctx, "ListTaskPushConfig")
	if req != nil {
		ctx = h.withLoggerContext(ctx, slog.String("task_id", string(req.TaskID)))
	}
	return doCall(ctx, callCtx, h, req, h.Handler.ListTaskPushConfig)
}

func (h *InterceptedHandler) CreateTaskPushConfig(ctx context.Context, req *a2a.CreateTaskPushConfigRequest) (*a2a.TaskPushConfig, error) {
	ctx, callCtx := withMethodCallContext(ctx, "CreateTaskPushConfig")
	if req != nil {
		ctx = h.withLoggerContext(ctx, slog.String("task_id", string(req.TaskID)))
	}
	return doCall(ctx, callCtx, h, req, h.Handler.CreateTaskPushConfig)
}

func (h *InterceptedHandler) DeleteTaskPushConfig(ctx context.Context, req *a2a.DeleteTaskPushConfigRequest) error {
	ctx, callCtx := withMethodCallContext(ctx, "DeleteTaskPushConfig")
	if req != nil {
		ctx = h.withLoggerContext(ctx, slog.String("task_id", string(req.TaskID)))
	}
	ctx, res := interceptBefore[*a2a.DeleteTaskPushConfigRequest, struct{}](ctx, h, callCtx, req)
	if res.earlyErr != nil {
		return res.earlyErr
	}
	if res.earlyResponse != nil {
		return nil
	}
	err := h.Handler.DeleteTaskPushConfig(ctx, res.reqOverride)
	var emptyResponse struct{}
	_, errOverride := interceptAfter(ctx, h.Interceptors, callCtx, emptyResponse, err)
	return errOverride
}

func (h *InterceptedHandler) GetExtendedAgentCard(ctx context.Context) (*a2a.AgentCard, error) {
	ctx, callCtx := withMethodCallContext(ctx, "GetExtendedAgentCard")
	ctx = h.withLoggerContext(ctx)

	var req *struct{}
	ctx, res := interceptBefore[*struct{}, *a2a.AgentCard](ctx, h, callCtx, req)
	if res.earlyErr != nil {
		return nil, res.earlyErr
	}
	if res.earlyResponse != nil {
		return *res.earlyResponse, nil
	}
	response, err := h.Handler.GetExtendedAgentCard(ctx)
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
	for i := len(interceptors) - 1; i >= 0; i-- {
		if err := interceptors[i].After(ctx, callCtx, response); err != nil {
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

func doCall[Req any, Resp any](
	ctx context.Context, callCtx *CallContext, h *InterceptedHandler, req Req,
	transportCall func(context.Context, Req) (Resp, error),
) (Resp, error) {
	ctx, res := interceptBefore[Req, Resp](ctx, h, callCtx, req)
	if res.earlyErr != nil {
		var zero Resp
		return zero, res.earlyErr
	}
	if res.earlyResponse != nil {
		return *res.earlyResponse, nil
	}
	response, err := transportCall(ctx, res.reqOverride)
	return interceptAfter(ctx, h.Interceptors, callCtx, response, err)
}
