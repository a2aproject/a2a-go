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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/a2aproject/a2a-go/v2/log"
)

const maxMetadataBytes = 1 << 20

// authServerMetadata is the subset of RFC 8414 Authorization Server Metadata and
// OpenID Connect Discovery the client uses.
type authServerMetadata struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint,omitempty"`
	TokenEndpoint                     string   `json:"token_endpoint,omitempty"`
	DeviceAuthorizationEndpoint       string   `json:"device_authorization_endpoint,omitempty"`
	RevocationEndpoint                string   `json:"revocation_endpoint,omitempty"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported,omitempty"`
	ClientIDMetadataDocumentSupported bool     `json:"client_id_metadata_document_supported,omitempty"`
}

// protectedResourceMetadata is the subset of RFC 9728 Protected Resource Metadata the client uses.
type protectedResourceMetadata struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers,omitempty"`
	ScopesSupported      []string `json:"scopes_supported,omitempty"`
}

// Endpoints are the authorization server endpoints a grant is obtained,
// refreshed and revoked with.
type Endpoints struct {
	// Issuer is the issuer identifier of the authorization server.
	Issuer string `json:"issuer"`
	// Authorization is the authorization endpoint of the authorization code
	// flow, opened in the user's browser.
	Authorization string `json:"authorization,omitempty"`
	// Token is the token endpoint, where codes are exchanged and grants
	// refreshed.
	Token string `json:"token"`
	// DeviceAuthorization is the device authorization endpoint of the device
	// code flow (RFC 8628).
	DeviceAuthorization string `json:"device_authorization,omitempty"`
	// Revocation is the token revocation endpoint (RFC 7009), if the server
	// has one.
	Revocation string `json:"revocation,omitempty"`
	// CIMDSupported reports that the server accepts Client ID Metadata Document URLs as client IDs.
	CIMDSupported bool `json:"cimd_supported,omitempty"`
}

// Endpoints returns the endpoints the metadata lists.
func (m *authServerMetadata) Endpoints() Endpoints {
	return Endpoints{
		Issuer:              m.Issuer,
		Authorization:       m.AuthorizationEndpoint,
		Token:               m.TokenEndpoint,
		DeviceAuthorization: m.DeviceAuthorizationEndpoint,
		Revocation:          m.RevocationEndpoint,
		CIMDSupported:       m.ClientIDMetadataDocumentSupported,
	}
}

// Validate checks that the endpoints name an issuer and include a token endpoint
// and an authorization or device authorization endpoint, all served over https
// (http only on loopback).
func (e Endpoints) Validate() error {
	if e.Issuer == "" {
		return errors.New("authorization server has no issuer")
	}
	return e.check("authorization server " + e.Issuer)
}

// validateInline checks endpoints an agent card declares inline like Validate,
// except that they have no issuer.
func (e Endpoints) validateInline() error {
	return e.check("inline flow")
}

func (e Endpoints) check(server string) error {
	if e.Token == "" {
		return fmt.Errorf("%s has no token endpoint", server)
	}
	for _, u := range []string{e.Authorization, e.Token, e.DeviceAuthorization, e.Revocation} {
		if u == "" {
			continue
		}
		if err := checkSecureURL(u); err != nil {
			return fmt.Errorf("%s: %w", server, err)
		}
	}
	if e.Authorization == "" && e.DeviceAuthorization == "" {
		return fmt.Errorf("%s has neither an authorization nor a device authorization endpoint", server)
	}
	return nil
}

// fetcher makes the client's own requests to metadata and authorization server
// endpoints. Its client never carries agent credentials.
type fetcher struct {
	client *http.Client
	// noLoopback refuses URLs on loopback hosts; see forAgent.
	noLoopback bool
}

// forAgent returns a fetcher for the metadata an agent points to. Unless the
// agent is on a loopback host itself, it refuses loopback URLs, redirects
// included, so an agent can't make the client probe the user's local ports.
func (f fetcher) forAgent(agent string) fetcher {
	f.noLoopback = !isLoopbackURL(agent)
	return f
}

