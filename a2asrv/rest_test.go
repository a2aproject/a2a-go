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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"iter"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2aclient"
	"github.com/a2aproject/a2a-go/a2asrv/taskstore"
	"github.com/a2aproject/a2a-go/internal/rest"
	"github.com/a2aproject/a2a-go/internal/testutil"
)

func TestREST_RequestRouting(t *testing.T) {
	testCases := []struct {
		method string
		call   func(ctx context.Context, client *a2aclient.Client) (any, error)
	}{
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
			method: "OnGetTask",
			call: func(ctx context.Context, client *a2aclient.Client) (any, error) {
				return client.GetTask(ctx, &a2a.TaskQueryParams{ID: "test-id"})
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
			method: "OnResubscribeToTask",
			call: func(ctx context.Context, client *a2aclient.Client) (any, error) {
				return handleSingleItemSeq(client.ResubscribeToTask(ctx, &a2a.TaskIDParams{}))
			},
		},
		{
			method: "OnSetTaskPushConfig",
			call: func(ctx context.Context, client *a2aclient.Client) (any, error) {
				return client.SetTaskPushConfig(ctx, &a2a.TaskPushConfig{TaskID: a2a.TaskID("test-id")})
			},
		},
		{
			method: "OnGetTaskPushConfig",
			call: func(ctx context.Context, client *a2aclient.Client) (any, error) {
				return client.GetTaskPushConfig(ctx, &a2a.GetTaskPushConfigParams{TaskID: a2a.TaskID("test-id"), ConfigID: "test-config-id"})
			},
		},
		{
			method: "OnListTaskPushConfig",
			call: func(ctx context.Context, client *a2aclient.Client) (any, error) {
				return client.ListTaskPushConfig(ctx, &a2a.ListTaskPushConfigParams{TaskID: a2a.TaskID("test-id")})
			},
		},
		{
			method: "OnDeleteTaskPushConfig",
			call: func(ctx context.Context, client *a2aclient.Client) (any, error) {
				return nil, client.DeleteTaskPushConfig(ctx, &a2a.DeleteTaskPushConfigParams{TaskID: a2a.TaskID("test-id"), ConfigID: "test-config-id"})
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

	server := httptest.NewServer(NewRESTHandler(reqHandler))

	client, err := a2aclient.NewFromEndpoints(ctx, []a2a.AgentInterface{
		{URL: server.URL, Transport: a2a.TransportProtocolHTTPJSON},
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
			case <-time.After(5 * time.Second):
				t.Fatalf("Routing failed")
			}
		})
	}
}

func TestREST_Validations(t *testing.T) {
	taskID := a2a.NewTaskID()
	config := a2a.PushConfig{
		ID:  string(taskID),
		URL: "https://example.com/push",
	}
	task := &a2a.Task{ID: taskID}

	authenticator := func(ctx context.Context) (string, error) {
		return "TestUser", nil
	}

	methods := []string{"POST", "GET", "PUT", "DELETE", "PATCH"}

	testCases := []struct {
		name    string
		methods []string
		path    string
		body    interface{}
	}{
		{
			name:    "SendMessage",
			methods: []string{http.MethodPost},
			path:    "/v1/message:send",
			body:    a2a.MessageSendParams{Message: a2a.NewMessage(a2a.MessageRoleUser, &a2a.TextPart{Text: "test"})},
		},
		{
			name:    "SendMessageStream",
			methods: []string{http.MethodPost},
			path:    "/v1/message:stream",
		},
		{
			name:    "GetTask",
			methods: []string{http.MethodGet},
			path:    "/v1/tasks/" + string(taskID),
		},
		{
			name:    "ListTasks",
			methods: []string{http.MethodGet},
			path:    "/v1/tasks",
		},
		{
			name:    "CancelTask",
			methods: []string{http.MethodPost},
			path:    "/v1/tasks/" + string(taskID) + ":cancel",
		},
		{
			name:    "ResubscribeToTask",
			methods: []string{http.MethodPost},
			path:    "/v1/tasks/" + string(taskID) + ":subscribe",
		},
		{
			name:    "SetAndListTaskPushConfig",
			methods: []string{http.MethodGet, http.MethodPost},
			path:    "/v1/tasks/" + string(taskID) + "/pushNotificationConfigs",
			body:    config,
		},
		{
			name:    "GetAndDeleteTaskPushConfig",
			methods: []string{http.MethodGet, http.MethodDelete},
			path:    "/v1/tasks/" + string(taskID) + "/pushNotificationConfigs/" + string(config.ID),
			body:    config,
		},
		{
			name:    "GetExtendedAgentCard",
			methods: []string{http.MethodGet},
			path:    "/v1/card",
		},
	}
	store := testutil.NewTestTaskStoreWithConfig(&taskstore.InMemoryStoreConfig{
		Authenticator: authenticator,
	}).WithTasks(t, task)
	pushstore := testutil.NewTestPushConfigStore()
	pushsender := testutil.NewTestPushSender(t).SetSendPushError(nil)
	mock := &mockAgentExecutor{
		ExecuteFunc: func(ctx context.Context, execCtx *ExecutorContext) iter.Seq2[a2a.Event, error] {
			return func(yield func(a2a.Event, error) bool) {
				yield(a2a.NewMessage(a2a.MessageRoleAgent, a2a.TextPart{Text: "test message"}), nil)
			}
		},
		CancelFunc: func(ctx context.Context, execCtx *ExecutorContext) iter.Seq2[a2a.Event, error] {
			return func(yield func(a2a.Event, error) bool) {
				yield(&a2a.Task{
					ID:     taskID,
					Status: a2a.TaskStatus{State: a2a.TaskStateCanceled},
				}, nil)
			}
		},
	}

	reqHandler := NewHandler(
		mock,
		WithTaskStore(store),
		WithPushNotifications(pushstore, pushsender),
		WithExtendedAgentCard(&a2a.AgentCard{}),
	)
	server := httptest.NewServer(NewRESTHandler(reqHandler))
	defer server.Close()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			for _, method := range methods {
				t.Run(method, func(t *testing.T) {
					ctx := t.Context()
					var bodyBytes []byte
					if tc.body != nil {
						bodyBytes, _ = json.Marshal(tc.body)
					} else {
						bodyBytes = []byte("{}")
					}

					req, err := http.NewRequestWithContext(ctx, method, server.URL+tc.path, bytes.NewBuffer(bodyBytes))
					if err != nil {
						t.Fatalf("failed to create request: %v", err)
					}

					resp, err := server.Client().Do(req)
					if err != nil {
						t.Fatalf("request failed: %v", err)
					}

					defer func() {
						if err := resp.Body.Close(); err != nil {
							t.Error(ctx, "failed to close http response body", err)
						}
					}()

					if slices.Contains(tc.methods, method) {
						// HAPPY PATH
						if resp.StatusCode != http.StatusOK {
							errBody, _ := io.ReadAll(resp.Body)
							t.Errorf("got %d want 200 OK for correct method %s. \nServer error details: %s", resp.StatusCode, method, string(errBody))
						}
					} else {
						// SAD PATH
						if resp.StatusCode == http.StatusOK {
							t.Errorf("got %v want non OK for wrong method %s, ", resp.StatusCode, method)
						}
					}
				})
			}
		})
	}
}

