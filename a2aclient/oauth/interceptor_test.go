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
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aclient/oauth/oauthtest"
	"github.com/a2aproject/a2a-go/v2/log"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

// inlineHandler publishes the metadata that inline cards are matched against,
// at https://inlineHost; see TestMain.
var inlineHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != wellKnownServerMetadata {
		http.NotFound(w, r)
		return
	}
	issuer := "https://" + inlineHost
	if _, err := fmt.Fprintf(w, `{"issuer":%q,"authorization_endpoint":%q,"token_endpoint":%q}`, issuer, issuer+"/authorize", issuer+"/token"); err != nil {
		log.Error(r.Context(), "failed to write metadata", err)
	}
})

var inlineEndpoints = Endpoints{
	Issuer:        "https://" + inlineHost,
	Authorization: "https://" + inlineHost + "/authorize",
	Token:         "https://" + inlineHost + "/token",
}

// inlineCard declares the inline oauth2 scheme, and an API key alternative when
// withAPIKey is set.
func inlineCard(withAPIKey bool) *a2a.AgentCard {
	card := &a2a.AgentCard{
		Name: "inline",
		SecuritySchemes: a2a.NamedSecuritySchemes{
			"oauth": a2a.OAuth2SecurityScheme{Flows: a2a.AuthorizationCodeOAuthFlow{AuthorizationURL: inlineEndpoints.Authorization, TokenURL: inlineEndpoints.Token}},
			"key":   a2a.APIKeySecurityScheme{Name: "X-API-Key", Location: "header"},
		},
		SecurityRequirements: a2a.SecurityRequirementsOptions{{"oauth": nil}},
	}
	if withAPIKey {
		card.SecurityRequirements = append(card.SecurityRequirements, a2a.SecurityRequirements{"key": nil})
	}
	return card
}

// managerWithGrant returns a manager whose store holds one grant for resource
// and scopes, with the given tokens.
func managerWithGrant(t *testing.T, resource string, scopes []string, access, refresh string) *Manager {
	t.Helper()
	store := NewMemoryStore()
	if resource != "" {
		g := Grant{
			Key:          NewGrantKey(inlineEndpoints.Issuer, "c", resource, scopes),
			AccessToken:  access,
			RefreshToken: refresh,
			Endpoints:    inlineEndpoints,
		}
		if access != "" {
			g.ExpiresAt = time.Now().Add(time.Hour)
		}
		if err := store.Put(t.Context(), g); err != nil {
			t.Fatal(err)
		}
	}
	return &Manager{Clients: []Registration{{Authority: inlineEndpoints.Issuer, ClientID: "c"}}, Store: store}
}

type failingLocker struct{ *MemoryStore }

func (failingLocker) Lock(context.Context) (func(), error) { return nil, errBroken }

// brokenStore fails every operation.
type brokenStore struct{}

func (brokenStore) List(context.Context) ([]Grant, error)         { return nil, errBroken }
func (brokenStore) Get(context.Context, GrantKey) (*Grant, error) { return nil, errBroken }
func (brokenStore) Put(context.Context, Grant) error              { return errBroken }
func (brokenStore) Delete(context.Context, GrantKey) error        { return errBroken }

var errBroken = errors.New("store unavailable")

// errClass names the class of an error the Manager returns.
func errClass(err error) string {
	var ir *InteractionRequiredError
	var nc *NoClientError
	var ac *AmbiguousClientError
	var fe *FailureError
	switch {
	case err == nil:
		return ""
	case errors.As(err, &ir):
		return "interaction required"
	case errors.As(err, &nc):
		return "no client"
	case errors.As(err, &ac):
		return "ambiguous client"
	case errors.As(err, &fe):
		return "failure"
	default:
		return fmt.Sprintf("unclassified %T", err)
	}
}

