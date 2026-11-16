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
	"iter"
	"net/url"
	"slices"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/log"
)

// Plugin returns an [a2aclient.Plugin] that attaches grants to the calls of an
// A2A client as an Authorization service parameter, refreshes a token the agent
// rejects and repeats the call once, and reports the calls that need the user
// to sign in as a [*SignInRequiredError]:
//
//	client, err := a2aclient.NewFromCard(ctx, card, a2aclient.WithPlugins(m.Plugin()))
//
// Before a call: when the agent card requires OAuth for the call's interface,
// the call fails without being sent with a [*SignInRequiredError] if no grant is
// stored, with a [*NoClientError] or an [*AmbiguousClientError] if no single
// client registration can be used, and with a [*FailureError] if discovery, the
// store or a refresh fails. Otherwise the stored grant for the longest resource
// covering the interface is attached when there is one, and problems finding it
// are only logged, so agents that accept other credentials or none are not
// affected. Calls that already carry an Authorization service parameter, for
// example from [a2aclient.AttachServiceParams], are left alone, errors
// included. Over HTTP, tokens are only sent over https or to loopback hosts;
// over the gRPC binding, the transport security of the connection is up to the
// application.
//
// After a call the agent refused, the plugin reads the response's
// WWW-Authenticate challenges from the error's [a2a.Error.ServiceParams]:
//   - a Bearer challenge with error="insufficient_scope", which RFC 6750 sends
//     with 403, makes a call that carried a grant fail with a
//     [*SignInRequiredError] for the grant's scopes and the ones the challenge
//     names;
//   - after a 401 (an error matching [a2a.ErrUnauthenticated]) with a Bearer
//     challenge naming no error or error="invalid_token", a call that carried a
//     grant has its token refreshed and is repeated once with the new one, or
//     fails with a [*SignInRequiredError] when the grant can't be refreshed; a
//     call sent without a grant follows the challenge to the agent's Protected
//     Resource Metadata (see [Manager.Discover]) and is repeated once with a
//     stored grant for the requirement found, or fails with a
//     [*SignInRequiredError] for it, or with the [*NoClientError] or
//     [*AmbiguousClientError] discovery returned;
//   - any other answer, a 401 without a Bearer challenge included, is returned
//     as it is, since nothing says OAuth would help.
//
// Network and metadata failures during discovery leave the original error. The
// new errors wrap the original one, so errors.Is and errors.As still find it. A
// stream is repeated only when its error came before its first event.
// Discovery needs an https, or loopback http, interface URL: over gRPC, stored
// grants are attached and refreshed, but a challenge to a call without a grant
// is returned as it is.
//
// The plugin wraps the client's [a2aclient.CallInterceptor]s, so it doesn't
// see the Authorization an interceptor sets, such as an
// [a2aclient.AuthInterceptor]: an interceptor that replaces the grant leaves a
// 401 for its credential to the plugin, which spends at most one refresh on it
// before returning a [*SignInRequiredError]. Provide such credentials with
// [a2aclient.AttachServiceParams], or in a plugin installed before this one,
// instead. The plugin doesn't see redirects either: the HTTP client keeps the
// Authorization header on redirects within a host.
func (m *Manager) Plugin() a2aclient.Plugin {
	return &plugin{m: m}
}

type plugin struct {
	a2aclient.PassthroughPlugin
	m *Manager
}

var _ a2aclient.Plugin = (*plugin)(nil)

// Name implements [a2aclient.Plugin].
func (*plugin) Name() string { return "oauth" }

