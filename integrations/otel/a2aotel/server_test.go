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
	"iter"
	"maps"
	"testing"

	"github.com/google/go-cmp/cmp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

var testAgentCard = &a2a.AgentCard{Name: "Weather", Description: "Forecasts the weather", Version: "2.0.0"}

func completingExecutor() a2asrv.AgentExecutor {
	return a2asrv.AgentExecutorFunc(func(_ context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
		return func(yield func(a2a.Event, error) bool) {
			if !yield(a2a.NewSubmittedTask(execCtx, execCtx.Message), nil) {
				return
			}
			yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCompleted, nil), nil)
		}
	})
}

func failingExecutor(err error) a2asrv.AgentExecutor {
	return a2asrv.AgentExecutorFunc(func(context.Context, *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
		return func(yield func(a2a.Event, error) bool) {
			yield(nil, err)
		}
	})
}

// callContext attaches the call context a transport serving A2A 1.0 creates.
func callContext(ctx context.Context, params map[string][]string) context.Context {
	allParams := map[string][]string{a2a.SvcParamVersion: {string(a2a.Version)}}
	maps.Copy(allParams, params)
	ctx, callCtx := a2asrv.NewCallContext(ctx, a2asrv.NewServiceParams(allParams))
	callCtx.ProtocolVersion = a2a.Version
	return ctx
}

func newUserMessage(id, contextID string) *a2a.Message {
	msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("What's the weather?"))
	msg.ID = id
	msg.ContextID = contextID
	return msg
}