func TestREST_InvalidPayloads(t *testing.T) {
	method := http.MethodPost
	payload := "[]"
	expectedErr := a2a.ErrParseError
	taskID := a2a.NewTaskID()

	testCases := []struct {
		name string
		path string
	}{
		{
			name: "SendMessage with invalid payload",
			path: "/v1/message:send",
		},
		{
			name: "SendMessageStream with invalid payload",
			path: "/v1/message:stream",
		},
		{
			name: "SetTaskPushConfig with invalid payload",
			path: "/v1/tasks/" + string(taskID) + "/pushNotificationConfigs",
		},
	}

	reqHandler := NewHandler(&mockAgentExecutor{})
	server := httptest.NewServer(NewRESTHandler(reqHandler))
	defer server.Close()

	ctx := t.Context()
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequestWithContext(ctx, method, server.URL+tc.path, bytes.NewBufferString(payload))
			if err != nil {
				t.Fatalf("failed to create request: %v", err)
			}
			req.Header.Set("Content-Type", "application/json")

			resp, err := server.Client().Do(req)

			if err != nil {
				t.Fatalf("request failed: %v", err)
			}

			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("got %d, want 400 Bad Request", resp.StatusCode)
			}

			if contentType := resp.Header.Get("Content-Type"); contentType != "application/problem+json" {
				t.Errorf("got Content-Type %q, want application/problem+json", contentType)
			}

			gotErr := rest.ToA2AError(resp)

			if !errors.Is(gotErr, expectedErr) {
				t.Errorf("got error %v, want %v", gotErr, expectedErr)
			}
		})
	}
}
