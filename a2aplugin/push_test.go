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
	"errors"
	"net/http"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv/push"
)

func newDelivery() *push.Delivery {
	return &push.Delivery{Config: &a2a.PushConfig{URL: "https://example.com/push"}, Headers: http.Header{}}
}

func TestServer_NoPushHooksReturnsNext(t *testing.T) {
	t.Parallel()
	next := push.DeliveryHandler(func(context.Context, *push.Delivery) (*push.DeliveryResult, error) {
		return &push.DeliveryResult{}, nil
	})

	got := Server("noop", ServerHooks{}).WrapPushDelivery(next)

	if reflect.ValueOf(got).Pointer() != reflect.ValueOf(next).Pointer() {
		t.Fatal("WrapPushDelivery() returned a wrapper, want next unchanged")
	}
}

func TestServer_PushHooks(t *testing.T) {
	t.Parallel()
	wantResult := &push.DeliveryResult{StatusCode: http.StatusOK}
	var deliveredCtxValue any
	var deliveredHeader string
	next := push.DeliveryHandler(func(ctx context.Context, d *push.Delivery) (*push.DeliveryResult, error) {
		deliveredCtxValue = ctx.Value(ctxKey{})
		deliveredHeader = d.Headers.Get("X-Trace")
		return wantResult, nil
	})
	var afterCtxValue any
	var afterResult *push.DeliveryResult
	plugin := Server("hooks", ServerHooks{
		BeforePush: func(ctx context.Context, d *push.Delivery) (context.Context, error) {
			d.Headers.Add("X-Trace", "trace-1")
			return context.WithValue(ctx, ctxKey{}, "before"), nil
		},
		AfterPush: func(ctx context.Context, _ *push.Delivery, result *push.DeliveryResult) {
			afterCtxValue = ctx.Value(ctxKey{})
			afterResult = result
		},
	})

	got, err := plugin.WrapPushDelivery(next)(t.Context(), newDelivery())
	if err != nil {
		t.Fatalf("WrapPushDelivery() error = %v, want nil", err)
	}

	if deliveredHeader != "trace-1" {
		t.Fatalf("delivered header = %q, want %q", deliveredHeader, "trace-1")
	}
	if deliveredCtxValue != "before" {
		t.Fatalf("delivery ctx value = %v, want %q", deliveredCtxValue, "before")
	}
	if afterCtxValue != "before" {
		t.Fatalf("AfterPush() ctx value = %v, want %q", afterCtxValue, "before")
	}
	if diff := cmp.Diff(wantResult, afterResult); diff != "" {
		t.Fatalf("AfterPush() wrong result (-want +got) diff = %s", diff)
	}
	if diff := cmp.Diff(wantResult, got); diff != "" {
		t.Fatalf("WrapPushDelivery() wrong result (-want +got) diff = %s", diff)
	}
}

func TestServer_BeforePushError(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("rejected")
	delivered := false
	next := push.DeliveryHandler(func(context.Context, *push.Delivery) (*push.DeliveryResult, error) {
		delivered = true
		return &push.DeliveryResult{}, nil
	})
	var afterResult *push.DeliveryResult
	plugin := Server("reject", ServerHooks{
		BeforePush: func(ctx context.Context, _ *push.Delivery) (context.Context, error) {
			return ctx, wantErr
		},
		AfterPush: func(_ context.Context, _ *push.Delivery, result *push.DeliveryResult) {
			afterResult = result
		},
	})

	_, err := plugin.WrapPushDelivery(next)(t.Context(), newDelivery())

	if !errors.Is(err, wantErr) {
		t.Fatalf("WrapPushDelivery() error = %v, want %v", err, wantErr)
	}
	if delivered {
		t.Fatal("push delivered, want BeforePush to stop the delivery")
	}
	if afterResult == nil || !errors.Is(afterResult.Err, wantErr) {
		t.Fatalf("AfterPush() result = %v, want Err = %v", afterResult, wantErr)
	}
}

func TestServer_AfterPushResult(t *testing.T) {
	t.Parallel()
	deliveryErr := errors.New("endpoint down")
	testCases := []struct {
		name       string
		result     *push.DeliveryResult
		err        error
		wantResult *push.DeliveryResult
	}{
		{
			name:       "failure without stopping the execution",
			result:     &push.DeliveryResult{StatusCode: http.StatusBadGateway, Err: deliveryErr},
			wantResult: &push.DeliveryResult{StatusCode: http.StatusBadGateway, Err: deliveryErr},
		},
		{
			name:       "nil result with error",
			err:        deliveryErr,
			wantResult: &push.DeliveryResult{Err: deliveryErr},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			next := push.DeliveryHandler(func(context.Context, *push.Delivery) (*push.DeliveryResult, error) {
				return tc.result, tc.err
			})
			var afterResult *push.DeliveryResult
			plugin := Server("after", ServerHooks{
				AfterPush: func(_ context.Context, _ *push.Delivery, result *push.DeliveryResult) {
					afterResult = result
				},
			})

			_, err := plugin.WrapPushDelivery(next)(t.Context(), newDelivery())

			if !errors.Is(err, tc.err) {
				t.Fatalf("WrapPushDelivery() error = %v, want %v", err, tc.err)
			}
			if diff := cmp.Diff(tc.wantResult, afterResult, cmpopts.EquateErrors()); diff != "" {
				t.Fatalf("AfterPush() wrong result (-want +got) diff = %s", diff)
			}
		})
	}
}

func TestServer_WrapPushDeliveryOutsideHooks(t *testing.T) {
	t.Parallel()
	var calls []string
	next := push.DeliveryHandler(func(context.Context, *push.Delivery) (*push.DeliveryResult, error) {
		calls = append(calls, "deliver")
		return &push.DeliveryResult{}, nil
	})
	plugin := Server("order", ServerHooks{
		BeforePush: func(ctx context.Context, _ *push.Delivery) (context.Context, error) {
			calls = append(calls, "before")
			return ctx, nil
		},
		WrapPushDelivery: func(next push.DeliveryHandler) push.DeliveryHandler {
			return func(ctx context.Context, d *push.Delivery) (*push.DeliveryResult, error) {
				calls = append(calls, "wrap")
				return next(ctx, d)
			}
		},
	})

	if _, err := plugin.WrapPushDelivery(next)(t.Context(), newDelivery()); err != nil {
		t.Fatalf("WrapPushDelivery() error = %v, want nil", err)
	}

	if diff := cmp.Diff([]string{"wrap", "before", "deliver"}, calls); diff != "" {
		t.Fatalf("WrapPushDelivery() wrong call order (-want +got) diff = %s", diff)
	}
}
