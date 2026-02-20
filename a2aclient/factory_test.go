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
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/a2aproject/a2a-go/a2a"
)

func parseProtocol(in string) (a2a.TransportProtocol, a2a.ProtocolVersion) {
	if prefix, suffix, ok := strings.Cut(in, ":"); ok {
		return a2a.TransportProtocol(prefix), a2a.ProtocolVersion(suffix)
	}
	return a2a.TransportProtocol(in), a2a.Version
}

func makeProtocols(in []string) []a2a.TransportProtocol {
	out := make([]a2a.TransportProtocol, len(in))
	for i, protocol := range in {
		out[i] = a2a.TransportProtocol(protocol)
	}
	return out
}

func makeEndpoints(protocols []string) []*a2a.AgentInterface {
	out := make([]*a2a.AgentInterface, len(protocols))
	for i, p := range protocols {
		protocol, version := parseProtocol(p)
		out[i] = a2a.NewAgentInterface("https://agent.com", protocol)
		out[i].ProtocolVersion = version
	}
	return out
}

func TestFactory_WithAdditionalOptions(t *testing.T) {
	f1 := NewFactory(WithConfig(Config{AcceptedOutputModes: []string{"application/json"}}))
	f2 := WithAdditionalOptions(f1, WithCallInterceptors(PassthroughInterceptor{}))

	if !reflect.DeepEqual(f1.config, f2.config) {
		t.Fatalf("WithAdditionalOptions() factory.config = %v, want %v", f2.config, f1.config)
	}
	if len(f2.interceptors) != 1 {
		t.Fatalf("WithAdditionalOptions() len(factory.interceptors) = %d, want 1", len(f2.interceptors))
	}
	if len(f1.interceptors) != 0 {
		t.Fatalf("WithAdditionalOptions() modified an argument: len(f.interceptors) = %d interceptors, want 0", len(f1.interceptors))
	}
}

func TestFactory_WithDefaultsDisabled(t *testing.T) {
	f1 := NewFactory()
	f2 := NewFactory(WithDefaultsDisabled())

	if len(f1.transports) == 0 {
		t.Fatal("want at least one transport to be registered by default")
	}
	if len(f2.transports) > 0 {
		t.Fatal("want no transports registered with disabled defaults")
	}
}

