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
	OnGetTaskFn           func(context.Context, *a2a.GetTaskRequest) (*a2a.Task, error)
	OnSendMessageFn       func(context.Context, *a2a.SendMessageRequest) (a2a.SendMessageResult, error)
	OnSendMessageStreamFn func(context.Context, *a2a.SendMessageRequest) iter.Seq2[a2a.Event, error]
}

var _ RequestHandler = (*mockHandler)(nil)

func (h *mockHandler) GetTask(ctx context.Context, query *a2a.GetTaskRequest) (*a2a.Task, error) {
	h.lastCallContext, _ = CallContextFrom(ctx)
	if h.OnGetTaskFn != nil {
		return h.OnGetTaskFn(ctx, query)
	}
	if h.resultErr != nil {
		return nil, h.resultErr
	}
	return &a2a.Task{}, nil
}

func (h *mockHandler) ListTasks(ctx context.Context, req *a2a.ListTasksRequest) (*a2a.ListTasksResponse, error) {
	h.lastCallContext, _ = CallContextFrom(ctx)
	if h.resultErr != nil {
		return nil, h.resultErr
	}
	return &a2a.ListTasksResponse{}, nil
}

func (h *mockHandler) CancelTask(ctx context.Context, params *a2a.CancelTaskRequest) (*a2a.Task, error) {
	h.lastCallContext, _ = CallContextFrom(ctx)
	if h.resultErr != nil {
		return nil, h.resultErr
	}
	return &a2a.Task{}, nil
}

func (h *mockHandler) SendMessage(ctx context.Context, params *a2a.SendMessageRequest) (a2a.SendMessageResult, error) {
	h.lastCallContext, _ = CallContextFrom(ctx)
	if h.OnSendMessageFn != nil {
		return h.OnSendMessageFn(ctx, params)
	}
	if h.resultErr != nil {
		return nil, h.resultErr
	}
	return &a2a.Task{}, nil
}

func (h *mockHandler) SendStreamingMessage(ctx context.Context, params *a2a.SendMessageRequest) iter.Seq2[a2a.Event, error] {
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

func (h *mockHandler) SubscribeToTask(ctx context.Context, params *a2a.SubscribeToTaskRequest) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		h.lastCallContext, _ = CallContextFrom(ctx)
		if h.resultErr != nil {
			yield(nil, h.resultErr)
			return
		}
		yield(&a2a.Task{}, nil)
	}
}

func (h *mockHandler) GetTaskPushConfig(ctx context.Context, params *a2a.GetTaskPushConfigRequest) (*a2a.TaskPushConfig, error) {
	h.lastCallContext, _ = CallContextFrom(ctx)
	if h.resultErr != nil {
		return nil, h.resultErr
	}
	return &a2a.TaskPushConfig{}, h.resultErr
}

func (h *mockHandler) ListTaskPushConfig(ctx context.Context, params *a2a.ListTaskPushConfigRequest) ([]*a2a.TaskPushConfig, error) {
	h.lastCallContext, _ = CallContextFrom(ctx)
	if h.resultErr != nil {
		return nil, h.resultErr
	}
	return []*a2a.TaskPushConfig{{}}, nil
}

func (h *mockHandler) CreateTaskPushConfig(ctx context.Context, params *a2a.CreateTaskPushConfigRequest) (*a2a.TaskPushConfig, error) {
	h.lastCallContext, _ = CallContextFrom(ctx)
	if h.resultErr != nil {
		return nil, h.resultErr
	}
	return &a2a.TaskPushConfig{}, h.resultErr
}

func (h *mockHandler) DeleteTaskPushConfig(ctx context.Context, params *a2a.DeleteTaskPushConfigRequest) error {
	h.lastCallContext, _ = CallContextFrom(ctx)
	return h.resultErr
}

