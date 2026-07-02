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

package trace_test

import (
	"context"
	"iter"
	"net/http/httptest"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aext/trace"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/internal/testutil/testexecutor"
)

func TestTripleHopTrace(t *testing.T) {
	t.Parallel()

	// Agent C: a leaf server that records the span it receives and the
	// trace header from ServiceParams.
	var capturedSpan *trace.TraceSpan
	var capturedTraceHeader string
	agentC := testexecutor.FromFunction(func(ctx context.Context, ec *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
		return func(yield func(a2a.Event, error) bool) {
			capturedSpan = trace.SpanFrom(ctx)
			if vals, ok := ec.ServiceParams.Get(trace.SvcParamTrace); ok && len(vals) > 0 {
				capturedTraceHeader = vals[0]
			}
			event := a2a.NewSubmittedTask(ec, ec.Message)
			event.Status = a2a.TaskStatus{State: a2a.TaskStateCompleted}
			yield(event, nil)
		}
	})

	handlerC := a2asrv.NewHandler(agentC,
		a2asrv.WithCallInterceptors(trace.NewServerInterceptor(trace.ServerConfig{
			AgentID: "agent-c",
		})),
	)
	serverC := httptest.NewServer(a2asrv.NewJSONRPCHandler(handlerC))
	t.Cleanup(serverC.Close)

	cardC := &a2a.AgentCard{
		Name: "agent-c",
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(serverC.URL, a2a.TransportProtocolJSONRPC),
		},
	}

	// Agent B: receives a call from Agent A, then delegates to Agent C.
	var agentBSpan *trace.TraceSpan
	agentB := testexecutor.FromFunction(func(ctx context.Context, ec *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
		return func(yield func(a2a.Event, error) bool) {
			agentBSpan = trace.SpanFrom(ctx)

			client, err := a2aclient.NewFromCard(ctx, cardC,
				a2aclient.WithCallInterceptors(trace.NewClientInterceptor(trace.ClientConfig{
					AgentID: "agent-b",
				})),
			)
			if err != nil {
				t.Errorf("a2aclient.NewFromCard() for agent C error = %v", err)
				return
			}

			_, err = client.SendMessage(ctx, &a2a.SendMessageRequest{
				Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("forwarded")),
			})
			if err != nil {
				t.Errorf("client.SendMessage() to agent C error = %v", err)
				return
			}

			event := a2a.NewSubmittedTask(ec, ec.Message)
			event.Status = a2a.TaskStatus{State: a2a.TaskStateCompleted}
			yield(event, nil)
		}
	})

	handlerB := a2asrv.NewHandler(agentB,
		a2asrv.WithCallInterceptors(trace.NewServerInterceptor(trace.ServerConfig{
			AgentID: "agent-b",
		})),
	)
	serverB := httptest.NewServer(a2asrv.NewJSONRPCHandler(handlerB))
	t.Cleanup(serverB.Close)

	cardB := &a2a.AgentCard{
		Name: "agent-b",
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(serverB.URL, a2a.TransportProtocolJSONRPC),
		},
	}

	// Agent A: the root caller.
	ctx := t.Context()
	clientA, err := a2aclient.NewFromCard(ctx, cardB,
		a2aclient.WithCallInterceptors(trace.NewClientInterceptor(trace.ClientConfig{
			AgentID: "agent-a",
		})),
	)
	if err != nil {
		t.Fatalf("a2aclient.NewFromCard() for agent B error = %v", err)
	}

	_, err = clientA.SendMessage(ctx, &a2a.SendMessageRequest{
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hello")),
	})
	if err != nil {
		t.Fatalf("clientA.SendMessage() error = %v", err)
	}

	// Assertions.

	if agentBSpan == nil {
		t.Fatal("agent B span is nil — server interceptor did not create it")
	}
	if capturedSpan == nil {
		t.Fatal("agent C span is nil — server interceptor did not create it")
	}

	// Agent C must share the same TraceID as Agent B (same delegation chain).
	if agentBSpan.TraceID != capturedSpan.TraceID {
		t.Errorf("TraceID mismatch: B=%s, C=%s", agentBSpan.TraceID, capturedSpan.TraceID)
	}

	// Agent C's span must have a non-empty parent (it was created as a child).
	if capturedSpan.ParentSpanID == "" {
		t.Error("Agent C ParentSpanID is empty — should be a child of a span from Agent B")
	}

	// The trace header received at Agent C should encode B's client span.
	// The chain is: A_client → B_server → B_client → C_server.
	// C's parent is B_client (the span created when B delegated to C).
	if capturedTraceHeader == "" {
		t.Error("capturedTraceHeader is empty — trace was not propagated")
	}
	t.Logf("trace header at Agent C: %s", capturedTraceHeader)

	// AgentIDs.
	if agentBSpan.AgentID != "agent-b" {
		t.Errorf("Agent B AgentID = %s, want agent-b", agentBSpan.AgentID)
	}
	if capturedSpan.AgentID != "agent-c" {
		t.Errorf("Agent C AgentID = %s, want agent-c", capturedSpan.AgentID)
	}

	// IDs must have correct lengths.
	if len(agentBSpan.TraceID) != 32 {
		t.Errorf("TraceID length = %d, want 32", len(agentBSpan.TraceID))
	}
	if len(agentBSpan.SpanID) != 16 {
		t.Errorf("SpanID length = %d, want 16", len(agentBSpan.SpanID))
	}
}

