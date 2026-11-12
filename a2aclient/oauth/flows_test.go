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
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2aclient/oauth/oauthtest"
	"github.com/google/go-cmp/cmp"
)

const testRedirectURI = "http://127.0.0.1:8123/callback"

var testNow = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

func testEndpoints(as *oauthtest.AuthServer) Endpoints {
	md := as.Metadata()
	e := Endpoints{Issuer: as.URL, Token: md["token_endpoint"].(string), DeviceAuthorization: md["device_authorization_endpoint"].(string)}
	if v, ok := md["authorization_endpoint"].(string); ok {
		e.Authorization = v
	}
	if v, ok := md["revocation_endpoint"].(string); ok {
		e.Revocation = v
	}
	return e
}

func testKey(as *oauthtest.AuthServer, scopes ...string) GrantKey {
	return NewGrantKey(as.URL, "client-1", "https://agent.example.com", scopes)
}

// redirectQuery follows the authorization URL as a browser would, approving the
// request, and returns the query the authorization server redirected back with.
func redirectQuery(t *testing.T, ar *AuthorizationRequest) url.Values {
	t.Helper()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get(ar.URL)
	if err != nil {
		t.Fatalf("GET %s error = %v", ar.URL, err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Errorf("resp.Body.Close() error = %v", err)
	}
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("GET %s status = %d, want a redirect", ar.URL, resp.StatusCode)
	}
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("url.Parse(Location) error = %v", err)
	}
	if got := loc.Scheme + "://" + loc.Host + loc.Path; got != ar.RedirectURI {
		t.Fatalf("redirect to %s, want %s", got, ar.RedirectURI)
	}
	return loc.Query()
}

// authorize obtains a grant from as with the authorization code flow.
func authorize(t *testing.T, c tokenClient, as *oauthtest.AuthServer, key GrantKey) *Grant {
	t.Helper()
	ar, err := newAuthorizationRequest(testEndpoints(as), key, testRedirectURI, false)
	if err != nil {
		t.Fatalf("newAuthorizationRequest() error = %v", err)
	}
	code, err := ar.ParseRedirect(redirectQuery(t, ar))
	if err != nil {
		t.Fatalf("AuthorizationRequest.ParseRedirect() error = %v", err)
	}
	g, err := c.ExchangeCode(t.Context(), ar, code)
	if err != nil {
		t.Fatalf("tokenClient.ExchangeCode() error = %v", err)
	}
	return g
}