func TestInterceptorBefore(t *testing.T) {
	t.Parallel()
	const agent = "https://agent.example.com"
	const iface = agent + "/a2a/v1"
	scoped := func(scopes ...string) *a2a.AgentCard {
		card := inlineCard(false)
		card.SecurityRequirements = a2a.SecurityRequirementsOptions{{"oauth": scopes}}
		return card
	}
	withNested := func(t *testing.T) *Manager {
		m := managerWithGrant(t, agent, []string{"a2a.invoke"}, "origin", "")
		nested := Grant{
			Key:         NewGrantKey(inlineEndpoints.Issuer, "c", agent+"/a2a", []string{"a2a.invoke"}),
			AccessToken: "nested",
			ExpiresAt:   time.Now().Add(time.Hour),
			Endpoints:   inlineEndpoints,
		}
		if err := m.Store.Put(t.Context(), nested); err != nil {
			t.Fatal(err)
		}
		return m
	}
	tests := []struct {
		name      string
		manager   func(t *testing.T) *Manager
		baseURL   string
		binding   a2a.TransportProtocol
		card      *a2a.AgentCard
		explicit  string
		want      []string
		wantClass string
		wantIs    error
	}{
		{
			name:    "optional OAuth without a grant",
			manager: func(t *testing.T) *Manager { return managerWithGrant(t, "", nil, "", "") },
			card:    inlineCard(true),
		},
		{
			name:    "optional OAuth with a grant",
			manager: func(t *testing.T) *Manager { return managerWithGrant(t, agent, nil, "at", "") },
			card:    inlineCard(true),
			want:    []string{"Bearer at"},
		},
		{
			name:      "required OAuth without a grant",
			manager:   func(t *testing.T) *Manager { return managerWithGrant(t, "", nil, "", "") },
			card:      inlineCard(false),
			wantClass: "interaction required",
		},
		{
			name: "optional OAuth without a client",
			manager: func(t *testing.T) *Manager {
				m := managerWithGrant(t, "", nil, "", "")
				m.Clients = nil
				return m
			},
			card: inlineCard(true),
		},
		{
			name: "required OAuth without a client",
			manager: func(t *testing.T) *Manager {
				m := managerWithGrant(t, "", nil, "", "")
				m.Clients = nil
				return m
			},
			card:      inlineCard(false),
			wantClass: "no client",
		},
		{
			name: "required OAuth with ambiguous registrations",
			manager: func(t *testing.T) *Manager {
				m := managerWithGrant(t, "", nil, "", "")
				m.Clients = append(m.Clients, Registration{Authority: inlineEndpoints.Issuer, ClientID: "other"})
				return m
			},
			card:      inlineCard(false),
			wantClass: "ambiguous client",
		},
		{
			name: "required OAuth with an invalid default client ID",
			manager: func(t *testing.T) *Manager {
				m := managerWithGrant(t, "", nil, "", "")
				m.DefaultClientID = "not-a-url"
				return m
			},
			card:      inlineCard(false),
			wantClass: "failure",
		},
		{
			name: "required OAuth with a broken store",
			manager: func(t *testing.T) *Manager {
				m := managerWithGrant(t, "", nil, "", "")
				m.Store = brokenStore{}
				return m
			},
			card:      inlineCard(false),
			wantClass: "failure",
			wantIs:    errBroken,
		},
		{
			name: "no requirement with a broken store",
			manager: func(t *testing.T) *Manager {
				m := managerWithGrant(t, "", nil, "", "")
				m.Store = brokenStore{}
				return m
			},
		},
		{
			name: "required OAuth with a failing lock",
			manager: func(t *testing.T) *Manager {
				m := managerWithGrant(t, agent, nil, "", "rt")
				m.Store = failingLocker{m.Store.(*MemoryStore)}
				return m
			},
			card:      inlineCard(false),
			wantClass: "failure",
			wantIs:    errBroken,
		},
		{
			name:      "required OAuth over plain HTTP",
			manager:   func(t *testing.T) *Manager { return managerWithGrant(t, "http://agent.example.com", nil, "secret", "") },
			baseURL:   "http://agent.example.com",
			card:      inlineCard(false),
			wantClass: "failure",
		},
		{
			name:    "no requirement over plain HTTP",
			manager: func(t *testing.T) *Manager { return managerWithGrant(t, "http://agent.example.com", nil, "secret", "") },
			baseURL: "http://agent.example.com",
		},
		{
			name:    "no requirement on loopback",
			manager: func(t *testing.T) *Manager { return managerWithGrant(t, "http://127.0.0.1:8080", nil, "at", "") },
			baseURL: "http://127.0.0.1:8080",
			want:    []string{"Bearer at"},
		},
		{
			name:      "required OAuth with a grant without tokens",
			manager:   func(t *testing.T) *Manager { return managerWithGrant(t, agent, nil, "", "") },
			card:      inlineCard(false),
			wantClass: "interaction required",
		},
		{
			name:    "no requirement with a grant without tokens",
			manager: func(t *testing.T) *Manager { return managerWithGrant(t, agent, nil, "", "") },
		},
		{
			name:    "origin grant covers a required interface",
			manager: func(t *testing.T) *Manager { return managerWithGrant(t, agent, []string{"a2a.invoke"}, "at", "") },
			baseURL: iface,
			card:    inlineCard(false),
			want:    []string{"Bearer at"},
		},
		{
			name:    "origin grant covers an interface without a requirement",
			manager: func(t *testing.T) *Manager { return managerWithGrant(t, agent, []string{"a2a.invoke"}, "at", "") },
			baseURL: iface,
			want:    []string{"Bearer at"},
		},
		{
			name:      "grant lacking a required scope",
			manager:   func(t *testing.T) *Manager { return managerWithGrant(t, agent, []string{"a2a.invoke"}, "at", "") },
			baseURL:   iface,
			card:      scoped("a2a.admin"),
			wantClass: "interaction required",
		},
		{
			name:    "grant for a sibling path",
			manager: func(t *testing.T) *Manager { return managerWithGrant(t, agent+"/a", nil, "at", "") },
			baseURL: agent + "/b",
		},
		{
			name:    "most specific covering grant for a required interface",
			manager: withNested,
			baseURL: iface,
			card:    inlineCard(false),
			want:    []string{"Bearer nested"},
		},
		{
			name:    "most specific covering grant without a requirement",
			manager: withNested,
			baseURL: iface,
			want:    []string{"Bearer nested"},
		},
		{
			name:    "gRPC target URI",
			manager: func(t *testing.T) *Manager { return managerWithGrant(t, "dns:///agent.example.com:443", nil, "at", "") },
			baseURL: "dns:///agent.example.com:443",
			binding: a2a.TransportProtocolGRPC,
			card:    inlineCard(false),
			want:    []string{"Bearer at"},
		},
		{
			name:    "gRPC address",
			manager: func(t *testing.T) *Manager { return managerWithGrant(t, "agent.example.com:443", nil, "at", "") },
			baseURL: "agent.example.com:443",
			binding: a2a.TransportProtocolGRPC,
			card:    inlineCard(false),
			want:    []string{"Bearer at"},
		},
		{
			name:     "explicit credential with a requirement",
			manager:  func(t *testing.T) *Manager { return managerWithGrant(t, agent, nil, "at", "") },
			card:     inlineCard(false),
			explicit: "Bearer explicit",
			want:     []string{"Bearer explicit"},
		},
		{
			name:     "explicit credential without a requirement",
			manager:  func(t *testing.T) *Manager { return managerWithGrant(t, agent, nil, "at", "") },
			explicit: "Bearer explicit",
			want:     []string{"Bearer explicit"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			baseURL, binding := tc.baseURL, tc.binding
			if baseURL == "" {
				baseURL = agent
			}
			if binding == "" {
				binding = a2a.TransportProtocolJSONRPC
			}
			req := &a2aclient.Request{
				BaseURL:         baseURL,
				ProtocolBinding: binding,
				Card:            tc.card,
				ServiceParams:   a2aclient.ServiceParams{},
			}
			if tc.explicit != "" {
				req.ServiceParams.Append("Authorization", tc.explicit)
			}
			_, _, err := tc.manager(t).Interceptor().Before(t.Context(), req)
			if got := errClass(err); got != tc.wantClass {
				t.Fatalf("Before() error = %v (%s), want %q", err, got, tc.wantClass)
			}
			if tc.wantIs != nil && !errors.Is(err, tc.wantIs) {
				t.Fatalf("Before() error = %v, want %v", err, tc.wantIs)
			}
			if diff := cmp.Diff(tc.want, req.ServiceParams.Get("authorization")); diff != "" {
				t.Fatalf("Before() Authorization wrong result (-want +got) diff = %s", diff)
			}
		})
	}
}