func (h *mockHandler) GetExtendedAgentCard(ctx context.Context) (*a2a.AgentCard, error) {
	h.lastCallContext, _ = CallContextFrom(ctx)
	if h.resultErr != nil {
		return nil, h.resultErr
	}
	return &a2a.AgentCard{}, nil
}

type mockInterceptor struct {
	beforeFn func(ctx context.Context, callCtx *CallContext, req *Request) (context.Context, any, error)
	afterFn  func(ctx context.Context, callCtx *CallContext, resp *Response) error
}

func (mi *mockInterceptor) Before(ctx context.Context, callCtx *CallContext, req *Request) (context.Context, any, error) {
	if mi.beforeFn != nil {
		return mi.beforeFn(ctx, callCtx, req)
	}
	return ctx, nil, nil
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
		method: "GetTask",
		call: func(ctx context.Context, h RequestHandler) (any, error) {
			return h.GetTask(ctx, &a2a.GetTaskRequest{})
		},
	},
	{
		method: "ListTasks",
		call: func(ctx context.Context, h RequestHandler) (any, error) {
			return h.ListTasks(ctx, &a2a.ListTasksRequest{})
		},
	},
	{
		method: "CancelTask",
		call: func(ctx context.Context, h RequestHandler) (any, error) {
			return h.CancelTask(ctx, &a2a.CancelTaskRequest{})
		},
	},
	{
		method: "SendMessage",
		call: func(ctx context.Context, h RequestHandler) (any, error) {
			return h.SendMessage(ctx, &a2a.SendMessageRequest{})
		},
	},
	{
		method: "SendStreamingMessage",
		call: func(ctx context.Context, h RequestHandler) (any, error) {
			return handleSingleItemSeq(h.SendStreamingMessage(ctx, &a2a.SendMessageRequest{}))
		},
	},
	{
		method: "SubscribeToTask",
		call: func(ctx context.Context, h RequestHandler) (any, error) {
			return handleSingleItemSeq(h.SubscribeToTask(ctx, &a2a.SubscribeToTaskRequest{}))
		},
	},
	{
		method: "ListTaskPushConfig",
		call: func(ctx context.Context, h RequestHandler) (any, error) {
			return h.ListTaskPushConfig(ctx, &a2a.ListTaskPushConfigRequest{})
		},
	},
	{
		method: "CreateTaskPushConfig",
		call: func(ctx context.Context, h RequestHandler) (any, error) {
			return h.CreateTaskPushConfig(ctx, &a2a.CreateTaskPushConfigRequest{})
		},
	},
	{
		method: "GetTaskPushConfig",
		call: func(ctx context.Context, h RequestHandler) (any, error) {
			return h.GetTaskPushConfig(ctx, &a2a.GetTaskPushConfigRequest{})
		},
	},
	{
		method: "DeleteTaskPushConfig",
		call: func(ctx context.Context, h RequestHandler) (any, error) {
			return nil, h.DeleteTaskPushConfig(ctx, &a2a.DeleteTaskPushConfigRequest{})
		},
	},
	{
		method: "GetExtendedAgentCard",
		call: func(ctx context.Context, h RequestHandler) (any, error) {
			return h.GetExtendedAgentCard(ctx)
		},
	},
}

func TestInterceptedHandler_Auth(t *testing.T) {
	ctx := t.Context()
	mockHandler, mockInterceptor := &mockHandler{}, &mockInterceptor{}
	handler := &InterceptedHandler{Handler: mockHandler, Interceptors: []CallInterceptor{mockInterceptor}}

	var capturedCallCtx *CallContext
	mockHandler.OnSendMessageFn = func(ctx context.Context, params *a2a.SendMessageRequest) (a2a.SendMessageResult, error) {
		if callCtx, ok := CallContextFrom(ctx); ok {
			capturedCallCtx = callCtx
		}
		return a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("Hi!")), nil
	}

	mockInterceptor.beforeFn = func(ctx context.Context, callCtx *CallContext, req *Request) (context.Context, any, error) {
		callCtx.User = NewAuthenticatedUser("test", nil)
		return ctx, nil, nil
	}

	_, _ = handler.SendMessage(ctx, &a2a.SendMessageRequest{})

	if !capturedCallCtx.User.Authenticated {
		t.Fatal("CallContext.User.Authenticated = false, want true")
	}
	if capturedCallCtx.User.Name != "test" {
		t.Fatalf("CallContext.User.Name = %s, want test", capturedCallCtx.User.Name)
	}
}

