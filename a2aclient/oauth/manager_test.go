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

package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aclient/agentcard"
	"github.com/a2aproject/a2a-go/v2/a2aclient/oauth/oauthtest"
	"github.com/google/go-cmp/cmp"
)

func newManager(as *oauthtest.AuthServer) *Manager {
	return &Manager{
		Clients:         []Registration{{Authority: as.URL, ClientID: "test-client"}},
		DefaultClientID: testCIMDClientID,
		Store:           NewMemoryStore(),
	}
}

func fetchCard(t *testing.T, agentURL string) *a2a.AgentCard {
	t.Helper()
	card, err := agentcard.DefaultResolver.Resolve(t.Context(), agentURL)
	if err != nil {
		t.Fatalf("Resolve(%q) error = %v", agentURL, err)
	}
	return card
}

// signIn obtains and stores a grant for req with the authorization code flow, or
// with the device flow when device is set, approving it as the user would.
func signIn(t *testing.T, m *Manager, req *Requirement, device bool) (*Grant, error) {
	t.Helper()
	if device {
		da, err := m.StartDeviceAuthorization(t.Context(), req, nil)
		if err != nil {
			return nil, err
		}
		oauthtest.Browser(t)(da.VerificationURIComplete)
		da.Interval = 0
		return m.PollDeviceAuthorization(t.Context(), da)
	}
	ar, err := m.NewAuthorizationRequest(req, testRedirectURI, nil)
	if err != nil {
		return nil, err
	}
	code, err := ar.ParseRedirect(redirectQuery(t, ar))
	if err != nil {
		return nil, err
	}
	return m.ExchangeCode(t.Context(), ar, code)
}

// signInToAgent signs in to the agent at agentURL as its card requires.
func signInToAgent(t *testing.T, m *Manager, agentURL string) (*Requirement, *Grant) {
	t.Helper()
	req, err := m.RequirementForCard(t.Context(), fetchCard(t, agentURL), agentURL)
	if err != nil || req == nil {
		t.Fatalf("Manager.RequirementForCard() = %v, %v, want a requirement", req, err)
	}
	g, err := signIn(t, m, req, false)
	if err != nil {
		t.Fatalf("signing in error = %v", err)
	}
	return req, g
}

func TestManagerRequirementForCard(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		card oauthtest.CardSecurity
		want *Requirement
	}{
		{name: "no security", card: oauthtest.CardNone},
		{name: "metadata url", card: oauthtest.CardMetadataURL, want: &Requirement{Scopes: []string{"a2a.invoke"}}},
		{name: "openid connect", card: oauthtest.CardOIDC, want: &Requirement{Scopes: []string{"a2a.invoke"}}},
		{
			name: "inline authorization code",
			card: oauthtest.CardAuthCode,
			want: &Requirement{Scopes: []string{"a2a.invoke"}},
		},
		{
			name: "inline device code",
			card: oauthtest.CardDeviceCode,
			want: &Requirement{Scopes: []string{"a2a.invoke"}, PreferDevice: true},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{})
			agent := oauthtest.NewProtectedAgent(t, as, oauthtest.AgentOptions{Card: tc.card, Scopes: []string{"a2a.invoke"}})
			m := newManager(as)
			card := fetchCard(t, agent.URL)
			got, err := m.RequirementForCard(t.Context(), card, agent.URL+"/")
			if err != nil {
				t.Fatalf("Manager.RequirementForCard() error = %v", err)
			}
			if tc.want != nil {
				tc.want.Endpoints, tc.want.Resource = testEndpoints(as), agent.URL
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("Manager.RequirementForCard() wrong result (-want +got) diff = %s", diff)
			}
			hits := as.Hits("/.well-known/oauth-authorization-server") + as.Hits("/.well-known/openid-configuration")
			if _, err := m.RequirementForCard(t.Context(), card, agent.URL+"/"); err != nil {
				t.Fatalf("Manager.RequirementForCard() again error = %v", err)
			}
			if again := as.Hits("/.well-known/oauth-authorization-server") + as.Hits("/.well-known/openid-configuration"); again != hits {
				t.Fatalf("Manager.RequirementForCard() again fetched metadata %d more times, want it cached", again-hits)
			}
		})
	}
}

func TestManagerRequirementForChallenge(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		agent     oauthtest.AgentOptions
		noClients bool
		server    oauthtest.ServerOptions
		noDefault bool
		wantErr   bool
		noClient  bool
	}{
		{name: "resource_metadata in the challenge", agent: oauthtest.AgentOptions{Scopes: []string{"a2a.invoke"}}},
		{name: "well-known probe", agent: oauthtest.AgentOptions{OmitResourceMetadata: true}},
		{name: "OIDC discovery only", server: oauthtest.ServerOptions{OIDCOnly: true}},
		{name: "no client", noClients: true, wantErr: true, noClient: true},
		{name: "CIMD fallback", noClients: true, server: oauthtest.ServerOptions{CIMD: true}},
		{name: "CIMD fallback disabled", noClients: true, server: oauthtest.ServerOptions{CIMD: true}, noDefault: true, wantErr: true, noClient: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			as := oauthtest.NewAuthServer(t, tc.server)
			agent := oauthtest.NewProtectedAgent(t, as, tc.agent)
			m := newManager(as)
			if tc.noClients {
				m.Clients = nil
			}
			if tc.noDefault {
				m.DefaultClientID = ""
			}
			resp, err := http.Post(agent.URL+"/message:send", "application/json", strings.NewReader("{}"))
			if err != nil {
				t.Fatal(err)
			}
			if err := resp.Body.Close(); err != nil {
				t.Errorf("resp.Body.Close() error = %v", err)
			}
			target, err := url.Parse(agent.URL + "/message:send")
			if err != nil {
				t.Fatal(err)
			}
			got, err := m.RequirementForChallenge(t.Context(), target, ParseChallenges(resp.Header.Values("WWW-Authenticate")))
			if (err != nil) != tc.wantErr {
				t.Fatalf("Manager.RequirementForChallenge() error = %v, want error %v", err, tc.wantErr)
			}
			var nc *NoClientError
			if errors.As(err, &nc) != tc.noClient {
				t.Fatalf("Manager.RequirementForChallenge() error = %v, want NoClientError %v", err, tc.noClient)
			}
			if tc.wantErr {
				return
			}
			if got.Resource != agent.URL || got.Endpoints.Issuer != as.URL || got.Endpoints.Token != as.URL+"/token" {
				t.Fatalf("Manager.RequirementForChallenge() = %+v, want resource %s at %s", got, agent.URL, as.URL)
			}
			if diff := cmp.Diff(tc.agent.Scopes, got.Scopes); diff != "" {
				t.Fatalf("Manager.RequirementForChallenge() scopes wrong result (-want +got) diff = %s", diff)
			}
		})
	}
}

func TestManagerRequirementForChallengeErrors(t *testing.T) {
	t.Parallel()
	as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{})
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"resource":"http://elsewhere.example","authorization_servers":["` + as.URL + `"]}`)); err != nil {
			t.Errorf("writing resource metadata error = %v", err)
		}
	}))
	t.Cleanup(other.Close)
	tests := []struct {
		name      string
		handler   http.HandlerFunc
		challenge string
		wantNil   bool
	}{
		{
			name: "cross-origin redirect",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, other.URL+"/prm", http.StatusFound)
			},
		},
		{
			name: "metadata for another resource",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if _, err := w.Write([]byte(`{"resource":"http://elsewhere.example","authorization_servers":["` + as.URL + `"]}`)); err != nil {
					t.Errorf("writing resource metadata error = %v", err)
				}
			},
		},
		{
			name: "authorization server with invalid metadata",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if _, err := w.Write([]byte(`{"resource":"http://` + r.Host + `/a2a","authorization_servers":["` + other.URL + `"]}`)); err != nil {
					t.Errorf("writing resource metadata error = %v", err)
				}
			},
		},
		{
			name:    "metadata URL not found",
			handler: http.NotFound,
		},
		{
			name:      "no metadata at the well-known locations",
			handler:   http.NotFound,
			challenge: "Bearer",
			wantNil:   true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(tc.handler)
			t.Cleanup(srv.Close)
			target, err := url.Parse(srv.URL + "/a2a")
			if err != nil {
				t.Fatal(err)
			}
			challenge := tc.challenge
			if challenge == "" {
				challenge = `Bearer resource_metadata="` + srv.URL + `/prm"`
			}
			chs := ParseChallenges([]string{challenge})
			got, err := newManager(as).RequirementForChallenge(t.Context(), target, chs)
			if tc.wantNil {
				if got != nil || err != nil {
					t.Fatalf("Manager.RequirementForChallenge() = %v, %v, want nil, nil", got, err)
				}
				return
			}
			var fe *FailureError
			if !errors.As(err, &fe) {
				t.Fatalf("Manager.RequirementForChallenge() error = %v, want FailureError", err)
			}
		})
	}
}

func TestManagerRequirementForURL(t *testing.T) {
	t.Parallel()
	as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{})
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(plain.Close)
	basic := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", `Basic realm="agent"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(basic.Close)
	tests := []struct {
		name      string
		target    func(t *testing.T) (string, *Requirement)
		wantClass string
	}{
		{
			name: "challenge only",
			target: func(t *testing.T) (string, *Requirement) {
				agent := oauthtest.NewProtectedAgent(t, as, oauthtest.AgentOptions{Scopes: []string{"a2a.invoke"}})
				return agent.URL, &Requirement{Endpoints: testEndpoints(as), Resource: agent.URL, Scopes: []string{"a2a.invoke"}}
			},
		},
		{
			name: "path-mounted agent without resource_metadata",
			target: func(t *testing.T) (string, *Requirement) {
				agent := oauthtest.NewProtectedAgent(t, as, oauthtest.AgentOptions{BasePath: "/a2a/v1", OmitResourceMetadata: true})
				return agent.URL, &Requirement{Endpoints: testEndpoints(as), Resource: agent.URL}
			},
		},
		{
			name:   "not protected",
			target: func(*testing.T) (string, *Requirement) { return plain.URL, nil },
		},
		{
			name:   "basic challenge",
			target: func(*testing.T) (string, *Requirement) { return basic.URL, nil },
		},
		{
			name:      "plain HTTP",
			target:    func(*testing.T) (string, *Requirement) { return "http://agent.example.com", nil },
			wantClass: "failure",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			raw, want := tc.target(t)
			target, err := url.Parse(raw)
			if err != nil {
				t.Fatal(err)
			}
			got, err := newManager(as).RequirementForURL(t.Context(), target)
			if class := errClass(err); class != tc.wantClass {
				t.Fatalf("Manager.RequirementForURL() error = %v (%s), want %q", err, class, tc.wantClass)
			}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Fatalf("Manager.RequirementForURL() wrong result (-want +got) diff = %s", diff)
			}
		})
	}
}

