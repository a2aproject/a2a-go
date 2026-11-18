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
	"iter"
	"net"
	"net/http/httptest"
	"testing"

	"github.com/google/go-cmp/cmp"
	"go.opentelemetry.io/otel/trace"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

func TestJSONRPC_ClientAndServerSpans(t *testing.T) {
	t.Parallel()
	telemetry := newTestTelemetry(t)
	executor := a2asrv.AgentExecutorFunc(func(_ context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
		return func(yield func(a2a.Event, error) bool) {
			yield(a2a.NewMessageForTask(a2a.MessageRoleAgent, execCtx, a2a.NewTextPart("Sunny")), nil)
		}
	})
	handler := a2asrv.NewHandler(executor, a2asrv.WithPlugins(NewServerPlugin(telemetry.serverOptions(WithAgentCard(testAgentCard))...)))
	server := httptest.NewServer(a2asrv.NewJSONRPCHandler(handler))
	t.Cleanup(server.Close)
	client, err := a2aclient.NewFromEndpoints(
		t.Context(),
		[]*a2a.AgentInterface{a2a.NewAgentInterface(server.URL, a2a.TransportProtocolJSONRPC)},
		a2aclient.WithPlugins(NewClientPlugin(telemetry.clientOptions()...)),
	)
	if err != nil {
		t.Fatalf("a2aclient.NewFromEndpoints() error = %v", err)
	}
	t.Cleanup(func() {
		if err := client.Destroy(); err != nil {
			t.Errorf("client.Destroy() error = %v", err)
		}
	})

	if _, err := client.SendMessage(t.Context(), &a2a.SendMessageRequest{Message: newUserMessage("msg-1", "ctx-1")}); err != nil {
		t.Fatalf("client.SendMessage() error = %v", err)
	}

	spans := telemetry.recorder.Ended()
	if len(spans) != 2 {
		t.Fatalf("recorded %d spans, want 2", len(spans))
	}
	if got, want := spans[0].Parent().SpanID(), spans[1].SpanContext().SpanID(); got != want {
		t.Fatalf("server span parent = %v, want client span %v", got, want)
	}
	addr := server.Listener.Addr().(*net.TCPAddr)
	callAttrs := map[string]any{
		"a2a.method.name":        "SendMessage",
		"a2a.protocol.version":   "1.0",
		"a2a.message.id":         "msg-1",
		"gen_ai.conversation.id": "ctx-1",
	}
	want := []spanSummary{
		{Name: "SendMessage", Kind: trace.SpanKindServer, Attrs: withAttrs(callAttrs, map[string]any{
			"gen_ai.agent.name":        "Weather",
			"gen_ai.agent.description": "Forecasts the weather",
			"gen_ai.agent.version":     "2.0.0",
		})},
		{Name: "SendMessage", Kind: trace.SpanKindClient, Attrs: withAttrs(callAttrs, map[string]any{
			"server.address": addr.IP.String(),
			"server.port":    int64(addr.Port),
		})},
	}
	if diff := cmp.Diff(want, summarizeAll(spans)); diff != "" {
		t.Fatalf("recorded spans wrong (-want +got) diff = %s", diff)
	}
}
