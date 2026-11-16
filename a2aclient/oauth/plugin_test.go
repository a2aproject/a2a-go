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
	"iter"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
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
		g := withKey(newGrantKey(inlineEndpoints.Issuer, "c", resource, scopes), Grant{
			AccessToken:  access,
			RefreshToken: refresh,
			Endpoints:    inlineEndpoints,
		})
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

func (brokenStore) List(context.Context) ([]Grant, error)       { return nil, errBroken }
func (brokenStore) Get(context.Context, string) (*Grant, error) { return nil, errBroken }
func (brokenStore) Put(context.Context, Grant) error            { return errBroken }
func (brokenStore) Delete(context.Context, string) error        { return errBroken }

var errBroken = errors.New("store unavailable")

// errClass names the class of an error the Manager returns.
func errClass(err error) string {
	var ir *SignInRequiredError
	var nc *NoClientError
	var ac *AmbiguousClientError
	var fe *FailureError
	switch {
	case err == nil:
		return ""
	case errors.As(err, &ir):
		return "sign-in required"
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

// runPlugin sends req through m's plugin to a handler that answers each call
// with the next of answers, nil meaning success, and returns the Authorization
// values of the calls it received and the error the caller gets.
func runPlugin(ctx context.Context, m *Manager, req a2aclient.Request, answers ...error) ([][]string, error) {
	var sent [][]string
	next := func(_ context.Context, r a2aclient.Request) iter.Seq2[any, error] {
		return func(yield func(any, error) bool) {
			sent = append(sent, r.ServiceParams.Get("authorization"))
			var err error
			if i := len(sent) - 1; i < len(answers) {
				err = answers[i]
			}
			if err != nil {
				yield(nil, err)
				return
			}
			yield("ok", nil)
		}
	}
	var err error
	for _, e := range m.Plugin().WrapCall(next)(ctx, req) {
		err = e
	}
	return sent, err
}

// forbidden returns the error a client transport returns for a 403 with the
// given WWW-Authenticate challenge.
func forbidden(challenge string) *a2a.Error {
	e := a2a.NewError(a2a.ErrUnauthorized, "unexpected HTTP status: 403 Forbidden")
	e.ServiceParams = map[string][]string{"www-authenticate": {challenge}}
	return e
}

// unauthenticated returns the error a client transport returns for a 401 with
// the given WWW-Authenticate challenge, if any.
func unauthenticated(challenge string) *a2a.Error {
	e := a2a.NewError(a2a.ErrUnauthenticated, "unexpected HTTP status: 401 Unauthorized")
	if challenge != "" {
		e.ServiceParams = map[string][]string{"www-authenticate": {challenge}}
	}
	return e
}

func TestPluginBefore(t *testing.T) {
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
		nested := withKey(newGrantKey(inlineEndpoints.Issuer, "c", agent+"/a2a", []string{"a2a.invoke"}), Grant{
			AccessToken: "nested",
			ExpiresAt:   time.Now().Add(time.Hour),
			Endpoints:   inlineEndpoints,
		})
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
			wantClass: "sign-in required",
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
				m.Locker = failingLocker{}
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
			wantClass: "sign-in required",
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
			wantClass: "sign-in required",
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
			req := a2aclient.Request{
				BaseURL:         baseURL,
				ProtocolBinding: binding,
				Card:            tc.card,
				ServiceParams:   a2aclient.ServiceParams{},
			}
			if tc.explicit != "" {
				req.ServiceParams.Append("Authorization", tc.explicit)
			}
			sent, err := runPlugin(t.Context(), tc.manager(t), req)
			if got := errClass(err); got != tc.wantClass {
				t.Fatalf("calling through the plugin error = %v (%s), want %q", err, got, tc.wantClass)
			}
			if tc.wantIs != nil && !errors.Is(err, tc.wantIs) {
				t.Fatalf("calling through the plugin error = %v, want %v", err, tc.wantIs)
			}
			if tc.wantClass != "" {
				if len(sent) != 0 {
					t.Fatalf("calls sent = %d, want none after an error before sending", len(sent))
				}
				return
			}
			if len(sent) != 1 {
				t.Fatalf("calls sent = %d, want 1", len(sent))
			}
			if diff := cmp.Diff(tc.want, sent[0]); diff != "" {
				t.Fatalf("Authorization sent wrong result (-want +got) diff = %s", diff)
			}
		})
	}
}

