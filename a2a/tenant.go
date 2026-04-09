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

package a2a

import (
	"context"
)

type tenantKeyType struct{}

// TenantFrom returns the value of the tenant field if it was set present in the request.
func TenantFrom(ctx context.Context) (string, bool) {
	t, ok := ctx.Value(tenantKeyType{}).(string)
	return t, ok
}

// AttachTenant attaches the tenant to the context. It can later be retrieved using [TenantFrom].
func AttachTenant(ctx context.Context, tenant string) context.Context {
	return context.WithValue(ctx, tenantKeyType{}, tenant)
}
