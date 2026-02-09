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

package a2aclient

import (
	"context"
	"errors"
	"iter"
	"reflect"
	"testing"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/internal/utils"
	"github.com/google/go-cmp/cmp"
)

type testTransport struct {
	GetTaskFn              func(context.Context, *a2a.TaskQueryParams) (*a2a.Task, error)
	ListTasksFn            func(context.Context, *a2a.ListTasksRequest) (*a2a.ListTasksResponse, error)
	CancelTaskFn           func(context.Context, *a2a.TaskIDParams) (*a2a.Task, error)
	SendMessageFn          func(context.Context, *a2a.MessageSendParams) (a2a.SendMessageResult, error)
	ResubscribeToTaskFn    func(context.Context, *a2a.TaskIDParams) iter.Seq2[a2a.Event, error]
	SendStreamingMessageFn func(context.Context, *a2a.MessageSendParams) iter.Seq2[a2a.Event, error]
	GetTaskPushConfigFn    func(context.Context, *a2a.GetTaskPushConfigParams) (*a2a.TaskPushConfig, error)
	ListTaskPushConfigFn   func(context.Context, *a2a.ListTaskPushConfigParams) ([]*a2a.TaskPushConfig, error)
	SetTaskPushConfigFn    func(context.Context, *a2a.TaskPushConfig) (*a2a.TaskPushConfig, error)
	DeleteTaskPushConfigFn func(context.Context, *a2a.DeleteTaskPushConfigParams) error
	GetAgentCardFn         func(context.Context) (*a2a.AgentCard, error)
}

var _ Transport = (*testTransport)(nil)

func (t *testTransport) GetTask(ctx context.Context, query *a2a.TaskQueryParams) (*a2a.Task, error) {
	return t.GetTaskFn(ctx, query)
}

func (t *testTransport) ListTasks(ctx context.Context, request *a2a.ListTasksRequest) (*a2a.ListTasksResponse, error) {
	return t.ListTasksFn(ctx, request)
}

func (t *testTransport) CancelTask(ctx context.Context, id *a2a.TaskIDParams) (*a2a.Task, error) {
	return t.CancelTaskFn(ctx, id)
}

func (t *testTransport) SendMessage(ctx context.Context, message *a2a.MessageSendParams) (a2a.SendMessageResult, error) {
	return t.SendMessageFn(ctx, message)
}

func (t *testTransport) ResubscribeToTask(ctx context.Context, id *a2a.TaskIDParams) iter.Seq2[a2a.Event, error] {
	return t.ResubscribeToTaskFn(ctx, id)
}

func (t *testTransport) SendStreamingMessage(ctx context.Context, message *a2a.MessageSendParams) iter.Seq2[a2a.Event, error] {
	return t.SendStreamingMessageFn(ctx, message)
}

func (t *testTransport) GetTaskPushConfig(ctx context.Context, params *a2a.GetTaskPushConfigParams) (*a2a.TaskPushConfig, error) {
	return t.GetTaskPushConfigFn(ctx, params)
}

func (t *testTransport) ListTaskPushConfig(ctx context.Context, params *a2a.ListTaskPushConfigParams) ([]*a2a.TaskPushConfig, error) {
	return t.ListTaskPushConfigFn(ctx, params)
}

func (t *testTransport) SetTaskPushConfig(ctx context.Context, params *a2a.TaskPushConfig) (*a2a.TaskPushConfig, error) {
	return t.SetTaskPushConfigFn(ctx, params)
}

func (t *testTransport) DeleteTaskPushConfig(ctx context.Context, params *a2a.DeleteTaskPushConfigParams) error {
	return t.DeleteTaskPushConfigFn(ctx, params)
}

func (t *testTransport) GetAgentCard(ctx context.Context) (*a2a.AgentCard, error) {
	return t.GetAgentCardFn(ctx)
}

func (t *testTransport) Destroy() error {
	return nil
}

func makeEventSeq2(events []a2a.Event) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		for _, event := range events {
			if !yield(event, nil) {
				break
			}
		}
	}
}

type testInterceptor struct {
	lastReq  *Request
	lastResp *Response
	BeforeFn func(context.Context, *Request) (context.Context, any, error)
	AfterFn  func(context.Context, *Response) error
}

var _ CallInterceptor = (*testInterceptor)(nil)

func (ti *testInterceptor) Before(ctx context.Context, req *Request) (context.Context, any, error) {
	ti.lastReq = req
	if ti.BeforeFn != nil {
		return ti.BeforeFn(ctx, req)
	}
	return ctx, nil, nil
}

func (ti *testInterceptor) After(ctx context.Context, resp *Response) error {
	ti.lastResp = resp
	if ti.AfterFn != nil {
		return ti.AfterFn(ctx, resp)
	}
	return nil
}

func newTestClient(transport Transport, interceptors ...CallInterceptor) *Client {
	return &Client{transport: transport, interceptors: interceptors}
}

func TestClient_CallFails(t *testing.T) {
	ctx := t.Context()
	wantErr := errors.New("call failed")

	transport := &testTransport{
		GetTaskFn: func(ctx context.Context, tqp *a2a.TaskQueryParams) (*a2a.Task, error) {
			return nil, wantErr
		},
	}
	client := newTestClient(transport)

	if _, err := client.GetTask(ctx, &a2a.TaskQueryParams{}); !errors.Is(err, wantErr) {
		t.Fatalf("client.GetTask() error = %v, want %v", err, wantErr)
	}
}