func TestPluginPlainHTTPLogsAtDebug(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	ctx := log.AttachLogger(t.Context(), slog.New(slog.NewTextHandler(&buf, nil)))
	m := managerWithGrant(t, "", nil, "", "")
	req := a2aclient.Request{BaseURL: "http://agent.example.com", ProtocolBinding: a2a.TransportProtocolJSONRPC, ServiceParams: a2aclient.ServiceParams{}}
	if _, err := runPlugin(ctx, m, req); err != nil {
		t.Fatalf("calling through the plugin error = %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("calling a plain HTTP agent logged %q, want nothing above debug", buf.String())
	}
}

func TestPluginAfterUnauthenticated(t *testing.T) {
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
	bearer := `Bearer resource_metadata="` + prm.URL + `/prm"`
	storedKey := newGrantKey(as.URL, "test-client", agent, []string{"a"})
	stored := withKey(storedKey, Grant{AccessToken: "at", ExpiresAt: time.Now().Add(time.Hour), Endpoints: testEndpoints(as)})
	withStored := func(t *testing.T, g Grant) *Manager {
		m := newManager(as)
		if err := m.Store.Put(t.Context(), g); err != nil {
			t.Fatal(err)
		}
		return m
	}
	tests := []struct {
		name      string
		manager   func(t *testing.T) *Manager
		baseURL   string
		explicit  bool
		answers   []error
		wantSent  [][]string
		want      *SignInRequiredError
		wantClass string
		unchanged bool
	}{
		{
			name:     "attached token rejected and not refreshable",
			manager:  func(t *testing.T) *Manager { return withStored(t, stored) },
			answers:  []error{unauthenticated(`Bearer error="invalid_token"`)},
			wantSent: [][]string{{"Bearer at"}},
			want:     &SignInRequiredError{Requirement: stored.requirement(), Reason: "the agent rejected the stored grant"},
		},
		{
			name: "attached token rejected and its refresh fails",
			manager: func(t *testing.T) *Manager {
				g := stored
				g.RefreshToken = "unknown-refresh-token"
				g.Endpoints.Token = broken.URL + "/token"
				return withStored(t, g)
			},
			answers:   []error{unauthenticated(`Bearer error="invalid_token"`)},
			wantSent:  [][]string{{"Bearer at"}},
			wantClass: "failure",
		},
		{
			name:     "attached token lacks a scope",
			manager:  func(t *testing.T) *Manager { return withStored(t, stored) },
			answers:  []error{unauthenticated(`Bearer error="insufficient_scope", scope="b"`)},
			wantSent: [][]string{{"Bearer at"}},
			want: &SignInRequiredError{
				Requirement: &Requirement{Endpoints: stored.Endpoints, Resource: agent, Scopes: []string{"a", "b"}},
				Reason:      "the agent asked for more scopes than the stored grant has",
			},
		},
		{
			name:     "attached token lacks a scope, 403",
			manager:  func(t *testing.T) *Manager { return withStored(t, stored) },
			answers:  []error{forbidden(`Bearer error="insufficient_scope", scope="b"`)},
			wantSent: [][]string{{"Bearer at"}},
			want: &SignInRequiredError{
				Requirement: &Requirement{Endpoints: stored.Endpoints, Resource: agent, Scopes: []string{"a", "b"}},
				Reason:      "the agent asked for more scopes than the stored grant has",
			},
		},
		{
			name:      "attached token and a 403 without insufficient_scope",
			manager:   func(t *testing.T) *Manager { return withStored(t, stored) },
			answers:   []error{forbidden(`Bearer error="invalid_token"`)},
			wantSent:  [][]string{{"Bearer at"}},
			unchanged: true,
		},
		{
			name:      "attached token and a Bearer invalid_request",
			manager:   func(t *testing.T) *Manager { return withStored(t, stored) },
			answers:   []error{unauthenticated(`Bearer error="invalid_request"`)},
			wantSent:  [][]string{{"Bearer at"}},
			unchanged: true,
		},
		{
			name:      "no grant and a 401 without a challenge",
			manager:   func(t *testing.T) *Manager { return newManager(as) },
			answers:   []error{unauthenticated("")},
			wantSent:  [][]string{nil},
			unchanged: true,
		},
		{
			name:      "attached token and a 401 without a challenge",
			manager:   func(t *testing.T) *Manager { return withStored(t, stored) },
			answers:   []error{unauthenticated("")},
			wantSent:  [][]string{{"Bearer at"}},
			unchanged: true,
		},
		{
			name:      "attached token and a basic challenge",
			manager:   func(t *testing.T) *Manager { return withStored(t, stored) },
			answers:   []error{unauthenticated(`Basic realm="agent"`)},
			wantSent:  [][]string{{"Bearer at"}},
			unchanged: true,
		},
		{
			name:     "bearer challenge",
			manager:  func(t *testing.T) *Manager { return newManager(as) },
			answers:  []error{unauthenticated(bearer)},
			wantSent: [][]string{nil},
			want:     &SignInRequiredError{Requirement: &Requirement{Endpoints: testEndpoints(as), Resource: agent}},
		},
		{
			name: "bearer challenge picks the grant of the server's client",
			manager: func(t *testing.T) *Manager {
				m := withStored(t, withKey(newGrantKey(as.URL, "test-client", agent, nil), Grant{AccessToken: "mine", ExpiresAt: time.Now().Add(time.Hour), Endpoints: testEndpoints(as)}))
				// A grant for another client at the same resource makes the
				// grant ambiguous before the agent names its server.
				other := withKey(newGrantKey("https://other.example.com", "other", agent, nil), Grant{AccessToken: "theirs", ExpiresAt: time.Now().Add(time.Hour)})
				if err := m.Store.Put(t.Context(), other); err != nil {
					t.Fatal(err)
				}
				return m
			},
			answers:  []error{unauthenticated(bearer)},
			wantSent: [][]string{nil, {"Bearer mine"}},
		},
		{
			name: "bearer challenge without a client",
			manager: func(t *testing.T) *Manager {
				m := newManager(as)
				m.Clients, m.DefaultClientID = nil, ""
				return m
			},
			answers:   []error{unauthenticated(bearer)},
			wantSent:  [][]string{nil},
			wantClass: "no client",
		},
		{
			name: "bearer challenge with ambiguous registrations",
			manager: func(t *testing.T) *Manager {
				m := newManager(as)
				m.Clients, m.DefaultClientID = []Registration{{Authority: as.URL, ClientID: "a"}, {Authority: as.URL, ClientID: "b"}}, ""
				return m
			},
			answers:   []error{unauthenticated(bearer)},
			wantSent:  [][]string{nil},
			wantClass: "ambiguous client",
		},
		{
			name:      "caller-supplied Authorization",
			manager:   func(t *testing.T) *Manager { return withStored(t, stored) },
			explicit:  true,
			answers:   []error{unauthenticated(bearer)},
			wantSent:  [][]string{{"Bearer user-supplied"}},
			unchanged: true,
		},
		{
			name:      "basic challenge",
			manager:   func(t *testing.T) *Manager { return newManager(as) },
			answers:   []error{unauthenticated(`Basic realm="agent"`)},
			wantSent:  [][]string{nil},
			unchanged: true,
		},
		{
			name:      "bearer challenge without metadata",
			manager:   func(t *testing.T) *Manager { return newManager(as) },
			baseURL:   unpublished.URL,
			answers:   []error{unauthenticated("Bearer")},
			wantSent:  [][]string{nil},
			unchanged: true,
		},
		{
			name:      "bearer challenge with failing discovery",
			manager:   func(t *testing.T) *Manager { return newManager(as) },
			answers:   []error{unauthenticated(`Bearer resource_metadata="` + broken.URL + `/prm"`)},
			wantSent:  [][]string{nil},
			unchanged: true,
		},
		{
			name:      "forbidden",
			manager:   func(t *testing.T) *Manager { return withStored(t, stored) },
			answers:   []error{a2a.NewError(a2a.ErrUnauthorized, "forbidden")},
			wantSent:  [][]string{{"Bearer at"}},
			unchanged: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			baseURL := tc.baseURL
			if baseURL == "" {
				baseURL = agent
			}
			req := a2aclient.Request{BaseURL: baseURL, ProtocolBinding: a2a.TransportProtocolJSONRPC, ServiceParams: a2aclient.ServiceParams{}}
			if tc.explicit {
				req.ServiceParams.Append("Authorization", "Bearer user-supplied")
			}
			sent, err := runPlugin(t.Context(), tc.manager(t), req, tc.answers...)
			if diff := cmp.Diff(tc.wantSent, sent); diff != "" {
				t.Fatalf("Authorization of the calls sent wrong result (-want +got) diff = %s", diff)
			}
			callErr := tc.answers[0]
			switch {
			case len(tc.wantSent) > 1:
				if err != nil {
					t.Fatalf("calling through the plugin error = %v, want the repeated call's success", err)
				}
			case tc.unchanged:
				if err != callErr {
					t.Fatalf("calling through the plugin error = %v, want %v unchanged", err, callErr)
				}
			case tc.wantClass != "":
				if got := errClass(err); got != tc.wantClass || !errors.Is(err, callErr) {
					t.Fatalf("calling through the plugin error = %v (%s), want %q wrapping the call's error", err, got, tc.wantClass)
				}
			default:
				var sr *SignInRequiredError
				if !errors.As(err, &sr) || !errors.Is(err, callErr) {
					t.Fatalf("calling through the plugin error = %v, want SignInRequiredError wrapping the call's error", err)
				}
				if diff := cmp.Diff(tc.want, sr, cmpopts.IgnoreFields(SignInRequiredError{}, "Err")); diff != "" {
					t.Fatalf("SignInRequiredError wrong result (-want +got) diff = %s", diff)
				}
			}
		})
	}
}