func TestInterceptedHandler_RequestResponseModification(t *testing.T) {
	ctx := t.Context()
	mockHandler, mockInterceptor := &mockHandler{}, &mockInterceptor{}
	handler := &InterceptedHandler{Handler: mockHandler, Interceptors: []CallInterceptor{mockInterceptor}}

	var capturedRequest *a2a.SendMessageRequest
	mockHandler.OnSendMessageFn = func(ctx context.Context, params *a2a.SendMessageRequest) (a2a.SendMessageResult, error) {
		capturedRequest = params
		return a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("Hi!")), nil
	}

	wantReqKey, wantReqVal := "reqKey", 42
	mockInterceptor.beforeFn = func(ctx context.Context, callCtx *CallContext, req *Request) (context.Context, any, error) {
		payload := req.Payload.(*a2a.SendMessageRequest)
		payload.Metadata = map[string]any{wantReqKey: wantReqVal}
		return ctx, nil, nil
	}

	wantRespKey, wantRespVal := "respKey", 43
	mockInterceptor.afterFn = func(ctx context.Context, callCtx *CallContext, resp *Response) error {
		payload := resp.Payload.(*a2a.Message)
		payload.Metadata = map[string]any{wantRespKey: wantRespVal}
		return nil
	}

	request := &a2a.SendMessageRequest{Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("Hello!"))}
	response, err := handler.SendMessage(ctx, request)
	if mockHandler.lastCallContext.method != "SendMessage" {
		t.Fatalf("handler.SendMessage() CallContext = %v, want method=SendMessage", mockHandler.lastCallContext)
	}
	if err != nil {
		t.Fatalf("handler.SendMessage() error = %v, want nil", err)
	}
	if capturedRequest.Metadata[wantReqKey] != wantReqVal {
		t.Fatalf("SendMessage() Request.Metadata[%q] = %v, want %d", wantReqKey, capturedRequest.Metadata[wantReqKey], wantReqVal)
	}
	responsMsg := response.(*a2a.Message)
	if responsMsg.Metadata[wantRespKey] != wantRespVal {
		t.Fatalf("SendMessage() Response.Metadata[%q] = %v, want %d", wantRespKey, responsMsg.Metadata[wantRespKey], wantRespVal)
	}
}

