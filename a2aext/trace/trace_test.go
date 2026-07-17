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

package trace

import (
	"context"
	"strings"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

func TestNewRootSpan(t *testing.T) {
	span := NewRootSpan("agent-a", "task-1", "ctx-1")
	if span.TraceID == "" {
		t.Error("TraceID must not be empty")
	}
	if len(span.TraceID) != 32 {
		t.Errorf("TraceID must be 32 hex chars, got %d", len(span.TraceID))
	}
	if span.SpanID == "" {
		t.Error("SpanID must not be empty")
	}
	if len(span.SpanID) != 16 {
		t.Errorf("SpanID must be 16 hex chars, got %d", len(span.SpanID))
	}
	if span.ParentSpanID != "" {
		t.Error("root span must have no parent")
	}
	if span.AgentID != "agent-a" {
		t.Errorf("AgentID = %q, want %q", span.AgentID, "agent-a")
	}
}

func TestNewChildSpan_Inheritance(t *testing.T) {
	parent := NewRootSpan("agent-a", "task-1", "ctx-1")
	// Child with empty taskID/contextID — should inherit from parent
	child := NewChildSpan(parent, "agent-b", "", "")
	if child.TraceID != parent.TraceID {
		t.Error("child must share parent TraceID")
	}
	if child.SpanID == parent.SpanID {
		t.Error("child must have unique SpanID")
	}
	if child.ParentSpanID != parent.SpanID {
		t.Errorf("child ParentSpanID = %q, want parent SpanID = %q", child.ParentSpanID, parent.SpanID)
	}
	if child.TaskID != parent.TaskID {
		t.Error("child must inherit parent TaskID when empty")
	}
	if child.ContextID != parent.ContextID {
		t.Error("child must inherit parent ContextID when empty")
	}
}

func TestNewChildSpan_Override(t *testing.T) {
	parent := NewRootSpan("agent-a", "task-1", "ctx-1")
	child := NewChildSpan(parent, "agent-b", "task-2", "ctx-2")
	if child.TaskID != "task-2" {
		t.Error("child must use explicit TaskID when provided")
	}
	if child.ContextID != "ctx-2" {
		t.Error("child must use explicit ContextID when provided")
	}
}

func TestEncodeTraceparent(t *testing.T) {
	span := &TraceSpan{
		TraceID: "0af7651916cd43dd8448eb211c80319c",
		SpanID:  "b7ad6b7169203331",
	}
	got := span.EncodeTraceparent()
	want := "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"
	if got != want {
		t.Errorf("EncodeTraceparent() = %q, want %q", got, want)
	}
}

func TestParseTraceparent_Valid(t *testing.T) {
	header := "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"
	span, err := ParseTraceparent(header)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if span.TraceID != "0af7651916cd43dd8448eb211c80319c" {
		t.Errorf("TraceID = %q, want %q", span.TraceID, "0af7651916cd43dd8448eb211c80319c")
	}
	if span.SpanID != "b7ad6b7169203331" {
		t.Errorf("SpanID = %q, want %q", span.SpanID, "b7ad6b7169203331")
	}
}

func TestParseTraceparent_Invalid(t *testing.T) {
	tests := []struct {
		name   string
		header string
	}{
		{"empty", ""},
		{"too few parts", "00-abc-def"},
		{"bad version", "ff-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"},
		{"short traceID", "00-abc-b7ad6b7169203331-01"},
		{"short spanID", "00-0af7651916cd43dd8448eb211c80319c-abc-01"},
		{"non-hex traceID", "00-ZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZ-b7ad6b7169203331-01"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseTraceparent(tt.header)
			if err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestParseTraceHeader_W3C(t *testing.T) {
	// W3C format
	header := "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"
	span := ParseTraceHeader(header)
	if span == nil {
		t.Fatal("expected span from W3C traceparent")
	}
	if span.TraceID != "0af7651916cd43dd8448eb211c80319c" {
		t.Errorf("TraceID = %q", span.TraceID)
	}
}

func TestParseTraceHeader_Legacy(t *testing.T) {
	// Legacy semicolon format
	header := "0af7651916cd43dd8448eb211c80319c;b7ad6b7169203331;aaaaaaaaaaaaaaaa;agent-a"
	span := ParseTraceHeader(header)
	if span == nil {
		t.Fatal("expected span from legacy format")
	}
	if span.TraceID != "0af7651916cd43dd8448eb211c80319c" {
		t.Errorf("TraceID = %q", span.TraceID)
	}
}

func TestParseTraceHeader_Empty(t *testing.T) {
	span := ParseTraceHeader("")
	if span != nil {
		t.Error("expected nil for empty header")
	}
}

func TestThreeHopChain(t *testing.T) {
	// Agent A creates root span
	root := NewRootSpan("agent-a", "task-1", "ctx-1")
	traceparentA := root.EncodeTraceparent()

	// Agent B receives and creates child (TaskID/ContextID from A2A payload)
	parentB := ParseTraceHeader(traceparentA)
	childB := NewChildSpan(parentB, "agent-b", "task-1", "ctx-1")
	traceparentB := childB.EncodeTraceparent()

	// Agent C receives and creates child (TaskID/ContextID from A2A payload)
	parentC := ParseTraceHeader(traceparentB)
	childC := NewChildSpan(parentC, "agent-c", "task-1", "ctx-1")

	// Verify chain
	if childC.TraceID != root.TraceID {
		t.Error("all spans must share same TraceID")
	}
	if childC.ParentSpanID != childB.SpanID {
		t.Error("C's parent must be B")
	}
	if childB.ParentSpanID != root.SpanID {
		t.Error("B's parent must be root")
	}
	// Verify explicit TaskID/ContextID (from A2A payload, not traceparent)
	if childC.TaskID != "task-1" {
		t.Errorf("C TaskID = %q, want task-1", childC.TaskID)
	}
	if childC.ContextID != "ctx-1" {
		t.Errorf("C ContextID = %q, want ctx-1", childC.ContextID)
	}
}

func TestChain(t *testing.T) {
	span := NewRootSpan("research-agent", "task-1", "")
	got := span.Chain()
	if !strings.Contains(got, "research-agent:") {
		t.Errorf("Chain() = %q, want agent:span format", got)
	}
}

func TestSpanFrom_WithSpan(t *testing.T) {
	ctx := context.Background()
	if SpanFrom(ctx) != nil {
		t.Error("SpanFrom on empty context should return nil")
	}
	span := NewRootSpan("agent-a", "", "")
	ctx = WithSpan(ctx, span)
	got := SpanFrom(ctx)
	if got == nil {
		t.Fatal("SpanFrom should return the span")
	}
	if got.SpanID != span.SpanID {
		t.Error("SpanFrom returned wrong span")
	}
}

func TestBridgeToMCP(t *testing.T) {
	ctx := context.Background()
	// No span → nil
	if tc := BridgeToMCP(ctx); tc != nil {
		t.Error("BridgeToMCP without span should return nil")
	}
	// With span → traceparent map
	span := NewRootSpan("agent-a", "task-1", "ctx-1")
	ctx = WithSpan(ctx, span)
	tc := BridgeToMCP(ctx)
	if tc == nil {
		t.Fatal("BridgeToMCP should return non-nil")
	}
	if tc["traceparent"] == "" {
		t.Error("BridgeToMCP must return traceparent")
	}
}

func TestBridgeFromMCP(t *testing.T) {
	ctx := context.Background()
	// Valid traceparent
	tp := "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"
	ctx = BridgeFromMCP(ctx, tp, "agent-b")
	span := SpanFrom(ctx)
	if span == nil {
		t.Fatal("BridgeFromMCP should attach a span")
	}
	if span.TraceID != "0af7651916cd43dd8448eb211c80319c" {
		t.Errorf("TraceID = %q, want %q", span.TraceID, "0af7651916cd43dd8448eb211c80319c")
	}
	if span.AgentID != "agent-b" {
		t.Errorf("AgentID = %q, want %q", span.AgentID, "agent-b")
	}
	// Malformed traceparent → new root span
	ctx2 := BridgeFromMCP(context.Background(), "garbage", "agent-c")
	span2 := SpanFrom(ctx2)
	if span2 == nil {
		t.Fatal("BridgeFromMCP should create root span on malformed input")
	}
	if span2.ParentSpanID != "" {
		t.Error("malformed input should produce root span (no parent)")
	}
}

func TestEncode(t *testing.T) {
	span := &TraceSpan{
		TraceID: "0af7651916cd43dd8448eb211c80319c",
		SpanID:  "b7ad6b7169203331",
	}
	// Encode() should produce W3C traceparent
	got := span.Encode()
	if !strings.HasPrefix(got, "00-") {
		t.Errorf("Encode() should produce W3C traceparent, got %q", got)
	}
}

func TestString(t *testing.T) {
	root := NewRootSpan("agent-a", "task-1", "")
	if !strings.Contains(root.String(), "agent-a") {
		t.Errorf("String() = %q, want agent-a", root.String())
	}
	child := NewChildSpan(root, "agent-b", "", "")
	if !strings.Contains(child.String(), "parent=") {
		t.Errorf("child String() should include parent, got %q", child.String())
	}
}

func TestIDCollisions(t *testing.T) {
	ids := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		id := newID(8)
		if ids[id] {
			t.Fatalf("ID collision at iteration %d", i)
		}
		ids[id] = true
	}
}

func TestServerInterceptor_Noop(t *testing.T) {
	si := NewServerInterceptor(ServerConfig{}) // empty AgentID
	if _, ok := si.(a2asrv.PassthroughCallInterceptor); !ok {
		t.Error("server interceptor with empty AgentID should be no-op")
	}
}
