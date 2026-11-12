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
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aclient/oauth/oauthtest"
	"github.com/google/go-cmp/cmp"
)

// roundTripFunc is an http.RoundTripper made from a function.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func unauthorized(r *http.Request, challenge string) *http.Response {
	h := http.Header{}
	if challenge != "" {
		h.Set("WWW-Authenticate", challenge)
	}
	return &http.Response{StatusCode: http.StatusUnauthorized, Header: h, Body: http.NoBody, Request: r}
}

func ok(r *http.Request) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: http.NoBody, Request: r}
}

// resourceMetadataServer serves Protected Resource Metadata for resource at /prm
// that names the authorization server issuer.
func resourceMetadataServer(t *testing.T, resource, issuer string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := io.WriteString(w, `{"resource":"`+resource+`","authorization_servers":["`+issuer+`"]}`); err != nil {
			t.Errorf("writing resource metadata error = %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// signedInGrant signs in to resource at as and returns the stored grant.
func signedInGrant(t *testing.T, m *Manager, as *oauthtest.AuthServer, resource string) *Grant {
	t.Helper()
	g, err := signIn(t, m, &Requirement{Endpoints: testEndpoints(as), Resource: resource}, false)
	if err != nil {
		t.Fatalf("signing in error = %v", err)
	}
	return g
}

func TestManagerTransport(t *testing.T) {
	t.Parallel()
	as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{})
	agent := oauthtest.NewProtectedAgent(t, as, oauthtest.AgentOptions{})
	m := newManager(as)
	client := &http.Client{Transport: m.Transport(nil)}
	send := func(auth string) *http.Response {
		t.Helper()
		body := `{"message":{"messageId":"1","role":"ROLE_USER","parts":[{"text":"hi"}]}}`
		req, err := http.NewRequest(http.MethodPost, agent.URL+"/message:send", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(a2a.SvcParamVersion, string(a2a.Version))
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("client.Do() error = %v, want a response", err)
		}
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close() error = %v", err)
		}
		return resp
	}

	resp := send("")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("POST without a grant status = %d, want 401", resp.StatusCode)
	}
	target := resp.Request.URL
	r, err := m.RequirementForChallenge(t.Context(), target, ParseChallenges(resp.Header.Values("WWW-Authenticate")))
	if err != nil || r == nil {
		t.Fatalf("Manager.RequirementForChallenge() = %v, %v, want a requirement", r, err)
	}
	if _, err := signIn(t, m, r, false); err != nil {
		t.Fatal(err)
	}
	if got := send("").StatusCode; got != http.StatusOK {
		t.Fatalf("POST with a stored grant status = %d, want 200", got)
	}
	if headers := agent.AuthorizationHeaders(); headers[0] != "" || slices.Contains(headers[1:], "") {
		t.Fatalf("Authorization headers = %q, want a token on every request after signing in", headers)
	}
	as.Invalidate()
	if got := send("").StatusCode; got != http.StatusOK {
		t.Fatalf("POST after the token was invalidated status = %d, want 200 after a refresh", got)
	}
	as.Invalidate()
	as.RevokeRefreshTokens()
	if got := send("").StatusCode; got != http.StatusUnauthorized {
		t.Fatalf("POST after the grant was revoked status = %d, want 401", got)
	}
	if as.SawAuthorizationHeader() {
		t.Fatalf("the authorization server received an Authorization header")
	}
	if got := send("Bearer user-supplied").StatusCode; got != http.StatusUnauthorized {
		t.Fatalf("POST with an explicit credential status = %d, want 401", got)
	}
}

func TestManagerTransportPassesThrough(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		challenge string
		attached  bool
	}{
		{name: "basic challenge", challenge: `Basic realm="agent"`},
		{name: "insufficient scope", challenge: `Bearer error="insufficient_scope", scope="a2a.admin"`, attached: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("WWW-Authenticate", tc.challenge)
				w.WriteHeader(http.StatusUnauthorized)
			}))
			t.Cleanup(srv.Close)
			m := &Manager{}
			req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
			if err != nil {
				t.Fatal(err)
			}
			if tc.attached {
				req = withToken(req, &Grant{AccessToken: "at", Key: NewGrantKey("https://login.example.com", "c", srv.URL, nil)})
			}
			resp, err := m.Transport(nil).RoundTrip(req)
			if err != nil {
				t.Fatalf("Transport.RoundTrip() error = %v, want the 401 passed through", err)
			}
			if err := resp.Body.Close(); err != nil {
				t.Errorf("resp.Body.Close() error = %v", err)
			}
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("Transport.RoundTrip() status = %d, want 401", resp.StatusCode)
			}
		})
	}
}

