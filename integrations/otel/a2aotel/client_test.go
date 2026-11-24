// Copyright 2026 The A2A Authors
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

package a2aotel

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"net/url"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
)

type callResult struct {
	Payload any
	Err     error
}

func newClientHandler(results ...callResult) a2aclient.CallHandler {
	return func(context.Context, a2aclient.Request) iter.Seq2[any, error] {
		return func(yield func(any, error) bool) {
			for _, r := range results {
				if !yield(r.Payload, r.Err) {
					return
				}
			}
		}
	}
}

func collect(seq iter.Seq2[any, error]) []callResult {
	var results []callResult
	for payload, err := range seq {
		results = append(results, callResult{Payload: payload, Err: err})
	}
	return results
}

func TestClientPlugin_Spans(t *testing.T) {
	t.Parallel()
	agentURL := "https://agent.example.com/a2a"
	card := &a2a.AgentCard{
		Name:        "Weather",
		Description: "Forecasts the weather",
		Version:     "2.0.0",
		SupportedInterfaces: []*a2a.AgentInterface{
			{URL: agentURL, ProtocolBinding: a2a.TransportProtocolJSONRPC, Tenant: "billing", ProtocolVersion: a2a.Version},
		},
	}
	agentAttrs := map[string]any{
		"gen_ai.agent.name":        "Weather",
		"gen_ai.agent.description": "Forecasts the weather",
		"gen_ai.agent.version":     "2.0.0",
	}
	testCases := []struct {
		name    string
		ctx     func(context.Context) context.Context
		req     a2aclient.Request
		results []callResult
		want    spanSummary
	}{
		{
			name: "SendMessage returning a task",
			req: a2aclient.Request{
				Method:          "SendMessage",
				BaseURL:         agentURL,
				ProtocolBinding: a2a.TransportProtocolJSONRPC,
				ServiceParams:   a2aclient.ServiceParams{"a2a-version": {"1.0"}},
				Card:            card,
				Payload: &a2a.SendMessageRequest{
					Tenant:  "billing",
					Config:  &a2a.SendMessageConfig{ReturnImmediately: true},
					Message: &a2a.Message{ID: "msg-1", ReferenceTasks: []a2a.TaskID{"task-0"}},
				},
			},
			results: []callResult{{Payload: &a2a.Task{ID: "task-1", ContextID: "ctx-1", Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}}}},
			want: spanSummary{
				Name: "SendMessage",
				Kind: trace.SpanKindClient,
				Attrs: withAttrs(agentAttrs, map[string]any{
					"a2a.method.name":                  "SendMessage",
					"a2a.protocol.version":             "1.0",
					"a2a.tenant":                       "billing",
					"a2a.message.id":                   "msg-1",
					"a2a.message.reference_task_ids":   []string{"task-0"},
					"a2a.operation.return_immediately": true,
					"a2a.task.id":                      "task-1",
					"a2a.task.state":                   "TASK_STATE_SUBMITTED",
					"gen_ai.conversation.id":           "ctx-1",
					"server.address":                   "agent.example.com",
					"server.port":                      int64(443),
				}),
			},
		},
		{
			name: "SendStreamingMessage with several task states",
			req: a2aclient.Request{
				Method:  "SendStreamingMessage",
				BaseURL: "http://localhost:8080",
				Payload: &a2a.SendMessageRequest{
					Tenant:  "acme",
					Config:  &a2a.SendMessageConfig{ReturnImmediately: true},
					Message: &a2a.Message{ID: "msg-1"},
				},
			},
			results: []callResult{
				{Payload: &a2a.Task{ID: "task-1", ContextID: "ctx-1", Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}}},
				{Payload: &a2a.TaskStatusUpdateEvent{TaskID: "task-1", ContextID: "ctx-1", Status: a2a.TaskStatus{State: a2a.TaskStateWorking}}},
				{Payload: &a2a.TaskArtifactUpdateEvent{TaskID: "task-1", ContextID: "ctx-1"}},
				{Payload: &a2a.TaskStatusUpdateEvent{TaskID: "task-1", ContextID: "ctx-1", Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}}},
			},
			want: spanSummary{
				Name: "SendStreamingMessage",
				Kind: trace.SpanKindClient,
				Attrs: map[string]any{
					"a2a.method.name":        "SendStreamingMessage",
					"a2a.tenant":             "acme",
					"a2a.message.id":         "msg-1",
					"a2a.task.id":            "task-1",
					"gen_ai.conversation.id": "ctx-1",
					"server.address":         "localhost",
					"server.port":            int64(8080),
				},
			},
		},
		{
			name: "ListTasks returning several tasks",
			req: a2aclient.Request{
				Method:  "ListTasks",
				BaseURL: "localhost:50051",
				Payload: &a2a.ListTasksRequest{},
			},
			results: []callResult{{Payload: &a2a.ListTasksResponse{Tasks: []*a2a.Task{
				{ID: "task-1", ContextID: "ctx-1", Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}},
				{ID: "task-2", ContextID: "ctx-1", Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}},
			}}}},
			want: spanSummary{
				Name: "ListTasks",
				Kind: trace.SpanKindClient,
				Attrs: map[string]any{
					"a2a.method.name":        "ListTasks",
					"a2a.task.state":         "TASK_STATE_COMPLETED",
					"gen_ai.conversation.id": "ctx-1",
					"server.address":         "localhost",
					"server.port":            int64(50051),
				},
			},
		},
		{
			name: "push config method",
			req: a2aclient.Request{
				Method:  "ListTaskPushConfigs",
				Payload: &a2a.ListTaskPushConfigRequest{TaskID: "task-1"},
			},
			results: []callResult{{Payload: []*a2a.PushConfig{{TaskID: "task-1", ID: "a"}, {TaskID: "task-1", ID: "b"}}}},
			want: spanSummary{
				Name: "ListTaskPushNotificationConfigs",
				Kind: trace.SpanKindClient,
				Attrs: map[string]any{
					"a2a.method.name": "ListTaskPushNotificationConfigs",
					"a2a.task.id":     "task-1",
				},
			},
		},
		{
			name: "A2A error",
			req: a2aclient.Request{
				Method:  "GetTask",
				Payload: &a2a.GetTaskRequest{ID: "task-404"},
			},
			results: []callResult{{Err: a2a.NewError(a2a.ErrTaskNotFound, "task not found: task-404")}},
			want: spanSummary{
				Name: "GetTask",
				Kind: trace.SpanKindClient,
				Attrs: map[string]any{
					"a2a.method.name": "GetTask",
					"a2a.task.id":     "task-404",
					"error.type":      "TASK_NOT_FOUND",
				},
				Status: sdktrace.Status{Code: codes.Error, Description: "task not found: task-404"},
			},
		},
		{
			name: "error after stream events",
			req: a2aclient.Request{
				Method:  "SubscribeToTask",
				Payload: &a2a.SubscribeToTaskRequest{ID: "task-1"},
			},
			results: []callResult{
				{Payload: &a2a.Task{ID: "task-1", ContextID: "ctx-1", Status: a2a.TaskStatus{State: a2a.TaskStateWorking}}},
				{Err: &url.Error{Op: "Get", URL: "http://localhost", Err: errors.New("connection reset")}},
			},
			want: spanSummary{
				Name: "SubscribeToTask",
				Kind: trace.SpanKindClient,
				Attrs: map[string]any{
					"a2a.method.name":        "SubscribeToTask",
					"a2a.task.id":            "task-1",
					"a2a.task.state":         "TASK_STATE_WORKING",
					"gen_ai.conversation.id": "ctx-1",
					"error.type":             "*url.Error",
				},
				Status: sdktrace.Status{Code: codes.Error, Description: `Get "http://localhost": connection reset`},
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			telemetry := newTestTelemetry(t)
			plugin := NewClientPlugin(telemetry.clientOptions()...)
			ctx := t.Context()
			if tc.ctx != nil {
				ctx = tc.ctx(ctx)
			}

			gotResults := collect(plugin.WrapCall(newClientHandler(tc.results...))(ctx, tc.req))

			if diff := cmp.Diff(tc.results, gotResults, cmpopts.EquateErrors()); diff != "" {
				t.Fatalf("WrapCall() wrong results (-want +got) diff = %s", diff)
			}
			got := summarizeAll(telemetry.recorder.Ended())
			if diff := cmp.Diff([]spanSummary{tc.want}, got); diff != "" {
				t.Fatalf("WrapCall() wrong spans (-want +got) diff = %s", diff)
			}
		})
	}
}

