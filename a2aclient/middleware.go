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

// Used to store CallMeta in context.Context after all the interceptors were applied.
type callMetaKey struct{}

// CallMeta holds things like auth headers, signatures etc.
// In jsonrpc it is passed as HTTP headers, in gRPC becomes a part of context.Context.
// Custom protocol implementations can use CallMetaFrom to access this data and
// perform the operations necessary for attaching it to the request.
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

// CallMetaFrom allows Transport implementations to access CallMeta after all
// the interceptors were applied.
func CallMetaFrom(ctx context.Context) (CallMeta, bool) {
	meta, ok := ctx.Value(callMetaKey{}).(CallMeta)
	return meta, ok
}

// WithCallMeta allows CallInterceptors to attach information like auth credentials
// to a request. Transport implementations will attach these to the call in
// a transport-specific way (eg. headers in HTTP).
func WithCallMeta(ctx context.Context, meta CallMeta) context.Context {
	return context.WithValue(ctx, callMetaKey{}, meta)
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

	return context.WithValue(ctx, callContextKey{}, CallContext{SessionID: sid})
}

func withMethod(ctx context.Context, method string) context.Context {
	if callCtx, ok := CallContextFrom(ctx); ok {
		callCtx.Method = method
		return context.WithValue(ctx, callContextKey{}, callCtx)
	}

	return context.WithValue(ctx, callContextKey{}, CallContext{Method: method})
}

// PassthroughInterceptor can be used by CallInterceptor implementers who don't need all methods.
// The struct can be embedded for providing a no-op implementation.
type PassthroughInterceptor struct{}

func (PassthroughInterceptor) Before(ctx context.Context, req *Request) (context.Context, error) {
	return ctx, nil
}

func (PassthroughInterceptor) After(ctx context.Context, resp *Response) error {
	return nil
}
