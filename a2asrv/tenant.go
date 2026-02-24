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
	"fmt"
	"net/http"
	"net/url"

	"github.com/a2aproject/a2a-go/internal/pathtemplate"
)

// StripTenant extracts tenant information from the URL path based on a provided template,
// strips the tenant prefix, and attaches the tenant ID to the request context.
// Examples of templates:
// - "/{*}"
// - "/locations/*/projects/{*}"
// - "/{locations/*/projects/*}"
func StripTenant(template string, h http.Handler) http.Handler {
	compiledTemplate, err := pathtemplate.New(template)
	if err != nil {
		panic(fmt.Errorf("invalid template: %w", err))
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		matchResult, ok := compiledTemplate.Match(r.URL.Path)
		if !ok {
			http.NotFound(w, r)
			return
		}

		r2 := new(http.Request)
		*r2 = *r
		r2 = r2.WithContext(AttachTenant(r.Context(), matchResult.Captured))
		r2.URL = new(url.URL)
		*r2.URL = *r.URL
		r2.URL.Path = matchResult.Rest
		h.ServeHTTP(w, r2)
	})
}