func TestClient_InterceptorModifiesRequest(t *testing.T) {
	ctx := t.Context()
	task := &a2a.Task{}
	var receivedMeta map[string]any
	transport := &testTransport{
		GetTaskFn: func(ctx context.Context, tqp *a2a.TaskQueryParams) (*a2a.Task, error) {
			receivedMeta = tqp.Metadata
			return task, nil
		},
	}
	metaKey, metaVal := "answer", 42
	interceptor := &testInterceptor{
		BeforeFn: func(ctx context.Context, r *Request) (context.Context, any, error) {
			typed := r.Payload.(*a2a.TaskQueryParams)
			typed.Metadata = map[string]any{metaKey: metaVal}
			return ctx, nil, nil
		},
	}

	client := newTestClient(transport, interceptor)
	if _, err := client.GetTask(ctx, &a2a.TaskQueryParams{}); err != nil {
		t.Fatalf("client.GetTask() error = %v, want nil", err)
	}
	if receivedMeta[metaKey] != metaVal {
		t.Fatalf("client.GetTask() meta[%s]=%v, want %v", metaKey, receivedMeta[metaKey], metaVal)
	}
}

func TestClient_DefaultSendMessageConfig(t *testing.T) {
	ctx := t.Context()
	task := &a2a.Task{}
	acceptedModes := []string{"text/plain"}
	pushConfig := &a2a.PushConfig{URL: "https://push.com", Token: "secret"}
	transport := &testTransport{
		SendMessageFn: func(ctx context.Context, params *a2a.MessageSendParams) (a2a.SendMessageResult, error) {
			return task, nil
		},
	}
	interceptor := &testInterceptor{}
	client := &Client{
		config:       Config{PushConfig: pushConfig, AcceptedOutputModes: acceptedModes, Polling: true},
		transport:    transport,
		interceptors: []CallInterceptor{interceptor},
	}
	for _, req := range []*a2a.MessageSendParams{{}, {Config: &a2a.MessageSendConfig{}}} {
		wantNilConfigAfter := req.Config == nil

		_, err := client.SendMessage(ctx, req)
		if err != nil {
			t.Fatalf("client.SendMessage() error = %v", err)
		}
		want := &a2a.MessageSendParams{
			Config: &a2a.MessageSendConfig{AcceptedOutputModes: acceptedModes, PushConfig: pushConfig, Blocking: utils.Ptr(false)},
		}
		if diff := cmp.Diff(want, interceptor.lastReq.Payload); diff != "" {
			t.Fatalf("client.SendMessage() wrong result (+got,-want) diff = %s", diff)
		}
		wantReq := &a2a.MessageSendParams{Config: &a2a.MessageSendConfig{}}
		if wantNilConfigAfter {
			wantReq = &a2a.MessageSendParams{}
		}
		if diff := cmp.Diff(wantReq, req); diff != "" {
			t.Fatalf("client.SendMessage() modified params (+got,-want) diff = %s", diff)
		}
	}
}

func TestClient_DefaultSendStreamingMessageConfig(t *testing.T) {
	ctx := t.Context()
	task := &a2a.Task{}
	acceptedModes := []string{"text/plain"}
	pushConfig := &a2a.PushConfig{URL: "https://push.com", Token: "secret"}
	transport := &testTransport{
		SendStreamingMessageFn: func(ctx context.Context, params *a2a.MessageSendParams) iter.Seq2[a2a.Event, error] {
			return func(yield func(a2a.Event, error) bool) { yield(task, nil) }
		},
	}
	interceptor := &testInterceptor{}
	client := &Client{
		config:       Config{PushConfig: pushConfig, AcceptedOutputModes: acceptedModes, Polling: true},
		transport:    transport,
		interceptors: []CallInterceptor{interceptor},
	}
	req := &a2a.MessageSendParams{}
	for _, err := range client.SendStreamingMessage(ctx, req) {
		if err != nil {
			t.Fatalf("client.SendStreamingMessage() error = %v", err)
		}
	}
	want := &a2a.MessageSendParams{
		Config: &a2a.MessageSendConfig{AcceptedOutputModes: acceptedModes, PushConfig: pushConfig, Blocking: utils.Ptr(true)},
	}
	if diff := cmp.Diff(want, interceptor.lastReq.Payload); diff != "" {
		t.Fatalf("client.SendStreamingMessage() wrong result (+got,-want) diff = %s", diff)
	}
	if diff := cmp.Diff(&a2a.MessageSendParams{}, req); diff != "" {
		t.Fatalf("client.SendStreamingMessage() modified params (+got,-want) diff = %s", diff)
	}
}