func TestServerPlugin_Spans(t *testing.T) {
	t.Parallel()
	agentAttrs := map[string]any{
		"gen_ai.agent.name":        "Weather",
		"gen_ai.agent.description": "Forecasts the weather",
		"gen_ai.agent.version":     "2.0.0",
	}
	disconnectDone := make(chan struct{})
	t.Cleanup(func() { close(disconnectDone) })
	testCases := []struct {
		name      string
		executor  a2asrv.AgentExecutor
		call      func(ctx context.Context, h a2asrv.RequestHandler) (a2a.TaskID, error)
		wantErr   bool
		wantAttrs func(taskID a2a.TaskID) map[string]any
		wantSpan  spanSummary
	}{
		{
			name:     "SendMessage",
			executor: completingExecutor(),
			call: func(ctx context.Context, h a2asrv.RequestHandler) (a2a.TaskID, error) {
				result, err := h.SendMessage(ctx, &a2a.SendMessageRequest{Tenant: "billing", Message: newUserMessage("msg-1", "ctx-1")})
				if err != nil {
					return "", err
				}
				return result.TaskInfo().TaskID, nil
			},
			wantAttrs: func(taskID a2a.TaskID) map[string]any {
				return map[string]any{
					"a2a.method.name":        "SendMessage",
					"a2a.protocol.version":   "1.0",
					"a2a.tenant":             "billing",
					"a2a.message.id":         "msg-1",
					"a2a.task.id":            string(taskID),
					"a2a.task.state":         "TASK_STATE_COMPLETED",
					"gen_ai.conversation.id": "ctx-1",
				}
			},
			wantSpan: spanSummary{Name: "SendMessage", Kind: trace.SpanKindServer},
		},
		{
			name:     "SendStreamingMessage",
			executor: completingExecutor(),
			call: func(ctx context.Context, h a2asrv.RequestHandler) (a2a.TaskID, error) {
				var taskID a2a.TaskID
				for event, err := range h.SendStreamingMessage(ctx, &a2a.SendMessageRequest{Message: newUserMessage("msg-1", "ctx-1")}) {
					if err != nil {
						return "", err
					}
					taskID = event.TaskInfo().TaskID
				}
				return taskID, nil
			},
			wantAttrs: func(taskID a2a.TaskID) map[string]any {
				return map[string]any{
					"a2a.method.name":        "SendStreamingMessage",
					"a2a.protocol.version":   "1.0",
					"a2a.message.id":         "msg-1",
					"a2a.task.id":            string(taskID),
					"gen_ai.conversation.id": "ctx-1",
				}
			},
			wantSpan: spanSummary{Name: "SendStreamingMessage", Kind: trace.SpanKindServer},
		},
		{
			name:     "A2A error",
			wantErr:  true,
			executor: completingExecutor(),
			call: func(ctx context.Context, h a2asrv.RequestHandler) (a2a.TaskID, error) {
				_, err := h.GetTask(ctx, &a2a.GetTaskRequest{ID: "task-404"})
				return "", err
			},
			wantAttrs: func(a2a.TaskID) map[string]any {
				return map[string]any{
					"a2a.method.name":      "GetTask",
					"a2a.protocol.version": "1.0",
					"a2a.task.id":          "task-404",
					"error.type":           "TASK_NOT_FOUND",
				}
			},
			wantSpan: spanSummary{
				Name:   "GetTask",
				Kind:   trace.SpanKindServer,
				Status: sdktrace.Status{Code: codes.Error, Description: "failed to get task: task not found"},
			},
		},
		{
			name:     "agent error",
			wantErr:  true,
			executor: failingExecutor(errors.New("model unavailable")),
			call: func(ctx context.Context, h a2asrv.RequestHandler) (a2a.TaskID, error) {
				_, err := h.SendMessage(ctx, &a2a.SendMessageRequest{Message: newUserMessage("msg-1", "ctx-1")})
				return "", err
			},
			wantAttrs: func(a2a.TaskID) map[string]any {
				return map[string]any{
					"a2a.method.name":        "SendMessage",
					"a2a.protocol.version":   "1.0",
					"a2a.message.id":         "msg-1",
					"gen_ai.conversation.id": "ctx-1",
					"error.type":             "INTERNAL_ERROR",
				}
			},
			wantSpan: spanSummary{
				Name:   "SendMessage",
				Kind:   trace.SpanKindServer,
				Status: sdktrace.Status{Code: codes.Error, Description: "model unavailable"},
			},
		},
		{
			name:    "client disconnect",
			wantErr: true,
			executor: a2asrv.AgentExecutorFunc(func(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
				return func(yield func(a2a.Event, error) bool) {
					if !yield(a2a.NewSubmittedTask(execCtx, execCtx.Message), nil) {
						return
					}
					select {
					case <-ctx.Done():
					case <-disconnectDone:
					}
				}
			}),
			call: func(ctx context.Context, h a2asrv.RequestHandler) (a2a.TaskID, error) {
				reqCtx, cancel := context.WithCancel(ctx)
				defer cancel()
				var taskID a2a.TaskID
				for event, err := range h.SendStreamingMessage(reqCtx, &a2a.SendMessageRequest{Message: newUserMessage("msg-1", "ctx-1")}) {
					if err != nil {
						return taskID, err
					}
					taskID = event.TaskInfo().TaskID
					cancel()
				}
				return taskID, nil
			},
			wantAttrs: func(taskID a2a.TaskID) map[string]any {
				return map[string]any{
					"a2a.method.name":        "SendStreamingMessage",
					"a2a.protocol.version":   "1.0",
					"a2a.message.id":         "msg-1",
					"a2a.task.id":            string(taskID),
					"a2a.task.state":         "TASK_STATE_SUBMITTED",
					"gen_ai.conversation.id": "ctx-1",
				}
			},
			wantSpan: spanSummary{Name: "SendStreamingMessage", Kind: trace.SpanKindServer},
		},
		{
			name:     "internal context canceled",
			wantErr:  true,
			executor: failingExecutor(context.Canceled),
			call: func(ctx context.Context, h a2asrv.RequestHandler) (a2a.TaskID, error) {
				_, err := h.SendMessage(ctx, &a2a.SendMessageRequest{Message: newUserMessage("msg-1", "ctx-1")})
				return "", err
			},
			wantAttrs: func(a2a.TaskID) map[string]any {
				return map[string]any{
					"a2a.method.name":        "SendMessage",
					"a2a.protocol.version":   "1.0",
					"a2a.message.id":         "msg-1",
					"gen_ai.conversation.id": "ctx-1",
					"error.type":             "INTERNAL_ERROR",
				}
			},
			wantSpan: spanSummary{
				Name:   "SendMessage",
				Kind:   trace.SpanKindServer,
				Status: sdktrace.Status{Code: codes.Error, Description: "context canceled"},
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			telemetry := newTestTelemetry(t)
			handler := a2asrv.NewHandler(tc.executor, a2asrv.WithPlugins(NewServerPlugin(telemetry.serverOptions(WithAgentCard(testAgentCard))...)))

			taskID, err := tc.call(callContext(t.Context(), nil), handler)
			if gotErr := err != nil; gotErr != tc.wantErr {
				t.Fatalf("handler call error = %v, want error %v", err, tc.wantErr)
			}

			want := tc.wantSpan
			want.Attrs = tc.wantAttrs(taskID)
			maps.Copy(want.Attrs, agentAttrs)
			if diff := cmp.Diff([]spanSummary{want}, summarizeAll(telemetry.recorder.Ended())); diff != "" {
				t.Fatalf("handler call wrong spans (-want +got) diff = %s", diff)
			}
		})
	}
}