// checkURL checks a URL to fetch: https, or http on a loopback host, and no
// loopback host when f refuses them.
func (f fetcher) checkURL(rawURL string) error {
	if err := checkSecureURL(rawURL); err != nil {
		return err
	}
	if f.noLoopback && isLoopbackURL(rawURL) {
		return fmt.Errorf("refusing loopback URL %q for an agent that is not on a loopback host", rawURL)
	}
	return nil
}

// checkEndpoints refuses authorization server endpoints on loopback hosts when
// f refuses loopback URLs, so an agent can't make the client send its token and
// device authorization requests to the user's local ports.
func (f fetcher) checkEndpoints(e Endpoints) error {
	if !f.noLoopback {
		return nil
	}
	for _, u := range []string{e.Authorization, e.Token, e.DeviceAuthorization, e.Revocation} {
		if u != "" && isLoopbackURL(u) {
			return fmt.Errorf("refusing loopback endpoint %q for an agent that is not on a loopback host", u)
		}
	}
	return nil
}

// getJSON fetches a JSON document over https (or loopback http). With
// sameOrigin, redirects are only followed within the URL's origin.
func (f fetcher) getJSON(ctx context.Context, rawURL string, sameOrigin bool, out any) error {
	if err := f.checkURL(rawURL); err != nil {
		return err
	}
	client := *f.client
	start := origin(rawURL)
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		if sameOrigin && origin(req.URL.String()) != start {
			return fmt.Errorf("redirect from %s to another origin %s refused", rawURL, req.URL)
		}
		return f.checkURL(req.URL.String())
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Error(ctx, "failed to close http response body", err)
		}
	}()
	if resp.StatusCode != http.StatusOK {
		return &statusError{url: rawURL, status: resp.StatusCode}
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxMetadataBytes)).Decode(out); err != nil {
		mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
		return &parseError{url: rawURL, err: err, notJSON: mediaType != "" && mediaType != "application/json" && !strings.HasSuffix(mediaType, "+json")}
	}
	return nil
}

type statusError struct {
	url    string
	status int
}

func (e *statusError) Error() string {
	return fmt.Sprintf("GET %s: HTTP %d", e.url, e.status)
}

type parseError struct {
	url string
	err error
	// notJSON is set when the response declared a content type other than JSON,
	// such as an HTML page.
	notJSON bool
}

func (e *parseError) Error() string {
	return fmt.Sprintf("parsing %s: %v", e.url, e.err)
}

func (e *parseError) Unwrap() error { return e.err }

// serverMetadata fetches the metadata of an issuer from the first of the
// locations serverMetadataURLs lists that serves a document naming the issuer.
// A location may serve another issuer's document, as when a host answers the
// RFC 8414 location with its own metadata whatever the path.
func (f fetcher) serverMetadata(ctx context.Context, issuer string) (*authServerMetadata, error) {
	urls := serverMetadataURLs(issuer)
	if len(urls) == 0 {
		return nil, fmt.Errorf("invalid authorization server identifier %q", issuer)
	}
	var errs []error
	absent := true
	for _, u := range urls {
		var m authServerMetadata
		if err := f.getJSON(ctx, u, false, &m); err != nil {
			errs = append(errs, err)
			if !notPublished(err) {
				absent = false
			}
			continue
		}
		if !sameIssuer(m.Issuer, issuer) {
			errs = append(errs, fmt.Errorf("metadata at %s names issuer %q, want %q", u, m.Issuer, issuer))
			absent = false
			continue
		}
		return &m, nil
	}
	if absent {
		return nil, fmt.Errorf("fetching metadata of %s: %w: %w", issuer, errNoServerMetadata, errors.Join(errs...))
	}
	return nil, fmt.Errorf("fetching metadata of %s: %w", issuer, errors.Join(errs...))
}

// notPublished reports whether a metadata fetch failed because nothing is
// published there: 404 or 410, or a response declared as something other than
// JSON. Rate limits, authorization errors, server errors and truncated JSON are
// failures, not absence.
func notPublished(err error) bool {
	var se *statusError
	if errors.As(err, &se) {
		return se.status == http.StatusNotFound || se.status == http.StatusGone
	}
	var pe *parseError
	return errors.As(err, &pe) && pe.notJSON
}

