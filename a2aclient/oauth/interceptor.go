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
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/log"
)

type attachedGrantKey struct{}

// withAttached records on ctx the grant whose token a request carries, so the
// transport can tell its own token from an explicit credential.
func withAttached(ctx context.Context, g *Grant) context.Context {
	return context.WithValue(ctx, attachedGrantKey{}, g)
}

func attachedFrom(ctx context.Context) (*Grant, bool) {
	g, ok := ctx.Value(attachedGrantKey{}).(*Grant)
	return g, ok
}

type callStateKey struct{}

// callState is shared by the interceptor and the transport during one call:
// the transport records the response it returned last and the result of
// following a 401's challenge, so After knows the call got a 401 and follows
// the challenge only once. Before puts it on the call's context, which the
// transport gets with the request.
type callState struct {
	mu        sync.Mutex
	last      *callResponse
	discovery *resolved[*Requirement]
}

// callResponse is what the transport returned for a request of a call.
type callResponse struct {
	status int
	url    *url.URL
	header http.Header
	// sent is the grant whose token the request carried, if the Manager
	// attached it, before or in the transport.
	sent *Grant
	// redirected is set for a request http.Client made to follow a redirect.
	redirected bool
}

// recordResponse keeps what the transport returned for req, replacing the
// record of an earlier hop, so a retry's answer wins. A nil resp, for a request
// that failed, clears the record. Headers are kept for a 401 only.
func (cs *callState) recordResponse(req *http.Request, resp *http.Response) {
	var r *callResponse
	if resp != nil {
		r = &callResponse{status: resp.StatusCode, url: req.URL, redirected: req.Response != nil}
		if resp.Request != nil {
			r.url = resp.Request.URL
			req = resp.Request
		}
		if g, ok := attachedFrom(req.Context()); ok && req.Header.Get("Authorization") == g.header() {
			r.sent = g
		}
		if resp.StatusCode == http.StatusUnauthorized {
			r.header = resp.Header.Clone()
		}
	}
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.last = r
}

// lastResponse returns the response the transport returned last, or nil.
func (cs *callState) lastResponse() *callResponse {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return cs.last
}

func withCallState(ctx context.Context) context.Context {
	return context.WithValue(ctx, callStateKey{}, &callState{})
}

func callStateFrom(ctx context.Context) *callState {
	cs, _ := ctx.Value(callStateKey{}).(*callState)
	return cs
}

// recordDiscovery keeps the result of following a 401's challenge.
func (cs *callState) recordDiscovery(r *Requirement, err error) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.discovery = &resolved[*Requirement]{value: r, err: err}
}

// discovered returns the recorded result of following a challenge, or nil.
func (cs *callState) discovered() *resolved[*Requirement] {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return cs.discovery
}

type callerAuthorizationKey struct{}

// withCallerAuthorization records on ctx that a call carried an Authorization
// service parameter the interceptor did not set.
func withCallerAuthorization(ctx context.Context) context.Context {
	return context.WithValue(ctx, callerAuthorizationKey{}, true)
}

func hasCallerAuthorization(ctx context.Context) bool {
	v, _ := ctx.Value(callerAuthorizationKey{}).(bool)
	return v
}

// Interceptor returns an [a2aclient.CallInterceptor] that attaches grants to
// calls as an Authorization service parameter and reports the calls the agent
// refused for want of one.
//
// Before a call: when the agent card requires OAuth for the call's interface,
// the call fails with [*InteractionRequiredError] if no grant is stored, with
// [*NoClientError] or [*AmbiguousClientError] if no single client registration
// can be used, and with [*FailureError] if discovery, the store or a refresh
// fails. Otherwise a stored grant is attached when there is one, and problems
// finding it are only logged, so agents that accept other credentials or none
// are not affected. Calls that already carry an Authorization service
// parameter, such as one set by an earlier [a2aclient.AuthInterceptor], are left
// alone. Over HTTP, tokens are only sent over https or to loopback hosts. gRPC
// addresses and calls over the gRPC binding get the token whatever the address,
// since the gRPC connection's credentials decide its transport security, which
// is up to the application.
//
// After a call that failed because the agent answered 401, which it learns from
// [Manager.Transport], so it needs the transport installed in the JSON-RPC and
// REST clients: if the call was sent with the grant the interceptor attached,
// the error becomes an [*InteractionRequiredError] for that grant; otherwise,
// if following the 401's Bearer challenge finds a requirement, it becomes an
// [*InteractionRequiredError] for it, and if that fails with a [*NoClientError]
// or an [*AmbiguousClientError], it becomes that error. When the transport
// already followed the challenge, its result is used instead of discovering
// again. In each case the new error wraps the original one, so errors.Is and
// errors.As still find it. A call that carried a caller-supplied
// Authorization keeps its original error, since signing in would not change the
// credential it sends; so does a call whose grant a later interceptor, such as
// an [a2aclient.AuthInterceptor], replaced, and so do network and metadata
// failures during discovery.
func (m *Manager) Interceptor() a2aclient.CallInterceptor {
	return &interceptor{m: m}
}

type interceptor struct {
	m *Manager
}

var _ a2aclient.CallInterceptor = (*interceptor)(nil)