// WrapCall implements [a2aclient.Plugin].
func (p *plugin) WrapCall(next a2aclient.CallHandler) a2aclient.CallHandler {
	return func(ctx context.Context, req a2aclient.Request) iter.Seq2[any, error] {
		if len(req.ServiceParams.Get("authorization")) > 0 {
			return next(ctx, req)
		}
		return func(yield func(any, error) bool) {
			// attempt sends the call with g attached, yielding what it returns,
			// unless its first result is an error, which it returns instead.
			attempt := func(g *Grant) (error, bool) {
				first := true
				for v, err := range next(ctx, withGrant(req, g)) {
					if first && err != nil {
						return err, true
					}
					first = false
					if !yield(v, err) {
						return nil, false
					}
				}
				return nil, true
			}
			g, err := p.prepare(ctx, req)
			if err != nil {
				yield(nil, err)
				return
			}
			err, more := attempt(g)
			if err == nil || !more {
				return
			}
			retry, err := p.afterUnauthenticated(ctx, req, g, err)
			if retry == nil {
				yield(nil, err)
				return
			}
			err, more = attempt(retry)
			if err == nil || !more {
				return
			}
			yield(nil, p.afterRetry(retry, err))
		}
	}
}

// withGrant returns req with g's token as its Authorization service parameter,
// on a copy of its service parameters, which the caller may still use.
func withGrant(req a2aclient.Request, g *Grant) a2aclient.Request {
	if g == nil {
		return req
	}
	params := make(a2aclient.ServiceParams, len(req.ServiceParams)+1)
	for k, v := range req.ServiceParams {
		params[k] = slices.Clone(v)
	}
	params.Append("Authorization", g.header())
	req.ServiceParams = params
	return req
}

// prepare returns the grant to attach to req, or nil, or the error that stops
// the call before it is sent.
func (p *plugin) prepare(ctx context.Context, req a2aclient.Request) (*Grant, error) {
	var r *Requirement
	if req.Card != nil {
		var err error
		r, err = p.m.requirementForCard(ctx, req.Card, req.BaseURL)
		if err != nil {
			if !acceptsOtherCredentials(req.Card) {
				return nil, err
			}
			log.Warn(ctx, "not using OAuth for an agent that accepts other credentials", "url", redactURL(req.BaseURL), "error", err)
			r = nil
		}
	}
	if err := checkTokenDestination(req.BaseURL, req.ProtocolBinding); err != nil {
		if r == nil || r.Optional {
			log.Debug(ctx, "not looking for a stored OAuth grant", "url", redactURL(req.BaseURL), "reason", err)
			return nil, nil
		}
		return nil, failure("%w", err)
	}
	if r == nil || r.Optional {
		g, err := p.optionalGrant(ctx, req, r)
		if err != nil {
			log.Warn(ctx, "not using a stored OAuth grant", "url", redactURL(req.BaseURL), "error", err)
			return nil, nil
		}
		return g, nil
	}
	g, err := p.m.grantForRequirement(ctx, r)
	if err != nil {
		return nil, err
	}
	if g == nil {
		return nil, &SignInRequiredError{Requirement: r}
	}
	return g, nil
}

// optionalGrant returns a stored grant for a call that doesn't require one: for
// the optional requirement r, or else for the call's interface.
func (p *plugin) optionalGrant(ctx context.Context, req a2aclient.Request, r *Requirement) (*Grant, error) {
	if r != nil {
		return p.m.grantForRequirement(ctx, r)
	}
	return p.m.grantForResource(ctx, req.BaseURL)
}

