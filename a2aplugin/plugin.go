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

package a2aplugin

import (
	"context"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

// Plugin is the common base of every plugin. Concrete plugins additionally
// implement one or more of the hook interfaces defined in this package.
type Plugin interface {
	// Name returns a short identifier used in logs and metrics.
	Name() string
}

// ServerPlugin is invoked around every call processed by a [a2asrv.RequestHandler].
// A non-nil error from [ServerPlugin.BeforeHandlerRun] rejects the call.
type ServerPlugin interface {
	Plugin

	// BeforeHandlerRun is called before the handler processes a request. The
	// returned context is passed to AfterHandlerRun.
	BeforeHandlerRun(ctx context.Context, req *ServerRequest) (context.Context, error)

	// AfterHandlerRun is called with the handler result. For streaming calls it
	// runs once per emitted event with IsStreamEvent=true, IsFinal=false, and
	// once more with IsFinal=true after the stream ends.
	AfterHandlerRun(ctx context.Context, resp *ServerResponse) (context.Context, error)
}

// ClientPlugin is invoked around every call made by an [a2aclient.Client].
// A non-nil error from [ClientPlugin.BeforeClientRequest] cancels the call.
type ClientPlugin interface {
	Plugin

	// BeforeClientRequest is called before the request is sent. The returned
	// context is passed to AfterClientRequest.
	BeforeClientRequest(ctx context.Context, req *ClientRequest) (context.Context, error)

	// AfterClientRequest is called with the response. Streaming semantics match
	// [ServerPlugin.AfterHandlerRun].
	AfterClientRequest(ctx context.Context, resp *ClientResponse) (context.Context, error)
}

// CompletePlugin is a plugin that implements both the client and the server hooks.
type CompletePlugin interface {
	ClientPlugin
	ServerPlugin
}

// ServerRequest is passed to [ServerPlugin.BeforeHandlerRun].
type ServerRequest struct {
	// Method is the A2A RPC name of the invoked handler method, e.g. "SendMessage".
	Method string
	// Payload is the request value. It is nil for methods without parameters,
	// otherwise one of a2a package core types.
	Payload any
	// CallContext holds the server-side request metadata.
	CallContext *a2asrv.CallContext
	// AgentCard is the agent card served by this handler, if available.
	AgentCard *a2a.AgentCard
}

// ServerResponse is passed to [ServerPlugin.AfterHandlerRun].
type ServerResponse struct {
	// Method is the A2A RPC name, matching [ServerRequest.Method].
	Method string
	// Payload is the handler result or stream event. It is nil when Err is non-nil
	// or on the final call that marks the end of a stream.
	Payload any
	// CallContext is the context passed to [ServerPlugin.BeforeHandlerRun].
	CallContext *a2asrv.CallContext
	// Err is the error returned by the handler, if any.
	Err error
	// IsStreamEvent is true when Payload is a single event of a streaming response.
	IsStreamEvent bool
	// IsFinal is true for the last call of a request. For unary calls it is
	// always true; for streaming calls it is true only for the trailing invocation.
	IsFinal bool
}

// ClientRequest is passed to [ClientPlugin.BeforeClientRequest].
type ClientRequest struct {
	// Method is the A2A RPC name, matching the server side.
	Method string
	// BaseURL is the URL of the agent interface the client is connected to.
	BaseURL string
	// ServiceParams holds outgoing request metadata. Plugins may modify it.
	ServiceParams a2aclient.ServiceParams
	// Card is the AgentCard of the remote agent. May be nil for URL-only clients.
	Card *a2a.AgentCard
	// Payload is the request value, one of a2a package core types.
	Payload any
}

// ClientResponse is passed to [ClientPlugin.AfterClientRequest].
type ClientResponse struct {
	// Method is the A2A RPC name, matching [ClientRequest.Method].
	Method string
	// BaseURL is the URL of the agent interface the client is connected to.
	BaseURL string
	// ServiceParams holds the response metadata.
	ServiceParams a2aclient.ServiceParams
	// Card is the AgentCard of the remote agent, matching [ClientRequest.Card].
	Card *a2a.AgentCard
	// Payload is the response or stream event. It is nil when Err is non-nil or
	// on the final call that marks the end of a stream.
	Payload any
	// Err is the error returned by the transport, if any.
	Err error
	// IsStreamEvent and IsFinal follow the same rules as the matching
	// [ServerResponse] fields.
	IsStreamEvent bool
	IsFinal       bool
}