// Before implements [a2aclient.CallInterceptor].
func (i *interceptor) Before(ctx context.Context, req *a2aclient.Request) (context.Context, any, error) {
	if len(req.ServiceParams.Get("authorization")) > 0 {
		return withCallerAuthorization(ctx), nil, nil
	}
	ctx = withCallState(ctx)
	var r *Requirement
	if req.Card != nil {
		var err error
		r, err = i.m.RequirementForCard(ctx, req.Card, req.BaseURL)
		if err != nil {
			if !acceptsOtherCredentials(req.Card) {
				return ctx, nil, err
			}
			log.Warn(ctx, "not using OAuth for an agent that accepts other credentials", "url", req.BaseURL, "error", err)
		}
	}
	if r == nil || r.Optional {
		g, err := i.optionalGrant(ctx, req, r)
		if err != nil || g == nil {
			if err != nil {
				log.Warn(ctx, "not using a stored OAuth grant", "url", req.BaseURL, "error", err)
			}
			return ctx, nil, nil
		}
		req.ServiceParams.Append("Authorization", g.header())
		return withAttached(ctx, g), nil, nil
	}
	if err := checkTokenDestination(req.BaseURL, req.ProtocolBinding); err != nil {
		return ctx, nil, failure("%w", err)
	}
	g, err := i.m.Grant(ctx, r)
	if err != nil {
		return ctx, nil, err
	}
	if g == nil {
		return ctx, nil, &InteractionRequiredError{Requirement: r}
	}
	req.ServiceParams.Append("Authorization", g.header())
	return withAttached(ctx, g), nil, nil
}

// optionalGrant returns a stored grant for a call that doesn't require one: for
// the optional requirement r, or else for the call's resource.
func (i *interceptor) optionalGrant(ctx context.Context, req *a2aclient.Request, r *Requirement) (*Grant, error) {
	if err := checkTokenDestination(req.BaseURL, req.ProtocolBinding); err != nil {
		log.Debug(ctx, "not looking for a stored OAuth grant", "url", req.BaseURL, "reason", err)
		return nil, nil
	}
	if r != nil {
		return i.m.Grant(ctx, r)
	}
	return i.m.GrantForResource(ctx, req.BaseURL)
}

// After implements [a2aclient.CallInterceptor].
func (i *interceptor) After(ctx context.Context, resp *a2aclient.Response) error {
	cs := callStateFrom(ctx)
	if resp.Err == nil || cs == nil || hasCallerAuthorization(ctx) {
		return nil
	}
	last := cs.lastResponse()
	if last == nil || last.status != http.StatusUnauthorized {
		return nil
	}
	g, attached := attachedFrom(ctx)
	if attached && !slices.Contains(resp.ServiceParams.Get("authorization"), g.header()) {
		// A later interceptor replaced the grant with its own credential.
		return nil
	}
	chs := ParseChallenges(last.header.Values("WWW-Authenticate"))
	switch {
	case last.sent != nil:
		// The request that got the 401 carried one of the Manager's grants: the
		// attached one, a refreshed one, or one the transport attached.
		resp.Err = rejectedGrantError(last.sent, chs, resp.Err)
		return nil
	case attached || last.redirected:
		// A redirect target that never saw the token answered 401: neither the
		// grant's fault nor the agent's challenge.
		return nil
	}
	r, err := i.requirement(ctx, cs, last, chs)
	var nc *NoClientError
	var ac *AmbiguousClientError
	switch {
	case errors.As(err, &nc), errors.As(err, &ac):
		resp.Err = fmt.Errorf("%w: %w", err, resp.Err)
		return nil
	case err != nil:
		log.Debug(ctx, "not using OAuth for a 401 response", "url", resp.BaseURL, "error", err)
		return nil
	}
	if r != nil {
		resp.Err = &InteractionRequiredError{Requirement: r, Err: resp.Err}
	}
	return nil
}

// requirement returns the requirement behind the 401 of a call: the one the
// transport found, or else the one its challenges lead to.
func (i *interceptor) requirement(ctx context.Context, cs *callState, last *callResponse, chs []Challenge) (*Requirement, error) {
	if d := cs.discovered(); d != nil {
		return d.value, d.err
	}
	if last.url == nil {
		return nil, nil
	}
	return i.m.requirementFor401(ctx, last.url, chs)
}

// rejectedGrantError reports that the agent refused a call that carried g: it
// asks for the scopes of an insufficient_scope challenge too.
func rejectedGrantError(g *Grant, chs []Challenge, err error) *InteractionRequiredError {
	r := g.requirement()
	bc, ok := bearerChallenge(chs)
	if !ok || bc.Params["error"] != "insufficient_scope" {
		return &InteractionRequiredError{Requirement: r, Reason: "the agent rejected the stored grant", Err: err}
	}
	r.Scopes = normalizeScopes(slices.Concat(r.Scopes, splitScopes(bc.Params["scope"])))
	return &InteractionRequiredError{Requirement: r, Reason: "the agent asked for more scopes than the stored grant has", Err: err}
}

// checkTokenDestination refuses to send a bearer token where it would travel in
// cleartext: over HTTP, it allows https and http to a loopback host. It allows
// calls over the gRPC binding and gRPC targets, which are addresses without a
// scheme or URIs with a scheme other than http and https, such as
// dns:///host:443: their transport security is up to the gRPC connection.
func checkTokenDestination(target string, binding a2a.TransportProtocol) error {
	if binding == a2a.TransportProtocolGRPC {
		return nil
	}
	scheme, _, ok := strings.Cut(target, "://")
	if !ok {
		return nil
	}
	if scheme = strings.ToLower(scheme); scheme != "http" && scheme != "https" {
		return nil
	}
	return checkHTTPTokenDestination(target)
}