// TestManagerTransportReturnsResponse checks that the transport hands back the
// agent's 401 instead of an error whenever it can't retry.
func TestManagerTransportReturnsResponse(t *testing.T) {
	t.Parallel()
	const agent = "https://agent.example.com"
	rejected := func(r *http.Request) (*http.Response, error) {
		return unauthorized(r, `Bearer error="invalid_token"`), nil
	}
	tests := []struct {
		name      string
		setup     func(t *testing.T) (*Manager, *http.Request, roundTripFunc)
		wantCalls int
	}{
		{
			name: "rejected token, refresh fails",
			setup: func(t *testing.T) (*Manager, *http.Request, roundTripFunc) {
				m := managerWithGrant(t, agent, nil, "at", "rt")
				m.HTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
					return nil, errors.New("authorization server unreachable")
				})}
				g, err := m.Grant(t.Context(), &Requirement{Endpoints: inlineEndpoints, Resource: agent})
				if err != nil {
					t.Fatal(err)
				}
				req := withToken(httptest.NewRequest(http.MethodGet, agent+"/tasks", nil), g)
				return m, req, rejected
			},
			wantCalls: 1,
		},
		{
			name: "rejected token without a refresh token",
			setup: func(t *testing.T) (*Manager, *http.Request, roundTripFunc) {
				m := managerWithGrant(t, agent, nil, "at", "")
				g, err := m.Grant(t.Context(), &Requirement{Endpoints: inlineEndpoints, Resource: agent})
				if err != nil {
					t.Fatal(err)
				}
				return m, withToken(httptest.NewRequest(http.MethodGet, agent+"/tasks", nil), g), rejected
			},
			wantCalls: 1,
		},
		{
			name: "body can't be replayed",
			setup: func(t *testing.T) (*Manager, *http.Request, roundTripFunc) {
				as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{})
				m := newManager(as)
				g := signedInGrant(t, m, as, agent)
				req := httptest.NewRequest(http.MethodPost, agent+"/message:send", strings.NewReader("{}"))
				req.GetBody = func() (io.ReadCloser, error) { return nil, errors.New("body gone") }
				base := func(r *http.Request) (*http.Response, error) {
					if r.Header.Get("Authorization") == g.header() {
						return rejected(r)
					}
					return ok(r), nil
				}
				return m, withToken(req, g), base
			},
			wantCalls: 1,
		},
		{
			name: "rejected token, body without GetBody",
			setup: func(t *testing.T) (*Manager, *http.Request, roundTripFunc) {
				m := managerWithGrant(t, agent, nil, "at", "rt")
				m.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					t.Errorf("refresh request to %s for a request that can't be retried", r.URL)
					return nil, errors.New("unexpected refresh")
				})}
				g, err := m.Grant(t.Context(), &Requirement{Endpoints: inlineEndpoints, Resource: agent})
				if err != nil {
					t.Fatal(err)
				}
				req := httptest.NewRequest(http.MethodPost, agent+"/message:send", io.NopCloser(strings.NewReader("{}")))
				req.GetBody = nil
				return m, withToken(req, g), rejected
			},
			wantCalls: 1,
		},
		{
			name: "challenge without a stored grant",
			setup: func(t *testing.T) (*Manager, *http.Request, roundTripFunc) {
				as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{})
				prm := resourceMetadataServer(t, agent, as.URL)
				base := func(r *http.Request) (*http.Response, error) {
					return unauthorized(r, `Bearer resource_metadata="`+prm.URL+`/prm"`), nil
				}
				return newManager(as), httptest.NewRequest(http.MethodGet, agent+"/tasks", nil), base
			},
			wantCalls: 1,
		},
		{
			name: "challenge with failing discovery",
			setup: func(t *testing.T) (*Manager, *http.Request, roundTripFunc) {
				broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					http.Error(w, "boom", http.StatusInternalServerError)
				}))
				t.Cleanup(broken.Close)
				base := func(r *http.Request) (*http.Response, error) {
					return unauthorized(r, `Bearer resource_metadata="`+broken.URL+`/prm"`), nil
				}
				return &Manager{}, httptest.NewRequest(http.MethodGet, agent+"/tasks", nil), base
			},
			wantCalls: 1,
		},
		{
			name: "challenge from an authorization server without a client",
			setup: func(t *testing.T) (*Manager, *http.Request, roundTripFunc) {
				as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{})
				prm := resourceMetadataServer(t, agent, as.URL)
				base := func(r *http.Request) (*http.Response, error) {
					return unauthorized(r, `Bearer resource_metadata="`+prm.URL+`/prm"`), nil
				}
				return &Manager{}, httptest.NewRequest(http.MethodGet, agent+"/tasks", nil), base
			},
			wantCalls: 1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m, req, base := tc.setup(t)
			req.RequestURI = ""
			var calls atomic.Int32
			counting := roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				return base(r)
			})
			resp, err := m.Transport(counting).RoundTrip(req)
			if err != nil {
				t.Fatalf("Transport.RoundTrip() error = %v, want the 401 response", err)
			}
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("Transport.RoundTrip() status = %d, want 401", resp.StatusCode)
			}
			if got := int(calls.Load()); got != tc.wantCalls {
				t.Fatalf("Transport.RoundTrip() sent %d requests, want %d", got, tc.wantCalls)
			}
		})
	}
}