func TestFactory_TransportSelection(t *testing.T) {
	ctx := t.Context()
	testCases := []struct {
		name              string
		serverSupports    []string // protocols advertised by the server
		clientSupports    []string // list of registered transport factories
		clientPrefers     []string // Config.PreferredTransports
		connectFails      []string // specifies which transports fail to connect, used to test fallback logic
		wantClientVersion a2a.ProtocolVersion
		wantInterface     string
		wantErr           bool
	}{
		{
			name:           "client supports fewer protocols",
			serverSupports: []string{"jsonrpc", "grpc"},
			clientSupports: []string{"grpc"},
			wantInterface:  "grpc",
		},
		{
			name:           "server supports fewer protocols",
			serverSupports: []string{"jsonrpc"},
			clientSupports: []string{"grpc", "jsonrpc"},
			wantInterface:  "jsonrpc",
		},
		{
			name:           "default to server preference order",
			serverSupports: []string{"jsonrpc", "grpc"},
			clientSupports: []string{"jsonrpc", "grpc"},
			wantInterface:  "jsonrpc",
		},
		{
			name:           "client preferences override server preferences",
			serverSupports: []string{"jsonrpc", "grpc"},
			clientSupports: []string{"jsonrpc", "grpc"},
			clientPrefers:  []string{"grpc", "jsonrpc"},
			wantInterface:  "grpc",
		},
		{
			name:           "client preferences as a subset of supported protocols",
			serverSupports: []string{"grpc", "jsonrpc", "stubby"},
			clientSupports: []string{"grpc", "stubby", "jsonrpc"},
			clientPrefers:  []string{"stubby"},
			wantInterface:  "stubby",
		},
		{
			name:           "selects the first working protocol",
			serverSupports: []string{"grpc", "jsonrpc", "stubby"},
			clientSupports: []string{"grpc", "stubby", "jsonrpc"},
			connectFails:   []string{"grpc", "jsonrpc"},
			wantInterface:  "stubby",
		},
		{
			name:           "all transports fail",
			serverSupports: []string{"grpc", "jsonrpc"},
			clientSupports: []string{"grpc", "jsonrpc"},
			connectFails:   []string{"grpc", "jsonrpc"},
			wantErr:        true,
		},
		{
			name:           "no protocols in common",
			serverSupports: []string{"jsonrpc", "grpc"},
			clientSupports: []string{"stubby", "http+json"},
			wantErr:        true,
		},
		{
			name:           "negotiates highest common protocol version",
			serverSupports: []string{"jsonrpc:1.0", "jsonrpc:2.0"},
			clientSupports: []string{"jsonrpc:1.0", "jsonrpc:2.0"},
			clientPrefers:  []string{"jsonrpc"},
			wantInterface:  "jsonrpc:2.0",
		},
		{
			name:           "client preference only applies within same version",
			serverSupports: []string{"jsonrpc:1.0", "grpc:0.3"},
			clientSupports: []string{"jsonrpc:1.0", "grpc:0.3"},
			clientPrefers:  []string{"grpc"},
			wantInterface:  "jsonrpc:1.0",
		},
		{
			name:           "newer protocol version is preferred",
			serverSupports: []string{"grpc:0.3", "jsonrpc:1.0"},
			clientSupports: []string{"grpc:0.3", "jsonrpc:1.0"},
			wantInterface:  "jsonrpc:1.0",
		},
		{
			name:           "client transports not configured",
			serverSupports: []string{"grpc"},
			wantErr:        true,
		},
		{
			name:              "compatible same major version (server older)",
			serverSupports:    []string{"jsonrpc:1.0"},
			clientSupports:    []string{"jsonrpc:1.5"},
			wantInterface:     "jsonrpc:1.0",
			wantClientVersion: "1.5",
		},
		{
			name:              "compatible same major version (server newer)",
			serverSupports:    []string{"jsonrpc:1.5"},
			clientSupports:    []string{"jsonrpc:1.0"},
			wantInterface:     "jsonrpc:1.5",
			wantClientVersion: "1.0",
		},
	}

	for _, tc := range testCases {
		if len(tc.serverSupports) < 1 {
			t.Fatal("servers have to specify at least one supported protocol")
		}
		if tc.clientSupports == nil {
			tc.clientSupports = make([]string, 0)
		}

		t.Run(tc.name, func(t *testing.T) {
			selectedProtocol := ""
			options := make([]FactoryOption, len(tc.clientSupports))
			for i, p := range tc.clientSupports {
				protocol, version := parseProtocol(p)
				options[i] = WithCompatTransport(version, protocol, TransportFactoryFn(func(ctx context.Context, card *a2a.AgentCard, iface *a2a.AgentInterface) (Transport, error) {
					if slices.Contains(tc.connectFails, p) {
						return nil, fmt.Errorf("connection failed")
					}
					if strings.ContainsRune(p, ':') {
						selectedProtocol = string(iface.ProtocolBinding) + ":" + string(iface.ProtocolVersion)
					} else {
						selectedProtocol = string(iface.ProtocolBinding)
					}
					return unimplementedTransport{}, nil
				}))
			}
			if tc.clientPrefers != nil {
				options = append(options, WithConfig(Config{PreferredTransports: makeProtocols(tc.clientPrefers)}))
			}
			factory := NewFactory(options...)

			// CreateFromCard
			card := &a2a.AgentCard{
				SupportedInterfaces: makeEndpoints(tc.serverSupports),
			}
			client, err := factory.CreateFromCard(ctx, card)
			if err != nil && !tc.wantErr {
				t.Fatalf("CreateFromCard() error = %v, want nil", err)
			}
			if err == nil && tc.wantErr {
				t.Fatalf("CreateFromCard() error = nil, want %v", tc.wantErr)
			}
			if selectedProtocol != tc.wantInterface {
				t.Fatalf("CreateFromCard() = %q, want %q", selectedProtocol, tc.wantInterface)
			}
			if tc.wantClientVersion != "" && client.protocolVersion != tc.wantClientVersion {
				t.Fatalf("CreateFromCard() client.protocolVersion = %q, want %q", client.protocolVersion, tc.wantClientVersion)
			}

			// CreateFromEndpoints
			selectedProtocol = ""
			client, err = factory.CreateFromEndpoints(ctx, makeEndpoints(tc.serverSupports))
			if err != nil && !tc.wantErr {
				t.Fatalf("CreateFromEndpoints() error = %v, want nil", err)
			}
			if err == nil && tc.wantErr {
				t.Fatalf("CreateFromEndpoints() error = nil, want %v", tc.wantErr)
			}
			if selectedProtocol != tc.wantInterface {
				t.Fatalf("CreateFromEndpoints() = %q, want %q", selectedProtocol, tc.wantInterface)
			}
			if tc.wantClientVersion != "" && client.protocolVersion != tc.wantClientVersion {
				t.Fatalf("CreateFromEndpoints() client.protocolVersion = %q, want %q", client.protocolVersion, tc.wantClientVersion)
			}
		})
	}
}
