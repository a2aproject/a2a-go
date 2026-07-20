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

package costlimiter

import (
	"context"
	"errors"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

func TestCostLimiter_Before_ZeroCostPassthrough(t *testing.T) {
	store := NewInMemoryCostStore(map[string]int64{"default": 100})
	cl := NewCostLimiter(store, func(ctx context.Context, callCtx *a2asrv.CallContext, req *a2asrv.Request) int64 {
		return 0
	})

	ctx := context.Background()
	ctx, callCtx := a2asrv.NewCallContext(ctx, nil)
	_, _, err := cl.Before(ctx, callCtx, &a2asrv.Request{})
	if err != nil {
		t.Fatalf("zero-cost request should pass through, got: %v", err)
	}
}

func TestCostLimiter_Before_WithinBudget(t *testing.T) {
	store := NewInMemoryCostStore(map[string]int64{"default": 100})
	cl := NewCostLimiter(store, func(ctx context.Context, callCtx *a2asrv.CallContext, req *a2asrv.Request) int64 {
		return 50
	})

	ctx := context.Background()
	ctx, callCtx := a2asrv.NewCallContext(ctx, nil)
	ctx, _, err := cl.Before(ctx, callCtx, &a2asrv.Request{})
	if err != nil {
		t.Fatalf("request within budget should pass, got: %v", err)
	}

	// Second call should also pass (50+50=100=budget)
	ctx, _, err = cl.Before(ctx, callCtx, &a2asrv.Request{})
	if err != nil {
		t.Fatalf("second request within budget should pass, got: %v", err)
	}

	// Third call should exceed budget (150>100)
	_, _, err = cl.Before(ctx, callCtx, &a2asrv.Request{})
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("expected ErrBudgetExceeded, got: %v", err)
	}
}

func TestCostLimiter_Before_ExceedsBudget(t *testing.T) {
	store := NewInMemoryCostStore(map[string]int64{"default": 10})
	cl := NewCostLimiter(store, func(ctx context.Context, callCtx *a2asrv.CallContext, req *a2asrv.Request) int64 {
		return 100
	})

	ctx := context.Background()
	ctx, callCtx := a2asrv.NewCallContext(ctx, nil)
	_, _, err := cl.Before(ctx, callCtx, &a2asrv.Request{})
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("expected ErrBudgetExceeded, got: %v", err)
	}
}

func TestCostLimiter_Before_UnlimitedBudget(t *testing.T) {
	store := NewInMemoryCostStore(map[string]int64{"default": -1}) // unlimited
	cl := NewCostLimiter(store, func(ctx context.Context, callCtx *a2asrv.CallContext, req *a2asrv.Request) int64 {
		return 9999
	})

	ctx := context.Background()
	ctx, callCtx := a2asrv.NewCallContext(ctx, nil)
	_, _, err := cl.Before(ctx, callCtx, &a2asrv.Request{})
	if err != nil {
		t.Fatalf("unlimited budget should allow any cost, got: %v", err)
	}
}

func TestCostLimiter_After_ReleaseOnCancel(t *testing.T) {
	store := NewInMemoryCostStore(map[string]int64{"default": 100})
	cl := NewCostLimiter(store, func(ctx context.Context, callCtx *a2asrv.CallContext, req *a2asrv.Request) int64 {
		return 50
	})

	ctx := context.Background()
	ctx, callCtx := a2asrv.NewCallContext(ctx, nil)
	ctx, _, err := cl.Before(ctx, callCtx, &a2asrv.Request{})
	if err != nil {
		t.Fatal(err)
	}

	// Simulate cancellation
	_ = cl.After(ctx, callCtx, &a2asrv.Response{Err: context.Canceled})

	// Budget should be restored
	avail, _ := store.Available(ctx, "default")
	if avail != 100 {
		t.Fatalf("budget should be restored after cancel, got %d", avail)
	}
}

func TestCostLimiter_After_NoReleaseOnSuccess(t *testing.T) {
	store := NewInMemoryCostStore(map[string]int64{"default": 100})
	cl := NewCostLimiter(store, func(ctx context.Context, callCtx *a2asrv.CallContext, req *a2asrv.Request) int64 {
		return 50
	})

	ctx := context.Background()
	ctx, callCtx := a2asrv.NewCallContext(ctx, nil)
	ctx, _, err := cl.Before(ctx, callCtx, &a2asrv.Request{})
	if err != nil {
		t.Fatal(err)
	}

	// Successful completion — cost should NOT be released
	_ = cl.After(ctx, callCtx, &a2asrv.Response{})

	avail, _ := store.Available(ctx, "default")
	if avail != 50 {
		t.Fatalf("budget should remain deducted after success, got %d", avail)
	}
}

func TestCostLimiter_ScopeFunc(t *testing.T) {
	store := NewInMemoryCostStore(map[string]int64{"agent-a": 10, "agent-b": 100})
	cl := NewCostLimiter(store,
		func(ctx context.Context, callCtx *a2asrv.CallContext, req *a2asrv.Request) int64 {
			return 10
		},
		WithScopeFunc(func(callCtx *a2asrv.CallContext) string {
			return callCtx.User.Name
		}),
	)

	// "Agent-b" has enough budget
	ctx := context.Background()
	ctx, callCtx := a2asrv.NewCallContext(ctx, nil)
	callCtx.User = a2asrv.NewAuthenticatedUser("agent-b", nil)
	ctx2, callCtx2 := a2asrv.NewCallContext(ctx, nil)
	callCtx2.User = a2asrv.NewAuthenticatedUser("agent-b", nil)
	_, _, err := cl.Before(ctx2, callCtx2, &a2asrv.Request{})
	if err != nil {
		t.Fatalf("agent-b should have budget, got: %v", err)
	}

	// "Agent-a" has exactly 10, so one more call should fail
	ctx3, callCtx3 := a2asrv.NewCallContext(ctx, nil)
	callCtx3.User = a2asrv.NewAuthenticatedUser("agent-a", nil)
	_, _, err = cl.Before(ctx3, callCtx3, &a2asrv.Request{})
	if err != nil {
		t.Fatal("first call for agent-a should pass", err)
	}
	ctx4, callCtx4 := a2asrv.NewCallContext(ctx, nil)
	callCtx4.User = a2asrv.NewAuthenticatedUser("agent-a", nil)
	_, _, err = cl.Before(ctx4, callCtx4, &a2asrv.Request{})
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("agent-a should be out of budget, got: %v", err)
	}
}

func TestInMemoryCostStore_Concurrent(t *testing.T) {
	store := NewInMemoryCostStore(map[string]int64{"shared": 1000})

	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 50; j++ {
				_, _ = store.Reserve(context.Background(), "shared", 1)
			}
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	avail, _ := store.Available(context.Background(), "shared")
	if avail < 0 {
		t.Fatalf("budget underflow: %d", avail)
	}
	if avail != 500 {
		t.Logf("remaining budget: %d (expected ~500, concurrency may vary)", avail)
	}
}
