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

package a2asrv

import "time"

// WithTransportKeepAlive enables SSE keep-alive messages at the specified interval.
// Keep-alive messages prevent API gateways from dropping idle connections.
// If interval is 0 or negative, keep-alive is disabled (default behavior).
func WithTransportKeepAlive(interval time.Duration) TransportOption {
	return func(c *TransportConfig) {
		c.KeepAliveInterval = interval
	}
}

// WithTransportPanicHandler sets a custom panic handler for the transport bindings.
// This gives the ability to recovery from panic by returning an error to the client.
func WithTransportPanicHandler(handler func(r any) error) TransportOption {
	return func(h *TransportConfig) {
		h.PanicHandler = handler
	}
}

// TransportConfig holds the configuration for transport bindings.
type TransportConfig struct {
	KeepAliveInterval time.Duration
	PanicHandler      func(r any) error
}

// TransportOption is a functional option for configuring protocol binding implementations.
type TransportOption func(*TransportConfig)
