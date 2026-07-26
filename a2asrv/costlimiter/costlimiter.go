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

// Package costlimiter provides cost-based budget enforcement for A2A server request handling.
//
// While [github.com/a2aproject/a2a-go/v2/a2asrv/limiter.ConcurrencyConfig] limits
// the number of in-flight executions, CostLimiter limits the total cost consumed
// over time. This is useful for enforcing LLM token budgets, compute-time quotas,
// or monetary credit limits per agent or tenant.
//
// CostLimiter is implemented as a [github.com/a2aproject/a2a-go/v2/a2asrv.CallInterceptor]
// and is fully pluggable — the cost estimation function and budget store are both
// provided by the caller.
package costlimiter

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/a2asrv/limiter"
)

// ErrBudgetExceeded indicates that the requested cost exceeds the available budget.
var ErrBudgetExceeded = errors.New("cost budget exceeded")

// CostFunc computes the expected cost of a request. Implementations may inspect
// the call context, request payload, or any other available information to
// estimate the cost. A return value of 0 means the request has no cost and
// should always be allowed through.
type CostFunc func(ctx context.Context, callCtx *a2asrv.CallContext, req *a2asrv.Request) int64

// CostStore tracks cost budgets for named scopes. Implementations are
// responsible for atomicity of Reserve and Release operations.
type CostStore interface {
	// Available returns the current available budget for the given scope.
	// A negative value conventionally means "unlimited."
	Available(ctx context.Context, scope string) (int64, error)

	// Reserve atomically deducts cost from the budget for scope.
	// Returns true if the reservation succeeded, false if budget was
	// insufficient. Implementations must be safe for concurrent use.
	Reserve(ctx context.Context, scope string, cost int64) (bool, error)

	// Release returns previously reserved cost back to the budget.
	Release(ctx context.Context, scope string, cost int64) error
}

// ScopeFunc derives a scope identifier from the call context. If nil,
// the scope attached to the context via [limiter.AttachScope] is used.
type ScopeFunc func(callCtx *a2asrv.CallContext) string

// A CostLimiter is a server-side [a2asrv.CallInterceptor] that enforces
// cost-based budgets on agent executions.
//
// The zero value is not usable; use [NewCostLimiter].
type CostLimiter struct {
	a2asrv.PassthroughCallInterceptor

	costFn  CostFunc
	store   CostStore
	scopeFn ScopeFunc
}

// NewCostLimiter creates a CostLimiter that uses store to track budgets
// and costFn to estimate the cost of each request.
func NewCostLimiter(store CostStore, costFn CostFunc, opts ...Option) *CostLimiter {
	cl := &CostLimiter{
		store:  store,
		costFn: costFn,
	}
	for _, opt := range opts {
		opt(cl)
	}
	// If no custom scope function was provided, the Before method will
	// fall back to the scope attached via limiter.AttachScope, as
	// documented in the ScopeFunc docstring.
	return cl
}

// Option configures a CostLimiter.
type Option func(*CostLimiter)

// WithScopeFunc sets a custom scope derivation function.
func WithScopeFunc(fn ScopeFunc) Option {
	return func(cl *CostLimiter) {
		cl.scopeFn = fn
	}
}

// Before implements [a2asrv.CallInterceptor]. It estimates the request cost
// and attempts to reserve budget. If the budget is insufficient, it returns
// a rate-limited error before the agent executor is invoked.
func (cl *CostLimiter) Before(ctx context.Context, callCtx *a2asrv.CallContext, req *a2asrv.Request) (context.Context, any, error) {
	scope := cl.resolveScope(ctx, callCtx)
	cost := cl.costFn(ctx, callCtx, req)
	if cost <= 0 {
		return ctx, nil, nil
	}

	ok, err := cl.store.Reserve(ctx, scope, cost)
	if err != nil {
		return ctx, nil, fmt.Errorf("costlimiter: reserving budget: %w", err)
	}
	if !ok {
		return ctx, nil, fmt.Errorf("%w: scope %q: budget exhausted", ErrBudgetExceeded, scope)
	}

	return context.WithValue(ctx, reservationKeyType{}, reservation{scope: scope, cost: cost}), nil, nil
}

// resolveScope returns the scope for this call. If a custom ScopeFunc was
// provided, it is used. Otherwise, falls back to the scope attached via
// [limiter.AttachScope].
func (cl *CostLimiter) resolveScope(ctx context.Context, callCtx *a2asrv.CallContext) string {
	if cl.scopeFn != nil {
		return cl.scopeFn(callCtx)
	}
	if s, ok := limiter.ScopeFrom(ctx); ok && s != "" {
		return s
	}
	return "default"
}

// After implements [a2asrv.CallInterceptor]. If the execution was cancelled
// before doing meaningful work, the reserved cost is released. The release
// uses context.WithoutCancel so that cancelled contexts do not prevent
// budget restoration.
func (cl *CostLimiter) After(ctx context.Context, _ *a2asrv.CallContext, resp *a2asrv.Response) error {
	res, ok := reservationFrom(ctx)
	if !ok {
		return nil
	}

	if resp != nil && resp.Err != nil && errors.Is(resp.Err, context.Canceled) {
		_ = cl.store.Release(context.WithoutCancel(ctx), res.scope, res.cost)
	}
	return nil
}

type reservationKeyType struct{}

type reservation struct {
	scope string
	cost  int64
}

func reservationFrom(ctx context.Context) (reservation, bool) {
	v, ok := ctx.Value(reservationKeyType{}).(reservation)
	return v, ok
}

// InMemoryCostStore is an in-memory implementation of [CostStore] suitable
// for single-process deployments. It is safe for concurrent use.
type InMemoryCostStore struct {
	mu     sync.Mutex
	budget map[string]int64
}

// NewInMemoryCostStore creates an InMemoryCostStore with the given initial
// budgets. A negative budget value means "unlimited" for that scope.
// Scopes not present in the map also default to unlimited.
func NewInMemoryCostStore(budgets map[string]int64) *InMemoryCostStore {
	cp := make(map[string]int64, len(budgets))
	for k, v := range budgets {
		cp[k] = v
	}
	return &InMemoryCostStore{budget: cp}
}

// Available implements CostStore.
func (s *InMemoryCostStore) Available(_ context.Context, scope string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.budget[scope]
	if !ok {
		return -1, nil
	}
	return v, nil
}

// Reserve implements CostStore.
// Scopes not present in the initial budgets map are treated as unlimited
// and are not tracked — they always succeed.
func (s *InMemoryCostStore) Reserve(_ context.Context, scope string, cost int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.budget[scope]
	if !ok {
		// Unknown scope — unlimited, no tracking.
		return true, nil
	}
	if v < 0 {
		return true, nil
	}
	if v < cost {
		return false, nil
	}
	s.budget[scope] = v - cost
	return true, nil
}

// Release implements CostStore.
func (s *InMemoryCostStore) Release(_ context.Context, scope string, cost int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.budget[scope]
	if !ok {
		return nil
	}
	if v < 0 {
		return nil
	}
	s.budget[scope] = v + cost
	return nil
}
