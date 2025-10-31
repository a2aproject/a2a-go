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
	"testing"

	"github.com/a2aproject/a2a-go/a2a"
)

func makeProtocols(in []string) []a2a.TransportProtocol {
	out := make([]a2a.TransportProtocol, len(in))
	for i, protocol := range in {
		out[i] = a2a.TransportProtocol(protocol)
	}
	return out
}

func makeEndpoints(protocols []string) []a2a.AgentInterface {
	out := make([]a2a.AgentInterface, len(protocols))
	for i, protocol := range protocols {
		out[i] = a2a.AgentInterface{Transport: a2a.TransportProtocol(protocol), URL: "https://agent.com"}
	}
	return out
}

func TestFactory_WithAdditionalOptions(t *testing.T) {
	f1 := NewFactory(WithConfig(Config{AcceptedOutputModes: []string{"application/json"}}))
	f2 := WithAdditionalOptions(f1, WithInterceptors(PassthroughInterceptor{}))

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
		name           string
		serverSupports []string // protocols advertised by the server
		clientSupports []string // list of registered transport factories
		clientPrefers  []string // Config.PreferredTransports
		connectFails   []string // specifies which transports fail to connect, used to test fallback logic
		want           string
		wantErr        bool
	}{
		{
			name:           "client supports fewer protocols",
			serverSupports: []string{"jsonrpc", "grpc"},
			clientSupports: []string{"grpc"},
			want:           "grpc",
		},
		{
			name:           "server supports fewer protocols",
			serverSupports: []string{"jsonrpc"},
			clientSupports: []string{"grpc", "jsonrpc"},
			want:           "jsonrpc",
		},
		{
			name:           "default to server preference order",
			serverSupports: []string{"jsonrpc", "grpc"},
			clientSupports: []string{"jsonrpc", "grpc"},
			want:           "jsonrpc",
		},
		{
			name:           "client preferences override server preferences",
			serverSupports: []string{"jsonrpc", "grpc"},
			clientSupports: []string{"jsonrpc", "grpc"},
			clientPrefers:  []string{"grpc", "jsonrpc"},
			want:           "grpc",
		},
		{
			name:           "client preferences as a subset of supported protocols",
			serverSupports: []string{"grpc", "jsonrpc", "stubby"},
			clientSupports: []string{"grpc", "stubby", "jsonrpc"},
			clientPrefers:  []string{"stubby"},
			want:           "stubby",
		},
		{
			name:           "selects the first working protocol",
			serverSupports: []string{"grpc", "jsonrpc", "stubby"},
			clientSupports: []string{"grpc", "stubby", "jsonrpc"},
			connectFails:   []string{"grpc", "jsonrpc"},
			want:           "stubby",
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
			name:           "client transports not configured",
			serverSupports: []string{"grpc"},
			wantErr:        true,
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
			for i, protocol := range tc.clientSupports {
				options[i] = WithTransport(a2a.TransportProtocol(protocol), TransportFactoryFn(func(context.Context, string, *a2a.AgentCard) (Transport, error) {
					if slices.Contains(tc.connectFails, protocol) {
						return nil, fmt.Errorf("connection failed")
					}
					selectedProtocol = protocol
					return unimplementedTransport{}, nil
				}))
			}
			if tc.clientPrefers != nil {
				options = append(options, WithConfig(Config{PreferredTransports: makeProtocols(tc.clientPrefers)}))
			}
			factory := NewFactory(options...)

			// CreateFromCard
			additional := make([]a2a.AgentInterface, len(tc.serverSupports)-1)
			for i, protocol := range tc.serverSupports[1:] {
				additional[i] = a2a.AgentInterface{Transport: a2a.TransportProtocol(protocol)}
			}
			card := &a2a.AgentCard{PreferredTransport: a2a.TransportProtocol(tc.serverSupports[0]), AdditionalInterfaces: additional}
			_, err := factory.CreateFromCard(ctx, card)
			if err != nil && !tc.wantErr {
				t.Fatalf("CreateFromCard() error = %v, want nil", err)
			}
			if err == nil && tc.wantErr {
				t.Fatalf("CreateFromCard() error = nil, want %v", tc.wantErr)
			}
			if selectedProtocol != tc.want {
				t.Fatalf("CreateFromCard() = %q, want %q", selectedProtocol, tc.want)
			}

			// CreateFromEndpoints
			selectedProtocol = ""
			_, err = factory.CreateFromEndpoints(ctx, makeEndpoints(tc.serverSupports))
			if err != nil && !tc.wantErr {
				t.Fatalf("CreateFromURL() error = %v, want nil", err)
			}
			if err == nil && tc.wantErr {
				t.Fatalf("CreateFromURL() error = nil, want %v", tc.wantErr)
			}
			if selectedProtocol != tc.want {
				t.Fatalf("CreateFromURL() = %q, want %q", selectedProtocol, tc.want)
			}
		})
	}
}
