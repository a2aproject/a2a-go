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
	"net/url"
)

// ClientSource tells where the client used with an authorization server came
// from.
type ClientSource string

// Client sources reported by [ClientChoice].
const (
	// ClientSourceConfig is a configured [Registration].
	ClientSourceConfig ClientSource = "config"
	// ClientSourceCIMD is the default client: a Client ID Metadata Document URL
	// used with a server that supports them and has no registration.
	ClientSourceCIMD ClientSource = "cimd-default"
)

// Registration is a client registration from configuration.
type Registration struct {
	// Authority is the issuer identifier of the authorization server.
	Authority string `json:"authority"`
	// ClientID is a pre-registered client ID or a Client ID Metadata Document URL.
	ClientID string `json:"client_id"`
	// Scopes are requested when the agent names none.
	Scopes []string `json:"scopes,omitempty"`
	// OmitResource leaves the RFC 8707 resource indicator out of the requests to
	// the authorization server, for servers that reject it, such as the
	// Microsoft identity platform when the resource is not an application ID URI.
	OmitResource bool `json:"omit_resource,omitempty"`
}

// ClientChoice is the client used with an authorization server.
type ClientChoice struct {
	Registration
	// Source is ClientSourceConfig or ClientSourceCIMD.
	Source ClientSource
}

// Validate checks that the registration names an authority, served over https (http
// only on loopback), and a client ID.
func (r Registration) Validate() error {
	if r.Authority == "" || r.ClientID == "" {
		return fmt.Errorf("invalid client registration %+v: authority and client ID are required", r)
	}
	if err := checkSecureURL(r.Authority); err != nil {
		return fmt.Errorf("invalid client registration authority: %w", err)
	}
	return nil
}

// selectClient picks the registration for an authorization server, falling back
// to defaultClientID, a Client ID Metadata Document URL, when the server supports
// Client ID Metadata Documents. An empty defaultClientID disables the fallback.
// It returns [*AmbiguousClientError] when several registrations match, and
// [*NoClientError] when none can be used.
func selectClient(regs []Registration, defaultClientID string, e Endpoints) (ClientChoice, error) {
	if err := checkCIMDClientID(defaultClientID); err != nil {
		return ClientChoice{}, err
	}
	var matches []Registration
	for _, r := range regs {
		if err := r.Validate(); err != nil {
			return ClientChoice{}, err
		}
		if sameIssuer(r.Authority, e.Issuer) {
			matches = append(matches, r)
		}
	}
	switch {
	case len(matches) == 1:
		return ClientChoice{Registration: matches[0], Source: ClientSourceConfig}, nil
	case len(matches) > 1:
		ids := make([]string, len(matches))
		for i, m := range matches {
			ids[i] = m.ClientID
		}
		return ClientChoice{}, &AmbiguousClientError{Issuer: e.Issuer, ClientIDs: ids}
	case e.CIMDSupported && defaultClientID != "":
		return ClientChoice{Registration: Registration{Authority: e.Issuer, ClientID: defaultClientID}, Source: ClientSourceCIMD}, nil
	default:
		return ClientChoice{}, &NoClientError{Issuers: []string{e.Issuer}}
	}
}

// checkCIMDClientID checks that a default client ID, when set, is a Client ID
// Metadata Document URL: https with a host.
func checkCIMDClientID(clientID string) error {
	if clientID == "" {
		return nil
	}
	u, err := url.Parse(clientID)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return fmt.Errorf("invalid default client ID %q: want the https URL of a Client ID Metadata Document", clientID)
	}
	return nil
}
