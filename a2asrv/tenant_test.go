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
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStripTenant(t *testing.T) {
	tests := []struct {
		name           string
		template       string
		path           string
		wantTenant     string
		wantRestPath   string
		wantStatusCode int
	}{
		{
			name:           "simple",
			template:       "/{*}",
			path:           "/my-tenant/tasks",
			wantTenant:     "my-tenant",
			wantRestPath:   "/tasks",
			wantStatusCode: http.StatusOK,
		},
		{
			name:           "complex with tenant",
			template:       "/locations/*/projects/{*}",
			path:           "/locations/us-central1/projects/my-project/tasks/123",
			wantTenant:     "my-project",
			wantRestPath:   "/tasks/123",
			wantStatusCode: http.StatusOK,
		},
		{
			name:           "multi-segment capture",
			template:       "{/locations/*/projects/*}",
			path:           "/locations/us-central1/projects/my-project/tasks",
			wantTenant:     "locations/us-central1/projects/my-project",
			wantRestPath:   "/tasks",
			wantStatusCode: http.StatusOK,
		},
		{
			name:           "trailing slash",
			template:       "/{*}",
			path:           "/my-tenant/",
			wantTenant:     "my-tenant",
			wantRestPath:   "/",
			wantStatusCode: http.StatusOK,
		},
		{
			name:           "no match",
			template:       "/fixed/{*}",
			path:           "/other/my-tenant/tasks",
			wantStatusCode: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := StripTenant(tt.template, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				tenant := TenantFromContext(r.Context())
				if tenant != tt.wantTenant {
					t.Errorf("tenant = %q, want %q", tenant, tt.wantTenant)
				}
				if r.URL.Path != tt.wantRestPath {
					t.Errorf("path = %q, want %q", r.URL.Path, tt.wantRestPath)
				}
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest("GET", tt.path, nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != tt.wantStatusCode {
				t.Errorf("status code = %v, want %v", rr.Code, tt.wantStatusCode)
			}
		})
	}
}
