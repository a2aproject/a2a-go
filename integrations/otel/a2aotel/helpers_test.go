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
	"maps"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

type spanSummary struct {
	Name   string
	Kind   trace.SpanKind
	Attrs  map[string]any
	Status sdktrace.Status
}

func summarize(span sdktrace.ReadOnlySpan) spanSummary {
	return spanSummary{
		Name:   span.Name(),
		Kind:   span.SpanKind(),
		Attrs:  attrMap(span.Attributes()),
		Status: span.Status(),
	}
}

func summarizeAll(spans []sdktrace.ReadOnlySpan) []spanSummary {
	summaries := make([]spanSummary, len(spans))
	for i, span := range spans {
		summaries[i] = summarize(span)
	}
	return summaries
}

func withAttrs(base, extra map[string]any) map[string]any {
	m := maps.Clone(base)
	maps.Copy(m, extra)
	return m
}

func attrMap(attrs []attribute.KeyValue) map[string]any {
	m := make(map[string]any, len(attrs))
	for _, attr := range attrs {
		m[string(attr.Key)] = attr.Value.AsInterface()
	}
	return m
}

type testTelemetry struct {
	provider *sdktrace.TracerProvider
	recorder *tracetest.SpanRecorder
}

func newTestTelemetry(t *testing.T) *testTelemetry {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() {
		if err := provider.Shutdown(context.Background()); err != nil {
			t.Errorf("provider.Shutdown() error = %v", err)
		}
	})
	return &testTelemetry{provider: provider, recorder: recorder}
}

func (tt *testTelemetry) clientOptions() []Option {
	return []Option{WithTracerProvider(tt.provider), WithPropagators(propagation.TraceContext{})}
}

func (tt *testTelemetry) serverOptions(opts ...ServerOption) []ServerOption {
	return append([]ServerOption{WithTracerProvider(tt.provider), WithPropagators(propagation.TraceContext{})}, opts...)
}
