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

package a2aclient

import (
	"slices"
	"testing"
)

func TestServiceParams(t *testing.T) {
	tests := []struct {
		name     string
		initial  ServiceParams
		key      string
		vals     []string
		expected []string
	}{
		{
			name:     "case insensitive key storage",
			initial:  make(ServiceParams),
			key:      "Key",
			vals:     []string{"value"},
			expected: []string{"value"},
		},
		{
			name:     "multiple values",
			initial:  make(ServiceParams),
			key:      "Multi",
			vals:     []string{"v1", "v2", "v3"},
			expected: []string{"v1", "v2", "v3"},
		},
		{
			name: "multiple values with duplicates",
			initial: ServiceParams{
				"multi": {"v1"},
			},
			key:      "Multi",
			vals:     []string{"v1", "v2", "v1"},
			expected: []string{"v1", "v2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.initial.Append(tt.key, tt.vals...)
			got := tt.initial.Get(tt.key)
			if !slices.Equal(got, tt.expected) {
				t.Errorf("ServiceParams.Append() = %v, want %v", got, tt.expected)
			}
		})
	}
}