type closeTrackingBody struct {
	io.Reader
	closed atomic.Bool
}

func (b *closeTrackingBody) Close() error {
	b.closed.Store(true)
	return nil
}

type getFailingStore struct{ *MemoryStore }

func (getFailingStore) Get(context.Context, GrantKey) (*Grant, error) { return nil, errBroken }

func TestManagerTransportClosesBodyOnError(t *testing.T) {
	t.Parallel()
	m := &Manager{Store: getFailingStore{NewMemoryStore()}}
	m.learn(NewGrantKey("https://login.example.com", "c", "https://agent.example.com", nil))
	var calls atomic.Int32
	base := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return ok(r), nil
	})
	body := &closeTrackingBody{Reader: strings.NewReader("{}")}
	req, err := http.NewRequest(http.MethodPost, "https://agent.example.com/message:send", body)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Transport(base).RoundTrip(req); !errors.Is(err, errBroken) {
		t.Fatalf("Transport.RoundTrip() error = %v, want %v", err, errBroken)
	}
	if !body.closed.Load() || calls.Load() != 0 {
		t.Fatalf("Transport.RoundTrip() closed body = %v, sent %d requests, want the body closed and nothing sent", body.closed.Load(), calls.Load())
	}
}

func TestManagerTransportRefusesCleartext(t *testing.T) {
	t.Parallel()
	const agent = "http://agent.example.com"
	as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{})
	prm := resourceMetadataServer(t, agent, as.URL)
	m := newManager(as)
	g := Grant{Key: NewGrantKey(as.URL, "test-client", agent, nil), AccessToken: "secret", ExpiresAt: time.Now().Add(time.Hour), Endpoints: testEndpoints(as)}
	if err := m.Store.Put(t.Context(), g); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var sent []string
	base := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		sent = append(sent, r.Header.Get("Authorization"))
		mu.Unlock()
		return unauthorized(r, `Bearer resource_metadata="`+prm.URL+`/prm"`), nil
	})
	req, err := http.NewRequest(http.MethodGet, agent+"/tasks", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := m.Transport(base).RoundTrip(req)
	if err != nil {
		t.Fatalf("Transport.RoundTrip() error = %v, want the 401 response", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("Transport.RoundTrip() status = %d, want 401", resp.StatusCode)
	}
	if diff := cmp.Diff([]string{""}, sent); diff != "" {
		t.Fatalf("Authorization headers sent to %s wrong result (-want +got) diff = %s", agent, diff)
	}
}

// TestManagerTransportRedirectToHTTP follows a same-host redirect from https to
// http, on which net/http copies the Authorization header.
func TestManagerTransportTokenOutsideResource(t *testing.T) {
	t.Parallel()
	const agent = "https://example.com/agents/alpha"
	tests := []struct {
		name     string
		url      string
		location string
		want     map[string]string
		wantErr  bool
	}{
		{
			name:     "redirect within the resource",
			url:      agent + "/x",
			location: agent + "/y",
			want:     map[string]string{agent + "/x": "Bearer secret", agent + "/y": "Bearer secret"},
		},
		{
			name:     "redirect to a subdomain",
			url:      agent + "/x",
			location: "https://tenant.example.com/agents/alpha/x",
			want:     map[string]string{agent + "/x": "Bearer secret"},
			wantErr:  true,
		},
		{
			name:     "redirect to another port",
			url:      agent + "/x",
			location: "https://example.com:8443/agents/alpha/x",
			want:     map[string]string{agent + "/x": "Bearer secret"},
			wantErr:  true,
		},
		{
			name:     "redirect to a sibling agent",
			url:      agent + "/x",
			location: "https://example.com/agents/beta/x",
			want:     map[string]string{agent + "/x": "Bearer secret"},
			wantErr:  true,
		},
		{
			name:    "request above the resource",
			url:     "https://example.com/agents/v1/tasks",
			want:    map[string]string{},
			wantErr: true,
		},
		{
			name:    "request with dot segments",
			url:     agent + "/../beta/x",
			want:    map[string]string{},
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := managerWithGrant(t, agent, nil, "secret", "")
			var mu sync.Mutex
			sent := map[string]string{}
			base := roundTripFunc(func(r *http.Request) (*http.Response, error) {
				mu.Lock()
				sent[r.URL.String()] = r.Header.Get("Authorization")
				mu.Unlock()
				resp := ok(r)
				if r.URL.String() == tc.url && tc.location != "" {
					resp.StatusCode = http.StatusTemporaryRedirect
					resp.Header.Set("Location", tc.location)
				}
				return resp, nil
			})
			areq := &a2aclient.Request{BaseURL: agent, ProtocolBinding: a2a.TransportProtocolHTTPJSON, Card: inlineCard(false), ServiceParams: a2aclient.ServiceParams{}}
			ctx, _, err := m.Interceptor().Before(t.Context(), areq)
			if err != nil {
				t.Fatalf("Before() error = %v", err)
			}
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, tc.url, nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", areq.ServiceParams.Get("authorization")[0])
			client := &http.Client{Transport: m.Transport(base)}
			resp, err := client.Do(req)
			if err == nil {
				if err := resp.Body.Close(); err != nil {
					t.Errorf("resp.Body.Close() error = %v", err)
				}
			}
			if (err != nil) != tc.wantErr {
				t.Fatalf("client.Do() error = %v, want error %v", err, tc.wantErr)
			}
			if diff := cmp.Diff(tc.want, sent); diff != "" {
				t.Fatalf("Authorization headers sent wrong result (-want +got) diff = %s", diff)
			}
		})
	}
}