func TestManagerRequirementForError(t *testing.T) {
	t.Parallel()
	as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{})
	// cardServer answers agent card requests with status and challenge, in which
	// %s stands for its URL, and serves its resource metadata.
	cardServer := func(t *testing.T, status int, challenge string) string {
		var srv *httptest.Server
		srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == wellKnownResourceMetadata {
				w.Header().Set("Content-Type", "application/json")
				if _, err := fmt.Fprintf(w, `{"resource":%q,"authorization_servers":[%q]}`, srv.URL, as.URL); err != nil {
					t.Errorf("writing resource metadata error = %v", err)
				}
				return
			}
			if challenge != "" {
				w.Header().Set("WWW-Authenticate", strings.ReplaceAll(challenge, "%s", srv.URL))
			}
			http.Error(w, "denied", status)
		}))
		t.Cleanup(srv.Close)
		return srv.URL
	}
	const bearer = `Bearer resource_metadata="%s/.well-known/oauth-protected-resource"`
	tests := []struct {
		name      string
		status    int
		challenge string
		target    string
		err       error
		noTrans   bool
		want      bool
	}{
		{name: "protected card", status: http.StatusUnauthorized, challenge: bearer, want: true},
		{name: "fetched without the transport: well-known metadata", status: http.StatusUnauthorized, challenge: `Bearer realm="agent"`, noTrans: true, want: true},
		{name: "forbidden", status: http.StatusForbidden, challenge: bearer},
		{name: "basic challenge", status: http.StatusUnauthorized, challenge: `Basic realm="agent"`},
		{name: "plain HTTP", status: http.StatusUnauthorized, challenge: bearer, target: "http://agent.example.com/.well-known/agent-card.json"},
		{name: "other error", status: http.StatusUnauthorized, err: a2a.ErrUnauthenticated},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			base := cardServer(t, tc.status, tc.challenge)
			m := newManager(as)
			cardErr := tc.err
			if cardErr == nil {
				client := &http.Client{Transport: m.Transport(nil)}
				if tc.noTrans {
					client = nil
				}
				_, err := agentcard.NewResolver(client).Resolve(t.Context(), base)
				cardErr = fmt.Errorf("resolving: %w", err)
			}
			raw := tc.target
			if raw == "" {
				raw = base + "/.well-known/agent-card.json"
			}
			target, err := url.Parse(raw)
			if err != nil {
				t.Fatal(err)
			}
			got, err := m.RequirementForError(t.Context(), target, cardErr)
			if err != nil {
				t.Fatalf("Manager.RequirementForError() error = %v", err)
			}
			var want *Requirement
			if tc.want {
				want = &Requirement{Endpoints: testEndpoints(as), Resource: base}
			}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Fatalf("Manager.RequirementForError() wrong result (-want +got) diff = %s", diff)
			}
		})
	}
}

func TestManagerProtectedAgentCard(t *testing.T) {
	t.Parallel()
	as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{})
	agent := oauthtest.NewProtectedAgent(t, as, oauthtest.AgentOptions{Card: oauthtest.CardMetadataURL, ProtectCard: true})
	m := newManager(as)
	resolver := agentcard.NewResolver(&http.Client{Transport: m.Transport(nil)})
	_, err := resolver.Resolve(t.Context(), agent.URL)
	cardURL, perr := url.Parse(agent.URL + "/.well-known/agent-card.json")
	if perr != nil {
		t.Fatal(perr)
	}
	r, err := m.RequirementForError(t.Context(), cardURL, err)
	if err != nil || r == nil {
		t.Fatalf("Manager.RequirementForError() = %v, %v, want a requirement", r, err)
	}
	if _, err := signIn(t, m, r, false); err != nil {
		t.Fatalf("signing in error = %v", err)
	}
	if _, err := resolver.Resolve(t.Context(), agent.URL); err != nil {
		t.Fatalf("Resolver.Resolve() after signing in error = %v", err)
	}
}

func TestManagerSignIn(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		server oauthtest.ServerOptions
		device bool
		path   string
	}{
		{name: "authorization code", path: "/token"},
		{name: "device flow", device: true, path: "/device"},
		{name: "device flow without authorization endpoint", server: oauthtest.ServerOptions{NoAuthorizationEndpoint: true, PendingPolls: 2}, device: true, path: "/device"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			as := oauthtest.NewAuthServer(t, tc.server)
			agent := oauthtest.NewProtectedAgent(t, as, oauthtest.AgentOptions{Card: oauthtest.CardMetadataURL, Scopes: []string{"a2a.invoke"}})
			m := newManager(as)
			req, err := m.RequirementForCard(t.Context(), fetchCard(t, agent.URL), agent.URL)
			if err != nil {
				t.Fatal(err)
			}
			g, err := signIn(t, m, req, tc.device)
			if err != nil {
				t.Fatalf("signing in error = %v", err)
			}
			if !as.Valid(g.AccessToken, agent.URL, []string{"a2a.invoke"}) {
				t.Fatalf("signing in returned a token the agent does not accept")
			}
			if g.ClientSource != ClientSourceConfig || g.Key.ClientID != "test-client" {
				t.Fatalf("signing in = %+v, want the configured client", g)
			}
			if got := as.LastForm(tc.path).Get("resource"); got != agent.URL {
				t.Fatalf("resource sent to %s = %q, want %q", tc.path, got, agent.URL)
			}
			stored, err := m.Grant(t.Context(), req)
			if err != nil || stored == nil || stored.AccessToken != g.AccessToken {
				t.Fatalf("Manager.Grant() after signing in = %v, %v, want the new grant", stored, err)
			}
		})
	}
}

func TestManagerSignInWithCIMD(t *testing.T) {
	t.Parallel()
	as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{CIMD: true})
	agent := oauthtest.NewProtectedAgent(t, as, oauthtest.AgentOptions{Card: oauthtest.CardMetadataURL})
	m := newManager(as)
	m.Clients = nil
	_, g := signInToAgent(t, m, agent.URL)
	if g.ClientSource != ClientSourceCIMD || g.Key.ClientID != testCIMDClientID {
		t.Fatalf("signing in = %+v, want the CIMD client", g)
	}
}

