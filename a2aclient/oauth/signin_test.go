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
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2aclient/oauth/oauthtest"
)

func TestManagerSignInFlows(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		server     oauthtest.ServerOptions
		card       oauthtest.CardSecurity
		showURL    bool
		showCode   bool
		path       string
		wantDevice bool
		wantErr    error
		wantAnyErr bool
	}{
		{name: "browser only", showURL: true},
		{name: "browser with a redirect path", showURL: true, path: "/oauth/callback"},
		{name: "device only", showCode: true, wantDevice: true},
		{name: "both", showURL: true, showCode: true},
		{name: "both, card prefers the device flow", card: oauthtest.CardDeviceCode, showURL: true, showCode: true, wantDevice: true},
		{name: "both, server without authorization endpoint", server: oauthtest.ServerOptions{NoAuthorizationEndpoint: true}, showURL: true, showCode: true, wantDevice: true},
		{name: "browser only, server without authorization endpoint", server: oauthtest.ServerOptions{NoAuthorizationEndpoint: true}, showURL: true, wantErr: ErrNoAuthorizationEndpoint},
		{name: "neither", wantAnyErr: true},
		{name: "relative redirect path", showURL: true, path: "callback", wantAnyErr: true},
		{name: "redirect path with a wildcard", showURL: true, path: "/callback/{bad", wantAnyErr: true},
		{name: "redirect path with a query", showURL: true, path: "/callback?x=1", wantAnyErr: true},
		{name: "redirect path with a space", showURL: true, path: "/call back", wantAnyErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			as := oauthtest.NewAuthServer(t, tc.server)
			card := tc.card
			if card == oauthtest.CardNone {
				card = oauthtest.CardMetadataURL
			}
			agent := oauthtest.NewProtectedAgent(t, as, oauthtest.AgentOptions{Card: card, Scopes: []string{"a2a.invoke"}})
			m := newManager(as)
			m.sleep = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
			req, err := m.Discover(t.Context(), agent.URL, fetchCard(t, agent.URL))
			if err != nil || req == nil {
				t.Fatalf("Manager.Discover() = %v, %v, want a requirement", req, err)
			}
			browser := oauthtest.Browser(t)
			var shownURL string
			var shownCode DeviceCode
			opts := SignInOptions{RedirectPath: tc.path}
			if tc.showURL {
				opts.ShowURL = func(u string) { shownURL = u; browser(u) }
			}
			if tc.showCode {
				opts.ShowCode = func(dc DeviceCode) { shownCode = dc; browser(dc.VerificationURIComplete) }
			}
			g, err := m.SignIn(t.Context(), req, opts)
			switch {
			case tc.wantErr != nil:
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("Manager.SignIn() error = %v, want %v", err, tc.wantErr)
				}
				return
			case tc.wantAnyErr:
				if err == nil {
					t.Fatal("Manager.SignIn() error = nil, want one")
				}
				return
			case err != nil:
				t.Fatalf("Manager.SignIn() error = %v", err)
			}
			if !as.Valid(g.AccessToken, agent.URL, []string{"a2a.invoke"}) {
				t.Fatal("Manager.SignIn() returned a token the agent does not accept")
			}
			if tc.wantDevice != (shownCode.UserCode != "") || tc.wantDevice == (shownURL != "") {
				t.Fatalf("Manager.SignIn() showed URL %q and code %+v, want the device flow %v", shownURL, shownCode, tc.wantDevice)
			}
			if tc.wantDevice && (shownCode.VerificationURI == "" || shownCode.VerificationURIComplete == "") {
				t.Fatalf("Manager.SignIn() showed code %+v, want the verification URIs", shownCode)
			}
			if !tc.wantDevice {
				u, err := url.Parse(shownURL)
				if err != nil {
					t.Fatal(err)
				}
				want := tc.path
				if want == "" {
					want = defaultRedirectPath
				}
				redirect, err := url.Parse(u.Query().Get("redirect_uri"))
				if err != nil || redirect.Hostname() != "127.0.0.1" || redirect.Path != want {
					t.Fatalf("redirect URI = %q, want http://127.0.0.1:<port>%s", u.Query().Get("redirect_uri"), want)
				}
			}
			stored, err := m.grantForRequirement(t.Context(), req)
			if err != nil || stored == nil || stored.AccessToken != g.AccessToken {
				t.Fatalf("stored grant after Manager.SignIn() = %v, %v, want the new grant", stored, err)
			}
		})
	}
}