func TestNewRootSpan(t *testing.T) {
	t.Parallel()

	span := trace.NewRootSpan("test-agent", "task-1", "ctx-1")

	if span.TraceID == "" {
		t.Error("TraceID is empty")
	}
	if span.SpanID == "" {
		t.Error("SpanID is empty")
	}
	if span.ParentSpanID != "" {
		t.Errorf("ParentSpanID = %s, want empty", span.ParentSpanID)
	}
	if span.AgentID != "test-agent" {
		t.Errorf("AgentID = %s, want test-agent", span.AgentID)
	}
	if span.TaskID != "task-1" {
		t.Errorf("TaskID = %s, want task-1", span.TaskID)
	}
	if span.ContextID != "ctx-1" {
		t.Errorf("ContextID = %s, want ctx-1", span.ContextID)
	}
	if span.Timestamp.IsZero() {
		t.Error("Timestamp is zero")
	}
}

func TestNewChildSpan(t *testing.T) {
	t.Parallel()

	parent := trace.NewRootSpan("parent-agent", "task-p", "ctx-p")
	child := trace.NewChildSpan(parent, "child-agent", "task-c", "ctx-c")

	if child.TraceID != parent.TraceID {
		t.Errorf("Child TraceID = %s, want %s (parent's)", child.TraceID, parent.TraceID)
	}
	if child.ParentSpanID != parent.SpanID {
		t.Errorf("Child ParentSpanID = %s, want %s (parent's SpanID)", child.ParentSpanID, parent.SpanID)
	}
	if child.SpanID == parent.SpanID {
		t.Error("Child SpanID should differ from parent SpanID")
	}
	if child.AgentID != "child-agent" {
		t.Errorf("Child AgentID = %s, want child-agent", child.AgentID)
	}
}

func TestSpanFromNilContext(t *testing.T) {
	t.Parallel()

	span := trace.SpanFrom(context.Background())
	if span != nil {
		t.Errorf("SpanFrom(empty context) = %v, want nil", span)
	}
}

func TestNoopServerInterceptor(t *testing.T) {
	t.Parallel()

	si := trace.NewServerInterceptor(trace.ServerConfig{})
	_, ok := si.(a2asrv.PassthroughCallInterceptor)
	if !ok {
		t.Errorf("NewServerInterceptor with empty AgentID should return PassthroughCallInterceptor, got %T", si)
	}
}

func TestSpanString(t *testing.T) {
	t.Parallel()

	root := &trace.TraceSpan{
		TraceID:      "abc123",
		SpanID:       "def456",
		ParentSpanID: "",
		AgentID:      "sg-architect",
		TaskID:       "task-xyz",
	}
	if root.String() == "" {
		t.Error("root String() returned empty")
	}

	child := &trace.TraceSpan{
		TraceID:      "abc123",
		SpanID:       "ghi789",
		ParentSpanID: "def456",
		AgentID:      "do-developer",
		TaskID:       "task-xyz",
	}
	if child.String() == "" {
		t.Error("child String() returned empty")
	}
}