func TestManagerSignInScopes(t *testing.T) {
	t.Parallel()
	as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{})
	m := newManager(as)
	m.Clients[0].Scopes = []string{"from-registration"}
	req := &Requirement{Endpoints: testEndpoints(as), Resource: "https://agent.example.com"}
	tests := []struct {
		name     string
		req      *Requirement
		override []string
		want     string
	}{
		{name: "registration", req: req, want: "from-registration"},
		{name: "requirement", req: &Requirement{Endpoints: req.Endpoints, Resource: req.Resource, Scopes: []string{"from-agent"}}, want: "from-agent"},
		{name: "override", req: &Requirement{Endpoints: req.Endpoints, Resource: req.Resource, Scopes: []string{"from-agent"}}, override: []string{"b", "a"}, want: "a b"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ar, err := m.NewAuthorizationRequest(tc.req, testRedirectURI, tc.override)
			if err != nil {
				t.Fatalf("Manager.NewAuthorizationRequest() error = %v", err)
			}
			u, err := url.Parse(ar.URL)
			if err != nil {
				t.Fatal(err)
			}
			if got := u.Query().Get("scope"); got != tc.want {
				t.Fatalf("Manager.NewAuthorizationRequest() scope = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestManagerSignInDenied(t *testing.T) {
	t.Parallel()
	for _, device := range []bool{false, true} {
		t.Run(fmt.Sprintf("device %v", device), func(t *testing.T) {
			t.Parallel()
			as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{DenyAuthorization: true})
			agent := oauthtest.NewProtectedAgent(t, as, oauthtest.AgentOptions{Card: oauthtest.CardMetadataURL})
			m := newManager(as)
			req, err := m.RequirementForCard(t.Context(), fetchCard(t, agent.URL), agent.URL)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := signIn(t, m, req, device); !isOAuthError(err, "access_denied") {
				t.Fatalf("signing in error = %v, want access_denied", err)
			}
			if grants, err := m.List(t.Context()); err != nil || len(grants) != 0 {
				t.Fatalf("Manager.List() = %v, %v, want no grants", grants, err)
			}
		})
	}
}

func TestManagerGrantRefreshesNearExpiry(t *testing.T) {
	t.Parallel()
	for _, rotate := range []bool{false, true} {
		t.Run(fmt.Sprintf("rotate %v", rotate), func(t *testing.T) {
			t.Parallel()
			as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{RotateRefreshTokens: rotate, TokenLifetime: 30 * time.Second})
			agent := oauthtest.NewProtectedAgent(t, as, oauthtest.AgentOptions{Card: oauthtest.CardMetadataURL})
			m := newManager(as)
			req, first := signInToAgent(t, m, agent.URL)
			for i := range 2 {
				g, err := m.Grant(t.Context(), req)
				if err != nil {
					t.Fatalf("Manager.Grant() #%d error = %v", i, err)
				}
				if g.AccessToken == first.AccessToken {
					t.Fatalf("Manager.Grant() #%d did not refresh a token that expires in 30s", i)
				}
				if !as.Valid(g.AccessToken, agent.URL, nil) {
					t.Fatalf("Manager.Grant() #%d returned an invalid token", i)
				}
			}
			if rotate && as.Hits("/token") != 3 {
				t.Fatalf("token endpoint hits = %d, want 3 (one exchange, two refreshes with rotated tokens)", as.Hits("/token"))
			}
		})
	}
}

func TestManagerGrantRefreshesOnce(t *testing.T) {
	t.Parallel()
	as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{RotateRefreshTokens: true})
	agent := oauthtest.NewProtectedAgent(t, as, oauthtest.AgentOptions{Card: oauthtest.CardMetadataURL})
	var now atomic.Pointer[time.Time]
	start := time.Now()
	now.Store(&start)
	m := newManager(as)
	m.now = func() time.Time { return *now.Load() }
	req, first := signInToAgent(t, m, agent.URL)
	later := start.Add(time.Hour - refreshSkew/2)
	now.Store(&later)

	var wg sync.WaitGroup
	tokens := make([]string, 10)
	for i := range tokens {
		wg.Go(func() {
			g, err := m.Grant(t.Context(), req)
			if err != nil || g == nil {
				t.Errorf("Manager.Grant() = %v, %v, want a grant", g, err)
				return
			}
			tokens[i] = g.AccessToken
		})
	}
	wg.Wait()
	if hits := as.Hits("/token"); hits != 2 {
		t.Fatalf("token endpoint hits = %d, want 2 (one exchange and one refresh shared by all callers)", hits)
	}
	for _, tok := range tokens {
		if tok == first.AccessToken || tok != tokens[0] {
			t.Fatalf("Manager.Grant() access tokens = %q, want one new token for all callers", tokens)
		}
	}
}

func TestManagerGrantDropsRevokedRefreshToken(t *testing.T) {
	t.Parallel()
	as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{TokenLifetime: 30 * time.Second})
	agent := oauthtest.NewProtectedAgent(t, as, oauthtest.AgentOptions{Card: oauthtest.CardMetadataURL})
	m := newManager(as)
	req, _ := signInToAgent(t, m, agent.URL)
	as.RevokeRefreshTokens()
	g, err := m.Grant(t.Context(), req)
	if err != nil || g != nil {
		t.Fatalf("Manager.Grant() with a revoked refresh token = %v, %v, want nil, nil", g, err)
	}
	if grants, err := m.List(t.Context()); err != nil || len(grants) != 0 {
		t.Fatalf("Manager.List() = %v, %v, want no grants", grants, err)
	}
}

// tokenlessListStore lists grants without their tokens, as a store that keeps
// them in a keychain may.
type tokenlessListStore struct{ *MemoryStore }

func (s tokenlessListStore) List(ctx context.Context) ([]Grant, error) {
	grants, err := s.MemoryStore.List(ctx)
	for i := range grants {
		grants[i].AccessToken, grants[i].RefreshToken = "", ""
	}
	return grants, err
}

func TestManagerRevokeAndDelete(t *testing.T) {
	t.Parallel()
	for _, tokenless := range []bool{false, true} {
		t.Run(fmt.Sprintf("tokenless list %v", tokenless), func(t *testing.T) {
			t.Parallel()
			as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{})
			agent := oauthtest.NewProtectedAgent(t, as, oauthtest.AgentOptions{Card: oauthtest.CardMetadataURL})
			m := newManager(as)
			if tokenless {
				m.Store = tokenlessListStore{NewMemoryStore()}
			}
			_, g := signInToAgent(t, m, agent.URL)
			grants, err := m.List(t.Context())
			if err != nil || len(grants) != 1 {
				t.Fatalf("Manager.List() = %v, %v, want one grant", grants, err)
			}
			if err := m.Revoke(t.Context(), grants[0].Key); err != nil {
				t.Fatalf("Manager.Revoke() error = %v", err)
			}
			if diff := cmp.Diff([]string{g.RefreshToken}, as.Revoked()); diff != "" {
				t.Fatalf("revoked tokens wrong result (-want +got) diff = %s", diff)
			}
			if err := m.Delete(t.Context(), grants[0].Key); err != nil {
				t.Fatalf("Manager.Delete() error = %v", err)
			}
			if grants, err := m.List(t.Context()); err != nil || len(grants) != 0 {
				t.Fatalf("Manager.List() = %v, %v, want no grants", grants, err)
			}
			if err := m.Revoke(t.Context(), g.Key); !errors.Is(err, ErrGrantNotFound) {
				t.Fatalf("Manager.Revoke() after Delete error = %v, want %v", err, ErrGrantNotFound)
			}
		})
	}
}

// lockingStore counts the times its lock is taken and checks that writes happen
// under it.
type lockingStore struct {
	*MemoryStore
	t      *testing.T
	mu     sync.Mutex
	held   bool
	locked int
}

func (s *lockingStore) Lock(context.Context) (func(), error) {
	s.mu.Lock()
	s.held = true
	s.locked++
	return func() { s.held = false; s.mu.Unlock() }, nil
}

func (s *lockingStore) Put(ctx context.Context, g Grant) error {
	if !s.held {
		s.t.Error("Store.Put() called without the lock")
	}
	return s.MemoryStore.Put(ctx, g)
}

func (s *lockingStore) Delete(ctx context.Context, key GrantKey) error {
	if !s.held {
		s.t.Error("Store.Delete() called without the lock")
	}
	return s.MemoryStore.Delete(ctx, key)
}

func TestManagerUsesStoreLock(t *testing.T) {
	t.Parallel()
	as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{TokenLifetime: 30 * time.Second})
	agent := oauthtest.NewProtectedAgent(t, as, oauthtest.AgentOptions{Card: oauthtest.CardMetadataURL})
	store := &lockingStore{MemoryStore: NewMemoryStore(), t: t}
	m := newManager(as)
	m.Store = store
	req, _ := signInToAgent(t, m, agent.URL)
	if _, err := m.Grant(t.Context(), req); err != nil {
		t.Fatalf("Manager.Grant() error = %v", err)
	}
	grants, err := m.List(t.Context())
	if err != nil || len(grants) != 1 {
		t.Fatalf("Manager.List() = %v, %v, want one grant", grants, err)
	}
	if err := m.Delete(t.Context(), grants[0].Key); err != nil {
		t.Fatalf("Manager.Delete() error = %v", err)
	}
	if store.locked != 3 {
		t.Fatalf("store locked %d times, want 3 (sign-in, refresh, delete)", store.locked)
	}
}

// wrappedStore hides the Locker of the store it wraps.
type wrappedStore struct{ Store }

