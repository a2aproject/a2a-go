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
	"iter"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

func newServerHandler(payloads ...any) (a2asrv.CallHandler, *int) {
	calls := 0
	return func(context.Context, *a2asrv.CallContext, a2asrv.Request) iter.Seq2[any, error] {
		return func(yield func(any, error) bool) {
			calls++
			for _, payload := range payloads {
				if !yield(payload, nil) {
					return
				}
			}
		}
	}, &calls
}

func TestServer_NoCallHooksReturnsNext(t *testing.T) {
	t.Parallel()
	next, _ := newServerHandler()

	got := Server("noop", ServerHooks{}).WrapCall(next)

	if reflect.ValueOf(got).Pointer() != reflect.ValueOf(next).Pointer() {
		t.Fatal("WrapCall() returned a wrapper, want next unchanged")
	}
}

func TestServer_BeforeCall(t *testing.T) {
	t.Parallel()
	original := &a2a.GetTaskRequest{ID: "original"}
	replaced := &a2a.GetTaskRequest{ID: "replaced"}
	var gotPayload, gotCtxValue, afterCtxValue any
	var gotCallCtx *a2asrv.CallContext
	next := func(ctx context.Context, callCtx *a2asrv.CallContext, req a2asrv.Request) iter.Seq2[any, error] {
		gotCtxValue = ctx.Value(ctxKey{})
		gotCallCtx = callCtx
		gotPayload = req.Payload
		return func(yield func(any, error) bool) { yield(&a2a.Task{}, nil) }
	}
	plugin := Server("hooks", ServerHooks{
		BeforeCall: func(ctx context.Context, _ *a2asrv.CallContext, req *a2asrv.Request) (context.Context, any, error) {
			req.Payload = replaced
			return context.WithValue(ctx, ctxKey{}, "before"), nil, nil
		},
		AfterCall: func(ctx context.Context, _ *a2asrv.CallContext, _ *a2asrv.Response) error {
			afterCtxValue = ctx.Value(ctxKey{})
			return nil
		},
	})
	ctx, callCtx := a2asrv.NewCallContext(t.Context(), nil)
	req := a2asrv.Request{Payload: original}

	collect(plugin.WrapCall(next)(ctx, callCtx, req))

	if gotPayload != replaced {
		t.Fatalf("next() payload = %v, want %v", gotPayload, replaced)
	}
	if req.Payload != original {
		t.Fatalf("caller's payload = %v, want %v", req.Payload, original)
	}
	if gotCallCtx != callCtx {
		t.Fatalf("next() callCtx = %p, want %p", gotCallCtx, callCtx)
	}
	if gotCtxValue != "before" {
		t.Fatalf("next() ctx value = %v, want %q", gotCtxValue, "before")
	}
	if afterCtxValue != "before" {
		t.Fatalf("AfterCall() ctx value = %v, want %q", afterCtxValue, "before")
	}
}