func TestAuthorizationCode(t *testing.T) {
	t.Parallel()
	as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{TokenLifetime: time.Hour})
	c := tokenClient{now: func() time.Time { return testNow }}
	key := testKey(as, "a2a.invoke", "profile")

	ar, err := newAuthorizationRequest(testEndpoints(as), key, testRedirectURI, false)
	if err != nil {
		t.Fatalf("newAuthorizationRequest() error = %v", err)
	}
	q := redirectQuery(t, ar)

	sent := as.LastAuthorizeQuery()
	wantSent := url.Values{
		"response_type":         {"code"},
		"client_id":             {"client-1"},
		"redirect_uri":          {testRedirectURI},
		"state":                 {ar.State},
		"code_challenge":        {pkceChallenge(ar.Verifier)},
		"code_challenge_method": {"S256"},
		"scope":                 {"a2a.invoke profile"},
		"resource":              {"https://agent.example.com"},
	}
	if diff := cmp.Diff(wantSent, sent); diff != "" {
		t.Fatalf("authorization request query wrong (-want +got) diff = %s", diff)
	}
	if !regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`).MatchString(ar.Verifier) || len(ar.State) < 22 {
		t.Fatalf("newAuthorizationRequest() verifier %q, state %q, want a verifier of 43 to 128 unreserved characters (RFC 7636 §4.1) and a state of at least 128 bits", ar.Verifier, ar.State)
	}

	code, err := ar.ParseRedirect(q)
	if err != nil {
		t.Fatalf("AuthorizationRequest.ParseRedirect() error = %v", err)
	}
	g, err := c.ExchangeCode(t.Context(), ar, code)
	if err != nil {
		t.Fatalf("tokenClient.ExchangeCode() error = %v", err)
	}
	if !as.Valid(g.AccessToken, key.Resource, key.Scopes) || g.RefreshToken == "" {
		t.Fatalf("tokenClient.ExchangeCode() = %+v, want a valid access token for %s and a refresh token", g, key.Resource)
	}
	want := &Grant{
		Key:           key,
		GrantedScopes: []string{"a2a.invoke", "profile"},
		AccessToken:   g.AccessToken,
		RefreshToken:  g.RefreshToken,
		Refreshable:   true,
		TokenType:     "Bearer",
		ExpiresAt:     testNow.Add(time.Hour),
		Endpoints:     testEndpoints(as),
		CreatedAt:     testNow,
	}
	if diff := cmp.Diff(want, g); diff != "" {
		t.Fatalf("tokenClient.ExchangeCode() wrong grant (-want +got) diff = %s", diff)
	}
	wantForm := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {"client-1"},
		"resource":      {"https://agent.example.com"},
		"code":          {code},
		"redirect_uri":  {testRedirectURI},
		"code_verifier": {ar.Verifier},
	}
	if diff := cmp.Diff(wantForm, as.LastForm("/token")); diff != "" {
		t.Fatalf("token request form wrong (-want +got) diff = %s", diff)
	}
}

func TestAuthorizationCodeDenied(t *testing.T) {
	t.Parallel()
	as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{DenyAuthorization: true})
	ar, err := newAuthorizationRequest(testEndpoints(as), testKey(as), testRedirectURI, false)
	if err != nil {
		t.Fatalf("newAuthorizationRequest() error = %v", err)
	}
	_, err = ar.ParseRedirect(redirectQuery(t, ar))
	if !isOAuthError(err, "access_denied") {
		t.Fatalf("AuthorizationRequest.ParseRedirect() error = %v, want access_denied", err)
	}
}

func TestExchangeCodeChecksVerifier(t *testing.T) {
	t.Parallel()
	as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{})
	ar, err := newAuthorizationRequest(testEndpoints(as), testKey(as), testRedirectURI, false)
	if err != nil {
		t.Fatalf("newAuthorizationRequest() error = %v", err)
	}
	code, err := ar.ParseRedirect(redirectQuery(t, ar))
	if err != nil {
		t.Fatalf("AuthorizationRequest.ParseRedirect() error = %v", err)
	}
	other := *ar
	other.Verifier = newVerifier()
	if _, err := (tokenClient{}).ExchangeCode(t.Context(), &other, code); !isOAuthError(err, "invalid_grant") {
		t.Fatalf("tokenClient.ExchangeCode() with another verifier error = %v, want invalid_grant", err)
	}
}

func TestNewAuthorizationRequestErrors(t *testing.T) {
	t.Parallel()
	e := Endpoints{Issuer: "https://login.example.com", Authorization: "https://login.example.com/authorize", Token: "https://login.example.com/token"}
	key := NewGrantKey(e.Issuer, "c", "https://agent.example.com", nil)
	tests := []struct {
		name        string
		endpoints   func(Endpoints) Endpoints
		redirectURI string
		wantIs      error
		wantErr     string
	}{
		{
			name:        "no authorization endpoint",
			endpoints:   func(e Endpoints) Endpoints { e.Authorization = ""; return e },
			redirectURI: testRedirectURI,
			wantIs:      ErrNoAuthorizationEndpoint,
		},
		{
			name:        "insecure authorization endpoint",
			endpoints:   func(e Endpoints) Endpoints { e.Authorization = "http://login.example.com/authorize"; return e },
			redirectURI: testRedirectURI,
			wantErr:     "https",
		},
		{
			name:      "no redirect URI",
			endpoints: func(e Endpoints) Endpoints { return e },
			wantErr:   "no redirect URI",
		},
		{
			name:        "key for another issuer",
			endpoints:   func(e Endpoints) Endpoints { e.Issuer = "https://other.example.com"; return e },
			redirectURI: testRedirectURI,
			wantErr:     `grant key for issuer "https://login.example.com" used with authorization server https://other.example.com`,
		},
		{
			name:        "key issuer with a trailing slash",
			endpoints:   func(e Endpoints) Endpoints { e.Issuer += "/"; return e },
			redirectURI: testRedirectURI,
		},
		{
			name:        "endpoints without an issuer",
			endpoints:   func(e Endpoints) Endpoints { e.Issuer = ""; return e },
			redirectURI: testRedirectURI,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := newAuthorizationRequest(tc.endpoints(e), key, tc.redirectURI, false)
			if tc.wantIs == nil && tc.wantErr == "" {
				if err != nil {
					t.Fatalf("newAuthorizationRequest() error = %v, want nil", err)
				}
				return
			}
			if err == nil || (tc.wantIs != nil && !errors.Is(err, tc.wantIs)) || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("newAuthorizationRequest() error = %v, want %v containing %q", err, tc.wantIs, tc.wantErr)
			}
		})
	}
}