// errNoServerMetadata is returned when every location of an issuer's metadata
// answers 404 or 410, or with a body declared as something other than JSON.
var errNoServerMetadata = errors.New("the authorization server publishes no metadata")

// metadataAt fetches metadata from a URL given by an agent card and checks that
// the URL is a well-known location of the issuer it names.
func (f fetcher) metadataAt(ctx context.Context, rawURL string) (*authServerMetadata, error) {
	var m authServerMetadata
	if err := f.getJSON(ctx, rawURL, false, &m); err != nil {
		return nil, err
	}
	for _, u := range serverMetadataURLs(m.Issuer) {
		if strings.TrimSuffix(u, "/") == strings.TrimSuffix(rawURL, "/") {
			return &m, nil
		}
	}
	return nil, fmt.Errorf("metadata at %s names issuer %q, which does not publish metadata there", rawURL, m.Issuer)
}

// resourceMetadata fetches the Protected Resource Metadata for a request that got
// a 401: from the challenge's resource_metadata, or else from the well-known
// locations. A well-known location that answers with a status other than 200 or
// with a body that isn't JSON is taken to publish nothing, since gateways often
// answer every path with an error or an HTML page. A document the challenge
// names describes the request URL, or a resource within its metadataScope. It
// returns errNoResourceMetadata when the resource publishes no metadata.
func (f fetcher) resourceMetadata(ctx context.Context, target *url.URL, metadataURL string) (*protectedResourceMetadata, error) {
	candidates := []string{metadataURL}
	explicit := metadataURL != ""
	if !explicit {
		candidates = resourceMetadataURLs(target)
	}
	var errs []error
	for _, u := range candidates {
		var m protectedResourceMetadata
		err := f.getJSON(ctx, u, true, &m)
		if err != nil {
			var se *statusError
			var pe *parseError
			if !explicit && (errors.As(err, &se) || errors.As(err, &pe)) {
				continue
			}
			errs = append(errs, err)
			continue
		}
		if m.Resource == "" {
			return nil, fmt.Errorf("resource metadata at %s has no resource", u)
		}
		// RFC 9728 §3.3 wants the resource to be the URL the metadata location was
		// derived from. Any resource that covers the target is accepted instead, so
		// one document can describe an agent whose requests go to paths below it;
		// the origin must still match.
		if !resourceCovers(m.Resource, target.String()) {
			return nil, fmt.Errorf("resource metadata at %s describes %s, not %s", u, m.Resource, target)
		}
		// The agent picks the location in the challenge, so a document it serves
		// under its own path must not claim a resource above that path, such as
		// the whole origin of a host it shares with other agents.
		if explicit && NormalizeResource(m.Resource) != NormalizeResource(target.String()) && !resourceCovers(metadataScope(u), m.Resource) {
			return nil, fmt.Errorf("resource metadata at %s may not describe %s", u, m.Resource)
		}
		if len(m.AuthorizationServers) == 0 {
			return nil, fmt.Errorf("resource metadata at %s lists no authorization servers", u)
		}
		return &m, nil
	}
	if len(errs) == 0 {
		return nil, errNoResourceMetadata
	}
	return nil, errors.Join(errs...)
}

// metadataScope returns the URL below which a resource metadata document a
// challenge points to may describe resources other than the request URL: the
// path before a /.well-known/ segment, which is the origin for the host's own
// well-known directory, or else the document's directory.
func metadataScope(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return ""
	}
	p := u.EscapedPath()
	if i := strings.Index(p+"/", "/.well-known/"); i >= 0 {
		p = p[:i]
	} else {
		p = p[:strings.LastIndex(p, "/")+1]
	}
	return u.Scheme + "://" + u.Host + p
}

// errNoResourceMetadata is returned when a resource publishes no Protected Resource Metadata.
var errNoResourceMetadata = errors.New("no protected resource metadata")
