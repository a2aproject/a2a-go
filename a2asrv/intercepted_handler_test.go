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
	"reflect"
	"strings"
	"testing"

	"github.com/a2aproject/a2a-go/a2a"
)

type mockHandler struct {
	lastCallContext       *CallContext
	resultErr             error
	OnSendMessageFn       func(ctx context.Context, params *a2a.MessageSendParams) (a2a.SendMessageResult, error)
	OnSendMessageStreamFn func(ctx context.Context, params *a2a.MessageSendParams) iter.Seq2[a2a.Event, error]
}

var _ RequestHandler = (*mockHandler)(nil)

func (h *mockHandler) OnGetTask(ctx context.Context, query *a2a.TaskQueryParams) (*a2a.Task, error) {
	h.lastCallContext, _ = CallContextFrom(ctx)
	if h.resultErr != nil {
		return nil, h.resultErr
	}
	return &a2a.Task{}, nil
}

func (h *mockHandler) OnCancelTask(ctx context.Context, params *a2a.TaskIDParams) (*a2a.Task, error) {
	h.lastCallContext, _ = CallContextFrom(ctx)
	if h.resultErr != nil {
		return nil, h.resultErr
	}
	return &a2a.Task{}, nil
}

func (h *mockHandler) OnSendMessage(ctx context.Context, params *a2a.MessageSendParams) (a2a.SendMessageResult, error) {
	h.lastCallContext, _ = CallContextFrom(ctx)
	if h.OnSendMessageFn != nil {
		return h.OnSendMessageFn(ctx, params)
	}
	if h.resultErr != nil {
		return nil, h.resultErr
	}
	return &a2a.Task{}, nil
}

func (h *mockHandler) OnSendMessageStream(ctx context.Context, params *a2a.MessageSendParams) iter.Seq2[a2a.Event, error] {
	if h.OnSendMessageStreamFn != nil {
		return h.OnSendMessageStreamFn(ctx, params)
	}
	return func(yield func(a2a.Event, error) bool) {
		h.lastCallContext, _ = CallContextFrom(ctx)
		if h.resultErr != nil {
			yield(nil, h.resultErr)
			return
		}
		yield(&a2a.Task{}, nil)
	}
}

func (h *mockHandler) OnResubscribeToTask(ctx context.Context, params *a2a.TaskIDParams) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		h.lastCallContext, _ = CallContextFrom(ctx)
		if h.resultErr != nil {
			yield(nil, h.resultErr)
			return
		}
		yield(&a2a.Task{}, nil)
	}
}

func (h *mockHandler) OnGetTaskPushConfig(ctx context.Context, params *a2a.GetTaskPushConfigParams) (*a2a.TaskPushConfig, error) {
	h.lastCallContext, _ = CallContextFrom(ctx)
	if h.resultErr != nil {
		return nil, h.resultErr
	}
	return &a2a.TaskPushConfig{}, h.resultErr
}

func (h *mockHandler) OnListTaskPushConfig(ctx context.Context, params *a2a.ListTaskPushConfigParams) ([]*a2a.TaskPushConfig, error) {
	h.lastCallContext, _ = CallContextFrom(ctx)
	if h.resultErr != nil {
		return nil, h.resultErr
	}
	return []*a2a.TaskPushConfig{{}}, nil
}

func (h *mockHandler) OnSetTaskPushConfig(ctx context.Context, params *a2a.TaskPushConfig) (*a2a.TaskPushConfig, error) {
	h.lastCallContext, _ = CallContextFrom(ctx)
	if h.resultErr != nil {
		return nil, h.resultErr
	}
	return &a2a.TaskPushConfig{}, h.resultErr
}

func (h *mockHandler) OnDeleteTaskPushConfig(ctx context.Context, params *a2a.DeleteTaskPushConfigParams) error {
	h.lastCallContext, _ = CallContextFrom(ctx)
	return h.resultErr
}

func (h *mockHandler) OnGetExtendedAgentCard(ctx context.Context) (*a2a.AgentCard, error) {
	h.lastCallContext, _ = CallContextFrom(ctx)
	if h.resultErr != nil {
		return nil, h.resultErr
	}
	return &a2a.AgentCard{}, nil
}