func TestClient_InterceptorsAttachCallMeta(t *testing.T) {
	ctx := t.Context()

	var receivedCallMeta CallMeta
	transport := &testTransport{
		GetTaskFn: func(ctx context.Context, tqp *a2a.TaskQueryParams) (*a2a.Task, error) {
			receivedCallMeta, _ = CallMetaFrom(ctx)
			return &a2a.Task{}, nil
		},
	}

	k1, v1, k2, v2 := "Authorization", "Basic ABCD", "X-Custom", "test"
	interceptor1 := &testInterceptor{
		BeforeFn: func(ctx context.Context, r *Request) (context.Context, any, error) {
			r.Meta[k1] = []string{v1}
			return ctx, nil, nil
		},
	}
	interceptor2 := &testInterceptor{
		BeforeFn: func(ctx context.Context, r *Request) (context.Context, any, error) {
			r.Meta[k2] = []string{v2}
			return ctx, nil, nil
		},
	}

	client := newTestClient(transport, interceptor1, interceptor2)
	if _, err := client.GetTask(ctx, &a2a.TaskQueryParams{}); err != nil {
		t.Fatalf("client.GetTask() error = %v, want nil", err)
	}
	wantCallMeta := CallMeta{k1: []string{v1}, k2: []string{v2}}
	if !reflect.DeepEqual(receivedCallMeta, wantCallMeta) {
		t.Fatalf("client.GetTask() meta = %v, want %v", receivedCallMeta, wantCallMeta)
	}
}

func TestClient_InterceptorModifiesResponse(t *testing.T) {
	ctx := t.Context()
	task := &a2a.Task{}
	transport := &testTransport{
		GetTaskFn: func(ctx context.Context, tqp *a2a.TaskQueryParams) (*a2a.Task, error) {
			return task, nil
		},
	}
	metaKey, metaVal := "answer", 42
	interceptor := &testInterceptor{
		AfterFn: func(ctx context.Context, r *Response) error {
			typed := r.Payload.(*a2a.Task)
			typed.Metadata = map[string]any{metaKey: metaVal}
			return nil
		},
	}

	client := newTestClient(transport, interceptor)
	task, err := client.GetTask(ctx, &a2a.TaskQueryParams{})
	if err != nil {
		t.Fatalf("client.GetTask() error = %v, want nil", err)
	}
	if task.Metadata[metaKey] != metaVal {
		t.Fatalf("client.GetTask() meta[%s]=%v, want %v", metaKey, task.Metadata[metaKey], metaVal)
	}
}

func TestClient_InterceptorRejectsRequest(t *testing.T) {
	ctx := t.Context()
	called := false
	transport := &testTransport{
		GetTaskFn: func(ctx context.Context, tqp *a2a.TaskQueryParams) (*a2a.Task, error) {
			called = true
			return &a2a.Task{}, nil
		},
	}
	wantErr := errors.New("failed")
	interceptor := &testInterceptor{
		BeforeFn: func(ctx context.Context, r *Request) (context.Context, any, error) {
			return ctx, nil, wantErr
		},
	}

	client := newTestClient(transport, interceptor)
	if task, err := client.GetTask(ctx, &a2a.TaskQueryParams{}); !errors.Is(err, wantErr) {
		t.Fatalf("client.GetTask() = (%v, %v), want error %v", task, err, wantErr)
	}
	if called {
		t.Fatal("expected transport to not be called")
	}
}

func TestClient_InterceptorRejectsResponse(t *testing.T) {
	ctx := t.Context()
	called := false
	transport := &testTransport{
		GetTaskFn: func(ctx context.Context, tqp *a2a.TaskQueryParams) (*a2a.Task, error) {
			called = true
			return &a2a.Task{}, nil
		},
	}
	wantErr := errors.New("failed")
	interceptor := &testInterceptor{
		AfterFn: func(ctx context.Context, r *Response) error {
			return wantErr
		},
	}

	client := newTestClient(transport, interceptor)
	if task, err := client.GetTask(ctx, &a2a.TaskQueryParams{}); !errors.Is(err, wantErr) {
		t.Fatalf("client.GetTask() = (%v, %v), want error %v", task, err, wantErr)
	}
	if !called {
		t.Fatal("expected transport to be called")
	}
}

func TestClient_InterceptorMethodsDataSharing(t *testing.T) {
	ctx := t.Context()
	transport := &testTransport{
		GetTaskFn: func(ctx context.Context, tqp *a2a.TaskQueryParams) (*a2a.Task, error) {
			return &a2a.Task{}, nil
		},
	}
	type ctxKey struct{}
	val := 42
	var receivedVal int
	interceptor := &testInterceptor{
		BeforeFn: func(ctx context.Context, r *Request) (context.Context, any, error) {
			return context.WithValue(ctx, ctxKey{}, val), nil, nil
		},
		AfterFn: func(ctx context.Context, r *Response) error {
			receivedVal = ctx.Value(ctxKey{}).(int)
			return nil
		},
	}

	client := newTestClient(transport, interceptor)
	if _, err := client.GetTask(ctx, &a2a.TaskQueryParams{}); err != nil {
		t.Fatalf("client.GetTask() error = %v, want nil", err)
	}

	if receivedVal != val {
		t.Fatal("expected transport to not be called")
	}
}

func TestClient_GetExtendedAgentCard(t *testing.T) {
	ctx := t.Context()
	want := &a2a.AgentCard{Name: "test", SupportsAuthenticatedExtendedCard: true}
	extendedCard := &a2a.AgentCard{Name: "test", Description: "secret"}
	transport := &testTransport{
		GetAgentCardFn: func(ctx context.Context) (*a2a.AgentCard, error) {
			return extendedCard, nil
		},
	}
	interceptor := &testInterceptor{}
	client := &Client{transport: transport, interceptors: []CallInterceptor{interceptor}}
	client.card.Store(want)
	got, err := client.GetAgentCard(ctx)
	if err != nil {
		t.Fatalf("client.GetAgentCard() error = %v, want nil", err)
	}
	if interceptor.lastReq == nil {
		t.Fatal("lastReq = nil, want GertAgentCard")
	}
	if diff := cmp.Diff(extendedCard, got); diff != "" {
		t.Fatalf("client.SendStreamingMessage() modified params (+got,-want) diff = %s", diff)
	}
}

