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
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/log"
)

// Requirement describes the grant a request needs: the authorization server to
// get it from, the resource it is for and the scopes it must cover.
type Requirement struct {
	// Endpoints of the authorization server.
	Endpoints Endpoints
	// Resource is the normalized agent URL, sent as the RFC 8707 resource indicator.
	Resource string
	// Scopes requested by the agent; empty when it named none.
	Scopes []string
	// PreferDevice is set when the OAuth scheme used declares the device code flow.
	PreferDevice bool
	// Optional is set when the agent card also accepts calls with other
	// credentials, such as an API key, or with none, so a missing grant is not an
	// error: the request goes out without one and the agent decides.
	Optional bool
}

// requirementFromCard returns the requirement a card declares for calls to
// baseURL, or nil when it declares none the client can meet. The first
// securityRequirements option whose only scheme is a usable oauth2 or
// openIdConnect scheme is used. Options whose scheme can't be resolved are
// skipped, and their errors returned only when no option is usable. Options
// that need no OAuth, wherever they are in the list, make the requirement
// optional. The endpoints of a flow declared inline have no issuer; resolve,
// when given, finds it (see resolveInlineIssuer), and an option it fails for is
// skipped like other unusable ones. When no option is usable and one failed for
// want of a client, that [*NoClientError] or [*AmbiguousClientError] is returned.
// Metadata URLs and endpoints on loopback hosts are refused unless baseURL is on
// one too, and the metadata of at most maxCardMetadataDocuments schemes is
// fetched.
func (f fetcher) requirementFromCard(ctx context.Context, card *a2a.AgentCard, baseURL string, resolve func(context.Context, Endpoints) (Endpoints, error)) (*Requirement, error) {
	if card == nil {
		return nil, nil
	}
	f = f.forAgent(baseURL)
	other := acceptsOtherCredentials(card)
	var errs []error
	var clientErr error
	tried := make(map[a2a.SecuritySchemeName]bool)
	fetched := 0
	for _, option := range card.SecurityRequirements {
		if len(option) != 1 {
			continue
		}
		for name, scopes := range option {
			scheme, ok := card.SecuritySchemes[name]
			if !ok || tried[name] {
				continue
			}
			tried[name] = true
			if schemeMetadataURL(scheme) != "" {
				if fetched == maxCardMetadataDocuments {
					errs = append(errs, fmt.Errorf("security scheme %q: not fetching the metadata of more than %d schemes", name, maxCardMetadataDocuments))
					continue
				}
				fetched++
			}
			endpoints, preferDevice, ok, err := f.schemeEndpoints(ctx, scheme)
			if err != nil {
				errs = append(errs, fmt.Errorf("security scheme %q: %w", name, err))
				continue
			}
			if !ok {
				continue
			}
			if endpoints.Issuer == "" && resolve != nil {
				resolved, err := resolve(ctx, endpoints)
				if err != nil {
					var nc *NoClientError
					var ac *AmbiguousClientError
					if clientErr == nil && (errors.As(err, &nc) || errors.As(err, &ac)) {
						clientErr = err
					}
					errs = append(errs, fmt.Errorf("security scheme %q: %w", name, err))
					continue
				}
				endpoints = resolved
			}
			logSkipped(ctx, errs)
			return &Requirement{
				Endpoints:    endpoints,
				Resource:     NormalizeResource(baseURL),
				Scopes:       append([]string(nil), scopes...),
				PreferDevice: preferDevice,
				Optional:     other,
			}, nil
		}
	}
	if len(errs) > 0 && !other {
		if clientErr != nil {
			logSkipped(ctx, errs)
			return nil, clientErr
		}
		return nil, errors.Join(errs...)
	}
	logSkipped(ctx, errs)
	return nil, nil
}

func logSkipped(ctx context.Context, errs []error) {
	for _, err := range errs {
		log.Warn(ctx, "skipping an unusable OAuth security scheme of an agent card", "error", err)
	}
}

