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
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestGrantExpiry(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name             string
		grant            Grant
		wantExpired      bool
		wantNeedsRefresh bool
	}{
		{name: "no expiry", grant: Grant{AccessToken: "at"}},
		{name: "valid", grant: Grant{AccessToken: "at", ExpiresAt: now.Add(time.Hour)}},
		{name: "within refresh skew", grant: Grant{AccessToken: "at", ExpiresAt: now.Add(refreshSkew / 2)}, wantNeedsRefresh: true},
		{name: "expired", grant: Grant{AccessToken: "at", ExpiresAt: now}, wantExpired: true, wantNeedsRefresh: true},
		{name: "no access token", grant: Grant{RefreshToken: "rt"}, wantNeedsRefresh: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.grant.expired(now); got != tc.wantExpired {
				t.Fatalf("Grant.expired() = %v, want %v", got, tc.wantExpired)
			}
			if got := tc.grant.needsRefresh(now); got != tc.wantNeedsRefresh {
				t.Fatalf("Grant.needsRefresh() = %v, want %v", got, tc.wantNeedsRefresh)
			}
		})
	}
}

func TestGrantApply(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		grant    Grant
		response tokenResponse
		want     Grant
	}{
		{
			name:     "response without a refresh token keeps it",
			grant:    Grant{AccessToken: "old", RefreshToken: "rt", Refreshable: true, ExpiresAt: now},
			response: tokenResponse{AccessToken: "new", TokenType: "Bearer", ExpiresIn: 60},
			want:     Grant{AccessToken: "new", RefreshToken: "rt", Refreshable: true, TokenType: "Bearer", ExpiresAt: now.Add(time.Minute)},
		},
		{
			name:     "rotated refresh token without expiry",
			grant:    Grant{AccessToken: "old", RefreshToken: "rt", Refreshable: true, ExpiresAt: now},
			response: tokenResponse{AccessToken: "new", RefreshToken: "rt2"},
			want:     Grant{AccessToken: "new", RefreshToken: "rt2", Refreshable: true},
		},
		{
			name:     "first refresh token",
			response: tokenResponse{AccessToken: "at", RefreshToken: "rt"},
			want:     Grant{AccessToken: "at", RefreshToken: "rt", Refreshable: true},
		},
		{
			name:     "no refresh token",
			grant:    Grant{Refreshable: true},
			response: tokenResponse{AccessToken: "at"},
			want:     Grant{AccessToken: "at"},
		},
		{
			name:     "granted scopes",
			response: tokenResponse{AccessToken: "at", Scope: "write  read read"},
			want:     Grant{AccessToken: "at", GrantedScopes: []string{"read", "write"}},
		},
		{
			name:     "granted scopes not reported",
			grant:    Grant{GrantedScopes: []string{"read"}},
			response: tokenResponse{AccessToken: "at"},
			want:     Grant{AccessToken: "at", GrantedScopes: []string{"read"}},
		},
		{
			name:     "absurd expires_in",
			response: tokenResponse{AccessToken: "at", ExpiresIn: 1 << 40},
			want:     Grant{AccessToken: "at", ExpiresAt: now.Add(maxLifetime)},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := tc.grant
			got.apply(&tc.response, now)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("Grant.apply() wrong result (-want +got) diff = %s", diff)
			}
		})
	}
}

func TestGrantHelpers(t *testing.T) {
	t.Parallel()
	g := Grant{
		Key:         NewGrantKey("https://as.example.com", "cli", "https://agent.example.com", []string{"read"}),
		AccessToken: "at",
		Endpoints:   Endpoints{Issuer: "https://as.example.com", Token: "https://as.example.com/token"},
	}
	if got := g.header(); got != "Bearer at" {
		t.Fatalf("Grant.header() = %q, want %q", got, "Bearer at")
	}
	want := &Requirement{Endpoints: g.Endpoints, Resource: "https://agent.example.com", Scopes: []string{"read"}}
	if diff := cmp.Diff(want, g.requirement()); diff != "" {
		t.Fatalf("Grant.requirement() wrong result (-want +got) diff = %s", diff)
	}
	if diff := cmp.Diff([]string{"read"}, g.scopes()); diff != "" {
		t.Fatalf("Grant.scopes() without granted scopes wrong result (-want +got) diff = %s", diff)
	}
	g.GrantedScopes = []string{"profile"}
	if diff := cmp.Diff([]string{"profile"}, g.scopes()); diff != "" {
		t.Fatalf("Grant.scopes() with granted scopes wrong result (-want +got) diff = %s", diff)
	}
}

func TestGrantKeyEqual(t *testing.T) {
	t.Parallel()
	key := func(clientID, resource string, scopes ...string) GrantKey {
		return GrantKey{Issuer: "https://as.example.com", ClientID: clientID, Resource: resource, Scopes: scopes}
	}
	const resource = "https://agent.example.com"
	tests := []struct {
		name string
		a, b GrantKey
		want bool
	}{
		{name: "same", a: key("c", resource, "a", "b"), b: key("c", resource, "a", "b"), want: true},
		{name: "nil and empty scopes", a: key("c", resource), b: GrantKey{Issuer: "https://as.example.com", ClientID: "c", Resource: resource, Scopes: []string{}}, want: true},
		{name: "scope with a space", a: key("c", resource, "a", "b"), b: key("c", resource, "a b")},
		{name: "other client", a: key("c", resource), b: key("d", resource)},
		{name: "field boundary", a: key("c", resource), b: key("c\n"+resource, "")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.a.Equal(tc.b); got != tc.want {
				t.Fatalf("GrantKey.Equal() = %v, want %v", got, tc.want)
			}
			if got := tc.a.ID() == tc.b.ID(); got != tc.want {
				t.Fatalf("GrantKey.ID() == other.ID() = %v, want %v", got, tc.want)
			}
		})
	}
}