func TestInterceptedHandler_RequestModification(t *testing.T) {
	ctx := t.Context()
	mockHandler, mockInterceptor := &mockHandler{}, &mockInterceptor{}
	handler := &InterceptedHandler{Handler: mockHandler, Interceptors: []CallInterceptor{mockInterceptor}}
	originalParams := &a2a.SendMessageRequest{
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("Hello!")),
	}
	var receivedParams *a2a.SendMessageRequest

	mockHandler.OnSendMessageFn = func(ctx context.Context, params *a2a.SendMessageRequest) (a2a.SendMessageResult, error) {
		receivedParams = params
		message := string(params.Message.Parts[0].Content.(a2a.Text))
		return a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart(message)), nil
	}

	mockInterceptor.beforeFn = func(ctx context.Context, callCtx *CallContext, req *Request) (context.Context, any, error) {
		if _, ok := req.Payload.(*a2a.SendMessageRequest); ok {
			req.Payload = &a2a.SendMessageRequest{
				Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("Modified!")),
			}
		}
		return ctx, nil, nil
	}

	resp, err := handler.SendMessage(ctx, originalParams)
	if err != nil {
		t.Fatalf("handler.SendMessage() error = %v, want nil", err)
	}
	if mockHandler.lastCallContext.method != "SendMessage" {
		t.Fatalf("handler.SendMessage() CallContext = %v, want method=SendMessage", mockHandler.lastCallContext)
	}
	if receivedParams == originalParams {
		t.Fatalf("handler.SendMessage() receivedParams = %v, want %v", receivedParams, originalParams)
	}
	reqMsg := resp.(*a2a.Message)
	if string(reqMsg.Parts[0].Content.(a2a.Text)) != "Modified!" {
		t.Fatalf("handler.SendMessage() Request.Text = %q, want %q", string(reqMsg.Parts[0].Content.(a2a.Text)), "Modified!")
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
			handlerResp: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("Original!")),
			interceptorFn: func(ctx context.Context, callCtx *CallContext, resp *Response) error {
				resp.Payload = a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("Modified!"))
				return nil
			},
			wantRespText: "Modified!",
		},
		{
			name:        "injected error: handler success, interceptor error",
			handlerResp: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("Success!")),
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

					resp.Payload = a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("Recovered from error!"))
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

			mockHandler.OnSendMessageFn = func(ctx context.Context, params *a2a.SendMessageRequest) (a2a.SendMessageResult, error) {
				return tt.handlerResp, tt.handlerErr
			}

			mockInterceptor.afterFn = tt.interceptorFn

			resp, err := handler.SendMessage(ctx, &a2a.SendMessageRequest{
				Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("Hello!")),
			})

			if !errors.Is(err, tt.wantErr) {
				t.Errorf("handler.OnSendMessage() error = %v, want %v", err, tt.wantErr)
			}

			if tt.wantErr == nil {
				if resp == nil {
					t.Errorf("handler.OnSendMessage() resp = nil, want %v", tt.wantRespText)
				}
				msg := resp.(*a2a.Message)
				if string(msg.Parts[0].Content.(a2a.Text)) != tt.wantRespText {
					t.Errorf("handler.OnSendMessage() resp.Text = %q, want %q", string(msg.Parts[0].Content.(a2a.Text)), tt.wantRespText)
				}
			}
		})
	}
}

