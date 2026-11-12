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

	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

// Server returns an [a2asrv.Plugin] which runs the hooks. Execution hooks run for agent executions,
// not for cancelations.
func Server(name string, hooks ServerHooks) a2asrv.Plugin {
	return &serverPlugin{name: name, hooks: hooks}
}

type serverPlugin struct {
	a2asrv.PassthroughPlugin
	name  string
	hooks ServerHooks
}

func (p *serverPlugin) Name() string {
	return p.name
}

func (p *serverPlugin) WrapCall(next a2asrv.CallHandler) a2asrv.CallHandler {
	if p.hooks.BeforeCall != nil || p.hooks.AfterCall != nil {
		next = p.hookCall(next)
	}
	if p.hooks.WrapCall != nil {
		next = p.hooks.WrapCall(next)
	}
	return next
}

func (p *serverPlugin) hookCall(next a2asrv.CallHandler) a2asrv.CallHandler {
	return func(ctx context.Context, callCtx *a2asrv.CallContext, req a2asrv.Request) iter.Seq2[any, error] {
		return func(yield func(any, error) bool) {
			ctx, req := ctx, req
			if p.hooks.BeforeCall != nil {
				hookCtx, result, err := p.hooks.BeforeCall(ctx, callCtx, &req)
				if err != nil || result != nil {
					yield(p.afterCall(ctx, callCtx, result, err))
					return
				}
				ctx = hookCtx
			}
			for payload, err := range next(ctx, callCtx, req) {
				if !yield(p.afterCall(ctx, callCtx, payload, err)) {
					return
				}
			}
		}
	}
}

func (p *serverPlugin) afterCall(ctx context.Context, callCtx *a2asrv.CallContext, payload any, err error) (any, error) {
	if p.hooks.AfterCall == nil {
		return payload, err
	}
	resp := &a2asrv.Response{Payload: payload, Err: err}
	if err := p.hooks.AfterCall(ctx, callCtx, resp); err != nil {
		return nil, err
	}
	return resp.Payload, resp.Err
}