type mockInterceptor struct {
	beforeFn func(ctx context.Context, callCtx *CallContext, req *Request) (context.Context, error)
	afterFn  func(ctx context.Context, callCtx *CallContext, resp *Response) error
}

func (mi *mockInterceptor) Before(ctx context.Context, callCtx *CallContext, req *Request) (context.Context, error) {
	if mi.beforeFn != nil {
		return mi.beforeFn(ctx, callCtx, req)
	}
	return ctx, nil
}

func (mi *mockInterceptor) After(ctx context.Context, callCtx *CallContext, resp *Response) error {
	if mi.afterFn != nil {
		return mi.afterFn(ctx, callCtx, resp)
	}
	return nil
}

func handleSingleItemSeq(seq iter.Seq2[a2a.Event, error]) (a2a.Event, error) {
	count := 0
	var lastEvent a2a.Event
	var lastErr error
	for ev, err := range seq {
		lastEvent, lastErr, count = ev, err, count+1
	}
	if count != 1 {
		return nil, fmt.Errorf("got %d events, want 1", count)
	}
	return lastEvent, lastErr
}

var methodCalls = []struct {
	method string
	call   func(ctx context.Context, h RequestHandler) (any, error)
}{
	{
		method: "OnGetTask",
		call: func(ctx context.Context, h RequestHandler) (any, error) {
			return h.OnGetTask(ctx, &a2a.TaskQueryParams{})
		},
	},
	{
		method: "OnCancelTask",
		call: func(ctx context.Context, h RequestHandler) (any, error) {
			return h.OnCancelTask(ctx, &a2a.TaskIDParams{})
		},
	},
	{
		method: "OnSendMessage",
		call: func(ctx context.Context, h RequestHandler) (any, error) {
			return h.OnSendMessage(ctx, &a2a.MessageSendParams{})
		},
	},
	{
		method: "OnSendMessageStream",
		call: func(ctx context.Context, h RequestHandler) (any, error) {
			return handleSingleItemSeq(h.OnSendMessageStream(ctx, &a2a.MessageSendParams{}))
		},
	},
	{
		method: "OnResubscribeToTask",
		call: func(ctx context.Context, h RequestHandler) (any, error) {
			return handleSingleItemSeq(h.OnResubscribeToTask(ctx, &a2a.TaskIDParams{}))
		},
	},
	{
		method: "OnListTaskPushConfig",
		call: func(ctx context.Context, h RequestHandler) (any, error) {
			return h.OnListTaskPushConfig(ctx, &a2a.ListTaskPushConfigParams{})
		},
	},
	{
		method: "OnSetTaskPushConfig",
		call: func(ctx context.Context, h RequestHandler) (any, error) {
			return h.OnSetTaskPushConfig(ctx, &a2a.TaskPushConfig{})
		},
	},
	{
		method: "OnGetTaskPushConfig",
		call: func(ctx context.Context, h RequestHandler) (any, error) {
			return h.OnGetTaskPushConfig(ctx, &a2a.GetTaskPushConfigParams{})
		},
	},
	{
		method: "OnDeleteTaskPushConfig",
		call: func(ctx context.Context, h RequestHandler) (any, error) {
			return nil, h.OnDeleteTaskPushConfig(ctx, &a2a.DeleteTaskPushConfigParams{})
		},
	},
	{
		method: "OnGetExtendedAgentCard",
		call: func(ctx context.Context, h RequestHandler) (any, error) {
			return h.OnGetExtendedAgentCard(ctx)
		},
	},
}

func TestInterceptedHandler_Auth(t *testing.T) {
	ctx := t.Context()
	mockHandler, mockInterceptor := &mockHandler{}, &mockInterceptor{}
	handler := &InterceptedHandler{Handler: mockHandler, Interceptors: []CallInterceptor{mockInterceptor}}

	var capturedCallCtx *CallContext
	mockHandler.OnSendMessageFn = func(ctx context.Context, params *a2a.MessageSendParams) (a2a.SendMessageResult, error) {
		if callCtx, ok := CallContextFrom(ctx); ok {
			capturedCallCtx = callCtx
		}
		return a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: "Hi!"}), nil
	}

	type testUser struct{ *AuthenticatedUser }

	mockInterceptor.beforeFn = func(ctx context.Context, callCtx *CallContext, req *Request) (context.Context, error) {
		callCtx.User = &testUser{}
		return ctx, nil
	}

	_, _ = handler.OnSendMessage(ctx, &a2a.MessageSendParams{})

	if !capturedCallCtx.User.Authenticated() {
		t.Fatal("CallContext.User.Authenticated() = false, want true")
	}
	if _, ok := capturedCallCtx.User.(*testUser); !ok {
		t.Fatalf("CallContext.User.(type) = %T, want *testUser", capturedCallCtx.User)
	}
}

