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
	"fmt"
	"io"
	"net/http"

	"github.com/a2aproject/a2a-go/v2/log"
)

// Transport returns an [http.RoundTripper] that attaches grants and handles 401
// responses to requests made through base, which defaults to
// [http.DefaultTransport]. It attaches the grant obtained in this process for
// the longest resource that covers a request without an Authorization header.
// On a 401 it refreshes a token the manager attached that the agent rejected,
// or follows the Bearer challenge (RFC 9728) of a request without credentials
// to a stored grant, and retries the request once. Otherwise, including when
// the request body can't be replayed, it returns the 401 response: the
// interceptor turns it into an [*InteractionRequiredError] for A2A calls, and
// [Manager.RequirementForError] does for other requests. Requests carrying an
// Authorization header the manager did not attach are passed through
// untouched, and so are 401 responses that report insufficient_scope.
//
// Redirects never gain credentials: on the requests [http.Client] makes to
// follow a redirect, the transport attaches no grant and does not act on a 401,
// so an agent can't redirect a request to another agent the user has a grant
// for. A request carrying a token the manager attached fails before it is sent
// when it goes to neither https nor a loopback host, as after a redirect from
// https to http on the same host, on which [http.Client] keeps the
// Authorization header.
//
// It returns an error instead of a response only when it fails before sending
// a request, or when sending the request or its retry fails.
//
// Use it as the transport of the [http.Client] given to the A2A client's HTTP
// transports, together with [Manager.Interceptor]. It covers agents whose card
// does not mention OAuth, and tokens that expire or are revoked between calls.
func (m *Manager) Transport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &transport{base: base, m: m}
}

type transport struct {
	base http.RoundTripper
	m    *Manager
}

// RoundTrip implements [http.RoundTripper]. It records the response it returns
// for the call's interceptor.
func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.roundTrip(req)
	switch cs := callStateFrom(req.Context()); {
	case cs != nil:
		cs.recordResponse(req, resp)
	case err == nil && resp.StatusCode == http.StatusUnauthorized:
		// Not an A2A call, such as an agent card fetch: keep the challenge for
		// RequirementForError.
		final := req
		if resp.Request != nil {
			final = resp.Request
		}
		t.m.recordChallenge(final.URL, resp.Header.Values("WWW-Authenticate"))
	}
	return resp, err
}

func (t *transport) roundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	if g, ok := attachedFrom(ctx); ok && req.Header.Get("Authorization") == g.header() {
		// http.Client copies the header to redirects within the host and its
		// subdomains, on any port and path, and the URL of a REST call is built
		// after the interceptor chose the grant.
		if err := checkHTTPTokenDestination(req.URL.Redacted()); err != nil {
			closeRequestBody(req)
			return nil, failure("%w", err)
		}
		if !resourceCovers(g.Key.Resource, req.URL.String()) {
			closeRequestBody(req)
			return nil, failure("refusing to send the OAuth token for %s to %s", g.Key.Resource, req.URL.Redacted())
		}
	}
	// http.Client sets Response on the requests that follow a redirect.
	firstHop := req.Response == nil
	if firstHop && req.Header.Get("Authorization") == "" && checkSecureURL(req.URL.String()) == nil {
		g, err := t.m.learnedGrant(ctx, req.URL)
		if err != nil {
			closeRequestBody(req)
			return nil, err
		}
		if g != nil {
			req = withToken(req, g)
		}
	}

	resp, err := t.base.RoundTrip(req)
	if err != nil || resp.StatusCode != http.StatusUnauthorized || !firstHop || !replayable(req) || checkSecureURL(req.URL.String()) != nil {
		return resp, err
	}
	g := t.retryGrant(req, ParseChallenges(resp.Header.Values("WWW-Authenticate")))
	if g == nil {
		return resp, nil
	}
	return t.retry(req, resp, g)
}

