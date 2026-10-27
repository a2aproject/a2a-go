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
	"errors"
	"net/url"
	"strings"
)

type httpQueryParamsKeyType struct{}

// AttachHTTPQueryParams returns a context carrying HTTP query parameters that
// HTTP-based transports append to the request URL. The built-in JSON-RPC and
// REST transports support these parameters. Custom [Transport] implementations
// should read them using [HTTPQueryParamsFrom] and apply them according to
// their protocol binding; parameters are dropped by transports that do not.
// Query parameters attached to the context earlier are preserved.
func AttachHTTPQueryParams(ctx context.Context, params url.Values) context.Context {
	if len(params) == 0 {
		return ctx
	}
	merged := make(url.Values)
	mergeQueryParams(merged, HTTPQueryParamsFrom(ctx))
	mergeQueryParams(merged, params)
	return context.WithValue(ctx, httpQueryParamsKeyType{}, merged)
}

// HTTPQueryParamsFrom returns HTTP query parameters previously attached to the
// context using [AttachHTTPQueryParams]. Returns nil if there are none.
func HTTPQueryParamsFrom(ctx context.Context) url.Values {
	params, _ := ctx.Value(httpQueryParamsKeyType{}).(url.Values)
	return params
}

// mergeQueryParams appends all key-value pairs from src to dst.
func mergeQueryParams(dst, src url.Values) {
	for k, vals := range src {
		for _, v := range vals {
			dst.Add(k, v)
		}
	}
}

// redactURLError strips the query part from [*url.Error] values so that
// credentials carried in URL query parameters do not leak into error
// messages and logs. Errors of other types are returned unchanged.
func redactURLError(err error) error {
	var urlErr *url.Error
	if !errors.As(err, &urlErr) {
		return err
	}
	i := strings.Index(urlErr.URL, "?")
	if i < 0 {
		return err
	}
	redacted := *urlErr
	redacted.URL = urlErr.URL[:i]
	return &redacted
}