func TestClient_PassAgentCardToInterceptorsAfterResolved(t *testing.T) {
	ctx := t.Context()
	extendedCard := &a2a.AgentCard{Name: "test", Description: "secret", SupportsAuthenticatedExtendedCard: true}
	transport := &testTransport{
		GetAgentCardFn: func(ctx context.Context) (*a2a.AgentCard, error) {
			return extendedCard, nil
		},
	}
	interceptor := &testInterceptor{}
	client := &Client{transport: transport, interceptors: []CallInterceptor{interceptor}}
	_, err := client.GetAgentCard(ctx)
	if err != nil {
		t.Fatalf("client.GetAgentCard() error = %v, want nil", err)
	}
	if interceptor.lastReq.Card != nil {
		t.Fatalf("lastReq.Card = %v, want nil", interceptor.lastReq.Card)
	}
	if interceptor.lastResp.Card != nil {
		t.Fatalf("lastResp.Card = %v, want nil", interceptor.lastResp.Card)
	}

	_, err = client.GetAgentCard(ctx)
	if err != nil {
		t.Fatalf("client.GetAgentCard() error = %v, want nil", err)
	}
	if diff := cmp.Diff(extendedCard, interceptor.lastReq.Card); diff != "" {
		t.Fatalf("wrong interceptor.lastReq.Card (+got,-want) diff = %s", diff)
	}
	if diff := cmp.Diff(extendedCard, interceptor.lastResp.Card); diff != "" {
		t.Fatalf("wrong interceptor.lastResp.Card (+got,-want) diff = %s", diff)
	}
}

func TestClient_GetAgentCardCallSkippedIfNoExtendedCard(t *testing.T) {
	ctx := t.Context()
	want := &a2a.AgentCard{Name: "test", SupportsAuthenticatedExtendedCard: false}
	interceptor := &testInterceptor{}
	client := &Client{interceptors: []CallInterceptor{interceptor}}
	client.card.Store(want)
	got, err := client.GetAgentCard(ctx)
	if err != nil {
		t.Fatalf("client.GetAgentCard() error = %v, want nil", err)
	}
	if interceptor.lastReq != nil {
		t.Fatalf("lastReq = %v, want nil", interceptor.lastReq)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("client.SendStreamingMessage() modified params (+got,-want) diff = %s", diff)
	}
}

func TestClient_FallbackToNonStreamingSend(t *testing.T) {
	ctx := t.Context()
	want := a2a.NewMessage(a2a.MessageRoleAgent)
	transport := &testTransport{
		SendMessageFn: func(ctx context.Context, params *a2a.MessageSendParams) (a2a.SendMessageResult, error) {
			return want, nil
		},
	}
	interceptor := &testInterceptor{}
	client := &Client{transport: transport, interceptors: []CallInterceptor{interceptor}}
	client.card.Store(&a2a.AgentCard{Capabilities: a2a.AgentCapabilities{Streaming: false}})

	eventCount := 0
	for got, err := range client.SendStreamingMessage(ctx, &a2a.MessageSendParams{}) {
		if err != nil {
			t.Fatalf("client.GetAgentCard() error = %v, want nil", err)
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Fatalf("client.SendStreamingMessage() wrong result (+got,-want) diff = %s", diff)
		}
		eventCount++
	}
	if eventCount != 1 {
		t.Fatalf("client.SendStreamingMessage() got %d events, want 1", eventCount)
	}
}

func TestClient_InterceptGetTask(t *testing.T) {
	ctx := t.Context()
	task := &a2a.Task{}
	transport := &testTransport{
		GetTaskFn: func(ctx context.Context, tqp *a2a.TaskQueryParams) (*a2a.Task, error) {
			return task, nil
		},
	}
	interceptor := &testInterceptor{}
	client := newTestClient(transport, interceptor)
	req := &a2a.TaskQueryParams{}
	resp, err := client.GetTask(ctx, req)
	if interceptor.lastReq.Method != "GetTask" {
		t.Fatalf("lastReq.Method = %v, want GetTask", interceptor.lastReq.Method)
	}
	if interceptor.lastResp.Method != "GetTask" {
		t.Fatalf("lastResp.Method = %v, want GetTask", interceptor.lastResp.Method)
	}
	if err != nil || resp != task {
		t.Fatalf("client.GetTask() = (%v, %v), want %v", resp, err, task)
	}
	if interceptor.lastReq.Payload != req {
		t.Fatalf("interceptor.Before() payload = %v, want %v", interceptor.lastReq.Payload, req)
	}
	if interceptor.lastResp.Payload != task {
		t.Fatalf("interceptor.After() payload = %v, want %v", interceptor.lastResp.Payload, task)
	}
}