// TestPluginRefreshRetry checks that a token the agent rejects is refreshed and
// the call repeated once with the new one, and that a second rejection asks
// the user to sign in.
func TestPluginRefreshRetry(t *testing.T) {
	t.Parallel()
	as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{RotateRefreshTokens: true})
	agent := oauthtest.NewProtectedAgent(t, as, oauthtest.AgentOptions{Card: oauthtest.CardMetadataURL})
	m := newManager(as)
	_, g := signInToAgent(t, m, agent.URL)
	req := a2aclient.Request{BaseURL: agent.URL, ProtocolBinding: a2a.TransportProtocolHTTPJSON, Card: fetchCard(t, agent.URL), ServiceParams: a2aclient.ServiceParams{}}
	rejected := unauthenticated(`Bearer error="invalid_token"`)

	sent, err := runPlugin(t.Context(), m, req, rejected)
	if err != nil || len(sent) != 2 || sent[0][0] != g.header() || sent[1][0] == g.header() {
		t.Fatalf("calls sent = %q, error %v, want the stored token, then a refreshed one, and success", sent, err)
	}
	sent, err = runPlugin(t.Context(), m, req, rejected, rejected)
	var sr *SignInRequiredError
	if len(sent) != 2 || !errors.As(err, &sr) || sr.Reason == "" {
		t.Fatalf("calls sent = %q, error %v, want two calls and a SignInRequiredError with a reason", sent, err)
	}
	if !errors.Is(err, a2a.ErrUnauthenticated) {
		t.Fatalf("error = %v, want it to wrap the agent's error", err)
	}
}

