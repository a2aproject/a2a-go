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
	"encoding/json"
	"iter"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv/eventqueue"
	"github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"
	"github.com/a2aproject/a2a-go/v2/a2asrv/workqueue"
	"github.com/google/go-cmp/cmp"
)

func TestCallCtxCodec_JSONRoundTrip(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		setupCtx    func(context.Context) context.Context
		codec       *callCtxCodec
		wantCallCtx *CallContext
		wantAttr    string
	}{
		{
			name: "call context and attributes",
			setupCtx: func(ctx context.Context) context.Context {
				ctx = context.WithValue(ctx, testCtxKeyType{}, "attr-val")
				ctx, cc := NewCallContext(ctx, NewServiceParams(map[string][]string{"x-api-key": {"key1", "key2"}}))
				cc.User = &User{Name: "alice", Authenticated: true, Attributes: map[string]any{"role": "admin"}}
				cc.tenant = "acme"
				return ctx
			},
			codec: &callCtxCodec{AttrCodec: &testCtxValueCodec{}},
			wantCallCtx: &CallContext{
				svcParams: NewServiceParams(map[string][]string{"x-api-key": {"key1", "key2"}}),
				User:      &User{Name: "alice", Authenticated: true, Attributes: map[string]any{"role": "admin"}},
				tenant:    "acme",
			},
			wantAttr: "attr-val",
		},
		{
			name: "call context only",
			setupCtx: func(ctx context.Context) context.Context {
				ctx, cc := NewCallContext(ctx, NewServiceParams(map[string][]string{"x-region": {"us-east-1"}}))
				cc.User = &User{Name: "bob", Authenticated: false}
				cc.tenant = "t1"
				return ctx
			},
			codec: &callCtxCodec{},
			wantCallCtx: &CallContext{
				svcParams: NewServiceParams(map[string][]string{"x-region": {"us-east-1"}}),
				User:      &User{Name: "bob", Authenticated: false},
				tenant:    "t1",
			},
		},
		{
			name: "attributes only",
			setupCtx: func(ctx context.Context) context.Context {
				return context.WithValue(ctx, testCtxKeyType{}, "only-attrs")
			},
			codec:    &callCtxCodec{AttrCodec: &testCtxValueCodec{}},
			wantAttr: "only-attrs",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := tc.setupCtx(t.Context())
			encoded, err := tc.codec.Encode(ctx)
			if err != nil {
				t.Fatalf("codec.Encode() error = %v", err)
			}

			jsonBytes, err := json.Marshal(encoded)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}
			var deserialized map[string]any
			if err := json.Unmarshal(jsonBytes, &deserialized); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}

			decodedCtx, err := tc.codec.Decode(context.Background(), deserialized)
			if err != nil {
				t.Fatalf("codec.Decode() error = %v", err)
			}

			if tc.wantCallCtx != nil {
				got, ok := CallContextFrom(decodedCtx)
				if !ok {
					t.Fatalf("CallContextFrom() = _, false, want true")
				}
				if diff := cmp.Diff(tc.wantCallCtx.User, got.User); diff != "" {
					t.Errorf("codec.Decode() wrong user (-want +got):\n%s", diff)
				}
				if diff := cmp.Diff(tc.wantCallCtx.ServiceParams().kv, got.ServiceParams().kv); diff != "" {
					t.Errorf("codec.Decode() wrong svcParams (-want +got):\n%s", diff)
				}
				if tc.wantCallCtx.Tenant() != got.Tenant() {
					t.Errorf("codec.Decode() tenant = %q, want %q", got.Tenant(), tc.wantCallCtx.Tenant())
				}
			} else {
				if _, ok := CallContextFrom(decodedCtx); ok {
					t.Errorf("CallContextFrom() = _, true, want false")
				}
			}

			if tc.wantAttr != "" {
				gotAttr, _ := decodedCtx.Value(testCtxKeyType{}).(string)
				if gotAttr != tc.wantAttr {
					t.Errorf("decoded attr = %q, want %q", gotAttr, tc.wantAttr)
				}
			}
		})
	}
}

func TestCallContextPropagation(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	gotExecCtxChan := make(chan *ExecutorContext, 1)
	gotCtxKeyValueChan := make(chan any, 1)
	handler := NewHandler(
		&mockAgentExecutor{
			ExecuteFunc: func(ctx context.Context, execCtx *ExecutorContext) iter.Seq2[a2a.Event, error] {
				return func(yield func(a2a.Event, error) bool) {
					gotExecCtxChan <- execCtx
					gotCtxKeyValueChan <- ctx.Value(testCtxKeyType{})
					yield(a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("Hello")), nil)
				}
			},
		},
		WithClusterMode(ClusterConfig{
			TaskStore:    taskstore.NewInMemory(nil),
			QueueManager: eventqueue.NewInMemoryManager(),
			WorkQueue:    workqueue.NewInMemory(nil),
			ContextCodec: &testCtxValueCodec{},
		}),
	)

	svcParams := NewServiceParams(map[string][]string{"foo": {"bar"}})
	ctx, callCtx := NewCallContext(ctx, svcParams)
	callCtx.User = &User{Name: "Test", Authenticated: true, Attributes: map[string]any{"bot": true}}
	callCtx.tenant = "t1"
	ctxKeyVal := "42"
	ctx = context.WithValue(ctx, testCtxKeyType{}, ctxKeyVal)

	_, err := handler.SendMessage(ctx, &a2a.SendMessageRequest{
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("Hi!")),
	})
	if err != nil {
		t.Fatalf("handler.SendMessage() error = %v", err)
	}

	gotExecCtx := <-gotExecCtxChan
	if diff := cmp.Diff(callCtx.User, gotExecCtx.User); diff != "" {
		t.Errorf("user mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(callCtx.ServiceParams().kv, gotExecCtx.ServiceParams.kv); diff != "" {
		t.Errorf("svcParams mismatch (-want +got):\n%s", diff)
	}
	if callCtx.Tenant() != gotExecCtx.Tenant {
		t.Errorf("tenant mismatch (-want +got): %q, %q", callCtx.Tenant(), gotExecCtx.Tenant)
	}

	gotCtxKeyValue := <-gotCtxKeyValueChan
	if gotCtxKeyValue != ctxKeyVal {
		t.Errorf("context.Value(testCtxKeyType{}) = %v, want %v", gotCtxKeyValue, ctxKeyVal)
	}
}

type testCtxKeyType struct{}

type testCtxValueCodec struct{}

func (c *testCtxValueCodec) Encode(ctx context.Context) (map[string]any, error) {
	if v := ctx.Value(testCtxKeyType{}); v != nil {
		return map[string]any{"custom": v}, nil
	}
	return nil, nil
}

func (c *testCtxValueCodec) Decode(ctx context.Context, data map[string]any) (context.Context, error) {
	if v, ok := data["custom"]; ok {
		return context.WithValue(ctx, testCtxKeyType{}, v), nil
	}
	return ctx, nil
}
