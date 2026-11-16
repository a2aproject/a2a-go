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
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	legacya2a "github.com/a2aproject/a2a-go/a2a"
	legacyclient "github.com/a2aproject/a2a-go/a2aclient"
	legacysrv "github.com/a2aproject/a2a-go/a2asrv"
	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func newDualVersionCard() *a2a.AgentCard {
	return &a2a.AgentCard{
		Name:    "agent",
		Version: "1.0.0",
		SupportedInterfaces: []*a2a.AgentInterface{
			{URL: "https://agent/v1", ProtocolBinding: a2a.TransportProtocolJSONRPC, ProtocolVersion: a2a.Version, Tenant: "t1"},
			{URL: "https://agent/v03", ProtocolBinding: a2a.TransportProtocolJSONRPC, ProtocolVersion: Version},
			{URL: "https://agent/v03/rest", ProtocolBinding: a2a.TransportProtocolHTTPJSON, ProtocolVersion: Version},
		},
		Capabilities: a2a.AgentCapabilities{ExtendedAgentCard: true},
		SecuritySchemes: a2a.NamedSecuritySchemes{
			"bearer": a2a.HTTPAuthSecurityScheme{Scheme: "Bearer"},
			"oauth": a2a.OAuth2SecurityScheme{
				Flows: a2a.ClientCredentialsOAuthFlow{TokenURL: "https://auth/token", Scopes: map[string]string{"read": "Read"}},
			},
		},
		SecurityRequirements: a2a.SecurityRequirementsOptions{{"bearer": {}}, {"oauth": {"read"}}},
	}
}

type extendedCardHandler struct {
	a2asrv.RequestHandler
	card *a2a.AgentCard
}

func (h *extendedCardHandler) GetExtendedAgentCard(ctx context.Context, req *a2a.GetExtendedAgentCardRequest) (*a2a.AgentCard, error) {
	return h.card, nil
}

func TestServer_GetExtendedAgentCard(t *testing.T) {
	t.Parallel()

	want := &legacya2a.AgentCard{
		Name:                              "agent",
		Version:                           "1.0.0",
		URL:                               "https://agent/v03",
		PreferredTransport:                legacya2a.TransportProtocolJSONRPC,
		ProtocolVersion:                   string(Version),
		AdditionalInterfaces:              []legacya2a.AgentInterface{{URL: "https://agent/v03/rest", Transport: legacya2a.TransportProtocolHTTPJSON}},
		SupportsAuthenticatedExtendedCard: true,
		SecuritySchemes: legacya2a.NamedSecuritySchemes{
			"bearer": legacya2a.HTTPAuthSecurityScheme{Scheme: "Bearer"},
			"oauth": legacya2a.OAuth2SecurityScheme{
				Flows: legacya2a.OAuthFlows{ClientCredentials: &legacya2a.ClientCredentialsOAuthFlow{
					TokenURL: "https://auth/token",
					Scopes:   map[string]string{"read": "Read"},
				}},
			},
		},
		Security: []legacya2a.SecurityRequirements{{"bearer": {}}, {"oauth": {"read"}}},
	}

	tests := []struct {
		name    string
		handler http.Handler
		newReq  func(url string) *http.Request
		decode  func(t *testing.T, body []byte) *legacya2a.AgentCard
	}{
		{
			name:    "JSON-RPC",
			handler: NewJSONRPCHandler(&extendedCardHandler{card: newDualVersionCard()}),
			newReq: func(url string) *http.Request {
				body := `{"jsonrpc":"2.0","id":1,"method":"agent/getAuthenticatedExtendedCard"}`
				req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
				return req
			},
			decode: func(t *testing.T, body []byte) *legacya2a.AgentCard {
				var resp struct {
					Result *legacya2a.AgentCard `json:"result"`
				}
				if err := json.Unmarshal(body, &resp); err != nil {
					t.Fatalf("json.Unmarshal() error = %v, want nil", err)
				}
				return resp.Result
			},
		},
		{
			name:    "REST",
			handler: NewRESTHandler(&extendedCardHandler{card: newDualVersionCard()}),
			newReq: func(url string) *http.Request {
				req, _ := http.NewRequest(http.MethodGet, url+RESTPathPrefix+"/card", nil)
				return req
			},
			decode: func(t *testing.T, body []byte) *legacya2a.AgentCard {
				var card legacya2a.AgentCard
				if err := json.Unmarshal(body, &card); err != nil {
					t.Fatalf("json.Unmarshal() error = %v, want nil", err)
				}
				return &card
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(tc.handler)
			defer server.Close()

			resp, err := server.Client().Do(tc.newReq(server.URL))
			if err != nil {
				t.Fatalf("Do() error = %v, want nil", err)
			}
			defer func() {
				if err := resp.Body.Close(); err != nil {
					t.Errorf("Close() error = %v, want nil", err)
				}
			}()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("io.ReadAll() error = %v, want nil", err)
			}
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("StatusCode = %d, want 200; body = %s", resp.StatusCode, string(body))
			}

			got := tc.decode(t, body)
			if diff := cmp.Diff(want, got, cmpopts.EquateEmpty()); diff != "" {
				t.Fatalf("GetExtendedAgentCard() wrong result (-want +got) diff = %s", diff)
			}
		})
	}
}

type noopServerInterceptor struct {
	legacysrv.PassthroughCallInterceptor
}

type noopClientInterceptor struct {
	legacyclient.PassthroughInterceptor
}

type addSkillServerInterceptor struct {
	legacysrv.PassthroughCallInterceptor
}

func (addSkillServerInterceptor) After(ctx context.Context, callCtx *legacysrv.CallContext, resp *legacysrv.Response) error {
	card := resp.Payload.(*legacya2a.AgentCard)
	card.Skills = append(card.Skills, legacya2a.AgentSkill{ID: "s1", Name: "skill"})
	return nil
}

func TestInterceptors_PreserveExtendedAgentCard(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		after func(card *a2a.AgentCard) (any, error)
		want  func() *a2a.AgentCard
	}{
		{
			name: "server interceptor without changes",
			after: func(card *a2a.AgentCard) (any, error) {
				resp := &a2asrv.Response{Payload: card}
				err := NewServerInterceptor(noopServerInterceptor{}).After(t.Context(), &a2asrv.CallContext{}, resp)
				return resp.Payload, err
			},
			want: newDualVersionCard,
		},
		{
			name: "client interceptor without changes",
			after: func(card *a2a.AgentCard) (any, error) {
				resp := &a2aclient.Response{Payload: card, ServiceParams: a2aclient.ServiceParams{}}
				err := NewClientInterceptor(noopClientInterceptor{}).After(t.Context(), resp)
				return resp.Payload, err
			},
			want: newDualVersionCard,
		},
		{
			name: "server interceptor modifying the card",
			after: func(card *a2a.AgentCard) (any, error) {
				resp := &a2asrv.Response{Payload: card}
				err := NewServerInterceptor(addSkillServerInterceptor{}).After(t.Context(), &a2asrv.CallContext{}, resp)
				return resp.Payload, err
			},
			want: func() *a2a.AgentCard {
				card := newDualVersionCard()
				card.SupportedInterfaces = card.SupportedInterfaces[1:]
				card.Skills = []a2a.AgentSkill{{ID: "s1", Name: "skill"}}
				return card
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := tc.after(newDualVersionCard())
			if err != nil {
				t.Fatalf("After() error = %v, want nil", err)
			}
			if diff := cmp.Diff(tc.want(), got, cmpopts.EquateEmpty()); diff != "" {
				t.Fatalf("After() wrong result (-want +got) diff = %s", diff)
			}
		})
	}
}