func TestParseRedirect(t *testing.T) {
	t.Parallel()
	const issuer = "https://login.example.com"
	tests := []struct {
		name     string
		ar       *AuthorizationRequest
		query    url.Values
		wantCode string
		wantErr  error
	}{
		{name: "code", query: url.Values{"state": {"s1"}, "code": {"c0de"}}, wantCode: "c0de"},
		{name: "error", query: url.Values{"state": {"s1"}, "error": {"access_denied"}, "error_description": {"no"}}, wantErr: &Error{Code: "access_denied", Description: "no"}},
		{name: "wrong state", query: url.Values{"state": {"s2"}, "code": {"c0de"}}, wantErr: errors.New("authorization response has an unexpected state")},
		{name: "no state", query: url.Values{"code": {"c0de"}}, wantErr: errors.New("authorization response has an unexpected state")},
		{name: "error with wrong state", query: url.Values{"state": {"s2"}, "error": {"access_denied"}}, wantErr: errors.New("authorization response has an unexpected state")},
		{name: "no code", query: url.Values{"state": {"s1"}}, wantErr: errors.New("authorization response has no code")},
		{
			name:    "request without state",
			ar:      &AuthorizationRequest{Endpoints: Endpoints{Issuer: issuer}},
			query:   url.Values{"code": {"c0de"}},
			wantErr: errors.New("authorization request has no state"),
		},
		{name: "iss", query: url.Values{"state": {"s1"}, "code": {"c0de"}, "iss": {issuer}}, wantCode: "c0de"},
		{
			name:    "iss of another server",
			query:   url.Values{"state": {"s1"}, "code": {"c0de"}, "iss": {"https://attacker.example.com"}},
			wantErr: errors.New(`authorization response is from issuer "https://attacker.example.com", want "https://login.example.com"`),
		},
		{
			name:    "error with iss of another server",
			query:   url.Values{"state": {"s1"}, "error": {"access_denied"}, "iss": {"https://attacker.example.com"}},
			wantErr: errors.New(`authorization response is from issuer "https://attacker.example.com", want "https://login.example.com"`),
		},
		{
			name:    "empty iss",
			query:   url.Values{"state": {"s1"}, "code": {"c0de"}, "iss": {""}},
			wantErr: errors.New(`authorization response is from issuer "", want "https://login.example.com"`),
		},
		{
			name:    "two iss",
			query:   url.Values{"state": {"s1"}, "code": {"c0de"}, "iss": {issuer, issuer}},
			wantErr: errors.New(`authorization response is from issuer "https://login.example.com, https://login.example.com", want "https://login.example.com"`),
		},
		{
			name:     "iss required",
			ar:       &AuthorizationRequest{State: "s1", Endpoints: Endpoints{Issuer: issuer, AuthorizationResponseIss: true}},
			query:    url.Values{"state": {"s1"}, "code": {"c0de"}, "iss": {issuer}},
			wantCode: "c0de",
		},
		{
			name:    "iss required but missing",
			ar:      &AuthorizationRequest{State: "s1", Endpoints: Endpoints{Issuer: issuer, AuthorizationResponseIss: true}},
			query:   url.Values{"state": {"s1"}, "code": {"c0de"}},
			wantErr: errors.New("authorization response from https://login.example.com has no iss parameter"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ar := tc.ar
			if ar == nil {
				ar = &AuthorizationRequest{State: "s1", Endpoints: Endpoints{Issuer: issuer}}
			}
			code, err := ar.ParseRedirect(tc.query)
			if (err == nil) != (tc.wantErr == nil) || (err != nil && err.Error() != tc.wantErr.Error()) {
				t.Fatalf("AuthorizationRequest.ParseRedirect() error = %v, want %v", err, tc.wantErr)
			}
			if code != tc.wantCode {
				t.Fatalf("AuthorizationRequest.ParseRedirect() = %q, want %q", code, tc.wantCode)
			}
		})
	}
}

// recordSleeps returns a sleep function that records the requested durations
// and returns at once, unless ctx is done.
func recordSleeps() (func(context.Context, time.Duration) error, func() []time.Duration) {
	var mu sync.Mutex
	var slept []time.Duration
	sleep := func(ctx context.Context, d time.Duration) error {
		mu.Lock()
		slept = append(slept, d)
		mu.Unlock()
		return ctx.Err()
	}
	return sleep, func() []time.Duration {
		mu.Lock()
		defer mu.Unlock()
		return slept
	}
}