func TestClient_InterceptListTasks(t *testing.T) {
	ctx := t.Context()
	task := &a2a.Task{}
	response := &a2a.ListTasksResponse{Tasks: []*a2a.Task{task}}
	transport := &testTransport{
		ListTasksFn: func(ctx context.Context, tqp *a2a.ListTasksRequest) (*a2a.ListTasksResponse, error) {
			return response, nil
		},
	}
	interceptor := &testInterceptor{}
	client := newTestClient(transport, interceptor)
	req := &a2a.ListTasksRequest{}
	resp, err := client.ListTasks(ctx, req)
	if interceptor.lastReq.Method != "ListTasks" {
		t.Fatalf("lastReq.Method = %v, want ListTasks", interceptor.lastReq.Method)
	}
	if interceptor.lastResp.Method != "ListTasks" {
		t.Fatalf("lastResp.Method = %v, want ListTasks", interceptor.lastResp.Method)
	}
	if err != nil || resp.Tasks[0] != task {
		t.Fatalf("client.ListTasks() = (%v, %v), want %v", resp, err, task)
	}
	if interceptor.lastReq.Payload != req {
		t.Fatalf("interceptor.Before() payload = %v, want %v", interceptor.lastReq.Payload, req)
	}
	if interceptor.lastResp.Payload != response {
		t.Fatalf("interceptor.After() payload = %v, want %v", interceptor.lastResp.Payload, response)
	}
}

func TestClient_InterceptCancelTask(t *testing.T) {
	ctx := t.Context()
	task := &a2a.Task{}
	transport := &testTransport{
		CancelTaskFn: func(ctx context.Context, id *a2a.TaskIDParams) (*a2a.Task, error) {
			return task, nil
		},
	}
	interceptor := &testInterceptor{}
	client := newTestClient(transport, interceptor)
	req := &a2a.TaskIDParams{}
	resp, err := client.CancelTask(ctx, req)
	if interceptor.lastReq.Method != "CancelTask" {
		t.Fatalf("lastReq.Method = %v, want CancelTask", interceptor.lastReq.Method)
	}
	if interceptor.lastResp.Method != "CancelTask" {
		t.Fatalf("lastResp.Method = %v, want CancelTask", interceptor.lastResp.Method)
	}
	if err != nil || resp != task {
		t.Fatalf("client.CancelTask() = (%v, %v), want %v", resp, err, task)
	}
	if interceptor.lastReq.Payload != req {
		t.Fatalf("interceptor.Before() payload = %v, want %v", interceptor.lastReq.Payload, req)
	}
	if interceptor.lastResp.Payload != task {
		t.Fatalf("interceptor.After() payload = %v, want %v", interceptor.lastResp.Payload, task)
	}
}

func TestClient_InterceptSendMessage(t *testing.T) {
	ctx := t.Context()
	task := &a2a.Task{}
	transport := &testTransport{
		SendMessageFn: func(ctx context.Context, params *a2a.MessageSendParams) (a2a.SendMessageResult, error) {
			return task, nil
		},
	}
	interceptor := &testInterceptor{}
	client := newTestClient(transport, interceptor)
	req := &a2a.MessageSendParams{}
	resp, err := client.SendMessage(ctx, req)
	if interceptor.lastReq.Method != "SendMessage" {
		t.Fatalf("lastReq.Method = %v, want SendMessage", interceptor.lastReq.Method)
	}
	if interceptor.lastResp.Method != "SendMessage" {
		t.Fatalf("lastResp.Method = %v, want SendMessage", interceptor.lastResp.Method)
	}
	if err != nil || resp != task {
		t.Fatalf("client.SendMessage() = (%v, %v), want %v", resp, err, task)
	}
	if diff := cmp.Diff(req, interceptor.lastReq.Payload); diff != "" {
		t.Fatalf("wrong interceptor.lastReq.Payload (+got,-want) diff = %s", diff)
	}
	if diff := cmp.Diff(task, interceptor.lastResp.Payload); diff != "" {
		t.Fatalf("wrong interceptor.lastResp.Payload (+got,-want) diff = %s", diff)
	}
}

func TestClient_InterceptResubscribeToTask(t *testing.T) {
	ctx := t.Context()
	events := []a2a.Event{
		&a2a.TaskStatusUpdateEvent{Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}},
		&a2a.TaskStatusUpdateEvent{Status: a2a.TaskStatus{State: a2a.TaskStateWorking}},
		&a2a.TaskStatusUpdateEvent{Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}},
	}
	transport := &testTransport{
		ResubscribeToTaskFn: func(ctx context.Context, ti *a2a.TaskIDParams) iter.Seq2[a2a.Event, error] {
			return makeEventSeq2(events)
		},
	}
	interceptor := &testInterceptor{}
	client := newTestClient(transport, interceptor)
	req := &a2a.TaskIDParams{}
	eventI := 0
	for resp, err := range client.ResubscribeToTask(ctx, req) {
		if err != nil || resp != events[eventI] {
			t.Fatalf("client.ResubscribeToTask()[%d] = (%v, %v), want %v", eventI, resp, err, events[eventI])
		}
		if interceptor.lastResp.Payload != events[eventI] {
			t.Fatalf("interceptor.After %d-th payload = %v, want %v", eventI, interceptor.lastResp.Payload, events[eventI])
		}
		eventI += 1
	}
	if interceptor.lastReq.Method != "ResubscribeToTask" {
		t.Fatalf("lastReq.Method = %v, want ResubscribeToTask", interceptor.lastReq.Method)
	}
	if interceptor.lastResp.Method != "ResubscribeToTask" {
		t.Fatalf("lastResp.Method = %v, want ResubscribeToTask", interceptor.lastResp.Method)
	}
	if interceptor.lastReq.Payload != req {
		t.Fatalf("interceptor.Before payload = %v, want %v", interceptor.lastReq.Payload, req)
	}
}

