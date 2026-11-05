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
	"slices"
	"strings"

	"github.com/google/uuid"
	"golang.org/x/mod/semver"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/internal/utils"
	"github.com/a2aproject/a2a-go/v2/log"
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

// GetTask implements RequestHandler.
func (h *InterceptedHandler) GetTask(ctx context.Context, req *a2a.GetTaskRequest) (*a2a.Task, error) {
	ctx, callCtx := attachMethodCallContext(ctx, "GetTask", req.Tenant)
	ctx = h.withLoggerContext(ctx, slog.String("task_id", string(req.ID)))
	return doCall(ctx, callCtx, h, req, h.Handler.GetTask)
}

// ListTasks implements RequestHandler.
func (h *InterceptedHandler) ListTasks(ctx context.Context, req *a2a.ListTasksRequest) (*a2a.ListTasksResponse, error) {
	ctx, callCtx := attachMethodCallContext(ctx, "ListTasks", req.Tenant)
	ctx = h.withLoggerContext(ctx)
	return doCall(ctx, callCtx, h, req, h.Handler.ListTasks)
}

// CancelTask implements RequestHandler.
func (h *InterceptedHandler) CancelTask(ctx context.Context, req *a2a.CancelTaskRequest) (*a2a.Task, error) {
	ctx, callCtx := attachMethodCallContext(ctx, "CancelTask", req.Tenant)
	ctx = h.withLoggerContext(ctx, slog.String("task_id", string(req.ID)))
	return doCall(ctx, callCtx, h, req, h.Handler.CancelTask)
}

// SendMessage implements RequestHandler.
func (h *InterceptedHandler) SendMessage(ctx context.Context, req *a2a.SendMessageRequest) (a2a.SendMessageResult, error) {
	ctx, callCtx := attachMethodCallContext(ctx, "SendMessage", req.Tenant)
	if msg := req.Message; msg != nil {
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

// SendStreamingMessage implements RequestHandler.
func (h *InterceptedHandler) SendStreamingMessage(ctx context.Context, req *a2a.SendMessageRequest) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		ctx, callCtx := attachMethodCallContext(ctx, "SendStreamingMessage", req.Tenant)
		if err := checkProtocolVersion(callCtx); err != nil {
			yield(nil, err)
			return
		}
		if req.Message != nil {
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
		streamCall(ctx, callCtx, h, req, h.Handler.SendStreamingMessage, yield)
	}
}

// SubscribeToTask implements RequestHandler.
func (h *InterceptedHandler) SubscribeToTask(ctx context.Context, req *a2a.SubscribeToTaskRequest) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		ctx, callCtx := attachMethodCallContext(ctx, "SubscribeToTask", req.Tenant)
		if err := checkProtocolVersion(callCtx); err != nil {
			yield(nil, err)
			return
		}
		ctx = h.withLoggerContext(ctx, slog.String("task_id", string(req.ID)))
		streamCall(ctx, callCtx, h, req, h.Handler.SubscribeToTask, yield)
	}
}

// GetTaskPushConfig implements RequestHandler.
func (h *InterceptedHandler) GetTaskPushConfig(ctx context.Context, req *a2a.GetTaskPushConfigRequest) (*a2a.PushConfig, error) {
	ctx, callCtx := attachMethodCallContext(ctx, "GetTaskPushConfig", req.Tenant)
	ctx = h.withLoggerContext(ctx, slog.String("task_id", string(req.TaskID)))
	return doCall(ctx, callCtx, h, req, h.Handler.GetTaskPushConfig)
}

// ListTaskPushConfigs implements RequestHandler.
func (h *InterceptedHandler) ListTaskPushConfigs(ctx context.Context, req *a2a.ListTaskPushConfigRequest) (*a2a.ListTaskPushConfigResponse, error) {
	ctx, callCtx := attachMethodCallContext(ctx, "ListTaskPushConfigs", req.Tenant)
	ctx = h.withLoggerContext(ctx, slog.String("task_id", string(req.TaskID)))
	return doCall(ctx, callCtx, h, req, h.Handler.ListTaskPushConfigs)
}

// CreateTaskPushConfig implements RequestHandler.
func (h *InterceptedHandler) CreateTaskPushConfig(ctx context.Context, req *a2a.PushConfig) (*a2a.PushConfig, error) {
	ctx, callCtx := attachMethodCallContext(ctx, "CreateTaskPushConfig", req.Tenant)
	ctx = h.withLoggerContext(ctx, slog.String("task_id", string(req.TaskID)))
	return doCall(ctx, callCtx, h, req, h.Handler.CreateTaskPushConfig)
}

// DeleteTaskPushConfig implements RequestHandler.
func (h *InterceptedHandler) DeleteTaskPushConfig(ctx context.Context, req *a2a.DeleteTaskPushConfigRequest) error {
	ctx, callCtx := attachMethodCallContext(ctx, "DeleteTaskPushConfig", req.Tenant)
	ctx = h.withLoggerContext(ctx, slog.String("task_id", string(req.TaskID)))
	_, err := doCall(ctx, callCtx, h, req, func(ctx context.Context, req *a2a.DeleteTaskPushConfigRequest) (struct{}, error) {
		return struct{}{}, h.Handler.DeleteTaskPushConfig(ctx, req)
	})
	return err
}

// GetExtendedAgentCard implements RequestHandler.
func (h *InterceptedHandler) GetExtendedAgentCard(ctx context.Context, req *a2a.GetExtendedAgentCardRequest) (*a2a.AgentCard, error) {
	ctx, callCtx := attachMethodCallContext(ctx, "GetExtendedAgentCard", req.Tenant)
	ctx = h.withLoggerContext(ctx)
	return doCall(ctx, callCtx, h, req, h.Handler.GetExtendedAgentCard)
}

