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
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

const (
	// SvcParamTrace is the ServiceParam key for trace propagation headers.
	// Values are formatted as: trace_id;span_id;parent_span_id;agent_id
	SvcParamTrace = "a2a-trace"
)

// ClientConfig configures the client-side trace interceptor.
type ClientConfig struct {
	// AgentID is the identifier injected into child spans when no parent span
	// exists. This handles the case where the client is the root caller.
	AgentID string

	// Logger receives span propagation events. If nil, slog.Default() is used.
	Logger *slog.Logger
}

// NewClientInterceptor returns a client interceptor that propagates trace
// context to outgoing A2A calls via ServiceParams headers. If a parent
// [TraceSpan] is present in the context (set by a [ServerInterceptor] on a
// prior inbound call), a child span is created and propagated. Otherwise,
// a new root span is created.
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
		c.cfg.Logger.DebugContext(ctx, "trace: child span propagated", "span", span.String())
	} else {
		span = NewRootSpan(c.cfg.AgentID, "", "")
		c.cfg.Logger.DebugContext(ctx, "trace: root span propagated", "span", span.String())
	}

	req.ServiceParams.Append(SvcParamTrace, span.Encode())
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
// If the incoming request carries a trace header (SvcParamTrace), the new span
// is created as a child, linking the caller's span as the parent. TaskID and
// ContextID are extracted from the request payload when available.
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
	s.cfg.Logger.DebugContext(ctx, "trace: span created", "span", span.String())
	return WithSpan(ctx, span), nil, nil
}

// readTraceHeader reads the SvcParamTrace value from the incoming call.
func (s *serverInterceptor) readTraceHeader(callCtx *a2asrv.CallContext) string {
	values, ok := callCtx.ServiceParams().Get(SvcParamTrace)
	if !ok || len(values) == 0 {
		return ""
	}
	return values[0]
}

// Encode serializes the span into a ServiceParams header value.
// Format: trace_id;span_id;parent_span_id;agent_id
func (s *TraceSpan) Encode() string {
	return strings.Join([]string{s.TraceID, s.SpanID, s.ParentSpanID, s.AgentID}, ";")
}

// ParseTraceHeader decodes a trace header value into a partial TraceSpan
// suitable as a parent reference. Returns nil if the header value is
// empty or has fewer than 3 semicolon-separated parts.
func ParseTraceHeader(val string) *TraceSpan {
	if val == "" {
		return nil
	}
	parts := strings.Split(val, ";")
	if len(parts) < 3 {
		return nil
	}
	return &TraceSpan{
		TraceID:      parts[0],
		SpanID:       parts[1],
		ParentSpanID: parts[2],
		AgentID:      optionalPart(parts, 3),
	}
}

func extractTaskInfo(payload any) (taskID, contextID string) {
	if provider, ok := payload.(a2a.TaskInfoProvider); ok {
		info := provider.TaskInfo()
		return string(info.TaskID), info.ContextID
	}
	return "", ""
}

func optionalPart(parts []string, idx int) string {
	if idx < len(parts) {
		return parts[idx]
	}
	return ""
}
