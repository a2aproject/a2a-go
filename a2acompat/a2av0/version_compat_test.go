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

package a2av0

import (
	"context"
	"iter"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/google/go-cmp/cmp"
)

func TestClientServer_CardVersionWithPatch(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		protocol      a2a.TransportProtocol
		newHandler    func(a2asrv.RequestHandler) http.Handler
		clientFactory a2aclient.TransportFactory
	}{
		{
			name:          "jsonrpc",
			protocol:      a2a.TransportProtocolJSONRPC,
			newHandler:    func(h a2asrv.RequestHandler) http.Handler { return NewJSONRPCHandler(h) },
			clientFactory: NewJSONRPCTransportFactory(JSONRPCTransportConfig{}),
		},
		{
			name:          "rest",
			protocol:      a2a.TransportProtocolHTTPJSON,
			newHandler:    func(h a2asrv.RequestHandler) http.Handler { return NewRESTHandler(h) },
			clientFactory: NewRESTTransportFactory(RESTTransportConfig{}),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(tc.newHandler(a2asrv.NewHandler(&replyingExecutor{})))
			t.Cleanup(server.Close)

			card := &a2a.AgentCard{SupportedInterfaces: []*a2a.AgentInterface{
				{URL: server.URL, ProtocolBinding: tc.protocol, ProtocolVersion: "0.3.0"},
			}}
			client, err := a2aclient.NewFromCard(
				t.Context(),
				card,
				a2aclient.WithCompatTransport("0.3.0", tc.protocol, tc.clientFactory),
			)
			if err != nil {
				t.Fatalf("a2aclient.NewFromCard() error = %v, want nil", err)
			}

			msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hello"))
			if _, err := client.SendMessage(t.Context(), &a2a.SendMessageRequest{Message: msg}); err != nil {
				t.Fatalf("client.SendMessage() error = %v, want nil", err)
			}
		})
	}
}

func TestClientServer_TransportURL(t *testing.T) {
	t.Parallel()

	jsonrpcOption := func(url string, client *http.Client) a2aclient.FactoryOption {
		return WithJSONRPCTransport(JSONRPCTransportConfig{URL: url, Client: client})
	}
	restOption := func(url string, client *http.Client) a2aclient.FactoryOption {
		return WithRESTTransport(RESTTransportConfig{URL: url, Client: client})
	}
	jsonrpcHandler := func(h a2asrv.RequestHandler) http.Handler { return NewJSONRPCHandler(h) }
	restHandler := func(h a2asrv.RequestHandler) http.Handler { return NewRESTHandler(h) }
	tests := []struct {
		name          string
		protocol      a2a.TransportProtocol
		newHandler    func(a2asrv.RequestHandler) http.Handler
		option        func(string, *http.Client) a2aclient.FactoryOption
		configuredURL string
		cardURLs      []string
		wantRequests  []int32
	}{
		{
			name: "jsonrpc/card URL", protocol: a2a.TransportProtocolJSONRPC,
			newHandler: jsonrpcHandler, option: jsonrpcOption,
			configuredURL: "", cardURLs: []string{"primary", "other"},
			wantRequests: []int32{1, 1},
		},
		{
			name: "jsonrpc/configured URL", protocol: a2a.TransportProtocolJSONRPC,
			newHandler: jsonrpcHandler, option: jsonrpcOption,
			configuredURL: "primary", cardURLs: []string{"primary", "other"},
			wantRequests: []int32{2, 0},
		},
		{
			name: "rest/card URL", protocol: a2a.TransportProtocolHTTPJSON,
			newHandler: restHandler, option: restOption,
			configuredURL: "", cardURLs: []string{"primary", "other"},
			wantRequests: []int32{1, 1},
		},
		{
			name: "rest/configured URL", protocol: a2a.TransportProtocolHTTPJSON,
			newHandler: restHandler, option: restOption,
			configuredURL: "primary", cardURLs: []string{"primary", "other"},
			wantRequests: []int32{2, 0},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var requests, otherRequests atomic.Int32
			newServer := func(count *atomic.Int32) *httptest.Server {
				handler := test.newHandler(a2asrv.NewHandler(&replyingExecutor{}))
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					count.Add(1)
					handler.ServeHTTP(w, r)
				}))
			}
			server := newServer(&requests)
			t.Cleanup(server.Close)
			other := newServer(&otherRequests)
			t.Cleanup(other.Close)

			endpoints := map[string]string{"": "", "primary": server.URL, "other": other.URL}
			option := test.option(endpoints[test.configuredURL], server.Client())
			for _, cardURL := range test.cardURLs {
				card := &a2a.AgentCard{SupportedInterfaces: []*a2a.AgentInterface{
					{URL: endpoints[cardURL], ProtocolBinding: test.protocol, ProtocolVersion: Version},
				}}
				client, err := a2aclient.NewFromCard(t.Context(), card, option)
				if err != nil {
					t.Fatalf("a2aclient.NewFromCard() error = %v, want nil", err)
				}
				msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hello"))
				if _, err := client.SendMessage(t.Context(), &a2a.SendMessageRequest{Message: msg}); err != nil {
					t.Fatalf("client.SendMessage() error = %v, want nil", err)
				}
			}
			if diff := cmp.Diff(test.wantRequests, []int32{requests.Load(), otherRequests.Load()}); diff != "" {
				t.Fatalf("client.SendMessage() request destinations wrong result (-want +got) diff = %s", diff)
			}
		})
	}
}

type replyingExecutor struct{}

func (*replyingExecutor) Execute(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		yield(a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("ok")), nil)
	}
}

func (*replyingExecutor) Cancel(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {}
}
