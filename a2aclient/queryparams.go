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

package a2aclient

import (
	"context"
	"net/url"
	"slices"
)

type httpQueryParamsKeyType struct{}

// attachHTTPQueryParams returns a context carrying HTTP query parameters that
// HTTP-based transports append to the request URL. Parameters attached to the
// context earlier are preserved.
func attachHTTPQueryParams(ctx context.Context, params url.Values) context.Context {
	if len(params) == 0 {
		return ctx
	}
	existing := httpQueryParamsFrom(ctx)
	merged := make(url.Values, len(existing)+len(params))
	for k, vs := range existing {
		merged[k] = slices.Clone(vs)
	}
	for k, vs := range params {
		for _, v := range vs {
			merged.Add(k, v)
		}
	}
	return context.WithValue(ctx, httpQueryParamsKeyType{}, merged)
}

// httpQueryParamsFrom returns HTTP query parameters previously attached to the
// context using [attachHTTPQueryParams]. Returns nil if there are none.
func httpQueryParamsFrom(ctx context.Context) url.Values {
	params, _ := ctx.Value(httpQueryParamsKeyType{}).(url.Values)
	return params
}
