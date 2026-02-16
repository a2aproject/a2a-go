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

package a2asrv

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2aclient"
	"github.com/a2aproject/a2a-go/a2asrv/taskstore"
	"github.com/a2aproject/a2a-go/internal/jsonrpc"
	"github.com/a2aproject/a2a-go/internal/sse"
	"github.com/a2aproject/a2a-go/internal/testutil"
	"github.com/google/go-cmp/cmp"
)

func TestJSONRPC_RequestRouting(t *testing.T) {
	testCases := []struct {
		method string
		call   func(ctx context.Context, client *a2aclient.Client) (any, error)
	}{
		{
			method: "OnGetTask",
			call: func(ctx context.Context, client *a2aclient.Client) (any, error) {
				return client.GetTask(ctx, &a2a.TaskQueryParams{})
			},
		},
		{
			method: "OnListTasks",
			call: func(ctx context.Context, client *a2aclient.Client) (any, error) {
				return client.ListTasks(ctx, &a2a.ListTasksRequest{})
			},
		},
		{
			method: "OnCancelTask",
			call: func(ctx context.Context, client *a2aclient.Client) (any, error) {
				return client.CancelTask(ctx, &a2a.TaskIDParams{})
			},
		},
		{
			method: "OnSendMessage",
			call: func(ctx context.Context, client *a2aclient.Client) (any, error) {
				return client.SendMessage(ctx, &a2a.MessageSendParams{})
			},
		},
		{
			method: "OnSendMessageStream",
			call: func(ctx context.Context, client *a2aclient.Client) (any, error) {
				return handleSingleItemSeq(client.SendStreamingMessage(ctx, &a2a.MessageSendParams{}))
			},
		},
		{
			method: "OnResubscribeToTask",
			call: func(ctx context.Context, client *a2aclient.Client) (any, error) {
				return handleSingleItemSeq(client.ResubscribeToTask(ctx, &a2a.TaskIDParams{}))
			},
		},
		{
			method: "OnListTaskPushConfig",
			call: func(ctx context.Context, client *a2aclient.Client) (any, error) {
				return client.ListTaskPushConfig(ctx, &a2a.ListTaskPushConfigParams{})
			},
		},
		{
			method: "OnSetTaskPushConfig",
			call: func(ctx context.Context, client *a2aclient.Client) (any, error) {
				return client.SetTaskPushConfig(ctx, &a2a.TaskPushConfig{})
			},
		},
		{
			method: "OnGetTaskPushConfig",
			call: func(ctx context.Context, client *a2aclient.Client) (any, error) {
				return client.GetTaskPushConfig(ctx, &a2a.GetTaskPushConfigParams{})
			},
		},
		{
			method: "OnDeleteTaskPushConfig",
			call: func(ctx context.Context, client *a2aclient.Client) (any, error) {
				return nil, client.DeleteTaskPushConfig(ctx, &a2a.DeleteTaskPushConfigParams{})
			},
		},
		{
			method: "OnGetExtendedAgentCard",
			call: func(ctx context.Context, client *a2aclient.Client) (any, error) {
				return client.GetAgentCard(ctx)
			},
		},
	}

	ctx := t.Context()
	lastCalledMethod := make(chan string, 1)
	interceptor := &mockInterceptor{
		beforeFn: func(ctx context.Context, callCtx *CallContext, req *Request) (context.Context, any, error) {
			lastCalledMethod <- callCtx.Method()
			return ctx, nil, nil
		},
	}
	reqHandler := NewHandler(
		&mockAgentExecutor{},
		WithCallInterceptors(interceptor),
		WithExtendedAgentCard(&a2a.AgentCard{}),
	)
	server := httptest.NewServer(NewJSONRPCHandler(reqHandler))

	client, err := a2aclient.NewFromEndpoints(ctx, []a2a.AgentInterface{
		{URL: server.URL, Transport: a2a.TransportProtocolJSONRPC},
	})
	if err != nil {
		t.Fatalf("a2aclient.NewFromEndpoints() error = %v", err)
	}

	for _, tc := range testCases {
		t.Run(tc.method, func(t *testing.T) {
			_, _ = tc.call(ctx, client)
			select {
			case calledMethod := <-lastCalledMethod:
				if calledMethod != tc.method {
					t.Fatalf("wrong method called: got %q, want %q", calledMethod, tc.method)
				}
			case <-time.After(2 * time.Second):
				t.Fatalf("method %q not called", tc.method)
			}
		})
	}
}

