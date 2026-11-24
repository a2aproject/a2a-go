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
	"net/url"
	"strconv"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aplugin"
)

// NewClientPlugin returns an [a2aclient.Plugin] which reports an A2A client span for every call
// and sends the trace context to the server in the call service parameters.
func NewClientPlugin(opts ...Option) a2aclient.Plugin {
	cfg := newConfig(opts)
	if !cfg.enabled {
		return a2aplugin.Client(pluginName, a2aplugin.ClientHooks{})
	}
	c := &clientInstrumentation{tracer: cfg.tracer(), propagators: cfg.propagators}
	return a2aplugin.Client(pluginName, a2aplugin.ClientHooks{WrapCall: c.wrapCall})
}

type clientInstrumentation struct {
	tracer      trace.Tracer
	propagators propagation.TextMapPropagator
}

func (c *clientInstrumentation) wrapCall(next a2aclient.CallHandler) a2aclient.CallHandler {
	return func(ctx context.Context, req a2aclient.Request) iter.Seq2[any, error] {
		return func(yield func(any, error) bool) {
			method := methodName(req.Method)
			spanCtx, span := c.tracer.Start(ctx, method,
				trace.WithSpanKind(trace.SpanKindClient),
				trace.WithAttributes(clientRequestAttributes(method, req)...),
			)
			observer := newCallObserver(req.Payload)
			defer func() {
				finishSpan(span, observer, clientErrorType)
				span.End()
			}()

			req.ServiceParams = injectServiceParams(spanCtx, c.propagators, req.ServiceParams)
			for payload, err := range next(spanCtx, req) {
				observer.observe(payload, err)
				if !yield(payload, err) {
					return
				}
			}
		}
	}
}

func clientRequestAttributes(method string, req a2aclient.Request) []attribute.KeyValue {
	attrs := requestAttributes(method, req.Payload)
	if versions := req.ServiceParams.Get(a2a.SvcParamVersion); len(versions) > 0 && versions[0] != "" {
		attrs = append(attrs, a2aProtocolVersionKey.String(versions[0]))
	}
	if tenant := requestTenant(req.Payload); tenant != "" {
		attrs = append(attrs, a2aTenantKey.String(tenant))
	}
	attrs = append(attrs, agentAttributes(req.Card)...)
	return append(attrs, serverAddressAttributes(req.BaseURL)...)
}

var defaultPorts = map[string]int{"http": 80, "https": 443}

// serverAddressAttributes returns the host and port of the agent interface URL. gRPC interface URLs
// can be a plain host:port target without a scheme.
func serverAddressAttributes(rawURL string) []attribute.KeyValue {
	host, port := "", 0
	if parsed, err := url.Parse(rawURL); err == nil && parsed.Host != "" {
		host, port = parsed.Hostname(), defaultPorts[parsed.Scheme]
		if explicit, err := strconv.Atoi(parsed.Port()); err == nil {
			port = explicit
		}
	} else if target, targetPort, err := net.SplitHostPort(rawURL); err == nil {
		host = target
		if explicit, err := strconv.Atoi(targetPort); err == nil {
			port = explicit
		}
	}
	if host == "" {
		return nil
	}
	attrs := []attribute.KeyValue{semconv.ServerAddressKey.String(host)}
	if port > 0 {
		attrs = append(attrs, semconv.ServerPortKey.Int(port))
	}
	return attrs
}