// acceptsOtherCredentials reports whether a card offers a securityRequirements
// option that needs no OAuth scheme: no credentials at all, or only schemes such
// as an API key or HTTP authentication.
func acceptsOtherCredentials(card *a2a.AgentCard) bool {
	for _, option := range card.SecurityRequirements {
		oauth, defined := false, true
		for name := range option {
			switch card.SecuritySchemes[name].(type) {
			case nil:
				defined = false
			case a2a.OAuth2SecurityScheme, a2a.OpenIDConnectSecurityScheme:
				oauth = true
			}
		}
		if defined && !oauth {
			return true
		}
	}
	return false
}

// schemeEndpoints resolves the endpoints of an oauth2 or openIdConnect scheme.
// It reports ok=false for schemes and flows the client can't use. The endpoints
// of an oauth2 flow declared inline have no issuer.
func (f fetcher) schemeEndpoints(ctx context.Context, scheme a2a.SecurityScheme) (Endpoints, bool, bool, error) {
	switch s := scheme.(type) {
	case a2a.OAuth2SecurityScheme:
		inline, device, ok := flowEndpoints(s.Flows)
		if !ok {
			return Endpoints{}, false, false, nil
		}
		if s.Oauth2MetadataURL == "" {
			return inline, device, true, errors.Join(inline.validateInline(), f.checkEndpoints(inline))
		}
		m, err := f.metadataAt(ctx, s.Oauth2MetadataURL)
		if err != nil {
			return Endpoints{}, false, false, err
		}
		e, err := m.usableEndpoints()
		return e, device || e.Authorization == "", true, errors.Join(err, f.checkEndpoints(e))
	case a2a.OpenIDConnectSecurityScheme:
		if s.OpenIDConnectURL == "" {
			return Endpoints{}, false, false, nil
		}
		m, err := f.metadataAt(ctx, s.OpenIDConnectURL)
		if err != nil {
			return Endpoints{}, false, false, err
		}
		e, err := m.usableEndpoints()
		return e, e.Authorization == "", true, errors.Join(err, f.checkEndpoints(e))
	default:
		return Endpoints{}, false, false, nil
	}
}

// schemeMetadataURL returns the authorization server metadata URL of an oauth2
// or openIdConnect scheme, or "" when it names none.
func schemeMetadataURL(scheme a2a.SecurityScheme) string {
	switch s := scheme.(type) {
	case a2a.OAuth2SecurityScheme:
		return s.Oauth2MetadataURL
	case a2a.OpenIDConnectSecurityScheme:
		return s.OpenIDConnectURL
	}
	return ""
}

// flowEndpoints returns the endpoints of an authorization code or device code
// flow, and whether it is the device code flow. It reports ok=false for other
// flows. Cards converted from gRPC carry pointers to flows.
func flowEndpoints(flows a2a.OAuthFlows) (e Endpoints, device, ok bool) {
	switch flow := flows.(type) {
	case a2a.AuthorizationCodeOAuthFlow:
		return Endpoints{Authorization: flow.AuthorizationURL, Token: flow.TokenURL}, false, true
	case *a2a.AuthorizationCodeOAuthFlow:
		if flow != nil {
			return flowEndpoints(*flow)
		}
	case a2a.DeviceCodeOAuthFlow:
		return Endpoints{DeviceAuthorization: flow.DeviceAuthorizationURL, Token: flow.TokenURL}, true, true
	case *a2a.DeviceCodeOAuthFlow:
		if flow != nil {
			return flowEndpoints(*flow)
		}
	}
	return Endpoints{}, false, false
}