func TestDeviceAuthorization(t *testing.T) {
	t.Parallel()
	as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{PendingPolls: 2, NoAuthorizationEndpoint: true})
	sleep, slept := recordSleeps()
	c := tokenClient{sleep: sleep}
	key := testKey(as, "a2a.invoke")

	da, err := c.StartDeviceAuthorization(t.Context(), testEndpoints(as), key, false)
	if err != nil {
		t.Fatalf("tokenClient.StartDeviceAuthorization() error = %v", err)
	}
	if da.UserCode == "" || da.VerificationURI != as.URL+"/activate" || da.VerificationURIComplete != as.URL+"/activate?user_code="+da.UserCode || da.Interval != time.Second {
		t.Fatalf("tokenClient.StartDeviceAuthorization() = %+v, want the user code, verification URIs and a 1s interval", da)
	}
	if until := time.Until(da.ExpiresAt); until < 9*time.Minute || until > 10*time.Minute {
		t.Fatalf("tokenClient.StartDeviceAuthorization() ExpiresAt in %v, want in 10 minutes", until)
	}
	wantForm := url.Values{"client_id": {"client-1"}, "scope": {"a2a.invoke"}, "resource": {"https://agent.example.com"}}
	if diff := cmp.Diff(wantForm, as.LastForm("/device")); diff != "" {
		t.Fatalf("device authorization request form wrong (-want +got) diff = %s", diff)
	}

	oauthtest.Browser(t)(da.VerificationURIComplete)
	g, err := c.PollDeviceAuthorization(t.Context(), da)
	if err != nil {
		t.Fatalf("tokenClient.PollDeviceAuthorization() error = %v", err)
	}
	if !as.Valid(g.AccessToken, key.Resource, key.Scopes) || !g.Key.Equal(key) || g.Endpoints != testEndpoints(as) {
		t.Fatalf("tokenClient.PollDeviceAuthorization() = %+v, want a valid grant for %+v", g, key)
	}
	if diff := cmp.Diff([]time.Duration{time.Second, time.Second, time.Second}, slept()); diff != "" {
		t.Fatalf("waits between polls wrong (-want +got) diff = %s", diff)
	}
}

func TestDeviceAuthorizationDenied(t *testing.T) {
	t.Parallel()
	as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{DenyAuthorization: true})
	sleep, _ := recordSleeps()
	c := tokenClient{sleep: sleep}
	da, err := c.StartDeviceAuthorization(t.Context(), testEndpoints(as), testKey(as), false)
	if err != nil {
		t.Fatalf("tokenClient.StartDeviceAuthorization() error = %v", err)
	}
	oauthtest.Browser(t)(da.VerificationURIComplete)
	if _, err := c.PollDeviceAuthorization(t.Context(), da); !isOAuthError(err, "access_denied") {
		t.Fatalf("tokenClient.PollDeviceAuthorization() error = %v, want access_denied", err)
	}
}

// deviceServer answers device authorization requests with deviceBody and token
// polls with the given responses in turn, repeating the last one. A response
// "status N" is an empty answer with HTTP status N.
func deviceServer(t *testing.T, deviceBody string, polls ...string) Endpoints {
	t.Helper()
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/device" {
			_, _ = w.Write([]byte(deviceBody))
			return
		}
		mu.Lock()
		body := polls[0]
		if len(polls) > 1 {
			polls = polls[1:]
		}
		mu.Unlock()
		if code, ok := strings.CutPrefix(body, "status "); ok {
			status, err := strconv.Atoi(code)
			if err != nil {
				t.Errorf("invalid status in poll %q", body)
			}
			w.WriteHeader(status)
			return
		}
		if strings.Contains(body, `"error"`) {
			w.WriteHeader(http.StatusBadRequest)
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return Endpoints{Issuer: srv.URL, Token: srv.URL + "/token", DeviceAuthorization: srv.URL + "/device"}
}

