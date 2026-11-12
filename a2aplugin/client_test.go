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
	"strconv"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
)

type callResult struct {
	Payload any
	Err     string
}

func newClientHandler(payloads ...any) (a2aclient.CallHandler, *int) {
	calls := 0
	return func(ctx context.Context, req a2aclient.Request) iter.Seq2[any, error] {
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

func collect(seq iter.Seq2[any, error]) []callResult {
	var results []callResult
	for payload, err := range seq {
		result := callResult{Payload: payload}
		if err != nil {
			result.Err = err.Error()
		}
		results = append(results, result)
	}
	return results
}

func TestClient_Name(t *testing.T) {
	t.Parallel()
	if got := Client("experiments", ClientHooks{}).Name(); got != "experiments" {
		t.Fatalf("Client().Name() = %q, want %q", got, "experiments")
	}
}

func TestClient_NoCallHooksReturnsNext(t *testing.T) {
	t.Parallel()
	next, _ := newClientHandler()

	got := Client("noop", ClientHooks{}).WrapCall(next)

	if reflect.ValueOf(got).Pointer() != reflect.ValueOf(next).Pointer() {
		t.Fatal("WrapCall() returned a wrapper, want next unchanged")
	}
}

func TestClient_BeforeCall(t *testing.T) {
	t.Parallel()
	var gotParams a2aclient.ServiceParams
	var gotCtxValue, afterCtxValue any
	next := func(ctx context.Context, req a2aclient.Request) iter.Seq2[any, error] {
		gotCtxValue = ctx.Value(ctxKey{})
		gotParams = req.ServiceParams
		return func(yield func(any, error) bool) { yield(&a2a.Task{}, nil) }
	}
	plugin := Client("experiments", ClientHooks{
		BeforeCall: func(ctx context.Context, req *a2aclient.Request) (context.Context, any, error) {
			req.ServiceParams.Append("x-experiment-id", "exp-1")
			return context.WithValue(ctx, ctxKey{}, "before"), nil, nil
		},
		AfterCall: func(ctx context.Context, _ *a2aclient.Response) error {
			afterCtxValue = ctx.Value(ctxKey{})
			return nil
		},
	})
	req := a2aclient.Request{Method: "GetTask", ServiceParams: a2aclient.ServiceParams{"a2a-version": {"1.0"}}}

	collect(plugin.WrapCall(next)(t.Context(), req))

	wantParams := a2aclient.ServiceParams{"a2a-version": {"1.0"}, "x-experiment-id": {"exp-1"}}
	if diff := cmp.Diff(wantParams, gotParams); diff != "" {
		t.Fatalf("next() wrong service params (-want +got) diff = %s", diff)
	}
	if diff := cmp.Diff(a2aclient.ServiceParams{"a2a-version": {"1.0"}}, req.ServiceParams); diff != "" {
		t.Fatalf("caller's service params modified (-want +got) diff = %s", diff)
	}
	if gotCtxValue != "before" {
		t.Fatalf("next() ctx value = %v, want %q", gotCtxValue, "before")
	}
	if afterCtxValue != "before" {
		t.Fatalf("AfterCall() ctx value = %v, want %q", afterCtxValue, "before")
	}
}

func TestClient_CallHooks(t *testing.T) {
	t.Parallel()
	early := &a2a.Task{ID: "early"}
	replaced := &a2a.Task{ID: "replaced"}
	testCases := []struct {
		name         string
		payloads     []any
		hooks        ClientHooks
		wantResults  []callResult
		wantNextCall bool
	}{
		{
			name:     "early answer from BeforeCall",
			payloads: []any{&a2a.Task{}},
			hooks: ClientHooks{
				BeforeCall: func(ctx context.Context, _ *a2aclient.Request) (context.Context, any, error) {
					return ctx, early, nil
				},
			},
			wantResults: []callResult{{Payload: early}},
		},
		{
			name:     "error from BeforeCall",
			payloads: []any{&a2a.Task{}},
			hooks: ClientHooks{
				BeforeCall: func(ctx context.Context, _ *a2aclient.Request) (context.Context, any, error) {
					return ctx, nil, errors.New("rejected")
				},
			},
			wantResults: []callResult{{Err: "rejected"}},
		},
		{
			name:     "AfterCall sees the early answer",
			payloads: []any{&a2a.Task{}},
			hooks: ClientHooks{
				BeforeCall: func(ctx context.Context, _ *a2aclient.Request) (context.Context, any, error) {
					return ctx, early, nil
				},
				AfterCall: func(_ context.Context, resp *a2aclient.Response) error {
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
			hooks: ClientHooks{
				AfterCall: func(_ context.Context, resp *a2aclient.Response) error {
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
			hooks: ClientHooks{
				AfterCall: func(context.Context, *a2aclient.Response) error {
					return errors.New("invalid response")
				},
			},
			wantResults:  []callResult{{Err: "invalid response"}},
			wantNextCall: true,
		},
		{
			name:     "AfterCall runs on every stream event",
			payloads: []any{&a2a.Task{ID: "1"}, &a2a.Task{ID: "2"}},
			hooks: ClientHooks{
				AfterCall: func(_ context.Context, resp *a2aclient.Response) error {
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
			next, nextCalls := newClientHandler(tc.payloads...)

			got := collect(Client("hooks", tc.hooks).WrapCall(next)(t.Context(), a2aclient.Request{Method: "GetTask"}))

			if diff := cmp.Diff(tc.wantResults, got); diff != "" {
				t.Fatalf("WrapCall() wrong results (-want +got) diff = %s", diff)
			}
			if gotNextCall := *nextCalls > 0; gotNextCall != tc.wantNextCall {
				t.Fatalf("next() called = %v, want %v", gotNextCall, tc.wantNextCall)
			}
		})
	}
}

func TestClient_WrapCallOutsideHooks(t *testing.T) {
	t.Parallel()
	var calls []string
	next := func(context.Context, a2aclient.Request) iter.Seq2[any, error] {
		return func(yield func(any, error) bool) {
			calls = append(calls, "send")
			yield(&a2a.Task{}, nil)
		}
	}
	plugin := Client("order", ClientHooks{
		BeforeCall: func(ctx context.Context, _ *a2aclient.Request) (context.Context, any, error) {
			calls = append(calls, "before")
			return ctx, nil, nil
		},
		WrapCall: func(next a2aclient.CallHandler) a2aclient.CallHandler {
			return func(ctx context.Context, req a2aclient.Request) iter.Seq2[any, error] {
				calls = append(calls, "wrap")
				return next(ctx, req)
			}
		},
	})

	collect(plugin.WrapCall(next)(t.Context(), a2aclient.Request{Method: "GetTask"}))

	if diff := cmp.Diff([]string{"wrap", "before", "send"}, calls); diff != "" {
		t.Fatalf("WrapCall() wrong call order (-want +got) diff = %s", diff)
	}
}

func TestClient_RetryWithOriginalRequest(t *testing.T) {
	t.Parallel()
	var sentParams []a2aclient.ServiceParams
	next := func(_ context.Context, req a2aclient.Request) iter.Seq2[any, error] {
		return func(yield func(any, error) bool) {
			sentParams = append(sentParams, req.ServiceParams)
			yield(nil, errors.New("unavailable"))
		}
	}
	attempt := 0
	plugin := Client("experiments", ClientHooks{
		BeforeCall: func(ctx context.Context, req *a2aclient.Request) (context.Context, any, error) {
			attempt++
			req.ServiceParams.Append("x-attempt", strconv.Itoa(attempt))
			return ctx, nil, nil
		},
	})
	retry := func(next a2aclient.CallHandler) a2aclient.CallHandler {
		return func(ctx context.Context, req a2aclient.Request) iter.Seq2[any, error] {
			collect(next(ctx, req))
			return next(ctx, req)
		}
	}
	req := a2aclient.Request{Method: "GetTask", ServiceParams: a2aclient.ServiceParams{}}

	collect(retry(plugin.WrapCall(next))(t.Context(), req))

	want := []a2aclient.ServiceParams{{"x-attempt": {"1"}}, {"x-attempt": {"2"}}}
	if diff := cmp.Diff(want, sentParams); diff != "" {
		t.Fatalf("next() wrong service params per attempt (-want +got) diff = %s", diff)
	}
}