func TestClientPlugin_PropagatesTraceContext(t *testing.T) {
	t.Parallel()
	telemetry := newTestTelemetry(t)
	plugin := NewClientPlugin(telemetry.clientOptions()...)
	var sentParams a2aclient.ServiceParams
	next := func(_ context.Context, req a2aclient.Request) iter.Seq2[any, error] {
		sentParams = req.ServiceParams
		return newClientHandler(callResult{Payload: &a2a.Task{}})(t.Context(), req)
	}
	callerParams := a2aclient.ServiceParams{"a2a-version": {"1.0"}}

	collect(plugin.WrapCall(next)(t.Context(), a2aclient.Request{Method: "GetTask", ServiceParams: callerParams}))

	spans := telemetry.recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("WrapCall() recorded %d spans, want 1", len(spans))
	}
	sc := spans[0].SpanContext()
	want := a2aclient.ServiceParams{
		"a2a-version": {"1.0"},
		"traceparent": {fmt.Sprintf("00-%s-%s-01", sc.TraceID(), sc.SpanID())},
	}
	if diff := cmp.Diff(want, sentParams); diff != "" {
		t.Fatalf("next() wrong service params (-want +got) diff = %s", diff)
	}
	if diff := cmp.Diff(a2aclient.ServiceParams{"a2a-version": {"1.0"}}, callerParams); diff != "" {
		t.Fatalf("caller's service params modified (-want +got) diff = %s", diff)
	}
}

