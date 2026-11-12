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
	"iter"
	"slices"

	"github.com/a2aproject/a2a-go/v2/a2aclient"
)

// ClientHooks are the hooks of a client plugin. All fields are optional.
type ClientHooks struct {
	// BeforeCall runs before every call and can modify the request. The returned context is passed to
	// the call and to AfterCall. If it returns a result or an error, the call is not sent and the value
	// is returned to the caller.
	BeforeCall func(ctx context.Context, req *a2aclient.Request) (context.Context, any, error)

	// AfterCall runs on every response value: once for a unary call, once per event for a streaming call.
	// It can modify the response. If it returns an error, the error is returned instead of the value.
	AfterCall func(ctx context.Context, resp *a2aclient.Response) error

	// WrapCall wraps every call outside the call hooks.
	WrapCall func(next a2aclient.CallHandler) a2aclient.CallHandler
}

// Client returns an [a2aclient.Plugin] which runs the hooks.
func Client(name string, hooks ClientHooks) a2aclient.Plugin {
	return &clientPlugin{name: name, hooks: hooks}
}

type clientPlugin struct {
	a2aclient.PassthroughPlugin
	name  string
	hooks ClientHooks
}

func (p *clientPlugin) Name() string {
	return p.name
}

func (p *clientPlugin) WrapCall(next a2aclient.CallHandler) a2aclient.CallHandler {
	if p.hooks.BeforeCall != nil || p.hooks.AfterCall != nil {
		next = p.hookCall(next)
	}
	if p.hooks.WrapCall != nil {
		next = p.hooks.WrapCall(next)
	}
	return next
}

func (p *clientPlugin) hookCall(next a2aclient.CallHandler) a2aclient.CallHandler {
	return func(ctx context.Context, req a2aclient.Request) iter.Seq2[any, error] {
		return func(yield func(any, error) bool) {
			ctx, req := ctx, req
			// BeforeCall can modify the service params, the caller's map must stay unchanged for retries.
			params := make(a2aclient.ServiceParams, len(req.ServiceParams))
			for k, v := range req.ServiceParams {
				params[k] = slices.Clone(v)
			}
			req.ServiceParams = params
			if p.hooks.BeforeCall != nil {
				hookCtx, result, err := p.hooks.BeforeCall(ctx, &req)
				ctx = hookCtx
				if err != nil || result != nil {
					yield(p.afterCall(ctx, req, result, err))
					return
				}
			}
			for payload, err := range next(ctx, req) {
				if !yield(p.afterCall(ctx, req, payload, err)) {
					return
				}
			}
		}
	}
}

func (p *clientPlugin) afterCall(ctx context.Context, req a2aclient.Request, payload any, err error) (any, error) {
	if p.hooks.AfterCall == nil {
		return payload, err
	}
	resp := &a2aclient.Response{
		Method:        req.Method,
		BaseURL:       req.BaseURL,
		ServiceParams: req.ServiceParams,
		Card:          req.Card,
		Payload:       payload,
		Err:           err,
	}
	if err := p.hooks.AfterCall(ctx, resp); err != nil {
		return nil, err
	}
	return resp.Payload, resp.Err
}
