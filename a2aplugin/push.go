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

package a2aplugin

import (
	"context"

	"github.com/a2aproject/a2a-go/v2/a2asrv/push"
)

func (p *serverPlugin) WrapPushDelivery(next push.DeliveryHandler) push.DeliveryHandler {
	if p.hooks.BeforePush != nil || p.hooks.AfterPush != nil {
		next = p.hookDelivery(next)
	}
	if p.hooks.WrapPushDelivery != nil {
		next = p.hooks.WrapPushDelivery(next)
	}
	return next
}

func (p *serverPlugin) hookDelivery(next push.DeliveryHandler) push.DeliveryHandler {
	return func(ctx context.Context, d *push.Delivery) (*push.DeliveryResult, error) {
		if p.hooks.BeforePush != nil {
			hookCtx, err := p.hooks.BeforePush(ctx, d)
			ctx = hookContext(ctx, hookCtx)
			if err != nil {
				result := &push.DeliveryResult{Err: err}
				p.afterPush(ctx, d, result)
				return result, err
			}
		}
		result, err := next(ctx, d)
		if result == nil {
			result = &push.DeliveryResult{Err: err}
		}
		p.afterPush(ctx, d, result)
		return result, err
	}
}

func (p *serverPlugin) afterPush(ctx context.Context, d *push.Delivery, result *push.DeliveryResult) {
	if p.hooks.AfterPush != nil {
		p.hooks.AfterPush(ctx, d, result)
	}
}
