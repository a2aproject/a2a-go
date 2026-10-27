// Copyright 2025 The A2A Authors
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

package a2aclient

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/log"
)

// ErrCredentialNotFound is returned by [CredentialsService] if a credential for the provided
// (sessionId, scheme) pair was not found.
var ErrCredentialNotFound = errors.New("credential not found")

// SessionID is a client-generated identifier used for scoping auth credentials.
type SessionID string

// Used to store a SessionID in context.Context.
type sessionIDKey struct{}

// AttachSessionID allows callers to attach a session identifier to the request.
// [CallInterceptor] can access this identifier using [SessionIDFrom].
func AttachSessionID(ctx context.Context, sid SessionID) context.Context {
	return context.WithValue(ctx, sessionIDKey{}, sid)
}

// SessionIDFrom allows to get a previously attached session identifier from Context.
func SessionIDFrom(ctx context.Context) (SessionID, bool) {
	sid, ok := ctx.Value(sessionIDKey{}).(SessionID)
	return sid, ok
}

// AuthCredential represents a security-scheme specific credential (eg. a JWT token).
type AuthCredential string

// AuthInterceptor implements [CallInterceptor].
// It uses SessionID provided using [AttachSessionID] to lookup credentials
// and attach them according to the security scheme specified in the agent card.
// Credentials fetching is delegated to [CredentialsService].
type AuthInterceptor struct {
	PassthroughInterceptor
	Service CredentialsService
}

var _ CallInterceptor = (*AuthInterceptor)(nil)

// Before implements the CallInterceptor interface.
// It retrieves credentials for the current session and security requirements,
// and attaches the appropriate authorization parameters (e.g. Bearer token or API Key)
// to the request's ServiceParams.
func (ai *AuthInterceptor) Before(ctx context.Context, req *Request) (context.Context, any, error) {
	if req.Card == nil || req.Card.SecurityRequirements == nil || req.Card.SecuritySchemes == nil {
		return ctx, nil, nil
	}

	sessionID, ok := SessionIDFrom(ctx)
	if !ok {
		return ctx, nil, nil
	}

	for _, requirement := range req.Card.SecurityRequirements {
		for schemeName := range requirement {
			credential, err := ai.Service.Get(ctx, sessionID, schemeName)
			if errors.Is(err, ErrCredentialNotFound) {
				continue
			}
			if err != nil {
				log.Error(ctx, "credentials service error", err)
				continue
			}
			scheme, ok := req.Card.SecuritySchemes[schemeName]
			if !ok {
				continue
			}
			switch v := scheme.(type) {
			case a2a.HTTPAuthSecurityScheme:
				authScheme := strings.TrimSpace(v.Scheme)
				switch {
				case authScheme == "", strings.EqualFold(authScheme, "bearer"):
					authScheme = "Bearer"
				case strings.EqualFold(authScheme, "basic"):
					authScheme = "Basic"
				}
				req.ServiceParams.set("Authorization", fmt.Sprintf("%s %s", authScheme, credential))
				return ctx, nil, nil
			case a2a.OAuth2SecurityScheme:
				req.ServiceParams.set("Authorization", fmt.Sprintf("Bearer %s", credential))
				return ctx, nil, nil
			case a2a.APIKeySecurityScheme:
				// Only the HTTP-based protocol bindings have a notion of query
				// and cookie locations. Other transports (gRPC, custom) convey
				// ServiceParams according to their protocol binding, so for
				// them the credential is attached to ServiceParams under the
				// scheme name regardless of the declared location.
				if !isHTTPProtocolBinding(req) {
					req.ServiceParams.set(v.Name, string(credential))
					return ctx, nil, nil
				}
				switch v.Location {
				case a2a.APIKeySecuritySchemeLocationQuery:
					// Query parameters cannot be expressed via ServiceParams
					// (they become HTTP headers in the HTTP protocol bindings).
					// Attach them to the context instead; HTTP transports append
					// them to the request URL.
					ctx = AttachHTTPQueryParams(ctx, url.Values{v.Name: {string(credential)}})
					return ctx, nil, nil
				case a2a.APIKeySecuritySchemeLocationCookie:
					if err := validateCookiePair(v.Name, string(credential)); err != nil {
						return ctx, nil, err
					}
					pair := v.Name + "=" + string(credential)
					// Clients must not send more than one Cookie header
					// (RFC 6265 §5.4), so merge with any existing cookies
					// into a single header value.
					if existing := req.ServiceParams.Get("Cookie"); len(existing) > 0 {
						pair = strings.Join(existing, "; ") + "; " + pair
					}
					req.ServiceParams.set("Cookie", pair)
					return ctx, nil, nil
				case a2a.APIKeySecuritySchemeLocationHeader, "":
					// An unset location is treated as header for backward compatibility.
					req.ServiceParams.set(v.Name, string(credential))
					return ctx, nil, nil
				default:
					// Do not inject the credential at an unknown location to
					// avoid leaking it to the wrong place on the wire.
					log.Warn(ctx, "unsupported API key security scheme location, credential not attached", "location", v.Location)
				}
			}
		}
	}

	return ctx, nil, nil
}

