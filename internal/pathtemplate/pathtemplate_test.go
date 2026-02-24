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

package pathtemplate

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestNew(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		raw     string
		wantErr string
	}{
		{
			name: "valid single wildcard capture",
			raw:  "v1/tasks/{*}",
		},
		{
			name: "valid path-like capture",
			raw:  "v1/{*/*/projects/*}/messages",
		},
		{
			name: "valid path-like capture with leading slash",
			raw:  "v1{/*/*/projects/*}/messages",
		},
		{
			name: "valid path-like capture with leading and trailing slash",
			raw:  "v1{/*/*/projects/*/}messages/",
		},
		{
			name:    "empty template",
			raw:     "",
			wantErr: "empty template",
		},
		{
			name:    "no capture group",
			raw:     "v1/tasks/*",
			wantErr: "no capture group {} in v1/tasks/*",
		},
		{
			name:    "invalid capture group order",
			raw:     "v1/tasks/}{",
			wantErr: "invalid capture group in v1/tasks/}{",
		},
		{
			name:    "duplicate open",
			raw:     "v1/{{*}}",
			wantErr: "duplicate { or } in v1/{{*}}",
		},
		{
			name:    "duplicate close",
			raw:     "v1/{*}}",
			wantErr: "duplicate { or } in v1/{*}}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := New(tt.raw)
			if (err != nil) != (tt.wantErr != "") {
				t.Fatalf("New(%q) error = %v, wantErr %v", tt.raw, err, tt.wantErr)
			}
			if err != nil && err.Error() != tt.wantErr {
				t.Fatalf("New(%q) error = %v, want %q", tt.raw, err, tt.wantErr)
			}
			if err == nil && got == nil {
				t.Fatalf("New(%q) = nil, want non-nil", tt.raw)
			}
		})
	}
}

func TestMatch(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		template string
		path     string
		want     *MatchResult
		wantOk   bool
	}{
		{
			name:     "simple match",
			template: "v1/tasks/{*}",
			path:     "v1/tasks/123",
			want:     &MatchResult{Captured: "123", Rest: "/"},
			wantOk:   true,
		},
		{
			name:     "match with rest",
			template: "v1/tasks/{*}",
			path:     "v1/tasks/123/messages",
			want:     &MatchResult{Captured: "123", Rest: "/messages"},
			wantOk:   true,
		},
		{
			name:     "multi-segment wildcard capture",
			template: "v1/{*/*/projects/*}/messages",
			path:     "v1/locations/us-central1/projects/my-project/messages/456",
			want:     &MatchResult{Captured: "locations/us-central1/projects/my-project", Rest: "/456"},
			wantOk:   true,
		},
		{
			name:     "multi-segment wildcard capture with leading and trailing slash",
			template: "v1{/*/*/projects/*/}messages",
			path:     "v1/locations/us-central1/projects/my-project/messages/456",
			want:     &MatchResult{Captured: "locations/us-central1/projects/my-project", Rest: "/456"},
			wantOk:   true,
		},
		{
			name:     "no match wrong segment",
			template: "v1/tasks/{*}",
			path:     "v2/tasks/123",
			wantOk:   false,
		},
		{
			name:     "no match path too short",
			template: "v1/tasks/{*}",
			path:     "v1/tasks",
			wantOk:   false,
		},
		{
			name:     "exact match inside capture",
			template: "v1/tasks/{foo}",
			path:     "v1/tasks/foo",
			want:     &MatchResult{Captured: "foo", Rest: "/"},
			wantOk:   true,
		},
		{
			name:     "exact match mismatch inside capture",
			template: "v1/tasks/{foo}",
			path:     "v1/tasks/bar",
			wantOk:   false,
		},
		{
			name:     "match with leading and trailing slashes",
			template: "/v1/tasks/{*}/",
			path:     "/v1/tasks/123/",
			want:     &MatchResult{Captured: "123", Rest: "/"},
			wantOk:   true,
		},
		{
			name:     "segments after capture match",
			template: "v1/tasks/{*}/messages",
			path:     "v1/tasks/123/messages/456",
			want:     &MatchResult{Captured: "123", Rest: "/456"},
			wantOk:   true,
		},
		{
			name:     "segments after capture mismatch",
			template: "v1/tasks/{*}/messages",
			path:     "v1/tasks/123/logs",
			wantOk:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tpl, err := New(tt.template)
			if err != nil {
				t.Fatalf("New(%q) unexpected error: %v", tt.template, err)
			}
			got, ok := tpl.Match(tt.path)
			if ok != tt.wantOk {
				t.Fatalf("Template(%q).Match(%q) ok = %v, want %v", tt.template, tt.path, ok, tt.wantOk)
			}
			if !ok {
				return
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Fatalf("Template(%q).Match(%q) wrong result (+got,-want) diff = %s", tt.template, tt.path, diff)
			}
		})
	}
}
