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

	"github.com/a2aproject/a2a-go/v2/log"
)

const (
	authKey           = "auth"
	authNameKey       = "name"
	authStatusKey     = "authenticated"
	authAttributesKey = "attributes"
	svcParamsKey      = "svcParams"
	tenantKey         = "tenant"
)

type callCtxCodec struct{}

// Encode implements taskexec ContextCodec.Encode.
func (c *callCtxCodec) Encode(ctx context.Context) map[string]any {
	data := map[string]any{}
	if cc, ok := CallContextFrom(ctx); ok {
		data[svcParamsKey] = cc.svcParams.cloneRaw()
		data[authKey] = map[string]any{
			authNameKey:       cc.User.Name,
			authStatusKey:     cc.User.Authenticated,
			authAttributesKey: cc.User.Attributes,
		}
		if cc.Tenant() != "" {
			data[tenantKey] = cc.Tenant()
		}
	}
	return data
}

// Decode implements taskexec ContextCodec.Decode.
func (c *callCtxCodec) Decode(ctx context.Context, data map[string]any) context.Context {
	var svcParams *ServiceParams

	if rawParams, ok := data[svcParamsKey]; ok {
		if typedParams, ok := rawParams.(map[string][]string); ok {
			svcParams = NewServiceParams(typedParams)
		} else if anyParams, ok := rawParams.(map[string]any); ok {
			// Handle type-erasure map[string]any with []any values.
			converted := make(map[string][]string, len(anyParams))
			for k, v := range anyParams {
				if arr, ok := v.([]any); ok {
					strs := make([]string, 0, len(arr))
					for _, elem := range arr {
						if s, ok := elem.(string); ok {
							strs = append(strs, s)
						}
					}
					converted[k] = strs
				}
			}
			svcParams = NewServiceParams(converted)
		} else {
			log.Warn(ctx, "unexpected service params type", "type", fmt.Sprintf("%T", rawParams))
		}
	}

	ctx, callCtx := NewCallContext(ctx, svcParams)

	callCtx.User = &User{}
	if d, ok := data[authKey]; ok {
		if authInfo, ok := d.(map[string]any); ok {
			user := &User{}
			if userName, ok := authInfo[authNameKey].(string); ok {
				user.Name = userName
			}
			if authenticated, ok := authInfo[authStatusKey].(bool); ok {
				user.Authenticated = authenticated
			}
			if attributes, ok := authInfo[authAttributesKey].(map[string]any); ok {
				user.Attributes = attributes
			}
			callCtx.User = user
		} else {
			log.Warn(ctx, "unexpected auth type", "type", fmt.Sprintf("%T", d))
		}
	}

	if d, ok := data[tenantKey]; ok {
		if t, ok := d.(string); ok {
			callCtx.tenant = t
		} else {
			log.Warn(ctx, "unexpected tenant type", "type", fmt.Sprintf("%T", d))
		}
	}

	return ctx
}
