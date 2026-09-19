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

package a2a

import (
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestSecurityRequirementsOptionsUnmarshalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		json string
		want SecurityRequirementsOptions
	}{
		{
			name: "protojson scopes wrapper",
			json: `[{"schemes":{"oauth2":{"list":["openid","profile"]}}}]`,
			want: SecurityRequirementsOptions{
				{"oauth2": {"openid", "profile"}},
			},
		},
		{
			name: "legacy scopes array",
			json: `[{"schemes":{"oauth2":["openid","profile"]}}]`,
			want: SecurityRequirementsOptions{
				{"oauth2": {"openid", "profile"}},
			},
		},
		{
			name: "empty protojson scopes wrapper",
			json: `[{"schemes":{"apiKey":{"list":[]}}}]`,
			want: SecurityRequirementsOptions{
				{"apiKey": {}},
			},
		},
		{
			name: "protojson empty message",
			json: `[{"schemes":{"apiKey":{}}}]`,
			want: SecurityRequirementsOptions{
				{"apiKey": nil},
			},
		},
		{
			name: "mixed protojson and legacy scopes",
			json: `[{"schemes":{"oauth2":{"list":["openid"]},"apiKey":[]}}]`,
			want: SecurityRequirementsOptions{
				{"oauth2": {"openid"}, "apiKey": {}},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var got SecurityRequirementsOptions
			if err := json.Unmarshal([]byte(tc.json), &got); err != nil {
				t.Fatalf("json.Unmarshal() error = %v, want nil", err)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("json.Unmarshal() wrong result (-want +got) diff = %s", diff)
			}
		})
	}
}

func TestSecurityRequirementsOptionsUnmarshalJSONRejectsInvalidScopesWrapper(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		json string
	}{
		{
			name: "list is not an array",
			json: `[{"schemes":{"oauth2":{"list":"openid"}}}]`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var got SecurityRequirementsOptions
			if err := json.Unmarshal([]byte(tc.json), &got); err == nil {
				t.Fatal("json.Unmarshal() error = nil, want non-nil")
			}
		})
	}
}
