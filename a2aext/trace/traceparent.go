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
	"fmt"
	"strings"
)

// W3C Trace Context constants.
const (
	// TraceParentVersion is the current W3C traceparent version (00).
	TraceParentVersion = "00"

	// TraceFlagSampled indicates the trace is sampled for recording.
	TraceFlagSampled = "01"

	// TraceFlagNotSampled indicates the trace is not sampled.
	TraceFlagNotSampled = "00"

	// TraceIDBytes is the number of bytes in a W3C Trace ID (16 bytes → 32 hex chars).
	TraceIDBytes = 16

	// SpanIDBytes is the number of bytes in a W3C Span ID (8 bytes → 16 hex chars).
	SpanIDBytes = 8
)

// EncodeTraceparent encodes the span as a W3C traceparent header value.
// Format: version-trace_id-span_id-flags
// Example: "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"
func (s *TraceSpan) EncodeTraceparent() string {
	flags := TraceFlagSampled
	return fmt.Sprintf("%s-%s-%s-%s", TraceParentVersion, s.TraceID, s.SpanID, flags)
}

// ParseTraceparent decodes a W3C traceparent header value into a partial
// TraceSpan suitable as a parent reference.
//
// Returns nil, error if:
//   - The header value is empty
//   - The version is not "00"
//   - The trace ID is not 32 hex chars
//   - The span ID is not 16 hex chars
func ParseTraceparent(val string) (*TraceSpan, error) {
	if val == "" {
		return nil, fmt.Errorf("trace: empty traceparent")
	}
	parts := strings.Split(val, "-")
	if len(parts) != 4 {
		return nil, fmt.Errorf("trace: invalid traceparent format: got %d parts, want 4", len(parts))
	}
	version, traceID, spanID, flags := parts[0], parts[1], parts[2], parts[3]

	if version != TraceParentVersion {
		return nil, fmt.Errorf("trace: unsupported traceparent version %q", version)
	}
	if len(traceID) != 2*TraceIDBytes {
		return nil, fmt.Errorf("trace: trace ID must be %d hex chars, got %d", 2*TraceIDBytes, len(traceID))
	}
	if len(spanID) != 2*SpanIDBytes {
		return nil, fmt.Errorf("trace: span ID must be %d hex chars, got %d", 2*SpanIDBytes, len(spanID))
	}
	if !isHex(traceID) {
		return nil, fmt.Errorf("trace: trace ID contains non-hex characters")
	}
	if !isHex(spanID) {
		return nil, fmt.Errorf("trace: span ID contains non-hex characters")
	}

	sampled := flags == TraceFlagSampled
	_ = sampled // reserved for future use (sampling decisions)

	return &TraceSpan{
		TraceID: traceID,
		SpanID:  spanID,
	}, nil
}

// Traceparent returns the span's traceparent string, or empty if the span
// lacks required fields.
func (s *TraceSpan) Traceparent() string {
	if s.TraceID == "" || s.SpanID == "" {
		return ""
	}
	return s.EncodeTraceparent()
}

func isHex(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// ServiceParams key for trace propagation.
const (
	// SvcParamTrace is the ServiceParams key for W3C traceparent propagation.
	// Value is a W3C traceparent string: "00-{trace_id}-{span_id}-{flags}"
	SvcParamTrace = "a2a-traceparent"
)

// Encode encodes the span as a ServiceParams-compatible string.
// This is the W3C traceparent format.
func (s *TraceSpan) Encode() string {
	return s.EncodeTraceparent()
}

// ParseTraceHeader decodes a trace header value into a partial TraceSpan
// suitable as a parent reference.
//
// Accepts both W3C traceparent (preferred) and legacy semicolon format
// for backward compatibility.
func ParseTraceHeader(val string) *TraceSpan {
	// Try W3C traceparent first
	span, err := ParseTraceparent(val)
	if err == nil {
		return span
	}
	// Fall back to legacy format: trace_id;span_id;parent_span_id;agent_id
	parts := strings.Split(val, ";")
	if len(parts) >= 3 && parts[0] != "" && parts[1] != "" {
		return &TraceSpan{
			TraceID:      parts[0],
			SpanID:       parts[1],
			ParentSpanID: parts[2],
			AgentID:      optionalPart(parts, 3),
		}
	}
	return nil
}

func optionalPart(parts []string, idx int) string {
	if idx < len(parts) {
		return parts[idx]
	}
	return ""
}
