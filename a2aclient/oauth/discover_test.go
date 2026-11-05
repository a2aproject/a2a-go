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
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient/agentcard"
	"github.com/a2aproject/a2a-go/v2/a2aclient/oauth/oauthtest"
	"github.com/google/go-cmp/cmp"
)

func testFetcher() fetcher { return fetcher{client: &http.Client{}} }

// jsonServer serves body as JSON at every path.
func jsonServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(body)); err != nil {
			t.Errorf("writing the response error = %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// serverEndpoints returns the endpoints an [oauthtest.AuthServer] publishes.
func serverEndpoints(as *oauthtest.AuthServer, cimd bool) Endpoints {
	return Endpoints{
		Issuer:              as.URL,
		Authorization:       as.AuthorizationURL(),
		Token:               as.URL + "/token",
		DeviceAuthorization: as.URL + "/device",
		Revocation:          as.URL + "/revoke",
		CIMDSupported:       cimd,
	}
}

// tenantServer serves RFC 8414 metadata for several issuers on one host, one
// per tenant path.
func tenantServer(t *testing.T, tenants ...string) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, tenant := range tenants {
			if r.URL.Path != wellKnownServerMetadata+tenant {
				continue
			}
			issuer := srv.URL + tenant
			if _, err := fmt.Fprintf(w, `{"issuer":%q,"authorization_endpoint":%q,"token_endpoint":%q}`, issuer, issuer+"/authorize", issuer+"/token"); err != nil {
				t.Errorf("writing the response error = %v", err)
			}
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestRequirementFromCard(t *testing.T) {
	t.Parallel()
	as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{})
	scopes := []string{"a2a.invoke"}
	tests := []struct {
		name string
		card oauthtest.CardSecurity
		want *Requirement
	}{
		{
			name: "oauth2 metadata URL",
			card: oauthtest.CardMetadataURL,
			want: &Requirement{Endpoints: serverEndpoints(as, false), Scopes: scopes},
		},
		{
			name: "inline authorization code flow",
			card: oauthtest.CardAuthCode,
			want: &Requirement{Endpoints: Endpoints{Authorization: as.URL + "/authorize", Token: as.URL + "/token"}, Scopes: scopes},
		},
		{
			name: "inline device code flow",
			card: oauthtest.CardDeviceCode,
			want: &Requirement{Endpoints: Endpoints{DeviceAuthorization: as.URL + "/device", Token: as.URL + "/token"}, Scopes: scopes, PreferDevice: true},
		},
		{
			name: "openIdConnect",
			card: oauthtest.CardOIDC,
			want: &Requirement{Endpoints: serverEndpoints(as, false), Scopes: scopes},
		},
		{
			name: "no security",
			card: oauthtest.CardNone,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			agent := oauthtest.NewProtectedAgent(t, as, oauthtest.AgentOptions{Card: tc.card, Scopes: scopes})
			card, err := agentcard.DefaultResolver.Resolve(t.Context(), agent.URL)
			if err != nil {
				t.Fatalf("Resolve(%q) error = %v", agent.URL, err)
			}
			if tc.want != nil {
				tc.want.Resource = NormalizeResource(agent.URL)
			}
			got, err := testFetcher().requirementFromCard(t.Context(), card, agent.URL, nil)
			if err != nil {
				t.Fatalf("requirementFromCard() error = %v", err)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("requirementFromCard() wrong result (-want +got) diff = %s", diff)
			}
		})
	}
}

func TestRequirementFromCardOptions(t *testing.T) {
	t.Parallel()
	const as = "https://as.example.com"
	authCode := a2a.AuthorizationCodeOAuthFlow{AuthorizationURL: as + "/authorize", TokenURL: as + "/token"}
	deviceCode := a2a.DeviceCodeOAuthFlow{DeviceAuthorizationURL: as + "/device", TokenURL: as + "/token"}
	schemes := a2a.NamedSecuritySchemes{
		"code":        a2a.OAuth2SecurityScheme{Flows: authCode},
		"codePtr":     a2a.OAuth2SecurityScheme{Flows: &authCode},
		"devicePtr":   a2a.OAuth2SecurityScheme{Flows: &deviceCode},
		"noToken":     a2a.OAuth2SecurityScheme{Flows: a2a.AuthorizationCodeOAuthFlow{AuthorizationURL: as + "/authorize"}},
		"plainHTTP":   a2a.OAuth2SecurityScheme{Flows: a2a.AuthorizationCodeOAuthFlow{AuthorizationURL: "http://as.example.com/authorize", TokenURL: "http://as.example.com/token"}},
		"credentials": a2a.OAuth2SecurityScheme{Flows: a2a.ClientCredentialsOAuthFlow{TokenURL: as + "/token"}},
		"key":         a2a.APIKeySecurityScheme{Name: "X-Key", Location: a2a.APIKeySecuritySchemeLocationHeader},
	}
	codeReq := &Requirement{
		Endpoints: Endpoints{Authorization: as + "/authorize", Token: as + "/token"},
		Resource:  "https://agent.example.com",
		Scopes:    []string{"read"},
	}
	optional := *codeReq
	optional.Optional = true
	tests := []struct {
		name    string
		options a2a.SecurityRequirementsOptions
		want    *Requirement
		wantErr string
	}{
		{name: "oauth only", options: a2a.SecurityRequirementsOptions{{"code": {"read"}}}, want: codeReq},
		{name: "oauth or no credentials", options: a2a.SecurityRequirementsOptions{{"code": {"read"}}, {}}, want: &optional},
		{name: "no credentials or oauth", options: a2a.SecurityRequirementsOptions{{}, {"code": {"read"}}}, want: &optional},
		{name: "oauth or api key", options: a2a.SecurityRequirementsOptions{{"code": {"read"}}, {"key": nil}}, want: &optional},
		{name: "api key or oauth", options: a2a.SecurityRequirementsOptions{{"key": nil}, {"code": {"read"}}}, want: &optional},
		{name: "oauth and api key together", options: a2a.SecurityRequirementsOptions{{"code": nil, "key": nil}}},
		{name: "undefined scheme", options: a2a.SecurityRequirementsOptions{{"missing": nil}}},
		{name: "client credentials only", options: a2a.SecurityRequirementsOptions{{"credentials": nil}}},
		{name: "pointer authorization code flow", options: a2a.SecurityRequirementsOptions{{"codePtr": {"read"}}}, want: codeReq},
		{
			name:    "pointer device code flow",
			options: a2a.SecurityRequirementsOptions{{"devicePtr": {"read"}}},
			want: &Requirement{
				Endpoints:    Endpoints{DeviceAuthorization: as + "/device", Token: as + "/token"},
				Resource:     "https://agent.example.com",
				Scopes:       []string{"read"},
				PreferDevice: true,
			},
		},
		{name: "broken options before a usable one", options: a2a.SecurityRequirementsOptions{{"noToken": nil}, {"plainHTTP": nil}, {"code": {"read"}}}, want: codeReq},
		{name: "broken option or no credentials", options: a2a.SecurityRequirementsOptions{{"noToken": nil}, {}}},
		{name: "only a broken option", options: a2a.SecurityRequirementsOptions{{"noToken": nil}}, wantErr: `security scheme "noToken": inline flow has no token endpoint`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			card := &a2a.AgentCard{SecuritySchemes: schemes, SecurityRequirements: tc.options}
			got, err := testFetcher().requirementFromCard(t.Context(), card, "https://agent.example.com/", nil)
			if tc.wantErr != "" {
				if err == nil || err.Error() != tc.wantErr {
					t.Fatalf("requirementFromCard() error = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("requirementFromCard() error = %v", err)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("requirementFromCard() wrong result (-want +got) diff = %s", diff)
			}
		})
	}
}

func TestRequirementFromCardNilCard(t *testing.T) {
	t.Parallel()
	if got, err := testFetcher().requirementFromCard(t.Context(), nil, "https://agent.example.com", nil); got != nil || err != nil {
		t.Fatalf("requirementFromCard(nil) = %+v, %v, want nil, nil", got, err)
	}
}

func TestRequirementFromCardMetadataURLFlows(t *testing.T) {
	t.Parallel()
	as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{})
	tests := []struct {
		name  string
		flows a2a.OAuthFlows
		want  *Requirement
	}{
		{
			name:  "authorization code",
			flows: a2a.AuthorizationCodeOAuthFlow{AuthorizationURL: as.URL + "/authorize", TokenURL: as.URL + "/token"},
			want:  &Requirement{Endpoints: serverEndpoints(as, false), Resource: "http://127.0.0.1:8080"},
		},
		{
			name:  "device code",
			flows: &a2a.DeviceCodeOAuthFlow{DeviceAuthorizationURL: as.URL + "/device", TokenURL: as.URL + "/token"},
			want:  &Requirement{Endpoints: serverEndpoints(as, false), Resource: "http://127.0.0.1:8080", PreferDevice: true},
		},
		{name: "client credentials", flows: a2a.ClientCredentialsOAuthFlow{TokenURL: as.URL + "/token"}},
		{name: "password", flows: a2a.PasswordOAuthFlow{TokenURL: as.URL + "/token"}},
		{name: "implicit", flows: a2a.ImplicitOAuthFlow{AuthorizationURL: as.URL + "/authorize"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			card := &a2a.AgentCard{
				SecuritySchemes:      a2a.NamedSecuritySchemes{"oauth": a2a.OAuth2SecurityScheme{Oauth2MetadataURL: as.MetadataURL(), Flows: tc.flows}},
				SecurityRequirements: a2a.SecurityRequirementsOptions{{"oauth": nil}},
			}
			got, err := testFetcher().requirementFromCard(t.Context(), card, "http://127.0.0.1:8080", nil)
			if err != nil {
				t.Fatalf("requirementFromCard() error = %v", err)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("requirementFromCard() wrong result (-want +got) diff = %s", diff)
			}
		})
	}
}

func TestRequirementFromCardRejectsBadMetadata(t *testing.T) {
	t.Parallel()
	srv := jsonServer(t, `{"issuer":"https://elsewhere.example.com","token_endpoint":"https://elsewhere.example.com/token"}`)
	card := &a2a.AgentCard{
		SecuritySchemes: a2a.NamedSecuritySchemes{"oauth": a2a.OAuth2SecurityScheme{
			Oauth2MetadataURL: srv.URL + "/.well-known/oauth-authorization-server",
			Flows:             a2a.AuthorizationCodeOAuthFlow{AuthorizationURL: srv.URL + "/authorize", TokenURL: srv.URL + "/token"},
		}},
		SecurityRequirements: a2a.SecurityRequirementsOptions{{"oauth": nil}},
	}
	_, err := testFetcher().requirementFromCard(t.Context(), card, "http://127.0.0.1:8080", nil)
	if err == nil || !strings.Contains(err.Error(), "does not publish metadata there") {
		t.Fatalf("requirementFromCard() error = %v, want the issuer mismatch", err)
	}
}

func TestResolveInlineIssuer(t *testing.T) {
	t.Parallel()
	pathIssuer := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{IssuerPath: "/oauth2/default", CIMD: true})
	splitHosts := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{SeparateAuthorizationHost: true})
	tenants := tenantServer(t, "/tenant-1/v2.0", "/tenant-2/v2.0")
	unregistered := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{})
	regs := []Registration{
		{Authority: pathIssuer.URL, ClientID: "path-client"},
		{Authority: splitHosts.URL, ClientID: "split-client"},
		{Authority: tenants.URL + "/tenant-1/v2.0", ClientID: "tenant-1-client"},
		{Authority: tenants.URL + "/tenant-2/v2.0", ClientID: "tenant-2-client"},
	}
	tenantEndpoints := func(tenant string) Endpoints {
		issuer := tenants.URL + tenant
		return Endpoints{Issuer: issuer, Authorization: issuer + "/authorize", Token: issuer + "/token"}
	}
	tests := []struct {
		name       string
		card       Endpoints
		want       Endpoints
		wantClient string
		wantErr    *NoClientError
	}{
		{
			name:       "issuer with a path",
			card:       Endpoints{Authorization: pathIssuer.URL + "/authorize", Token: pathIssuer.URL + "/token"},
			want:       serverEndpoints(pathIssuer, true),
			wantClient: "path-client",
		},
		{
			name:       "device flow",
			card:       Endpoints{DeviceAuthorization: pathIssuer.URL + "/device", Token: pathIssuer.URL + "/token"},
			want:       serverEndpoints(pathIssuer, true),
			wantClient: "path-client",
		},
		{
			name:       "authorization endpoint on another host",
			card:       Endpoints{Authorization: splitHosts.AuthorizationURL(), Token: splitHosts.URL + "/token"},
			want:       serverEndpoints(splitHosts, false),
			wantClient: "split-client",
		},
		{
			name:       "first tenant on a shared host",
			card:       Endpoints{Authorization: tenants.URL + "/tenant-1/v2.0/authorize", Token: tenants.URL + "/tenant-1/v2.0/token"},
			want:       tenantEndpoints("/tenant-1/v2.0"),
			wantClient: "tenant-1-client",
		},
		{
			name:       "second tenant on a shared host",
			card:       Endpoints{Authorization: tenants.URL + "/tenant-2/v2.0/authorize", Token: tenants.URL + "/tenant-2/v2.0/token"},
			want:       tenantEndpoints("/tenant-2/v2.0"),
			wantClient: "tenant-2-client",
		},
		{
			name:    "authorization endpoint differs from the metadata",
			card:    Endpoints{Authorization: "https://elsewhere.example.com/authorize", Token: pathIssuer.URL + "/token"},
			wantErr: &NoClientError{Issuers: []string{strings.TrimSuffix(pathIssuer.URL, "/oauth2/default")}},
		},
		{
			name:    "no registration for the server",
			card:    Endpoints{Authorization: unregistered.URL + "/authorize", Token: unregistered.URL + "/token"},
			wantErr: &NoClientError{Issuers: []string{unregistered.URL}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := testFetcher().resolveInlineIssuer(t.Context(), regs, tc.card, false)
			if tc.wantErr != nil {
				var nc *NoClientError
				if !errors.As(err, &nc) {
					t.Fatalf("resolveInlineIssuer() error = %v, want a NoClientError", err)
				}
				if diff := cmp.Diff(tc.wantErr, nc); diff != "" {
					t.Fatalf("resolveInlineIssuer() error wrong result (-want +got) diff = %s", diff)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveInlineIssuer() error = %v", err)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("resolveInlineIssuer() wrong result (-want +got) diff = %s", diff)
			}
			choice, err := selectClient(regs, "", got)
			if err != nil || choice.ClientID != tc.wantClient {
				t.Fatalf("selectClient() = %+v, %v, want client %q", choice, err, tc.wantClient)
			}
		})
	}
}