// resolveInlineIssuer finds the authorization server behind endpoints an agent
// card declares inline, which name no issuer. It fetches the metadata of each
// registration's authority and adopts the endpoints of the first whose token
// endpoint is the card's, so the issuer, the revocation endpoint and CIMD
// support come from the server. An authorization or device authorization
// endpoint both declare must be the same; one only the card declares is kept
// only on the origin of the server's issuer or token endpoint.
//
// A registration whose authority is an origin and publishes no metadata, as
// GitHub's doesn't, is used for a card whose endpoints are all on that origin.
//
// With cimd set, when no registration matches, it also looks for the metadata of
// an issuer at the token endpoint's URL with path segments removed, and adopts
// it only when it names the card's token endpoint and supports Client ID
// Metadata Documents, so the default client can be used. The issuer is never
// taken from the token URL without that metadata or a registration.
//
// It returns [*NoClientError] naming the origin of the token endpoint when no
// server is found, and the fetch errors when the metadata of a registration
// could not be fetched.
func (f fetcher) resolveInlineIssuer(ctx context.Context, regs []Registration, e Endpoints, cimd bool) (Endpoints, error) {
	var failed []error
	var bare []Registration
	for _, r := range regs {
		m, err := f.serverMetadata(ctx, r.Authority)
		if errors.Is(err, errNoServerMetadata) {
			bare = append(bare, r)
			continue
		}
		if err != nil {
			failed = append(failed, err)
			continue
		}
		resolved, err := adoptInline(m, e)
		if err != nil {
			log.Debug(ctx, "skipping a client registration for an inline OAuth flow", "authority", r.Authority, "error", err)
			continue
		}
		return resolved, nil
	}
	for _, r := range bare {
		if resolved, ok := bareInline(r, e); ok {
			return resolved, nil
		}
	}
	if cimd {
		f := f.forAgent(e.Token)
		for _, issuer := range inlineIssuerCandidates(e.Token) {
			m, err := f.serverMetadata(ctx, issuer)
			if err != nil {
				continue
			}
			resolved, err := adoptInline(m, e)
			if err == nil && !resolved.CIMDSupported {
				err = errors.New("the server does not support Client ID Metadata Documents")
			}
			if err == nil {
				err = f.checkEndpoints(resolved)
			}
			if err != nil {
				log.Debug(ctx, "not using the default client for an inline OAuth flow", "issuer", issuer, "error", err)
				continue
			}
			return resolved, nil
		}
	}
	if len(failed) > 0 {
		return Endpoints{}, fmt.Errorf("finding the authorization server of an inline OAuth flow: %w", errors.Join(failed...))
	}
	return Endpoints{}, &NoClientError{Issuers: []string{origin(e.Token)}}
}

// adoptInline returns the endpoints of m for an inline flow declaring e: m must
// name e's token endpoint and agree with its other endpoints. An endpoint only e
// declares is kept when it is on the origin of m's issuer or token endpoint, so
// a card can't send a flow of a trusted server, and the user's client ID with
// it, to a host of its own.
func adoptInline(m *authServerMetadata, e Endpoints) (Endpoints, error) {
	resolved := m.Endpoints()
	if !sameEndpoint(resolved.Token, e.Token) ||
		!compatibleEndpoint(e.Authorization, resolved.Authorization) ||
		!compatibleEndpoint(e.DeviceAuthorization, resolved.DeviceAuthorization) {
		return Endpoints{}, fmt.Errorf("the endpoints of %s are not the card's", resolved.Issuer)
	}
	resolved.Authorization = cmp.Or(resolved.Authorization, serverEndpoint(resolved, e.Authorization))
	resolved.DeviceAuthorization = cmp.Or(resolved.DeviceAuthorization, serverEndpoint(resolved, e.DeviceAuthorization))
	if !m.supportsS256() {
		if resolved.DeviceAuthorization == "" {
			return Endpoints{}, fmt.Errorf("authorization server %s does not support PKCE with S256", m.Issuer)
		}
		resolved.Authorization = ""
	}
	if err := resolved.Validate(); err != nil {
		return Endpoints{}, err
	}
	return resolved, nil
}

// serverEndpoint returns an endpoint a card declares and the server's metadata
// doesn't, when it is below the issuer or next to the token endpoint, and ""
// otherwise. On a host that serves several issuers below paths, such as
// Keycloak realms, a card can't borrow another issuer's endpoint.
func serverEndpoint(server Endpoints, card string) string {
	if card == "" {
		return ""
	}
	if resourceCovers(server.Issuer, card) || resourceCovers(parentURL(server.Token), card) {
		return card
	}
	return ""
}