func TestInterceptedHandler_TypeSafety(t *testing.T) {
	ctx := t.Context()
	mockHandler, mockInterceptor := &mockHandler{}, &mockInterceptor{}
	handler := &InterceptedHandler{Handler: mockHandler, Interceptors: []CallInterceptor{mockInterceptor}}

	mockInterceptor.beforeFn = func(ctx context.Context, callCtx *CallContext, req *Request) (context.Context, any, error) {
		req.Payload = &a2a.Task{}
		return ctx, nil, nil
	}

	_, err := handler.SendMessage(ctx, &a2a.SendMessageRequest{})

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
			beforeFn: func(ctx context.Context, callCtx *CallContext, resp *Request) (context.Context, any, error) {
				beforeCalls = append(beforeCalls, pos)
				return ctx, nil, nil
			},
			afterFn: func(ctx context.Context, callCtx *CallContext, resp *Response) error {
				afterCalls = append(afterCalls, pos)
				return nil
			},
		}
	}

	interceptor1, interceptor2 := createInterceptor(1), createInterceptor(2)
	handler := &InterceptedHandler{Handler: mockHandler, Interceptors: []CallInterceptor{interceptor1, interceptor2}}

	_, _ = handler.GetTask(ctx, &a2a.GetTaskRequest{})

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
	mockHandler.OnSendMessageStreamFn = func(ctx context.Context, params *a2a.SendMessageRequest) iter.Seq2[a2a.Event, error] {
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
	request := &a2a.SendMessageRequest{Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("Hello!"))}
	for ev, err := range handler.SendStreamingMessage(ctx, request) {
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
			mockInterceptor.beforeFn = func(ctx context.Context, callCtx *CallContext, req *Request) (context.Context, any, error) {
				callCtx.Extensions().Activate(wantActiveExtension)
				beforeCallCtx = callCtx
				return ctx, nil, nil
			}
			var afterCallCtx *CallContext
			mockInterceptor.afterFn = func(ctx context.Context, callCtx *CallContext, resp *Response) error {
				afterCallCtx = callCtx
				return nil
			}

			key := ExtensionsMetaKey
			wantVal := "test"
			meta := map[string][]string{key: {wantVal}}
			ctx, callCtx := WithCallContext(ctx, NewServiceParams(meta))
			_, _ = tc.call(ctx, handler)

			if beforeCallCtx != afterCallCtx {
				t.Error("want Before() CallContext to be the same as After() CallContext")
			}
			if beforeCallCtx != callCtx {
				t.Error("want CallContext to be the same as provided by the caller")
			}
			gotVal, ok := beforeCallCtx.ServiceParams().Get(key)
			if !ok || len(gotVal) != 1 || gotVal[0] != wantVal {
				t.Errorf("%s() ServiceParams().Get(%s) = (%v, %v), want ([%q] true)", tc.method, key, gotVal, ok, wantVal)
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
			mockInterceptor.beforeFn = func(ctx context.Context, callCtx *CallContext, req *Request) (context.Context, any, error) {
				return context.WithValue(ctx, contextKey{}, wantVal), nil, nil
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
			mockInterceptor.beforeFn = func(context.Context, *CallContext, *Request) (context.Context, any, error) {
				return nil, nil, wantErr
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

func TestInterceptedHandler_EarlyReturn(t *testing.T) {
	ctx := t.Context()
	originalQuery := &a2a.GetTaskRequest{ID: "original"}
	earlyResult := &a2a.Task{ID: "early-cached-result"}

	mockHandler, interceptor1, interceptor2, interceptor3 := &mockHandler{}, &mockInterceptor{}, &mockInterceptor{}, &mockInterceptor{}

	handlerCalled := false
	mockHandler.OnGetTaskFn = func(ctx context.Context, query *a2a.GetTaskRequest) (*a2a.Task, error) {
		handlerCalled = true
		return nil, fmt.Errorf("handler should not be called")
	}

	var callOrder []string
	interceptor1.beforeFn = func(ctx context.Context, callCtx *CallContext, req *Request) (context.Context, any, error) {
		callOrder = append(callOrder, "1-Before")
		return ctx, nil, nil
	}
	interceptor1.afterFn = func(ctx context.Context, callCtx *CallContext, resp *Response) error {
		callOrder = append(callOrder, "1-After")
		return nil
	}
	// Interceptor 2 returns early result
	interceptor2.beforeFn = func(ctx context.Context, callCtx *CallContext, req *Request) (context.Context, any, error) {
		callOrder = append(callOrder, "2-Before")
		return ctx, earlyResult, nil
	}
	interceptor2.afterFn = func(ctx context.Context, callCtx *CallContext, resp *Response) error {
		callOrder = append(callOrder, "2-After")
		return nil
	}
	interceptor3.beforeFn = func(ctx context.Context, callCtx *CallContext, req *Request) (context.Context, any, error) {
		callOrder = append(callOrder, "3-Before")
		return ctx, nil, nil
	}

	handler := &InterceptedHandler{
		Handler:      mockHandler,
		Interceptors: []CallInterceptor{interceptor1, interceptor2, interceptor3},
	}

	response, err := handler.GetTask(ctx, originalQuery)
	if err != nil {
		t.Errorf("OnGetTask() error = %v, want nil", err)
	}
	if response != earlyResult {
		t.Errorf("OnGetTask() response = %v, want %v", response, earlyResult)
	}
	if handlerCalled {
		t.Error("Handler.OnGetTask() was called, want it to be skipped")
	}
	wantCallOrder := []string{"1-Before", "2-Before", "2-After", "1-After"}
	if !reflect.DeepEqual(callOrder, wantCallOrder) {
		t.Errorf("Call order = %v, want %v", callOrder, wantCallOrder)
	}
}