func TestResolveInlineIssuerDefaultClient(t *testing.T) {
	t.Parallel()
	pathIssuer := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{IssuerPath: "/oauth2/default", CIMD: true})
	originIssuer := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{CIMD: true})
	noCIMD := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{})
	tests := []struct {
		name string
		card Endpoints
		cimd bool
		want *Endpoints
	}{
		{
			name: "issuer with a path",
			card: Endpoints{Authorization: pathIssuer.URL + "/authorize", Token: pathIssuer.URL + "/token"},
			cimd: true,
			want: new(serverEndpoints(pathIssuer, true)),
		},
		{
			name: "issuer at the origin",
			card: Endpoints{DeviceAuthorization: originIssuer.URL + "/device", Token: originIssuer.URL + "/token"},
			cimd: true,
			want: new(serverEndpoints(originIssuer, true)),
		},
		{
			name: "no default client",
			card: Endpoints{Authorization: pathIssuer.URL + "/authorize", Token: pathIssuer.URL + "/token"},
		},
		{
			name: "server without CIMD",
			card: Endpoints{Authorization: noCIMD.URL + "/authorize", Token: noCIMD.URL + "/token"},
			cimd: true,
		},
		{
			name: "authorization endpoint differs from the metadata",
			card: Endpoints{Authorization: "https://elsewhere.example.com/authorize", Token: originIssuer.URL + "/token"},
			cimd: true,
		},
		{
			name: "token endpoint the metadata doesn't name",
			card: Endpoints{Authorization: originIssuer.URL + "/authorize", Token: originIssuer.URL + "/other/token"},
			cimd: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := testFetcher().resolveInlineIssuer(t.Context(), nil, tc.card, tc.cimd)
			if tc.want == nil {
				var nc *NoClientError
				if !errors.As(err, &nc) {
					t.Fatalf("resolveInlineIssuer() = %+v, %v, want a NoClientError", got, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveInlineIssuer() error = %v", err)
			}
			if diff := cmp.Diff(*tc.want, got); diff != "" {
				t.Fatalf("resolveInlineIssuer() wrong result (-want +got) diff = %s", diff)
			}
			choice, err := selectClient(nil, testCIMDClientID, got)
			if err != nil || choice.Source != ClientSourceCIMD {
				t.Fatalf("selectClient() = %+v, %v, want the default client", choice, err)
			}
		})
	}
}

