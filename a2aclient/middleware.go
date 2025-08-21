// Copyright 2025 The A2A Authors
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
)

// Used to store a CallContext in context.Context.
type callContextKey struct{}

// CallMeta holds things like auth headers, signatures etc.
// In jsonrpc it is passed as HTTP headers, in gRPC becomes a part of context.Context.
type CallMeta map[string]string

// Request represents a transport-agnostic request to be sent to A2A server.
// Payload is one of a2a package core types.
type Request struct {
	Meta    CallMeta
	Payload any
}

// Response represents a transport-agnostic result received from A2A server.
// Payload is one of a2a package core types.
type Response struct {
	Err     error
	Meta    CallMeta
	Payload any
}

// CallInterceptor can be attached to an a2aclient.Client.
// If multiple interceptors are added:
//   - Before will be executed in the order of attachment sequentially.
//   - After will be executed in the reverse order sequentially.
type CallInterceptor interface {
	// Before allows to observe, modify or reject a Request.
	// A new context.Context can be returned to pass information to After.
	Before(ctx context.Context, req *Request) (context.Context, error)

	// After allows to observe, modify or reject a Response.
	After(ctx context.Context, resp *Response) error
}

// CallContext holds additional information about the intercepted request.
type CallContext struct {
	Method    string
	SessionID SessionID
}

// CallContextFrom allows CallInterceptors to get additional information about the intercepted request.
func CallContextFrom(ctx context.Context) (CallContext, bool) {
	callCtx, ok := ctx.Value(callContextKey{}).(CallContext)
	return callCtx, ok
}

// WithSessionID allows callers to attach a random session identifier to the request.
// CallInterceptor can access this identifier through CallContext.
func WithSessionID(ctx context.Context, sid SessionID) context.Context {
	if callCtx, ok := CallContextFrom(ctx); ok {
		callCtx.SessionID = sid
		return context.WithValue(ctx, callContextKey{}, callCtx)
	}

	callCtx := CallContext{SessionID: sid}
	return context.WithValue(ctx, callContextKey{}, callCtx)
}

// PassthroughCallInterceptor can be used by CallInterceptor implementers who don't need all methods.
// The struct can be embedded for providing a no-op implementation.
type PassthroughCallInterceptor struct{}

func (PassthroughCallInterceptor) Before(ctx context.Context, req *Request) (context.Context, error) {
	return ctx, nil
}

func (PassthroughCallInterceptor) After(ctx context.Context, resp *Response) error {
	return nil
}
