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
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultDeviceInterval = 5 * time.Second
	slowDownIncrement     = 5 * time.Second
	// maxTransientPollFailures bounds the polls in a row that fail in a way that
	// may pass, such as a 503 from a gateway, before device polling gives up.
	maxTransientPollFailures = 3
	defaultHTTPTimeout       = 30 * time.Second
)

// defaultHTTPClient makes requests to authorization servers when no HTTP client
// is given.
var defaultHTTPClient = &http.Client{Timeout: defaultHTTPTimeout}

// tokenClient obtains, refreshes and revokes grants at authorization servers. It
// implements the protocol steps only: showing URLs and codes to the user and
// receiving the authorization code at the redirect URI are up to the caller.
type tokenClient struct {
	// httpClient makes token, device authorization and revocation requests.
	// Defaults to defaultHTTPClient. It must not add agent credentials. Its
	// CheckRedirect is not used: redirects are never followed.
	httpClient *http.Client

	// timeout replaces defaultHTTPTimeout when httpClient is nil.
	timeout time.Duration
	now     func() time.Time
	sleep   func(ctx context.Context, d time.Duration) error
}

func (c tokenClient) fetcher() fetcher {
	switch {
	case c.httpClient != nil:
		return fetcher{client: c.httpClient}
	case c.timeout > 0:
		return fetcher{client: &http.Client{Timeout: c.timeout}}
	default:
		return fetcher{client: defaultHTTPClient}
	}
}

func (c tokenClient) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

