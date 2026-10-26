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

package a2aclient

import (
	"context"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aplugin/core"
)

// ClientPlugin is invoked around every call made by a [Client].
// A non-nil error from [ClientPlugin.BeforeClientRequest] cancels the call.
// See [github.com/a2aproject/a2a-go/v2/a2aplugin] for composing and registering plugins.
type ClientPlugin interface {
	core.Plugin

	// BeforeClientRequest is called before the request is sent. The returned
	// context is passed to AfterClientRequest.
	BeforeClientRequest(ctx context.Context, req *ClientRequest) (context.Context, error)

	// AfterClientRequest is called with the response. Streaming semantics match
	// those of server plugins: once per event, then once with IsFinal=true.
	AfterClientRequest(ctx context.Context, resp *ClientResponse) (context.Context, error)
}

// ClientRequest is passed to [ClientPlugin.BeforeClientRequest].
type ClientRequest struct {
	// Method is the A2A RPC name, matching the server side.
	Method string
	// BaseURL is the URL of the agent interface the client is connected to.
	BaseURL string
	// Payload is the request value, one of a2a package core types.
	Payload any
	// Card is the AgentCard of the remote agent. May be nil for URL-only clients.
	Card *a2a.AgentCard
	// ServiceParams holds the outgoing request metadata. Plugins may modify it.
	ServiceParams ServiceParams
}

// ClientResponse is passed to [ClientPlugin.AfterClientRequest].
type ClientResponse struct {
	// Method is the A2A RPC name, matching [ClientRequest.Method].
	Method string
	// BaseURL is the URL of the agent interface the client is connected to.
	BaseURL string
	// Payload is the response or stream event. It is nil when Err is non-nil or
	// on the final call that marks the end of a stream.
	Payload any
	// Card is the AgentCard of the remote agent, matching [ClientRequest.Card].
	Card *a2a.AgentCard
	// ServiceParams holds the response metadata.
	ServiceParams ServiceParams
	// Err is the error returned by the transport, if any.
	Err error
	// IsStreamEvent is true when Payload is a single event of a streaming response.
	IsStreamEvent bool
	// IsFinal is true for the last call of a request.
	IsFinal bool
}