func TestInterceptedHandler_RequestResponseModification(t *testing.T) {
	ctx := t.Context()
	mockHandler, mockInterceptor := &mockHandler{}, &mockInterceptor{}
	handler := &InterceptedHandler{Handler: mockHandler, Interceptors: []CallInterceptor{mockInterceptor}}

	var capturedRequest *a2a.MessageSendParams
	mockHandler.OnSendMessageFn = func(ctx context.Context, params *a2a.MessageSendParams) (a2a.SendMessageResult, error) {
		capturedRequest = params
		return a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: "Hi!"}), nil
	}

	wantReqKey, wantReqVal := "reqKey", 42
	mockInterceptor.beforeFn = func(ctx context.Context, callCtx *CallContext, req *Request) (context.Context, error) {
		payload := req.Payload.(*a2a.MessageSendParams)
		payload.Metadata = map[string]any{wantReqKey: wantReqVal}
		return ctx, nil
	}

	wantRespKey, wantRespVal := "respKey", 43
	mockInterceptor.afterFn = func(ctx context.Context, callCtx *CallContext, resp *Response) error {
		payload := resp.Payload.(*a2a.Message)
		payload.Metadata = map[string]any{wantRespKey: wantRespVal}
		return nil
	}

	request := &a2a.MessageSendParams{Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: "Hello!"})}
	response, err := handler.OnSendMessage(ctx, request)
	if mockHandler.lastCallContext.method != "OnSendMessage" {
		t.Fatalf("handler.OnSendMessage() CallContext = %v, want method=OnSendMessage", mockHandler.lastCallContext)
	}
	if err != nil {
		t.Fatalf("handler.OnSendMessage() error = %v, want nil", err)
	}
	if capturedRequest.Metadata[wantReqKey] != wantReqVal {
		t.Fatalf("OnSendMessage() Request.Metadata[%q] = %v, want %d", wantReqKey, capturedRequest.Metadata[wantReqKey], wantReqVal)
	}
	responsMsg := response.(*a2a.Message)
	if responsMsg.Metadata[wantRespKey] != wantRespVal {
		t.Fatalf("OnSendMessage() Response.Metadata[%q] = %v, want %d", wantRespKey, responsMsg.Metadata[wantRespKey], wantRespVal)
	}
}

func TestInterceptedHandler_RequestModification(t *testing.T) {
	ctx := t.Context()
	mockHandler, mockInterceptor := &mockHandler{}, &mockInterceptor{}
	handler := &InterceptedHandler{Handler: mockHandler, Interceptors: []CallInterceptor{mockInterceptor}}
	originalParams := &a2a.MessageSendParams{
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: "Hello!"}),
	}
	var receivedParams *a2a.MessageSendParams

	mockHandler.OnSendMessageFn = func(ctx context.Context, params *a2a.MessageSendParams) (a2a.SendMessageResult, error) {
		receivedParams = params
		message := params.Message.Parts[0].(a2a.TextPart).Text
		return a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: message}), nil
	}

	mockInterceptor.beforeFn = func(ctx context.Context, callCtx *CallContext, req *Request) (context.Context, error) {
		if _, ok := req.Payload.(*a2a.MessageSendParams); ok {
			req.Payload = &a2a.MessageSendParams{
				Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: "Modified!"}),
			}
		}
		return ctx, nil
	}

	resp, err := handler.OnSendMessage(ctx, originalParams)
	if err != nil {
		t.Fatalf("handler.OnSendMessage() error = %v, want nil", err)
	}
	if mockHandler.lastCallContext.method != "OnSendMessage" {
		t.Fatalf("handler.OnSendMessage() CallContext = %v, want method=OnSendMessage", mockHandler.lastCallContext)
	}
	if receivedParams == originalParams {
		t.Fatalf("handler.OnSendMessage() receivedParams = %v, want %v", receivedParams, originalParams)
	}
	reqMsg := resp.(*a2a.Message)
	if reqMsg.Parts[0].(a2a.TextPart).Text != "Modified!" {
		t.Fatalf("handler.OnSendMessage() Request.Text = %q, want %q", reqMsg.Parts[0].(a2a.TextPart).Text, "Modified!")
	}
}

