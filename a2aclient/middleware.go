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
	"slices"
	"strings"

	"github.com/a2aproject/a2a-go/a2a"
)

// ServiceParams holds things like auth headers and signatures.
// In jsonrpc it is passed as HTTP headers, in gRPC it becomes a part of [context.Context].
// Custom protocol implementations can use [ServiceParams] to access this data and
// perform the operations necessary for attaching it to the request.
type ServiceParams map[string][]string

// Get performs case-insensitive lookup or the provided key. Returns nil if value is not present.
func (m ServiceParams) Get(key string) []string {
	val := m[strings.ToLower(key)]
	return val
}

// Append appends the provided values to the list of values associated with the key.
// Duplicates values will not be added. Key matching is case-insensitive.
func (m ServiceParams) Append(key string, vals ...string) {
	result := m.Get(key)
	for _, v := range vals {
		if slices.Contains(result, v) {
			continue
		}
		result = append(result, v)
	}
	m[strings.ToLower(key)] = result
}

// Request represents a transport-agnostic request to be sent to A2A server.
type Request struct {
	// Method is the name of the method invoked on the A2A-server.
	Method string
	// BaseURL is the URL of the agent interface to which the Client is connected.
	BaseURL string
	// ServiceParams holds horizontally applicable context or parameters with case-insensitive keys.
	// The transmission mechanism for these service parameter key-value pairs is defined by the specific protocol binding
	ServiceParams ServiceParams
	// Card is the AgentCard of the agent the client is connected to. Might be nil if Client was
	// created directly from server URL and extended AgentCard was never fetched.
	Card *a2a.AgentCard
	// Payload is the request payload. It is nil if the method does not take any parameters. Otherwise, it is one of a2a package core types otherwise.
	Payload any
}

// Response represents a transport-agnostic result received from A2A server.
type Response struct {
	// Method is the name of the method invoked on the A2A-server.
	Method string
	// BaseURL is the URL of the agent interface to which the Client is connected.
	BaseURL string
	// Err is the error response. It is nil for successful invocations.
	Err error
	// ServiceParams holds horizontally applicable context or parameters with case-insensitive keys.
	// The transmission mechanism for these service parameter key-value pairs is defined by the specific protocol binding
	ServiceParams ServiceParams
	// Card is the AgentCard of the agent the client is connected to. Might be nil if Client was
	// created directly from server URL and extended AgentCard was never fetched.
	Card *a2a.AgentCard
	// Payload is the response. It is nil if method doesn't return anything or Err was returned. Otherwise, it is one of a2a package core types otherwise.
	Payload any
}

// CallInterceptor can be attached to an [Client].
// If multiple interceptors are added:
//   - Before will be executed in the order of attachment sequentially.
//   - After will be executed in the reverse order sequentially.
type CallInterceptor interface {
	// Before allows to observe, modify or reject a Request.
	// A new context.Context can be returned to pass information to After.
	// If either the result (2nd return value) or the error (3rd return value) is non nil,
	// the network request will not be made and the value will be returned to the caller.
	Before(ctx context.Context, req *Request) (context.Context, any, error)

	// After allows to observe, modify or reject a Response.
	After(ctx context.Context, resp *Response) error
}

// NewServiceParamsInjector creates a [CallInterceptor] which attaches the provided meta to all requests.
func NewServiceParamsInjector(params ServiceParams) CallInterceptor {
	return &serviceParamsInjector{inject: params}
}

type serviceParamsInjector struct {
	PassthroughInterceptor
	inject ServiceParams
}

func (mi *serviceParamsInjector) Before(ctx context.Context, req *Request) (context.Context, any, error) {
	for k, values := range mi.inject {
		req.ServiceParams.Append(k, values...)
	}
	return ctx, nil, nil
}

// PassthroughInterceptor can be used by CallInterceptor implementers who don't need all methods.
// The struct can be embedded for providing a no-op implementation.
type PassthroughInterceptor struct{}

var _ CallInterceptor = (*PassthroughInterceptor)(nil)

func (PassthroughInterceptor) Before(ctx context.Context, req *Request) (context.Context, any, error) {
	return ctx, nil, nil
}

func (PassthroughInterceptor) After(ctx context.Context, resp *Response) error {
	return nil
}