func TestManagerExplicitLocker(t *testing.T) {
	t.Parallel()
	as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{TokenLifetime: 30 * time.Second})
	agent := oauthtest.NewProtectedAgent(t, as, oauthtest.AgentOptions{Card: oauthtest.CardMetadataURL})
	inner := &lockingStore{MemoryStore: NewMemoryStore(), t: t}
	m := newManager(as)
	m.Store = wrappedStore{inner}
	m.Locker = inner
	req, _ := signInToAgent(t, m, agent.URL)
	if _, err := m.Grant(t.Context(), req); err != nil {
		t.Fatalf("Manager.Grant() error = %v", err)
	}
	if inner.locked != 2 {
		t.Fatalf("store locked %d times, want 2 (sign-in, refresh)", inner.locked)
	}
}

func TestManagerDefaultStore(t *testing.T) {
	t.Parallel()
	as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{})
	agent := oauthtest.NewProtectedAgent(t, as, oauthtest.AgentOptions{Card: oauthtest.CardMetadataURL})
	m := newManager(as)
	m.Store = nil
	req, g := signInToAgent(t, m, agent.URL)
	got, err := m.Grant(t.Context(), req)
	if err != nil || got == nil || got.AccessToken != g.AccessToken {
		t.Fatalf("Manager.Grant() with the default store = %v, %v, want the stored grant", got, err)
	}
}

// TestManagerWithClient runs the interceptor and the transport in an A2A client,
// for agents whose card declares OAuth and agents whose card does not.
func TestManagerWithClient(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		card    oauthtest.CardSecurity
		jsonrpc bool
	}{
		{name: "card declares OAuth, REST", card: oauthtest.CardMetadataURL},
		{name: "card declares OAuth, JSON-RPC", card: oauthtest.CardMetadataURL, jsonrpc: true},
		{name: "challenge only, REST", card: oauthtest.CardNone},
		{name: "challenge only, JSON-RPC", card: oauthtest.CardNone, jsonrpc: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{})
			agent := oauthtest.NewProtectedAgent(t, as, oauthtest.AgentOptions{Card: tc.card, Scopes: []string{"a2a.invoke"}, JSONRPC: tc.jsonrpc})
			m := newManager(as)
			hc := &http.Client{Transport: m.Transport(nil)}
			client, err := a2aclient.NewFromCard(t.Context(), fetchCard(t, agent.URL),
				a2aclient.WithJSONRPCTransport(hc),
				a2aclient.WithRESTTransport(hc),
				a2aclient.WithCallInterceptors(m.Interceptor()),
			)
			if err != nil {
				t.Fatalf("a2aclient.NewFromCard() error = %v", err)
			}
			send := func() error {
				msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hi"))
				_, err := client.SendMessage(t.Context(), &a2a.SendMessageRequest{Message: msg})
				return err
			}

			err = send()
			var ir *InteractionRequiredError
			if !errors.As(err, &ir) {
				t.Fatalf("Client.SendMessage() without a grant error = %v, want InteractionRequiredError", err)
			}
			if tc.card == oauthtest.CardNone && ir.Err == nil {
				t.Fatalf("Client.SendMessage() without a grant error = %v, want it to wrap the agent's 401", err)
			}
			if _, err := signIn(t, m, ir.Requirement, false); err != nil {
				t.Fatalf("signing in error = %v", err)
			}
			if err := send(); err != nil {
				t.Fatalf("Client.SendMessage() with a grant error = %v", err)
			}
			as.Invalidate()
			if err := send(); err != nil {
				t.Fatalf("Client.SendMessage() after the token was invalidated error = %v", err)
			}
			as.Invalidate()
			as.RevokeRefreshTokens()
			err = send()
			if !errors.As(err, &ir) || ir.Reason == "" {
				t.Fatalf("Client.SendMessage() after the grant was revoked error = %v, want InteractionRequiredError with a reason", err)
			}
			if tc.card == oauthtest.CardMetadataURL && slices.Contains(agent.AuthorizationHeaders(), "") {
				t.Fatalf("Authorization headers the agent received = %q, want a token on every request when the card declares OAuth", agent.AuthorizationHeaders())
			}
		})
	}
}

// TestManagerWithClientUnauthorized checks that a 401 the Manager can't act on
// reaches the caller unchanged.
func TestManagerWithClientUnauthorized(t *testing.T) {
	t.Parallel()
	as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{})
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(broken.Close)
	tests := []struct {
		name      string
		challenge func(agentURL string) string
		noClients bool
		wantClass string
	}{
		{name: "no challenge", challenge: func(string) string { return "" }},
		{name: "basic challenge", challenge: func(string) string { return `Basic realm="agent"` }},
		{name: "bearer without metadata", challenge: func(string) string { return "Bearer" }},
		{name: "bearer with failing discovery", challenge: func(string) string { return `Bearer resource_metadata="` + broken.URL + `/prm"` }},
		{name: "bearer without a client", challenge: func(u string) string { return `Bearer resource_metadata="` + u + `/prm"` }, noClients: true, wantClass: "no client"},
	}
	for _, tc := range tests {
		for _, protocol := range []a2a.TransportProtocol{a2a.TransportProtocolJSONRPC, a2a.TransportProtocolHTTPJSON} {
			t.Run(tc.name+", "+string(protocol), func(t *testing.T) {
				t.Parallel()
				var agentURL string
				agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/prm" {
						w.Header().Set("Content-Type", "application/json")
						if _, err := io.WriteString(w, `{"resource":"`+agentURL+`","authorization_servers":["`+as.URL+`"]}`); err != nil {
							t.Errorf("writing resource metadata error = %v", err)
						}
						return
					}
					if c := tc.challenge(agentURL); c != "" {
						w.Header().Set("WWW-Authenticate", c)
					}
					http.Error(w, "unauthorized", http.StatusUnauthorized)
				}))
				t.Cleanup(agent.Close)
				agentURL = agent.URL
				m := newManager(as)
				if tc.noClients {
					m.Clients, m.DefaultClientID = nil, ""
				}
				hc := &http.Client{Transport: m.Transport(nil)}
				client, err := a2aclient.NewFromEndpoints(t.Context(), []*a2a.AgentInterface{a2a.NewAgentInterface(agent.URL, protocol)},
					a2aclient.WithJSONRPCTransport(hc),
					a2aclient.WithRESTTransport(hc),
					a2aclient.WithCallInterceptors(m.Interceptor()),
				)
				if err != nil {
					t.Fatalf("a2aclient.NewFromEndpoints() error = %v", err)
				}
				_, err = client.GetTask(t.Context(), &a2a.GetTaskRequest{ID: "task"})
				if !strings.Contains(err.Error(), "401") {
					t.Fatalf("Client.GetTask() error = %v, want the agent's 401", err)
				}
				if got := errClass(err); got != tc.wantClass && !(tc.wantClass == "" && strings.HasPrefix(got, "unclassified")) {
					t.Fatalf("Client.GetTask() error = %v (%s), want %q", err, got, tc.wantClass)
				}
			})
		}
	}
}

// TestManagerWithClientExplicitAuthorization checks that a 401 to a call that
// carried the caller's own Authorization reaches the caller unchanged: signing
// in would not help, since the caller's credential would win again.
func TestManagerWithClientExplicitAuthorization(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		card oauthtest.CardSecurity
	}{
		{name: "challenge only", card: oauthtest.CardNone},
		{name: "card declares OAuth", card: oauthtest.CardMetadataURL},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{})
			agent := oauthtest.NewProtectedAgent(t, as, oauthtest.AgentOptions{Card: tc.card})
			m := newManager(as)
			hc := &http.Client{Transport: m.Transport(nil)}
			client, err := a2aclient.NewFromCard(t.Context(), fetchCard(t, agent.URL),
				a2aclient.WithJSONRPCTransport(hc),
				a2aclient.WithRESTTransport(hc),
				a2aclient.WithCallInterceptors(m.Interceptor()),
			)
			if err != nil {
				t.Fatalf("a2aclient.NewFromCard() error = %v", err)
			}
			params := a2aclient.ServiceParams{}
			params.Append("Authorization", "Bearer user-supplied")
			ctx := a2aclient.AttachServiceParams(t.Context(), params)
			msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hi"))
			_, err = client.SendMessage(ctx, &a2a.SendMessageRequest{Message: msg})
			if err == nil || !strings.Contains(err.Error(), "401") {
				t.Fatalf("Client.SendMessage() error = %v, want the agent's 401", err)
			}
			if got := errClass(err); !strings.HasPrefix(got, "unclassified") {
				t.Fatalf("Client.SendMessage() error = %v (%s), want the plain 401", err, got)
			}
			if diff := cmp.Diff([]string{"Bearer user-supplied"}, agent.AuthorizationHeaders()); diff != "" {
				t.Fatalf("Authorization headers the agent received wrong result (-want +got) diff = %s", diff)
			}
		})
	}
}