func TestInlineIssuerCandidates(t *testing.T) {
	t.Parallel()
	tests := []struct {
		token string
		want  []string
	}{
		{"https://as.example.com/token", []string{"https://as.example.com"}},
		{"https://as.example.com/oauth2/default/v1/token", []string{"https://as.example.com/oauth2/default/v1", "https://as.example.com/oauth2/default", "https://as.example.com/oauth2", "https://as.example.com"}},
		{"https://as.example.com/a/b/c/d/e/f/token", []string{"https://as.example.com/a/b/c/d", "https://as.example.com/a/b/c", "https://as.example.com/a/b", "https://as.example.com/a", "https://as.example.com"}},
		{"not a url", nil},
	}
	for _, tc := range tests {
		t.Run(tc.token, func(t *testing.T) {
			t.Parallel()
			if diff := cmp.Diff(tc.want, inlineIssuerCandidates(tc.token)); diff != "" {
				t.Fatalf("inlineIssuerCandidates(%q) wrong result (-want +got) diff = %s", tc.token, diff)
			}
		})
	}
}

func TestDiscoverFromChallenge(t *testing.T) {
	t.Parallel()
	as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{})
	tests := []struct {
		name  string
		agent oauthtest.AgentOptions
	}{
		{name: "resource_metadata in the challenge", agent: oauthtest.AgentOptions{}},
		{name: "well-known location at the origin", agent: oauthtest.AgentOptions{OmitResourceMetadata: true}},
		{name: "resource_metadata below a base path", agent: oauthtest.AgentOptions{BasePath: "/a2a/v1"}},
		{name: "well-known location below a base path", agent: oauthtest.AgentOptions{OmitResourceMetadata: true, BasePath: "/a2a/v1"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.agent.Scopes = []string{"a2a.invoke"}
			agent := oauthtest.NewProtectedAgent(t, as, tc.agent)
			resp, err := http.Post(agent.URL+"/message:send", "application/json", strings.NewReader("{}"))
			if err != nil {
				t.Fatalf("POST %s error = %v", agent.URL, err)
			}
			if err := resp.Body.Close(); err != nil {
				t.Fatalf("closing the response error = %v", err)
			}
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("POST %s status = %d, want 401", agent.URL, resp.StatusCode)
			}
			target, err := url.Parse(agent.URL + "/message:send")
			if err != nil {
				t.Fatalf("url.Parse() error = %v", err)
			}
			got, err := testFetcher().discoverFromChallenge(t.Context(), target, ParseChallenges(resp.Header.Values("WWW-Authenticate")))
			if err != nil {
				t.Fatalf("discoverFromChallenge() error = %v", err)
			}
			want := &challengeTarget{
				resource: &protectedResourceMetadata{Resource: agent.URL, AuthorizationServers: []string{as.URL}, ScopesSupported: []string{"a2a.invoke"}},
				scopes:   []string{"a2a.invoke"},
			}
			if diff := cmp.Diff(want, got, cmp.AllowUnexported(challengeTarget{})); diff != "" {
				t.Fatalf("discoverFromChallenge() wrong result (-want +got) diff = %s", diff)
			}
		})
	}
}

func TestResourceMetadataErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		status   int
		body     string
		explicit bool
		wantErr  string
	}{
		{name: "other resource", body: `{"resource":"https://other.example.com","authorization_servers":["https://as.example.com"]}`, wantErr: "describes"},
		{name: "no resource", body: `{"authorization_servers":["https://as.example.com"]}`, wantErr: "has no resource"},
		{name: "no authorization servers", body: `{"resource":"%s"}`, wantErr: "lists no authorization servers"},
		{name: "explicit URL not found", status: http.StatusNotFound, explicit: true, wantErr: "HTTP 404"},
		{name: "explicit URL forbidden", status: http.StatusForbidden, explicit: true, wantErr: "HTTP 403"},
		{name: "explicit URL serves HTML", body: "<html></html>", explicit: true, wantErr: "parsing"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var srv *httptest.Server
			srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tc.status != 0 {
					w.WriteHeader(tc.status)
					return
				}
				if _, err := w.Write([]byte(strings.ReplaceAll(tc.body, "%s", srv.URL))); err != nil {
					t.Errorf("writing the response error = %v", err)
				}
			}))
			t.Cleanup(srv.Close)
			target, err := url.Parse(srv.URL)
			if err != nil {
				t.Fatalf("url.Parse() error = %v", err)
			}
			metadataURL := ""
			if tc.explicit {
				metadataURL = srv.URL + "/.well-known/oauth-protected-resource"
			}
			_, err = testFetcher().resourceMetadata(t.Context(), target, metadataURL)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("resourceMetadata() error = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}

	t.Run("cross-origin redirect", func(t *testing.T) {
		t.Parallel()
		other := jsonServer(t, `{"resource":"https://agent.example.com","authorization_servers":["https://as.example.com"]}`)
		redirect := httptest.NewServer(http.RedirectHandler(other.URL+"/prm", http.StatusFound))
		t.Cleanup(redirect.Close)
		target, err := url.Parse(redirect.URL)
		if err != nil {
			t.Fatalf("url.Parse() error = %v", err)
		}
		_, err = testFetcher().resourceMetadata(t.Context(), target, redirect.URL+"/prm")
		if err == nil || !strings.Contains(err.Error(), "another origin") {
			t.Fatalf("resourceMetadata() error = %v, want the cross-origin redirect refused", err)
		}
	})
}

func TestResourceMetadataGuessedLocations(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		status  int
		body    string
		rootPRM bool
	}{
		{name: "not found", status: http.StatusNotFound},
		{name: "unauthorized", status: http.StatusUnauthorized},
		{name: "forbidden", status: http.StatusForbidden},
		{name: "server error", status: http.StatusInternalServerError},
		{name: "HTML page", body: "<!doctype html><html></html>"},
		{name: "forbidden below the path, published at the origin", status: http.StatusForbidden, rootPRM: true},
		{name: "HTML below the path, published at the origin", body: "<html></html>", rootPRM: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var srv *httptest.Server
			srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body := tc.body
				if tc.rootPRM && r.URL.Path == wellKnownResourceMetadata {
					body = fmt.Sprintf(`{"resource":%q,"authorization_servers":["https://as.example.com"]}`, srv.URL)
				} else if tc.status != 0 {
					w.WriteHeader(tc.status)
					return
				}
				if _, err := w.Write([]byte(body)); err != nil {
					t.Errorf("writing the response error = %v", err)
				}
			}))
			t.Cleanup(srv.Close)
			target, err := url.Parse(srv.URL + "/a2a/v1/message:send")
			if err != nil {
				t.Fatalf("url.Parse() error = %v", err)
			}
			got, err := testFetcher().resourceMetadata(t.Context(), target, "")
			if !tc.rootPRM {
				if !errors.Is(err, errNoResourceMetadata) {
					t.Fatalf("resourceMetadata() error = %v, want %v", err, errNoResourceMetadata)
				}
				return
			}
			if err != nil {
				t.Fatalf("resourceMetadata() error = %v", err)
			}
			want := &protectedResourceMetadata{Resource: srv.URL, AuthorizationServers: []string{"https://as.example.com"}}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Fatalf("resourceMetadata() wrong result (-want +got) diff = %s", diff)
			}
		})
	}
}

func TestServerMetadataEndpoints(t *testing.T) {
	t.Parallel()
	body := `{
		"issuer": "https://login.example.com",
		"authorization_endpoint": "https://login.example.com/authorize",
		"token_endpoint": "https://login.example.com/token",
		"client_id_metadata_document_supported": true,
		"authorization_response_iss_parameter_supported": true
	}`
	var m authServerMetadata
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	want := Endpoints{
		Issuer:                   "https://login.example.com",
		Authorization:            "https://login.example.com/authorize",
		Token:                    "https://login.example.com/token",
		CIMDSupported:            true,
		AuthorizationResponseIss: true,
	}
	if diff := cmp.Diff(want, m.Endpoints()); diff != "" {
		t.Fatalf("authServerMetadata.Endpoints() wrong result (-want +got) diff = %s", diff)
	}
}

func TestServerMetadata(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		opts oauthtest.ServerOptions
	}{
		{name: "RFC 8414", opts: oauthtest.ServerOptions{CIMD: true}},
		{name: "OpenID Connect Discovery only", opts: oauthtest.ServerOptions{OIDCOnly: true}},
		{name: "issuer with a path", opts: oauthtest.ServerOptions{IssuerPath: "/oauth2/default"}},
		{name: "issuer with a path, OpenID Connect Discovery only", opts: oauthtest.ServerOptions{IssuerPath: "/realms/a2a", OIDCOnly: true}},
		{name: "issuer with a path, well-known suffix appended", opts: oauthtest.ServerOptions{IssuerPath: "/oauth2/default", AppendedMetadataOnly: true}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			as := oauthtest.NewAuthServer(t, tc.opts)
			want := serverEndpoints(as, tc.opts.CIMD)

			m, err := testFetcher().serverMetadata(t.Context(), as.URL)
			if err != nil {
				t.Fatalf("serverMetadata() error = %v", err)
			}
			if diff := cmp.Diff(want, m.Endpoints()); diff != "" {
				t.Fatalf("serverMetadata().Endpoints() wrong result (-want +got) diff = %s", diff)
			}

			m, err = testFetcher().metadataAt(t.Context(), as.MetadataURL())
			if err != nil {
				t.Fatalf("metadataAt() error = %v", err)
			}
			if diff := cmp.Diff(want, m.Endpoints()); diff != "" {
				t.Fatalf("metadataAt().Endpoints() wrong result (-want +got) diff = %s", diff)
			}
		})
	}

	t.Run("host document at the inserted location", func(t *testing.T) {
		t.Parallel()
		var srv *httptest.Server
		srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			issuer := srv.URL
			switch r.URL.Path {
			case wellKnownServerMetadata + "/tenant":
			case "/tenant" + wellKnownServerMetadata:
				issuer += "/tenant"
			default:
				http.NotFound(w, r)
				return
			}
			if _, err := fmt.Fprintf(w, `{"issuer":%q,"authorization_endpoint":%q,"token_endpoint":%q}`, issuer, issuer+"/authorize", issuer+"/token"); err != nil {
				t.Errorf("writing the response error = %v", err)
			}
		}))
		t.Cleanup(srv.Close)
		m, err := testFetcher().serverMetadata(t.Context(), srv.URL+"/tenant")
		if err != nil {
			t.Fatalf("serverMetadata() error = %v, want the metadata at the appended location", err)
		}
		if m.Issuer != srv.URL+"/tenant" {
			t.Fatalf("serverMetadata() issuer = %q, want %q", m.Issuer, srv.URL+"/tenant")
		}
	})

	t.Run("issuer mismatch", func(t *testing.T) {
		t.Parallel()
		srv := jsonServer(t, `{"issuer":"https://elsewhere.example.com"}`)
		if _, err := testFetcher().serverMetadata(t.Context(), srv.URL); err == nil || !strings.Contains(err.Error(), "names issuer") {
			t.Fatalf("serverMetadata() error = %v, want the issuer mismatch", err)
		}
	})
}

