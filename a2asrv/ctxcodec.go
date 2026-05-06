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

	"github.com/a2aproject/a2a-go/v2/internal/utils"
)

type encodedCtx struct {
	CallContext *encodedCallCtx `json:"callContext,omitempty"`
	Attributes  map[string]any  `json:"attrs,omitempty"`
}

type encodedCallCtx struct {
	Tenant    string              `json:"tenant,omitempty"`
	SvcParams map[string][]string `json:"svcParams,omitempty"`
	User      *User               `json:"user,omitempty"`
}

type callCtxCodec struct {
	// AttrCodec is an extension point for values users want to propagate.
	AttrCodec ContextCodec
}

// Encode implements taskexec [ContextCodec.Encode].
func (c *callCtxCodec) Encode(ctx context.Context) (map[string]any, error) {
	result := encodedCtx{}

	if c.AttrCodec != nil {
		attrs, err := c.AttrCodec.Encode(ctx)
		if err != nil {
			return nil, fmt.Errorf("custom attr encoding failed: %w", err)
		}
		result.Attributes = attrs
	}

	if cc, ok := CallContextFrom(ctx); ok {
		result.CallContext = &encodedCallCtx{
			Tenant:    cc.tenant,
			SvcParams: cc.svcParams.cloneRaw(),
			User:      cc.User,
		}
	}

	return utils.ToMapStruct(result)
}

// Decode implements taskexec [ContextCodec.Decode].
func (c *callCtxCodec) Decode(ctx context.Context, data map[string]any) (context.Context, error) {
	if data == nil {
		return ctx, nil
	}

	encodedCtx, err := utils.FromMapStruct[encodedCtx](data)
	if err != nil {
		return ctx, err
	}

	if ecc := encodedCtx.CallContext; ecc != nil {
		svcParams := NewServiceParams(ecc.SvcParams)
		localCtx, callCtx := NewCallContext(ctx, svcParams)
		callCtx.tenant = ecc.Tenant
		callCtx.User = ecc.User
		ctx = localCtx
	}

	if encodedCtx.Attributes != nil {
		if c.AttrCodec == nil {
			return ctx, fmt.Errorf("custom attr decoding failed, codec not set")
		}
		localCtx, err := c.AttrCodec.Decode(ctx, encodedCtx.Attributes)
		if err != nil {
			return nil, fmt.Errorf("custom attr decoding failed: %w", err)
		}
		ctx = localCtx
	}

	return ctx, nil
}