func TestManagerLearnedGrantMostSpecific(t *testing.T) {
	t.Parallel()
	const agent = "https://agent.example.com"
	m := managerWithGrant(t, agent, nil, "origin", "")
	nested := Grant{
		Key:         NewGrantKey(inlineEndpoints.Issuer, "c", agent+"/a", nil),
		AccessToken: "nested",
		ExpiresAt:   time.Now().Add(time.Hour),
		Endpoints:   inlineEndpoints,
	}
	if err := m.Store.Put(t.Context(), nested); err != nil {
		t.Fatal(err)
	}
	m.learn(NewGrantKey(inlineEndpoints.Issuer, "c", agent, nil))
	m.learn(nested.Key)
	var mu sync.Mutex
	var sent []string
	base := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		sent = append(sent, r.Header.Get("Authorization"))
		mu.Unlock()
		return ok(r), nil
	})
	const n = 100
	for range n {
		req, err := http.NewRequest(http.MethodGet, agent+"/a/x", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := m.Transport(base).RoundTrip(req)
		if err != nil {
			t.Fatalf("Transport.RoundTrip() error = %v", err)
		}
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close() error = %v", err)
		}
	}
	want := slices.Repeat([]string{"Bearer nested"}, n)
	if diff := cmp.Diff(want, sent); diff != "" {
		t.Fatalf("Authorization headers sent to %s/a/x wrong result (-want +got) diff = %s", agent, diff)
	}
}

func TestManagerGrantResolution(t *testing.T) {
	t.Parallel()
	const agent = "https://agent.example.com"
	grant := func(resource string, scopes, granted []string, token string) Grant {
		return Grant{
			Key:           NewGrantKey(inlineEndpoints.Issuer, "c", resource, scopes),
			GrantedScopes: granted,
			AccessToken:   token,
			ExpiresAt:     time.Now().Add(time.Hour),
			Endpoints:     inlineEndpoints,
		}
	}
	tests := []struct {
		name   string
		grants []Grant
		scopes []string
		target string
		want   string
	}{
		{
			name:   "longest covering resource",
			grants: []Grant{grant(agent, []string{"a"}, nil, "origin"), grant(agent+"/v1", []string{"a"}, nil, "nested")},
			scopes: []string{"a"},
			target: agent + "/v1/x",
			want:   "nested",
		},
		{
			name:   "two scope variants of one resource",
			grants: []Grant{grant(agent, []string{"a", "b"}, nil, "ab"), grant(agent, []string{"a", "c"}, nil, "ac")},
			scopes: []string{"a"},
			target: agent + "/v1",
		},
		{
			name:   "granted scopes cover more than requested",
			grants: []Grant{grant(agent, []string{"a"}, []string{"a", "b"}, "granted")},
			scopes: []string{"b"},
			target: agent + "/v1",
			want:   "granted",
		},
		{
			name:   "granted scopes cover less than requested",
			grants: []Grant{grant(agent, []string{"a", "b"}, []string{"a"}, "narrowed")},
			scopes: []string{"b"},
			target: agent + "/v1",
		},
		{
			name:   "exact key whose server listed fewer scopes than requested",
			grants: []Grant{grant(agent+"/v1", []string{"a", "b"}, []string{"a"}, "narrowed")},
			scopes: []string{"a", "b"},
			target: agent + "/v1",
			want:   "narrowed",
		},
		{
			name:   "exact key whose granted scopes cover the requested ones",
			grants: []Grant{grant(agent+"/v1", []string{"a", "b"}, []string{"b", "a"}, "exact")},
			scopes: []string{"a", "b"},
			target: agent + "/v1",
			want:   "exact",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := managerWithGrant(t, "", nil, "", "")
			for _, g := range tc.grants {
				if err := m.Store.Put(t.Context(), g); err != nil {
					t.Fatal(err)
				}
			}
			req := &Requirement{Endpoints: inlineEndpoints, Resource: tc.target, Scopes: tc.scopes}
			for range 50 {
				g, err := m.Grant(t.Context(), req)
				if err != nil {
					t.Fatalf("Manager.Grant() error = %v", err)
				}
				var got string
				if g != nil {
					got = g.AccessToken
				}
				if got != tc.want {
					t.Fatalf("Manager.Grant() access token = %q, want %q", got, tc.want)
				}
			}
		})
	}
}

func TestManagerRefreshSurvivesCancel(t *testing.T) {
	t.Parallel()
	as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{RotateRefreshTokens: true, TokenLifetime: 30 * time.Second})
	agent := oauthtest.NewProtectedAgent(t, as, oauthtest.AgentOptions{Card: oauthtest.CardMetadataURL})
	m := newManager(as)
	req, first := signInToAgent(t, m, agent.URL)
	ctx, cancel := context.WithCancel(t.Context())
	m.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		resp, err := http.DefaultTransport.RoundTrip(r)
		if err != nil || r.URL.Path != "/token" {
			return resp, err
		}
		cancel()
		if err := r.Context().Err(); err != nil {
			if err := resp.Body.Close(); err != nil {
				t.Errorf("resp.Body.Close() error = %v", err)
			}
			return nil, err
		}
		return resp, nil
	})}
	if _, err := m.Grant(ctx, req); err != nil {
		t.Fatalf("Manager.Grant() with a context canceled during the refresh error = %v", err)
	}
	stored, err := m.Store.Get(t.Context(), first.Key)
	if err != nil {
		t.Fatalf("Store.Get() error = %v", err)
	}
	if stored.RefreshToken == first.RefreshToken {
		t.Fatalf("Store.Get() refresh token is the one the server rotated away, want the new one")
	}
	g, err := m.Grant(t.Context(), req)
	if err != nil || g == nil || !as.Valid(g.AccessToken, agent.URL, nil) {
		t.Fatalf("Manager.Grant() after the canceled refresh = %v, %v, want a valid grant", g, err)
	}
}

// processLocker is a Locker that, like an fcntl lock, excludes only other
// processes.
type processLocker struct{ *MemoryStore }

func (processLocker) Lock(context.Context) (func(), error) { return func() {}, nil }

func TestManagerLocksInProcessFirst(t *testing.T) {
	t.Parallel()
	as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{RotateRefreshTokens: true})
	agent := oauthtest.NewProtectedAgent(t, as, oauthtest.AgentOptions{Card: oauthtest.CardMetadataURL})
	var now atomic.Pointer[time.Time]
	start := time.Now()
	now.Store(&start)
	m := newManager(as)
	m.Store = processLocker{NewMemoryStore()}
	m.now = func() time.Time { return *now.Load() }
	req, _ := signInToAgent(t, m, agent.URL)
	later := start.Add(time.Hour - refreshSkew/2)
	now.Store(&later)

	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			if g, err := m.Grant(t.Context(), req); err != nil || g == nil {
				t.Errorf("Manager.Grant() = %v, %v, want a grant", g, err)
			}
		})
	}
	wg.Wait()
	if hits := as.Hits("/token"); hits != 2 {
		t.Fatalf("token endpoint hits = %d, want 2 (one exchange and one refresh)", hits)
	}
}

func TestManagerOmitResource(t *testing.T) {
	t.Parallel()
	as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{TokenLifetime: 30 * time.Second})
	agent := oauthtest.NewProtectedAgent(t, as, oauthtest.AgentOptions{Card: oauthtest.CardMetadataURL})
	m := newManager(as)
	m.Clients[0].OmitResource = true
	req, err := m.RequirementForCard(t.Context(), fetchCard(t, agent.URL), agent.URL)
	if err != nil {
		t.Fatal(err)
	}
	ar, err := m.NewAuthorizationRequest(req, testRedirectURI, nil)
	if err != nil {
		t.Fatalf("Manager.NewAuthorizationRequest() error = %v", err)
	}
	if got := ar.URL; strings.Contains(got, "resource=") {
		t.Fatalf("Manager.NewAuthorizationRequest() URL = %q, want no resource indicator", got)
	}
	for _, device := range []bool{false, true} {
		g, err := signIn(t, m, req, device)
		if err != nil {
			t.Fatalf("signing in (device %v) error = %v", device, err)
		}
		if !g.OmitResource {
			t.Fatalf("signing in (device %v) grant OmitResource = false, want true", device)
		}
	}
	if got := as.LastForm("/device"); got.Has("resource") {
		t.Fatalf("device authorization form = %v, want no resource indicator", got)
	}
	if _, err := m.Grant(t.Context(), req); err != nil {
		t.Fatalf("Manager.Grant() error = %v", err)
	}
	if got := as.LastForm("/token"); got.Get("grant_type") != "refresh_token" || got.Has("resource") {
		t.Fatalf("last token form = %v, want a refresh without a resource indicator", got)
	}
}