func TestClient_InterceptSendStreamingMessage(t *testing.T) {
	ctx := t.Context()
	events := []a2a.Event{
		&a2a.TaskStatusUpdateEvent{Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}},
		&a2a.TaskStatusUpdateEvent{Status: a2a.TaskStatus{State: a2a.TaskStateWorking}},
		&a2a.TaskStatusUpdateEvent{Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}},
	}
	transport := &testTransport{
		SendStreamingMessageFn: func(ctx context.Context, ti *a2a.MessageSendParams) iter.Seq2[a2a.Event, error] {
			return makeEventSeq2(events)
		},
	}
	interceptor := &testInterceptor{}
	client := newTestClient(transport, interceptor)
	req := &a2a.MessageSendParams{}
	eventI := 0
	for resp, err := range client.SendStreamingMessage(ctx, req) {
		if err != nil || resp != events[eventI] {
			t.Fatalf("client.SendStreamingMessage()[%d] = (%v, %v), want %v", eventI, resp, err, events[eventI])
		}
		if interceptor.lastResp.Payload != events[eventI] {
			t.Fatalf("interceptor.After %d-th payload = %v, want %v", eventI, interceptor.lastResp.Payload, events[eventI])
		}
		eventI += 1
	}
	if eventI != len(events) {
		t.Fatalf("client.SendStreamingMessage() event count = %d, want %d", eventI, len(events))
	}
	if interceptor.lastReq.Method != "SendStreamingMessage" {
		t.Fatalf("lastReq.Method = %v, want SendStreamingMessage", interceptor.lastReq.Method)
	}
	if interceptor.lastResp.Method != "SendStreamingMessage" {
		t.Fatalf("lastResp.Method = %v, want SendStreamingMessage", interceptor.lastResp.Method)
	}
	if diff := cmp.Diff(req, interceptor.lastReq.Payload); diff != "" {
		t.Fatalf("wrong interceptor.lastReq.Payload (+got,-want) diff = %s", diff)
	}
}

func TestClient_InterceptGetTaskPushConfig(t *testing.T) {
	ctx := t.Context()
	config := &a2a.TaskPushConfig{}
	transport := &testTransport{
		GetTaskPushConfigFn: func(ctx context.Context, params *a2a.GetTaskPushConfigParams) (*a2a.TaskPushConfig, error) {
			return config, nil
		},
	}
	interceptor := &testInterceptor{}
	client := newTestClient(transport, interceptor)
	req := &a2a.GetTaskPushConfigParams{}
	resp, err := client.GetTaskPushConfig(ctx, req)
	if interceptor.lastReq.Method != "GetTaskPushConfig" {
		t.Fatalf("lastReq.Method = %v, want GetTaskPushConfig", interceptor.lastReq.Method)
	}
	if interceptor.lastResp.Method != "GetTaskPushConfig" {
		t.Fatalf("lastResp.Method = %v, want GetTaskPushConfig", interceptor.lastResp.Method)
	}
	if err != nil || resp != config {
		t.Fatalf("client.GetTaskPushConfig() = (%v, %v), want %v", resp, err, config)
	}
	if interceptor.lastReq.Payload != req {
		t.Fatalf("interceptor.Before payload = %v, want %v", interceptor.lastReq.Payload, req)
	}
	if interceptor.lastResp.Payload != config {
		t.Fatalf("interceptor.After payload = %v, want %v", interceptor.lastResp.Payload, config)
	}
}

func TestClient_InterceptListTaskPushConfig(t *testing.T) {
	ctx := t.Context()
	config := &a2a.TaskPushConfig{}
	transport := &testTransport{
		ListTaskPushConfigFn: func(ctx context.Context, params *a2a.ListTaskPushConfigParams) ([]*a2a.TaskPushConfig, error) {
			return []*a2a.TaskPushConfig{config}, nil
		},
	}
	interceptor := &testInterceptor{}
	client := newTestClient(transport, interceptor)
	req := &a2a.ListTaskPushConfigParams{}
	resp, err := client.ListTaskPushConfig(ctx, req)
	if interceptor.lastReq.Method != "ListTaskPushConfig" {
		t.Fatalf("lastReq.Method = %v, want ListTaskPushConfig", interceptor.lastReq.Method)
	}
	if interceptor.lastResp.Method != "ListTaskPushConfig" {
		t.Fatalf("lastResp.Method = %v, want ListTaskPushConfig", interceptor.lastResp.Method)
	}
	if err != nil || len(resp) != 1 || resp[0] != config {
		t.Fatalf("client.ListTaskPushConfig() = (%v, %v), want %v", resp, err, config)
	}
	if interceptor.lastReq.Payload != req {
		t.Fatalf("interceptor.Before payload = %v, want %v", interceptor.lastReq.Payload, req)
	}
	if interceptor.lastResp.Payload.([]*a2a.TaskPushConfig)[0] != config {
		t.Fatalf("interceptor.After payload = %v, want %v", interceptor.lastResp.Payload, config)
	}
}

