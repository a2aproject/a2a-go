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
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
	"go.opentelemetry.io/otel/trace"
)

// finishSpan records the attributes known after the call and the call error, following the
// OpenTelemetry Recording Errors conventions. It does not end the span.
func finishSpan(span trace.Span, observer *callObserver, errorType func(error) string) {
	span.SetAttributes(observer.attributes()...)
	if observer.err == nil {
		return
	}
	span.SetAttributes(semconv.ErrorTypeKey.String(errorType(observer.err)))
	span.SetStatus(codes.Error, observer.err.Error())
}

type transportSpanKind int

const (
	transportSpanNone transportSpanKind = iota
	transportSpanHTTP
	transportSpanGRPC
)

// readableSpan is implemented by the spans of the OpenTelemetry SDK.
type readableSpan interface {
	SpanKind() trace.SpanKind
	Attributes() []attribute.KeyValue
}

// detectTransportSpan reports whether span is a local HTTP or gRPC server span, as created by
// otelhttp or otelgrpc, in which the A2A request is handled. Both the current and the legacy
// HTTP and RPC semantic convention attributes are recognized.
func detectTransportSpan(span trace.Span) transportSpanKind {
	if !span.IsRecording() {
		return transportSpanNone
	}
	readable, ok := span.(readableSpan)
	if !ok || readable.SpanKind() != trace.SpanKindServer {
		return transportSpanNone
	}
	kind := transportSpanNone
	for _, attr := range readable.Attributes() {
		switch attr.Key {
		case "rpc.system.name", "rpc.system":
			if attr.Value.AsString() == "grpc" {
				return transportSpanGRPC
			}
		case "http.request.method", "http.method":
			kind = transportSpanHTTP
		}
	}
	return kind
}