func TestServerPlugin_TransportSpan(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name          string
		transportName string
		transportKind trace.SpanKind
		transportAttr attribute.KeyValue
		wantNames     []string
	}{
		{
			name:          "HTTP server span is renamed",
			transportName: "POST /",
			transportKind: trace.SpanKindServer,
			transportAttr: attribute.String("http.request.method", "POST"),
			wantNames:     []string{"GetTask"},
		},
		{
			name:          "gRPC server span keeps its name",
			transportName: "lf.a2a.v1.A2AService/GetTask",
			transportKind: trace.SpanKindServer,
			transportAttr: attribute.String("rpc.system.name", "grpc"),
			wantNames:     []string{"lf.a2a.v1.A2AService/GetTask"},
		},
		{
			name:          "server span of another kind gets a child",
			transportName: "process",
			transportKind: trace.SpanKindServer,
			transportAttr: attribute.String("messaging.system", "kafka"),
			wantNames:     []string{"GetTask", "process"},
		},
		{
			name:          "internal span gets a child",
			transportName: "handle",
			transportKind: trace.SpanKindInternal,
			transportAttr: attribute.String("http.request.method", "POST"),
			wantNames:     []string{"GetTask", "handle"},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			telemetry := newTestTelemetry(t)
			handler := a2asrv.NewHandler(completingExecutor(), a2asrv.WithPlugins(NewServerPlugin(telemetry.serverOptions()...)))
			ctx, transportSpan := telemetry.provider.Tracer("transport").Start(t.Context(), tc.transportName,
				trace.WithSpanKind(tc.transportKind),
				trace.WithAttributes(tc.transportAttr),
			)

			_, err := handler.GetTask(callContext(ctx, nil), &a2a.GetTaskRequest{ID: "task-404"})
			transportSpan.End()
			if !errors.Is(err, a2a.ErrTaskNotFound) {
				t.Fatalf("handler.GetTask() error = %v, want %v", err, a2a.ErrTaskNotFound)
			}

			spans := telemetry.recorder.Ended()
			var gotNames []string
			for _, span := range spans {
				gotNames = append(gotNames, span.Name())
			}
			if diff := cmp.Diff(tc.wantNames, gotNames); diff != "" {
				t.Fatalf("handler.GetTask() wrong span names (-want +got) diff = %s", diff)
			}
			a2aSpan := summarize(spans[0])
			if got := a2aSpan.Attrs["a2a.method.name"]; got != "GetTask" {
				t.Fatalf("a2a.method.name = %v, want %q", got, "GetTask")
			}
			if got := a2aSpan.Attrs["error.type"]; got != "TASK_NOT_FOUND" {
				t.Fatalf("error.type = %v, want %q", got, "TASK_NOT_FOUND")
			}
			if got := a2aSpan.Status.Code; got != codes.Error {
				t.Fatalf("span status = %v, want %v", got, codes.Error)
			}
		})
	}
}

func TestServerPlugin_Parent(t *testing.T) {
	t.Parallel()
	remoteTraceID := "4bf92f3577b34da6a3ce929d0e0e4736"
	remoteSpanID := "00f067aa0ba902b7"
	traceparent := "00-" + remoteTraceID + "-" + remoteSpanID + "-01"
	testCases := []struct {
		name          string
		params        map[string][]string
		withLocalSpan bool
		wantRemote    bool
	}{
		{
			name:       "trace context from service params",
			params:     map[string][]string{"Traceparent": {traceparent}},
			wantRemote: true,
		},
		{
			name:          "span in context takes precedence",
			params:        map[string][]string{"traceparent": {traceparent}},
			withLocalSpan: true,
		},
		{
			name: "no trace context",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			telemetry := newTestTelemetry(t)
			handler := a2asrv.NewHandler(completingExecutor(), a2asrv.WithPlugins(NewServerPlugin(telemetry.serverOptions()...)))
			ctx := t.Context()
			var localSpan trace.Span
			if tc.withLocalSpan {
				ctx, localSpan = telemetry.provider.Tracer("test").Start(ctx, "local")
			}

			if _, err := handler.GetTask(callContext(ctx, tc.params), &a2a.GetTaskRequest{ID: "task-404"}); err == nil {
				t.Fatal("handler.GetTask() error = nil, want an error")
			}

			spans := telemetry.recorder.Ended()
			if len(spans) != 1 {
				t.Fatalf("handler.GetTask() recorded %d spans, want 1", len(spans))
			}
			parent := spans[0].Parent()
			switch {
			case tc.wantRemote:
				if !parent.IsRemote() || parent.TraceID().String() != remoteTraceID || parent.SpanID().String() != remoteSpanID {
					t.Fatalf("server span parent = %v, want remote %s/%s", parent, remoteTraceID, remoteSpanID)
				}
			case tc.withLocalSpan:
				if parent.SpanID() != localSpan.SpanContext().SpanID() {
					t.Fatalf("server span parent = %v, want %v", parent.SpanID(), localSpan.SpanContext().SpanID())
				}
			default:
				if parent.IsValid() {
					t.Fatalf("server span parent = %v, want none", parent)
				}
			}
		})
	}
}