func TestInterceptorPlainHTTPLogsAtDebug(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	ctx := log.AttachLogger(t.Context(), slog.New(slog.NewTextHandler(&buf, nil)))
	m := managerWithGrant(t, "", nil, "", "")
	req := &a2aclient.Request{BaseURL: "http://agent.example.com", ProtocolBinding: a2a.TransportProtocolJSONRPC, ServiceParams: a2aclient.ServiceParams{}}
	if _, _, err := m.Interceptor().Before(ctx, req); err != nil {
		t.Fatalf("Before() error = %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("Before() for a plain HTTP agent logged %q, want nothing above debug", buf.String())
	}
}

func TestInterceptorAfter(t *testing.T) {
	t.Parallel()
	as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{})
	const agent = "http://127.0.0.1:1"
	prm := resourceMetadataServer(t, agent, as.URL)
	unpublished := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(unpublished.Close)
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(broken.Close)
	callErr := a2a.NewError(a2a.ErrUnauthenticated, "unauthorized")
	stored := Grant{
		Key:         NewGrantKey(as.URL, "test-client", agent, []string{"a"}),
		AccessToken: "at",
		ExpiresAt:   time.Now().Add(time.Hour),
		Endpoints:   testEndpoints(as),
	}
	tests := []struct {
		name      string
		baseURL   string
		attached  bool
		replaced  bool
		explicit  bool
		clients   []Registration
		status    int
		challenge string
		want      *InteractionRequiredError
		wantClass string
	}{
		{
			name:      "attached token rejected",
			attached:  true,
			status:    http.StatusUnauthorized,
			challenge: `Bearer error="invalid_token"`,
			want:      &InteractionRequiredError{Requirement: stored.requirement(), Reason: "the agent rejected the stored grant"},
		},
		{
			name:      "attached token replaced by a later interceptor",
			attached:  true,
			replaced:  true,
			status:    http.StatusUnauthorized,
			challenge: `Bearer error="invalid_token"`,
		},
		{
			name:      "attached token lacks a scope",
			attached:  true,
			status:    http.StatusUnauthorized,
			challenge: `Bearer error="insufficient_scope", scope="b"`,
			want: &InteractionRequiredError{
				Requirement: &Requirement{Endpoints: stored.Endpoints, Resource: agent, Scopes: []string{"a", "b"}},
				Reason:      "the agent asked for more scopes than the stored grant has",
			},
		},
		{
			name:      "bearer challenge",
			status:    http.StatusUnauthorized,
			challenge: `Bearer resource_metadata="` + prm.URL + `/prm"`,
			want:      &InteractionRequiredError{Requirement: &Requirement{Endpoints: testEndpoints(as), Resource: agent}},
		},
		{
			name:      "bearer challenge without a client",
			clients:   []Registration{},
			status:    http.StatusUnauthorized,
			challenge: `Bearer resource_metadata="` + prm.URL + `/prm"`,
			wantClass: "no client",
		},
		{
			name:      "bearer challenge with ambiguous registrations",
			clients:   []Registration{{Authority: as.URL, ClientID: "a"}, {Authority: as.URL, ClientID: "b"}},
			status:    http.StatusUnauthorized,
			challenge: `Bearer resource_metadata="` + prm.URL + `/prm"`,
			wantClass: "ambiguous client",
		},
		{
			name:      "caller-supplied Authorization",
			explicit:  true,
			status:    http.StatusUnauthorized,
			challenge: `Bearer resource_metadata="` + prm.URL + `/prm"`,
		},
		{
			name:      "caller-supplied Authorization without a client",
			explicit:  true,
			clients:   []Registration{},
			status:    http.StatusUnauthorized,
			challenge: `Bearer resource_metadata="` + prm.URL + `/prm"`,
		},
		{
			name:      "basic challenge",
			status:    http.StatusUnauthorized,
			challenge: `Basic realm="agent"`,
		},
		{
			name:      "bearer challenge without metadata",
			baseURL:   unpublished.URL,
			status:    http.StatusUnauthorized,
			challenge: "Bearer",
		},
		{
			name:      "bearer challenge with failing discovery",
			status:    http.StatusUnauthorized,
			challenge: `Bearer resource_metadata="` + broken.URL + `/prm"`,
		},
		{
			name:      "forbidden",
			status:    http.StatusForbidden,
			challenge: `Bearer resource_metadata="` + prm.URL + `/prm"`,
		},
		{
			name: "no response recorded",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := newManager(as)
			if tc.clients != nil {
				m.Clients, m.DefaultClientID = tc.clients, ""
			}
			baseURL := tc.baseURL
			if baseURL == "" {
				baseURL = agent
			}
			ctx := withCallState(t.Context())
			sent := a2aclient.ServiceParams{}
			if tc.attached {
				ctx = withAttached(ctx, stored.clone())
				sent.Append("Authorization", stored.header())
				if tc.replaced {
					sent = a2aclient.ServiceParams{"authorization": {"Bearer other"}}
				}
			}
			if tc.status != 0 {
				target, err := url.Parse(baseURL)
				if err != nil {
					t.Fatal(err)
				}
				h := http.Header{}
				if tc.challenge != "" {
					h.Set("WWW-Authenticate", tc.challenge)
				}
				// The request the transport sent carries the call's Authorization.
				req := (&http.Request{URL: target, Header: http.Header{}}).WithContext(ctx)
				if auth := sent.Get("authorization"); len(auth) > 0 {
					req.Header.Set("Authorization", auth[0])
				}
				callStateFrom(ctx).recordResponse(req, &http.Response{StatusCode: tc.status, Header: h, Request: req})
			}
			if tc.explicit {
				req := &a2aclient.Request{BaseURL: baseURL, ProtocolBinding: a2a.TransportProtocolJSONRPC, ServiceParams: a2aclient.ServiceParams{}}
				req.ServiceParams.Append("Authorization", "Bearer user-supplied")
				var err error
				if ctx, _, err = m.Interceptor().Before(ctx, req); err != nil {
					t.Fatalf("Before() error = %v", err)
				}
			}
			resp := &a2aclient.Response{BaseURL: baseURL, Err: callErr, ServiceParams: sent}
			if err := m.Interceptor().After(ctx, resp); err != nil {
				t.Fatalf("After() error = %v", err)
			}
			if tc.wantClass != "" {
				if got := errClass(resp.Err); got != tc.wantClass || !errors.Is(resp.Err, callErr) {
					t.Fatalf("After() Response.Err = %v (%s), want %q wrapping the call's error", resp.Err, got, tc.wantClass)
				}
				return
			}
			if tc.want == nil {
				if resp.Err != callErr {
					t.Fatalf("After() Response.Err = %v, want %v unchanged", resp.Err, callErr)
				}
				return
			}
			var ir *InteractionRequiredError
			if !errors.As(resp.Err, &ir) {
				t.Fatalf("After() Response.Err = %v, want InteractionRequiredError", resp.Err)
			}
			if diff := cmp.Diff(tc.want, ir, cmpopts.IgnoreFields(InteractionRequiredError{}, "Err")); diff != "" {
				t.Fatalf("After() Response.Err wrong result (-want +got) diff = %s", diff)
			}
			if !errors.Is(resp.Err, callErr) {
				t.Fatalf("After() Response.Err = %v, want it to wrap the call's error", resp.Err)
			}
		})
	}
}