// TestPluginStreams checks that a stream is repeated when its error comes
// before its first event, and not after.
func TestPluginStreams(t *testing.T) {
	t.Parallel()
	as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{})
	agent := oauthtest.NewProtectedAgent(t, as, oauthtest.AgentOptions{Card: oauthtest.CardMetadataURL})
	m := newManager(as)
	signInToAgent(t, m, agent.URL)
	req := a2aclient.Request{BaseURL: agent.URL, ProtocolBinding: a2a.TransportProtocolHTTPJSON, Card: fetchCard(t, agent.URL), ServiceParams: a2aclient.ServiceParams{}}
	rejected := unauthenticated(`Bearer error="invalid_token"`)
	for _, tc := range []struct {
		name       string
		failFirst  bool
		wantCalls  int
		wantEvents []any
		wantErr    bool
	}{
		{name: "error before the first event", failFirst: true, wantCalls: 2, wantEvents: []any{"e1", "e2"}},
		{name: "error after an event", wantCalls: 1, wantEvents: []any{"e1"}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			next := func(_ context.Context, _ a2aclient.Request) iter.Seq2[any, error] {
				return func(yield func(any, error) bool) {
					calls++
					if tc.failFirst && calls == 1 {
						yield(nil, rejected)
						return
					}
					if !yield("e1", nil) {
						return
					}
					if !tc.failFirst {
						yield(nil, rejected)
						return
					}
					yield("e2", nil)
				}
			}
			var events []any
			var gotErr error
			for v, err := range m.Plugin().WrapCall(next)(t.Context(), req) {
				if err != nil {
					gotErr = err
					continue
				}
				events = append(events, v)
			}
			if calls != tc.wantCalls || (gotErr != nil) != tc.wantErr {
				t.Fatalf("calls = %d, error %v, want %d calls and error %v", calls, gotErr, tc.wantCalls, tc.wantErr)
			}
			if diff := cmp.Diff(tc.wantEvents, events); diff != "" {
				t.Fatalf("events wrong result (-want +got) diff = %s", diff)
			}
			if tc.wantErr && gotErr != rejected {
				t.Fatalf("error = %v, want the stream's error unchanged", gotErr)
			}
		})
	}
}

