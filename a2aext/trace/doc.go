// Copyright 2026 The A2A Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package trace provides an audit trail for A2A task execution chains.
//
// When Agent A delegates to Agent B, trace attaches a span to the outgoing call
// and records a span on the incoming side. The result is a causal chain of
// [TraceSpan] values — who called whom, for what task, at what time — that
// can be collected and queried for post-hoc audit.
//
// # Design
//
// A [ServerInterceptor] (on the callee) creates a new span for every incoming A2A
// call, stashing it in the context. A [ClientInterceptor] (on the caller) reads
// the span from the context and attaches it as the parent to any outgoing calls.
//
// Multiple hops accumulate a chain:
//
//	Agent A  ──call──►  Agent B  ──call──►  Agent C
//	span₀               span₁(parent=₀)      span₂(parent=₁)
//
// # Relationship to a2aext propagator
//
// The a2aext propagator carries extension metadata; trace carries task-level
// provenance. They are complementary and can be combined in the same interceptor
// chain. The propagator's context key is distinct, so no interference occurs.
package trace
