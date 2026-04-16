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

	codec := &callCtxCodec{}

	svcParams := NewServiceParams(map[string][]string{"x-api-key": {"key1", "key2"}, "x-region": {"us-east-1"}})
	ctx, callCtx := NewCallContext(t.Context(), svcParams)
	callCtx.User = &User{Name: "alice", Authenticated: true, Attributes: map[string]any{"role": "admin"}}
	callCtx.tenant = "acme"

	encoded := codec.Encode(ctx)

	// Simulate JSON serialization round-trip (as a real queue backend would do).
	jsonBytes, err := json.Marshal(encoded)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var deserialized map[string]any
	if err := json.Unmarshal(jsonBytes, &deserialized); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	decodedCtx := codec.Decode(context.Background(), deserialized)
	got, ok := CallContextFrom(decodedCtx)
	if !ok {
		t.Fatalf("CallContextFrom() = _, false, want true")
	}

	if diff := cmp.Diff(callCtx.User, got.User); diff != "" {
		t.Errorf("callCtxCodec.Decode() user mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(callCtx.ServiceParams().kv, got.ServiceParams().kv); diff != "" {
		t.Errorf("callCtxCodec.Decode() svcParams mismatch (-want +got):\n%s", diff)
	}
	if callCtx.Tenant() != got.Tenant() {
		t.Errorf("callCtxCodec.Decode() tenant = %q, want %q", got.Tenant(), callCtx.Tenant())
	}
}

func TestCallContextPropagation(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	taskChan := make(chan *workqueue.Payload)
	queue, startExecutionFn := workqueue.NewPushQueue(workqueue.WriterFunc(func(ctx context.Context, p *workqueue.Payload) (a2a.TaskID, error) {
		taskChan <- p
		return p.TaskID, nil
	}))
	go func() {
		task := <-taskChan
		if _, err := startExecutionFn(ctx, task); err != nil {
			t.Errorf("startExecutionFn() error = %v", err)
		}
	}()

	gotExecCtxChan := make(chan *ExecutorContext, 1)
	handler := NewHandler(
		&mockAgentExecutor{
			ExecuteFunc: func(ctx context.Context, execCtx *ExecutorContext) iter.Seq2[a2a.Event, error] {
				return func(yield func(a2a.Event, error) bool) {
					gotExecCtxChan <- execCtx
					yield(a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("Hello")), nil)
				}
			},
		},
		WithClusterMode(ClusterConfig{
			TaskStore:    taskstore.NewInMemory(nil),
			QueueManager: eventqueue.NewInMemoryManager(),
			WorkQueue:    queue,
		}),
	)

	svcParams := NewServiceParams(map[string][]string{"foo": {"bar"}})
	ctx, callCtx := NewCallContext(ctx, svcParams)
	callCtx.User = &User{Name: "Test", Authenticated: true, Attributes: map[string]any{"bot": true}}
	callCtx.tenant = "t1"

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
}
