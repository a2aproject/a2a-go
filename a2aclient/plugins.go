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
	"fmt"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aplugin/core"
	"github.com/a2aproject/a2a-go/v2/internal/plugincall"
	"github.com/a2aproject/a2a-go/v2/log"
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

// WithPlugins sets the [ClientPlugin] whose hooks run around every call made by created [Client]s.
// If omitted, clients use the global plugin registered with
// [github.com/a2aproject/a2a-go/v2/a2aplugin.SetGlobal] at the time they are created.
// Passing nil is a no-op.
func WithPlugins(plugin ClientPlugin) FactoryOption {
	return factoryOptionFn(func(f *Factory) {
		if plugin == nil {
			return
		}
		f.plugin = plugin
	})
}

// beginPluginCall runs [ClientPlugin.BeforeClientRequest] and returns a handle used to
// emit per-event and final AfterClientRequest calls, together with the context and payload
// to continue the call with. Service params modified by plugins are attached to the returned
// context. A non-nil error means the call must be rejected without further plugin dispatch.
// If the client has no plugin hooks the returned handle is inactive and the inputs are
// returned unchanged.
func beginPluginCall[Req any](ctx context.Context, c *Client, method string, payload Req) (*plugincall.Call, context.Context, Req, error) {
	if !core.HasHooks(c.plugin, core.ClientHooks) {
		return nil, ctx, payload, nil
	}
	params := serviceParamsCloneFrom(ctx)
	params[a2a.SvcParamVersion] = []string{string(c.protocolVersion)}
	req := &ClientRequest{
		Method:        method,
		BaseURL:       c.endpoint.URL,
		Payload:       payload,
		Card:          c.card.Load(),
		ServiceParams: params,
	}
	newCtx, err := c.plugin.BeforeClientRequest(ctx, req)
	if err != nil {
		return nil, ctx, payload, err
	}
	newCtx = context.WithValue(newCtx, serviceParamsKeyType{}, req.ServiceParams)
	plugin := c.plugin
	hook := plugincall.New(func(payload any, err error, isStreamEvent bool) {
		resp := &ClientResponse{
			Method:        req.Method,
			BaseURL:       req.BaseURL,
			Payload:       payload,
			Card:          req.Card,
			ServiceParams: req.ServiceParams,
			Err:           err,
			IsStreamEvent: isStreamEvent,
			IsFinal:       !isStreamEvent,
		}
		if _, err := plugin.AfterClientRequest(newCtx, resp); err != nil {
			log.Error(newCtx, "plugin AfterClientRequest failed", err, "plugin", plugin.Name())
		}
	})
	typed, ok := req.Payload.(Req)
	if !ok {
		err := fmt.Errorf("payload type changed from %T to %T", payload, req.Payload)
		hook.Finish(nil, err)
		return nil, ctx, payload, err
	}
	return hook, newCtx, typed, nil
}