func TestInterceptedHandler_ResponseAndErrorModification(t *testing.T) {
	injectedErr := fmt.Errorf("injected error")
	handlerErr := fmt.Errorf("handler error")

	tests := []struct {
		name          string
		handlerResp   a2a.SendMessageResult
		handlerErr    error
		interceptorFn func(ctx context.Context, callCtx *CallContext, resp *Response) error
		wantErr       error
		wantRespText  string
	}{
		{
			name:        "replace response object",
			handlerResp: a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: "Original!"}),
			interceptorFn: func(ctx context.Context, callCtx *CallContext, resp *Response) error {
				resp.Payload = a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: "Modified!"})
				return nil
			},
			wantRespText: "Modified!",
		},
		{
			name:        "injected error: handler success, interceptor error",
			handlerResp: a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: "Success!"}),
			interceptorFn: func(ctx context.Context, callCtx *CallContext, resp *Response) error {
				resp.Err = injectedErr
				return nil
			},
			wantErr: injectedErr,
		},
		{
			name:       "injected error: handler error, interceptor success",
			handlerErr: handlerErr,
			interceptorFn: func(ctx context.Context, callCtx *CallContext, resp *Response) error {
				if resp.Err != nil {
					resp.Err = nil

					resp.Payload = a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: "Recovered from error!"})
				}
				return nil
			},
			wantErr:      nil,
			wantRespText: "Recovered from error!",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			mockHandler, mockInterceptor := &mockHandler{}, &mockInterceptor{}
			handler := &InterceptedHandler{Handler: mockHandler, Interceptors: []CallInterceptor{mockInterceptor}}

			mockHandler.OnSendMessageFn = func(ctx context.Context, params *a2a.MessageSendParams) (a2a.SendMessageResult, error) {
				return tt.handlerResp, tt.handlerErr
			}

			mockInterceptor.afterFn = tt.interceptorFn

			resp, err := handler.OnSendMessage(ctx, &a2a.MessageSendParams{
				Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: "Hello!"}),
			})

			if !errors.Is(err, tt.wantErr) {
				t.Errorf("handler.OnSendMessage() error = %v, want %v", err, tt.wantErr)
			}

			if tt.wantErr == nil {
				if resp == nil {
					t.Errorf("handler.OnSendMessage() resp = nil, want %v", tt.wantRespText)
				}
				msg := resp.(*a2a.Message)
				if msg.Parts[0].(a2a.TextPart).Text != tt.wantRespText {
					t.Errorf("handler.OnSendMessage() resp.Text = %q, want %q", msg.Parts[0].(a2a.TextPart).Text, tt.wantRespText)
				}
			}
		})
	}
}

func TestInterceptedHandler_TypeSafety(t *testing.T) {
	ctx := t.Context()
	mockHandler, mockInterceptor := &mockHandler{}, &mockInterceptor{}
	handler := &InterceptedHandler{Handler: mockHandler, Interceptors: []CallInterceptor{mockInterceptor}}

	mockInterceptor.beforeFn = func(ctx context.Context, callCtx *CallContext, req *Request) (context.Context, error) {
		req.Payload = &a2a.Task{}
		return ctx, nil
	}

	_, err := handler.OnSendMessage(ctx, &a2a.MessageSendParams{})

	if err == nil {
		t.Fatal("got nil error, want error due to payload type mismatch")
	}
	expectedErrorFragment := "payload type changed"
	if !strings.Contains(err.Error(), expectedErrorFragment) {
		t.Errorf("Error = %q, want it to contain %q", err.Error(), expectedErrorFragment)
	}
}