func TestEndpointsValidate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		e       Endpoints
		wantErr string
	}{
		{name: "authorization code", e: Endpoints{Issuer: "https://as.example.com", Authorization: "https://as.example.com/authorize", Token: "https://as.example.com/token"}},
		{name: "device only", e: Endpoints{Issuer: "https://as.example.com", DeviceAuthorization: "https://as.example.com/device", Token: "https://as.example.com/token"}},
		{name: "no issuer", e: Endpoints{Token: "https://as.example.com/token"}, wantErr: "no issuer"},
		{name: "no token endpoint", e: Endpoints{Issuer: "https://as.example.com", Authorization: "https://as.example.com/authorize"}, wantErr: "no token endpoint"},
		{name: "no user flow", e: Endpoints{Issuer: "https://as.example.com", Token: "https://as.example.com/token"}, wantErr: "neither"},
		{name: "plain http", e: Endpoints{Issuer: "http://as.example.com", Authorization: "http://as.example.com/authorize", Token: "http://as.example.com/token"}, wantErr: "https"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.e.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Endpoints.Validate() error = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Endpoints.Validate() error = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestAcceptsOtherCredentials(t *testing.T) {
	t.Parallel()
	oauthScheme := a2a.OAuth2SecurityScheme{Flows: a2a.AuthorizationCodeOAuthFlow{AuthorizationURL: "https://as.example.com/authorize", TokenURL: "https://as.example.com/token"}}
	schemes := a2a.NamedSecuritySchemes{
		"oauth": oauthScheme,
		"oidc":  a2a.OpenIDConnectSecurityScheme{OpenIDConnectURL: "https://as.example.com/.well-known/openid-configuration"},
		"key":   a2a.APIKeySecurityScheme{Name: "X-Key", Location: "header"},
	}
	tests := []struct {
		name    string
		options a2a.SecurityRequirementsOptions
		want    bool
	}{
		{name: "oauth only", options: a2a.SecurityRequirementsOptions{{"oauth": nil}}},
		{name: "oauth or openIdConnect", options: a2a.SecurityRequirementsOptions{{"oauth": nil}, {"oidc": nil}}},
		{name: "oauth or api key", options: a2a.SecurityRequirementsOptions{{"oauth": nil}, {"key": nil}}, want: true},
		{name: "oauth or no credentials", options: a2a.SecurityRequirementsOptions{{"oauth": nil}, {}}, want: true},
		{name: "no credentials or oauth", options: a2a.SecurityRequirementsOptions{{}, {"oauth": nil}}, want: true},
		{name: "oauth and api key together", options: a2a.SecurityRequirementsOptions{{"oauth": nil, "key": nil}}},
		{name: "undefined scheme", options: a2a.SecurityRequirementsOptions{{"oauth": nil}, {"missing": nil}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			card := &a2a.AgentCard{SecuritySchemes: schemes, SecurityRequirements: tc.options}
			if got := acceptsOtherCredentials(card); got != tc.want {
				t.Fatalf("acceptsOtherCredentials() = %v, want %v", got, tc.want)
			}
		})
	}
}

// remoteHosts serves the requests to the hosts it names with their handlers and
// sends the others over the network, so tests can stand in for agents and
// authorization servers that are not on loopback.
type remoteHosts map[string]http.Handler

func (h remoteHosts) RoundTrip(r *http.Request) (*http.Response, error) {
	handler, ok := h[r.URL.Host]
	if !ok {
		return http.DefaultTransport.RoundTrip(r)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, r)
	resp := rec.Result()
	resp.Request = r
	return resp, nil
}

// countingServer answers every request with 404 and counts them.
func countingServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func TestDiscoverFromChallengeAuthorizationServers(t *testing.T) {
	t.Parallel()
	const remote = "https://agent.example.com"
	const local = "http://127.0.0.1:1"
	servers := func(n int) []string {
		s := make([]string, n)
		for i := range s {
			s[i] = fmt.Sprintf("https://as%d.example.com", i)
		}
		return s
	}
	tests := []struct {
		name        string
		agent       string
		listed      []string
		metadataURL string
		want        []string
		wantErr     string
	}{
		{name: "at most five servers", agent: remote, listed: servers(7), want: servers(maxAuthorizationServers)},
		{name: "loopback server for a remote agent", agent: remote, listed: []string{"http://127.0.0.1:8080", "https://as0.example.com"}, want: []string{"https://as0.example.com"}},
		{name: "localhost server for a remote agent", agent: remote, listed: []string{"https://localhost", "https://as0.example.com"}, want: []string{"https://as0.example.com"}},
		{name: "only loopback servers for a remote agent", agent: remote, listed: []string{"http://[::1]:8080"}, wantErr: "loopback"},
		{name: "loopback metadata URL for a remote agent", agent: remote, metadataURL: "loopback", wantErr: "loopback"},
		{name: "loopback server for a loopback agent", agent: local, listed: []string{"http://localhost:8080", "https://as0.example.com"}, want: []string{"http://localhost:8080", "https://as0.example.com"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			loopback, hits := countingServer(t)
			agentURL, err := url.Parse(tc.agent)
			if err != nil {
				t.Fatal(err)
			}
			prm := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				body, err := json.Marshal(protectedResourceMetadata{Resource: tc.agent, AuthorizationServers: tc.listed})
				if err != nil {
					t.Errorf("json.Marshal() error = %v", err)
				}
				if _, err := w.Write(body); err != nil {
					t.Errorf("writing the response error = %v", err)
				}
			})
			f := fetcher{client: &http.Client{Transport: remoteHosts{agentURL.Host: prm}}}
			metadataURL := tc.agent + "/prm"
			if tc.metadataURL == "loopback" {
				metadataURL = loopback.URL + "/prm"
			}
			chs := ParseChallenges([]string{`Bearer resource_metadata="` + metadataURL + `"`})
			got, err := f.discoverFromChallenge(t.Context(), agentURL, chs)
			if hits.Load() != 0 {
				t.Fatalf("discoverFromChallenge() sent %d requests to a loopback server for %s, want none", hits.Load(), tc.agent)
			}
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("discoverFromChallenge() error = %v, want it to contain %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("discoverFromChallenge() error = %v", err)
			}
			if diff := cmp.Diff(tc.want, got.resource.AuthorizationServers); diff != "" {
				t.Fatalf("discoverFromChallenge() authorization servers wrong result (-want +got) diff = %s", diff)
			}
		})
	}
}

