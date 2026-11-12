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

// Package oauthtest provides an in-process OAuth authorization server and an
// OAuth-protected A2A agent for tests.
package oauthtest

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/log"
)

const (
	wellKnownServerMetadata = "/.well-known/oauth-authorization-server"
	wellKnownOpenIDConfig   = "/.well-known/openid-configuration"
)

// ServerOptions configure an [AuthServer].
type ServerOptions struct {
	// RotateRefreshTokens issues a new refresh token on every refresh and
	// invalidates the old one.
	RotateRefreshTokens bool
	// TokenLifetime is the access token lifetime. Defaults to an hour.
	TokenLifetime time.Duration
	// CIMD advertises client_id_metadata_document_supported.
	CIMD bool
	// NoAuthorizationEndpoint leaves out the authorization endpoint, so only the
	// device flow is available.
	NoAuthorizationEndpoint bool
	// NoRevocation leaves out the revocation endpoint.
	NoRevocation bool
	// OIDCOnly serves metadata only at the OpenID Connect Discovery location.
	OIDCOnly bool
	// IssuerPath is appended to the server URL to form the issuer, as in
	// "/oauth2/default". The endpoints are served below the issuer, and the RFC
	// 8414 metadata with the well-known segment inserted before the path.
	IssuerPath string
	// AppendedMetadataOnly serves metadata only with the RFC 8414 well-known
	// segment appended to the issuer, as Okta custom authorization servers do.
	AppendedMetadataOnly bool
	// SeparateAuthorizationHost serves the authorization endpoint from a second
	// host, as Google does.
	SeparateAuthorizationHost bool
	// PendingPolls is how many device token polls answer authorization_pending
	// after the user approved.
	PendingPolls int
	// DenyAuthorization makes the authorization and device flows fail with access_denied.
	DenyAuthorization bool
	// TLS serves https with the certificate returned by [AuthServer.Certificate].
	TLS bool
}

type codeGrant struct {
	clientID, redirectURI, challenge, resource, scope string
}

type tokenInfo struct {
	clientID, resource, scope string
	expires                   time.Time
	revoked                   bool
}

type deviceGrant struct {
	clientID, resource, scope, userCode string
	approved, denied                    bool
	polls                               int
}

// AuthServer is an in-process OAuth 2.0 authorization server.
type AuthServer struct {
	// URL is the issuer identifier and the base URL of the endpoints, except
	// the authorization endpoint with [ServerOptions.SeparateAuthorizationHost].
	URL string

	opts         ServerOptions
	srv          *httptest.Server
	authorizeURL string
	mu           sync.Mutex

	codes   map[string]codeGrant
	access  map[string]*tokenInfo
	refresh map[string]*tokenInfo
	devices map[string]*deviceGrant

	hits      map[string]int
	forms     map[string]url.Values
	authSeen  bool
	revoked   []string
	authQuery url.Values
}

// NewAuthServer starts an authorization server that is stopped with the test.
func NewAuthServer(t testing.TB, opts ServerOptions) *AuthServer {
	t.Helper()
	if opts.TokenLifetime == 0 {
		opts.TokenLifetime = time.Hour
	}
	path := strings.TrimSuffix(opts.IssuerPath, "/")
	s := &AuthServer{
		opts:    opts,
		codes:   map[string]codeGrant{},
		access:  map[string]*tokenInfo{},
		refresh: map[string]*tokenInfo{},
		devices: map[string]*deviceGrant{},
		hits:    map[string]int{},
		forms:   map[string]url.Values{},
	}
	mux := http.NewServeMux()
	switch {
	case opts.AppendedMetadataOnly:
		mux.HandleFunc("GET "+path+wellKnownServerMetadata, s.metadata)
	case opts.OIDCOnly:
		mux.HandleFunc("GET "+path+wellKnownOpenIDConfig, s.metadata)
	default:
		mux.HandleFunc("GET "+wellKnownServerMetadata+path, s.metadata)
		mux.HandleFunc("GET "+path+wellKnownOpenIDConfig, s.metadata)
	}
	mux.HandleFunc("GET "+path+"/authorize", s.authorize)
	mux.HandleFunc("POST "+path+"/token", s.token)
	mux.HandleFunc("POST "+path+"/device", s.device)
	mux.HandleFunc("GET "+path+"/activate", s.activate)
	mux.HandleFunc("POST "+path+"/revoke", s.revoke)
	handler := s.record(mux)
	s.srv = startServer(t, handler, opts.TLS)
	s.URL = s.srv.URL + path
	s.authorizeURL = s.URL + "/authorize"
	if opts.SeparateAuthorizationHost {
		s.authorizeURL = startServer(t, handler, opts.TLS).URL + path + "/authorize"
	}
	return s
}

