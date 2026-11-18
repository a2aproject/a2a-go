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

package a2aotel_test

import (
	"context"
	"errors"
	"iter"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/google/go-cmp/cmp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/integrations/otel/a2aotel"
)

type e2eSpan struct {
	Name   string
	Kind   trace.SpanKind
	Attrs  map[string]any
	Parent string
}

func TestJSONRPC_ClientAndServerSpans(t *testing.T) {
	t.Parallel()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() {
		if err := provider.Shutdown(context.Background()); err != nil {
			t.Errorf("provider.Shutdown() error = %v", err)
		}
	})
	tracerProvider := a2aotel.WithTracerProvider(provider)
	propagators := a2aotel.WithPropagators(propagation.TraceContext{})
	card := &a2a.AgentCard{Name: "Weather", Description: "Forecasts the weather", Version: "2.0.0"}
	executor := a2asrv.AgentExecutorFunc(func(_ context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
		return func(yield func(a2a.Event, error) bool) {
			yield(a2a.NewMessageForTask(a2a.MessageRoleAgent, execCtx, a2a.NewTextPart("Sunny")), nil)
		}
	})
	handler := a2asrv.NewHandler(executor, a2asrv.WithPlugins(a2aotel.NewServerPlugin(tracerProvider, propagators, a2aotel.WithAgentCard(card))))
	server := httptest.NewServer(a2asrv.NewJSONRPCHandler(handler))
	t.Cleanup(server.Close)
	client, err := a2aclient.NewFromEndpoints(
		t.Context(),
		[]*a2a.AgentInterface{a2a.NewAgentInterface(server.URL, a2a.TransportProtocolJSONRPC)},
		a2aclient.WithPlugins(a2aotel.NewClientPlugin(tracerProvider, propagators)),
	)
	if err != nil {
		t.Fatalf("a2aclient.NewFromEndpoints() error = %v", err)
	}
	t.Cleanup(func() {
		if err := client.Destroy(); err != nil {
			t.Errorf("client.Destroy() error = %v", err)
		}
	})
	serverURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	serverPort, err := strconv.Atoi(serverURL.Port())
	if err != nil {
		t.Fatalf("strconv.Atoi() error = %v", err)
	}
	msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("What's the weather?"))
	msg.ID = "msg-1"
	msg.ContextID = "ctx-1"

	if _, err := client.SendMessage(t.Context(), &a2a.SendMessageRequest{Message: msg}); err != nil {
		t.Fatalf("client.SendMessage() error = %v", err)
	}
	if _, err := client.GetTask(t.Context(), &a2a.GetTaskRequest{ID: "task-404"}); !errors.Is(err, a2a.ErrTaskNotFound) {
		t.Fatalf("client.GetTask() error = %v, want %v", err, a2a.ErrTaskNotFound)
	}

	spans := recorder.Ended()
	names := map[trace.SpanID]string{}
	for _, span := range spans {
		names[span.SpanContext().SpanID()] = span.Name() + "/" + span.SpanKind().String()
	}
	var got []e2eSpan
	for _, span := range spans {
		attrs := map[string]any{}
		for _, attr := range span.Attributes() {
			attrs[string(attr.Key)] = attr.Value.AsInterface()
		}
		got = append(got, e2eSpan{Name: span.Name(), Kind: span.SpanKind(), Attrs: attrs, Parent: names[span.Parent().SpanID()]})
	}
	clientAttrs := []attribute.KeyValue{
		attribute.String("a2a.protocol.version", "1.0"),
		attribute.String("server.address", serverURL.Hostname()),
		attribute.Int("server.port", serverPort),
	}
	serverAttrs := []attribute.KeyValue{
		attribute.String("a2a.protocol.version", "1.0"),
		attribute.String("gen_ai.agent.name", "Weather"),
		attribute.String("gen_ai.agent.description", "Forecasts the weather"),
		attribute.String("gen_ai.agent.version", "2.0.0"),
	}
	sendAttrs := []attribute.KeyValue{
		attribute.String("a2a.method.name", "SendMessage"),
		attribute.String("a2a.message.id", "msg-1"),
		attribute.String("gen_ai.conversation.id", "ctx-1"),
	}
	getAttrs := []attribute.KeyValue{
		attribute.String("a2a.method.name", "GetTask"),
		attribute.String("a2a.task.id", "task-404"),
		attribute.String("error.type", "TASK_NOT_FOUND"),
	}
	want := []e2eSpan{
		{Name: "SendMessage", Kind: trace.SpanKindServer, Attrs: attrsOf(serverAttrs, sendAttrs), Parent: "SendMessage/client"},
		{Name: "SendMessage", Kind: trace.SpanKindClient, Attrs: attrsOf(clientAttrs, sendAttrs)},
		{Name: "GetTask", Kind: trace.SpanKindServer, Attrs: attrsOf(serverAttrs, getAttrs), Parent: "GetTask/client"},
		{Name: "GetTask", Kind: trace.SpanKindClient, Attrs: attrsOf(clientAttrs, getAttrs)},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("recorded spans wrong (-want +got) diff = %s", diff)
	}
}

func attrsOf(groups ...[]attribute.KeyValue) map[string]any {
	attrs := map[string]any{}
	for _, group := range groups {
		for _, attr := range group {
			attrs[string(attr.Key)] = attr.Value.AsInterface()
		}
	}
	return attrs
}
