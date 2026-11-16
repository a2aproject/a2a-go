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
	"net/http"
	"net/url"
	"slices"
	"sync"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
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
)

// Manager finds, obtains, refreshes and stores grants for requests to agents.
// Install [Manager.Plugin] on an A2A client to attach them to its calls. It is
// safe for concurrent use, but its fields must not be changed after first use,
// and it must not be copied. Diagnostics are written with the logger attached
// to the request context (see package log); tokens are never logged.
//
// A Manager and its Store hold the grants of one user: they don't distinguish
// the session IDs of [a2aclient.AttachSessionID]. Use one Manager per user.
//
// Errors are a [*SignInRequiredError] when the user has to sign in, a
// [*NoClientError] or an [*AmbiguousClientError] when no single client
// registration can be used with an authorization server, and a [*FailureError]
// when discovery, metadata validation, the store, its lock or a refresh fails.
// Signing in also returns the errors of the authorization server, as an
// [*Error].
//
// Refreshes and the store writes that keep a grant run under a lock: the
// Manager's own, then Locker if set. A caller waits for it until its context is
// done, or for at most 50 seconds. Once a caller holds the lock, the work under
// it is detached from the caller's cancellation and bounded to 30 seconds, plus
// 10 seconds to store a refreshed grant, so a refresh token the server rotated
// is not lost. A refresh does not start for a caller that has already given up.
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
	// the Store, such as other processes; without it the Manager only locks
	// within itself. The Manager takes its own lock first, so a Locker need not
	// exclude the goroutines that use this Manager, but it must exclude other
	// Managers sharing the Store, in this process too; a [MemoryStore] shared by
	// several Managers can serve as their Locker. The context passed to
	// Locker.Lock only bounds the wait: it is canceled once Lock returns, while
	// the lock is still held. The Store's Get, Put and Delete are called with the
	// lock held.
	Locker Locker
	// HTTPClient makes metadata, token, device authorization and revocation
	// requests, and the probes of [Manager.Discover]. Defaults to a client with a
	// 30-second timeout. It must not add agent credentials. Its CheckRedirect is
	// replaced: metadata requests follow redirects to https (or loopback) URLs
	// only, and the other requests don't follow redirects.
	HTTPClient *http.Client

	now   func() time.Time
	sleep func(ctx context.Context, d time.Duration) error

	mu       sync.Mutex
	store    Store
	sem      chan struct{}
	cardReqs boundedCache[cardReqKey, resolved[*Requirement]]
	inline   boundedCache[Endpoints, resolved[Endpoints]]
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

func (m *Manager) fetcher() fetcher {
	if m.HTTPClient == nil {
		return fetcher{client: defaultHTTPClient}
	}
	return fetcher{client: m.HTTPClient}
}

