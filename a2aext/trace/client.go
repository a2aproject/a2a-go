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
	"log/slog"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

// ClientConfig configures the client-side trace interceptor.
type ClientConfig struct {
	// AgentID is the identifier injected into spans created by this interceptor.
	AgentID string

	// Logger receives span propagation events. If nil, slog.Default() is used.
	Logger *slog.Logger
}

// NewClientInterceptor returns a client interceptor that propagates W3C trace
// context to outgoing A2A calls. If a parent [TraceSpan] is present in the
// context, a child span is created and propagated. Otherwise, a new root span
// is created.
//
// The trace context is sent as a W3C traceparent header via ServiceParams.
//
// Usage:
//
//	client, err := a2aclient.NewFromCard(ctx, card,
//	    a2aclient.WithCallInterceptors(trace.NewClientInterceptor(trace.ClientConfig{
//	        AgentID: "sg-architect",
//	    })),
//	)
func NewClientInterceptor(cfg ClientConfig) a2aclient.CallInterceptor {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &clientInterceptor{cfg: cfg}
}

type clientInterceptor struct {
	a2aclient.PassthroughInterceptor
	cfg ClientConfig
}

func (c *clientInterceptor) Before(ctx context.Context, req *a2aclient.Request) (context.Context, any, error) {
	parent := SpanFrom(ctx)

	var span *TraceSpan
	if parent != nil {
		span = NewChildSpan(parent, c.cfg.AgentID, "", "")
		c.cfg.Logger.DebugContext(ctx, "trace: child span propagated",
			"trace_id", span.TraceID,
			"span_id", span.SpanID,
			"parent_span_id", span.ParentSpanID,
		)
	} else {
		span = NewRootSpan(c.cfg.AgentID, "", "")
		c.cfg.Logger.DebugContext(ctx, "trace: root span propagated",
			"trace_id", span.TraceID,
			"span_id", span.SpanID,
		)
	}

	req.ServiceParams.Append(SvcParamTrace, span.EncodeTraceparent())
	return WithSpan(ctx, span), nil, nil
}

// ServerConfig configures the server-side trace interceptor.
type ServerConfig struct {
	// AgentID is the identifier injected into every span created by this server.
	// If empty, the interceptor is a no-op.
	AgentID string

	// Logger receives span creation events. If nil, slog.Default() is used.
	Logger *slog.Logger
}

// NewServerInterceptor returns a server interceptor that creates a new
// [TraceSpan] for every incoming A2A call. The span is attached to the context
// and can be retrieved by downstream interceptors or executors via [SpanFrom].
//
// The interceptor reads the W3C traceparent from ServiceParams and creates
// a child span linked to the caller's span.
//
// Usage:
//
//	handler := a2asrv.NewHandler(executor,
//	    a2asrv.WithCallInterceptors(trace.NewServerInterceptor(trace.ServerConfig{
//	        AgentID: "do-developer",
//	    })),
//	)
func NewServerInterceptor(cfg ServerConfig) a2asrv.CallInterceptor {
	if cfg.AgentID == "" {
		return a2asrv.PassthroughCallInterceptor{}
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &serverInterceptor{cfg: cfg}
}

type serverInterceptor struct {
	a2asrv.PassthroughCallInterceptor
	cfg ServerConfig
}

func (s *serverInterceptor) Before(ctx context.Context, callCtx *a2asrv.CallContext, req *a2asrv.Request) (context.Context, any, error) {
	taskID, contextID := extractTaskInfo(req.Payload)
	headerValue := s.readTraceHeader(callCtx)

	var span *TraceSpan
	if parent := ParseTraceHeader(headerValue); parent != nil {
		span = NewChildSpan(parent, s.cfg.AgentID, taskID, contextID)
	} else {
		span = NewRootSpan(s.cfg.AgentID, taskID, contextID)
	}
	s.cfg.Logger.DebugContext(ctx, "trace: span created",
		"trace_id", span.TraceID,
		"span_id", span.SpanID,
		"agent", span.AgentID,
	)
	return WithSpan(ctx, span), nil, nil
}

// readTraceHeader reads the trace context from the incoming call.
func (s *serverInterceptor) readTraceHeader(callCtx *a2asrv.CallContext) string {
	values, ok := callCtx.ServiceParams().Get(SvcParamTrace)
	if !ok || len(values) == 0 {
		return ""
	}
	return values[0]
}

func extractTaskInfo(payload any) (taskID, contextID string) {
	if provider, ok := payload.(a2a.TaskInfoProvider); ok {
		info := provider.TaskInfo()
		return string(info.TaskID), info.ContextID
	}
	return "", ""
}

// BridgeToMCP extracts the current trace context for injection into
// an MCP tools/call request's params._meta. Returns nil if no span
// is present in the context.
//
// Usage:
//
//	if tc := trace.BridgeToMCP(ctx); tc != nil {
//	    mcpReq.Params.Meta["traceparent"] = tc["traceparent"]
//	}
func BridgeToMCP(ctx context.Context) map[string]string {
	span := SpanFrom(ctx)
	if span == nil {
		return nil
	}
	return map[string]string{
		"traceparent": span.EncodeTraceparent(),
	}
}

// BridgeFromMCP creates a span from an MCP traceparent and attaches
// it to the context. Returns the new context with the span attached.
//
// Usage:
//
//	if tp, ok := mcpReq.Params.Meta["traceparent"]; ok {
//	    ctx = trace.BridgeFromMCP(ctx, tp.(string), "agent-id")
//	}
func BridgeFromMCP(ctx context.Context, traceparent, agentID string) context.Context {
	parent, err := ParseTraceparent(traceparent)
	if err != nil {
		// Malformed or absent traceparent — start a new root span
		span := NewRootSpan(agentID, "", "")
		return WithSpan(ctx, span)
	}
	span := NewChildSpan(parent, agentID, "", "")
	return WithSpan(ctx, span)
}
