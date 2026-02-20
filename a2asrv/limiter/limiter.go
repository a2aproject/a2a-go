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

// Package limiter provides configurations for controlling concurrency limit.
package limiter

import (
	"context"
)

type limiterScopeKeyType struct{}

var limiterScopeKey = limiterScopeKeyType{}

// ConcurrencyConfig defines how many concurrent executions are allowed globally or per-scope.
type ConcurrencyConfig struct {
	// MaxExecutions sets a limit on the number of active executions. A number of goroutines started by execution might be greater than 1.
	// A limit is enforced only when it is greater than zero.
	MaxExecutions int
	// GetMaxExecutions sets a limit on the number of active executions for a scope. A scope can be attached to context
	// using [WithScope] function. A number of goroutines started by execution might be greater than 1.
	// A limit is enforced only when it is greater than zero.
	GetMaxExecutions func(scope string) int
}

// AttachScope attaches the provided scope to context. It will later be used by components responsible
// for resource management for tracking and enforcing configured quotas.
func AttachScope(ctx context.Context, scope string) context.Context {
	return context.WithValue(ctx, limiterScopeKey, scope)
}

// ScopeFrom retrieves scope from context.
func ScopeFrom(ctx context.Context) (string, bool) {
	scope, ok := ctx.Value(limiterScopeKey).(string)
	return scope, ok
}
