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
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/a2aproject/a2a-go/v2/log"
)

// defaultRedirectPath is the path of the loopback redirect URI when
// SignInOptions.RedirectPath is empty.
const defaultRedirectPath = "/callback"

// SignInOptions control how [Manager.SignIn] asks the user to authorize. Set
// ShowURL, ShowCode or both: they choose the flows SignIn may use.
type SignInOptions struct {
	// Scopes replace the scopes of the requirement and of the client
	// registration.
	Scopes []string
	// ShowURL enables the authorization code flow with PKCE (RFC 7636) and a
	// loopback redirect (RFC 8252). It is called with the authorization URL the
	// user has to open in a browser on this machine, for example by printing it
	// or opening a browser.
	ShowURL func(authURL string)
	// ShowCode enables the device authorization grant (RFC 8628), for machines
	// without a browser. It is called with the code the user enters on another
	// device and where to enter it.
	ShowCode func(DeviceCode)
	// RedirectPath is the path of the loopback redirect URI, which is
	// http://127.0.0.1:<port><RedirectPath> on a port of the system's choosing.
	// Defaults to /callback. A client identified by a Client ID Metadata Document
	// must use a path the document lists.
	RedirectPath string
}

// DeviceCode is what the user needs to approve a device authorization grant.
type DeviceCode struct {
	// UserCode is the code the user enters at VerificationURI.
	UserCode string
	// VerificationURI is where the user enters UserCode.
	VerificationURI string
	// VerificationURIComplete, if the server gave one, includes the user code,
	// so the user only has to open it.
	VerificationURIComplete string
	// ExpiresAt is when the code expires; zero when the server named no
	// lifetime.
	ExpiresAt time.Time
}

// SignIn obtains a grant for req interactively and stores it. It uses the device
// authorization grant when only ShowCode is set, or when both are set and the
// card prefers the device flow or the server has no authorization endpoint; it
// uses the authorization code flow otherwise. With the authorization code flow
// it listens on a loopback address for the redirect until ctx is done. It
// returns [ErrNoAuthorizationEndpoint] or [ErrNoDeviceAuthorizationEndpoint]
// when the server does not offer the flow opts allow, an [*Error] when the
// authorization server reports one, such as access_denied, and the errors of
// [Manager] otherwise.
func (m *Manager) SignIn(ctx context.Context, req *Requirement, opts SignInOptions) (*Grant, error) {
	switch {
	case req == nil:
		return nil, errors.New("SignIn needs a Requirement")
	case opts.ShowURL == nil && opts.ShowCode == nil:
		return nil, errors.New("SignInOptions sets neither ShowURL nor ShowCode")
	case opts.ShowCode != nil && (opts.ShowURL == nil || req.PreferDevice || req.Endpoints.Authorization == ""):
		return m.signInWithDevice(ctx, req, opts)
	default:
		return m.signInWithBrowser(ctx, req, opts)
	}
}

type redirectResult struct {
	code string
	err  error
}

// signInWithBrowser runs the authorization code flow with a loopback redirect.
func (m *Manager) signInWithBrowser(ctx context.Context, req *Requirement, opts SignInOptions) (*Grant, error) {
	path := opts.RedirectPath
	if path == "" {
		path = defaultRedirectPath
	}
	if err := checkRedirectPath(path); err != nil {
		return nil, err
	}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("starting the loopback listener: %w", err)
	}
	p, err := m.BeginSignIn(ctx, req, fmt.Sprintf("http://%s%s", lis.Addr(), path), opts.Scopes)
	if err != nil {
		_ = lis.Close()
		return nil, err
	}

	results := make(chan redirectResult, 1)
	var once sync.Once
	deliver := func(r redirectResult) { once.Do(func() { results <- r }) }
	mux := http.NewServeMux()
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		code, err := p.ar.parseRedirect(r.URL.Query())
		var oe *Error
		switch {
		case errors.As(err, &oe):
			deliver(redirectResult{err: err})
			http.Error(w, "Authorization was not granted. You can close this tab.", http.StatusForbidden)
		case err != nil:
			// Not the redirect for this request; keep waiting for it.
			http.Error(w, fmt.Sprintf("Authorization failed: %v. You can close this tab.", err), http.StatusBadRequest)
		default:
			deliver(redirectResult{code: code})
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = io.WriteString(w, "Authorized. You can close this tab.\n")
		}
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(lis) }()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	opts.ShowURL(p.URL)
	var res redirectResult
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("waiting for authorization: %w", ctx.Err())
	case res = <-results:
	}
	if res.err != nil {
		return nil, res.err
	}
	return m.exchange(ctx, p.ar, res.code)
}