func TestClientPlugin_EndsSpanWhenConsumerStops(t *testing.T) {
	t.Parallel()
	telemetry := newTestTelemetry(t)
	plugin := NewClientPlugin(telemetry.clientOptions()...)
	next := newClientHandler(
		callResult{Payload: &a2a.Task{ID: "task-1", ContextID: "ctx-1", Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}}},
		callResult{Payload: &a2a.TaskStatusUpdateEvent{TaskID: "task-1", ContextID: "ctx-1", Status: a2a.TaskStatus{State: a2a.TaskStateWorking}}},
	)

	for _, err := range plugin.WrapCall(next)(t.Context(), a2aclient.Request{Method: "SendStreamingMessage"}) {
		if err != nil {
			t.Fatalf("WrapCall() error = %v", err)
		}
		break
	}

	want := []spanSummary{{
		Name: "SendStreamingMessage",
		Kind: trace.SpanKindClient,
		Attrs: map[string]any{
			"a2a.method.name":        "SendStreamingMessage",
			"a2a.task.id":            "task-1",
			"a2a.task.state":         "TASK_STATE_SUBMITTED",
			"gen_ai.conversation.id": "ctx-1",
		},
	}}
	if diff := cmp.Diff(want, summarizeAll(telemetry.recorder.Ended())); diff != "" {
		t.Fatalf("WrapCall() wrong spans (-want +got) diff = %s", diff)
	}
}

type fakeGetTaskTransport struct {
	a2aclient.Transport
}

func (fakeGetTaskTransport) GetTask(_ context.Context, _ a2aclient.ServiceParams, req *a2a.GetTaskRequest) (*a2a.Task, error) {
	return &a2a.Task{ID: req.ID, ContextID: "ctx-1", Status: a2a.TaskStatus{State: a2a.TaskStateWorking}}, nil
}

func TestClientPlugin_Tenant(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name               string
		endpointTenant     string
		requestTenant      string
		contextTenant      string
		disablePropagation bool
		wantTenant         string
	}{
		{
			name:          "explicit request tenant",
			requestTenant: "explicit",
			contextTenant: "propagated",
			wantTenant:    "explicit",
		},
		{
			name:           "endpoint tenant without card",
			endpointTenant: "billing",
			contextTenant:  "propagated",
			wantTenant:     "billing",
		},
		{
			name:          "context tenant propagated",
			contextTenant: "propagated",
			wantTenant:    "propagated",
		},
		{
			name:               "context tenant propagation disabled",
			contextTenant:      "propagated",
			disablePropagation: true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			telemetry := newTestTelemetry(t)
			iface := a2a.NewAgentInterface("https://agent.example.com/a2a", a2a.TransportProtocolJSONRPC)
			iface.Tenant = tc.endpointTenant

			client, err := a2aclient.NewFromEndpoints(t.Context(), []*a2a.AgentInterface{iface},
				a2aclient.WithConfig(a2aclient.Config{DisableTenantPropagation: tc.disablePropagation}),
				a2aclient.WithTransport(a2a.TransportProtocolJSONRPC, a2aclient.TransportFactoryFn(func(context.Context, *a2a.AgentCard, *a2a.AgentInterface) (a2aclient.Transport, error) {
					return fakeGetTaskTransport{}, nil
				})),
				a2aclient.WithPlugins(NewClientPlugin(telemetry.clientOptions()...)),
			)
			if err != nil {
				t.Fatalf("a2aclient.NewFromEndpoints() error = %v, want nil", err)
			}

			ctx := t.Context()
			if tc.contextTenant != "" {
				ctx = a2a.AttachTenant(ctx, tc.contextTenant)
			}
			if _, err := client.GetTask(ctx, &a2a.GetTaskRequest{ID: "task-1", Tenant: tc.requestTenant}); err != nil {
				t.Fatalf("client.GetTask() error = %v, want nil", err)
			}

			spans := summarizeAll(telemetry.recorder.Ended())
			if len(spans) != 1 {
				t.Fatalf("client.GetTask() recorded %d spans, want 1", len(spans))
			}
			gotTenant, hasTenant := spans[0].Attrs["a2a.tenant"]
			if tc.wantTenant == "" {
				if hasTenant {
					t.Fatalf("a2a.tenant = %v, want omitted", gotTenant)
				}
				return
			}
			if gotTenant != tc.wantTenant {
				t.Fatalf("a2a.tenant = %v, want %q", gotTenant, tc.wantTenant)
			}
		})
	}
}