func TestPollDeviceAuthorization(t *testing.T) {
	t.Parallel()
	const device = `{"device_code":"dc","user_code":"UC","verification_uri":"https://login.example.com/activate","interval":2,"expires_in":600}`
	tests := []struct {
		name       string
		polls      []string
		cancel     bool
		expired    bool
		wantSlept  []time.Duration
		wantOAuth  string
		wantIs     error
		wantErr    string
		wantAccess string
	}{
		{
			name:       "slow down",
			polls:      []string{`{"error":"authorization_pending"}`, `{"error":"slow_down"}`, `{"error":"authorization_pending"}`, `{"access_token":"at"}`},
			wantSlept:  []time.Duration{2 * time.Second, 2 * time.Second, 7 * time.Second, 7 * time.Second},
			wantAccess: "at",
		},
		{
			name:       "transient server errors",
			polls:      []string{"status 503", `{"error":"authorization_pending"}`, "status 502", "status 429", `{"access_token":"at"}`},
			wantSlept:  []time.Duration{2 * time.Second, 7 * time.Second, 7 * time.Second, 12 * time.Second, 17 * time.Second},
			wantAccess: "at",
		},
		{
			name:       "temporarily unavailable",
			polls:      []string{`{"error":"temporarily_unavailable"}`, `{"access_token":"at"}`},
			wantSlept:  []time.Duration{2 * time.Second, 7 * time.Second},
			wantAccess: "at",
		},
		{
			name:      "too many server errors in a row",
			polls:     []string{"status 503"},
			wantSlept: []time.Duration{2 * time.Second, 7 * time.Second, 12 * time.Second, 17 * time.Second},
			wantErr:   "HTTP 503",
		},
		{
			name:      "client error",
			polls:     []string{"status 404"},
			wantSlept: []time.Duration{2 * time.Second},
			wantErr:   "HTTP 404",
		},
		{
			name:      "expired token",
			polls:     []string{`{"error":"expired_token"}`},
			wantSlept: []time.Duration{2 * time.Second},
			wantOAuth: "expired_token",
		},
		{
			name:      "device code expired",
			polls:     []string{`{"error":"authorization_pending"}`},
			expired:   true,
			wantSlept: []time.Duration{2 * time.Second},
			wantIs:    context.DeadlineExceeded,
			wantErr:   "the device code expired",
		},
		{
			name:      "canceled",
			polls:     []string{`{"error":"authorization_pending"}`},
			cancel:    true,
			wantSlept: []time.Duration{2 * time.Second},
			wantIs:    context.Canceled,
			wantErr:   "waiting for the device authorization",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sleep, slept := recordSleeps()
			c := tokenClient{sleep: sleep}
			e := deviceServer(t, device, tc.polls...)
			da, err := c.StartDeviceAuthorization(t.Context(), e, NewGrantKey(e.Issuer, "c", "", nil), false)
			if err != nil {
				t.Fatalf("tokenClient.StartDeviceAuthorization() error = %v", err)
			}
			ctx := t.Context()
			if tc.cancel {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if tc.expired {
				da.ExpiresAt = time.Now().Add(-time.Second)
			}
			g, err := c.PollDeviceAuthorization(ctx, da)
			if tc.wantAccess != "" {
				if err != nil || g.AccessToken != tc.wantAccess {
					t.Fatalf("tokenClient.PollDeviceAuthorization() = %+v, %v, want access token %q", g, err, tc.wantAccess)
				}
			} else if err == nil || (tc.wantOAuth != "" && !isOAuthError(err, tc.wantOAuth)) || (tc.wantIs != nil && !errors.Is(err, tc.wantIs)) || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("tokenClient.PollDeviceAuthorization() error = %v, want %q %v containing %q", err, tc.wantOAuth, tc.wantIs, tc.wantErr)
			}
			if diff := cmp.Diff(tc.wantSlept, slept()); diff != "" {
				t.Fatalf("waits between polls wrong (-want +got) diff = %s", diff)
			}
		})
	}
}

func TestStartDeviceAuthorization(t *testing.T) {
	t.Parallel()
	c := tokenClient{now: func() time.Time { return testNow }}
	tests := []struct {
		name    string
		body    string
		want    *DeviceAuthorization
		wantErr string
	}{
		{
			name: "draft verification_url and default interval",
			body: `{"device_code":"dc","user_code":"UC","verification_url":"https://login.example.com/activate"}`,
			want: &DeviceAuthorization{UserCode: "UC", VerificationURI: "https://login.example.com/activate", Interval: 5 * time.Second, deviceCode: "dc"},
		},
		{
			name: "string numbers",
			body: `{"device_code":"dc","user_code":"UC","verification_uri":"https://login.example.com/activate","verification_uri_complete":"https://login.example.com/activate?c=UC","interval":"3","expires_in":"60"}`,
			want: &DeviceAuthorization{
				UserCode:                "UC",
				VerificationURI:         "https://login.example.com/activate",
				VerificationURIComplete: "https://login.example.com/activate?c=UC",
				ExpiresAt:               testNow.Add(time.Minute),
				Interval:                3 * time.Second,
				deviceCode:              "dc",
			},
		},
		{
			name: "absurd numbers",
			body: `{"device_code":"dc","user_code":"UC","verification_uri":"https://login.example.com/activate","interval":1099511627776,"expires_in":1099511627776}`,
			want: &DeviceAuthorization{
				UserCode:        "UC",
				VerificationURI: "https://login.example.com/activate",
				ExpiresAt:       testNow.Add(maxLifetime),
				Interval:        maxLifetime,
				deviceCode:      "dc",
			},
		},
		{name: "no device code", body: `{"user_code":"UC","verification_uri":"https://login.example.com/activate"}`, wantErr: "missing device_code"},
		{name: "no verification uri", body: `{"device_code":"dc","user_code":"UC"}`, wantErr: "missing device_code"},
		{name: "oauth error", body: `{"error":"invalid_client"}`, wantErr: "starting the device authorization: invalid_client"},
		{name: "plain-HTTP verification uri", body: `{"device_code":"dc","user_code":"UC","verification_uri":"http://evil.example.com/activate"}`, wantErr: "unusable verification URI"},
		{name: "javascript verification uri complete", body: `{"device_code":"dc","user_code":"UC","verification_uri":"https://login.example.com/activate","verification_uri_complete":"javascript:alert(1)"}`, wantErr: "unusable verification URI"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := deviceServer(t, tc.body, `{"error":"authorization_pending"}`)
			key := NewGrantKey(e.Issuer, "c", "", nil)
			got, err := c.StartDeviceAuthorization(t.Context(), e, key, false)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("tokenClient.StartDeviceAuthorization() error = %v, want it to contain %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("tokenClient.StartDeviceAuthorization() error = %v", err)
			}
			tc.want.Endpoints, tc.want.Key = e, key
			if diff := cmp.Diff(tc.want, got, cmp.AllowUnexported(DeviceAuthorization{})); diff != "" {
				t.Fatalf("tokenClient.StartDeviceAuthorization() wrong result (-want +got) diff = %s", diff)
			}
		})
	}

	e := Endpoints{Issuer: "https://login.example.com", Token: "https://login.example.com/token"}
	if _, err := c.StartDeviceAuthorization(t.Context(), e, NewGrantKey(e.Issuer, "c", "", nil), false); !errors.Is(err, ErrNoDeviceAuthorizationEndpoint) {
		t.Fatalf("tokenClient.StartDeviceAuthorization() without an endpoint error = %v, want %v", err, ErrNoDeviceAuthorizationEndpoint)
	}
	e.DeviceAuthorization = "https://login.example.com/device"
	wantErr := `grant key for issuer "https://other.example.com" used with authorization server https://login.example.com`
	if _, err := c.StartDeviceAuthorization(t.Context(), e, NewGrantKey("https://other.example.com", "c", "", nil), false); err == nil || err.Error() != wantErr {
		t.Fatalf("tokenClient.StartDeviceAuthorization() with a key for another issuer error = %v, want %v", err, wantErr)
	}
}