func (m *Manager) tokens() tokenClient {
	return tokenClient{httpClient: m.HTTPClient, now: m.now, sleep: m.sleep}
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

// Discover returns the OAuth requirement for requests to target, or nil when
// there is none.
//
// With a card, it is the requirement the card declares for calls to target, the
// URL of one of its interfaces. A flow the card declares inline is matched to
// the authorization server of a client registration by its token endpoint.
// Results are cached per card and URL, and inline flows per declared endpoints,
// for a bounded number of them; failures are cached for a minute, except those
// of a call whose context was done.
//
// Without a card, it requests target without credentials, as for an agent card
// that may be protected or an interface whose card says nothing about OAuth,
// and follows the Bearer challenge of a 401 answer, or the RFC 9728 well-known
// locations when the answer has no challenge, to the agent's Protected Resource
// Metadata and the first listed authorization server whose metadata validates
// and that has a usable client. Only the first five listed servers are tried.
// Unless target is on a loopback host, metadata and authorization servers on
// loopback hosts are refused, so an agent can't make the client probe the
// user's machine. Target must be https, or http on a loopback host. It returns
// nil when target does not answer 401 or publishes no Protected Resource
// Metadata.
//
// It returns [*NoClientError] when no client can be used, [*AmbiguousClientError]
// when several registrations name the authorization server, and
// [*FailureError] for other failures.
func (m *Manager) Discover(ctx context.Context, target string, card *a2a.AgentCard) (*Requirement, error) {
	if card != nil {
		return m.requirementForCard(ctx, card, target)
	}
	if err := checkSecureURL(target); err != nil {
		return nil, failure("%w", err)
	}
	u, err := url.Parse(target)
	if err != nil {
		return nil, failure("%w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, failure("%w", err)
	}
	client := *m.fetcher().client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return nil, failure("requesting %s: %w", u.Redacted(), err)
	}
	drain(ctx, resp)
	chs := parseChallenges(resp.Header.Values("WWW-Authenticate"))
	if resp.StatusCode != http.StatusUnauthorized || !wantsBearer(chs) {
		return nil, nil
	}
	return m.requirementForChallenge(ctx, u, chs)
}

// requirementForCard returns the requirement card declares for calls to
// baseURL, with the caching described at [Manager.Discover].
func (m *Manager) requirementForCard(ctx context.Context, card *a2a.AgentCard, baseURL string) (*Requirement, error) {
	key := cardReqKey{card: card, baseURL: baseURL}
	m.mu.Lock()
	c, ok := m.cardReqs.get(key)
	m.mu.Unlock()
	if !ok || !c.fresh(m.clock()) {
		c = resolved[*Requirement]{at: m.clock()}
		c.value, c.err = m.resolveCard(ctx, card, baseURL)
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

func (m *Manager) resolveCard(ctx context.Context, card *a2a.AgentCard, baseURL string) (*Requirement, error) {
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

// requirementForChallenge follows the Bearer challenges of a 401 response for
// target to its Protected Resource Metadata and an authorization server, as
// described at [Manager.Discover]. It returns (nil, nil) when the resource
// publishes no metadata.
func (m *Manager) requirementForChallenge(ctx context.Context, target *url.URL, chs []challenge) (*Requirement, error) {
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

// wantsBearer reports whether a 401 may be OAuth-protected: it offers a Bearer
// challenge, or no challenge at all.
func wantsBearer(chs []challenge) bool {
	if len(chs) == 0 {
		return true
	}
	_, ok := bearerChallenge(chs)
	return ok
}

// key returns the grant key and client for a requirement. Scopes come from
// override, else the requirement, else the client registration.
func (m *Manager) key(req *Requirement, override []string) (grantKey, clientChoice, error) {
	choice, err := selectClient(m.Clients, m.DefaultClientID, req.Endpoints)
	if err != nil {
		return grantKey{}, clientChoice{}, clientError(err)
	}
	scopes := override
	if len(scopes) == 0 {
		scopes = req.Scopes
	}
	if len(scopes) == 0 {
		scopes = choice.Scopes
	}
	return newGrantKey(req.Endpoints.Issuer, choice.ClientID, req.Resource, scopes), choice, nil
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

// Token returns an access token for requests to target from the stored grant
// for the longest resource covering it, refreshed when it is about to expire.
// Use it for requests an A2A client doesn't make, such as fetching a protected
// agent card. Target must be https, or http on a loopback host. It returns an
// error wrapping [ErrGrantNotFound] when no single grant covers target, and a
// [*SignInRequiredError] when the grant can't be refreshed.
func (m *Manager) Token(ctx context.Context, target string) (string, error) {
	if err := checkHTTPTokenDestination(target); err != nil {
		return "", failure("%w", err)
	}
	grants, err := m.grants().List(ctx)
	if err != nil {
		return "", failure("listing the grant store: %w", err)
	}
	found := resolveGrant(ctx, grants, target, nil)
	if found == nil {
		return "", fmt.Errorf("%w for %s", ErrGrantNotFound, redactURL(target))
	}
	g, err := m.grant(ctx, found.key())
	if err != nil {
		return "", err
	}
	if g == nil || g.AccessToken == "" {
		return "", &SignInRequiredError{Requirement: found.requirement(), Reason: "the stored grant can't be refreshed"}
	}
	return g.AccessToken, nil
}

// grantForRequirement returns the stored grant for a requirement, refreshed when
// it is about to expire, or nil when there is none that can be used without
// user interaction. It uses the grant for the requirement's key, whatever
// scopes the server listed when granting it, or else the grant for the same
// server and client whose resource is the longest that covers the requirement's
// and whose granted scopes, or requested ones when the server did not name
// them, include the requirement's; for the requirement's own resource,
// requested scopes that include the requirement's are enough. Of several grants
// for that resource it uses the one whose requested scopes include the others',
// and otherwise none. Signing in for more scopes deletes the narrower grants for
// the resource.
func (m *Manager) grantForRequirement(ctx context.Context, req *Requirement) (*Grant, error) {
	key, _, err := m.key(req, nil)
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
// is none; see [Manager.grantForRequirement].
func (m *Manager) lookup(ctx context.Context, key grantKey) (*grantKey, error) {
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
	found := resolveGrant(ctx, grants, key.Resource, func(g *Grant) bool {
		k := g.key()
		if k.Issuer != key.Issuer || k.ClientID != key.ClientID {
			return false
		}
		return containsAll(g.scopes(), key.Scopes) || (k.Resource == key.Resource && containsAll(k.Scopes, key.Scopes))
	})
	if found == nil {
		return nil, nil
	}
	k := found.key()
	return &k, nil
}

// grantForResource returns the stored grant for the longest resource covering
// target, refreshed when needed, or nil when there is none or several grants
// share that resource.
func (m *Manager) grantForResource(ctx context.Context, target string) (*Grant, error) {
	grants, err := m.grants().List(ctx)
	if err != nil {
		return nil, failure("listing the grant store: %w", err)
	}
	found := resolveGrant(ctx, grants, target, nil)
	if found == nil {
		return nil, nil
	}
	return m.grant(ctx, found.key())
}

// resolveGrant returns the grant whose resource is the longest that covers
// target among those that match, or nil when there is none or when several
// share that resource and none of them supersedes the others.
func resolveGrant(ctx context.Context, grants []Grant, target string, match func(*Grant) bool) *Grant {
	var best []*Grant
	for i := range grants {
		g := &grants[i]
		if !g.Covers(target) || (match != nil && !match(g)) {
			continue
		}
		switch {
		case len(best) == 0 || len(NormalizeResource(g.Resource)) > len(NormalizeResource(best[0].Resource)):
			best = []*Grant{g}
		case len(NormalizeResource(g.Resource)) == len(NormalizeResource(best[0].Resource)):
			best = append(best, g)
		}
	}
	switch {
	case len(best) == 0:
		return nil
	case len(best) == 1:
		return best[0]
	}
	if w := widest(best); w != nil {
		return w
	}
	log.Debug(ctx, "several OAuth grants match; not choosing one", "resource", best[0].Resource)
	return nil
}

// widest returns the grant whose requested scopes include those of every other
// grant in gs, when all of them are for the same issuer, client and resource,
// as after the user signed in again for more scopes; otherwise nil.
func widest(gs []*Grant) *Grant {
	w := gs[0]
	for _, g := range gs[1:] {
		if containsAll(g.Scopes, w.Scopes) {
			w = g
		}
	}
	wk := w.key()
	for _, g := range gs {
		k := g.key()
		if k.Issuer != wk.Issuer || k.ClientID != wk.ClientID || k.Resource != wk.Resource || !containsAll(wk.Scopes, k.Scopes) {
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
func (m *Manager) get(ctx context.Context, key grantKey) (*Grant, error) {
	g, err := m.grants().Get(ctx, key.id())
	if err == nil && g == nil {
		return nil, ErrGrantNotFound
	}
	return g, err
}

func (m *Manager) grant(ctx context.Context, key grantKey) (*Grant, error) {
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
func (m *Manager) refreshRejected(ctx context.Context, key grantKey, token string) (*Grant, error) {
	return m.refresh(ctx, key, true, token)
}

// refresh refreshes the grant for key under the lock when its access token is
// missing or about to expire, or, when rejected is set, when it is still token.
// It returns nil when a new authorization is needed. Once it holds the lock, it
// runs detached from the cancellation of ctx, so a refresh token the server
// rotated is not lost; it does not start when ctx is already done by then.
func (m *Manager) refresh(ctx context.Context, key grantKey, rejected bool, token string) (*Grant, error) {
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
			return nil, m.dropRejected(ctx, key)
		case !rejected && usable:
			return g, nil
		default:
			return nil, nil
		}
	}
	log.Debug(ctx, "refreshing the OAuth access token", "resource", g.Resource)
	refreshed, err := m.tokens().Refresh(ctx, g)
	if isOAuthError(err, "invalid_grant") {
		log.Info(ctx, "the OAuth refresh token was rejected; a new authorization is needed", "resource", g.Resource)
		if err := m.grants().Delete(ctx, key.id()); err != nil {
			return nil, failure("deleting the rejected grant: %w", err)
		}
		return nil, nil
	}
	if err != nil {
		if !rejected && usable {
			log.Warn(ctx, "refreshing the OAuth access token failed; using the current one", "resource", g.Resource, "error", err)
			return g, nil
		}
		return nil, failure("refreshing the access token for %s: %w", g.Resource, err)
	}
	putCtx, cancelPut := context.WithTimeout(context.WithoutCancel(ctx), storeTimeout)
	defer cancelPut()
	if err := m.grants().Put(putCtx, *refreshed); err != nil {
		return nil, failure("storing the refreshed grant: %w", err)
	}
	return refreshed, nil
}

// dropRejected deletes a grant whose access token the agent rejected and that
// can't be refreshed. Kept without a token, it would still be listed and chosen
// over a usable grant for a parent resource.
func (m *Manager) dropRejected(ctx context.Context, key grantKey) error {
	log.Debug(ctx, "the agent rejected the OAuth access token, which can't be refreshed; deleting the grant", "resource", key.Resource)
	if err := m.grants().Delete(ctx, key.id()); err != nil {
		return failure("deleting the rejected grant: %w", err)
	}
	return nil
}

// save stores a new grant. The wait for the lock and the write are detached
// from the cancellation of ctx, so a grant obtained just before the caller gave
// up is still stored.
func (m *Manager) save(ctx context.Context, g *Grant) error {
	if choice, err := selectClient(m.Clients, m.DefaultClientID, g.Endpoints); err == nil && choice.ClientID == g.ClientID {
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
	return m.pruneNarrower(ctx, g.key())
}

// pruneNarrower deletes the grants that key supersedes: those for the same
// issuer, client and resource whose requested scopes key's include, as after
// the user signed in again because the agent asked for more scopes. Left in the
// store, the narrower grant would still be attached to calls that ask for its
// scopes, and the agent would refuse it again. The caller holds the lock.
func (m *Manager) pruneNarrower(ctx context.Context, key grantKey) error {
	grants, err := m.grants().List(ctx)
	if err != nil {
		return failure("listing the grant store: %w", err)
	}
	for _, g := range grants {
		k := g.key()
		if k.equal(key) || k.Issuer != key.Issuer || k.ClientID != key.ClientID || k.Resource != key.Resource || !containsAll(key.Scopes, k.Scopes) {
			continue
		}
		if err := m.grants().Delete(ctx, k.id()); err != nil {
			return failure("deleting a grant the new one supersedes: %w", err)
		}
	}
	return nil
}

// Revoke revokes g at its authorization server (RFC 7009) and removes it from
// the store. The grant may come from Store.List without its tokens: they are
// read from the store. The grant is removed even when revocation fails, so the
// client stops using it either way; the error then says why the server did not
// revoke it, and is [ErrNoRevocationEndpoint] when the server does not support
// revocation. A grant without tokens, such as one the server already
// invalidated, is removed with an error saying there was nothing to revoke.
func (m *Manager) Revoke(ctx context.Context, g Grant) error {
	key := g.key()
	stored, err := m.get(ctx, key)
	switch {
	case err == nil:
		g = *stored
	case !errors.Is(err, ErrGrantNotFound):
		return failure("reading the grant store: %w", err)
	}
	revokeErr := m.tokens().Revoke(ctx, &g)
	// The grant is removed even when the caller's context ran out during the
	// revocation, so the wait for the lock is detached from it.
	unlock, err := m.lock(context.WithoutCancel(ctx))
	if err != nil {
		return err
	}
	defer unlock()
	dctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), detachedTimeout)
	defer cancel()
	if err := m.grants().Delete(dctx, key.id()); err != nil {
		return failure("deleting the grant: %w", err)
	}
	return revokeErr
}

// lock takes the manager's own lock, then Locker if set. It waits until ctx is
// done, or at most lockTimeout.
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
	if m.Locker == nil {
		return func() { <-sem }, nil
	}
	unlock, err := m.Locker.Lock(ctx)
	if err != nil {
		<-sem
		return nil, failure("locking the grant store: %w", err)
	}
	return func() {
		unlock()
		<-sem
	}, nil
}

// checkHTTPTokenDestination refuses to send a bearer token over HTTP where it
// would travel in cleartext: it allows https and http to a loopback host.
func checkHTTPTokenDestination(target string) error {
	if err := checkSecureURL(target); err != nil {
		return fmt.Errorf("refusing to send an OAuth token to %s over plain HTTP", redactURL(target))
	}
	return nil
}

// redactURL returns raw without its query and with the password masked, for
// messages.
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "the URL"
	}
	u.RawQuery, u.Fragment = "", ""
	return u.Redacted()
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
