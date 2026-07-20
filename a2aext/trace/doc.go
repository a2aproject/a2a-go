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

// Package trace provides a W3C Trace Context audit trail for A2A task chains.
//
// When Agent A delegates to Agent B, trace attaches a span to the outgoing
// call and records a span on the incoming side. The result is a causal chain
// that can be collected and queried for post-hoc audit.
//
// # Design
//
// A [ServerInterceptor] (callee) creates a new span for every inbound A2A
// call. A [ClientInterceptor] (caller) reads the current span and propagates
// it as the parent to outgoing calls via the W3C traceparent format.
//
// Multiple hops form a linked chain:
//
//	Agent A  ──call──►  Agent B  ──call──►  Agent C
//	span₀               span₁(parent=₀)      span₂(parent=₁)
//
// # W3C Trace Context
//
// Trace context is encoded as a standard W3C traceparent header
// (https://www.w3.org/TR/trace-context/):
//
//	version-trace_id-span_id-flags
//
// Example: 00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01
//
// This enables interoperability with OpenTelemetry, MCP trace context,
// and any W3C-compliant observability stack.
//
// # Cross-Protocol Bridge
//
// The package provides [BridgeToMCP] and [BridgeFromMCP] hooks for
// propagating trace context between A2A and MCP tool calls. See the
// cross-protocol bridge spec at deeparchi-ai/macs for details.
//
// # Relationship to a2aext propagator
//
// The a2aext propagator carries extension metadata; trace carries task-level
// provenance. They share complementary roles and can coexist without
// interference.
package trace
