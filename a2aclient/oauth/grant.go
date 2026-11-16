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
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strconv"
	"strings"
	"time"
)

// refreshSkew is how long before expiry a token is refreshed.
const refreshSkew = time.Minute

// maxLifetime caps the lifetimes and intervals an authorization server names, so
// absurd values neither overflow nor keep a client waiting for years.
const maxLifetime = 365 * 24 * time.Hour

// grantKey identifies a grant: the authorization server, the client, the
// resource and the requested scopes, normalized.
type grantKey struct {
	Issuer   string
	ClientID string
	Resource string
	Scopes   []string
}

// newGrantKey builds a key with normalized resource and sorted, deduplicated scopes.
func newGrantKey(issuer, clientID, resource string, scopes []string) grantKey {
	return grantKey{
		Issuer:   strings.TrimSuffix(issuer, "/"),
		ClientID: clientID,
		Resource: NormalizeResource(resource),
		Scopes:   normalizeScopes(scopes),
	}
}

func normalizeScopes(scopes []string) []string {
	s := slices.Clone(scopes)
	slices.Sort(s)
	s = slices.Compact(s)
	if len(s) == 0 {
		return nil
	}
	return s
}

// id is a stable identifier of the key. Two keys have the same id exactly when
// they are equal.
func (k grantKey) id() string {
	fields := []string{strconv.Quote(k.Issuer), strconv.Quote(k.ClientID), strconv.Quote(k.Resource)}
	for _, s := range k.Scopes {
		fields = append(fields, strconv.Quote(s))
	}
	sum := sha256.Sum256([]byte(strings.Join(fields, "\n")))
	return hex.EncodeToString(sum[:16])
}

// equal reports whether two keys identify the same grant.
func (k grantKey) equal(o grantKey) bool {
	return k.Issuer == o.Issuer && k.ClientID == o.ClientID && k.Resource == o.Resource && slices.Equal(k.Scopes, o.Scopes)
}

// Grant is a stored authorization: the tokens and the endpoints to refresh and
// revoke them with. A grant is identified by its issuer, client ID, resource and
// requested scopes; see [Grant.ID].
type Grant struct {
	// Issuer is the authorization server that issued the grant.
	Issuer string `json:"issuer"`
	// ClientID is the client the grant was obtained with.
	ClientID string `json:"client_id"`
	// Resource is the agent URL the grant is for, the RFC 8707 resource
	// indicator, normalized. The grant also serves the URLs below it; see
	// [Grant.Covers].
	Resource string `json:"resource"`
	// Scopes are the scopes that were requested.
	Scopes []string `json:"scopes,omitempty"`
	// GrantedScopes are the scopes the authorization server reported granting;
	// empty when it did not say, which means the requested ones (RFC 6749 §5.1).
	GrantedScopes []string `json:"granted_scopes,omitempty"`
	// AccessToken is the bearer token sent to the agent. It is empty in grants
	// a [Store] lists without their tokens, and after the agent rejected it.
	AccessToken string `json:"access_token,omitempty"`
	// RefreshToken obtains a new access token, if the server issued one. The
	// server may rotate it on every refresh.
	RefreshToken string `json:"refresh_token,omitempty"`
	// Refreshable reports that the grant has a refresh token. Unlike
	// RefreshToken, it is set in grants a [Store] lists without their tokens.
	Refreshable bool `json:"refreshable,omitempty"`
	// TokenType is the token type the server reported; only Bearer is accepted.
	TokenType string `json:"token_type,omitempty"`
	// ExpiresAt is when the access token expires; zero when the server did not
	// say.
	ExpiresAt time.Time `json:"expires_at,omitzero"`
	// Endpoints are those of the server that issued the grant, used to refresh
	// and revoke it.
	Endpoints Endpoints `json:"endpoints"`
	// CreatedAt is when the grant was obtained.
	CreatedAt time.Time `json:"created_at,omitzero"`
	// ClientSource tells where the client the grant was obtained with came
	// from, if known.
	ClientSource ClientSource `json:"client_source,omitempty"`
	// OmitResource reports that the grant was obtained without the RFC 8707
	// resource indicator (see [Registration.OmitResource]), so it is refreshed
	// without it too. Resource still names the resource.
	OmitResource bool `json:"omit_resource,omitempty"`
}

// ID identifies the grant in a [Store]: a hash of its issuer, client ID,
// resource and requested scopes, normalized. Grants with the same ID replace
// each other. It stays the same when the grant is refreshed.
func (g Grant) ID() string {
	return g.key().id()
}

// Covers reports whether the grant applies to requests to target: target is on
// the origin of the grant's resource, and its path is the resource's path or
// below it.
func (g Grant) Covers(target string) bool {
	return resourceCovers(g.Resource, target)
}

// key returns the normalized identity of the grant.
func (g *Grant) key() grantKey {
	return newGrantKey(g.Issuer, g.ClientID, g.Resource, g.Scopes)
}

// setKey sets the identity of the grant.
func (g *Grant) setKey(k grantKey) {
	g.Issuer, g.ClientID, g.Resource, g.Scopes = k.Issuer, k.ClientID, k.Resource, slices.Clone(k.Scopes)
}

// clone returns a copy of the grant that shares no memory with it.
func (g *Grant) clone() *Grant {
	c := *g
	c.Scopes = slices.Clone(g.Scopes)
	c.GrantedScopes = slices.Clone(g.GrantedScopes)
	return &c
}

// scopes returns the scopes the grant covers: the granted ones when the server
// named them, the requested ones otherwise.
func (g *Grant) scopes() []string {
	if len(g.GrantedScopes) > 0 {
		return g.GrantedScopes
	}
	return g.Scopes
}

// expired reports whether the access token is past its expiry.
func (g *Grant) expired(now time.Time) bool {
	return !g.ExpiresAt.IsZero() && !now.Before(g.ExpiresAt)
}

// needsRefresh reports whether the access token expires within [refreshSkew].
func (g *Grant) needsRefresh(now time.Time) bool {
	return g.AccessToken == "" || (!g.ExpiresAt.IsZero() && !now.Add(refreshSkew).Before(g.ExpiresAt))
}

// header is the Authorization header value for the grant.
func (g *Grant) header() string {
	return "Bearer " + g.AccessToken
}

// requirement rebuilds the requirement the grant was obtained for.
func (g *Grant) requirement() *Requirement {
	return &Requirement{Endpoints: g.Endpoints, Resource: g.Resource, Scopes: g.Scopes}
}

func (g *Grant) apply(t *tokenResponse, now time.Time) {
	g.AccessToken = t.AccessToken
	if t.RefreshToken != "" {
		g.RefreshToken = t.RefreshToken
	}
	g.Refreshable = g.RefreshToken != ""
	if t.Scope != "" {
		g.GrantedScopes = normalizeScopes(strings.Fields(t.Scope))
	}
	g.TokenType = t.TokenType
	g.ExpiresAt = time.Time{}
	if t.ExpiresIn > 0 {
		g.ExpiresAt = now.Add(t.ExpiresIn.seconds()).UTC()
	}
}
