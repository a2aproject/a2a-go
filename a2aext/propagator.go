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

package a2aext

import (
	"context"
	"maps"
	"slices"
	"strings"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2aclient"
	"github.com/a2aproject/a2a-go/a2asrv"
)

// propagatorCtxKeyType is the context key used to pass values which need to be propagated.
type propagatorCtxKeyType struct{}

type propagatorContext struct {
	requestHeaders map[string][]string
	metadata       map[string]any
}

// PropagatorPredicate returns true if the given metadata key should be propagated.
type PropagatorPredicate func(ctx context.Context, key string) bool

// PropagatorConfig configures the behavior of the metadata propagator.
type PropagatorConfig struct {
	// MetadataPredicate determines which payload metadata keys are propagated.
	// If not provided requested A2A-Extensions vlaues fields will be propagated.
	MetadataPredicate PropagatorPredicate
	// HeaderPredicate determines which request headers will be propagated.
	// If not provided requested A2A-Extensions will be propagated.
	HeaderPredicate PropagatorPredicate
}

// NewPropagator returns a client and server interceptor pair configured to propagate payload metada header values.
// The server interceptor needs to be set on request handler using [a2asrv.WithCallInterceptor] option.
// The client interceptor needs to be set on a2aclient or client factory using [a2aclient.WithInterceptors] option.
func NewPropagator(cfg PropagatorConfig) (a2aclient.CallInterceptor, a2asrv.CallInterceptor) {
	if cfg.MetadataPredicate == nil {
		// Propagate all extension-added metadata keys.
		cfg.MetadataPredicate = func(ctx context.Context, key string) bool {
			if extensions, ok := a2asrv.ExtensionsFrom(ctx); ok {
				return slices.Contains(extensions.RequestedURIs(), key)
			}
			return false
		}
	}
	if cfg.HeaderPredicate == nil {
		// Propagate requested extensions.
		cfg.HeaderPredicate = func(ctx context.Context, key string) bool {
			return strings.EqualFold(key, CallMetaKey)
		}
	}
	return &clientPropagator{PropagatorConfig: cfg}, &serverPropagator{PropagatorConfig: cfg}
}

// serverPropagator implements [a2asrv.CallInterceptor].
type serverPropagator struct {
	a2asrv.PassthroughCallInterceptor
	PropagatorConfig
}

// Before extracts valid keys from the incoming request and attaches them to the context
// so the client interceptor can find them later.
func (s *serverPropagator) Before(ctx context.Context, callCtx *a2asrv.CallContext, req *a2asrv.Request) (context.Context, error) {
	propagatorCtx := &propagatorContext{
		metadata:       make(map[string]any),
		requestHeaders: make(map[string][]string),
	}

	if mc, ok := req.Payload.(a2a.MetadataCarrier); ok {
		meta := mc.Meta()
		for k, v := range meta {
			if s.MetadataPredicate(ctx, k) {
				propagatorCtx.metadata[k] = v
			}
		}
	}

	for headerName, headerValues := range callCtx.RequestMeta().List() {
		if s.HeaderPredicate(ctx, headerName) {
			propagatorCtx.requestHeaders[headerName] = headerValues
		}
	}

	return context.WithValue(ctx, propagatorCtxKeyType{}, propagatorCtx), nil
}

// clientPropagator implements [a2aclient.CallInterceptor].
type clientPropagator struct {
	a2aclient.PassthroughInterceptor
	PropagatorConfig
}

// Before checks the context for propagated values and injects them into the outgoing request.
func (c *clientPropagator) Before(ctx context.Context, req *a2aclient.Request) (context.Context, error) {
	toPropagate, ok := ctx.Value(propagatorCtxKeyType{}).(*propagatorContext)
	if !ok {
		return ctx, nil
	}

	if len(toPropagate.metadata) > 0 {
		if mc, ok := req.Payload.(a2a.MetadataCarrier); ok {
			meta := mc.Meta()
			if meta == nil {
				meta = make(map[string]any)
				mc.SetMeta(meta)
			}
			maps.Copy(meta, toPropagate.metadata)
		}
	}

	for headerName, headerValues := range toPropagate.requestHeaders {
		for _, headerValue := range headerValues {
			if req.Meta == nil {
				req.Meta = make(map[string][]string)
			}
			values := req.Meta[headerName]
			if !slices.Contains(values, headerValue) {
				values = append(values, headerValue)
			}
			req.Meta[headerName] = values
		}
	}

	return ctx, nil
}
