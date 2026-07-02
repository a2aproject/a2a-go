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
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// spanCtxKey is the context key for [TraceSpan].
type spanCtxKey struct{}

// SpanFrom extracts the current trace span from the context.
// Returns nil if no span is present.
func SpanFrom(ctx context.Context) *TraceSpan {
	span, ok := ctx.Value(spanCtxKey{}).(*TraceSpan)
	if !ok {
		return nil
	}
	return span
}

// WithSpan attaches a [TraceSpan] to the context.
func WithSpan(ctx context.Context, span *TraceSpan) context.Context {
	return context.WithValue(ctx, spanCtxKey{}, span)
}

// TraceSpan records a single step in a cross-agent task chain.
//
// A span is created by a [ServerInterceptor] when an A2A call arrives.
// The [ClientInterceptor] reads the span and links it as the parent of any
// further outgoing calls.
type TraceSpan struct {
	// TraceID identifies the end-to-end delegation chain.
	// All spans within the same chain share the same TraceID.
	// Generated when no parent span exists (the root of the chain).
	TraceID string

	// SpanID is a unique identifier for this individual call.
	SpanID string

	// ParentSpanID is the SpanID of the calling agent's span, or empty
	// if this is the root of the chain.
	ParentSpanID string

	// AgentID identifies the agent that handled this call.
	AgentID string

	// TaskID is the A2A task identifier associated with this call.
	TaskID string

	// ContextID is the A2A context identifier grouping related interactions.
	ContextID string

	// Timestamp records when this span was created.
	Timestamp time.Time
}

// NewRootSpan creates a root span with a new TraceID and no parent.
func NewRootSpan(agentID, taskID, contextID string) *TraceSpan {
	return &TraceSpan{
		TraceID:      newID(16),
		SpanID:       newID(8),
		ParentSpanID: "",
		AgentID:      agentID,
		TaskID:       taskID,
		ContextID:    contextID,
		Timestamp:    time.Now(),
	}
}

// NewChildSpan creates a child span inheriting the parent's TraceID.
func NewChildSpan(parent *TraceSpan, agentID, taskID, contextID string) *TraceSpan {
	return &TraceSpan{
		TraceID:      parent.TraceID,
		SpanID:       newID(8),
		ParentSpanID: parent.SpanID,
		AgentID:      agentID,
		TaskID:       taskID,
		ContextID:    contextID,
		Timestamp:    time.Now(),
	}
}

// Chain returns the causal chain as a human-readable string:
//
//	agent-a:span₀ → agent-b:span₁ → agent-c:span₂
func (s *TraceSpan) Chain() string {
	return fmt.Sprintf("%s:%s", s.AgentID, s.SpanID)
}

// String returns a compact representation suitable for log lines.
func (s *TraceSpan) String() string {
	if s.ParentSpanID == "" {
		return fmt.Sprintf("[trace=%s span=%s agent=%s task=%s]", s.TraceID, s.SpanID, s.AgentID, s.TaskID)
	}
	return fmt.Sprintf("[trace=%s span=%s parent=%s agent=%s task=%s]", s.TraceID, s.SpanID, s.ParentSpanID, s.AgentID, s.TaskID)
}

// newID generates a random hex string with n bytes of entropy.
func newID(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("trace: failed to read random bytes: %v", err))
	}
	return hex.EncodeToString(b)
}