func TestRequirementFromCardLoopbackMetadata(t *testing.T) {
	t.Parallel()
	const remote = "https://agent.example.com"
	tests := []struct {
		name     string
		scheme   func(as *oauthtest.AuthServer) a2a.SecurityScheme
		redirect bool
	}{
		{
			name: "oauth2 metadata URL",
			scheme: func(as *oauthtest.AuthServer) a2a.SecurityScheme {
				return a2a.OAuth2SecurityScheme{Oauth2MetadataURL: as.MetadataURL(), Flows: a2a.AuthorizationCodeOAuthFlow{AuthorizationURL: as.URL + "/authorize", TokenURL: as.URL + "/token"}}
			},
		},
		{
			name: "openIdConnect URL",
			scheme: func(as *oauthtest.AuthServer) a2a.SecurityScheme {
				return a2a.OpenIDConnectSecurityScheme{OpenIDConnectURL: as.OpenIDConfigurationURL()}
			},
		},
		{
			name:     "redirect to a loopback metadata URL",
			redirect: true,
			scheme: func(*oauthtest.AuthServer) a2a.SecurityScheme {
				return a2a.OpenIDConnectSecurityScheme{OpenIDConnectURL: "https://as.example.com/.well-known/openid-configuration"}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{})
			hosts := remoteHosts{"as.example.com": http.RedirectHandler(as.OpenIDConfigurationURL(), http.StatusFound)}
			f := fetcher{client: &http.Client{Transport: hosts}}
			card := &a2a.AgentCard{
				SecuritySchemes:      a2a.NamedSecuritySchemes{"oauth": tc.scheme(as)},
				SecurityRequirements: a2a.SecurityRequirementsOptions{{"oauth": nil}},
			}
			_, err := f.requirementFromCard(t.Context(), card, remote, nil)
			if err == nil || !strings.Contains(err.Error(), "loopback") {
				t.Fatalf("requirementFromCard() error = %v, want the loopback metadata URL refused", err)
			}
			if hits := as.Hits("/.well-known/openid-configuration") + as.Hits(wellKnownServerMetadata); hits != 0 {
				t.Fatalf("requirementFromCard() sent %d metadata requests to a loopback server for %s, want none", hits, remote)
			}

			local, err := testFetcher().requirementFromCard(t.Context(), card, "http://127.0.0.1:8080", nil)
			if tc.redirect {
				return
			}
			if err != nil || local == nil {
				t.Fatalf("requirementFromCard() for a loopback agent = %v, %v, want a requirement", local, err)
			}
		})
	}
}

func TestResourceMetadataFromChallengeScope(t *testing.T) {
	t.Parallel()
	const other = "http://127.0.0.1:1"
	tests := []struct {
		name     string
		docPath  string
		resource string
		target   string
		wantErr  bool
	}{
		{name: "tenant path claims the origin", docPath: "/tenants/mallory/prm", resource: "%s", target: "%s/tenants/mallory/agent", wantErr: true},
		{name: "tenant path claims its agent", docPath: "/tenants/mallory/prm", resource: "%s/tenants/mallory/agent", target: "%s/tenants/mallory/agent/v1/tasks"},
		{name: "tenant path claims a sibling tree", docPath: "/tenants/mallory/prm", resource: "%s/tenants", target: "%s/tenants/mallory/agent", wantErr: true},
		{name: "host well-known directory claims the origin", docPath: wellKnownResourceMetadata + "/tenants/mallory/agent", resource: "%s", target: "%s/tenants/mallory/agent"},
		{name: "well-known below the agent", docPath: "/tenants/mallory/agent" + wellKnownResourceMetadata, resource: "%s/tenants/mallory/agent", target: "%s/tenants/mallory/agent/v1/tasks"},
		{name: "well-known below the agent claims above it", docPath: "/tenants/mallory/agent" + wellKnownResourceMetadata, resource: "%s/tenants", target: "%s/tenants/mallory/agent", wantErr: true},
		{name: "other origin names the request URL", docPath: "/prm", resource: other + "/agent", target: other + "/agent"},
		{name: "other origin claims above the request URL", docPath: "/prm", resource: other, target: other + "/agent", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var srv *httptest.Server
			srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				body := fmt.Sprintf(`{"resource":%q,"authorization_servers":["https://as.example.com"]}`, strings.ReplaceAll(tc.resource, "%s", srv.URL))
				if _, err := w.Write([]byte(body)); err != nil {
					t.Errorf("writing the response error = %v", err)
				}
			}))
			t.Cleanup(srv.Close)
			target, err := url.Parse(strings.ReplaceAll(tc.target, "%s", srv.URL))
			if err != nil {
				t.Fatalf("url.Parse() error = %v", err)
			}
			_, err = testFetcher().resourceMetadata(t.Context(), target, srv.URL+tc.docPath)
			if tc.wantErr {
				if err == nil || !strings.Contains(err.Error(), "may not describe") {
					t.Fatalf("resourceMetadata() error = %v, want the resource refused", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("resourceMetadata() error = %v", err)
			}
		})
	}
}

func TestRequirementFromCardBoundsMetadataFetches(t *testing.T) {
	t.Parallel()
	srv, hits := countingServer(t)
	schemes := a2a.NamedSecuritySchemes{}
	var options a2a.SecurityRequirementsOptions
	for i := range maxCardMetadataDocuments + 3 {
		name := a2a.SecuritySchemeName(fmt.Sprintf("s%d", i))
		schemes[name] = a2a.OpenIDConnectSecurityScheme{OpenIDConnectURL: fmt.Sprintf("%s/%d/.well-known/openid-configuration", srv.URL, i)}
		options = append(options, a2a.SecurityRequirements{name: nil}, a2a.SecurityRequirements{name: nil})
	}
	card := &a2a.AgentCard{SecuritySchemes: schemes, SecurityRequirements: options}
	_, err := testFetcher().requirementFromCard(t.Context(), card, srv.URL, nil)
	if err == nil || !strings.Contains(err.Error(), "not fetching") {
		t.Fatalf("requirementFromCard() error = %v, want the extra schemes skipped", err)
	}
	if got := int(hits.Load()); got != maxCardMetadataDocuments {
		t.Fatalf("requirementFromCard() sent %d metadata requests, want %d", got, maxCardMetadataDocuments)
	}
}