// retryGrant returns the grant to repeat a request that got a 401 with, or nil
// when there is none.
func (t *transport) retryGrant(req *http.Request, chs []Challenge) *Grant {
	ctx := req.Context()
	if auth := req.Header.Get("Authorization"); auth != "" {
		used, ok := attachedFrom(ctx)
		if !ok || used.header() != auth {
			return nil
		}
		if bc, ok := bearerChallenge(chs); ok && bc.Params["error"] == "insufficient_scope" {
			return nil
		}
		log.Debug(ctx, "the agent rejected the OAuth access token; refreshing", "resource", used.Key.Resource)
		g, err := t.m.refreshRejected(ctx, used.Key, used.AccessToken)
		if err != nil {
			log.Info(ctx, "not retrying a request whose OAuth access token the agent rejected", "url", req.URL.Redacted(), "error", err)
			return nil
		}
		return g
	}
	if !wantsBearer(chs) {
		return nil
	}
	r, err := t.m.RequirementForChallenge(ctx, req.URL, chs)
	if cs := callStateFrom(ctx); cs != nil {
		cs.recordDiscovery(r, err)
	}
	if err != nil {
		log.Debug(ctx, "not using OAuth for a 401 response", "url", req.URL.Redacted(), "error", err)
		return nil
	}
	if r == nil {
		return nil
	}
	g, err := t.m.Grant(ctx, r)
	if err != nil {
		log.Info(ctx, "not using a stored OAuth grant for a 401 response", "url", req.URL.Redacted(), "error", err)
		return nil
	}
	if g != nil {
		t.m.learn(g.Key)
	}
	return g
}

// replayable reports whether req can be sent again: it has no body, or a
// GetBody to get it anew. A request that can't be retried gets no refresh or
// discovery either, so no refresh token is spent for nothing.
func replayable(req *http.Request) bool {
	return req.Body == nil || req.Body == http.NoBody || req.GetBody != nil
}

// retry repeats req once with the grant's token, or returns resp when the
// request body can't be obtained again.
func (t *transport) retry(req *http.Request, resp *http.Response, g *Grant) (*http.Response, error) {
	ctx := req.Context()
	clone := withToken(req, g)
	if req.Body != nil && req.Body != http.NoBody {
		body, err := req.GetBody()
		if err != nil {
			log.Info(ctx, "not retrying a request whose body can't be replayed", "url", req.URL.Redacted(), "error", err)
			return resp, nil
		}
		clone.Body = body
	}
	drain(ctx, resp)
	return t.base.RoundTrip(clone)
}

// wantsBearer reports whether a 401 may be OAuth-protected: it offers a Bearer
// challenge, or no challenge at all.
func wantsBearer(chs []Challenge) bool {
	if len(chs) == 0 {
		return true
	}
	_, ok := bearerChallenge(chs)
	return ok
}

// checkHTTPTokenDestination refuses to send a bearer token over HTTP where it
// would travel in cleartext: it allows https and http to a loopback host.
func checkHTTPTokenDestination(target string) error {
	if err := checkSecureURL(target); err != nil {
		return fmt.Errorf("refusing to send an OAuth token to %s over plain HTTP", target)
	}
	return nil
}

func withToken(req *http.Request, g *Grant) *http.Request {
	clone := req.Clone(withAttached(req.Context(), g))
	clone.Header.Set("Authorization", g.header())
	return clone
}

func closeRequestBody(req *http.Request) {
	if req.Body == nil {
		return
	}
	if err := req.Body.Close(); err != nil {
		log.Error(req.Context(), "failed to close http request body", err)
	}
}

// drain reads what is left of a response that is not returned, up to a limit,
// so the connection can be reused, and closes it.
func drain(ctx context.Context, resp *http.Response) {
	if _, err := io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10)); err != nil {
		log.Debug(ctx, "failed to read the rest of an http response body", "error", err)
	}
	if err := resp.Body.Close(); err != nil {
		log.Error(ctx, "failed to close http response body", err)
	}
}