func TestRefresh(t *testing.T) {
	t.Parallel()
	for _, rotate := range []bool{false, true} {
		as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{RotateRefreshTokens: rotate})
		later := testNow.Add(30 * time.Minute)
		key := testKey(as, "a2a.invoke")
		g := authorize(t, tokenClient{now: func() time.Time { return testNow }}, as, key)
		before := *g.clone()
		as.Invalidate()

		got, err := tokenClient{now: func() time.Time { return later }}.Refresh(t.Context(), g)
		if err != nil {
			t.Fatalf("tokenClient.Refresh() (rotate %v) error = %v", rotate, err)
		}
		if diff := cmp.Diff(&before, g); diff != "" {
			t.Fatalf("tokenClient.Refresh() (rotate %v) changed its argument (-want +got) diff = %s", rotate, diff)
		}
		if !as.Valid(got.AccessToken, key.Resource, key.Scopes) || as.Valid(g.AccessToken, key.Resource, key.Scopes) {
			t.Fatalf("tokenClient.Refresh() (rotate %v) = %+v, want a new valid access token", rotate, got)
		}
		if rotated := got.RefreshToken != g.RefreshToken; rotated != rotate || got.RefreshToken == "" {
			t.Fatalf("tokenClient.Refresh() (rotate %v) refresh token %q, old %q", rotate, got.RefreshToken, g.RefreshToken)
		}
		if !got.ExpiresAt.Equal(later.Add(time.Hour)) || !got.CreatedAt.Equal(testNow) || !got.Key.Equal(key) {
			t.Fatalf("tokenClient.Refresh() (rotate %v) = %+v, want expiry an hour after the refresh and the original key and creation time", rotate, got)
		}
		wantForm := url.Values{
			"grant_type":    {"refresh_token"},
			"client_id":     {"client-1"},
			"resource":      {"https://agent.example.com"},
			"refresh_token": {g.RefreshToken},
		}
		if diff := cmp.Diff(wantForm, as.LastForm("/token")); diff != "" {
			t.Fatalf("refresh request form (rotate %v) wrong (-want +got) diff = %s", rotate, diff)
		}
	}
}

func TestRefreshErrors(t *testing.T) {
	t.Parallel()
	as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{})
	g := authorize(t, tokenClient{}, as, testKey(as))
	as.RevokeRefreshTokens()
	if _, err := (tokenClient{}).Refresh(t.Context(), g); !isOAuthError(err, "invalid_grant") {
		t.Fatalf("tokenClient.Refresh() with a revoked refresh token error = %v, want invalid_grant", err)
	}
	g.RefreshToken = ""
	if _, err := (tokenClient{}).Refresh(t.Context(), g); err == nil {
		t.Fatal("tokenClient.Refresh() without a refresh token error = nil, want an error")
	}
}

