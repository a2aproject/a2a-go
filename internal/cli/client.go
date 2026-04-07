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

func newClient(ctx context.Context, cfg *globalConfig, agentURL string) (*a2aclient.Client, error) {
	factoryOpts := clientFactoryOpts(cfg)

	if cfg.transport != "" {
		proto, err := parseTransport(cfg.transport)
		if err != nil {
			return nil, err
		}
		cfg.logf("connecting directly to %s via %s (skipping card resolution)", agentURL, cfg.transport)
		endpoint := a2a.NewAgentInterface(agentURL, proto)
		return a2aclient.NewFromEndpoints(ctx, []*a2a.AgentInterface{endpoint}, factoryOpts...)
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
	return a2aclient.NewFromCard(ctx, card, factoryOpts...)
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