// startServer starts an httptest server, with TLS if requested, that is stopped
// with the test.
func startServer(t testing.TB, h http.Handler, useTLS bool) *httptest.Server {
	srv := httptest.NewUnstartedServer(h)
	if useTLS {
		srv.StartTLS()
	} else {
		srv.Start()
	}
	t.Cleanup(srv.Close)
	return srv
}

// Certificate returns the server's TLS certificate, or nil without
// [ServerOptions.TLS].
func (s *AuthServer) Certificate() *x509.Certificate { return s.srv.Certificate() }

func (s *AuthServer) record(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.hits[r.URL.Path]++
		if r.Header.Get("Authorization") != "" {
			s.authSeen = true
		}
		s.mu.Unlock()
		next.ServeHTTP(w, r)
	})
}

// MetadataURL returns the URL the server's metadata is served at: the RFC 8414
// location, or the only location the options leave.
func (s *AuthServer) MetadataURL() string {
	switch {
	case s.opts.AppendedMetadataOnly:
		return s.URL + wellKnownServerMetadata
	case s.opts.OIDCOnly:
		return s.OpenIDConfigurationURL()
	default:
		return s.srv.URL + wellKnownServerMetadata + strings.TrimSuffix(s.opts.IssuerPath, "/")
	}
}

// OpenIDConfigurationURL returns the server's OpenID Connect Discovery URL.
func (s *AuthServer) OpenIDConfigurationURL() string {
	return s.URL + wellKnownOpenIDConfig
}

// AuthorizationURL returns the URL of the authorization endpoint.
func (s *AuthServer) AuthorizationURL() string { return s.authorizeURL }

// Metadata returns the server's RFC 8414 metadata document.
func (s *AuthServer) Metadata() map[string]any {
	m := map[string]any{
		"issuer":                           s.URL,
		"token_endpoint":                   s.URL + "/token",
		"device_authorization_endpoint":    s.URL + "/device",
		"response_types_supported":         []string{"code"},
		"code_challenge_methods_supported": []string{"S256"},
		"grant_types_supported":            []string{"authorization_code", "refresh_token", "urn:ietf:params:oauth:grant-type:device_code"},
	}
	if !s.opts.NoAuthorizationEndpoint {
		m["authorization_endpoint"] = s.authorizeURL
	}
	if !s.opts.NoRevocation {
		m["revocation_endpoint"] = s.URL + "/revoke"
	}
	if s.opts.CIMD {
		m["client_id_metadata_document_supported"] = true
	}
	return m
}

func (s *AuthServer) metadata(w http.ResponseWriter, r *http.Request) {
	writeJSON(r.Context(), w, http.StatusOK, s.Metadata())
}

func (s *AuthServer) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	s.mu.Lock()
	s.authQuery = q
	s.mu.Unlock()
	redirect, err := url.Parse(q.Get("redirect_uri"))
	if err != nil || redirect.Scheme != "http" || !slices.Contains([]string{"127.0.0.1", "::1", "localhost"}, redirect.Hostname()) {
		http.Error(w, "invalid redirect_uri", http.StatusBadRequest)
		return
	}
	if q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" || q.Get("client_id") == "" {
		http.Error(w, "invalid authorization request", http.StatusBadRequest)
		return
	}
	back := redirect.Query()
	back.Set("state", q.Get("state"))
	if s.opts.DenyAuthorization {
		back.Set("error", "access_denied")
	} else {
		code := rand.Text()
		s.mu.Lock()
		s.codes[code] = codeGrant{
			clientID:    q.Get("client_id"),
			redirectURI: q.Get("redirect_uri"),
			challenge:   q.Get("code_challenge"),
			resource:    q.Get("resource"),
			scope:       q.Get("scope"),
		}
		s.mu.Unlock()
		back.Set("code", code)
	}
	redirect.RawQuery = back.Encode()
	http.Redirect(w, r, redirect.String(), http.StatusFound)
}