func TestRevoke(t *testing.T) {
	t.Parallel()
	as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{})
	key := testKey(as)
	c := tokenClient{}

	g := authorize(t, c, as, key)
	if err := c.Revoke(t.Context(), g); err != nil {
		t.Fatalf("tokenClient.Revoke() error = %v", err)
	}
	accessOnly := authorize(t, c, as, key)
	accessOnly.RefreshToken = ""
	if err := c.Revoke(t.Context(), accessOnly); err != nil {
		t.Fatalf("tokenClient.Revoke() of an access token error = %v", err)
	}
	if diff := cmp.Diff([]string{g.RefreshToken, accessOnly.AccessToken}, as.Revoked()); diff != "" {
		t.Fatalf("revoked tokens wrong (-want +got) diff = %s", diff)
	}
	if as.Valid(g.AccessToken, key.Resource, nil) {
		t.Fatal("access token still valid after revoking its refresh token")
	}

	accessOnly.AccessToken = ""
	if err := c.Revoke(t.Context(), accessOnly); err == nil {
		t.Fatal("tokenClient.Revoke() of a grant without tokens error = nil, want an error")
	}
	noRevocation := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{NoRevocation: true})
	g = authorize(t, c, noRevocation, testKey(noRevocation))
	if err := c.Revoke(t.Context(), g); !errors.Is(err, ErrNoRevocationEndpoint) {
		t.Fatalf("tokenClient.Revoke() without a revocation endpoint error = %v, want %v", err, ErrNoRevocationEndpoint)
	}
}

// formServer answers POSTs with the body respond returns for their form, and
// returns the forms it received.
func formServer(t *testing.T, respond func(url.Values) string) (Endpoints, func() []url.Values) {
	t.Helper()
	var mu sync.Mutex
	var forms []url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm() error = %v", err)
		}
		mu.Lock()
		forms = append(forms, r.PostForm)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(respond(r.PostForm)))
	}))
	t.Cleanup(srv.Close)
	e := Endpoints{Issuer: srv.URL, Token: srv.URL + "/token"}
	return e, func() []url.Values {
		mu.Lock()
		defer mu.Unlock()
		return forms
	}
}