func TestJSONRPC_Validations(t *testing.T) {
	taskID := a2a.NewTaskID()
	query := json.RawMessage(fmt.Sprintf(`{"id": %q}`, taskID))
	task := &a2a.Task{ID: taskID}
	want := mustUnmarshal(t, mustMarshal(t, task))
	listResponse := &a2a.ListTasksResponse{Tasks: []*a2a.Task{task}, TotalSize: 1, PageSize: 3}
	listTasksWant := mustUnmarshal(t, mustMarshal(t, listResponse))
	auth := func(ctx context.Context) (string, error) { return "TestUser", nil }

	testCases := []struct {
		name    string
		method  string
		request []byte
		wantErr error
		want    any
	}{
		{
			name:    "success",
			method:  "POST",
			request: mustMarshal(t, jsonrpcRequest{JSONRPC: "2.0", Method: jsonrpc.MethodTasksGet, Params: query, ID: "123"}),
			want:    want,
		},
		{
			name:    "success with number ID",
			method:  "POST",
			request: mustMarshal(t, jsonrpcRequest{JSONRPC: "2.0", Method: jsonrpc.MethodTasksGet, Params: query, ID: 123}),
			want:    want,
		},
		{
			name:    "success with nil ID",
			method:  "POST",
			request: mustMarshal(t, jsonrpcRequest{JSONRPC: "2.0", Method: jsonrpc.MethodTasksGet, Params: query, ID: nil}),
			want:    want,
		},
		{
			name:    "success tasks/list",
			method:  "POST",
			request: mustMarshal(t, jsonrpcRequest{JSONRPC: "2.0", Method: jsonrpc.MethodTasksList, Params: json.RawMessage(`{"pageSize": 3}`), ID: "123"}),
			want:    listTasksWant,
		},
		{
			name:    "tasks/list with invalid page size",
			method:  "POST",
			request: mustMarshal(t, jsonrpcRequest{JSONRPC: "2.0", Method: jsonrpc.MethodTasksList, Params: json.RawMessage(`{"pageSize": 125}`), ID: "123"}),
			wantErr: a2a.ErrInvalidRequest,
		},
		{
			name:    "invalid ID",
			method:  "POST",
			request: mustMarshal(t, jsonrpcRequest{JSONRPC: "2.0", Method: jsonrpc.MethodTasksGet, Params: query, ID: false}),
			wantErr: a2a.ErrInvalidRequest,
		},
		{
			name:    "http get",
			method:  "GET",
			request: mustMarshal(t, jsonrpcRequest{JSONRPC: "2.0", Method: jsonrpc.MethodTasksGet, Params: query}),
			wantErr: a2a.ErrInvalidRequest,
		},
		{
			name:    "http delete",
			method:  "DELETE",
			request: mustMarshal(t, jsonrpcRequest{JSONRPC: "2.0", Method: jsonrpc.MethodTasksGet, Params: query}),
			wantErr: a2a.ErrInvalidRequest,
		},
		{
			name:    "http put",
			method:  "PUT",
			request: mustMarshal(t, jsonrpcRequest{JSONRPC: "2.0", Method: jsonrpc.MethodTasksGet, Params: query}),
			wantErr: a2a.ErrInvalidRequest,
		},
		{
			name:    "http patch",
			method:  "PATCH",
			request: mustMarshal(t, jsonrpcRequest{JSONRPC: "2.0", Method: jsonrpc.MethodTasksGet, Params: query}),
			wantErr: a2a.ErrInvalidRequest,
		},
		{
			name:    "wrong version",
			method:  "POST",
			request: mustMarshal(t, jsonrpcRequest{JSONRPC: "99", Method: jsonrpc.MethodTasksGet, Params: query}),
			wantErr: a2a.ErrInvalidRequest,
		},
		{
			name:    "invalid method",
			method:  "POST",
			request: mustMarshal(t, jsonrpcRequest{JSONRPC: "2.0", Method: "calculate", Params: query}),
			wantErr: a2a.ErrMethodNotFound,
		},
		{
			name:    "no method in jsonrpcRequest",
			method:  "POST",
			request: mustMarshal(t, jsonrpcRequest{JSONRPC: "2.0", Params: query}),
			wantErr: a2a.ErrInvalidRequest,
		},
		{
			name:    "invalid params error",
			method:  "POST",
			request: mustMarshal(t, jsonrpcRequest{JSONRPC: "2.0", Method: jsonrpc.MethodTasksGet, Params: json.RawMessage("[]")}),
			wantErr: a2a.ErrInvalidParams,
		},
	}

	store := testutil.NewTestTaskStoreWithConfig(&taskstore.InMemoryStoreConfig{
		Authenticator: auth,
	}).WithTasks(t, task)
	reqHandler := NewHandler(&mockAgentExecutor{}, WithTaskStore(store))
	server := httptest.NewServer(NewJSONRPCHandler(reqHandler))

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			req, err := http.NewRequestWithContext(ctx, tc.method, server.URL, bytes.NewBuffer(tc.request))
			if err != nil {
				t.Errorf("http.NewRequestWithContext() error = %v", err)
			}
			client := &http.Client{}
			resp, err := client.Do(req)
			if err != nil {
				t.Errorf("client.Do() error = %v", err)
			}
			if resp.StatusCode != 200 {
				t.Errorf("resp.StatusCode = %d, want 200", resp.StatusCode)
			}
			var payload jsonrpcResponse
			if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
				t.Errorf("decoder.Decode() error = %v", err)
			}
			if tc.wantErr != nil {
				if payload.Error == nil {
					t.Errorf("payload.Error = nil, want %v", tc.wantErr)
				}
				if !errors.Is(payload.Error.ToA2AError(), tc.wantErr) {
					t.Errorf("payload.Error = %v, want %v", payload.Error.ToA2AError(), tc.wantErr)
				}
			} else {
				if payload.Error != nil {
					t.Errorf("payload.Error = %v, want nil", payload.Error.ToA2AError())
				}
				if diff := cmp.Diff(tc.want, payload.Result); diff != "" {
					t.Errorf("payload.Result = %v, want %v", payload.Result, tc.want)
				}
			}
		})
	}
}