// TestPluginKeepsCallerServiceParams checks that attaching a grant doesn't
// change the service parameters of the caller's request, which a plugin that
// repeats the call may still use.
func TestPluginKeepsCallerServiceParams(t *testing.T) {
	t.Parallel()
	m := managerWithGrant(t, "https://agent.example.com", nil, "at", "")
	params := a2aclient.ServiceParams{}
	params.Append("X-Trace", "1")
	req := a2aclient.Request{BaseURL: "https://agent.example.com", ProtocolBinding: a2a.TransportProtocolJSONRPC, ServiceParams: params}
	sent, err := runPlugin(t.Context(), m, req)
	if err != nil || len(sent) != 1 || len(sent[0]) != 1 {
		t.Fatalf("calls sent = %q, error %v, want one call with the grant", sent, err)
	}
	if diff := cmp.Diff(a2aclient.ServiceParams{"x-trace": {"1"}}, params); diff != "" {
		t.Fatalf("caller's service parameters changed (-want +got) diff = %s", diff)
	}
}

// TestPluginWithAuthInterceptor runs the plugin with an a2aclient.AuthInterceptor,
// which replaces the OAuth token with its own credential: the plugin spends one
// refresh on the agent's 401 for that credential, then asks for a sign-in.
func TestPluginWithAuthInterceptor(t *testing.T) {
	t.Parallel()
	as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{})
	agent := oauthtest.NewProtectedAgent(t, as, oauthtest.AgentOptions{Card: oauthtest.CardMetadataURL})
	m := newManager(as)
	signInToAgent(t, m, agent.URL)
	creds := a2aclient.NewInMemoryCredentialsStore()
	creds.Set("session", "oauth", "other-token")
	client, err := a2aclient.NewFromCard(t.Context(), fetchCard(t, agent.URL),
		a2aclient.WithRESTTransport(&http.Client{}),
		a2aclient.WithPlugins(m.Plugin()),
		a2aclient.WithCallInterceptors(&a2aclient.AuthInterceptor{Service: creds}),
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
	if diff := cmp.Diff([]string{"Bearer other-token", "Bearer other-token"}, agent.AuthorizationHeaders()); diff != "" {
		t.Fatalf("Authorization headers the agent received wrong result (-want +got) diff = %s", diff)
	}
}