// TestInterceptorBeforeAuthInterceptor places the interceptor before an
// a2aclient.AuthInterceptor, which replaces the OAuth token with its own
// credential: a 401 for that credential is not the stored grant's fault.
func TestInterceptorBeforeAuthInterceptor(t *testing.T) {
	t.Parallel()
	as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{})
	agent := oauthtest.NewProtectedAgent(t, as, oauthtest.AgentOptions{Card: oauthtest.CardMetadataURL})
	m := newManager(as)
	signInToAgent(t, m, agent.URL)
	creds := a2aclient.NewInMemoryCredentialsStore()
	creds.Set("session", "oauth", "other-token")
	hc := &http.Client{Transport: m.Transport(nil)}
	client, err := a2aclient.NewFromCard(t.Context(), fetchCard(t, agent.URL),
		a2aclient.WithRESTTransport(hc),
		a2aclient.WithCallInterceptors(m.Interceptor(), &a2aclient.AuthInterceptor{Service: creds}),
	)
	if err != nil {
		t.Fatalf("a2aclient.NewFromCard() error = %v", err)
	}
	ctx := a2aclient.AttachSessionID(t.Context(), "session")
	msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hi"))
	_, err = client.SendMessage(ctx, &a2a.SendMessageRequest{Message: msg})
	if !errors.Is(err, a2a.ErrUnauthenticated) {
		t.Fatalf("Client.SendMessage() error = %v, want the agent's 401", err)
	}
	if got := errClass(err); !strings.HasPrefix(got, "unclassified") {
		t.Fatalf("Client.SendMessage() error = %v (%s), want the plain 401", err, got)
	}
	if diff := cmp.Diff([]string{"Bearer other-token"}, agent.AuthorizationHeaders()); diff != "" {
		t.Fatalf("Authorization headers the agent received wrong result (-want +got) diff = %s", diff)
	}
}