func TestSpanChain(t *testing.T) {
	t.Parallel()

	span := &trace.TraceSpan{
		AgentID: "agent-a",
		SpanID:  "span0",
	}
	want := "agent-a:span0"
	if got := span.Chain(); got != want {
		t.Errorf("Chain() = %s, want %s", got, want)
	}
}

func TestEncodeDecode(t *testing.T) {
	t.Parallel()

	original := trace.NewRootSpan("test-agent", "task-1", "ctx-1")
	encoded := original.Encode()

	decoded := trace.ParseTraceHeader(encoded)
	if decoded == nil {
		t.Fatal("ParseTraceHeader returned nil")
	}
	if decoded.TraceID != original.TraceID {
		t.Errorf("TraceID = %s, want %s", decoded.TraceID, original.TraceID)
	}
	if decoded.SpanID != original.SpanID {
		t.Errorf("SpanID = %s, want %s", decoded.SpanID, original.SpanID)
	}
	if decoded.ParentSpanID != original.ParentSpanID {
		t.Errorf("ParentSpanID = %s, want %s", decoded.ParentSpanID, original.ParentSpanID)
	}
	if decoded.AgentID != original.AgentID {
		t.Errorf("AgentID = %s, want %s", decoded.AgentID, original.AgentID)
	}
}

func TestParseTraceHeaderInvalid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"too few parts", "abc;def"},
		{"single part", "abc"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := trace.ParseTraceHeader(tc.input); got != nil {
				t.Errorf("ParseTraceHeader(%q) = %v, want nil", tc.input, got)
			}
		})
	}
}

func TestTraceHeaderPropagation(t *testing.T) {
	t.Parallel()

	// Agent B: verifies that the incoming SvcParamTrace header carries the
	// caller's trace context and that the server interceptor creates a child span.
	var receivedSpan *trace.TraceSpan
	var receivedTraceHeader string
	agentB := testexecutor.FromFunction(func(ctx context.Context, ec *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
		return func(yield func(a2a.Event, error) bool) {
			receivedSpan = trace.SpanFrom(ctx)
			if vals, ok := ec.ServiceParams.Get(trace.SvcParamTrace); ok && len(vals) > 0 {
				receivedTraceHeader = vals[0]
			}
			event := a2a.NewSubmittedTask(ec, ec.Message)
			event.Status = a2a.TaskStatus{State: a2a.TaskStateCompleted}
			yield(event, nil)
		}
	})

	handler := a2asrv.NewHandler(agentB,
		a2asrv.WithCallInterceptors(trace.NewServerInterceptor(trace.ServerConfig{
			AgentID: "agent-b",
		})),
	)
	server := httptest.NewServer(a2asrv.NewJSONRPCHandler(handler))
	t.Cleanup(server.Close)

	card := &a2a.AgentCard{
		Name: "agent-b",
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(server.URL, a2a.TransportProtocolJSONRPC),
		},
	}

	ctx := t.Context()
	client, err := a2aclient.NewFromCard(ctx, card,
		a2aclient.WithCallInterceptors(trace.NewClientInterceptor(trace.ClientConfig{
			AgentID: "agent-a",
		})),
	)
	if err != nil {
		t.Fatalf("a2aclient.NewFromCard() error = %v", err)
	}

	_, err = client.SendMessage(ctx, &a2a.SendMessageRequest{
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("ping")),
	})
	if err != nil {
		t.Fatalf("client.SendMessage() error = %v", err)
	}

	// The server should have received a trace header from the client.
	if receivedTraceHeader == "" {
		t.Fatal("receivedTraceHeader is empty — trace was not propagated")
	}
	t.Logf("received trace header: %s", receivedTraceHeader)

	// The server interceptor should have created a span.
	if receivedSpan == nil {
		t.Fatal("receivedSpan is nil — server interceptor did not create a span")
	}

	// The server span should be a child of the caller's span (non-empty parent).
	if receivedSpan.ParentSpanID == "" {
		t.Error("receivedSpan has empty ParentSpanID — should be a child of the caller's span")
	}

	// Server AgentID should be correct.
	if receivedSpan.AgentID != "agent-b" {
		t.Errorf("receivedSpan.AgentID = %s, want agent-b", receivedSpan.AgentID)
	}
}
