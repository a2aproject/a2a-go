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

// Used to store CallMeta in context.Context after all the interceptors were applied.
type callMetaKey struct{}

// CallMeta holds things like auth headers, signatures etc.
// In jsonrpc it is passed as HTTP headers, in gRPC it becomes a part of context.Context.
// Custom protocol implementations can use CallMetaFrom to access this data and
// perform the operations necessary for attaching it to the request.
type CallMeta map[string][]string

// Request represents a transport-agnostic request to be sent to A2A server.
// Payload is one of a2a package core types.
type Request struct {
	method  string
	Meta    CallMeta
	Payload any
}

func (r *Request) Method() string {
	return r.method
}

// Response represents a transport-agnostic result received from A2A server.
// Payload is one of a2a package core types.
type Response struct {
	method  string
	Err     error
	Meta    CallMeta
	Payload any
}

func (r *Response) Method() string {
	return r.method
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

// CallMetaFrom allows Transport implementations to access CallMeta after all
// the interceptors were applied.
func CallMetaFrom(ctx context.Context) (CallMeta, bool) {
	meta, ok := ctx.Value(callMetaKey{}).(CallMeta)
	return meta, ok
}

func withCallMeta(ctx context.Context, meta CallMeta) context.Context {
	return context.WithValue(ctx, callMetaKey{}, meta)
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
