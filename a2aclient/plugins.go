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
		f.plugins = plugin
	})
}

// clientHooksReporter is implemented by plugin managers which can report
// that none of the registered plugins implement client hooks.
type clientHooksReporter interface {
	HasClientHooks() bool
}

// pluginCall carries the per-call plugin state across the Before and After
// (or stream-end) hooks of a single request.
type pluginCall struct {
	active    bool
	plugin    ClientPlugin
	ctx       context.Context
	req       *ClientRequest
	streamErr error
	finaled   bool
}

// beginPluginCall runs [ClientPlugin.BeforeClientRequest] and returns a handle used to
// emit per-event and final AfterClientRequest calls, together with the context and payload
// to continue the call with. Service params modified by plugins are attached to the returned
// context. A non-nil error means the call must be rejected without further plugin dispatch.
// If the client has no plugin hooks the returned handle is inactive and the inputs are
// returned unchanged.
func beginPluginCall[Req any](ctx context.Context, c *Client, method string, payload Req) (*pluginCall, context.Context, Req, error) {
	if !core.HasHooks(c.plugins, clientHooksReporter.HasClientHooks) {
		return &pluginCall{}, ctx, payload, nil
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
	newCtx, err := c.plugins.BeforeClientRequest(ctx, req)
	if err != nil {
		return &pluginCall{}, ctx, payload, err
	}
	newCtx = context.WithValue(newCtx, serviceParamsKeyType{}, req.ServiceParams)
	hook := &pluginCall{active: true, plugin: c.plugins, ctx: newCtx, req: req}
	typed, ok := req.Payload.(Req)
	if !ok {
		err := fmt.Errorf("payload type changed from %T to %T", payload, req.Payload)
		hook.finish(nil, err)
		return &pluginCall{}, ctx, payload, err
	}
	return hook, newCtx, typed, nil
}

// finish fires the final [ClientPlugin.AfterClientRequest] call at most once.
func (p *pluginCall) finish(payload any, err error) {
	if !p.active || p.finaled {
		return
	}
	p.finaled = true
	resp := p.newResponse(err)
	resp.IsFinal = true
	if err == nil {
		resp.Payload = payload
	}
	p.after(resp)
}

// finishStream fires the final [ClientPlugin.AfterClientRequest] call of a stream with
// the last error yielded through the function returned by wrapStreamYield.
func (p *pluginCall) finishStream() {
	p.finish(nil, p.streamErr)
}

// wrapStreamYield returns a yield function that fires a per-event
// [ClientPlugin.AfterClientRequest] call before each yield, recording any yielded
// error for finishStream.
func (p *pluginCall) wrapStreamYield(yield func(a2a.Event, error) bool) func(a2a.Event, error) bool {
	if !p.active {
		return yield
	}
	return func(ev a2a.Event, err error) bool {
		resp := p.newResponse(err)
		resp.IsStreamEvent = true
		if err != nil {
			p.streamErr = err
		} else {
			resp.Payload = ev
		}
		p.after(resp)
		return yield(ev, err)
	}
}

func (p *pluginCall) newResponse(err error) *ClientResponse {
	return &ClientResponse{
		Method:        p.req.Method,
		BaseURL:       p.req.BaseURL,
		Card:          p.req.Card,
		ServiceParams: p.req.ServiceParams,
		Err:           err,
	}
}

func (p *pluginCall) after(resp *ClientResponse) {
	if _, err := p.plugin.AfterClientRequest(p.ctx, resp); err != nil {
		log.Error(p.ctx, "plugin AfterClientRequest failed", err, "plugin", p.plugin.Name())
	}
}