func TestManagerTransportRedirectToHTTP(t *testing.T) {
	t.Parallel()
	const agent = "https://agent.example.com"
	tests := []struct {
		name    string
		request func(t *testing.T, m *Manager) *http.Request
		want    map[string]string
		wantErr bool
	}{
		{
			name:    "token attached by the interceptor",
			want:    map[string]string{agent + "/x": "Bearer secret"},
			wantErr: true,
			request: func(t *testing.T, m *Manager) *http.Request {
				areq := &a2aclient.Request{BaseURL: agent, ProtocolBinding: a2a.TransportProtocolHTTPJSON, Card: inlineCard(false), ServiceParams: a2aclient.ServiceParams{}}
				ctx, _, err := m.Interceptor().Before(t.Context(), areq)
				if err != nil {
					t.Fatalf("Before() error = %v", err)
				}
				req, err := http.NewRequestWithContext(ctx, http.MethodGet, agent+"/x", nil)
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("Authorization", areq.ServiceParams.Get("authorization")[0])
				return req
			},
		},
		{
			name: "token attached by the transport",
			want: map[string]string{agent + "/x": "Bearer secret", "http://agent.example.com/x": ""},
			request: func(t *testing.T, m *Manager) *http.Request {
				grants, err := m.List(t.Context())
				if err != nil || len(grants) != 1 {
					t.Fatalf("Manager.List() = %v, %v, want one grant", grants, err)
				}
				m.learn(grants[0].Key)
				req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, agent+"/x", nil)
				if err != nil {
					t.Fatal(err)
				}
				return req
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := managerWithGrant(t, agent, nil, "secret", "")
			var mu sync.Mutex
			sent := map[string]string{}
			base := roundTripFunc(func(r *http.Request) (*http.Response, error) {
				mu.Lock()
				sent[r.URL.String()] = r.Header.Get("Authorization")
				mu.Unlock()
				if r.URL.Scheme == "https" {
					resp := ok(r)
					resp.StatusCode = http.StatusTemporaryRedirect
					resp.Header.Set("Location", "http://agent.example.com/x")
					return resp, nil
				}
				return ok(r), nil
			})
			client := &http.Client{Transport: m.Transport(base)}
			resp, err := client.Do(tc.request(t, m))
			if err == nil {
				if err := resp.Body.Close(); err != nil {
					t.Errorf("resp.Body.Close() error = %v", err)
				}
			}
			if (err != nil) != tc.wantErr {
				t.Fatalf("client.Do() error = %v, want error %v", err, tc.wantErr)
			}
			if diff := cmp.Diff(tc.want, sent); diff != "" {
				t.Fatalf("Authorization headers sent wrong result (-want +got) diff = %s", diff)
			}
		})
	}
}