// parentURL returns rawURL without its last path segment.
func parentURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	u.Path = u.Path[:strings.LastIndex(u.Path, "/")+1]
	u.RawPath, u.RawQuery, u.Fragment = "", "", ""
	return u.String()
}

// bareInline returns the endpoints a card declares inline with r's authority as
// the issuer, for a registration whose server publishes no metadata. The
// authority must be an origin, and every endpoint must be on it.
func bareInline(r Registration, e Endpoints) (Endpoints, bool) {
	o := origin(r.Authority)
	if !sameIssuer(r.Authority, o) {
		return Endpoints{}, false
	}
	for _, u := range []string{e.Authorization, e.Token, e.DeviceAuthorization} {
		if u != "" && origin(u) != o {
			return Endpoints{}, false
		}
	}
	resolved := e
	resolved.Issuer = r.Authority
	if resolved.Validate() != nil {
		return Endpoints{}, false
	}
	return resolved, true
}

// inlineIssuerCandidates returns the issuers that may publish metadata for a
// token endpoint: its URL with the last path segment removed, then each
// ancestor down to the origin, for at most maxResourcePathSegments segments.
func inlineIssuerCandidates(tokenURL string) []string {
	u, err := url.Parse(tokenURL)
	if err != nil || u.Host == "" {
		return nil
	}
	base := u.Scheme + "://" + u.Host
	var segments []string
	if p := strings.Trim(u.EscapedPath(), "/"); p != "" {
		segments = strings.Split(p, "/")
	}
	segments = segments[:min(max(len(segments)-1, 0), maxResourcePathSegments)]
	candidates := make([]string, 0, len(segments)+1)
	for i := len(segments); i > 0; i-- {
		candidates = append(candidates, base+"/"+strings.Join(segments[:i], "/"))
	}
	return append(candidates, base)
}

// compatibleEndpoint reports whether an endpoint declared by a card agrees with
// the one in the authorization server metadata, when both are given.
func compatibleEndpoint(card, metadata string) bool {
	return card == "" || metadata == "" || sameEndpoint(card, metadata)
}

// challengeTarget is what a Bearer challenge tells about a 401 response.
type challengeTarget struct {
	resource *protectedResourceMetadata
	scopes   []string
}

// discoverFromChallenge follows a 401 to the resource's metadata. It returns
// errNoResourceMetadata when the resource doesn't advertise any. Unless target
// is on a loopback host, it refuses a loopback resource_metadata URL and drops
// loopback authorization servers. It keeps at most maxAuthorizationServers of
// them.
func (f fetcher) discoverFromChallenge(ctx context.Context, target *url.URL, chs []Challenge) (*challengeTarget, error) {
	f = f.forAgent(target.String())
	bc, _ := bearerChallenge(chs)
	prm, err := f.resourceMetadata(ctx, target, bc.Params["resource_metadata"])
	if err != nil {
		return nil, err
	}
	if prm.AuthorizationServers, err = f.authorizationServers(ctx, prm); err != nil {
		return nil, err
	}
	return &challengeTarget{resource: prm, scopes: splitScopes(bc.Params["scope"])}, nil
}

// authorizationServers returns the authorization servers of prm to try: the
// first maxAuthorizationServers that f does not refuse.
func (f fetcher) authorizationServers(ctx context.Context, prm *protectedResourceMetadata) ([]string, error) {
	var servers []string
	var errs []error
	for _, s := range prm.AuthorizationServers {
		if err := f.checkURL(s); err != nil {
			log.Info(ctx, "skipping an authorization server of protected resource metadata", "resource", prm.Resource, "error", err)
			errs = append(errs, err)
			continue
		}
		servers = append(servers, s)
	}
	if len(servers) == 0 {
		return nil, fmt.Errorf("resource metadata for %s lists no usable authorization server: %w", prm.Resource, errors.Join(errs...))
	}
	if len(servers) > maxAuthorizationServers {
		log.Info(ctx, "trying only the first authorization servers of protected resource metadata", "resource", prm.Resource, "skipped", servers[maxAuthorizationServers:])
		servers = servers[:maxAuthorizationServers]
	}
	return servers, nil
}