func TestManagerRequirementForCardCache(t *testing.T) {
	t.Parallel()
	m := managerWithGrant(t, "", nil, "", "")
	card := inlineCard(false)
	first, err := m.RequirementForCard(t.Context(), card, "https://agent.example.com")
	if err != nil || first == nil {
		t.Fatalf("Manager.RequirementForCard() = %v, %v, want a requirement", first, err)
	}
	want := *first
	want.Scopes = slices.Clone(first.Scopes)
	first.Resource = "https://mutated.example.com"
	first.Scopes = append(first.Scopes, "mutated")
	again, err := m.RequirementForCard(t.Context(), card, "https://agent.example.com")
	if err != nil {
		t.Fatalf("Manager.RequirementForCard() again error = %v", err)
	}
	if diff := cmp.Diff(&want, again); diff != "" {
		t.Fatalf("Manager.RequirementForCard() after mutating a result wrong result (-want +got) diff = %s", diff)
	}
	for i := range 1000 {
		if _, err := m.RequirementForCard(t.Context(), &a2a.AgentCard{Name: fmt.Sprint(i)}, "https://agent.example.com"); err != nil {
			t.Fatalf("Manager.RequirementForCard() error = %v", err)
		}
	}
	if got := len(m.cardReqs.entries); got > maxCachedCardRequirements {
		t.Fatalf("cached card requirements = %d, want at most %d", got, maxCachedCardRequirements)
	}
}

func TestFailure(t *testing.T) {
	t.Parallel()
	err := failure("refreshing the grant for %s: %w", "https://agent.example.com", io.ErrUnexpectedEOF)
	var fe *FailureError
	if !errors.As(err, &fe) || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("failure() = %#v, want a *FailureError wrapping the cause", err)
	}
	if want := "refreshing the grant for https://agent.example.com: unexpected EOF"; err.Error() != want {
		t.Fatalf("failure().Error() = %q, want %q", err.Error(), want)
	}
}

func TestInteractionRequiredError(t *testing.T) {
	t.Parallel()
	req := &Requirement{Endpoints: Endpoints{Issuer: "https://login.example.com"}, Resource: "https://agent.example.com"}
	scoped := &Requirement{Endpoints: req.Endpoints, Resource: req.Resource, Scopes: []string{"a2a.invoke", "openid"}}
	tests := []struct {
		name string
		err  *InteractionRequiredError
		want string
	}{
		{name: "no scopes", err: &InteractionRequiredError{Requirement: req}, want: "https://agent.example.com requires authorization at https://login.example.com"},
		{name: "scopes", err: &InteractionRequiredError{Requirement: scoped}, want: "https://agent.example.com requires authorization at https://login.example.com for scopes a2a.invoke openid"},
		{name: "reason", err: &InteractionRequiredError{Requirement: req, Reason: "the agent rejected the stored grant"}, want: "https://agent.example.com requires authorization at https://login.example.com: the agent rejected the stored grant"},
		{name: "zero value", err: &InteractionRequiredError{}, want: "authorization required"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.err.Error(); got != tc.want {
				t.Fatalf("InteractionRequiredError.Error() = %q, want %q", got, tc.want)
			}
		})
	}
}

// countingClient returns an HTTP client that counts the requests it sends.
func countingClient(base http.RoundTripper) (*http.Client, *atomic.Int32) {
	var n atomic.Int32
	if base == nil {
		base = http.DefaultTransport
	}
	return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		n.Add(1)
		return base.RoundTrip(r)
	})}, &n
}

// TestManagerRequirementForChallengeLimits checks that Protected Resource
// Metadata can't make the Manager fetch from many servers, or from loopback
// ones for an agent that is not on loopback.
func TestManagerRequirementForChallengeLimits(t *testing.T) {
	t.Parallel()
	const agent = "https://agent.example.com"
	as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{})
	tests := []struct {
		name      string
		servers   int
		redirect  bool
		wantFetch int
	}{
		{name: "many authorization servers", servers: 50, wantFetch: 1 + maxAuthorizationServers*len(serverMetadataURLs("https://as0.example.com"))},
		{name: "metadata redirected to a loopback server", servers: 1, redirect: true, wantFetch: 1 + len(serverMetadataURLs("https://as0.example.com"))},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			hosts := remoteHosts{}
			var listed []string
			for i := range tc.servers {
				host := fmt.Sprintf("as%d.example.com", i)
				listed = append(listed, "https://"+host)
				hosts[host] = http.NotFoundHandler()
				if tc.redirect {
					hosts[host] = http.RedirectHandler(as.MetadataURL(), http.StatusFound)
				}
			}
			hosts["agent.example.com"] = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				body, err := json.Marshal(protectedResourceMetadata{Resource: agent, AuthorizationServers: listed})
				if err != nil {
					t.Errorf("json.Marshal() error = %v", err)
				}
				if _, err := w.Write(body); err != nil {
					t.Errorf("writing resource metadata error = %v", err)
				}
			})
			m := newManager(as)
			var fetched *atomic.Int32
			m.HTTPClient, fetched = countingClient(hosts)
			target, err := url.Parse(agent + "/v1")
			if err != nil {
				t.Fatal(err)
			}
			chs := ParseChallenges([]string{`Bearer resource_metadata="` + agent + `/prm"`})
			if _, err := m.RequirementForChallenge(t.Context(), target, chs); err == nil {
				t.Fatalf("Manager.RequirementForChallenge() error = nil, want a failure")
			}
			if got := int(fetched.Load()); got > tc.wantFetch {
				t.Fatalf("Manager.RequirementForChallenge() sent %d requests, want at most %d", got, tc.wantFetch)
			}
			if hits := as.Hits(wellKnownServerMetadata); hits != 0 {
				t.Fatalf("Manager.RequirementForChallenge() fetched loopback metadata %d times for %s, want none", hits, agent)
			}
		})
	}
}

// TestManagerRequirementForCardCachesFailures checks that cards whose
// requirement can't be resolved don't refetch metadata on every call, and that
// inline flows are resolved once for every card that declares them.
func TestManagerRequirementForCardCachesFailures(t *testing.T) {
	t.Parallel()
	unknown := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(unknown.Close)
	tests := []struct {
		name      string
		card      func() *a2a.AgentCard
		wantClass string
	}{
		{name: "inline flow resolved", card: func() *a2a.AgentCard { return inlineCard(false) }},
		{
			name: "inline flow without a registration",
			card: func() *a2a.AgentCard {
				card := inlineCard(false)
				card.SecuritySchemes["oauth"] = a2a.OAuth2SecurityScheme{Flows: a2a.AuthorizationCodeOAuthFlow{AuthorizationURL: unknown.URL + "/authorize", TokenURL: unknown.URL + "/token"}}
				return card
			},
			wantClass: "no client",
		},
		{
			name: "unreachable metadata URL",
			card: func() *a2a.AgentCard {
				card := inlineCard(false)
				card.SecuritySchemes["oauth"] = a2a.OAuth2SecurityScheme{Oauth2MetadataURL: unknown.URL + wellKnownServerMetadata, Flows: a2a.AuthorizationCodeOAuthFlow{AuthorizationURL: unknown.URL + "/authorize", TokenURL: unknown.URL + "/token"}}
				return card
			},
			wantClass: "failure",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			const agent = "http://127.0.0.1:1"
			m := managerWithGrant(t, "", nil, "", "")
			m.Clients = append(m.Clients, Registration{Authority: unknown.URL + "/other", ClientID: "other"})
			var now atomic.Pointer[time.Time]
			start := time.Now()
			now.Store(&start)
			m.now = func() time.Time { return *now.Load() }
			var fetched *atomic.Int32
			m.HTTPClient, fetched = countingClient(nil)
			resolve := func(card *a2a.AgentCard) {
				t.Helper()
				if _, err := m.RequirementForCard(t.Context(), card, agent); errClass(err) != tc.wantClass {
					t.Fatalf("Manager.RequirementForCard() error = %v (%s), want %q", err, errClass(err), tc.wantClass)
				}
			}
			card := tc.card()
			resolve(card)
			first := fetched.Load()
			for range 5 {
				resolve(card)
			}
			if got := fetched.Load(); got != first {
				t.Fatalf("metadata requests after resolving one card 6 times = %d, want %d as after the first", got, first)
			}
			if tc.wantClass == "failure" {
				return
			}
			for range 5 {
				resolve(tc.card())
			}
			if got := fetched.Load(); got != first {
				t.Fatalf("metadata requests after resolving 5 more copies of the card = %d, want %d as after the first", got, first)
			}
		})
	}

	t.Run("failures expire", func(t *testing.T) {
		t.Parallel()
		m := managerWithGrant(t, "", nil, "", "")
		var now atomic.Pointer[time.Time]
		start := time.Now()
		now.Store(&start)
		m.now = func() time.Time { return *now.Load() }
		var fetched *atomic.Int32
		m.HTTPClient, fetched = countingClient(nil)
		card := inlineCard(false)
		card.SecuritySchemes["oauth"] = a2a.OAuth2SecurityScheme{Oauth2MetadataURL: unknown.URL + wellKnownServerMetadata, Flows: a2a.AuthorizationCodeOAuthFlow{AuthorizationURL: unknown.URL + "/authorize", TokenURL: unknown.URL + "/token"}}
		for range 2 {
			if _, err := m.RequirementForCard(t.Context(), card, "http://127.0.0.1:1"); err == nil {
				t.Fatalf("Manager.RequirementForCard() error = nil, want a failure")
			}
		}
		first := fetched.Load()
		later := start.Add(failureCacheTTL + time.Second)
		now.Store(&later)
		if _, err := m.RequirementForCard(t.Context(), card, "http://127.0.0.1:1"); err == nil {
			t.Fatalf("Manager.RequirementForCard() error = nil, want a failure")
		}
		if got := fetched.Load(); got <= first {
			t.Fatalf("metadata requests after the failure expired = %d, want more than %d", got, first)
		}
	})
}