func TestServer_CallHooks(t *testing.T) {
	t.Parallel()
	early := &a2a.Task{ID: "early"}
	replaced := &a2a.Task{ID: "replaced"}
	testCases := []struct {
		name         string
		payloads     []any
		hooks        ServerHooks
		wantResults  []callResult
		wantNextCall bool
	}{
		{
			name:     "early answer from BeforeCall",
			payloads: []any{&a2a.Task{}},
			hooks: ServerHooks{
				BeforeCall: func(ctx context.Context, _ *a2asrv.CallContext, _ *a2asrv.Request) (context.Context, any, error) {
					return ctx, early, nil
				},
			},
			wantResults: []callResult{{Payload: early}},
		},
		{
			name:     "error from BeforeCall",
			payloads: []any{&a2a.Task{}},
			hooks: ServerHooks{
				BeforeCall: func(ctx context.Context, _ *a2asrv.CallContext, _ *a2asrv.Request) (context.Context, any, error) {
					return ctx, nil, errors.New("rejected")
				},
			},
			wantResults: []callResult{{Err: "rejected"}},
		},
		{
			name:     "AfterCall sees the early answer",
			payloads: []any{&a2a.Task{}},
			hooks: ServerHooks{
				BeforeCall: func(ctx context.Context, _ *a2asrv.CallContext, _ *a2asrv.Request) (context.Context, any, error) {
					return ctx, early, nil
				},
				AfterCall: func(_ context.Context, _ *a2asrv.CallContext, resp *a2asrv.Response) error {
					if resp.Payload != early {
						return errors.New("unexpected payload")
					}
					resp.Payload = replaced
					return nil
				},
			},
			wantResults: []callResult{{Payload: replaced}},
		},
		{
			name:     "AfterCall replaces the response",
			payloads: []any{&a2a.Task{}},
			hooks: ServerHooks{
				AfterCall: func(_ context.Context, _ *a2asrv.CallContext, resp *a2asrv.Response) error {
					resp.Payload = replaced
					return nil
				},
			},
			wantResults:  []callResult{{Payload: replaced}},
			wantNextCall: true,
		},
		{
			name:     "error from AfterCall",
			payloads: []any{&a2a.Task{}},
			hooks: ServerHooks{
				AfterCall: func(context.Context, *a2asrv.CallContext, *a2asrv.Response) error {
					return errors.New("invalid response")
				},
			},
			wantResults:  []callResult{{Err: "invalid response"}},
			wantNextCall: true,
		},
		{
			name:     "AfterCall runs on every stream event",
			payloads: []any{&a2a.Task{ID: "1"}, &a2a.Task{ID: "2"}},
			hooks: ServerHooks{
				AfterCall: func(_ context.Context, _ *a2asrv.CallContext, resp *a2asrv.Response) error {
					task := *resp.Payload.(*a2a.Task)
					task.ID += "-after"
					resp.Payload = &task
					return nil
				},
			},
			wantResults:  []callResult{{Payload: &a2a.Task{ID: "1-after"}}, {Payload: &a2a.Task{ID: "2-after"}}},
			wantNextCall: true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			next, nextCalls := newServerHandler(tc.payloads...)
			ctx, callCtx := a2asrv.NewCallContext(t.Context(), nil)

			got := collect(Server("hooks", tc.hooks).WrapCall(next)(ctx, callCtx, a2asrv.Request{}))

			if diff := cmp.Diff(tc.wantResults, got); diff != "" {
				t.Fatalf("WrapCall() wrong results (-want +got) diff = %s", diff)
			}
			if gotNextCall := *nextCalls > 0; gotNextCall != tc.wantNextCall {
				t.Fatalf("next() called = %v, want %v", gotNextCall, tc.wantNextCall)
			}
		})
	}
}

func TestServer_WrapCallOutsideHooks(t *testing.T) {
	t.Parallel()
	var calls []string
	next := func(context.Context, *a2asrv.CallContext, a2asrv.Request) iter.Seq2[any, error] {
		return func(yield func(any, error) bool) {
			calls = append(calls, "handle")
			yield(&a2a.Task{}, nil)
		}
	}
	plugin := Server("order", ServerHooks{
		BeforeCall: func(ctx context.Context, _ *a2asrv.CallContext, _ *a2asrv.Request) (context.Context, any, error) {
			calls = append(calls, "before")
			return ctx, nil, nil
		},
		WrapCall: func(next a2asrv.CallHandler) a2asrv.CallHandler {
			return func(ctx context.Context, callCtx *a2asrv.CallContext, req a2asrv.Request) iter.Seq2[any, error] {
				calls = append(calls, "wrap")
				return next(ctx, callCtx, req)
			}
		},
	})
	ctx, callCtx := a2asrv.NewCallContext(t.Context(), nil)

	collect(plugin.WrapCall(next)(ctx, callCtx, a2asrv.Request{}))

	if diff := cmp.Diff([]string{"wrap", "before", "handle"}, calls); diff != "" {
		t.Fatalf("WrapCall() wrong call order (-want +got) diff = %s", diff)
	}
}