// TestManagerSignInCanceled checks that SignIn stops waiting for the redirect
// when ctx is done.
func TestManagerSignInCanceled(t *testing.T) {
	t.Parallel()
	as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{})
	m := newManager(as)
	ctx, cancel := context.WithCancel(t.Context())
	req := &Requirement{Endpoints: testEndpoints(as), Resource: "https://agent.example.com"}
	_, err := m.SignIn(ctx, req, SignInOptions{ShowURL: func(string) { cancel() }})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Manager.SignIn() error = %v, want %v", err, context.Canceled)
	}
}

// TestManagerSignInIgnoresStrayRedirects checks that a request to the loopback
// redirect URI that is not the authorization response, such as one with
// another state, doesn't end the sign-in.
func TestManagerSignInIgnoresStrayRedirects(t *testing.T) {
	t.Parallel()
	as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{})
	agent := oauthtest.NewProtectedAgent(t, as, oauthtest.AgentOptions{Card: oauthtest.CardMetadataURL})
	m := newManager(as)
	req, err := m.Discover(t.Context(), agent.URL, fetchCard(t, agent.URL))
	if err != nil {
		t.Fatal(err)
	}
	browser := oauthtest.Browser(t)
	g, err := m.SignIn(t.Context(), req, SignInOptions{ShowURL: func(u string) {
		au, err := url.Parse(u)
		if err != nil {
			t.Error(err)
			return
		}
		stray := au.Query().Get("redirect_uri") + "?state=other&code=stolen"
		browser(stray)
		browser(u)
	}})
	if err != nil || !as.Valid(g.AccessToken, agent.URL, nil) {
		t.Fatalf("Manager.SignIn() = %v, %v, want a grant from the real redirect", g, err)
	}
}

func TestManagerBeginCompleteSignIn(t *testing.T) {
	t.Parallel()
	as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{})
	agent := oauthtest.NewProtectedAgent(t, as, oauthtest.AgentOptions{Card: oauthtest.CardMetadataURL})
	m := newManager(as)
	req, err := m.Discover(t.Context(), agent.URL, fetchCard(t, agent.URL))
	if err != nil {
		t.Fatal(err)
	}
	p, err := m.BeginSignIn(t.Context(), req, testRedirectURI, nil)
	if err != nil {
		t.Fatalf("Manager.BeginSignIn() error = %v", err)
	}
	if p.State == "" || !strings.Contains(p.URL, url.QueryEscape(p.State)) {
		t.Fatalf("PendingSignIn = %+v, want its state in its URL", p)
	}
	query := redirectQuery(t, p.ar)
	wrong := url.Values{"state": {"other"}, "code": query["code"]}
	if _, err := m.CompleteSignIn(t.Context(), p, wrong); err == nil {
		t.Fatal("Manager.CompleteSignIn() with another state error = nil, want one")
	}
	if _, err := m.CompleteSignIn(t.Context(), &PendingSignIn{}, query); err == nil {
		t.Fatal("Manager.CompleteSignIn() with a PendingSignIn not from BeginSignIn error = nil, want one")
	}
	g, err := m.CompleteSignIn(t.Context(), p, query)
	if err != nil || !as.Valid(g.AccessToken, agent.URL, nil) {
		t.Fatalf("Manager.CompleteSignIn() = %v, %v, want a valid grant", g, err)
	}
}

// TestManagerSignInNilRequirement checks that SignIn and BeginSignIn report a
// missing requirement, as Discover returns for an agent that needs no OAuth,
// instead of panicking.
func TestManagerSignInNilRequirement(t *testing.T) {
	t.Parallel()
	m := &Manager{}
	if _, err := m.SignIn(t.Context(), nil, SignInOptions{ShowURL: func(string) {}}); err == nil {
		t.Error("Manager.SignIn(nil) error = nil, want one")
	}
	if _, err := m.BeginSignIn(t.Context(), nil, testRedirectURI, nil); err == nil {
		t.Error("Manager.BeginSignIn(nil) error = nil, want one")
	}
}