func TestJSONRPC_StreamingKeepAlive(t *testing.T) {
	agentTimeout := 20 * time.Millisecond
	testCases := []struct {
		name        string
		option      JSONRPCHandlerOption
		wantEnabled bool
	}{
		{
			name:   "default disabled", // TODO: change default for 1.0
			option: nil,
		},
		{
			name:   "zero for disabled",
			option: WithKeepAlive(0),
		},
		{
			name:   "negative for disabled",
			option: WithKeepAlive(-1),
		},
		{
			name:        "positive for enabled",
			option:      WithKeepAlive(5 * time.Millisecond),
			wantEnabled: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()

			mockExecutor := &mockAgentExecutor{
				ExecuteFunc: func(ctx context.Context, execCtx *ExecutorContext) iter.Seq2[a2a.Event, error] {
					return func(yield func(a2a.Event, error) bool) {
						time.Sleep(agentTimeout)
						if !yield(a2a.NewMessage(a2a.MessageRoleAgent, a2a.TextPart{Text: "test message"}), nil) {
							return
						}
					}
				},
			}

			opts := []JSONRPCHandlerOption{}
			if tc.option != nil {
				opts = append(opts, tc.option)
			}
			reqHandler := NewHandler(mockExecutor)
			server := httptest.NewServer(NewJSONRPCHandler(reqHandler, opts...))
			defer server.Close()

			request := jsonrpcRequest{
				JSONRPC: "2.0",
				Method:  jsonrpc.MethodMessageStream,
				Params: mustMarshal(t, &a2a.MessageSendParams{
					Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: "hello"}),
				}),
				ID: 1,
			}

			req, err := http.NewRequestWithContext(ctx, "POST", server.URL, bytes.NewBuffer(mustMarshal(t, request)))
			if err != nil {
				t.Fatalf("http.NewRequestWithContext() error = %v", err)
			}
			req.Header.Set("Accept", sse.ContentEventStream)
			client := http.Client{}
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("client.Do() error = %v", err)
			}
			defer func() { _ = resp.Body.Close() }()

			scanner := bufio.NewScanner(resp.Body)
			var keepAlives int16
			for scanner.Scan() {
				line := scanner.Bytes()
				if string(line) == ": keep-alive" {
					keepAlives++
					break
				}
			}
			if tc.wantEnabled && keepAlives == 0 {
				t.Error("keep-alive enabled but none received")
			}
			if !tc.wantEnabled && keepAlives > 0 {
				t.Errorf("keep-alive disabled but received %d", keepAlives)
			}
		})
	}
}

func mustMarshal(t *testing.T, data any) []byte {
	t.Helper()
	body, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return body
}

func mustUnmarshal(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	return result
}