func (s *AuthServer) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		oauthError(r.Context(), w, "invalid_request")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.forms["/token"] = r.PostForm
	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		c, ok := s.codes[r.PostForm.Get("code")]
		delete(s.codes, r.PostForm.Get("code"))
		sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
		switch {
		case !ok:
			oauthError(r.Context(), w, "invalid_grant")
		case c.clientID != r.PostForm.Get("client_id") || c.redirectURI != r.PostForm.Get("redirect_uri"):
			oauthError(r.Context(), w, "invalid_grant")
		case base64.RawURLEncoding.EncodeToString(sum[:]) != c.challenge:
			oauthError(r.Context(), w, "invalid_grant")
		default:
			s.issue(r.Context(), w, c.clientID, firstNonEmpty(r.PostForm.Get("resource"), c.resource), c.scope)
		}
	case "refresh_token":
		info, ok := s.refresh[r.PostForm.Get("refresh_token")]
		if !ok || info.revoked || info.clientID != r.PostForm.Get("client_id") {
			oauthError(r.Context(), w, "invalid_grant")
			return
		}
		if s.opts.RotateRefreshTokens {
			info.revoked = true
			s.issue(r.Context(), w, info.clientID, info.resource, info.scope)
			return
		}
		access := rand.Text()
		s.access[access] = &tokenInfo{clientID: info.clientID, resource: info.resource, scope: info.scope, expires: time.Now().Add(s.opts.TokenLifetime)}
		writeJSON(r.Context(), w, http.StatusOK, map[string]any{"access_token": access, "token_type": "Bearer", "expires_in": int(s.opts.TokenLifetime / time.Second)})
	case "urn:ietf:params:oauth:grant-type:device_code":
		d, ok := s.devices[r.PostForm.Get("device_code")]
		switch {
		case !ok:
			oauthError(r.Context(), w, "invalid_grant")
		case d.denied:
			oauthError(r.Context(), w, "access_denied")
		case !d.approved || d.polls < s.opts.PendingPolls:
			if d.approved {
				d.polls++
			}
			oauthError(r.Context(), w, "authorization_pending")
		default:
			delete(s.devices, r.PostForm.Get("device_code"))
			s.issue(r.Context(), w, d.clientID, firstNonEmpty(r.PostForm.Get("resource"), d.resource), d.scope)
		}
	default:
		oauthError(r.Context(), w, "unsupported_grant_type")
	}
}

// issue writes a token response; s.mu must be held.
func (s *AuthServer) issue(ctx context.Context, w http.ResponseWriter, clientID, resource, scope string) {
	access, refresh := rand.Text(), rand.Text()
	s.access[access] = &tokenInfo{clientID: clientID, resource: resource, scope: scope, expires: time.Now().Add(s.opts.TokenLifetime)}
	s.refresh[refresh] = &tokenInfo{clientID: clientID, resource: resource, scope: scope}
	writeJSON(ctx, w, http.StatusOK, map[string]any{
		"access_token":  access,
		"token_type":    "Bearer",
		"expires_in":    int(s.opts.TokenLifetime / time.Second),
		"refresh_token": refresh,
		"scope":         scope,
	})
}

