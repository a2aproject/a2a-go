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
	"context"
	"iter"
	"net/http/httptest"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/go-cmp/cmp"
)

func TestRESTActionEncodedBoundaries(t *testing.T) {
	t.Parallel()
	cases := []struct{ method, path, operation, id string }{
		{"GET", "/tasks/foo%3Asubscribe", "get", "foo:subscribe"},
		{"GET", "/tasks/foo%3as%75bscribe?historyLength=0", "get", "foo:subscribe"},
		{"GET", "/tasks/foo%253Asubscribe", "get", "foo%3Asubscribe"},
		{"GET", "/tasks/foo:s%75bscribe", "subscribe", "foo"},
		{"GET", "/tasks/foo%3Asubscribe:subscribe", "subscribe", "foo:subscribe"},
		{"POST", "/tasks/foo%3Acancel", "", ""},
		{"POST", "/tasks/foo:c%61ncel", "cancel", "foo"},
		{"POST", "/tasks/foo%3Acancel:cancel", "cancel", "foo:cancel"},
	}
	for _, tc := range cases {
		for _, tenant := range []string{"", "tenant"} {
			t.Run(tenant+tc.method+tc.path, func(t *testing.T) {
				t.Parallel()
				var operation, id string
				mock := &mockRequestHandler{
					getTaskFunc: func(_ context.Context, req *a2a.GetTaskRequest) (*a2a.Task, error) {
						operation, id = "get", string(req.ID)
						return &a2a.Task{ID: req.ID}, nil
					},
					cancelTaskFunc: func(_ context.Context, req *a2a.CancelTaskRequest) (*a2a.Task, error) {
						operation, id = "cancel", string(req.ID)
						return &a2a.Task{ID: req.ID}, nil
					},
					subscribeToTaskFunc: func(_ context.Context, req *a2a.SubscribeToTaskRequest) iter.Seq2[a2a.Event, error] {
						operation, id = "subscribe", string(req.ID)
						return func(yield func(a2a.Event, error) bool) {}
					},
				}
				rw := httptest.NewRecorder()
				handler := NewRESTHandler(mock)
				path := tc.path
				if tenant != "" {
					handler = NewTenantRESTHandler("/{*}", mock)
					path = "/" + tenant + path
				}
				handler.ServeHTTP(rw, httptest.NewRequest(tc.method, path, nil))
				if diff := cmp.Diff([]string{tc.operation, tc.id}, []string{operation, id}); diff != "" {
					t.Fatalf("route wrong result (-want +got) diff = %s", diff)
				}
			})
		}
	}
}