func (c tokenClient) wait(ctx context.Context, d time.Duration) error {
	if c.sleep != nil {
		return c.sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (c tokenClient) newGrant(e Endpoints, key GrantKey, omitResource bool, t *tokenResponse) *Grant {
	now := c.clock()
	g := &Grant{Key: key, Endpoints: e, CreatedAt: now.UTC(), OmitResource: omitResource}
	g.apply(t, now)
	return g
}

// AuthorizationRequest is an authorization code request with PKCE (RFC 7636).
// The fields are exported so a caller can keep the request until the redirect
// comes back, but State and Verifier must stay secret.
type AuthorizationRequest struct {
	// URL is the authorization URL to send the user to.
	URL string
	// RedirectURI is where the authorization server sends the user back to.
	RedirectURI string
	// State binds the redirect to this request.
	State string
	// Verifier is the PKCE code verifier sent with the code exchange.
	Verifier string
	// Endpoints are the endpoints of the authorization server.
	Endpoints Endpoints
	// Key identifies the grant being requested.
	Key GrantKey
	// OmitResource reports that the request leaves out the RFC 8707 resource
	// indicator; the code exchange and the grant's refreshes leave it out too.
	OmitResource bool
}

// newAuthorizationRequest builds an authorization code request with PKCE for the
// client, resource and scopes in key, without the resource indicator when
// omitResource is set. Native apps should use a loopback redirectURI with a port
// of their choosing (RFC 8252 §7.3). It returns [ErrNoAuthorizationEndpoint]
// when the server only offers the device authorization grant.
func newAuthorizationRequest(e Endpoints, key GrantKey, redirectURI string, omitResource bool) (*AuthorizationRequest, error) {
	if err := checkKeyIssuer(e, key); err != nil {
		return nil, err
	}
	if e.Authorization == "" {
		return nil, fmt.Errorf("%w: %s", ErrNoAuthorizationEndpoint, e.Issuer)
	}
	if err := checkSecureURL(e.Authorization); err != nil {
		return nil, err
	}
	authURL, err := url.Parse(e.Authorization)
	if err != nil {
		return nil, fmt.Errorf("invalid authorization endpoint: %w", err)
	}
	if redirectURI == "" {
		return nil, errors.New("no redirect URI")
	}
	ar := &AuthorizationRequest{
		RedirectURI:  redirectURI,
		State:        rand.Text(),
		Verifier:     newVerifier(),
		Endpoints:    e,
		Key:          key,
		OmitResource: omitResource,
	}
	q := authURL.Query()
	q.Set("response_type", "code")
	q.Set("client_id", key.ClientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("state", ar.State)
	q.Set("code_challenge", pkceChallenge(ar.Verifier))
	q.Set("code_challenge_method", "S256")
	if len(key.Scopes) > 0 {
		q.Set("scope", strings.Join(key.Scopes, " "))
	}
	if r := resourceIndicator(key, omitResource); r != "" {
		q.Set("resource", r)
	}
	authURL.RawQuery = q.Encode()
	ar.URL = authURL.String()
	return ar, nil
}

// ParseRedirect checks the query of the request that came back to the redirect
// URI and returns its authorization code. It fails when the state does not match,
// or when the response names another issuer than ar.Endpoints.Issuer or names
// none although the server promised to (RFC 9207). It returns an [*Error] when
// the authorization server reported one, such as access_denied.
func (ar *AuthorizationRequest) ParseRedirect(query url.Values) (string, error) {
	if ar.State == "" {
		return "", errors.New("authorization request has no state")
	}
	if subtle.ConstantTimeCompare([]byte(query.Get("state")), []byte(ar.State)) != 1 {
		return "", errors.New("authorization response has an unexpected state")
	}
	iss, hasIss := query["iss"]
	switch {
	case hasIss && (len(iss) != 1 || iss[0] != ar.Endpoints.Issuer):
		return "", fmt.Errorf("authorization response is from issuer %q, want %q", strings.Join(iss, ", "), ar.Endpoints.Issuer)
	case !hasIss && ar.Endpoints.AuthorizationResponseIss:
		return "", fmt.Errorf("authorization response from %s has no iss parameter", ar.Endpoints.Issuer)
	}
	if code := query.Get("error"); code != "" {
		return "", &Error{Code: code, Description: query.Get("error_description")}
	}
	code := query.Get("code")
	if code == "" {
		return "", errors.New("authorization response has no code")
	}
	return code, nil
}

// ExchangeCode exchanges an authorization code returned for ar for a grant.
func (c tokenClient) ExchangeCode(ctx context.Context, ar *AuthorizationRequest, code string) (*Grant, error) {
	form := tokenForm(grantTypeAuthorizationCode, ar.Key.ClientID, resourceIndicator(ar.Key, ar.OmitResource))
	form.Set("code", code)
	form.Set("redirect_uri", ar.RedirectURI)
	form.Set("code_verifier", ar.Verifier)
	t, err := c.fetcher().token(ctx, ar.Endpoints.Token, form)
	if err != nil {
		return nil, err
	}
	return c.newGrant(ar.Endpoints, ar.Key, ar.OmitResource, t), nil
}

// DeviceAuthorization is a pending device authorization grant (RFC 8628). The
// user visits VerificationURI and enters UserCode, or visits
// VerificationURIComplete, while the client polls for the grant.
type DeviceAuthorization struct {
	// UserCode is the code the user enters at VerificationURI.
	UserCode string
	// VerificationURI is where the user enters UserCode.
	VerificationURI string
	// VerificationURIComplete, if set, includes the user code.
	VerificationURIComplete string
	// ExpiresAt is when the device code expires; zero when the server named no
	// lifetime.
	ExpiresAt time.Time
	// Interval is the time to wait between polls.
	Interval time.Duration
	// Endpoints are the endpoints of the authorization server.
	Endpoints Endpoints
	// Key identifies the grant being requested.
	Key GrantKey
	// OmitResource reports that the request left out the RFC 8707 resource
	// indicator; the polls and the grant's refreshes leave it out too.
	OmitResource bool

	deviceCode string
}

type deviceAuthorizationResponse struct {
	DeviceCode              string  `json:"device_code"`
	UserCode                string  `json:"user_code"`
	VerificationURI         string  `json:"verification_uri"`
	VerificationURL         string  `json:"verification_url"`
	VerificationURIComplete string  `json:"verification_uri_complete"`
	ExpiresIn               flexInt `json:"expires_in"`
	Interval                flexInt `json:"interval"`
}

// StartDeviceAuthorization starts a device authorization grant for the client,
// resource and scopes in key, without the resource indicator when omitResource
// is set. It returns [ErrNoDeviceAuthorizationEndpoint] when the server does not
// support it.
func (c tokenClient) StartDeviceAuthorization(ctx context.Context, e Endpoints, key GrantKey, omitResource bool) (*DeviceAuthorization, error) {
	if err := checkKeyIssuer(e, key); err != nil {
		return nil, err
	}
	if e.DeviceAuthorization == "" {
		return nil, fmt.Errorf("%w: %s", ErrNoDeviceAuthorizationEndpoint, e.Issuer)
	}
	form := url.Values{"client_id": {key.ClientID}}
	if len(key.Scopes) > 0 {
		form.Set("scope", strings.Join(key.Scopes, " "))
	}
	if r := resourceIndicator(key, omitResource); r != "" {
		form.Set("resource", r)
	}
	var resp deviceAuthorizationResponse
	if err := c.fetcher().postForm(ctx, e.DeviceAuthorization, form, &resp); err != nil {
		return nil, fmt.Errorf("starting the device authorization: %w", err)
	}
	verification := resp.VerificationURI
	if verification == "" {
		// Some servers use the draft name.
		verification = resp.VerificationURL
	}
	if resp.DeviceCode == "" || resp.UserCode == "" || verification == "" {
		return nil, errors.New("device authorization response is missing device_code, user_code or verification_uri")
	}
	// The application shows these to the user, who opens them in a browser.
	for _, u := range []string{verification, resp.VerificationURIComplete} {
		if u == "" {
			continue
		}
		if err := checkSecureURL(u); err != nil {
			return nil, fmt.Errorf("device authorization response has an unusable verification URI: %w", err)
		}
	}
	da := &DeviceAuthorization{
		UserCode:                resp.UserCode,
		VerificationURI:         verification,
		VerificationURIComplete: resp.VerificationURIComplete,
		Interval:                defaultDeviceInterval,
		Endpoints:               e,
		Key:                     key,
		OmitResource:            omitResource,
		deviceCode:              resp.DeviceCode,
	}
	if resp.Interval > 0 {
		da.Interval = resp.Interval.seconds()
	}
	if resp.ExpiresIn > 0 {
		da.ExpiresAt = c.clock().Add(resp.ExpiresIn.seconds()).UTC()
	}
	return da, nil
}

// PollDeviceAuthorization polls the token endpoint every da.Interval until the
// user approves or denies the request, the device code expires or ctx is done.
// It slows down when the server asks to (RFC 8628 §3.5). A denial is returned as
// an [*Error] with the code access_denied.
func (c tokenClient) PollDeviceAuthorization(ctx context.Context, da *DeviceAuthorization) (*Grant, error) {
	pollCtx := ctx
	if !da.ExpiresAt.IsZero() {
		var cancel context.CancelFunc
		pollCtx, cancel = context.WithDeadline(ctx, da.ExpiresAt)
		defer cancel()
	}
	interval := da.Interval
	failures := 0
	for {
		if err := c.wait(pollCtx, interval); err != nil {
			if ctx.Err() == nil {
				return nil, fmt.Errorf("the device code expired before the request was approved: %w", err)
			}
			return nil, fmt.Errorf("waiting for the device authorization: %w", err)
		}
		form := tokenForm(grantTypeDeviceCode, da.Key.ClientID, resourceIndicator(da.Key, da.OmitResource))
		form.Set("device_code", da.deviceCode)
		t, err := c.fetcher().token(pollCtx, da.Endpoints.Token, form)
		switch {
		case err == nil:
			return c.newGrant(da.Endpoints, da.Key, da.OmitResource, t), nil
		case pollCtx.Err() != nil && ctx.Err() == nil:
			return nil, fmt.Errorf("the device code expired before the request was approved: %w", pollCtx.Err())
		case isOAuthError(err, "authorization_pending"):
		case isOAuthError(err, "slow_down"):
			interval += slowDownIncrement
		case ctx.Err() == nil && failures < maxTransientPollFailures && transientPollError(err):
			// RFC 8628 §3.5: back off when the server can't be reached.
			failures++
			interval += slowDownIncrement
			continue
		default:
			return nil, err
		}
		failures = 0
	}
}

// transientPollError reports whether a failed poll may pass when repeated: the
// token endpoint answered temporarily_unavailable, 429 or a 5xx status, or could
// not be reached.
func transientPollError(err error) bool {
	if isOAuthError(err, "temporarily_unavailable") {
		return true
	}
	var se *statusError
	if errors.As(err, &se) {
		return se.status == http.StatusTooManyRequests || se.status >= http.StatusInternalServerError
	}
	var ne net.Error
	return errors.As(err, &ne)
}

// Refresh exchanges the grant's refresh token for a new access token and returns
// the updated grant; g is not modified. It sends no scope, so the server keeps
// the scopes it granted (RFC 6749 §6), including ones it did not list in its
// response. The server may rotate the refresh token. A refresh token the server
// no longer accepts is returned as an [*Error] with the code invalid_grant.
func (c tokenClient) Refresh(ctx context.Context, g *Grant) (*Grant, error) {
	if g.RefreshToken == "" {
		return nil, errors.New("the grant has no refresh token")
	}
	form := tokenForm(grantTypeRefreshToken, g.Key.ClientID, resourceIndicator(g.Key, g.OmitResource))
	form.Set("refresh_token", g.RefreshToken)
	t, err := c.fetcher().token(ctx, g.Endpoints.Token, form)
	if err != nil {
		return nil, err
	}
	updated := g.clone()
	updated.apply(t, c.clock())
	return updated, nil
}

// Revoke revokes the grant's refresh token, or its access token when it has none
// (RFC 7009). It returns [ErrNoRevocationEndpoint] when the server does not
// support revocation.
func (c tokenClient) Revoke(ctx context.Context, g *Grant) error {
	if g.Endpoints.Revocation == "" {
		return fmt.Errorf("%w: %s", ErrNoRevocationEndpoint, g.Endpoints.Issuer)
	}
	token, hint := g.RefreshToken, "refresh_token"
	if token == "" {
		token, hint = g.AccessToken, "access_token"
	}
	if token == "" {
		return errors.New("the grant has no token to revoke")
	}
	form := url.Values{"token": {token}, "token_type_hint": {hint}, "client_id": {g.Key.ClientID}}
	return c.fetcher().postForm(ctx, g.Endpoints.Revocation, form, nil)
}

// checkKeyIssuer checks that key is for the authorization server of e, unless e
// has no issuer yet.
func checkKeyIssuer(e Endpoints, key GrantKey) error {
	if e.Issuer != "" && !sameIssuer(e.Issuer, key.Issuer) {
		return fmt.Errorf("grant key for issuer %q used with authorization server %s", key.Issuer, e.Issuer)
	}
	return nil
}

// resourceIndicator returns the RFC 8707 resource to send for key: none when omit is set.
func resourceIndicator(key GrantKey, omit bool) string {
	if omit {
		return ""
	}
	return key.Resource
}

// newVerifier returns a PKCE code verifier of 52 unreserved characters with
// 256 bits of entropy (RFC 7636 §4.1).
func newVerifier() string {
	return rand.Text() + rand.Text()
}

// pkceChallenge returns the S256 challenge for a verifier (RFC 7636 §4.2).
func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
