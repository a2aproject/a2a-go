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

package a2asrv

import (
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/go-cmp/cmp"
)

func TestExtensions_RequestedURIs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		params map[string][]string
		want   []string
	}{
		{
			name:   "no extensions requested",
			params: map[string][]string{},
			want:   []string{},
		},
		{
			name:   "single value",
			params: map[string][]string{a2a.SvcParamExtensions: {"urn:a"}},
			want:   []string{"urn:a"},
		},
		{
			name:   "repeated values",
			params: map[string][]string{a2a.SvcParamExtensions: {"urn:a", "urn:b"}},
			want:   []string{"urn:a", "urn:b"},
		},
		{
			name:   "comma-separated value",
			params: map[string][]string{a2a.SvcParamExtensions: {"urn:a,urn:b"}},
			want:   []string{"urn:a", "urn:b"},
		},
		{
			name:   "comma-separated and repeated values",
			params: map[string][]string{a2a.SvcParamExtensions: {"urn:a, urn:b", "urn:c"}},
			want:   []string{"urn:a", "urn:b", "urn:c"},
		},
		{
			name:   "spaces and tabs around elements",
			params: map[string][]string{a2a.SvcParamExtensions: {" urn:a\t,\t urn:b "}},
			want:   []string{"urn:a", "urn:b"},
		},
		{
			name:   "empty elements",
			params: map[string][]string{a2a.SvcParamExtensions: {"", ",urn:a,, ,", " "}},
			want:   []string{"urn:a"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, callCtx := NewCallContext(t.Context(), NewServiceParams(tc.params))

			got := callCtx.Extensions().RequestedURIs()

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("Extensions.RequestedURIs() wrong result (-want +got) diff = %s", diff)
			}
		})
	}
}

func TestExtensions_Requested(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		params    map[string][]string
		extension string
		want      bool
	}{
		{
			name:      "repeated values",
			params:    map[string][]string{a2a.SvcParamExtensions: {"urn:a", "urn:b"}},
			extension: "urn:b",
			want:      true,
		},
		{
			name:      "first element of comma-separated value",
			params:    map[string][]string{a2a.SvcParamExtensions: {"urn:a,urn:b"}},
			extension: "urn:a",
			want:      true,
		},
		{
			name:      "last element of comma-separated value",
			params:    map[string][]string{a2a.SvcParamExtensions: {"urn:a, urn:b"}},
			extension: "urn:b",
			want:      true,
		},
		{
			name:      "not requested",
			params:    map[string][]string{a2a.SvcParamExtensions: {"urn:a,urn:b"}},
			extension: "urn:c",
			want:      false,
		},
		{
			name:      "whole comma-separated value is not an extension",
			params:    map[string][]string{a2a.SvcParamExtensions: {"urn:a,urn:b"}},
			extension: "urn:a,urn:b",
			want:      false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, callCtx := NewCallContext(t.Context(), NewServiceParams(tc.params))

			got := callCtx.Extensions().Requested(&a2a.AgentExtension{URI: tc.extension})

			if got != tc.want {
				t.Fatalf("Extensions.Requested(%q) = %v, want %v", tc.extension, got, tc.want)
			}
		})
	}
}