func TestTokenRequestsDoNotFollowRedirects(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		endpoint func(Endpoints) string
		call     func(tokenClient, *Grant) error
	}{
		{
			name:     "refresh",
			endpoint: func(e Endpoints) string { return e.Token },
			call: func(c tokenClient, g *Grant) error {
				_, err := c.Refresh(t.Context(), g)
				return err
			},
		},
		{
			name:     "device authorization",
			endpoint: func(e Endpoints) string { return e.DeviceAuthorization },
			call: func(c tokenClient, g *Grant) error {
				_, err := c.StartDeviceAuthorization(t.Context(), g.Endpoints, g.Key, false)
				return err
			},
		},
		{
			name:     "revocation",
			endpoint: func(e Endpoints) string { return e.Revocation },
			call:     func(c tokenClient, g *Grant) error { return c.Revoke(t.Context(), g) },
		},
	}
	for _, tc := range tests {
		for _, status := range []int{http.StatusTemporaryRedirect, http.StatusFound} {
			t.Run(fmt.Sprintf("%s %d", tc.name, status), func(t *testing.T) {
				t.Parallel()
				var hits atomic.Int32
				target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					hits.Add(1)
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"access_token":"stolen","token_type":"Bearer","device_code":"dc","user_code":"UC","verification_uri":"https://x.example.com"}`))
				}))
				t.Cleanup(target.Close)
				as := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					http.Redirect(w, r, target.URL+"/steal", status)
				}))
				t.Cleanup(as.Close)
				e := Endpoints{Issuer: as.URL, Token: as.URL + "/token", DeviceAuthorization: as.URL + "/device", Revocation: as.URL + "/revoke"}
				g := &Grant{Key: NewGrantKey(as.URL, "c", "https://agent.example.com", nil), RefreshToken: "secret-rt", Endpoints: e}

				err := tc.call(tokenClient{}, g)
				wantErr := fmt.Sprintf("POST %s: HTTP %d redirect not followed", tc.endpoint(e), status)
				if err == nil || !strings.Contains(err.Error(), wantErr) {
					t.Fatalf("%s error = %v, want it to contain %q", tc.name, err, wantErr)
				}
				if got := hits.Load(); got != 0 {
					t.Fatalf("redirect target got %d requests, want 0", got)
				}
			})
		}
	}
}

func TestGrantedScopes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		codeResponse    string
		refreshResponse string
		wantGranted     []string
		wantRefreshed   []string
	}{
		{
			name:            "down-scoped",
			codeResponse:    `{"access_token":"at","refresh_token":"rt","scope":"read"}`,
			refreshResponse: `{"access_token":"at2"}`,
			wantGranted:     []string{"read"},
			wantRefreshed:   []string{"read"},
		},
		{
			name:            "scope not reported",
			codeResponse:    `{"access_token":"at","refresh_token":"rt"}`,
			refreshResponse: `{"access_token":"at2","scope":"write read"}`,
			wantRefreshed:   []string{"read", "write"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e, forms := formServer(t, func(form url.Values) string {
				if form.Get("grant_type") == grantTypeRefreshToken {
					return tc.refreshResponse
				}
				return tc.codeResponse
			})
			ar := &AuthorizationRequest{Endpoints: e, Key: NewGrantKey(e.Issuer, "c", "", []string{"write", "read"})}
			g, err := tokenClient{}.ExchangeCode(t.Context(), ar, "c0de")
			if err != nil {
				t.Fatalf("tokenClient.ExchangeCode() error = %v", err)
			}
			if diff := cmp.Diff(tc.wantGranted, g.GrantedScopes); diff != "" {
				t.Fatalf("tokenClient.ExchangeCode() GrantedScopes wrong result (-want +got) diff = %s", diff)
			}
			refreshed, err := tokenClient{}.Refresh(t.Context(), g)
			if err != nil {
				t.Fatalf("tokenClient.Refresh() error = %v", err)
			}
			if got, ok := forms()[1]["scope"]; ok {
				t.Fatalf("tokenClient.Refresh() sent scope %q, want none", got)
			}
			if diff := cmp.Diff(tc.wantRefreshed, refreshed.GrantedScopes); diff != "" {
				t.Fatalf("tokenClient.Refresh() GrantedScopes wrong result (-want +got) diff = %s", diff)
			}
		})
	}
}

func TestOmitResource(t *testing.T) {
	t.Parallel()
	const resource = "https://agent.example.com"
	tests := []struct {
		name  string
		grant func(*testing.T, tokenClient, *oauthtest.AuthServer, GrantKey) (*Grant, []url.Values)
	}{
		{
			name: "authorization code",
			grant: func(t *testing.T, c tokenClient, as *oauthtest.AuthServer, key GrantKey) (*Grant, []url.Values) {
				ar, err := newAuthorizationRequest(testEndpoints(as), key, testRedirectURI, true)
				if err != nil {
					t.Fatalf("newAuthorizationRequest() error = %v", err)
				}
				code, err := ar.ParseRedirect(redirectQuery(t, ar))
				if err != nil {
					t.Fatalf("AuthorizationRequest.ParseRedirect() error = %v", err)
				}
				g, err := c.ExchangeCode(t.Context(), ar, code)
				if err != nil {
					t.Fatalf("tokenClient.ExchangeCode() error = %v", err)
				}
				return g, []url.Values{as.LastAuthorizeQuery(), as.LastForm("/token")}
			},
		},
		{
			name: "device authorization",
			grant: func(t *testing.T, c tokenClient, as *oauthtest.AuthServer, key GrantKey) (*Grant, []url.Values) {
				da, err := c.StartDeviceAuthorization(t.Context(), testEndpoints(as), key, true)
				if err != nil {
					t.Fatalf("tokenClient.StartDeviceAuthorization() error = %v", err)
				}
				oauthtest.Browser(t)(da.VerificationURIComplete)
				g, err := c.PollDeviceAuthorization(t.Context(), da)
				if err != nil {
					t.Fatalf("tokenClient.PollDeviceAuthorization() error = %v", err)
				}
				return g, []url.Values{as.LastForm("/device"), as.LastForm("/token")}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{})
			sleep, _ := recordSleeps()
			c := tokenClient{sleep: sleep}
			key := NewGrantKey(as.URL, "client-1", resource, []string{"a2a.invoke"})

			g, forms := tc.grant(t, c, as, key)
			if !g.OmitResource || g.Key.Resource != resource {
				t.Fatalf("grant OmitResource = %v, Key.Resource = %q, want true, %q", g.OmitResource, g.Key.Resource, resource)
			}
			if _, err := c.Refresh(t.Context(), g); err != nil {
				t.Fatalf("tokenClient.Refresh() error = %v", err)
			}
			for _, form := range append(forms, as.LastForm("/token")) {
				if form.Has("resource") {
					t.Fatalf("request %v has a resource, want none", form)
				}
			}
		})
	}
}

func TestDefaultHTTPClient(t *testing.T) {
	t.Parallel()
	if got := (tokenClient{}).fetcher().client; got != defaultHTTPClient || got.Timeout != 30*time.Second {
		t.Fatalf("tokenClient{}.fetcher().client = %+v, want defaultHTTPClient with a 30s timeout", got)
	}

	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })
	g := &Grant{Key: NewGrantKey(srv.URL, "c", "", nil), RefreshToken: "rt", Endpoints: Endpoints{Issuer: srv.URL, Token: srv.URL + "/token"}}
	_, err := tokenClient{timeout: 50 * time.Millisecond}.Refresh(context.Background(), g)
	var netErr net.Error
	if !errors.As(err, &netErr) || !netErr.Timeout() {
		t.Fatalf("tokenClient.Refresh() of a hung endpoint error = %v, want a timeout", err)
	}
}