// TestManagerTransportRejectedTokenWithoutRefreshToken checks that a token the
// agent rejected, and that can't be refreshed, is not attached again.
func TestManagerTransportRejectedTokenWithoutRefreshToken(t *testing.T) {
	t.Parallel()
	const agent = "https://agent.example.com"
	m := managerWithGrant(t, agent, nil, "at", "")
	var sent atomic.Int32
	base := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "" {
			sent.Add(1)
		}
		return unauthorized(r, `Bearer error="invalid_token"`), nil
	})
	client := &http.Client{Transport: m.Transport(base)}
	var errs []error
	for range 3 {
		areq := &a2aclient.Request{BaseURL: agent, ProtocolBinding: a2a.TransportProtocolHTTPJSON, Card: inlineCard(false), ServiceParams: a2aclient.ServiceParams{}}
		ctx, _, err := m.Interceptor().Before(t.Context(), areq)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, agent+"/message:send", strings.NewReader("{}"))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", areq.ServiceParams.Get("authorization")[0])
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("client.Do() error = %v, want the 401 response", err)
		}
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close() error = %v", err)
		}
	}
	if got := sent.Load(); got != 1 {
		t.Fatalf("requests carrying the rejected token = %d, want 1", got)
	}
	var ir *InteractionRequiredError
	if len(errs) != 2 || !errors.As(errs[0], &ir) {
		t.Fatalf("Before() errors after the rejection = %v, want InteractionRequiredError for each later call", errs)
	}
}

// TestManagerTransportRedirectAddsNoCredentials follows a redirect from agent A
// to agent B, for which the user holds a grant: net/http drops the
// Authorization header on a cross-host redirect, and the transport must not
// put B's token on a request that A chose.
func TestManagerTransportRedirectAddsNoCredentials(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		learn bool
	}{
		{name: "grant learned in this process", learn: true},
		{name: "stored grant found by the challenge"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var mu sync.Mutex
			var received []string
			var bURL string
			b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/prm" {
					w.Header().Set("Content-Type", "application/json")
					if _, err := io.WriteString(w, `{"resource":"`+bURL+`","authorization_servers":["`+inlineEndpoints.Issuer+`"]}`); err != nil {
						t.Errorf("writing resource metadata error = %v", err)
					}
					return
				}
				mu.Lock()
				received = append(received, r.Header.Get("Authorization")+" "+r.Method+" "+r.URL.Path)
				mu.Unlock()
				if r.Header.Get("Authorization") != "Bearer b-token" {
					w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+bURL+`/prm"`)
					w.WriteHeader(http.StatusUnauthorized)
				}
			}))
			t.Cleanup(b.Close)
			bURL = strings.Replace(b.URL, "127.0.0.1", "localhost", 1)
			a := httptest.NewServer(http.RedirectHandler(bURL+"/v1/tasks/victim:cancel", http.StatusTemporaryRedirect))
			t.Cleanup(a.Close)

			m := managerWithGrant(t, bURL, nil, "b-token", "")
			if tc.learn {
				m.learn(NewGrantKey(inlineEndpoints.Issuer, "c", bURL, nil))
			}
			client := &http.Client{Transport: m.Transport(nil)}
			resp, err := client.Post(a.URL+"/message:send", "application/json", strings.NewReader("{}"))
			if err != nil {
				t.Fatalf("client.Post() error = %v", err)
			}
			if err := resp.Body.Close(); err != nil {
				t.Errorf("resp.Body.Close() error = %v", err)
			}
			mu.Lock()
			defer mu.Unlock()
			if diff := cmp.Diff([]string{" POST /v1/tasks/victim:cancel"}, received); diff != "" {
				t.Fatalf("requests agent B received wrong result (-want +got) diff = %s", diff)
			}
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("client.Post() status = %d, want B's 401", resp.StatusCode)
			}
		})
	}
}

