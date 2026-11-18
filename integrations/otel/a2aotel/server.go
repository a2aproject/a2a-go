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

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aplugin"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/integrations/otel/a2aotel/internal/semconv"
)

// NewServerPlugin returns an [a2asrv.Plugin] which traces every call handled by the server.
//
// When the call is handled within a local HTTP or gRPC server span, for example one created by otelhttp
// or otelgrpc, the plugin adds the A2A attributes to that span instead of creating a nested one. HTTP
// server spans are renamed to the A2A method name, gRPC server spans keep their name. Otherwise, the plugin
// creates an A2A server span, using the trace context received in the call service parameters as its parent
// when the context doesn't carry a span.
func NewServerPlugin(opts ...ServerOption) a2asrv.Plugin {
	cfg := newServerConfig(opts)
	if !cfg.enabled {
		return a2aplugin.Server(pluginName, a2aplugin.ServerHooks{})
	}
	s := &serverInstrumentation{
		tracer:          cfg.tracer(),
		propagators:     cfg.propagators,
		agentAttributes: agentAttributes(cfg.agentCard),
	}
	return a2aplugin.Server(pluginName, a2aplugin.ServerHooks{WrapCall: s.wrapCall})
}

type serverInstrumentation struct {
	tracer          trace.Tracer
	propagators     propagation.TextMapPropagator
	agentAttributes []attribute.KeyValue
}

func (s *serverInstrumentation) wrapCall(next a2asrv.CallHandler) a2asrv.CallHandler {
	return func(ctx context.Context, callCtx *a2asrv.CallContext, req a2asrv.Request) iter.Seq2[any, error] {
		return func(yield func(any, error) bool) {
			method := semconv.MethodName(callCtx.Method())
			spanCtx, span, owned := s.startSpan(ctx, callCtx, method, s.requestAttributes(callCtx, method, req))
			observer := newCallObserver(req.Payload)
			defer func() {
				finishSpan(span, observer, semconv.ServerErrorType)
				if owned {
					span.End()
				}
			}()

			for payload, err := range next(spanCtx, callCtx, req) {
				observer.observe(payload, err)
				if !yield(payload, err) {
					return
				}
			}
		}
	}
}

// startSpan returns the span describing the call and whether the plugin owns it and must end it.
func (s *serverInstrumentation) startSpan(ctx context.Context, callCtx *a2asrv.CallContext, method string, attrs []attribute.KeyValue) (context.Context, trace.Span, bool) {
	current := trace.SpanFromContext(ctx)
	switch detectTransportSpan(current) {
	case transportSpanHTTP:
		current.SetName(method)
		current.SetAttributes(attrs...)
		return ctx, current, false
	case transportSpanGRPC:
		current.SetAttributes(attrs...)
		return ctx, current, false
	}

	if !trace.SpanContextFromContext(ctx).IsValid() {
		ctx = s.propagators.Extract(ctx, serverParamsCarrier{params: callCtx.ServiceParams()})
	}
	spanCtx, span := s.tracer.Start(ctx, method, trace.WithSpanKind(trace.SpanKindServer), trace.WithAttributes(attrs...))
	return spanCtx, span, true
}

func (s *serverInstrumentation) requestAttributes(callCtx *a2asrv.CallContext, method string, req a2asrv.Request) []attribute.KeyValue {
	attrs := requestAttributes(method, req.Payload)
	if version := serverProtocolVersion(callCtx); version != "" {
		attrs = append(attrs, semconv.A2AProtocolVersionKey.String(version))
	}
	if tenant := callCtx.Tenant(); tenant != "" {
		attrs = append(attrs, semconv.A2ATenantKey.String(tenant))
	}
	return append(attrs, s.agentAttributes...)
}

// serverProtocolVersion returns the version served by the transport. Transports which don't declare it
// accept any version, so the version requested by the client is the one used.
func serverProtocolVersion(callCtx *a2asrv.CallContext) string {
	if callCtx.ProtocolVersion != "" {
		return string(callCtx.ProtocolVersion)
	}
	if versions, ok := callCtx.ServiceParams().Get(a2a.SvcParamVersion); ok && len(versions) == 1 {
		return versions[0]
	}
	return ""
}