// afterUnauthenticated handles the error of a call sent with g, or without a
// grant when g is nil: it returns the grant to repeat the call with, or the
// error to return.
func (p *plugin) afterUnauthenticated(ctx context.Context, req a2aclient.Request, g *Grant, err error) (*Grant, error) {
	chs := challengesOf(err)
	bc, bearer := bearerChallenge(chs)
	if g != nil && insufficientScope(err, bc, bearer) {
		return nil, rejectedGrantError(g, chs, err)
	}
	if !errors.Is(err, a2a.ErrUnauthenticated) || !bearer {
		// Without a Bearer challenge nothing says OAuth would help, as when
		// the agent refuses the call for another reason.
		return nil, err
	}
	if g != nil {
		if code := bc.Params["error"]; code != "" && code != "invalid_token" {
			// The agent refused the request itself (invalid_request), not
			// the token.
			return nil, err
		}
		log.Debug(ctx, "the agent rejected the OAuth access token; refreshing", "resource", g.Resource)
		refreshed, rerr := p.m.refreshRejected(ctx, g.key(), g.AccessToken)
		var fe *FailureError
		switch {
		case errors.As(rerr, &fe):
			return nil, &FailureError{Err: fmt.Errorf("the agent rejected the OAuth access token, and refreshing it failed: %w", errors.Join(fe.Err, err))}
		case rerr != nil:
			return nil, rerr
		case refreshed == nil:
			return nil, rejectedGrantError(g, chs, err)
		}
		return refreshed, nil
	}
	target, perr := url.Parse(req.BaseURL)
	if perr != nil || checkSecureURL(req.BaseURL) != nil {
		// Discovery needs an https, or loopback http, URL to find the
		// agent's Protected Resource Metadata; a gRPC address has none.
		return nil, err
	}
	r, derr := p.m.requirementForChallenge(ctx, target, chs)
	var nc *NoClientError
	var ac *AmbiguousClientError
	switch {
	case errors.As(derr, &nc), errors.As(derr, &ac):
		return nil, fmt.Errorf("%w: %w", derr, err)
	case derr != nil:
		log.Debug(ctx, "not using OAuth for an unauthenticated call", "url", redactURL(req.BaseURL), "error", derr)
		return nil, err
	case r == nil:
		return nil, err
	}
	stored, gerr := p.m.grantForRequirement(ctx, r)
	if gerr != nil {
		log.Info(ctx, "not using a stored OAuth grant for an unauthenticated call", "url", redactURL(req.BaseURL), "error", gerr)
	}
	if stored != nil {
		return stored, nil
	}
	return nil, &SignInRequiredError{Requirement: r, Err: err}
}

// afterRetry returns the error of a call repeated with g.
func (p *plugin) afterRetry(g *Grant, err error) error {
	chs := challengesOf(err)
	bc, bearer := bearerChallenge(chs)
	if insufficientScope(err, bc, bearer) || (bearer && errors.Is(err, a2a.ErrUnauthenticated)) {
		return rejectedGrantError(g, chs, err)
	}
	return err
}

// insufficientScope reports whether err refuses a token for want of scopes: a
// Bearer challenge with error="insufficient_scope", which RFC 6750 sends with
// 403, and some servers with 401.
func insufficientScope(err error, bc challenge, bearer bool) bool {
	return bearer && bc.Params["error"] == "insufficient_scope" &&
		(errors.Is(err, a2a.ErrUnauthorized) || errors.Is(err, a2a.ErrUnauthenticated))
}

// challengesOf returns the WWW-Authenticate challenges of the response an error
// came from, as the client transport recorded them in [a2a.Error.ServiceParams].
func challengesOf(err error) []challenge {
	var ae *a2a.Error
	if !errors.As(err, &ae) {
		return nil
	}
	var values []string
	for k, v := range ae.ServiceParams {
		if strings.EqualFold(k, "WWW-Authenticate") {
			values = append(values, v...)
		}
	}
	return parseChallenges(values)
}

// rejectedGrantError reports that the agent refused a call that carried g: it
// asks for the scopes of an insufficient_scope challenge too.
func rejectedGrantError(g *Grant, chs []challenge, err error) *SignInRequiredError {
	r := g.requirement()
	bc, ok := bearerChallenge(chs)
	if !ok || bc.Params["error"] != "insufficient_scope" {
		return &SignInRequiredError{Requirement: r, Reason: "the agent rejected the stored grant", Err: err}
	}
	r.Scopes = normalizeScopes(slices.Concat(r.Scopes, splitScopes(bc.Params["scope"])))
	return &SignInRequiredError{Requirement: r, Reason: "the agent asked for more scopes than the stored grant has", Err: err}
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