// isHTTPProtocolBinding reports whether the request will be sent over one of
// the HTTP-based protocol bindings (JSON-RPC or REST) declared in the agent
// card. Requests that cannot be matched to a declared interface are treated
// as non-HTTP so that transports conveying [ServiceParams] (gRPC, custom)
// keep receiving credentials the way they always did.
func isHTTPProtocolBinding(req *Request) bool {
	if req.Card == nil {
		return false
	}
	for _, iface := range req.Card.SupportedInterfaces {
		if iface == nil || iface.URL != req.BaseURL {
			continue
		}
		return iface.ProtocolBinding == a2a.TransportProtocolJSONRPC ||
			iface.ProtocolBinding == a2a.TransportProtocolHTTPJSON
	}
	return false
}

// validateCookiePair checks that the cookie name and credential can be safely
// embedded into a Cookie header value. Values containing cookie separators
// (";" or ","), whitespace, quotes or control characters are rejected because
// they would corrupt the header or allow injecting additional cookies.
// The error message must not include the credential.
func validateCookiePair(name, credential string) error {
	switch {
	case name == "":
		return errors.New("API key cookie name is empty")
	case strings.ContainsAny(name, "=;,\"\\ \t\r\n") || containsControlChars(name):
		return fmt.Errorf("API key cookie name %q contains invalid characters", name)
	case strings.ContainsAny(credential, ";,\"\\ \t\r\n") || containsControlChars(credential):
		return errors.New("API key credential contains characters that are invalid for a cookie value")
	}
	return nil
}

func containsControlChars(s string) bool {
	return strings.ContainsFunc(s, func(r rune) bool {
		return r < 0x20 || r == 0x7f
	})
}

// CredentialsService is used by [AuthInterceptor] for resolving credentials.
type CredentialsService interface {
	// Get retrieves the credential for the given session ID and security scheme name.
	Get(ctx context.Context, sid SessionID, scheme a2a.SecuritySchemeName) (AuthCredential, error)
}

// SessionCredentials is a map of scheme names to auth credentials.
type SessionCredentials map[a2a.SecuritySchemeName]AuthCredential

// InMemoryCredentialsStore implements [CredentialsService].
type InMemoryCredentialsStore struct {
	mu          sync.RWMutex
	credentials map[SessionID]SessionCredentials
}

var _ CredentialsService = (*InMemoryCredentialsStore)(nil)

// NewInMemoryCredentialsStore initializes an in-memory implementation of [CredentialsService].
func NewInMemoryCredentialsStore() *InMemoryCredentialsStore {
	return &InMemoryCredentialsStore{
		credentials: make(map[SessionID]SessionCredentials),
	}
}

// Get retrieves the credential for the given session ID and security scheme name.
func (s *InMemoryCredentialsStore) Get(ctx context.Context, sid SessionID, scheme a2a.SecuritySchemeName) (AuthCredential, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	forSession, ok := s.credentials[sid]
	if !ok {
		return AuthCredential(""), ErrCredentialNotFound
	}

	credential, ok := forSession[scheme]
	if !ok {
		return AuthCredential(""), ErrCredentialNotFound
	}

	return credential, nil
}

// Set stores the credential for the given session ID and security scheme name.
func (s *InMemoryCredentialsStore) Set(sid SessionID, scheme a2a.SecuritySchemeName, credential AuthCredential) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.credentials[sid]; !ok {
		s.credentials[sid] = make(map[a2a.SecuritySchemeName]AuthCredential)
	}
	s.credentials[sid][scheme] = credential
}