// TestPluginPlain401Probes checks that a 401 without a Bearer challenge is
// returned as it is, without probing the agent for Protected Resource
// Metadata, and that a Bearer challenge without a metadata URL is followed once
// per call.
func TestPluginPlain401Probes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		challenge string
		wantPer   bool
	}{
		{name: "no challenge"},
		{name: "bearer challenge without metadata URL", challenge: "Bearer", wantPer: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var probes atomic.Int32
			agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.URL.Path, wellKnownResourceMetadata) {
					probes.Add(1)
					http.NotFound(w, r)
					return
				}
				if tc.challenge != "" {
					w.Header().Set("WWW-Authenticate", tc.challenge)
				}
				http.Error(w, "unauthorized", http.StatusUnauthorized)
			}))
			t.Cleanup(agent.Close)
			m := managerWithGrant(t, "", nil, "", "")
			endpoint := agent.URL + "/a/b/c/rpc"
			client, err := a2aclient.NewFromEndpoints(t.Context(), []*a2a.AgentInterface{a2a.NewAgentInterface(endpoint, a2a.TransportProtocolJSONRPC)},
				a2aclient.WithJSONRPCTransport(&http.Client{}),
				a2aclient.WithPlugins(m.Plugin()),
			)
			if err != nil {
				t.Fatalf("a2aclient.NewFromEndpoints() error = %v", err)
			}
			target, err := url.Parse(endpoint)
			if err != nil {
				t.Fatal(err)
			}
			want := 0
			const calls = 3
			if tc.wantPer {
				want = calls * len(resourceMetadataURLs(target))
			}
			for range calls {
				if _, err := client.GetTask(t.Context(), &a2a.GetTaskRequest{ID: "task"}); !errors.Is(err, a2a.ErrUnauthenticated) || errClass(err) != "unclassified *a2a.Error" {
					t.Fatalf("Client.GetTask() error = %v (%s), want the agent's 401 as it is", err, errClass(err))
				}
			}
			if got := int(probes.Load()); got != want {
				t.Fatalf("Protected Resource Metadata probes for %d calls = %d, want %d", calls, got, want)
			}
		})
	}
}