// signInWithDevice runs the device authorization grant.
func (m *Manager) signInWithDevice(ctx context.Context, req *Requirement, opts SignInOptions) (*Grant, error) {
	key, choice, err := m.key(req, opts.Scopes)
	if err != nil {
		return nil, err
	}
	log.Debug(ctx, "starting the device authorization grant", "resource", req.Resource, "issuer", req.Endpoints.Issuer, "client_id", key.ClientID)
	da, err := m.tokens().StartDeviceAuthorization(ctx, req.Endpoints, key, choice.OmitResource)
	if err != nil {
		return nil, err
	}
	opts.ShowCode(DeviceCode{
		UserCode:                da.UserCode,
		VerificationURI:         da.VerificationURI,
		VerificationURIComplete: da.VerificationURIComplete,
		ExpiresAt:               da.ExpiresAt,
	})
	g, err := m.tokens().PollDeviceAuthorization(ctx, da)
	if err != nil {
		return nil, err
	}
	if err := m.save(ctx, g); err != nil {
		return nil, err
	}
	return g, nil
}

// PendingSignIn is an authorization code request started by
// [Manager.BeginSignIn], for applications that receive the redirect
// themselves. Keep it, in the process that started it, until the redirect comes
// back, and pass it to [Manager.CompleteSignIn].
type PendingSignIn struct {
	// URL is the authorization URL to send the user to.
	URL string
	// State is the state parameter the redirect carries back, to find the
	// pending sign-in it answers.
	State string

	ar *authorizationRequest
}

// BeginSignIn starts the authorization code flow with PKCE for req, with the
// redirect URI the application receives the user at, and returns the pending
// sign-in to complete with [Manager.CompleteSignIn]. Scopes replace those of the
// requirement and the client registration. Native applications should use a
// loopback redirect URI with a port of their choosing (RFC 8252); [Manager.SignIn]
// does that for them. It returns [ErrNoAuthorizationEndpoint] when the server
// only offers the device authorization grant.
func (m *Manager) BeginSignIn(ctx context.Context, req *Requirement, redirectURI string, scopes []string) (*PendingSignIn, error) {
	if req == nil {
		return nil, errors.New("BeginSignIn needs a Requirement")
	}
	key, choice, err := m.key(req, scopes)
	if err != nil {
		return nil, err
	}
	log.Debug(ctx, "starting the authorization code flow", "resource", req.Resource, "issuer", req.Endpoints.Issuer, "client_id", key.ClientID)
	ar, err := newAuthorizationRequest(req.Endpoints, key, redirectURI, choice.OmitResource)
	if err != nil {
		return nil, err
	}
	return &PendingSignIn{URL: ar.URL, State: ar.State, ar: ar}, nil
}

// CompleteSignIn checks the query of the request that came back to the redirect
// URI of p, exchanges its authorization code for a grant and stores it. It fails
// when the state does not match, or when the response names another issuer than
// the server's or names none although the server promised to (RFC 9207). It
// returns an [*Error] when the authorization server reported one, such as
// access_denied.
func (m *Manager) CompleteSignIn(ctx context.Context, p *PendingSignIn, redirect url.Values) (*Grant, error) {
	if p == nil || p.ar == nil {
		return nil, errors.New("CompleteSignIn needs a PendingSignIn from BeginSignIn")
	}
	code, err := p.ar.parseRedirect(redirect)
	if err != nil {
		return nil, err
	}
	return m.exchange(ctx, p.ar, code)
}

// checkRedirectPath checks that path is an absolute URL path the loopback
// listener can serve: no query, fragment, wildcard or space.
func checkRedirectPath(path string) error {
	if !strings.HasPrefix(path, "/") {
		return fmt.Errorf("redirect path %q does not start with a slash", path)
	}
	if strings.ContainsAny(path, "?#{} \t") || (&url.URL{Path: path}).EscapedPath() != path {
		return fmt.Errorf("redirect path %q is not a plain URL path", path)
	}
	return nil
}

// exchange exchanges an authorization code for a grant and stores it.
func (m *Manager) exchange(ctx context.Context, ar *authorizationRequest, code string) (*Grant, error) {
	g, err := m.tokens().ExchangeCode(ctx, ar, code)
	if err != nil {
		return nil, err
	}
	if err := m.save(ctx, g); err != nil {
		return nil, err
	}
	return g, nil
}