func TestClient_InterceptSetTaskPushConfig(t *testing.T) {
	ctx := t.Context()
	config := &a2a.TaskPushConfig{}
	transport := &testTransport{
		SetTaskPushConfigFn: func(ctx context.Context, params *a2a.TaskPushConfig) (*a2a.TaskPushConfig, error) {
			return config, nil
		},
	}
	interceptor := &testInterceptor{}
	client := newTestClient(transport, interceptor)
	req := &a2a.TaskPushConfig{}
	resp, err := client.SetTaskPushConfig(ctx, req)
	if interceptor.lastReq.Method != "SetTaskPushConfig" {
		t.Fatalf("lastReq.Method = %v, want SetTaskPushConfig", interceptor.lastReq.Method)
	}
	if interceptor.lastResp.Method != "SetTaskPushConfig" {
		t.Fatalf("lastResp.Method = %v, want SetTaskPushConfig", interceptor.lastResp.Method)
	}
	if err != nil || resp != config {
		t.Fatalf("client.SetTaskPushConfig() = (%v, %v), want %v", resp, err, config)
	}
	if interceptor.lastReq.Payload != req {
		t.Fatalf("interceptor.Before payload = %v, want %v", interceptor.lastReq.Payload, req)
	}
	if interceptor.lastResp.Payload != config {
		t.Fatalf("interceptor.After payload = %v, want %v", interceptor.lastResp.Payload, config)
	}
}

func TestClient_InterceptDeleteTaskPushConfig(t *testing.T) {
	ctx := t.Context()
	transport := &testTransport{
		DeleteTaskPushConfigFn: func(ctx context.Context, dtpcp *a2a.DeleteTaskPushConfigParams) error {
			return nil
		},
	}
	interceptor := &testInterceptor{}
	client := newTestClient(transport, interceptor)
	req := &a2a.DeleteTaskPushConfigParams{}
	err := client.DeleteTaskPushConfig(ctx, req)
	if interceptor.lastReq.Method != "DeleteTaskPushConfig" {
		t.Fatalf("lastReq.Method = %v, want DeleteTaskPushConfig", interceptor.lastReq.Method)
	}
	if interceptor.lastResp.Method != "DeleteTaskPushConfig" {
		t.Fatalf("lastResp.Method = %v, want DeleteTaskPushConfig", interceptor.lastResp.Method)
	}
	if err != nil {
		t.Fatalf("client.DeleteTaskPushConfig() error = %v, want nil", err)
	}
	if interceptor.lastReq.Payload != req {
		t.Fatalf("interceptor.Before payload = %v, want %v", interceptor.lastReq.Payload, req)
	}
	if _, ok := interceptor.lastResp.Payload.(struct{}); !ok {
		t.Fatalf("interceptor.After payload = %v, want struct{}", interceptor.lastResp.Payload)
	}
}

func TestClient_InterceptGetAgentCard(t *testing.T) {
	ctx := t.Context()
	card := &a2a.AgentCard{}
	transport := &testTransport{
		GetAgentCardFn: func(ctx context.Context) (*a2a.AgentCard, error) {
			return card, nil
		},
	}
	interceptor := &testInterceptor{}
	client := newTestClient(transport, interceptor)
	client.baseURL = "https://base.com"
	resp, err := client.GetAgentCard(ctx)
	if interceptor.lastReq.Method != "GetAgentCard" {
		t.Fatalf("lastReq.Method = %v, want GetAgentCard", interceptor.lastReq.Method)
	}
	if interceptor.lastReq.BaseURL != client.baseURL {
		t.Fatalf("lastReq.BaseURL = %q, want %q", interceptor.lastReq.BaseURL, client.baseURL)
	}
	if interceptor.lastResp.Method != "GetAgentCard" {
		t.Fatalf("lastResp.Method = %v, want GetAgentCard", interceptor.lastResp.Method)
	}
	if interceptor.lastResp.BaseURL != client.baseURL {
		t.Fatalf("lastResp.BaseURL = %q, want %q", interceptor.lastResp.BaseURL, client.baseURL)
	}
	if err != nil {
		t.Fatalf("client.GetAgentCard() error = %v, want nil", err)
	}
	if payload, ok := interceptor.lastReq.Payload.(*struct{}); !ok || payload != nil {
		t.Fatalf("interceptor.Before payload = %v, want *struct{}", interceptor.lastReq.Payload)
	}
	if interceptor.lastResp.Payload != resp {
		t.Fatalf("interceptor.After payload = %v, want %v", interceptor.lastResp.Payload, resp)
	}
}

func TestClient_Intercept_RequestModification(t *testing.T) {
	ctx := t.Context()
	originalParams := &a2a.MessageSendParams{
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: "Hello!"}),
	}
	modifiedParams := &a2a.MessageSendParams{
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: "Modified"}),
	}
	var receivedParams *a2a.MessageSendParams
	transport := &testTransport{
		SendMessageFn: func(ctx context.Context, params *a2a.MessageSendParams) (a2a.SendMessageResult, error) {
			receivedParams = params
			message := params.Message.Parts[0].(a2a.TextPart).Text
			return a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: message}), nil
		},
	}

	interceptor := &testInterceptor{
		BeforeFn: func(ctx context.Context, req *Request) (context.Context, any, error) {
			req.Payload = modifiedParams
			return ctx, nil, nil
		},
	}

	client := newTestClient(transport, interceptor)
	resp, err := client.SendMessage(ctx, originalParams)
	if err != nil {
		t.Fatalf("client.SendMessage() error = %v, want nil", err)
	}
	if receivedParams == originalParams {
		t.Fatalf("receivedParams = %v, want %v", receivedParams, modifiedParams)
	}
	reqMsg := resp.(*a2a.Message).Parts[0].(a2a.TextPart).Text
	if reqMsg != "Modified" {
		t.Fatalf("reqMsg = %q, want %q", reqMsg, "Modified")
	}

}

