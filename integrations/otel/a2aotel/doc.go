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

// Package a2aotel instruments A2A clients and servers with OpenTelemetry following the semantic
// conventions for the Agent2Agent protocol:
// https://github.com/open-telemetry/semantic-conventions-genai/blob/main/docs/gen-ai/a2a.md.
//
// The plugins report a span for every A2A call. Install them with the client and server options:
//
//	client, err := a2aclient.NewFromCard(ctx, card, a2aclient.WithPlugins(a2aotel.NewClientPlugin()))
//
//	handler := a2asrv.NewHandler(executor, a2asrv.WithPlugins(a2aotel.NewServerPlugin(a2aotel.WithAgentCard(card))))
//
// The plugins use the global tracer provider and propagators unless [WithTracerProvider] and [WithPropagators]
// are set. The client plugin sends the trace context in the call service parameters, which transports send as
// HTTP headers or gRPC metadata, and the server plugin uses it as the parent of the server span.
//
// Setting the OTEL_INSTRUMENTATION_A2A_SDK_ENABLED environment variable to "false" disables the plugins: they
// pass calls through without emitting telemetry or propagating the trace context. The variable is read when a
// plugin is created.
package a2aotel