// cancelingLocker cancels the caller's context while it takes the lock, as
// when the caller gives up just as the lock is acquired.
type cancelingLocker struct {
	*MemoryStore
	cancel context.CancelFunc
}

func (l cancelingLocker) Lock(context.Context) (func(), error) {
	l.cancel()
	return func() {}, nil
}

// TestManagerLockHonorsCancel checks that a caller that gives up while waiting
// for the lock stops waiting, and that no refresh starts on its behalf.
func TestManagerLockHonorsCancel(t *testing.T) {
	t.Parallel()
	const agent = "https://agent.example.com"
	tests := []struct {
		name  string
		setup func(t *testing.T, m *Manager, cancel context.CancelFunc)
	}{
		{
			name: "lock held by another caller",
			setup: func(t *testing.T, m *Manager, cancel context.CancelFunc) {
				unlock, err := m.lock(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				time.AfterFunc(100*time.Millisecond, cancel)
				time.AfterFunc(time.Second, unlock)
			},
		},
		{
			name: "canceled as the lock is acquired",
			setup: func(_ *testing.T, m *Manager, cancel context.CancelFunc) {
				m.Store = cancelingLocker{MemoryStore: m.Store.(*MemoryStore), cancel: cancel}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := managerWithGrant(t, agent, nil, "", "rt")
			var refreshes *atomic.Int32
			m.HTTPClient, refreshes = countingClient(nil)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			tc.setup(t, m, cancel)
			start := time.Now()
			g, err := m.Grant(ctx, &Requirement{Endpoints: inlineEndpoints, Resource: agent})
			if !errors.Is(err, context.Canceled) || g != nil {
				t.Fatalf("Manager.Grant() = %v, %v, want an error wrapping %v", g, err, context.Canceled)
			}
			if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
				t.Fatalf("Manager.Grant() returned after %v, want it to stop waiting when the caller gave up", elapsed)
			}
			time.Sleep(1500 * time.Millisecond)
			if got := refreshes.Load(); got != 0 {
				t.Fatalf("refresh requests for a caller that gave up = %d, want 0", got)
			}
		})
	}
}

func TestManagerLockTimeouts(t *testing.T) {
	t.Parallel()
	if hold := detachedTimeout + storeTimeout; lockTimeout <= hold {
		t.Fatalf("lockTimeout = %v, want more than %v, the time a holder may keep the lock", lockTimeout, hold)
	}
}

// nilGetStore returns no grant and no error from Get, as a buggy store might.
type nilGetStore struct{ *MemoryStore }

func (nilGetStore) Get(context.Context, GrantKey) (*Grant, error) { return nil, nil }

func TestManagerNilGrantFromStore(t *testing.T) {
	t.Parallel()
	const agent = "https://agent.example.com"
	m := managerWithGrant(t, agent, nil, "at", "rt")
	m.Store = nilGetStore{m.Store.(*MemoryStore)}
	req := &Requirement{Endpoints: inlineEndpoints, Resource: agent}
	if g, err := m.Grant(t.Context(), req); g != nil || err != nil {
		t.Fatalf("Manager.Grant() = %v, %v, want nil, nil", g, err)
	}
	if g, err := m.GrantForResource(t.Context(), agent); g != nil || err != nil {
		t.Fatalf("Manager.GrantForResource() = %v, %v, want nil, nil", g, err)
	}
	key := NewGrantKey(inlineEndpoints.Issuer, "c", agent, nil)
	if g, err := m.refreshRejected(t.Context(), key, "at"); g != nil || err != nil {
		t.Fatalf("Manager.refreshRejected() = %v, %v, want nil, nil", g, err)
	}
	if err := m.Revoke(t.Context(), key); !errors.Is(err, ErrGrantNotFound) {
		t.Fatalf("Manager.Revoke() error = %v, want %v", err, ErrGrantNotFound)
	}
}

func TestManagerLearnedGrantsBounded(t *testing.T) {
	t.Parallel()
	m := managerWithGrant(t, "", nil, "", "")
	for i := range 3 * maxLearnedGrants {
		m.learn(NewGrantKey(inlineEndpoints.Issuer, "c", fmt.Sprintf("https://agent%d.example.com", i), nil))
	}
	if got := len(m.learned.entries); got > maxLearnedGrants {
		t.Fatalf("learned grants = %d, want at most %d", got, maxLearnedGrants)
	}
	key := NewGrantKey(inlineEndpoints.Issuer, "c", "https://agent.example.com", nil)
	m.learn(key)
	if err := m.Delete(t.Context(), key); err != nil {
		t.Fatalf("Manager.Delete() error = %v", err)
	}
	if _, ok := m.learned.get(key.Resource); ok {
		t.Fatalf("learned grants after Manager.Delete() still hold %s", key.Resource)
	}
}

func TestManagerRequirementForChallengeLoopbackEndpoints(t *testing.T) {
	t.Parallel()
	serve := func(body string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if _, err := w.Write([]byte(body)); err != nil {
				t.Errorf("writing the response error = %v", err)
			}
		})
	}
	hosts := remoteHosts{
		"agent.example.com": serve(`{"resource":"https://agent.example.com","authorization_servers":["https://as.example.com"]}`),
		"as.example.com":    serve(`{"issuer":"https://as.example.com","authorization_endpoint":"https://as.example.com/authorize","token_endpoint":"http://127.0.0.1:9/token"}`),
	}
	m := &Manager{
		HTTPClient: &http.Client{Transport: hosts},
		Clients:    []Registration{{Authority: "https://as.example.com", ClientID: "c"}},
	}
	target, err := url.Parse("https://agent.example.com")
	if err != nil {
		t.Fatal(err)
	}
	chs := []Challenge{{Scheme: "Bearer", Params: map[string]string{"resource_metadata": "https://agent.example.com/.well-known/oauth-protected-resource"}}}
	r, err := m.RequirementForChallenge(t.Context(), target, chs)
	if err == nil || !strings.Contains(err.Error(), "loopback endpoint") {
		t.Fatalf("Manager.RequirementForChallenge() = %v, %v, want the loopback token endpoint refused", r, err)
	}
}

func TestManagerRequirementForCardInlineDefaultClient(t *testing.T) {
	t.Parallel()
	for name, card := range map[string]oauthtest.CardSecurity{"authorization code": oauthtest.CardAuthCode, "device code": oauthtest.CardDeviceCode} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{CIMD: true})
			agent := oauthtest.NewProtectedAgent(t, as, oauthtest.AgentOptions{Card: card, Scopes: []string{"a2a.invoke"}})
			m := &Manager{DefaultClientID: testCIMDClientID}
			r, err := m.RequirementForCard(t.Context(), fetchCard(t, agent.URL), agent.URL)
			if err != nil || r == nil {
				t.Fatalf("Manager.RequirementForCard() = %v, %v, want a requirement", r, err)
			}
			if r.Endpoints.Issuer != as.URL || !r.Endpoints.CIMDSupported {
				t.Fatalf("Manager.RequirementForCard() endpoints = %+v, want %s's metadata", r.Endpoints, as.URL)
			}
			key, choice, err := m.Key(r, nil)
			if err != nil || key.ClientID != testCIMDClientID || choice.Source != ClientSourceCIMD {
				t.Fatalf("Manager.Key() = %+v, %+v, %v, want the default client", key, choice, err)
			}
		})
	}
}

