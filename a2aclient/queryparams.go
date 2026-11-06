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
	"context"
	"net/url"

	"github.com/a2aproject/a2a-go/v2/internal/utils"
)

type httpQueryParamsKeyType struct{}

// AttachHTTPQueryParams returns a context with query parameters which the built-in JSON-RPC and REST
// transports append to request URLs. Custom [Transport] implementations need to apply them using
// [HTTPQueryParamsFrom]. Parameters attached earlier are preserved.
func AttachHTTPQueryParams(ctx context.Context, params url.Values) context.Context {
	if len(params) == 0 {
		return ctx
	}
	merged := make(url.Values)
	utils.MergeQueryParams(merged, HTTPQueryParamsFrom(ctx))
	utils.MergeQueryParams(merged, params)
	return context.WithValue(ctx, httpQueryParamsKeyType{}, merged)
}

// HTTPQueryParamsFrom returns query parameters attached using [AttachHTTPQueryParams].
func HTTPQueryParamsFrom(ctx context.Context) url.Values {
	params, _ := ctx.Value(httpQueryParamsKeyType{}).(url.Values)
	return params
}
