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
	"net/http"
	"net/url"
	"slices"
	"sync"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient/agentcard"
	"github.com/a2aproject/a2a-go/v2/log"
)

const (
	// detachedTimeout bounds the work done with the lock held, which runs on
	// after the caller may have given up: a refresh, which the server may answer
	// by rotating the refresh token, and the store read before it.
	detachedTimeout = 30 * time.Second
	// storeTimeout bounds storing a refreshed grant. It starts when the refresh
	// returns, so a slow refresh doesn't leave too little time to keep a rotated
	// refresh token.
	storeTimeout = 10 * time.Second
	// lockTimeout bounds the wait for the lock. It is longer than a holder may
	// keep the lock, so a caller does not fail while another one's refresh is
	// still going to succeed.
	lockTimeout = detachedTimeout + storeTimeout + 10*time.Second
	// maxCachedCardRequirements bounds the requirements cached per agent card,
	// and the inline flows cached per declared endpoints.
	maxCachedCardRequirements = 64
	// failureCacheTTL is how long a failure to resolve a card's requirement is
	// cached.
	failureCacheTTL = time.Minute
	// maxLearnedGrants bounds the grants the transport attaches before an agent
	// asks for them.
	maxLearnedGrants = 64
)

// Manager finds, refreshes and stores grants for requests to agents. It is safe
// for concurrent use, but its fields must not be changed after first use, and it
// must not be copied. Diagnostics are written with the logger attached to the
// request context (see package log); tokens are never logged.
//
// A Manager and its Store hold the grants of one user: they don't distinguish
// the session IDs of [a2aclient.AttachSessionID]. Use one Manager per user.
//
// Methods that look for a usable grant, [Manager.Grant] and
// [Manager.GrantForResource], return nil and no error when there is none.
// Methods given the key of a grant, such as [Manager.Revoke], return an error
// wrapping [ErrGrantNotFound] when it is not stored.
//
// Errors are a [*NoClientError] or an [*AmbiguousClientError] when no single
// client registration can be used with an authorization server, and a
// [*FailureError] when discovery, metadata validation, the store, its lock or a
// refresh fails. The sign-in methods also return the errors of the authorization
// server, as an [*Error].
//
// Refreshes and the store writes that keep a grant run under a lock (see
// Locker). A caller waits for it until its context is done, or for at most 50
// seconds. Once a caller holds the lock, the work under it is detached from the
// caller's cancellation and bounded to 30 seconds, plus 10 seconds to store a
// refreshed grant, so a refresh token the server rotated is not lost. A refresh
// does not start for a caller that has already given up.
type Manager struct {
	// Clients are the client registrations to use with authorization servers.
	Clients []Registration
	// DefaultClientID is used as the client ID with authorization servers that
	// support Client ID Metadata Documents when no registration matches. It must
	// be the URL of the client's metadata document. Empty disables the fallback.
	DefaultClientID string
	// Store keeps grants. Defaults to a [MemoryStore].
	Store Store
	// Locker serializes the refreshes and writes of grants with other users of
	// the Store, such as other processes. Defaults to the Store when it
	// implements [Locker]; set it when the Store wraps one that does. The Manager
	// takes its own lock first, so a Locker need not exclude the goroutines that
	// use this Manager, but it must exclude other Managers sharing the Store,
	// in this process too; [MemoryStore]'s does. The context passed to Locker.Lock only bounds the wait: it is
	// canceled once Lock returns, while the lock is still held. The Store's Get,
	// Put and Delete are called with the lock held.
	Locker Locker
	// HTTPClient makes metadata, token, device authorization and revocation
	// requests. Defaults to a client with a 30-second timeout. It must not add
	// agent credentials, so it must not use [Manager.Transport]. Its
	// CheckRedirect is replaced: metadata requests follow redirects to https (or
	// loopback) URLs only, and the other requests don't follow redirects.
	HTTPClient *http.Client

	now func() time.Time

	mu       sync.Mutex
	store    Store
	sem      chan struct{}
	cardReqs boundedCache[cardReqKey, resolved[*Requirement]]
	inline   boundedCache[Endpoints, resolved[Endpoints]]
	learned  boundedCache[string, GrantKey]
	// challenges holds the WWW-Authenticate values of the latest 401 the
	// transport returned per URL, for requests that are not A2A calls, such as
	// agent card fetches; see RequirementForError.
	challenges boundedCache[string, []string]
}