// resourceMetadataServer serves Protected Resource Metadata for resource at /prm
// that names the authorization server issuer.
func resourceMetadataServer(t *testing.T, resource, issuer string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/prm" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := fmt.Fprintf(w, `{"resource":%q,"authorization_servers":[%q]}`, resource, issuer); err != nil {
			t.Errorf("writing resource metadata error = %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestPluginRejectedTokenWithoutRefreshToken checks that a token the agent
// rejects, which can't be refreshed, is not sent again.
func TestPluginRejectedTokenWithoutRefreshToken(t *testing.T) {
	t.Parallel()
	const agent = "https://agent.example.com"
	m := managerWithGrant(t, agent, nil, "at", "")
	req := a2aclient.Request{BaseURL: agent, ProtocolBinding: a2a.TransportProtocolHTTPJSON, Card: inlineCard(false), ServiceParams: a2aclient.ServiceParams{}}
	rejected := unauthenticated(`Bearer error="invalid_token"`)
	var carrying int
	var errs []error
	for range 3 {
		sent, err := runPlugin(t.Context(), m, req, rejected)
		for _, auth := range sent {
			if len(auth) > 0 {
				carrying++
			}
		}
		errs = append(errs, err)
	}
	if carrying != 1 {
		t.Fatalf("calls carrying the rejected token = %d, want 1", carrying)
	}
	for i, err := range errs {
		if errClass(err) != "sign-in required" {
			t.Fatalf("call %d error = %v, want SignInRequiredError", i, err)
		}
	}
}

// TestPluginRetryAfterDiscoveryInsufficientScope checks that a call repeated
// with the stored grant that discovery found, and refused for want of a scope,
// asks for that scope.
func TestPluginRetryAfterDiscoveryInsufficientScope(t *testing.T) {
	t.Parallel()
	as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{})
	const agent = "http://127.0.0.1:1"
	prm := resourceMetadataServer(t, agent, as.URL)
	m := newManager(as)
	g := withKey(newGrantKey(as.URL, "test-client", agent, nil), Grant{AccessToken: "at", ExpiresAt: time.Now().Add(time.Hour), Endpoints: testEndpoints(as)})
	if err := m.Store.Put(t.Context(), g); err != nil {
		t.Fatal(err)
	}
	// A grant that covers the agent is attached from the start, so hide it
	// until the first call is sent, as when it is stored while the call runs.
	hidden := &hidingStore{Store: m.Store, hide: true}
	m.Store = hidden
	answers := []error{
		unauthenticated(fmt.Sprintf(`Bearer resource_metadata=%q`, prm.URL+"/prm")),
		unauthenticated(`Bearer error="insufficient_scope", scope="b"`),
	}
	var sent [][]string
	next := func(_ context.Context, r a2aclient.Request) iter.Seq2[any, error] {
		return func(yield func(any, error) bool) {
			hidden.hide = false
			sent = append(sent, r.ServiceParams.Get("authorization"))
			yield(nil, answers[len(sent)-1])
		}
	}
	req := a2aclient.Request{BaseURL: agent, ProtocolBinding: a2a.TransportProtocolJSONRPC, ServiceParams: a2aclient.ServiceParams{}}
	var err error
	for _, e := range m.Plugin().WrapCall(next)(t.Context(), req) {
		err = e
	}
	if diff := cmp.Diff([][]string{nil, {"Bearer at"}}, sent); diff != "" {
		t.Fatalf("Authorization of the calls sent wrong result (-want +got) diff = %s", diff)
	}
	var sr *SignInRequiredError
	if !errors.As(err, &sr) || !slices.Contains(sr.Requirement.Scopes, "b") {
		t.Fatalf("error = %v, want a SignInRequiredError asking for scope b", err)
	}
}

// hidingStore lists no grants while hide is set.
type hidingStore struct {
	Store
	hide bool
}

func (s *hidingStore) List(ctx context.Context) ([]Grant, error) {
	if s.hide {
		return nil, nil
	}
	return s.Store.List(ctx)
}

// TestPluginGRPCRefresh checks that over the gRPC binding, whose errors carry
// the response metadata too, a rejected token is refreshed and the call
// repeated.
func TestPluginGRPCRefresh(t *testing.T) {
	t.Parallel()
	as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{})
	const target = "dns:///agent.example.com:443"
	m := newManager(as)
	g, err := signIn(t, m, &Requirement{Endpoints: testEndpoints(as), Resource: target}, false)
	if err != nil {
		t.Fatalf("signing in error = %v", err)
	}
	req := a2aclient.Request{BaseURL: target, ProtocolBinding: a2a.TransportProtocolGRPC, ServiceParams: a2aclient.ServiceParams{}}
	sent, err := runPlugin(t.Context(), m, req, unauthenticated(`Bearer error="invalid_token"`))
	if err != nil || len(sent) != 2 || sent[0][0] != g.header() || sent[1][0] == g.header() {
		t.Fatalf("calls sent = %q, error %v, want the stored token, then a refreshed one, and success", sent, err)
	}
}
