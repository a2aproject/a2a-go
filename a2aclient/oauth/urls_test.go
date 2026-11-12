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
	"net/url"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestNormalizeResource(t *testing.T) {
	t.Parallel()
	tests := []struct{ in, want string }{
		{"https://Agent.Example.com/", "https://agent.example.com"},
		{"https://agent.example.com:443/a2a/", "https://agent.example.com/a2a"},
		{"http://127.0.0.1:8080/a?x=1#frag", "http://127.0.0.1:8080/a"},
		{"http://localhost:80", "http://localhost"},
		{"agent.example.com:50051", "https://agent.example.com:50051"},
		{"", ""},
		{"%zz", "%zz"},
		{"https://", "https://"},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			if got := NormalizeResource(tc.in); got != tc.want {
				t.Fatalf("NormalizeResource(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestResourceCovers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		resource, target string
		want             bool
	}{
		{"https://agent.example.com", "https://agent.example.com/message:send", true},
		{"https://agent.example.com/a2a", "https://agent.example.com/a2a/v1/tasks", true},
		{"https://agent.example.com/a2a", "https://agent.example.com/a2a", true},
		{"https://agent.example.com/a2a", "https://agent.example.com/a2ab", false},
		{"https://agent.example.com", "https://other.example.com", false},
		{"https://agent.example.com", "http://agent.example.com", false},
		{"https://agent.example.com/agents/alpha", "https://agent.example.com/agents/alpha/../beta/rpc", false},
		{"https://agent.example.com/agents/alpha", "https://agent.example.com/agents/alpha/%2e%2e/beta", false},
		{"https://agent.example.com/agents/alpha", "https://agent.example.com/agents/alpha/x%2F..%2F..%2Fbeta", false},
		{"https://agent.example.com/agents/alpha", "https://agent.example.com/agents/alpha/x%5C..%5Cbeta", false},
		{"https://agent.example.com/agents/alpha", "https://agent.example.com/agents/alpha/./rpc", false},
		{"https://agent.example.com/agents/x/..", "https://agent.example.com/agents/beta", false},
		{"https://agent.example.com/agents/alpha", "https://agent.example.com/agents/alpha/v1.0/..rpc", true},
	}
	for _, tc := range tests {
		t.Run(tc.resource+" "+tc.target, func(t *testing.T) {
			t.Parallel()
			if got := ResourceCovers(tc.resource, tc.target); got != tc.want {
				t.Fatalf("ResourceCovers(%q, %q) = %v, want %v", tc.resource, tc.target, got, tc.want)
			}
		})
	}
}

func TestCheckSecureURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in string
		ok bool
	}{
		{"https://login.example.com/token", true},
		{"http://127.0.0.1:9000/token", true},
		{"http://localhost/token", true},
		{"http://[::1]:9000/token", true},
		{"http://login.example.com/token", false},
		{"ftp://login.example.com", false},
		{"/token", false},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			if err := checkSecureURL(tc.in); (err == nil) != tc.ok {
				t.Fatalf("checkSecureURL(%q) error = %v, want ok %v", tc.in, err, tc.ok)
			}
		})
	}
}

func TestIsLoopbackURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want bool
	}{
		{"https://127.0.0.1:8443/x", true},
		{"https://[::1]/x", true},
		{"https://localhost/x", true},
		{"https://localhost.:8443/x", true},
		{"https://foo.localhost/x", true},
		{"https://0.0.0.0:8443/x", true},
		{"https://[::]:8443/x", true},
		{"https://127.1:8443/x", true},
		{"https://2130706433/x", true},
		{"https://0x7f000001/x", true},
		{"https://login.example.com/x", false},
		{"https://10.0.0.1/x", false},
		{"https://1password.com/x", false},
		{"agent.example.com:443", false},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			if got := isLoopbackURL(tc.in); got != tc.want {
				t.Fatalf("isLoopbackURL(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestServerMetadataURLs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		issuer string
		want   []string
	}{
		{
			issuer: "https://login.example.com",
			want: []string{
				"https://login.example.com/.well-known/oauth-authorization-server",
				"https://login.example.com/.well-known/openid-configuration",
			},
		},
		{
			issuer: "https://login.example.com/tenant/v2.0/",
			want: []string{
				"https://login.example.com/.well-known/oauth-authorization-server/tenant/v2.0",
				"https://login.example.com/tenant/v2.0/.well-known/oauth-authorization-server",
				"https://login.example.com/tenant/v2.0/.well-known/openid-configuration",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.issuer, func(t *testing.T) {
			t.Parallel()
			if diff := cmp.Diff(tc.want, serverMetadataURLs(tc.issuer)); diff != "" {
				t.Fatalf("serverMetadataURLs(%q) wrong result (-want +got) diff = %s", tc.issuer, diff)
			}
		})
	}
}

func TestResourceMetadataURLs(t *testing.T) {
	t.Parallel()
	const prm = "https://agent.example.com/.well-known/oauth-protected-resource"
	tests := []struct {
		target string
		want   []string
	}{
		{target: "https://agent.example.com", want: []string{prm}},
		{target: "https://agent.example.com/a2a/v1/", want: []string{prm + "/a2a/v1", prm + "/a2a", prm}},
		{target: "https://agent.example.com/a2a/v1/message:send", want: []string{prm + "/a2a/v1/message:send", prm + "/a2a/v1", prm + "/a2a", prm}},
		{target: "https://agent.example.com/t/a2a/v1/tasks/abc", want: []string{prm + "/t/a2a/v1/tasks", prm + "/t/a2a/v1", prm + "/t/a2a", prm + "/t", prm}},
	}
	for _, tc := range tests {
		t.Run(tc.target, func(t *testing.T) {
			t.Parallel()
			u, err := url.Parse(tc.target)
			if err != nil {
				t.Fatalf("url.Parse(%q) error = %v", tc.target, err)
			}
			if diff := cmp.Diff(tc.want, resourceMetadataURLs(u)); diff != "" {
				t.Fatalf("resourceMetadataURLs(%q) wrong result (-want +got) diff = %s", tc.target, diff)
			}
		})
	}
}
