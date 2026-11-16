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

	protocols := []struct {
		name       string
		protocol   a2a.TransportProtocol
		newHandler func(a2asrv.RequestHandler) http.Handler
		option     func(string) a2aclient.FactoryOption
	}{
		{
			name:     "jsonrpc",
			protocol: a2a.TransportProtocolJSONRPC,
			newHandler: func(h a2asrv.RequestHandler) http.Handler {
				mux := http.NewServeMux()
				mux.Handle("POST /{$}", NewJSONRPCHandler(h))
				return mux
			},
			option: func(url string) a2aclient.FactoryOption {
				return WithJSONRPCTransport(JSONRPCTransportConfig{URL: url})
			},
		},
		{
			name:       "rest",
			protocol:   a2a.TransportProtocolHTTPJSON,
			newHandler: func(h a2asrv.RequestHandler) http.Handler { return NewRESTHandler(h) },
			option: func(url string) a2aclient.FactoryOption {
				return WithRESTTransport(RESTTransportConfig{URL: url})
			},
		},
	}
	for _, protocol := range protocols {
		for _, override := range []bool{false, true} {
			name := "card URL"
			if override {
				name = "configured URL"
			}
			t.Run(protocol.name+"/"+name, func(t *testing.T) {
				t.Parallel()
				var requests, otherRequests atomic.Int32
				newServer := func(count *atomic.Int32) *httptest.Server {
					handler := protocol.newHandler(a2asrv.NewHandler(&replyingExecutor{}))
					return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						count.Add(1)
						handler.ServeHTTP(w, r)
					}))
				}
				server := newServer(&requests)
				t.Cleanup(server.Close)
				other := newServer(&otherRequests)
				t.Cleanup(other.Close)

				configuredURL := ""
				if override {
					configuredURL = server.URL
				}
				option := protocol.option(configuredURL)
				for _, cardURL := range []string{server.URL, other.URL} {
					if override {
						cardURL += "/unreachable-internal-endpoint"
					}
					card := &a2a.AgentCard{SupportedInterfaces: []*a2a.AgentInterface{
						{URL: cardURL, ProtocolBinding: protocol.protocol, ProtocolVersion: Version},
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
				want := []int32{1, 1}
				if override {
					want = []int32{2, 0}
				}
				if diff := cmp.Diff(want, []int32{requests.Load(), otherRequests.Load()}); diff != "" {
					t.Fatalf("client.SendMessage() request destinations wrong result (-want +got) diff = %s", diff)
				}
			})
		}
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