// TestManagerTransportRecordsFinalResponse checks that the transport records
// the response it returns, the retry's after a refresh, so the interceptor
// reads the challenge of the 401 the call ended with.
func TestManagerTransportRecordsFinalResponse(t *testing.T) {
	t.Parallel()
	const agent = "http://127.0.0.1:1"
	as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{})
	m := newManager(as)
	g := signedInGrant(t, m, as, agent)
	var calls atomic.Int32
	base := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			return unauthorized(r, `Bearer error="invalid_token"`), nil
		}
		return unauthorized(r, `Bearer error="insufficient_scope", scope="b"`), nil
	})
	ctx := withAttached(withCallState(t.Context()), g)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, agent+"/tasks/1", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", g.header())
	resp, err := (&http.Client{Transport: m.Transport(base)}).Do(req)
	if err != nil {
		t.Fatalf("client.Do() error = %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Errorf("resp.Body.Close() error = %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("requests sent = %d, want 2: the call and its retry after a refresh", got)
	}
	last := callStateFrom(ctx).lastResponse()
	if last == nil || last.status != http.StatusUnauthorized || !strings.Contains(last.header.Get("WWW-Authenticate"), "insufficient_scope") {
		t.Fatalf("recorded response = %+v, want the retry's 401", last)
	}

	params := a2aclient.ServiceParams{}
	params.Append("Authorization", g.header())
	callResp := &a2aclient.Response{BaseURL: agent, Err: errors.New("unexpected HTTP status: 401 Unauthorized"), ServiceParams: params}
	if err := m.Interceptor().After(ctx, callResp); err != nil {
		t.Fatalf("After() error = %v", err)
	}
	var ir *InteractionRequiredError
	if !errors.As(callResp.Err, &ir) || !slices.Contains(ir.Requirement.Scopes, "b") {
		t.Fatalf("After() Response.Err = %v, want an InteractionRequiredError asking for scope b", callResp.Err)
	}
}

// TestInterceptorAfterWithoutTransport checks that without Manager.Transport
// the interceptor learns of no 401 and leaves the call's error alone.
func TestInterceptorAfterWithoutTransport(t *testing.T) {
	t.Parallel()
	m := managerWithGrant(t, "", nil, "", "")
	ctx := withCallState(t.Context())
	callErr := errors.New("unexpected HTTP status: 401 Unauthorized")
	resp := &a2aclient.Response{BaseURL: "https://agent.example.com", Err: callErr, ServiceParams: a2aclient.ServiceParams{}}
	if err := m.Interceptor().After(ctx, resp); err != nil {
		t.Fatalf("After() error = %v", err)
	}
	if resp.Err != callErr {
		t.Fatalf("After() Response.Err = %v, want %v unchanged", resp.Err, callErr)
	}
}