func (s *AuthServer) device(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil || r.PostForm.Get("client_id") == "" {
		oauthError(r.Context(), w, "invalid_request")
		return
	}
	dc, uc := rand.Text(), rand.Text()[:8]
	s.mu.Lock()
	s.forms["/device"] = r.PostForm
	s.devices[dc] = &deviceGrant{clientID: r.PostForm.Get("client_id"), resource: r.PostForm.Get("resource"), scope: r.PostForm.Get("scope"), userCode: uc}
	s.mu.Unlock()
	writeJSON(r.Context(), w, http.StatusOK, map[string]any{
		"device_code":               dc,
		"user_code":                 uc,
		"verification_uri":          s.URL + "/activate",
		"verification_uri_complete": s.URL + "/activate?user_code=" + uc,
		"expires_in":                600,
		"interval":                  1,
	})
}

// activate approves (or denies) the device grant with the given user_code.
func (s *AuthServer) activate(w http.ResponseWriter, r *http.Request) {
	uc := r.URL.Query().Get("user_code")
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.devices {
		if d.userCode == uc {
			d.approved = !s.opts.DenyAuthorization
			d.denied = s.opts.DenyAuthorization
			w.WriteHeader(http.StatusOK)
			return
		}
	}
	http.Error(w, "unknown user code", http.StatusNotFound)
}

func (s *AuthServer) revoke(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		oauthError(r.Context(), w, "invalid_request")
		return
	}
	tok := r.PostForm.Get("token")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.revoked = append(s.revoked, tok)
	if info, ok := s.refresh[tok]; ok {
		info.revoked = true
		for _, a := range s.access {
			if a.clientID == info.clientID && a.resource == info.resource {
				a.revoked = true
			}
		}
	}
	if info, ok := s.access[tok]; ok {
		info.revoked = true
	}
	w.WriteHeader(http.StatusOK)
}

// Valid reports whether token is a live access token issued for resource and
// covering every scope in scopes. Tokens issued without a resource indicator are
// not valid for any resource.
func (s *AuthServer) Valid(token, resource string, scopes []string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	info, ok := s.access[token]
	if !ok || info.revoked || time.Now().After(info.expires) {
		return false
	}
	if info.resource == "" || strings.TrimSuffix(info.resource, "/") != strings.TrimSuffix(resource, "/") {
		return false
	}
	granted := strings.Fields(info.scope)
	for _, sc := range scopes {
		if !slices.Contains(granted, sc) {
			return false
		}
	}
	return true
}

// Invalidate makes every issued access token invalid, as if they had been
// revoked server-side; refresh tokens keep working.
func (s *AuthServer) Invalidate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, info := range s.access {
		info.revoked = true
	}
}

// RevokeRefreshTokens makes every issued refresh token invalid.
func (s *AuthServer) RevokeRefreshTokens() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, info := range s.refresh {
		info.revoked = true
	}
}

// Hits returns how many requests reached path.
func (s *AuthServer) Hits(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits[path]
}

// SawAuthorizationHeader reports whether any request to the server carried an
// Authorization header. Agent credentials must never reach the server.
func (s *AuthServer) SawAuthorizationHeader() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.authSeen
}

// LastForm returns the form of the last request to /token or /device.
func (s *AuthServer) LastForm(path string) url.Values {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.forms[path]
}

// LastAuthorizeQuery returns the query of the last authorization request.
func (s *AuthServer) LastAuthorizeQuery() url.Values {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.authQuery
}

// Revoked returns the tokens sent to the revocation endpoint.
func (s *AuthServer) Revoked() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.revoked)
}

// Browser returns a function that plays the user's browser: given an
// authorization URL, it follows the authorization server's redirect back to the
// client's loopback redirect URI; given a device verification URL, it approves
// the device code.
func Browser(t testing.TB) func(string) {
	t.Helper()
	// The fixture servers use self-signed certificates; this client is for tests only.
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	return func(u string) {
		resp, err := client.Get(u)
		if err != nil {
			t.Errorf("browser: GET %s error = %v", u, err)
			return
		}
		if err := resp.Body.Close(); err != nil {
			t.Errorf("browser: closing the response of %s error = %v", u, err)
		}
	}
}

func oauthError(ctx context.Context, w http.ResponseWriter, code string) {
	writeJSON(ctx, w, http.StatusBadRequest, map[string]string{"error": code})
}

func writeJSON(ctx context.Context, w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Error(ctx, "failed to write JSON response", err)
	}
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