// interceptCalls returns a callHandler which runs the interceptors around next.
func interceptCalls(h *InterceptedHandler, next callHandler) callHandler {
	return func(ctx context.Context, callCtx *CallContext, req Request) iter.Seq2[any, error] {
		return func(yield func(any, error) bool) {
			for i, interceptor := range h.Interceptors {
				localCtx, result, err := interceptor.Before(ctx, callCtx, &req)
				if err != nil || result != nil {
					yield(interceptAfter(ctx, h.Interceptors[:i+1], callCtx, result, err))
					return
				}
				ctx = localCtx
			}
			for payload, err := range next(ctx, callCtx, req) {
				if !yield(interceptAfter(ctx, h.Interceptors, callCtx, payload, err)) {
					return
				}
			}
		}
	}
}

func interceptAfter(ctx context.Context, interceptors []CallInterceptor, callCtx *CallContext, payload any, responseErr error) (any, error) {
	response := &Response{Payload: payload, Err: responseErr}
	for _, interceptor := range slices.Backward(interceptors) {
		if err := interceptor.After(ctx, callCtx, response); err != nil {
			return nil, err
		}
	}
	return response.Payload, response.Err
}

// unaryHandler returns a callHandler which invokes a unary [RequestHandler] method.
func unaryHandler[Req, Resp any](handlerCall func(context.Context, Req) (Resp, error)) callHandler {
	return func(ctx context.Context, _ *CallContext, req Request) iter.Seq2[any, error] {
		return func(yield func(any, error) bool) {
			typed, err := utils.CastPayload[Req](req.Payload)
			if err != nil {
				yield(nil, err)
				return
			}
			yield(handlerCall(ctx, typed))
		}
	}
}

// streamHandler returns a callHandler which invokes a streaming [RequestHandler] method.
func streamHandler[Req any](handlerCall func(context.Context, Req) iter.Seq2[a2a.Event, error]) callHandler {
	return func(ctx context.Context, _ *CallContext, req Request) iter.Seq2[any, error] {
		return func(yield func(any, error) bool) {
			typed, err := utils.CastPayload[Req](req.Payload)
			if err != nil {
				yield(nil, err)
				return
			}
			for event, err := range handlerCall(ctx, typed) {
				if !yield(event, err) {
					return
				}
			}
		}
	}
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
	return log.AttachLogger(ctx, withAttrs)
}

// attachMethodCallContext is a private utility function which modifies CallContext.method if a CallContext
// was passed by a transport implementation or initializes a new CallContext with the provided method.
func attachMethodCallContext(ctx context.Context, method string, tenant string) (context.Context, *CallContext) {
	// AttachTenant needed for a2aclient package, which is not aware of CallContext.
	if tenant != "" {
		ctx = a2a.AttachTenant(ctx, tenant)
	}

	callCtx, ok := CallContextFrom(ctx)
	if !ok {
		ctx, callCtx = NewCallContext(ctx, nil)
	}

	callCtx.method = method
	if tenant != "" {
		callCtx.tenant = tenant
	}
	return ctx, callCtx
}

func doCall[Req any, Resp any](
	ctx context.Context, callCtx *CallContext, h *InterceptedHandler, req Req,
	handlerCall func(context.Context, Req) (Resp, error),
) (Resp, error) {
	var zero Resp
	if err := checkProtocolVersion(callCtx); err != nil {
		return zero, err
	}
	for payload, err := range interceptCalls(h, unaryHandler(handlerCall))(ctx, callCtx, Request{Payload: req}) {
		return utils.CastResult[Resp](payload, err)
	}
	return zero, fmt.Errorf("%s produced no result", callCtx.Method())
}

// streamCall runs a streaming call through the interceptors and passes the results to yield.
func streamCall[Req any](
	ctx context.Context, callCtx *CallContext, h *InterceptedHandler, req Req,
	handlerCall func(context.Context, Req) iter.Seq2[a2a.Event, error], yield func(a2a.Event, error) bool,
) {
	for payload, err := range interceptCalls(h, streamHandler(handlerCall))(ctx, callCtx, Request{Payload: req}) {
		event, err := utils.CastResult[a2a.Event](payload, err)
		if err != nil {
			yield(nil, err)
			return
		}
		if !yield(event, nil) {
			return
		}
	}
}

// checkProtocolVersion checks if the client's protocol version is compatible with the server's.
// Unset or empty A2A-Version values are treated as "0.3". Only Major.Minor are compared.
func checkProtocolVersion(callCtx *CallContext) error {
	if callCtx.ProtocolVersion == "" {
		return nil
	}
	versions, _ := callCtx.ServiceParams().Get(a2a.SvcParamVersion)
	if len(versions) > 1 {
		return fmt.Errorf("%w: %d protocol versions specified", a2a.ErrInvalidRequest, len(versions))
	}
	clientVersion := a2a.ProtocolVersion("0.3")
	if len(versions) == 1 && versions[0] != "" {
		clientVersion = a2a.ProtocolVersion(versions[0])
	}
	if mm := majorMinor(clientVersion); mm == "" || mm != majorMinor(callCtx.ProtocolVersion) {
		return fmt.Errorf("%w: requested %q, served %q", a2a.ErrVersionNotSupported, clientVersion, callCtx.ProtocolVersion)
	}
	return nil
}

func majorMinor(v a2a.ProtocolVersion) string {
	s := string(v)
	if !strings.HasPrefix(s, "v") {
		s = "v" + s
	}
	return semver.MajorMinor(s)
}
