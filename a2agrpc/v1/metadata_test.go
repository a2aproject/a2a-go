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

package a2agrpc

import (
	"context"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/metadata"
)

func TestWithGRPCMetadata(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		callerMD []string
		params   a2aclient.ServiceParams
		want     metadata.MD
	}{
		{
			name:   "no caller metadata",
			params: a2aclient.ServiceParams{a2a.SvcParamVersion: {"1.0"}},
			want:   metadata.MD{"a2a-version": {"1.0"}},
		},
		{
			name:     "caller metadata preserved",
			callerMD: []string{"authorization", "Bearer token", "x-request-id", "req-1"},
			params:   a2aclient.ServiceParams{a2a.SvcParamVersion: {"1.0"}, "X-Tenant": {"acme"}},
			want: metadata.MD{
				"authorization": {"Bearer token"},
				"x-request-id":  {"req-1"},
				"a2a-version":   {"1.0"},
				"x-tenant":      {"acme"},
			},
		},
		{
			name:     "service params override conflicting caller keys",
			callerMD: []string{"authorization", "Bearer token", "a2a-version", "stale"},
			params:   a2aclient.ServiceParams{a2a.SvcParamVersion: {"1.0"}},
			want: metadata.MD{
				"authorization": {"Bearer token"},
				"a2a-version":   {"1.0"},
			},
		},
		{
			name:     "empty params keep caller metadata",
			callerMD: []string{"authorization", "Bearer token"},
			want:     metadata.MD{"authorization": {"Bearer token"}},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			if len(tc.callerMD) > 0 {
				ctx = metadata.AppendToOutgoingContext(ctx, tc.callerMD...)
			}

			got, _ := metadata.FromOutgoingContext(withGRPCMetadata(ctx, tc.params))
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("withGRPCMetadata() wrong result (-want +got) diff = %s", diff)
			}
		})
	}
}