func TestManagerGrantScopesNotListed(t *testing.T) {
	t.Parallel()
	var refreshForms []url.Values
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm() error = %v", err)
		}
		if r.PostForm.Get("grant_type") == grantTypeRefreshToken {
			mu.Lock()
			refreshForms = append(refreshForms, r.PostForm)
			mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"access_token":"at","token_type":"Bearer","expires_in":3600,"refresh_token":"rt","scope":"api://a2a/invoke"}`)); err != nil {
			t.Errorf("writing the response error = %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	e := Endpoints{Issuer: srv.URL, Token: srv.URL + "/token", Authorization: srv.URL + "/authorize"}
	m := &Manager{Clients: []Registration{{Authority: e.Issuer, ClientID: "c", Scopes: []string{"openid", "offline_access", "api://a2a/invoke"}}}}
	req := &Requirement{Endpoints: e, Resource: "https://agent.example.com"}
	ar, err := m.NewAuthorizationRequest(req, testRedirectURI, nil)
	if err != nil {
		t.Fatalf("Manager.NewAuthorizationRequest() error = %v", err)
	}
	signedIn, err := m.ExchangeCode(t.Context(), ar, "code")
	if err != nil {
		t.Fatalf("Manager.ExchangeCode() error = %v", err)
	}
	got, err := m.Grant(t.Context(), req)
	if err != nil || got == nil || got.AccessToken != "at" {
		t.Fatalf("Manager.Grant() after signing in = %+v, %v, want the stored grant", got, err)
	}
	if _, err := m.tokens().Refresh(t.Context(), signedIn); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(refreshForms) != 1 || refreshForms[0].Has("scope") {
		t.Fatalf("refresh requests = %v, want one without a scope", refreshForms)
	}
}

func TestManagerRequirementForCardCanceledNotCached(t *testing.T) {
	t.Parallel()
	as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{})
	agent := oauthtest.NewProtectedAgent(t, as, oauthtest.AgentOptions{Card: oauthtest.CardAuthCode, Scopes: []string{"a2a.invoke"}})
	m := newManager(as)
	card := fetchCard(t, agent.URL)
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := m.RequirementForCard(canceled, card, agent.URL); err == nil {
		t.Fatalf("Manager.RequirementForCard() with a canceled context error = nil, want one")
	}
	r, err := m.RequirementForCard(t.Context(), card, agent.URL)
	if err != nil || r == nil {
		t.Fatalf("Manager.RequirementForCard() after a canceled call = %v, %v, want a requirement", r, err)
	}
}

// TestManagerRequirementForCardOptionalFailure checks that a card that also
// accepts an API key requires nothing when its OAuth metadata can't be read,
// and that this answer is kept only like a failure: not at all for a canceled
// call, and for failureCacheTTL otherwise.
func TestManagerRequirementForCardOptionalFailure(t *testing.T) {
	t.Parallel()
	var fail atomic.Bool
	var srvURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = fmt.Fprintf(w, `{"issuer":%q,"authorization_endpoint":%q,"token_endpoint":%q}`, srvURL, srvURL+"/authorize", srvURL+"/token")
	}))
	t.Cleanup(srv.Close)
	srvURL = srv.URL
	card := &a2a.AgentCard{
		Name: "optional",
		SecuritySchemes: a2a.NamedSecuritySchemes{
			"oidc": a2a.OpenIDConnectSecurityScheme{OpenIDConnectURL: srv.URL + "/.well-known/openid-configuration"},
			"key":  a2a.APIKeySecurityScheme{Name: "X-API-Key", Location: a2a.APIKeySecuritySchemeLocationHeader},
		},
		SecurityRequirements: a2a.SecurityRequirementsOptions{{"oidc": nil}, {"key": nil}},
	}
	var now atomic.Pointer[time.Time]
	start := time.Now()
	now.Store(&start)
	m := &Manager{now: func() time.Time { return *now.Load() }}

	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if r, err := m.RequirementForCard(canceled, card, srv.URL); r != nil || err != nil {
		t.Fatalf("Manager.RequirementForCard() with a canceled context = %v, %v, want nil, nil", r, err)
	}
	if r, err := m.RequirementForCard(t.Context(), card, srv.URL); err != nil || r == nil || !r.Optional {
		t.Fatalf("Manager.RequirementForCard() after a canceled call = %v, %v, want an optional requirement", r, err)
	}

	other := *card
	fail.Store(true)
	if r, err := m.RequirementForCard(t.Context(), &other, srv.URL); r != nil || err != nil {
		t.Fatalf("Manager.RequirementForCard() with unavailable metadata = %v, %v, want nil, nil", r, err)
	}
	fail.Store(false)
	if r, err := m.RequirementForCard(t.Context(), &other, srv.URL); r != nil || err != nil {
		t.Fatalf("Manager.RequirementForCard() within failureCacheTTL = %v, %v, want the cached nil, nil", r, err)
	}
	later := start.Add(failureCacheTTL + time.Second)
	now.Store(&later)
	if r, err := m.RequirementForCard(t.Context(), &other, srv.URL); err != nil || r == nil || !r.Optional {
		t.Fatalf("Manager.RequirementForCard() after failureCacheTTL = %v, %v, want an optional requirement", r, err)
	}
}

// TestManagerStepUp signs in again after the agent asks for more scopes and
// checks that the wider grant replaces the narrower one, also for a server that
// leaves scopes such as offline_access out of its response.
func TestManagerStepUp(t *testing.T) {
	t.Parallel()
	const agent = "https://agent.example.com"
	card := &a2a.AgentCard{
		Name: "stepup",
		SecuritySchemes: a2a.NamedSecuritySchemes{
			"oauth": a2a.OAuth2SecurityScheme{Flows: a2a.AuthorizationCodeOAuthFlow{AuthorizationURL: inlineEndpoints.Authorization, TokenURL: inlineEndpoints.Token}},
		},
		SecurityRequirements: a2a.SecurityRequirementsOptions{{"oauth": {"a", "offline_access"}}},
	}
	for _, granted := range [][]string{nil, {"a", "b"}} {
		t.Run(fmt.Sprintf("granted %v", granted), func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			m := managerWithGrant(t, agent, []string{"a", "offline_access"}, "token-a", "")
			attached := func() string {
				req := &a2aclient.Request{BaseURL: agent, ProtocolBinding: a2a.TransportProtocolJSONRPC, Card: card, ServiceParams: a2aclient.ServiceParams{}}
				if _, _, err := m.Interceptor().Before(ctx, req); err != nil {
					t.Fatalf("Before() error = %v", err)
				}
				return strings.Join(req.ServiceParams.Get("authorization"), ",")
			}
			if got := attached(); got != "Bearer token-a" {
				t.Fatalf("first call Authorization = %q, want the stored grant", got)
			}
			r, err := m.RequirementForCard(ctx, card, agent)
			if err != nil {
				t.Fatal(err)
			}
			g1, err := m.Grant(ctx, r)
			if err != nil || g1 == nil {
				t.Fatalf("Manager.Grant() = %v, %v", g1, err)
			}
			ir := rejectedGrantError(g1, ParseChallenges([]string{`Bearer error="insufficient_scope", scope="b"`}), errors.New("401"))
			key2, _, err := m.Key(ir.Requirement, nil)
			if err != nil {
				t.Fatal(err)
			}
			g2 := &Grant{Key: key2, GrantedScopes: granted, AccessToken: "token-ab", ExpiresAt: time.Now().Add(time.Hour), Endpoints: g1.Endpoints}
			if err := m.save(ctx, g2); err != nil {
				t.Fatal(err)
			}
			if got := attached(); got != "Bearer token-ab" {
				t.Fatalf("call after signing in for more scopes Authorization = %q, want the wider grant", got)
			}
			grants, err := m.List(ctx)
			if err != nil || len(grants) != 1 || !grants[0].Key.Equal(key2) {
				t.Fatalf("stored grants = %v, %v; want only the wider one", grants, err)
			}
			fresh := &Manager{Clients: m.Clients, Store: m.Store}
			if g, err := fresh.GrantForResource(ctx, agent); err != nil || g == nil || g.AccessToken != "token-ab" {
				t.Fatalf("GrantForResource() in a new Manager = %v, %v; want the wider grant", g, err)
			}
		})
	}
}

// TestResolveGrantWidest checks that, of grants for one resource, the one whose
// requested scopes include the others' wins, and that otherwise none does.
func TestResolveGrantWidest(t *testing.T) {
	t.Parallel()
	const agent = "https://agent.example.com"
	grant := func(issuer string, scopes ...string) Grant {
		return Grant{Key: NewGrantKey(issuer, "c", agent, scopes)}
	}
	tests := []struct {
		name   string
		grants []Grant
		want   []string
	}{
		{name: "wider scopes win", grants: []Grant{grant("https://as", "a"), grant("https://as", "a", "b")}, want: []string{"a", "b"}},
		{name: "disjoint scopes tie", grants: []Grant{grant("https://as", "a"), grant("https://as", "b")}},
		{name: "different issuers tie", grants: []Grant{grant("https://as", "a"), grant("https://other", "a", "b")}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := resolveGrant(t.Context(), tc.grants, agent+"/v1", nil)
			if tc.want == nil {
				if got != nil {
					t.Fatalf("resolveGrant() = %v, want none", got)
				}
				return
			}
			if got == nil || !slices.Equal(got.Scopes, tc.want) {
				t.Fatalf("resolveGrant() = %v, want scopes %v", got, tc.want)
			}
		})
	}
}