func TestInterceptedHandler_InterceptorOrdering(t *testing.T) {
	ctx := t.Context()
	mockHandler := &mockHandler{}

	beforeCalls := []int{}
	afterCalls := []int{}
	createInterceptor := func(pos int) *mockInterceptor {
		return &mockInterceptor{
			beforeFn: func(ctx context.Context, callCtx *CallContext, resp *Request) (context.Context, error) {
				beforeCalls = append(beforeCalls, pos)
				return ctx, nil
			},
			afterFn: func(ctx context.Context, callCtx *CallContext, resp *Response) error {
				afterCalls = append(afterCalls, pos)
				return nil
			},
		}
	}

	interceptor1, interceptor2 := createInterceptor(1), createInterceptor(2)
	handler := &InterceptedHandler{Handler: mockHandler, Interceptors: []CallInterceptor{interceptor1, interceptor2}}

	_, _ = handler.OnGetTask(ctx, &a2a.TaskQueryParams{})

	wantBefore := []int{1, 2}
	if !reflect.DeepEqual(beforeCalls, wantBefore) {
		t.Errorf("Before() invocation order = %v, want %v", beforeCalls, wantBefore)
	}
	wantAfter := []int{2, 1}
	if !reflect.DeepEqual(afterCalls, wantAfter) {
		t.Errorf("After() invocation order = %v, want %v", afterCalls, wantAfter)
	}
}

func TestInterceptedHandler_EveryStreamValueIntercepted(t *testing.T) {
	ctx := t.Context()
	mockHandler, mockInterceptor := &mockHandler{}, &mockInterceptor{}
	handler := &InterceptedHandler{Handler: mockHandler, Interceptors: []CallInterceptor{mockInterceptor}}

	totalCount := 5
	mockHandler.OnSendMessageStreamFn = func(ctx context.Context, params *a2a.MessageSendParams) iter.Seq2[a2a.Event, error] {
		return func(yield func(a2a.Event, error) bool) {
			for range totalCount {
				if !yield(&a2a.TaskStatusUpdateEvent{Metadata: map[string]any{"count": 0}}, nil) {
					return
				}
			}
		}
	}

	countKey := "count"
	afterCount := 0
	mockInterceptor.afterFn = func(ctx context.Context, callCtx *CallContext, resp *Response) error {
		ev := resp.Payload.(*a2a.TaskStatusUpdateEvent)
		ev.Metadata[countKey] = afterCount
		afterCount++
		return nil
	}

	count := 0
	request := &a2a.MessageSendParams{Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: "Hello!"})}
	for ev, err := range handler.OnSendMessageStream(ctx, request) {
		if err != nil {
			t.Fatalf("handler.OnSendMessageStream() error %v, want nil", err)
		}
		if ev.Meta()[countKey] != count {
			t.Fatalf("event.Meta()[%q] = %v, want %v", countKey, ev.Meta()[countKey], count)
		}
		count++
	}

	if count != afterCount {
		t.Fatalf("handler.OnSendMessageStream() produced %d events, want %d", count, totalCount)
	}
}

func TestInterceptedHandler_CallContextPropagation(t *testing.T) {
	for _, tc := range methodCalls {
		t.Run(tc.method, func(t *testing.T) {
			ctx := t.Context()
			mockHandler, mockInterceptor := &mockHandler{}, &mockInterceptor{}
			handler := &InterceptedHandler{Handler: mockHandler, Interceptors: []CallInterceptor{mockInterceptor}}

			wantActiveExtension := &a2a.AgentExtension{URI: "https://test.com"}

			var beforeCallCtx *CallContext
			mockInterceptor.beforeFn = func(ctx context.Context, callCtx *CallContext, req *Request) (context.Context, error) {
				callCtx.Extensions().Activate(wantActiveExtension)
				beforeCallCtx = callCtx
				return ctx, nil
			}
			var afterCallCtx *CallContext
			mockInterceptor.afterFn = func(ctx context.Context, callCtx *CallContext, resp *Response) error {
				afterCallCtx = callCtx
				return nil
			}

			key := ExtensionsMetaKey
			wantVal := "test"
			meta := map[string][]string{key: {wantVal}}
			ctx, callCtx := WithCallContext(ctx, NewRequestMeta(meta))
			_, _ = tc.call(ctx, handler)

			if beforeCallCtx != afterCallCtx {
				t.Error("want Before() CallContext to be the same as After() CallContext")
			}
			if beforeCallCtx != callCtx {
				t.Error("want CallContext to be the same as provided by the caller")
			}
			gotVal, ok := beforeCallCtx.RequestMeta().Get(key)
			if !ok || len(gotVal) != 1 || gotVal[0] != wantVal {
				t.Errorf("%s() RequestMeta().Get(%s) = (%v, %v), want ([%q] true)", tc.method, key, gotVal, ok, wantVal)
			}
			if !callCtx.Extensions().Active(wantActiveExtension) {
				t.Errorf("%s() Extensions().Active(%q) = false, want true", tc.method, wantActiveExtension.URI)
			}
		})
	}
}