// TestInterceptorRedirectTargetUnauthorized checks that a 401 from the target of
// a cross-host redirect, which never saw the token, neither blames the attached
// grant nor starts a sign-in for the redirect target.
func TestInterceptorRedirectTargetUnauthorized(t *testing.T) {
	t.Parallel()
	as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{})
	var target *httptest.Server
	target = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("the redirect target got Authorization %q", r.Header.Get("Authorization"))
		}
		if r.URL.Path == "/prm" {
			_, _ = fmt.Fprintf(w, `{"resource":%q,"authorization_servers":[%q]}`, target.URL+"/rpc", as.URL)
			return
		}
		w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer resource_metadata=%q`, target.URL+"/prm"))
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(target.Close)
	targetURL := strings.Replace(target.URL, "127.0.0.1", "localhost", 1)
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, targetURL+"/rpc", http.StatusTemporaryRedirect)
	}))
	t.Cleanup(agent.Close)

	for _, withGrant := range []bool{true, false} {
		t.Run(fmt.Sprintf("grant attached %v", withGrant), func(t *testing.T) {
			t.Parallel()
			m := newManager(as)
			if withGrant {
				signedInGrant(t, m, as, agent.URL)
			}
			ic := m.Interceptor()
			areq := &a2aclient.Request{BaseURL: agent.URL, ProtocolBinding: a2a.TransportProtocolJSONRPC, ServiceParams: a2aclient.ServiceParams{}}
			ctx, _, err := ic.Before(t.Context(), areq)
			if err != nil {
				t.Fatalf("Before() error = %v", err)
			}
			if got := len(areq.ServiceParams.Get("authorization")) > 0; got != withGrant {
				t.Fatalf("Before() attached a grant = %v, want %v", got, withGrant)
			}
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, agent.URL, strings.NewReader("{}"))
			if err != nil {
				t.Fatal(err)
			}
			for _, v := range areq.ServiceParams.Get("authorization") {
				req.Header.Set("Authorization", v)
			}
			resp, err := (&http.Client{Transport: m.Transport(nil)}).Do(req)
			if err != nil {
				t.Fatalf("client.Do() error = %v", err)
			}
			if err := resp.Body.Close(); err != nil {
				t.Errorf("resp.Body.Close() error = %v", err)
			}
			callErr := errors.New("unexpected HTTP status: 401 Unauthorized")
			callResp := &a2aclient.Response{BaseURL: agent.URL, Err: callErr, ServiceParams: areq.ServiceParams}
			if err := ic.After(ctx, callResp); err != nil {
				t.Fatalf("After() error = %v", err)
			}
			if callResp.Err != callErr {
				t.Fatalf("After() Response.Err = %v, want the redirect target's 401 left alone", callResp.Err)
			}
		})
	}
}

// TestInterceptorRetryUnauthorized checks that when the transport follows a
// challenge to a stored grant and the agent refuses the retry for more scopes,
// After reports that grant and the scopes the retry's challenge asks for.
func TestInterceptorRetryUnauthorized(t *testing.T) {
	t.Parallel()
	as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{})
	const agent = "http://127.0.0.1:1"
	prm := resourceMetadataServer(t, agent, as.URL)
	m := newManager(as)
	signedInGrant(t, m, as, agent)
	var calls atomic.Int32
	base := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			return unauthorized(r, fmt.Sprintf(`Bearer resource_metadata=%q`, prm.URL+"/prm")), nil
		}
		return unauthorized(r, `Bearer error="insufficient_scope", scope="b"`), nil
	})
	ic := m.Interceptor()
	areq := &a2aclient.Request{BaseURL: agent, ProtocolBinding: a2a.TransportProtocolJSONRPC, ServiceParams: a2aclient.ServiceParams{}}
	ctx, _, err := ic.Before(t.Context(), areq)
	if err != nil {
		t.Fatalf("Before() error = %v", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, agent, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := (&http.Client{Transport: m.Transport(base)}).Do(req)
	if err != nil {
		t.Fatalf("client.Do() error = %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Errorf("resp.Body.Close() error = %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("requests sent = %d, want the call and its retry with the stored grant", got)
	}
	callResp := &a2aclient.Response{BaseURL: agent, Err: errors.New("unexpected HTTP status: 401 Unauthorized"), ServiceParams: areq.ServiceParams}
	if err := ic.After(ctx, callResp); err != nil {
		t.Fatalf("After() error = %v", err)
	}
	var ir *InteractionRequiredError
	if !errors.As(callResp.Err, &ir) || !slices.Contains(ir.Requirement.Scopes, "b") {
		t.Fatalf("After() Response.Err = %v, want an InteractionRequiredError asking for scope b", callResp.Err)
	}
}
