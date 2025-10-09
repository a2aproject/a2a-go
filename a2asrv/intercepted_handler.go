package a2asrv

import (
	"context"
	"iter"

	"github.com/a2aproject/a2a-go/a2a"
)

// InterceptedHandler implements RequestHandler. It can be used to attach call interceptors and initialize
// call context for every method of the wrapped handler.
type InterceptedHandler struct {
	// Handler is responsible for the actual processing of every call.
	Handler RequestHandler
	// Interceptors is a list of call interceptors which will be applied before and after each call.
	Interceptors []CallInterceptor
}

func (h *InterceptedHandler) OnGetTask(ctx context.Context, query *a2a.TaskQueryParams) (*a2a.Task, error) {
	ctx, callCtx := withMethodCallContext(ctx, "OnGetTask")
	ctx, err := h.interceptBefore(ctx, callCtx, query)
	if err != nil {
		return nil, err
	}
	response, err := h.Handler.OnGetTask(ctx, query)
	if errOverride := h.interceptAfter(ctx, callCtx, response, err); errOverride != nil {
		return nil, errOverride
	}
	return response, err
}

func (h *InterceptedHandler) OnCancelTask(ctx context.Context, params *a2a.TaskIDParams) (*a2a.Task, error) {
	ctx, callCtx := withMethodCallContext(ctx, "OnCancelTask")
	ctx, err := h.interceptBefore(ctx, callCtx, params)
	if err != nil {
		return nil, err
	}
	response, err := h.Handler.OnCancelTask(ctx, params)
	if errOverride := h.interceptAfter(ctx, callCtx, response, err); errOverride != nil {
		return nil, errOverride
	}
	return response, err
}

func (h *InterceptedHandler) OnSendMessage(ctx context.Context, params *a2a.MessageSendParams) (a2a.SendMessageResult, error) {
	ctx, callCtx := withMethodCallContext(ctx, "OnSendMessage")
	ctx, err := h.interceptBefore(ctx, callCtx, params)
	if err != nil {
		return nil, err
	}
	response, err := h.Handler.OnSendMessage(ctx, params)
	if errOverride := h.interceptAfter(ctx, callCtx, response, err); errOverride != nil {
		return nil, errOverride
	}
	return response, err
}

func (h *InterceptedHandler) OnSendMessageStream(ctx context.Context, params *a2a.MessageSendParams) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		ctx, callCtx := withMethodCallContext(ctx, "OnSendMessageStream")
		ctx, err := h.interceptBefore(ctx, callCtx, params)
		if err != nil {
			yield(nil, err)
			return
		}
		for event, err := range h.Handler.OnSendMessageStream(ctx, params) {
			if errOverride := h.interceptAfter(ctx, callCtx, event, err); errOverride != nil {
				yield(nil, errOverride)
				return
			}
			if !yield(event, err) {
				return
			}
		}
	}
}

func (h *InterceptedHandler) OnResubscribeToTask(ctx context.Context, params *a2a.TaskIDParams) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		ctx, callCtx := withMethodCallContext(ctx, "OnResubscribeToTask")
		ctx, err := h.interceptBefore(ctx, callCtx, params)
		if err != nil {
			yield(nil, err)
			return
		}
		for event, err := range h.Handler.OnResubscribeToTask(ctx, params) {
			if errOverride := h.interceptAfter(ctx, callCtx, event, err); errOverride != nil {
				yield(nil, errOverride)
				return
			}
			if !yield(event, err) {
				return
			}
		}
	}
}

func (h *InterceptedHandler) OnGetTaskPushConfig(ctx context.Context, params *a2a.GetTaskPushConfigParams) (*a2a.TaskPushConfig, error) {
	ctx, callCtx := withMethodCallContext(ctx, "OnGetTaskPushConfig")
	ctx, err := h.interceptBefore(ctx, callCtx, params)
	if err != nil {
		return nil, err
	}
	response, err := h.Handler.OnGetTaskPushConfig(ctx, params)
	if errOverride := h.interceptAfter(ctx, callCtx, response, err); errOverride != nil {
		return nil, errOverride
	}
	return response, err
}

func (h *InterceptedHandler) OnListTaskPushConfig(ctx context.Context, params *a2a.ListTaskPushConfigParams) ([]*a2a.TaskPushConfig, error) {
	ctx, callCtx := withMethodCallContext(ctx, "OnListTaskPushConfig")
	ctx, err := h.interceptBefore(ctx, callCtx, params)
	if err != nil {
		return nil, err
	}
	response, err := h.Handler.OnListTaskPushConfig(ctx, params)
	if errOverride := h.interceptAfter(ctx, callCtx, response, err); errOverride != nil {
		return nil, errOverride
	}
	return response, err
}

func (h *InterceptedHandler) OnSetTaskPushConfig(ctx context.Context, params *a2a.TaskPushConfig) (*a2a.TaskPushConfig, error) {
	ctx, callCtx := withMethodCallContext(ctx, "OnSetTaskPushConfig")
	ctx, err := h.interceptBefore(ctx, callCtx, params)
	if err != nil {
		return nil, err
	}
	response, err := h.Handler.OnSetTaskPushConfig(ctx, params)
	if errOverride := h.interceptAfter(ctx, callCtx, response, err); errOverride != nil {
		return nil, errOverride
	}
	return response, err
}

func (h *InterceptedHandler) OnDeleteTaskPushConfig(ctx context.Context, params *a2a.DeleteTaskPushConfigParams) error {
	ctx, callCtx := withMethodCallContext(ctx, "OnDeleteTaskPushConfig")
	ctx, err := h.interceptBefore(ctx, callCtx, params)
	if err != nil {
		return err
	}
	err = h.Handler.OnDeleteTaskPushConfig(ctx, params)
	if errOverride := h.interceptAfter(ctx, callCtx, nil, err); errOverride != nil {
		return errOverride
	}
	return err
}

func (h *InterceptedHandler) interceptBefore(ctx context.Context, callCtx *CallContext, payload any) (context.Context, error) {
	request := &Request{Payload: payload}

	for _, interceptor := range h.Interceptors {
		localCtx, err := interceptor.Before(ctx, callCtx, request)
		if err != nil {
			return ctx, err
		}
		ctx = localCtx
	}

	return ctx, nil
}

func (h *InterceptedHandler) interceptAfter(ctx context.Context, callCtx *CallContext, payload any, responseErr error) error {
	response := &Response{Payload: payload, Err: responseErr}

	for i := range len(h.Interceptors) {
		interceptor := h.Interceptors[len(h.Interceptors)-i-1]
		if err := interceptor.After(ctx, callCtx, response); err != nil {
			return err
		}
	}

	return nil
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