func TestRequirementFromCardLoopbackEndpoints(t *testing.T) {
	t.Parallel()
	const remote = "https://agent.example.com"
	const loopbackToken = "http://127.0.0.1:9/token"
	metadata := fmt.Sprintf(`{"issuer":"https://as.example.com","authorization_endpoint":"https://as.example.com/authorize","token_endpoint":%q}`, loopbackToken)
	hosts := remoteHosts{"as.example.com": http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write([]byte(metadata)); err != nil {
			t.Errorf("writing the response error = %v", err)
		}
	})}
	f := fetcher{client: &http.Client{Transport: hosts}}
	tests := []struct {
		name   string
		scheme a2a.SecurityScheme
	}{
		{name: "inline token endpoint", scheme: a2a.OAuth2SecurityScheme{Flows: a2a.AuthorizationCodeOAuthFlow{AuthorizationURL: "https://as.example.com/authorize", TokenURL: loopbackToken}}},
		{name: "inline device authorization endpoint", scheme: a2a.OAuth2SecurityScheme{Flows: a2a.DeviceCodeOAuthFlow{DeviceAuthorizationURL: "http://localhost:9/device", TokenURL: "https://as.example.com/token"}}},
		{name: "metadata token endpoint", scheme: a2a.OpenIDConnectSecurityScheme{OpenIDConnectURL: "https://as.example.com/.well-known/openid-configuration"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			card := &a2a.AgentCard{
				SecuritySchemes:      a2a.NamedSecuritySchemes{"oauth": tc.scheme},
				SecurityRequirements: a2a.SecurityRequirementsOptions{{"oauth": nil}},
			}
			_, err := f.requirementFromCard(t.Context(), card, remote, nil)
			if err == nil || !strings.Contains(err.Error(), "loopback endpoint") {
				t.Fatalf("requirementFromCard() error = %v, want the loopback endpoint refused", err)
			}
			local, err := f.requirementFromCard(t.Context(), card, "http://127.0.0.1:8080", nil)
			if err != nil || local == nil {
				t.Fatalf("requirementFromCard() for a loopback agent = %v, %v, want a requirement", local, err)
			}
		})
	}
}

func TestResolveInlineIssuerCardOnlyEndpoints(t *testing.T) {
	t.Parallel()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != wellKnownServerMetadata {
			http.NotFound(w, r)
			return
		}
		if _, err := fmt.Fprintf(w, `{"issuer":%q,"token_endpoint":%q}`, srv.URL, srv.URL+"/token"); err != nil {
			t.Errorf("writing the response error = %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	regs := []Registration{{Authority: srv.URL, ClientID: "corp-client"}}
	tests := []struct {
		name    string
		card    Endpoints
		want    Endpoints
		wantErr bool
	}{
		{
			name: "device endpoint on the server's origin",
			card: Endpoints{DeviceAuthorization: srv.URL + "/device", Token: srv.URL + "/token"},
			want: Endpoints{Issuer: srv.URL, DeviceAuthorization: srv.URL + "/device", Token: srv.URL + "/token"},
		},
		{
			name:    "device endpoint on another host",
			card:    Endpoints{DeviceAuthorization: "https://evil.example/device", Token: srv.URL + "/token"},
			wantErr: true,
		},
		{
			name:    "authorization endpoint on another host",
			card:    Endpoints{Authorization: "https://evil.example/authorize", Token: srv.URL + "/token"},
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := testFetcher().resolveInlineIssuer(t.Context(), regs, tc.card, false)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("resolveInlineIssuer() = %+v, want the card's endpoint refused", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveInlineIssuer() error = %v", err)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("resolveInlineIssuer() wrong result (-want +got) diff = %s", diff)
			}
		})
	}
}

func TestResolveInlineIssuerWithoutMetadata(t *testing.T) {
	t.Parallel()
	bare, _ := countingServer(t)
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(failing.Close)
	github := Endpoints{DeviceAuthorization: bare.URL + "/login/device/code", Token: bare.URL + "/login/oauth/access_token"}
	tests := []struct {
		name       string
		regs       []Registration
		card       Endpoints
		want       *Endpoints
		wantClient bool
	}{
		{
			name: "registration for the token endpoint's origin",
			regs: []Registration{{Authority: bare.URL, ClientID: "gh-client"}},
			card: github,
			want: &Endpoints{Issuer: bare.URL, DeviceAuthorization: github.DeviceAuthorization, Token: github.Token},
		},
		{
			name:       "an endpoint on another origin",
			regs:       []Registration{{Authority: bare.URL, ClientID: "gh-client"}},
			card:       Endpoints{DeviceAuthorization: "https://evil.example/device", Token: github.Token},
			wantClient: true,
		},
		{
			name:       "authority with a path",
			regs:       []Registration{{Authority: bare.URL + "/login", ClientID: "gh-client"}},
			card:       github,
			wantClient: true,
		},
		{
			name: "registration server failing",
			regs: []Registration{{Authority: failing.URL, ClientID: "c"}},
			card: Endpoints{Authorization: failing.URL + "/authorize", Token: failing.URL + "/token"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := testFetcher().resolveInlineIssuer(t.Context(), tc.regs, tc.card, false)
			var nc *NoClientError
			switch {
			case tc.want != nil:
				if err != nil {
					t.Fatalf("resolveInlineIssuer() error = %v", err)
				}
				if diff := cmp.Diff(*tc.want, got); diff != "" {
					t.Fatalf("resolveInlineIssuer() wrong result (-want +got) diff = %s", diff)
				}
				choice, err := selectClient(tc.regs, "", got)
				if err != nil || choice.ClientID != "gh-client" {
					t.Fatalf("selectClient() = %+v, %v, want gh-client", choice, err)
				}
			case tc.wantClient:
				if !errors.As(err, &nc) {
					t.Fatalf("resolveInlineIssuer() = %+v, %v, want a NoClientError", got, err)
				}
			default:
				if err == nil || errors.As(err, &nc) || !strings.Contains(err.Error(), "HTTP 503") {
					t.Fatalf("resolveInlineIssuer() error = %v, want the fetch failure", err)
				}
			}
		})
	}
}

func TestRequirementFromCardInlineResolution(t *testing.T) {
	t.Parallel()
	as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{})
	inline := a2a.OAuth2SecurityScheme{Flows: a2a.AuthorizationCodeOAuthFlow{AuthorizationURL: "https://unregistered.example.com/authorize", TokenURL: "https://unregistered.example.com/token"}}
	oidc := a2a.OpenIDConnectSecurityScheme{OpenIDConnectURL: as.OpenIDConfigurationURL()}
	resolve := func(_ context.Context, e Endpoints) (Endpoints, error) {
		return Endpoints{}, &NoClientError{Issuers: []string{origin(e.Token)}}
	}
	tests := []struct {
		name       string
		options    a2a.SecurityRequirementsOptions
		wantIssuer string
		wantClient bool
	}{
		{name: "later option works", options: a2a.SecurityRequirementsOptions{{"inline": nil}, {"oidc": nil}}, wantIssuer: as.URL},
		{name: "only the inline option", options: a2a.SecurityRequirementsOptions{{"inline": nil}}, wantClient: true},
		{name: "inline option or an API key", options: a2a.SecurityRequirementsOptions{{"inline": nil}, {"key": nil}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			card := &a2a.AgentCard{
				SecuritySchemes:      a2a.NamedSecuritySchemes{"inline": inline, "oidc": oidc, "key": a2a.APIKeySecurityScheme{Name: "X-API-Key", Location: "header"}},
				SecurityRequirements: tc.options,
			}
			got, err := testFetcher().requirementFromCard(t.Context(), card, "http://127.0.0.1:8080", resolve)
			var nc *NoClientError
			switch {
			case tc.wantClient:
				if !errors.As(err, &nc) {
					t.Fatalf("requirementFromCard() = %+v, %v, want a NoClientError", got, err)
				}
			case tc.wantIssuer != "":
				if err != nil || got == nil || got.Endpoints.Issuer != tc.wantIssuer {
					t.Fatalf("requirementFromCard() = %+v, %v, want issuer %s", got, err, tc.wantIssuer)
				}
			default:
				if err != nil || got != nil {
					t.Fatalf("requirementFromCard() = %+v, %v, want nil, nil", got, err)
				}
			}
		})
	}
}

