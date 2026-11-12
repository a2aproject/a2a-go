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
	"fmt"
	"strings"
)

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
