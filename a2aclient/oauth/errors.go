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
	"errors"
	"fmt"
	"strings"
)

var (
	// ErrNoAuthorizationEndpoint is returned when the authorization code flow is
	// started with an authorization server that has no authorization endpoint.
	// The device authorization grant may still be available.
	ErrNoAuthorizationEndpoint = errors.New("the authorization server has no authorization endpoint")
	// ErrNoDeviceAuthorizationEndpoint is returned when the device authorization
	// grant is started with an authorization server that does not support it.
	ErrNoDeviceAuthorizationEndpoint = errors.New("the authorization server has no device authorization endpoint")
	// ErrNoRevocationEndpoint is returned when a grant is revoked at an
	// authorization server that has no revocation endpoint.
	ErrNoRevocationEndpoint = errors.New("the authorization server has no revocation endpoint")
	// ErrGrantNotFound is returned by [Store.Get] when no grant is stored for the
	// key.
	ErrGrantNotFound = errors.New("grant not found")
)

// Error is an OAuth error response from an authorization server (RFC 6749 §5.2,
// RFC 8628 §3.5), or an error returned to the redirect URI (RFC 6749 §4.1.2.1).
type Error struct {
	// Code is the error code, such as "invalid_grant" or "access_denied".
	Code string `json:"error"`
	// Description is the optional human-readable error_description.
	Description string `json:"error_description,omitempty"`
}

// Error implements the error interface.
func (e *Error) Error() string {
	code := cmp.Or(e.Code, "OAuth error response without an error code")
	if e.Description != "" {
		return fmt.Sprintf("%s: %s", code, e.Description)
	}
	return code
}

// isOAuthError reports whether err is an OAuth error response with the given code.
func isOAuthError(err error, code string) bool {
	var oe *Error
	return errors.As(err, &oe) && oe.Code == code
}

// InteractionRequiredError reports that a request needs a grant that has to be
// obtained with user interaction first, for example with
// [Manager.NewAuthorizationRequest] or [Manager.StartDeviceAuthorization].
type InteractionRequiredError struct {
	// Requirement describes where and for what to obtain the grant.
	Requirement *Requirement
	// Reason explains why a stored grant could not be used; empty when there was none.
	Reason string
	// Err is the error the agent's 401 response produced; nil when the request
	// was not sent.
	Err error
}

// Error implements the error interface.
func (e *InteractionRequiredError) Error() string {
	if e.Requirement == nil {
		return "authorization required"
	}
	msg := fmt.Sprintf("%s requires authorization at %s", e.Requirement.Resource, e.Requirement.Endpoints.Issuer)
	if len(e.Requirement.Scopes) > 0 {
		msg += " for scopes " + strings.Join(e.Requirement.Scopes, " ")
	}
	if e.Reason != "" {
		msg += ": " + e.Reason
	}
	return msg
}

// Unwrap returns the error the agent's response produced, so that, for
// example, errors.Is(err, a2a.ErrUnauthenticated) holds for a 401.
func (e *InteractionRequiredError) Unwrap() error { return e.Err }

// FailureError reports a failed step of the authorization process: discovery,
// metadata validation, reading or writing the grant store, taking its lock, or
// token refresh.
type FailureError struct {
	// Err is the cause.
	Err error
}

// Error implements the error interface.
func (e *FailureError) Error() string {
	if e.Err == nil {
		return "OAuth authorization failed"
	}
	return e.Err.Error()
}

// Unwrap returns the cause.
func (e *FailureError) Unwrap() error { return e.Err }

func failure(format string, args ...any) error {
	return &FailureError{Err: fmt.Errorf(format, args...)}
}

// NoClientError reports that no client registration can be used with an
// authorization server.
type NoClientError struct {
	// Issuers are the authorization servers that were tried.
	Issuers []string
}

// Error implements the error interface.
func (e *NoClientError) Error() string {
	return fmt.Sprintf("no client registration matches the authorization server %s", strings.Join(e.Issuers, ", "))
}

// AmbiguousClientError reports that several client registrations name the same
// authorization server, so none can be chosen.
type AmbiguousClientError struct {
	// Issuer is the authorization server.
	Issuer string
	// ClientIDs are the client IDs of the matching registrations.
	ClientIDs []string
}

// Error implements the error interface.
func (e *AmbiguousClientError) Error() string {
	return fmt.Sprintf("several client registrations match the authorization server %s (%s); keep one", e.Issuer, strings.Join(e.ClientIDs, ", "))
}
