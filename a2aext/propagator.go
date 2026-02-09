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

// ClientPropagatorConfig configures the behavior of the client metadata propagator.
type ClientPropagatorConfig struct {
	// MetadataPredicate determines which payload metadata keys are propagated.
	// If not provided, metadata payload fields matching server-supported
	// extensions will be propagated.
	// If a Client was created from an AgentInterface, the logic will assume
	// that the server supports all extensions.
	MetadataPredicate func(ctx context.Context, card *a2a.AgentCard, key string) bool
	// HeaderPredicate determines which request headers will be propagated.
	// If not provided, A2A-Extensions header values matching server-supported
	// extensions will be propagated.
	// If a Client was created from an AgentInterface, the logic will assume
	// that the server supports all extensions.
	HeaderPredicate func(ctx context.Context, card *a2a.AgentCard, key string, val string) bool
}

// ServerPropagatorConfig configures the behavior of the metadata propagator.
type ServerPropagatorConfig struct {
	// MetadataPredicate determines which payload metadata keys are propagated.
	// If not provided, metadata payload fields matching client-requested extensions
	// will be propagated.
	MetadataPredicate func(ctx context.Context, key string) bool
	// HeaderPredicate determines which request headers will be propagated.
	// If not provided, A2A-Extensions header value will be propagated.
	HeaderPredicate func(ctx context.Context, key string) bool
}

// GetRequestHeaders returns the request headers attached as per [ServerPropagatorConfig].
// Returns nil context was not found.
func GetRequestHeaders(ctx context.Context) map[string][]string {
	val, ok := ctx.Value(propagatorCtxKeyType{}).(*propagatorContext)
	if !ok {
		return nil
	}
	return val.requestHeaders
}

// GetMetadata returns the metadata attached as per [ServerPropagatorConfig].
// Returns nil context was not found.
func GetMetadata(ctx context.Context) map[string]any {
	val, ok := ctx.Value(propagatorCtxKeyType{}).(*propagatorContext)
	if !ok {
		return nil
	}
	return val.metadata
}

// NewClientPropagator returns a client interceptor that propagates payload metada header values.
// The client interceptor needs to be set on a2aclient or client factory using [a2aclient.WithInterceptors] option.
func NewClientPropagator(config *ClientPropagatorConfig) a2aclient.CallInterceptor {
	var cfg ClientPropagatorConfig
	if config != nil {
		cfg = *config
	}
	if cfg.MetadataPredicate == nil {
		// Propagate all extensions supported by the server.
		cfg.MetadataPredicate = func(ctx context.Context, card *a2a.AgentCard, key string) bool {
			extensions, ok := a2asrv.ExtensionsFrom(ctx)
			if !ok {
				return false
			}
			if !slices.Contains(extensions.RequestedURIs(), key) {
				return false
			}
			return isExtensionSupported(card, key)
		}
	}
	if cfg.HeaderPredicate == nil {
		// Propagate requested extensions.
		cfg.HeaderPredicate = func(ctx context.Context, card *a2a.AgentCard, key string, val string) bool {
			if !strings.EqualFold(key, CallMetaKey) {
				return false
			}
			return isExtensionSupported(card, val)
		}
	}
	return &clientPropagator{ClientPropagatorConfig: cfg}
}

// NewServerPropagator returns a server interceptor that propagates payload metada header values.
// The server interceptor needs to be set on request handler using [a2asrv.WithCallInterceptors] option.
func NewServerPropagator(config *ServerPropagatorConfig) a2asrv.CallInterceptor {
	var cfg ServerPropagatorConfig
	if config != nil {
		cfg = *config
	}
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
	return &serverPropagator{ServerPropagatorConfig: cfg}
}

// serverPropagator implements [a2asrv.CallInterceptor].
type serverPropagator struct {
	a2asrv.PassthroughCallInterceptor
	ServerPropagatorConfig
}

// Before extracts valid keys from the incoming request and attaches them to the context
// so the client interceptor can find them later.
func (s *serverPropagator) Before(ctx context.Context, callCtx *a2asrv.CallContext, req *a2asrv.Request) (context.Context, any, error) {
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

	return context.WithValue(ctx, propagatorCtxKeyType{}, propagatorCtx), nil, nil
}

// clientPropagator implements [a2aclient.CallInterceptor].
type clientPropagator struct {
	a2aclient.PassthroughInterceptor
	ClientPropagatorConfig
}

// Before checks the context for propagated values and injects them into the outgoing request.
func (c *clientPropagator) Before(ctx context.Context, req *a2aclient.Request) (context.Context, any, error) {
	toPropagate, ok := ctx.Value(propagatorCtxKeyType{}).(*propagatorContext)
	if !ok {
		return ctx, nil, nil
	}

	if len(toPropagate.metadata) > 0 {
		if mc, ok := req.Payload.(a2a.MetadataCarrier); ok {
			for k, v := range toPropagate.metadata {
				if c.MetadataPredicate(ctx, req.Card, k) {
					mc.SetMeta(k, v)
				}
			}
		}
	}

	for headerName, headerValues := range toPropagate.requestHeaders {
		for _, headerValue := range headerValues {
			if !c.HeaderPredicate(ctx, req.Card, headerName, headerValue) {
				continue
			}
			req.Meta.Append(headerName, headerValue)
		}
	}

	return ctx, nil, nil
}
