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

package cli

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aclient/agentcard"
	"github.com/a2aproject/a2a-go/v2/a2acompat/a2av0"
	a2agrpcv0 "github.com/a2aproject/a2a-go/v2/a2agrpc/v0"
	a2agrpc "github.com/a2aproject/a2a-go/v2/a2agrpc/v1"
)

func newClient(ctx context.Context, cfg *globalConfig, agentURL string, extraOpts ...a2aclient.FactoryOption) (*a2aclient.Client, error) {
	factoryOpts := append(clientFactoryOpts(cfg), extraOpts...)

	if cfg.transport != "" {
		proto, err := parseTransport(cfg.transport)
		if err != nil {
			return nil, err
		}
		endpointURL := agentURL
		if proto == a2a.TransportProtocolGRPC {
			endpointURL = stripHTTPScheme(agentURL)
		}
		cfg.logf("connecting directly to %s via %s (skipping card resolution)", endpointURL, cfg.transport)
		endpoint := a2a.NewAgentInterface(endpointURL, proto)
		client, err := a2aclient.NewFromEndpoints(ctx, []*a2a.AgentInterface{endpoint}, factoryOpts...)
		return client, hintInsecure(err)
	}

	cfg.logf("resolving agent card from %s", agentURL)
	var resolveOpts []agentcard.ResolveOption
	if cfg.auth != "" {
		resolveOpts = append(resolveOpts, agentcard.WithRequestHeader("Authorization", cfg.auth))
	}
	card, err := agentcard.DefaultResolver.Resolve(ctx, agentURL, resolveOpts...)
	if err != nil {
		return nil, fmt.Errorf("resolving agent card: %w", err)
	}
	cfg.logf("creating client for %s", card.Name)
	client, err := a2aclient.NewFromCard(ctx, card, factoryOpts...)
	return client, hintInsecure(err)
}

// hintInsecure wraps gRPC "no transport security set" errors with a
// user-friendly suggestion to pass --insecure.
func hintInsecure(err error) error {
	if err != nil && strings.Contains(err.Error(), "no transport security set") {
		return fmt.Errorf("%w\n\nhint: pass --insecure to allow plaintext gRPC connections", err)
	}
	return err
}

func clientFactoryOpts(cfg *globalConfig) []a2aclient.FactoryOption {
	factoryOpts := []a2aclient.FactoryOption{
		a2av0.WithRESTTransport(a2av0.RESTTransportConfig{}),
		a2av0.WithJSONRPCTransport(a2av0.JSONRPCTransportConfig{}),
	}
	var grpcOpts []grpc.DialOption
	if cfg.insecureGRPC {
		grpcOpts = append(grpcOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}
	factoryOpts = append(factoryOpts,
		a2agrpcv0.WithGRPCTransport(grpcOpts...),
		a2agrpc.WithGRPCTransport(grpcOpts...),
	)
	return factoryOpts
}

func withServiceParams(ctx context.Context, cfg *globalConfig) context.Context {
	params := a2aclient.ServiceParams{}
	for _, kv := range cfg.svcParams {
		if k, v, ok := strings.Cut(kv, "="); ok {
			params.Append(k, v)
		}
	}
	if cfg.auth != "" {
		params.Append("Authorization", cfg.auth)
	}
	if len(params) > 0 {
		ctx = a2aclient.AttachServiceParams(ctx, params)
	}
	return ctx
}

// stripHTTPScheme converts an HTTP(S) URL to a bare host:port suitable for
// grpc.NewClient, which expects a target address without an HTTP scheme.
func stripHTTPScheme(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return raw
	}
	return u.Host
}

func parseTransport(s string) (a2a.TransportProtocol, error) {
	switch strings.ToLower(s) {
	case "rest":
		return a2a.TransportProtocolHTTPJSON, nil
	case "jsonrpc":
		return a2a.TransportProtocolJSONRPC, nil
	case "grpc":
		return a2a.TransportProtocolGRPC, nil
	default:
		return "", fmt.Errorf("unknown transport %q (use rest, jsonrpc, or grpc)", s)
	}
}