type cardReqKey struct {
	card    *a2a.AgentCard
	baseURL string
}

// resolved is a cached result: a value, or the error and when it happened.
type resolved[V any] struct {
	value V
	err   error
	at    time.Time
}

// fresh reports whether a cached result can still be used at now: values
// always, errors for failureCacheTTL.
func (r resolved[V]) fresh(now time.Time) bool {
	return r.err == nil || now.Sub(r.at) < failureCacheTTL
}

// boundedCache is a map that keeps at most a given number of entries, dropping
// the oldest. Its zero value is empty. The Manager's mu guards it.
type boundedCache[K comparable, V any] struct {
	entries map[K]V
	order   []K
}

func (c *boundedCache[K, V]) get(k K) (V, bool) {
	v, ok := c.entries[k]
	return v, ok
}

func (c *boundedCache[K, V]) put(k K, v V, limit int) {
	if c.entries == nil {
		c.entries = map[K]V{}
	}
	if _, ok := c.entries[k]; !ok {
		if len(c.order) >= limit {
			delete(c.entries, c.order[0])
			c.order = c.order[1:]
		}
		c.order = append(c.order, k)
	}
	c.entries[k] = v
}

func (c *boundedCache[K, V]) remove(k K) {
	if _, ok := c.entries[k]; !ok {
		return
	}
	delete(c.entries, k)
	c.order = slices.DeleteFunc(c.order, func(o K) bool { return o == k })
}

func (m *Manager) fetcher() fetcher {
	if m.HTTPClient == nil {
		return fetcher{client: defaultHTTPClient}
	}
	return fetcher{client: m.HTTPClient}
}

func (m *Manager) tokens() tokenClient {
	return tokenClient{httpClient: m.HTTPClient, now: m.now}
}

func (m *Manager) clock() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