// TestInterceptorDiscoversOncePerCall checks that a call the agent refuses
// runs discovery once, in the transport, and that After reuses its result.
func TestInterceptorDiscoversOncePerCall(t *testing.T) {
	t.Parallel()
	var probes atomic.Int32
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, wellKnownResourceMetadata) {
			probes.Add(1)
			http.NotFound(w, r)
			return
		}
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	t.Cleanup(agent.Close)
	m := managerWithGrant(t, "", nil, "", "")
	hc := &http.Client{Transport: m.Transport(nil)}
	endpoint := agent.URL + "/a/b/c/rpc"
	client, err := a2aclient.NewFromEndpoints(t.Context(), []*a2a.AgentInterface{a2a.NewAgentInterface(endpoint, a2a.TransportProtocolJSONRPC)},
		a2aclient.WithJSONRPCTransport(hc),
		a2aclient.WithCallInterceptors(m.Interceptor()),
	)
	if err != nil {
		t.Fatalf("a2aclient.NewFromEndpoints() error = %v", err)
	}
	target, err := url.Parse(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	perCall := len(resourceMetadataURLs(target))
	const calls = 3
	for range calls {
		if _, err := client.GetTask(t.Context(), &a2a.GetTaskRequest{ID: "task"}); err == nil || !strings.Contains(err.Error(), "401") {
			t.Fatalf("Client.GetTask() error = %v, want the agent's 401", err)
		}
	}
	if got := int(probes.Load()); got != calls*perCall {
		t.Fatalf("Protected Resource Metadata probes for %d calls = %d, want %d", calls, got, calls*perCall)
	}
}