func TestInterceptedHandler_ContextDataPassing(t *testing.T) {
	for _, tc := range methodCalls {
		t.Run(tc.method, func(t *testing.T) {
			ctx := t.Context()
			mockHandler, mockInterceptor := &mockHandler{}, &mockInterceptor{}
			handler := &InterceptedHandler{Handler: mockHandler, Interceptors: []CallInterceptor{mockInterceptor}}

			type contextKey struct{}
			wantVal := 42
			mockInterceptor.beforeFn = func(ctx context.Context, callCtx *CallContext, req *Request) (context.Context, error) {
				return context.WithValue(ctx, contextKey{}, wantVal), nil
			}
			var gotVal any
			mockInterceptor.afterFn = func(ctx context.Context, callCtx *CallContext, resp *Response) error {
				gotVal = ctx.Value(contextKey{})
				return nil
			}
			_, _ = tc.call(ctx, handler)

			if gotVal != wantVal {
				t.Errorf("After() Context.Value() = %v, want %d", gotVal, wantVal)
			}
		})
	}
}

func TestInterceptedHandler_RejectRequest(t *testing.T) {
	for _, tc := range methodCalls {
		t.Run(tc.method, func(t *testing.T) {
			ctx := t.Context()
			mockHandler, mockInterceptor := &mockHandler{}, &mockInterceptor{}
			handler := &InterceptedHandler{Handler: mockHandler, Interceptors: []CallInterceptor{mockInterceptor}}

			wantErr := errors.New("rejected")
			mockInterceptor.beforeFn = func(context.Context, *CallContext, *Request) (context.Context, error) {
				return nil, wantErr
			}
			_, gotErr := tc.call(ctx, handler)

			if mockHandler.lastCallContext != nil {
				t.Error("mockHandler was invoked, want Before to reject request")
			}
			if !errors.Is(gotErr, wantErr) {
				t.Errorf("%s() error = %v, want %v", tc.method, gotErr, wantErr)
			}
		})
	}
}

func TestInterceptedHandler_RejectResponse(t *testing.T) {
	for _, tc := range methodCalls {
		t.Run(tc.method, func(t *testing.T) {
			ctx := t.Context()
			mockHandler, mockInterceptor := &mockHandler{}, &mockInterceptor{}
			handler := &InterceptedHandler{Handler: mockHandler, Interceptors: []CallInterceptor{mockInterceptor}}

			wantInterceptErr := errors.New("ignored")
			mockHandler.resultErr = wantInterceptErr

			wantErr := errors.New("rejected")
			var interceptedErr error
			mockInterceptor.afterFn = func(ctx context.Context, callCtx *CallContext, resp *Response) error {
				interceptedErr = resp.Err
				return wantErr
			}

			_, gotErr := tc.call(ctx, handler)
			if mockHandler.lastCallContext.Method() != tc.method {
				t.Errorf("%s() CallContext.Method() = %v, want %s", tc.method, mockHandler.lastCallContext.Method(), tc.method)
			}
			if !errors.Is(interceptedErr, wantInterceptErr) {
				t.Errorf("After() Response.Err = %v, want %v", interceptedErr, wantInterceptErr)
			}
			if !errors.Is(gotErr, wantErr) {
				t.Errorf("%s() error = %v, want %v", tc.method, gotErr, wantErr)
			}
		})
	}
}
