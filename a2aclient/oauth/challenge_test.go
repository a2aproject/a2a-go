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

	"github.com/google/go-cmp/cmp"
)

func TestParseChallenges(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		values []string
		want   []Challenge
	}{
		{
			name:   "bearer with quoted params",
			values: []string{`Bearer resource_metadata="https://agent.example.com/.well-known/oauth-protected-resource", scope="a2a.invoke a2a.read"`},
			want: []Challenge{{Scheme: "bearer", Params: map[string]string{
				"resource_metadata": "https://agent.example.com/.well-known/oauth-protected-resource",
				"scope":             "a2a.invoke a2a.read",
			}}},
		},
		{
			name:   "bare scheme",
			values: []string{"Bearer"},
			want:   []Challenge{{Scheme: "bearer", Params: map[string]string{}}},
		},
		{
			name:   "several challenges in one header",
			values: []string{`Basic realm="corp", Bearer error="invalid_token", error_description="expired"`},
			want: []Challenge{
				{Scheme: "basic", Params: map[string]string{"realm": "corp"}},
				{Scheme: "bearer", Params: map[string]string{"error": "invalid_token", "error_description": "expired"}},
			},
		},
		{
			name:   "several headers",
			values: []string{`Negotiate abc==`, `Bearer realm=agents`},
			want: []Challenge{
				{Scheme: "negotiate", Params: map[string]string{}},
				{Scheme: "bearer", Params: map[string]string{"realm": "agents"}},
			},
		},
		{
			name:   "escaped quotes and case-insensitive names",
			values: []string{`BEARER Realm="a \"quoted\" realm"`},
			want:   []Challenge{{Scheme: "bearer", Params: map[string]string{"realm": `a "quoted" realm`}}},
		},
		{
			name:   "unquoted url value",
			values: []string{`Bearer resource_metadata=https://agent.example.com/prm`},
			want:   []Challenge{{Scheme: "bearer", Params: map[string]string{"resource_metadata": "https://agent.example.com/prm"}}},
		},
		{
			name: "no headers",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ParseChallenges(tc.values)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("ParseChallenges(%q) wrong result (-want +got) diff = %s", tc.values, diff)
			}
		})
	}
}
