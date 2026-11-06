// Copyright 2025 The A2A Authors
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
	"maps"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestServiceParams_With(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		params     *ServiceParams
		additional map[string][]string
		want       map[string][]string
	}{
		{
			name:       "nil params",
			params:     nil,
			additional: map[string][]string{"X-Key": {"v1"}},
			want:       map[string][]string{"x-key": {"v1"}},
		},
		{
			name:       "nil params and no additional values",
			params:     nil,
			additional: nil,
			want:       map[string][]string{},
		},
		{
			name:       "existing params",
			params:     NewServiceParams(map[string][]string{"A": {"1"}, "B": {"2"}}),
			additional: map[string][]string{"b": {"3"}, "C": {"4"}},
			want:       map[string][]string{"a": {"1"}, "b": {"3"}, "c": {"4"}},
		},
		{
			name:       "existing params and no additional values",
			params:     NewServiceParams(map[string][]string{"A": {"1"}}),
			additional: nil,
			want:       map[string][]string{"a": {"1"}},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			before := maps.Collect(tc.params.List())

			got := tc.params.With(tc.additional)

			if diff := cmp.Diff(tc.want, maps.Collect(got.List())); diff != "" {
				t.Fatalf("ServiceParams.With() wrong result (-want +got) diff = %s", diff)
			}
			if diff := cmp.Diff(before, maps.Collect(tc.params.List())); diff != "" {
				t.Fatalf("ServiceParams.With() modified receiver (-want +got) diff = %s", diff)
			}
		})
	}
}