func TestClient_Intercept_ResponseAndErrorModification(t *testing.T) {
	injectedErr := errors.New("injected error")
	transportErr := errors.New("transport error")

	tests := []struct {
		name          string
		transportResp a2a.SendMessageResult
		transportErr  error
		interceptorFn func(ctx context.Context, resp *Response) error
		wantErr       error
		wantRespText  string
	}{
		{
			name:          "response modification",
			transportResp: a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: "Original"}),
			interceptorFn: func(ctx context.Context, resp *Response) error {
				resp.Payload = a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: "Modified"})
				return nil
			},
			wantRespText: "Modified",
		},
		{
			name:          "injected error: transport success, interceptor error",
			transportResp: a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: "Success"}),
			interceptorFn: func(ctx context.Context, resp *Response) error {
				resp.Err = injectedErr
				return nil
			},
			wantErr: injectedErr,
		},
		{
			name:         "injected error: transport error, interceptor success",
			transportErr: transportErr,
			interceptorFn: func(ctx context.Context, resp *Response) error {
				if resp.Err != nil {
					resp.Err = nil
					resp.Payload = a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: "Recovered from error"})
				}
				return nil
			},
			wantRespText: "Recovered from error",
		},
		{
			name:         "injected error: transport error, interceptor error",
			transportErr: transportErr,
			interceptorFn: func(ctx context.Context, resp *Response) error {
				if resp.Err != nil {
					resp.Err = injectedErr
				}
				return nil
			},
			wantErr: injectedErr,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			transport := &testTransport{
				SendMessageFn: func(ctx context.Context, params *a2a.MessageSendParams) (a2a.SendMessageResult, error) {
					return tt.transportResp, tt.transportErr
				},
			}
			interceptor := &testInterceptor{
				AfterFn: tt.interceptorFn,
			}
			client := newTestClient(transport, interceptor)
			resp, err := client.SendMessage(ctx, &a2a.MessageSendParams{})

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("client.SendMessage() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil {
				if resp == nil {
					t.Fatalf("client.SendMessage() resp = %v, want %v", resp, tt.wantRespText)
				}
				if msg, ok := resp.(*a2a.Message); ok {
					if msg.Parts[0].(a2a.TextPart).Text != tt.wantRespText {
						t.Fatalf("client.SendMessage() resp = %v, want %v", resp, tt.wantRespText)
					}
				}
			}
		})
	}
}

func TestClient_intercept_EarlyReturn(t *testing.T) {
	ctx := t.Context()
	originalQuery := &a2a.TaskQueryParams{ID: "original"}
	earlyResult := &a2a.Task{ID: "early-cached-result"}

	transport := &testTransport{}
	interceptor1, interceptor2, interceptor3 := &testInterceptor{}, &testInterceptor{}, &testInterceptor{}

	transportMethodCalled := false
	transport.GetTaskFn = func(ctx context.Context, params *a2a.TaskQueryParams) (*a2a.Task, error) {
		transportMethodCalled = true
		return nil, errors.New("transport method should not be called")
	}

	var callOrder []string
	interceptor1.BeforeFn = func(ctx context.Context, req *Request) (context.Context, any, error) {
		callOrder = append(callOrder, "1-Before")
		return ctx, nil, nil
	}
	interceptor1.AfterFn = func(ctx context.Context, resp *Response) error {
		callOrder = append(callOrder, "1-After")
		return nil
	}
	//interceptor2 returns early result
	interceptor2.BeforeFn = func(ctx context.Context, req *Request) (context.Context, any, error) {
		callOrder = append(callOrder, "2-Before")
		return ctx, earlyResult, nil
	}
	interceptor2.AfterFn = func(ctx context.Context, resp *Response) error {
		callOrder = append(callOrder, "2-After")
		return nil
	}
	//interceptor3 should not be called
	interceptor3.BeforeFn = func(ctx context.Context, req *Request) (context.Context, any, error) {
		callOrder = append(callOrder, "3-Before")
		return ctx, nil, nil
	}

	client := &Client{
		interceptors: []CallInterceptor{interceptor1, interceptor2, interceptor3},
		transport:    transport,
	}

	task, err := client.GetTask(ctx, originalQuery)
	if err != nil {
		t.Fatalf("client.GetTask() error = %v, want nil", err)
	}
	if task != earlyResult {
		t.Fatalf("client.GetTask() task = %v, want %v", task, earlyResult)
	}
	if transportMethodCalled {
		t.Fatalf("transport method should not be called")
	}
	wantCallOrder := []string{"1-Before", "2-Before", "2-After", "1-After"}
	if !reflect.DeepEqual(callOrder, wantCallOrder) {
		t.Fatalf("callOrder = %v, want %v", callOrder, wantCallOrder)
	}
}