func (m *Manager) grants() Store {
	if m.Store != nil {
		return m.Store
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.store == nil {
		m.store = NewMemoryStore()
	}
	return m.store
}

func (r *Requirement) clone() *Requirement {
	if r == nil {
		return nil
	}
	c := *r
	c.Scopes = slices.Clone(r.Scopes)
	return &c
}

// RequirementForCard returns the OAuth requirement the card declares for calls to
// baseURL, or nil when it declares none. A flow the card declares inline is
// matched to the authorization server of a client registration by its token
// endpoint. Results are cached per card and URL, and inline flows per declared
// endpoints, for a bounded number of them; failures are cached for a minute,
// except those of a call whose context was done. It returns [*NoClientError]
// when no registration matches an inline flow and no other option of the card
// can be used, and [*FailureError] for other failures.
func (m *Manager) RequirementForCard(ctx context.Context, card *a2a.AgentCard, baseURL string) (*Requirement, error) {
	key := cardReqKey{card: card, baseURL: baseURL}
	m.mu.Lock()
	c, ok := m.cardReqs.get(key)
	m.mu.Unlock()
	if !ok || !c.fresh(m.clock()) {
		c = resolved[*Requirement]{at: m.clock()}
		c.value, c.err = m.requirementForCard(ctx, card, baseURL)
		if c.err == nil || ctx.Err() == nil {
			m.mu.Lock()
			m.cardReqs.put(key, c, maxCachedCardRequirements)
			m.mu.Unlock()
		}
	}
	// A card that accepts other credentials requires nothing when its OAuth
	// options fail, but that answer is cached like a failure.
	var skipped *skippedOptionsError
	if errors.As(c.err, &skipped) {
		return nil, nil
	}
	return c.value.clone(), c.err
}

func (m *Manager) requirementForCard(ctx context.Context, card *a2a.AgentCard, baseURL string) (*Requirement, error) {
	r, err := m.fetcher().requirementFromCard(ctx, card, baseURL, m.resolveInlineIssuer)
	var nc *NoClientError
	var ac *AmbiguousClientError
	var skipped *skippedOptionsError
	switch {
	case errors.As(err, &nc), errors.As(err, &ac), errors.As(err, &skipped):
		return nil, err
	case err != nil:
		return nil, failure("reading the authorization requirements of %s: %w", card.Name, err)
	case r == nil:
		return nil, nil
	}
	log.Debug(ctx, "agent card requires OAuth authorization", "issuer", r.Endpoints.Issuer, "resource", r.Resource)
	return r, nil
}

// resolveInlineIssuer resolves the endpoints of an inline flow with the client
// registrations and the default client, which don't change after first use, and
// caches the result per declared endpoints.
func (m *Manager) resolveInlineIssuer(ctx context.Context, declared Endpoints) (Endpoints, error) {
	m.mu.Lock()
	c, ok := m.inline.get(declared)
	m.mu.Unlock()
	if ok && c.fresh(m.clock()) {
		return c.value, c.err
	}
	c = resolved[Endpoints]{at: m.clock()}
	c.value, c.err = m.fetcher().resolveInlineIssuer(ctx, m.Clients, declared, m.DefaultClientID != "")
	if c.err == nil || ctx.Err() == nil {
		m.mu.Lock()
		m.inline.put(declared, c, maxCachedCardRequirements)
		m.mu.Unlock()
	}
	return c.value, c.err
}

// RequirementForChallenge follows the Bearer challenges of a 401 response for
// target to its Protected Resource Metadata and the first listed authorization
// server whose metadata validates and that has a usable client. Only the first
// five listed servers are tried. Unless target is on a loopback host, metadata
// and authorization servers on loopback hosts are refused, so an agent can't
// make the client probe the user's machine. It returns (nil, nil) when the
// resource publishes no metadata, [*NoClientError] when no client can be used
// with any of the servers, [*AmbiguousClientError] when several registrations
// name one, and [*FailureError] for other failures.
func (m *Manager) RequirementForChallenge(ctx context.Context, target *url.URL, chs []Challenge) (*Requirement, error) {
	f := m.fetcher().forAgent(target.String())
	ct, err := f.discoverFromChallenge(ctx, target, chs)
	if errors.Is(err, errNoResourceMetadata) {
		return nil, nil
	}
	if err != nil {
		return nil, failure("discovering authorization for %s: %w", target.Redacted(), err)
	}
	log.Debug(ctx, "found protected resource metadata", "resource", ct.resource.Resource, "authorization_servers", ct.resource.AuthorizationServers)
	var errs []error
	var noClient []string
	for _, issuer := range ct.resource.AuthorizationServers {
		md, err := f.serverMetadata(ctx, issuer)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		e, err := md.usableEndpoints()
		if err := errors.Join(err, f.checkEndpoints(e)); err != nil {
			errs = append(errs, err)
			continue
		}
		if _, err := selectClient(m.Clients, m.DefaultClientID, e); err != nil {
			var nc *NoClientError
			if errors.As(err, &nc) {
				noClient = append(noClient, issuer)
				continue
			}
			return nil, clientError(err)
		}
		return &Requirement{Endpoints: e, Resource: NormalizeResource(ct.resource.Resource), Scopes: ct.scopes}, nil
	}
	if len(noClient) > 0 {
		return nil, &NoClientError{Issuers: noClient}
	}
	return nil, failure("no usable authorization server for %s: %w", ct.resource.Resource, errors.Join(errs...))
}

// RequirementForError returns the requirement behind err, the result of an
// agent card request to target: a 401 [*agentcard.ErrStatusNotOK]. The
// challenge comes from the 401 that [Manager.Transport] returned for target, so
// the card resolver's HTTP client should use the transport; without it, only the
// RFC 9728 well-known locations are tried. It returns nil when err is not such
// an error, when the challenge is neither Bearer nor absent, when target is
// neither https nor http on a loopback host, or when the agent publishes no
// Protected Resource Metadata, and otherwise the errors of
// [Manager.RequirementForChallenge]. A2A calls don't need it: the interceptor
// handles their 401 responses.
func (m *Manager) RequirementForError(ctx context.Context, target *url.URL, err error) (*Requirement, error) {
	var se *agentcard.ErrStatusNotOK
	if !errors.As(err, &se) || se == nil || se.StatusCode != http.StatusUnauthorized {
		return nil, nil
	}
	m.mu.Lock()
	values, _ := m.challenges.get(NormalizeResource(target.String()))
	m.mu.Unlock()
	return m.requirementFor401(ctx, target, ParseChallenges(values))
}

// recordChallenge keeps the WWW-Authenticate values of a 401 for target, for a
// bounded number of URLs.
func (m *Manager) recordChallenge(target *url.URL, values []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.challenges.put(NormalizeResource(target.String()), slices.Clone(values), maxLearnedGrants)
}

// requirementFor401 follows the challenges of a 401 response to a request for
// target, when they offer a Bearer challenge or none at all and target is https
// or http on a loopback host.
func (m *Manager) requirementFor401(ctx context.Context, target *url.URL, chs []Challenge) (*Requirement, error) {
	if !wantsBearer(chs) || checkSecureURL(target.String()) != nil {
		return nil, nil
	}
	return m.RequirementForChallenge(ctx, target, chs)
}

// RequirementForURL requests target without credentials and, when the answer
// is a 401 with a Bearer challenge or no challenge at all, follows it, or the
// RFC 9728 well-known locations, to the requirement. It returns nil when target
// does not answer 401 or publishes no Protected Resource Metadata. Use it to
// find where to sign in for an agent interface before calling it. Target must
// be https, or http on a loopback host. Errors are those of
// [Manager.RequirementForChallenge].
func (m *Manager) RequirementForURL(ctx context.Context, target *url.URL) (*Requirement, error) {
	if err := checkSecureURL(target.String()); err != nil {
		return nil, failure("%w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, failure("%w", err)
	}
	client := *m.fetcher().client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return nil, failure("requesting %s: %w", target.Redacted(), err)
	}
	drain(ctx, resp)
	chs := ParseChallenges(resp.Header.Values("WWW-Authenticate"))
	if resp.StatusCode != http.StatusUnauthorized || !wantsBearer(chs) {
		return nil, nil
	}
	return m.RequirementForChallenge(ctx, target, chs)
}

// Key returns the grant key and client for a requirement. Scopes come from
// override, else the requirement, else the client registration.
func (m *Manager) Key(req *Requirement, override []string) (GrantKey, ClientChoice, error) {
	choice, err := selectClient(m.Clients, m.DefaultClientID, req.Endpoints)
	if err != nil {
		return GrantKey{}, ClientChoice{}, clientError(err)
	}
	scopes := override
	if len(scopes) == 0 {
		scopes = req.Scopes
	}
	if len(scopes) == 0 {
		scopes = choice.Scopes
	}
	return NewGrantKey(req.Endpoints.Issuer, choice.ClientID, req.Resource, scopes), choice, nil
}

// clientError keeps the error types that say which registration to fix and
// reports other registration problems as a [*FailureError].
func clientError(err error) error {
	var nc *NoClientError
	var ac *AmbiguousClientError
	if errors.As(err, &nc) || errors.As(err, &ac) {
		return err
	}
	return failure("%w", err)
}

// Grant returns the stored grant for a requirement, refreshed when it is about to
// expire, or nil when there is none that can be used without user interaction.
// It uses the grant for the requirement's key, whatever scopes the server
// listed when granting it, or else the grant for the same server and client
// whose resource is the longest that covers the requirement's and whose granted
// scopes, or requested ones when the server did not name them, include the
// requirement's; for the requirement's own resource, requested scopes that
// include the requirement's are enough. Of several grants for that resource it
// uses the one whose requested scopes include the others', and otherwise none.
// Signing in for more scopes deletes the narrower grants for the resource.
func (m *Manager) Grant(ctx context.Context, req *Requirement) (*Grant, error) {
	key, _, err := m.Key(req, nil)
	if err != nil {
		return nil, err
	}
	found, err := m.lookup(ctx, key)
	if err != nil || found == nil {
		return nil, err
	}
	return m.grant(ctx, *found)
}

// lookup returns the key of the stored grant to use for key, or nil when there
// is none; see [Manager.Grant].
func (m *Manager) lookup(ctx context.Context, key GrantKey) (*GrantKey, error) {
	// The grant stored under the key was asked for with its scopes, so it is
	// used even when the server listed fewer in its response, as Entra does
	// with offline_access. Granted scopes only decide whether a grant for
	// another key will do.
	_, err := m.get(ctx, key)
	switch {
	case err == nil:
		return &key, nil
	case !errors.Is(err, ErrGrantNotFound):
		return nil, failure("reading the grant store: %w", err)
	}
	grants, err := m.grants().List(ctx)
	if err != nil {
		return nil, failure("listing the grant store: %w", err)
	}
	// A grant for the same resource that was asked for at least key's scopes,
	// as after a step-up, is used like the exact key's; a grant for a resource
	// above key's must have been granted them.
	return resolveGrant(ctx, grants, key.Resource, func(g *Grant) bool {
		if g.Key.Issuer != key.Issuer || g.Key.ClientID != key.ClientID {
			return false
		}
		sameResource := NormalizeResource(g.Key.Resource) == NormalizeResource(key.Resource)
		return containsAll(g.scopes(), key.Scopes) || (sameResource && containsAll(g.Key.Scopes, key.Scopes))
	}), nil
}

// resolveGrant returns the key of the grant whose resource is the longest that
// covers target among those that match, or nil when there is none or when
// several share that resource.
func resolveGrant(ctx context.Context, grants []Grant, target string, match func(*Grant) bool) *GrantKey {
	var best []*Grant
	for i := range grants {
		g := &grants[i]
		if !resourceCovers(g.Key.Resource, target) || (match != nil && !match(g)) {
			continue
		}
		switch {
		case len(best) == 0 || len(g.Key.Resource) > len(best[0].Key.Resource):
			best = []*Grant{g}
		case len(g.Key.Resource) == len(best[0].Key.Resource):
			best = append(best, g)
		}
	}
	switch {
	case len(best) == 0:
		return nil
	case len(best) == 1:
		return &best[0].Key
	}
	if w := widest(best); w != nil {
		return &w.Key
	}
	log.Debug(ctx, "several OAuth grants match; not choosing one", "resource", best[0].Key.Resource)
	return nil
}

// widest returns the grant whose requested scopes include those of every other
// grant in gs, when all of them are for the same issuer, client and resource,
// as after the user signed in again for more scopes; otherwise nil.
func widest(gs []*Grant) *Grant {
	w := gs[0]
	for _, g := range gs[1:] {
		if containsAll(g.Key.Scopes, w.Key.Scopes) {
			w = g
		}
	}
	for _, g := range gs {
		if g.Key.Issuer != w.Key.Issuer || g.Key.ClientID != w.Key.ClientID ||
			NormalizeResource(g.Key.Resource) != NormalizeResource(w.Key.Resource) || !containsAll(w.Key.Scopes, g.Key.Scopes) {
			return nil
		}
	}
	return w
}

func containsAll(have, want []string) bool {
	for _, w := range want {
		if !slices.Contains(have, w) {
			return false
		}
	}
	return true
}

// get reads the grant for key from the store. A store that returns neither a
// grant nor an error is taken to hold none.
func (m *Manager) get(ctx context.Context, key GrantKey) (*Grant, error) {
	g, err := m.grants().Get(ctx, key)
	if err == nil && g == nil {
		return nil, ErrGrantNotFound
	}
	return g, err
}

func (m *Manager) grant(ctx context.Context, key GrantKey) (*Grant, error) {
	g, err := m.get(ctx, key)
	if errors.Is(err, ErrGrantNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, failure("reading the grant store: %w", err)
	}
	if !g.needsRefresh(m.clock()) {
		return g, nil
	}
	return m.refresh(ctx, key, false, "")
}

// refreshRejected refreshes the grant for key after the agent refused its
// access token, unless another process has already replaced that token. It
// returns nil when a new authorization is needed.
func (m *Manager) refreshRejected(ctx context.Context, key GrantKey, token string) (*Grant, error) {
	return m.refresh(ctx, key, true, token)
}

// refresh refreshes the grant for key under the lock when its access token is
// missing or about to expire, or, when rejected is set, when it is still token.
// It returns nil when a new authorization is needed. Once it holds the lock, it
// runs detached from the cancellation of ctx, so a refresh token the server
// rotated is not lost; it does not start when ctx is already done by then.
func (m *Manager) refresh(ctx context.Context, key GrantKey, rejected bool, token string) (*Grant, error) {
	unlock, err := m.lock(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if err := ctx.Err(); err != nil {
		return nil, failure("locking grants: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), detachedTimeout)
	defer cancel()

	g, err := m.get(ctx, key)
	if errors.Is(err, ErrGrantNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, failure("reading the grant store: %w", err)
	}
	now := m.clock()
	if g.AccessToken == "" && g.RefreshToken == "" {
		return nil, nil
	}
	if !rejected && !g.needsRefresh(now) {
		return g, nil
	}
	if rejected && g.AccessToken != token && !g.needsRefresh(now) {
		return g, nil
	}
	usable := g.AccessToken != "" && !g.expired(now)
	if g.RefreshToken == "" {
		switch {
		case rejected && g.AccessToken == token:
			return nil, m.forgetAccessToken(ctx, g)
		case !rejected && usable:
			return g, nil
		default:
			return nil, nil
		}
	}
	log.Debug(ctx, "refreshing the OAuth access token", "resource", g.Key.Resource)
	refreshed, err := m.tokens().Refresh(ctx, g)
	if isOAuthError(err, "invalid_grant") {
		log.Info(ctx, "the OAuth refresh token was rejected; a new authorization is needed", "resource", g.Key.Resource)
		if err := m.grants().Delete(ctx, key); err != nil {
			return nil, failure("deleting the rejected grant: %w", err)
		}
		m.forget(key)
		return nil, nil
	}
	if err != nil {
		if !rejected && usable {
			log.Warn(ctx, "refreshing the OAuth access token failed; using the current one", "resource", g.Key.Resource, "error", err)
			return g, nil
		}
		return nil, failure("refreshing the access token for %s: %w", g.Key.Resource, err)
	}
	putCtx, cancelPut := context.WithTimeout(context.WithoutCancel(ctx), storeTimeout)
	defer cancelPut()
	if err := m.grants().Put(putCtx, *refreshed); err != nil {
		return nil, failure("storing the refreshed grant: %w", err)
	}
	return refreshed, nil
}

// forgetAccessToken removes an access token the agent rejected from a grant
// that can't be refreshed, so it is not sent again.
func (m *Manager) forgetAccessToken(ctx context.Context, g *Grant) error {
	log.Debug(ctx, "the agent rejected the OAuth access token, which can't be refreshed; forgetting it", "resource", g.Key.Resource)
	g.AccessToken, g.ExpiresAt = "", time.Time{}
	if err := m.grants().Put(ctx, *g); err != nil {
		return failure("storing the grant: %w", err)
	}
	return nil
}

// NewAuthorizationRequest starts the authorization code flow for a requirement
// with the client from [Manager.Key]. Pass the code that comes back to
// [Manager.ExchangeCode].
func (m *Manager) NewAuthorizationRequest(req *Requirement, redirectURI string, scopes []string) (*AuthorizationRequest, error) {
	key, choice, err := m.Key(req, scopes)
	if err != nil {
		return nil, err
	}
	return newAuthorizationRequest(req.Endpoints, key, redirectURI, choice.OmitResource)
}

// ExchangeCode exchanges an authorization code for a grant and stores it.
func (m *Manager) ExchangeCode(ctx context.Context, ar *AuthorizationRequest, code string) (*Grant, error) {
	g, err := m.tokens().ExchangeCode(ctx, ar, code)
	if err != nil {
		return nil, err
	}
	if err := m.save(ctx, g); err != nil {
		return nil, err
	}
	return g, nil
}

// StartDeviceAuthorization starts the device authorization grant for a
// requirement with the client from [Manager.Key]. Pass the result to
// [Manager.PollDeviceAuthorization].
func (m *Manager) StartDeviceAuthorization(ctx context.Context, req *Requirement, scopes []string) (*DeviceAuthorization, error) {
	key, choice, err := m.Key(req, scopes)
	if err != nil {
		return nil, err
	}
	return m.tokens().StartDeviceAuthorization(ctx, req.Endpoints, key, choice.OmitResource)
}

// PollDeviceAuthorization waits for the user to approve a device authorization
// and stores the grant.
func (m *Manager) PollDeviceAuthorization(ctx context.Context, da *DeviceAuthorization) (*Grant, error) {
	g, err := m.tokens().PollDeviceAuthorization(ctx, da)
	if err != nil {
		return nil, err
	}
	if err := m.save(ctx, g); err != nil {
		return nil, err
	}
	return g, nil
}

// save stores a new grant and remembers it for requests to its resource made
// before the agent asks for it. The wait for the lock and the write are
// detached from the cancellation of ctx, so a grant obtained just before the
// caller gave up is still stored.
func (m *Manager) save(ctx context.Context, g *Grant) error {
	if choice, err := selectClient(m.Clients, m.DefaultClientID, g.Endpoints); err == nil && choice.ClientID == g.Key.ClientID {
		g.ClientSource = choice.Source
	}
	unlock, err := m.lock(context.WithoutCancel(ctx))
	if err != nil {
		return err
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), detachedTimeout)
	defer cancel()
	if err := m.grants().Put(ctx, *g); err != nil {
		return failure("storing the grant: %w", err)
	}
	m.learn(g.Key)
	return m.pruneNarrower(ctx, g.Key)
}

// pruneNarrower deletes the grants that key supersedes: those for the same
// issuer, client and resource whose requested scopes key's include, as after
// the user signed in again because the agent asked for more scopes. Left in the
// store, the narrower grant would still be attached to calls that ask for its
// scopes, and the agent would refuse it again. The caller holds the lock.
func (m *Manager) pruneNarrower(ctx context.Context, key GrantKey) error {
	grants, err := m.grants().List(ctx)
	if err != nil {
		return failure("listing the grant store: %w", err)
	}
	for _, g := range grants {
		if g.Key.Equal(key) || g.Key.Issuer != key.Issuer || g.Key.ClientID != key.ClientID ||
			NormalizeResource(g.Key.Resource) != NormalizeResource(key.Resource) || !containsAll(key.Scopes, g.Key.Scopes) {
			continue
		}
		if err := m.grants().Delete(ctx, g.Key); err != nil {
			return failure("deleting a grant the new one supersedes: %w", err)
		}
		m.forget(g.Key)
	}
	return nil
}

// GrantForResource returns the stored grant for the longest resource covering
// resource, refreshed when needed, or nil when there is none or several grants
// share that resource.
func (m *Manager) GrantForResource(ctx context.Context, resource string) (*Grant, error) {
	resource = NormalizeResource(resource)
	m.mu.Lock()
	key, ok := m.learned.get(resource)
	m.mu.Unlock()
	if ok {
		return m.grant(ctx, key)
	}
	grants, err := m.grants().List(ctx)
	if err != nil {
		return nil, failure("listing the grant store: %w", err)
	}
	found := resolveGrant(ctx, grants, resource, nil)
	if found == nil {
		return nil, nil
	}
	return m.grant(ctx, *found)
}

// learnedGrant returns the grant learned in this process for the longest
// resource covering target.
func (m *Manager) learnedGrant(ctx context.Context, target *url.URL) (*Grant, error) {
	m.mu.Lock()
	learned := make([]Grant, 0, len(m.learned.entries))
	for _, k := range m.learned.entries {
		learned = append(learned, Grant{Key: k})
	}
	m.mu.Unlock()
	key := resolveGrant(ctx, learned, target.String(), nil)
	if key == nil {
		return nil, nil
	}
	return m.grant(ctx, *key)
}

// learn remembers the grant for key for the requests to its resource, for a
// bounded number of resources.
func (m *Manager) learn(key GrantKey) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.learned.put(key.Resource, key, maxLearnedGrants)
}

// forget stops attaching the grant for key to requests before the agent asks.
func (m *Manager) forget(key GrantKey) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if k, ok := m.learned.get(key.Resource); ok && k.Equal(key) {
		m.learned.remove(key.Resource)
	}
}

// List returns every stored grant, in no particular order. Depending on the
// Store, the grants may lack their tokens (see [Store]); Refreshable still
// reports whether a grant has a refresh token. Pass a grant's key to
// [Manager.Revoke] or [Manager.Delete].
func (m *Manager) List(ctx context.Context) ([]Grant, error) {
	grants, err := m.grants().List(ctx)
	if err != nil {
		return nil, failure("listing the grant store: %w", err)
	}
	return grants, nil
}

// Revoke revokes the stored grant for key at its authorization server. It does
// not remove the grant from the store; see [Manager.Delete]. It returns an error
// wrapping [ErrGrantNotFound] when no grant is stored for key, and
// [ErrNoRevocationEndpoint] when the server does not support revocation.
func (m *Manager) Revoke(ctx context.Context, key GrantKey) error {
	g, err := m.get(ctx, key)
	if errors.Is(err, ErrGrantNotFound) {
		return err
	}
	if err != nil {
		return failure("reading the grant store: %w", err)
	}
	return m.tokens().Revoke(ctx, g)
}

// Delete removes the grant for key from the store, if any.
func (m *Manager) Delete(ctx context.Context, key GrantKey) error {
	unlock, err := m.lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), detachedTimeout)
	defer cancel()
	if err := m.grants().Delete(ctx, key); err != nil {
		return failure("deleting the grant: %w", err)
	}
	m.forget(key)
	return nil
}

func (m *Manager) locker() Locker {
	if m.Locker != nil {
		return m.Locker
	}
	l, _ := m.grants().(Locker)
	return l
}

// lock takes the manager's own lock, then the [Locker] if there is one. It waits
// until ctx is done, or at most lockTimeout.
func (m *Manager) lock(ctx context.Context) (func(), error) {
	ctx, cancel := context.WithTimeout(ctx, lockTimeout)
	defer cancel()
	m.mu.Lock()
	if m.sem == nil {
		m.sem = make(chan struct{}, 1)
	}
	sem := m.sem
	m.mu.Unlock()
	select {
	case sem <- struct{}{}:
	case <-ctx.Done():
		return nil, failure("locking grants: %w", ctx.Err())
	}
	l := m.locker()
	if l == nil {
		return func() { <-sem }, nil
	}
	unlock, err := l.Lock(ctx)
	if err != nil {
		<-sem
		return nil, failure("locking the grant store: %w", err)
	}
	return func() {
		unlock()
		<-sem
	}, nil
}