func TestResolveInlineIssuerTenantEndpoints(t *testing.T) {
	t.Parallel()
	tenants := tenantServer(t, "/tenant-1/v2.0", "/tenant-2/v2.0")
	issuer := tenants.URL + "/tenant-1/v2.0"
	regs := []Registration{{Authority: issuer, ClientID: "tenant-1-client"}}
	tests := []struct {
		name       string
		device     string
		wantDevice string
	}{
		{name: "device endpoint below the issuer", device: issuer + "/device", wantDevice: issuer + "/device"},
		{name: "device endpoint of another tenant", device: tenants.URL + "/tenant-2/v2.0/device"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			card := Endpoints{Authorization: issuer + "/authorize", DeviceAuthorization: tc.device, Token: issuer + "/token"}
			got, err := testFetcher().resolveInlineIssuer(t.Context(), regs, card, false)
			if err != nil {
				t.Fatalf("resolveInlineIssuer() error = %v", err)
			}
			if got.DeviceAuthorization != tc.wantDevice {
				t.Fatalf("resolveInlineIssuer() DeviceAuthorization = %q, want %q", got.DeviceAuthorization, tc.wantDevice)
			}
		})
	}
}

func TestServerMetadataNotPublished(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		status      int
		contentType string
		body        string
		absent      bool
	}{
		{name: "not found", status: http.StatusNotFound, contentType: "text/plain", body: "404", absent: true},
		{name: "gone", status: http.StatusGone, contentType: "text/plain", absent: true},
		{name: "HTML page", status: http.StatusOK, contentType: "text/html", body: "<html></html>", absent: true},
		{name: "rate limited", status: http.StatusTooManyRequests, contentType: "text/plain"},
		{name: "forbidden", status: http.StatusForbidden, contentType: "text/plain"},
		{name: "truncated JSON", status: http.StatusOK, contentType: "application/json", body: `{"issuer":"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				w.WriteHeader(tc.status)
				if _, err := w.Write([]byte(tc.body)); err != nil {
					t.Errorf("writing the response error = %v", err)
				}
			}))
			t.Cleanup(srv.Close)
			_, err := testFetcher().serverMetadata(t.Context(), srv.URL)
			if err == nil || errors.Is(err, errNoServerMetadata) != tc.absent {
				t.Fatalf("serverMetadata() error = %v, want absent %v", err, tc.absent)
			}
		})
	}
}

func TestUsableEndpointsPKCE(t *testing.T) {
	t.Parallel()
	const issuer = "https://as.example.com"
	tests := []struct {
		name    string
		methods []string
		device  bool
		want    Endpoints
		wantErr bool
	}{
		{name: "no methods listed", want: Endpoints{Issuer: issuer, Authorization: issuer + "/authorize", Token: issuer + "/token"}},
		{name: "S256 listed", methods: []string{"plain", "S256"}, want: Endpoints{Issuer: issuer, Authorization: issuer + "/authorize", Token: issuer + "/token"}},
		{name: "only plain, with a device endpoint", methods: []string{"plain"}, device: true, want: Endpoints{Issuer: issuer, Token: issuer + "/token", DeviceAuthorization: issuer + "/device"}},
		{name: "only plain", methods: []string{"plain"}, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := &authServerMetadata{Issuer: issuer, AuthorizationEndpoint: issuer + "/authorize", TokenEndpoint: issuer + "/token", CodeChallengeMethodsSupported: tc.methods}
			if tc.device {
				m.DeviceAuthorizationEndpoint = issuer + "/device"
			}
			got, err := m.usableEndpoints()
			if tc.wantErr {
				if err == nil || !strings.Contains(err.Error(), "S256") {
					t.Fatalf("usableEndpoints() = %+v, %v, want PKCE S256 refused", got, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("usableEndpoints() error = %v", err)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("usableEndpoints() wrong result (-want +got) diff = %s", diff)
			}
		})
	}

	t.Run("card scheme prefers the device flow", func(t *testing.T) {
		t.Parallel()
		metadata := fmt.Sprintf(`{"issuer":%q,"authorization_endpoint":%q,"token_endpoint":%q,"device_authorization_endpoint":%q,"code_challenge_methods_supported":["plain"]}`, issuer, issuer+"/authorize", issuer+"/token", issuer+"/device")
		hosts := remoteHosts{"as.example.com": http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if _, err := w.Write([]byte(metadata)); err != nil {
				t.Errorf("writing the response error = %v", err)
			}
		})}
		f := fetcher{client: &http.Client{Transport: hosts}}
		card := &a2a.AgentCard{
			SecuritySchemes:      a2a.NamedSecuritySchemes{"oidc": a2a.OpenIDConnectSecurityScheme{OpenIDConnectURL: issuer + "/.well-known/openid-configuration"}},
			SecurityRequirements: a2a.SecurityRequirementsOptions{{"oidc": nil}},
		}
		r, err := f.requirementFromCard(t.Context(), card, "https://agent.example.com", nil)
		if err != nil || r == nil || !r.PreferDevice || r.Endpoints.Authorization != "" {
			t.Fatalf("requirementFromCard() = %+v, %v, want the device flow only", r, err)
		}
	})
}
