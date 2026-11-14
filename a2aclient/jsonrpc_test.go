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

package a2aclient

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/errordetails"
	"github.com/a2aproject/a2a-go/v2/internal/jsonrpc"
	"github.com/a2aproject/a2a-go/v2/internal/utils"
	"github.com/google/go-cmp/cmp"
)

func mustDecodeJSONRPC(t *testing.T, httpReq *http.Request, method string) jsonrpc.ClientRequest {
	t.Helper()
	if httpReq.Method != "POST" {
		t.Errorf("got %s, want POST", httpReq.Method)
	}
	if httpReq.Header.Get("Content-Type") != "application/json" {
		t.Errorf("got Content-Type %s, want application/json", httpReq.Header.Get("Content-Type"))
	}

	var req jsonrpc.ClientRequest
	if err := utils.DecodeJSON(httpReq.Body, &req); err != nil {
		t.Fatalf("Failed to decode request: %v", err)
	}
	if req.Method != method {
		t.Fatalf("got method %s, want %s", req.Method, method)
	}
	if req.JSONRPC != "2.0" {
		t.Fatalf("got jsonrpc %s, want 2.0", req.JSONRPC)
	}
	return req
}

func newResponse(req jsonrpc.ClientRequest, msg json.RawMessage) jsonrpc.ClientResponse {
	return jsonrpc.ClientResponse{JSONRPC: "2.0", ID: req.ID, Result: msg}
}

func TestJSONRPCTransport_RejectsMismatchedResponseID(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		stream    bool
		preflight bool
		isError   bool
		nonOK     bool
	}{
		{name: "single result"},
		{name: "single error", isError: true},
		{name: "stream result", stream: true},
		{name: "stream error", stream: true, isError: true},
		{name: "stream preflight error", stream: true, preflight: true, isError: true},
		{name: "single HTTP error", isError: true, nonOK: true},
		{name: "stream HTTP error", stream: true, preflight: true, isError: true, nonOK: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request jsonrpc.ClientRequest
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Errorf("Decode() error = %v", err)
					return
				}
				response := jsonrpc.ClientResponse{JSONRPC: "2.0", ID: request.ID + "-other"}
				if tc.isError {
					response.Error = &jsonrpc.Error{Code: -32001, Message: "not found"}
				} else if tc.stream {
					response.Result = json.RawMessage(`{"task":{"id":"unexpected","status":{"state":"TASK_STATE_COMPLETED"}}}`)
				} else {
					response.Result = json.RawMessage(`{"id":"unexpected","status":{"state":"TASK_STATE_COMPLETED"}}`)
				}
				if tc.stream && !tc.preflight {
					w.Header().Set("Content-Type", "text/event-stream")
					data, err := json.Marshal(response)
					if err != nil {
						t.Errorf("Marshal() error = %v", err)
						return
					}
					if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
						t.Errorf("Write() error = %v", err)
						return
					}
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if tc.nonOK {
					w.WriteHeader(http.StatusBadRequest)
				}
				if err := json.NewEncoder(w).Encode(response); err != nil {
					t.Errorf("Encode() error = %v", err)
				}
			}))
			defer server.Close()

			transport := NewJSONRPCTransport(server.URL, nil)
			var got error
			if tc.stream {
				for _, err := range transport.SubscribeToTask(t.Context(), nil, &a2a.SubscribeToTaskRequest{ID: "task"}) {
					got = err
				}
			} else {
				_, got = transport.GetTask(t.Context(), nil, &a2a.GetTaskRequest{ID: "task"})
			}
			if !errors.Is(got, a2a.ErrInvalidAgentResponse) {
				t.Fatalf("response error = %v, want %v", got, a2a.ErrInvalidAgentResponse)
			}
		})
	}
}

func TestJSONRPCTransport_NullAndMissingResponseIDs(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		body string
		want error
	}{
		{name: "null error", body: `{"jsonrpc":"2.0","id":null,"error":{"code":-32600,"message":"bad request"}}`, want: a2a.ErrInvalidRequest},
		{name: "null result", body: `{"jsonrpc":"2.0","id":null,"result":{"id":"task"}}`, want: a2a.ErrInvalidAgentResponse},
		{name: "missing error id", body: `{"jsonrpc":"2.0","error":{"code":-32600,"message":"bad request"}}`, want: a2a.ErrInvalidAgentResponse},
		{name: "empty error id", body: `{"jsonrpc":"2.0","id":"","error":{"code":-32600,"message":"bad request"}}`, want: a2a.ErrInvalidAgentResponse},
		{name: "numeric error id", body: `{"jsonrpc":"2.0","id":42,"error":{"code":-32600,"message":"bad request"}}`, want: a2a.ErrInvalidAgentResponse},
		{name: "object error id", body: `{"jsonrpc":"2.0","id":{},"error":{"code":-32600,"message":"bad request"}}`, want: a2a.ErrInvalidAgentResponse},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if _, err := w.Write([]byte(tc.body)); err != nil {
					t.Errorf("Write() error = %v", err)
				}
			}))
			defer server.Close()
			transport := NewJSONRPCTransport(server.URL, nil)
			_, got := transport.GetTask(t.Context(), nil, &a2a.GetTaskRequest{ID: "task"})
			if !errors.Is(got, tc.want) {
				t.Fatalf("GetTask() error = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestJSONRPCTransport_SendMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := mustDecodeJSONRPC(t, r, "SendMessage")

		resp := newResponse(
			req,
			json.RawMessage(`{"task":{"id":"task-123","contextId":"ctx-123","status":{"state":"TASK_STATE_SUBMITTED"}}}`),
		)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Create transport
	transport := NewJSONRPCTransport(server.URL, nil)

	// Send message
	result, err := transport.SendMessage(t.Context(), ServiceParams{}, &a2a.SendMessageRequest{
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("test message")),
	})

	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}

	task, ok := result.(*a2a.Task)
	if !ok {
		t.Fatalf("got result type %T, want *Task", result)
	}

	if task.ID != "task-123" {
		t.Errorf("got task ID %s, want task-123", task.ID)
	}
}

func TestJSONRPCTransport_SendMessage_MessageResult(t *testing.T) {
	// Create a mock server that returns a Message instead of Task
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := mustDecodeJSONRPC(t, r, "SendMessage")

		// Send Message response (has "role" field, not "status" field)
		resp := newResponse(
			req,
			json.RawMessage(`{"message":{"messageId":"msg-123","role":"ROLE_AGENT","parts":[{"text":"Hello"}]}}`),
		)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	transport := NewJSONRPCTransport(server.URL, nil)

	result, err := transport.SendMessage(t.Context(), ServiceParams{}, &a2a.SendMessageRequest{
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("test message")),
	})

	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}

	msg, ok := result.(*a2a.Message)
	if !ok {
		t.Fatalf("got result type %T, want *Message", result)
	}

	if msg.ID != "msg-123" {
		t.Errorf("got message ID %s, want msg-123", msg.ID)
	}

	if msg.Role != a2a.MessageRoleAgent {
		t.Errorf("got role %s, want agent", msg.Role)
	}
}

func TestJSONRPCTransport_ServiceParamsHeaders(t *testing.T) {
	wantValues := []string{"bar", "baz"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := mustDecodeJSONRPC(t, r, "GetTask")

		if diff := cmp.Diff(wantValues, r.Header.Values("foo")); diff != "" {
			t.Fatalf("r.Header.Get() wrong result (-want +got) diff = %s", diff)
		}

		resp := newResponse(req, json.RawMessage(`{"kind":"task"}`))
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	transport := NewJSONRPCTransport(server.URL, nil)

	_, err := transport.GetTask(t.Context(), ServiceParams{"foo": wantValues}, &a2a.GetTaskRequest{})
	if err != nil {
		t.Fatalf("GetTask failed: %v", err)
	}
}

func TestJSONRPCTransport_GetTask(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := mustDecodeJSONRPC(t, r, "GetTask")

		resp := newResponse(
			req,
			json.RawMessage(`{"id":"task-123","contextId":"ctx-123","status":{"state":"TASK_STATE_COMPLETED"}}`),
		)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	transport := NewJSONRPCTransport(server.URL, nil)

	task, err := transport.GetTask(t.Context(), ServiceParams{}, &a2a.GetTaskRequest{
		ID: "task-123",
	})

	if err != nil {
		t.Fatalf("GetTask failed: %v", err)
	}

	if task.ID != "task-123" {
		t.Errorf("got task ID %s, want task-123", task.ID)
	}
	if task.Status.State != a2a.TaskStateCompleted {
		t.Errorf("got status %s, want TASK_STATE_COMPLETED", task.Status.State)
	}
}

func TestJSONRPCTransport_ListTasks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := mustDecodeJSONRPC(t, r, "ListTasks")

		resp := newResponse(
			req,
			json.RawMessage(
				`{
						"tasks":[
							{"kind":"task","id":"task-1","contextId":"ctx-1","status":{"state":"TASK_STATE_COMPLETED"}},
							{"kind":"task","id":"task-2","contextId":"ctx-2","status":{"state":"TASK_STATE_WORKING"}}
						],
						"totalSize": 2,
						"pageSize": 10,
						"nextPageToken": "test-page-token"
				}`,
			),
		)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	transport := NewJSONRPCTransport(server.URL, nil)

	wantResult := &a2a.ListTasksResponse{
		Tasks: []*a2a.Task{
			{
				ID:        "task-1",
				ContextID: "ctx-1",
				Status: a2a.TaskStatus{
					State: a2a.TaskStateCompleted,
				},
			},
			{
				ID:        "task-2",
				ContextID: "ctx-2",
				Status: a2a.TaskStatus{
					State: a2a.TaskStateWorking,
				},
			},
		},
		TotalSize:     2,
		PageSize:      10,
		NextPageToken: "test-page-token",
	}

	tasks, err := transport.ListTasks(t.Context(), ServiceParams{}, &a2a.ListTasksRequest{})
	if err != nil {
		t.Fatalf("ListTasks failed: %v", err)
	}

	if diff := cmp.Diff(wantResult, tasks); diff != "" {
		t.Fatalf("ListTasks wrong result (-want +got): %s", diff)
	}
}

func TestJSONRPCTransport_ErrorHandling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := mustDecodeJSONRPC(t, r, "GetTask")

		resp := jsonrpc.ClientResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &jsonrpc.Error{
				Code:    -32600,
				Message: "Invalid Request",
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	transport := NewJSONRPCTransport(server.URL, nil)

	_, err := transport.GetTask(t.Context(), ServiceParams{}, &a2a.GetTaskRequest{
		ID: "task-123",
	})

	if !errors.Is(err, a2a.ErrInvalidRequest) {
		t.Fatalf("got error = %v, want %v", err, a2a.ErrInvalidRequest)
	}
}

func TestJSONRPCTransport_SendStreamingMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request := mustDecodeJSONRPC(t, r, "SendStreamingMessage")
		if r.Header.Get("Accept") != "text/event-stream, application/json" {
			t.Errorf("got Accept %s, want text/event-stream, application/json", r.Header.Get("Accept"))
		}

		w.Header().Set("Content-Type", "text/event-stream")

		// Send multiple SSE events
		events := []string{
			`data: {"jsonrpc":"2.0","id":"test","result":{"task":{"id":"task-123","contextId":"ctx-123","status":{"state":"TASK_STATE_WORKING"}}}}`,
			``,
			`data: {"jsonrpc":"2.0","id":"test","result":{"message":{"messageId":"msg-1","role":"agent","parts":[{"text":"Processing..."}]}}}`,
			``,
			`data: {"jsonrpc":"2.0","id":"test","result":{"task":{"id":"task-123","contextId":"ctx-123","status":{"state":"TASK_STATE_COMPLETED"}}}}`,
			``,
		}

		for _, event := range events {
			event = strings.ReplaceAll(event, `"id":"test"`, `"id":"`+request.ID+`"`)
			_, _ = w.Write([]byte(event + "\n"))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}))
	defer server.Close()

	transport := NewJSONRPCTransport(server.URL, nil)

	events := []a2a.Event{}
	for event, err := range transport.SendStreamingMessage(t.Context(), ServiceParams{}, &a2a.SendMessageRequest{
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("test")),
	}) {
		if err != nil {
			t.Fatalf("Stream error: %v", err)
		}
		events = append(events, event)
	}

	if len(events) != 3 {
		t.Errorf("got %d events, want 3", len(events))
	}

	// First event should be a Task
	if _, ok := events[0].(*a2a.Task); !ok {
		t.Errorf("got events[0] type %T, want *Task", events[0])
	}

	// Second event should be a Message
	if _, ok := events[1].(*a2a.Message); !ok {
		t.Errorf("got events[1] type %T, want *Message", events[1])
	}

	// Third event should be a Task
	if _, ok := events[2].(*a2a.Task); !ok {
		t.Errorf("got events[2] type %T, want *Task", events[2])
	}
}

func TestParseSSEStream(t *testing.T) {
	sseData := `data: {"jsonrpc":"2.0","id":"1","result":{"task":{"id":"task-1"}}}

data: {"jsonrpc":"2.0","id":"1","result":{"message":{"role":"agent"}}}

`
	body := io.NopCloser(bytes.NewBufferString(sseData))

	results := []json.RawMessage{}
	for result, err := range parseSSEStream(body, "1") {
		if err != nil {
			t.Fatalf("Parse error: %v", err)
		}
		results = append(results, result)
	}

	if len(results) != 2 {
		t.Errorf("got %d results, want 2", len(results))
	}
}

func TestJSONRPCTransport_ResubscribeToTask(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request := mustDecodeJSONRPC(t, r, "SubscribeToTask")

		if r.Header.Get("Accept") != "text/event-stream, application/json" {
			t.Errorf("got Accept %s, want text/event-stream, application/json", r.Header.Get("Accept"))
		}

		w.Header().Set("Content-Type", "text/event-stream")

		// Send task updates via SSE
		events := []string{
			`data: {"jsonrpc":"2.0","id":"test","result":{"task":{"id":"task-123","contextId":"ctx-123","status":{"state":"TASK_STATE_WORKING"}}}}`,
			``,
			`data: {"jsonrpc":"2.0","id":"test","result":{"statusUpdate":{"taskId":"task-123","contextId":"ctx-123","status":{"state":"TASK_STATE_COMPLETED"}}}}`,
			``,
		}

		for _, event := range events {
			event = strings.ReplaceAll(event, `"id":"test"`, `"id":"`+request.ID+`"`)
			_, _ = w.Write([]byte(event + "\n"))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}))
	defer server.Close()

	transport := NewJSONRPCTransport(server.URL, nil)

	events := []a2a.Event{}
	for event, err := range transport.SubscribeToTask(t.Context(), ServiceParams{}, &a2a.SubscribeToTaskRequest{
		ID: "task-123",
	}) {
		if err != nil {
			t.Fatalf("Stream error: %v", err)
		}
		events = append(events, event)
	}

	if len(events) != 2 {
		t.Errorf("got %d events, want 2", len(events))
	}

	// First event should be a Task
	if _, ok := events[0].(*a2a.Task); !ok {
		t.Errorf("got events[0] type %T, want *Task", events[0])
	}

	// Second event should be a TaskStatusUpdateEvent
	if _, ok := events[1].(*a2a.TaskStatusUpdateEvent); !ok {
		t.Errorf("got events[1] type %T, want *TaskStatusUpdateEvent", events[1])
	}
}

func TestJSONRPCTransport_GetAgentCard(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := mustDecodeJSONRPC(t, r, "GetExtendedAgentCard")

		resp := newResponse(
			req,
			json.RawMessage(`{"supportedInterfaces":[{"url":"http://example.com"}],"name":"Test agent","description":"test"}`),
		)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	transport := NewJSONRPCTransport(server.URL, nil)

	got, err := transport.GetExtendedAgentCard(t.Context(), ServiceParams{}, &a2a.GetExtendedAgentCardRequest{})

	if err != nil {
		t.Fatalf("GetAgentCard failed: %v", err)
	}

	cardAddr := "http://example.com"
	want := &a2a.AgentCard{
		Name: "Test agent",
		SupportedInterfaces: []*a2a.AgentInterface{
			{URL: cardAddr},
		},
		Description: "test",
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("got wrong card (-want +got) diff = %s", diff)
	}
}

func TestJSONRPCTransport_CancelTask(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := mustDecodeJSONRPC(t, r, "CancelTask")

		resp := newResponse(
			req,
			json.RawMessage(`{"id":"task-123","contextId":"ctx-123","status":{"state":"TASK_STATE_CANCELED"}}`),
		)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	transport := NewJSONRPCTransport(server.URL, nil)

	task, err := transport.CancelTask(t.Context(), ServiceParams{}, &a2a.CancelTaskRequest{
		ID: "task-123",
	})

	if err != nil {
		t.Fatalf("CancelTask failed: %v", err)
	}

	if task.Status.State != a2a.TaskStateCanceled {
		t.Errorf("got status %s, want TASK_STATE_CANCELED", task.Status.State)
	}
}

func TestJSONRPCTransport_PushNotificationConfig(t *testing.T) {
	t.Run("Get", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			req := mustDecodeJSONRPC(t, r, "GetTaskPushNotificationConfig")

			resp := newResponse(
				req,
				json.RawMessage(`{"taskId":"task-123","config":{"id":"config-123","url":"https://webhook.example.com"}}`),
			)
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		transport := NewJSONRPCTransport(server.URL, nil)

		config, err := transport.GetTaskPushConfig(t.Context(), ServiceParams{}, &a2a.GetTaskPushConfigRequest{
			TaskID: "task-123",
		})

		if err != nil {
			t.Fatalf("GetTaskPushConfig failed: %v", err)
		}

		if config.TaskID != "task-123" {
			t.Errorf("got taskId %s, want task-123", config.TaskID)
		}
	})

	t.Run("List", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			req := mustDecodeJSONRPC(t, r, "ListTaskPushNotificationConfigs")

			resp := newResponse(
				req,
				json.RawMessage(`{"configs": [{"taskId":"task-1","config":{"id":"config-1","url":"https://webhook1.example.com"}},{"taskId":"task-2","config":{"id":"config-2","url":"https://webhook2.example.com"}}]}`),
			)
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		transport := NewJSONRPCTransport(server.URL, nil)

		configs, err := transport.ListTaskPushConfigs(t.Context(), ServiceParams{}, &a2a.ListTaskPushConfigRequest{})

		if err != nil {
			t.Fatalf("ListTaskPushConfigs failed: %v", err)
		}

		if len(configs) != 2 {
			t.Errorf("got %d configs, want 2", len(configs))
		}
	})

	t.Run("Create", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			req := mustDecodeJSONRPC(t, r, "CreateTaskPushNotificationConfig")

			resp := newResponse(
				req,
				json.RawMessage(`{"taskId":"task-123","id":"config-123","url":"https://webhook.example.com"}`),
			)
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		transport := NewJSONRPCTransport(server.URL, nil)

		config, err := transport.CreateTaskPushConfig(t.Context(), ServiceParams{}, &a2a.PushConfig{
			TaskID: "task-123",
			ID:     "config-123",
			URL:    "https://webhook.example.com",
		})

		if err != nil {
			t.Fatalf("CreateTaskPushConfig failed: %v", err)
		}

		if config.TaskID != "task-123" {
			t.Errorf("got taskId %s, want task-123", config.TaskID)
		}

		if config.URL != "https://webhook.example.com" {
			t.Errorf("got URL %s, want https://webhook.example.com", config.URL)
		}
	})

	t.Run("Delete", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			req := mustDecodeJSONRPC(t, r, "DeleteTaskPushNotificationConfig")

			resp := newResponse(req, json.RawMessage(`{}`))
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		transport := NewJSONRPCTransport(server.URL, nil)

		err := transport.DeleteTaskPushConfig(t.Context(), ServiceParams{}, &a2a.DeleteTaskPushConfigRequest{
			TaskID: "task-123",
		})

		if err != nil {
			t.Fatalf("DeleteTaskPushConfig failed: %v", err)
		}
	})
}

func TestJSONRPCTransport_WithHTTPClient(t *testing.T) {
	customClient := &http.Client{
		Timeout: 10 * time.Second,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonrpc.ClientRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("Failed to decode request: %v", err)
			return
		}

		resp := newResponse(
			req,
			json.RawMessage(`{"id":"task-123","contextId":"ctx-123","status":{"state":"TASK_STATE_COMPLETED"}}`),
		)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	transport := NewJSONRPCTransport(server.URL, customClient)

	// Verify custom client is used
	jt := transport.(*jsonrpcTransport)
	if jt.httpClient.Timeout != 10*time.Second {
		t.Errorf("got timeout %v, want 10s", jt.httpClient.Timeout)
	}

	task, err := transport.GetTask(t.Context(), ServiceParams{}, &a2a.GetTaskRequest{
		ID: "task-123",
	})

	if err != nil {
		t.Fatalf("GetTask failed: %v", err)
	}

	if task.ID != "task-123" {
		t.Errorf("got task ID %s, want task-123", task.ID)
	}
}

func TestJSONRPCTransport_ErrorDetails(t *testing.T) {
	wantMsg := "Access Denied"
	wantDetails := map[string]any{"reason": "expired_token", "scope": "read"}
	typedDetails := []*errordetails.Typed{errordetails.NewTyped("google.protobuf.Struct", wantDetails)}
	rawData, err := json.Marshal(typedDetails)
	if err != nil {
		t.Fatalf("failed to marshal typed details: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := mustDecodeJSONRPC(t, r, "GetTask")

		resp := jsonrpc.ClientResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &jsonrpc.Error{
				Code:    -31403,
				Message: wantMsg,
				Data:    rawData,
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	transport := NewJSONRPCTransport(server.URL, nil)

	_, err = transport.GetTask(t.Context(), ServiceParams{}, &a2a.GetTaskRequest{
		ID: "task-123",
	})

	var a2aErr *a2a.Error
	if !errors.As(err, &a2aErr) {
		t.Fatalf("got error type %T, want *a2a.Error", err)
	}
	if !errors.Is(a2aErr.Err, a2a.ErrUnauthorized) {
		t.Errorf("got inner error %v, want ErrUnauthorized", a2aErr.Err)
	}
	if a2aErr.Message != wantMsg {
		t.Errorf("got message %q, want %q", a2aErr.Message, wantMsg)
	}
	if diff := cmp.Diff(wantDetails, a2aErr.Details); diff != "" {
		t.Errorf("got wrong details (-want +got) diff = %s", diff)
	}
}

func TestJSONRPCTransport_ErrorWithNonTypedData(t *testing.T) {
	// Regression test: a v0.3 server may return error.data as a plain JSON object
	// (not an array of @type-tagged details). The client must still surface the
	// error code and message instead of failing the whole decode.
	wantMsg := "real error message"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request := mustDecodeJSONRPC(t, r, "GetTask")

		// Write raw JSON with error.data as a plain object.
		w.Header().Set("Content-Type", "application/json")
		if _, err := fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%q,"error":{"code":-32603,"message":"real error message","data":{"reason":"detail"}}}`, request.ID); err != nil {
			t.Errorf("Write() error = %v", err)
		}
	}))
	defer server.Close()

	transport := NewJSONRPCTransport(server.URL, nil)

	_, err := transport.GetTask(t.Context(), ServiceParams{}, &a2a.GetTaskRequest{
		ID: "task-1",
	})

	if err == nil {
		t.Fatalf("expected error, got nil")
	}

	var a2aErr *a2a.Error
	if !errors.As(err, &a2aErr) {
		t.Fatalf("got error type %T, want *a2a.Error", err)
	}
	if !errors.Is(a2aErr.Err, a2a.ErrInternalError) {
		t.Errorf("got inner error %v, want ErrInternalError", a2aErr.Err)
	}
	if a2aErr.Message != wantMsg {
		t.Errorf("got message %q, want %q", a2aErr.Message, wantMsg)
	}
	// The plain object should have been preserved as a struct detail.
	if a2aErr.Details["reason"] != "detail" {
		t.Errorf("got details %v, want reason=detail", a2aErr.Details)
	}
}

func TestJSONRPCTransport_ErrorWithStringData(t *testing.T) {
	// error.data may be any JSON value, including a plain string.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request := mustDecodeJSONRPC(t, r, "GetTask")
		w.Header().Set("Content-Type", "application/json")
		if _, err := fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%q,"error":{"code":-32600,"message":"bad request","data":"extra info"}}`, request.ID); err != nil {
			t.Errorf("Write() error = %v", err)
		}
	}))
	defer server.Close()

	transport := NewJSONRPCTransport(server.URL, nil)

	_, err := transport.GetTask(t.Context(), ServiceParams{}, &a2a.GetTaskRequest{ID: "task-1"})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}

	var a2aErr *a2a.Error
	if !errors.As(err, &a2aErr) {
		t.Fatalf("got error type %T, want *a2a.Error", err)
	}
	if !errors.Is(a2aErr.Err, a2a.ErrInvalidRequest) {
		t.Errorf("got inner error %v, want ErrInvalidRequest", a2aErr.Err)
	}
	if a2aErr.Message != "bad request" {
		t.Errorf("got message %q, want %q", a2aErr.Message, "bad request")
	}
}

func TestJSONRPCTransport_Tenant(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := mustDecodeJSONRPC(t, r, "ListTasks")
		params, ok := req.Params.(map[string]any)
		if !ok {
			t.Fatalf("expected map[string]any params, got %T", req.Params)
		}
		if params["tenant"] != "my-tenant" {
			t.Errorf("expected tenant my-tenant, got %v", params["tenant"])
		}

		resp := newResponse(req, json.RawMessage(`{"tasks":[]}`))
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	iface := a2a.NewAgentInterface(server.URL, a2a.TransportProtocolJSONRPC)
	iface.Tenant = "my-tenant"
	transport := NewJSONRPCTransport(iface.URL, nil)
	// Apply decorator manually as we are bypassing the factory
	transport = &tenantTransportDecorator{base: transport, tenant: iface.Tenant}

	_, err := transport.ListTasks(t.Context(), ServiceParams{}, &a2a.ListTasksRequest{})
	if err != nil {
		t.Fatalf("ListTasks failed: %v", err)
	}
}
func TestJSONRPCTransport_Serialization(t *testing.T) {
	tests := []struct {
		name     string
		role     a2a.MessageRole
		wantJSON string
	}{
		{
			name:     "ROLE_USER",
			role:     a2a.MessageRoleUser,
			wantJSON: `"ROLE_USER"`,
		},
		{
			name:     "ROLE_AGENT",
			role:     a2a.MessageRoleAgent,
			wantJSON: `"ROLE_AGENT"`,
		},
		{
			name:     "ROLE_UNSPECIFIED",
			role:     a2a.MessageRoleUnspecified,
			wantJSON: `"ROLE_UNSPECIFIED"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				if !bytes.Contains(body, []byte(tt.wantJSON)) {
					t.Errorf("body does not contain %s: %s", tt.wantJSON, string(body))
				}

				req := jsonrpc.ClientRequest{}
				_ = json.Unmarshal(body, &req)
				resp := newResponse(req, json.RawMessage(`{"kind":"task"}`))
				_ = json.NewEncoder(w).Encode(resp)
			}))
			defer server.Close()

			transport := NewJSONRPCTransport(server.URL, nil)
			_, _ = transport.SendMessage(t.Context(), ServiceParams{}, &a2a.SendMessageRequest{
				Message: a2a.NewMessage(tt.role, a2a.NewTextPart("test")),
			})
		})
	}
}

func TestJSONRPCTransport_Deserialization(t *testing.T) {
	tests := []struct {
		name      string
		rawJSON   string
		wantState a2a.TaskState
	}{
		{
			name:      "TASK_STATE_COMPLETED",
			rawJSON:   `{"id":"task-1","status":{"state":"TASK_STATE_COMPLETED"}}`,
			wantState: a2a.TaskStateCompleted,
		},
		{
			name:      "TASK_STATE_UNSPECIFIED",
			rawJSON:   `{"id":"task-1","status":{"state":"TASK_STATE_UNSPECIFIED"}}`,
			wantState: a2a.TaskStateUnspecified,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				req := mustDecodeJSONRPC(t, r, "GetTask")
				resp := newResponse(req, json.RawMessage(tt.rawJSON))
				_ = json.NewEncoder(w).Encode(resp)
			}))
			defer server.Close()

			transport := NewJSONRPCTransport(server.URL, nil)
			task, err := transport.GetTask(t.Context(), ServiceParams{}, &a2a.GetTaskRequest{ID: "task-1"})
			if err != nil {
				t.Fatalf("GetTask failed: %v", err)
			}
			if task.Status.State != tt.wantState {
				t.Errorf("got state %s, want %s", task.Status.State, tt.wantState)
			}
		})
	}
}

func TestJSONRPCTransport_ResponseTrailingData(t *testing.T) {
	t.Parallel()

	result := json.RawMessage(`{"id":"task-123","contextId":"ctx-123","status":{"state":"TASK_STATE_COMPLETED"}}`)
	want := &a2a.Task{ID: "task-123", ContextID: "ctx-123", Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}}

	testCases := []struct {
		name    string
		body    func(resp []byte) []byte
		wantErr error
	}{
		{
			name: "trailing whitespace is accepted",
			body: func(resp []byte) []byte { return slices.Concat(resp, []byte("\n\t ")) },
		},
		{
			name:    "trailing garbage",
			body:    func(resp []byte) []byte { return slices.Concat(resp, []byte("TRAILING_GARBAGE")) },
			wantErr: utils.ErrTrailingData,
		},
		{
			name:    "concatenated responses",
			body:    func(resp []byte) []byte { return slices.Concat(resp, resp) },
			wantErr: utils.ErrTrailingData,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				req := mustDecodeJSONRPC(t, r, "GetTask")
				resp, err := json.Marshal(newResponse(req, result))
				if err != nil {
					t.Errorf("failed to marshal response: %v", err)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(tc.body(resp))
			}))
			defer server.Close()

			transport := NewJSONRPCTransport(server.URL, nil)

			got, err := transport.GetTask(t.Context(), ServiceParams{}, &a2a.GetTaskRequest{ID: "task-123"})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("GetTask() error = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				return
			}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("GetTask() wrong result (-want +got):\n%s", diff)
			}
		})
	}
}

func TestJSONRPCTransport_QueryParamsFromContext(t *testing.T) {
	t.Parallel()

	var gotQuery url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		req := mustDecodeJSONRPC(t, r, "GetTask")
		resp := newResponse(req, json.RawMessage(`{"kind":"task","id":"task-123"}`))
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(server.Close)

	transport := NewJSONRPCTransport(server.URL, nil)
	ctx := AttachHTTPQueryParams(t.Context(), url.Values{"api_key": []string{"secret"}})

	_, err := transport.GetTask(ctx, ServiceParams{}, &a2a.GetTaskRequest{ID: "task-123"})
	if err != nil {
		t.Fatalf("transport.GetTask() error = %v", err)
	}

	want := url.Values{"api_key": []string{"secret"}}
	if diff := cmp.Diff(want, gotQuery); diff != "" {
		t.Errorf("wrong query params (-want +got) diff = %s", diff)
	}
}

func TestJSONRPCTransport_ErrorRedactsQueryParams(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	serverURL := server.URL
	server.Close()

	transport := NewJSONRPCTransport(serverURL, nil)
	ctx := AttachHTTPQueryParams(t.Context(), url.Values{"api_key": []string{"super-secret"}})

	_, err := transport.GetTask(ctx, ServiceParams{}, &a2a.GetTaskRequest{ID: "task-123"})
	if err == nil {
		t.Fatalf("transport.GetTask() error = nil, want non-nil")
	}
	if strings.Contains(err.Error(), "super-secret") {
		t.Errorf("transport.GetTask() error = %v, must not contain query params", err)
	}
}

func TestAuth_APIKeyCookieLocation_JSONRPC(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	var gotCookie string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCookie = r.Header.Get("Cookie")
		if got := r.Header.Get("session_key"); got != "" {
			t.Errorf("credential leaked into header session_key = %q, want empty", got)
		}
		req := mustDecodeJSONRPC(t, r, "SendMessage")
		resp := newResponse(req, json.RawMessage(`{"task":{"id":"task-123","contextId":"ctx-123","status":{"state":"TASK_STATE_SUBMITTED"}}}`))
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(server.Close)

	schemeName := a2a.SecuritySchemeName("api-key")
	card := &a2a.AgentCard{
		SupportedInterfaces: []*a2a.AgentInterface{
			{
				ProtocolBinding: a2a.TransportProtocolJSONRPC,
				ProtocolVersion: a2a.Version,
				URL:             server.URL,
			},
		},
		SecurityRequirements: []a2a.SecurityRequirements{{schemeName: []string{}}},
		SecuritySchemes: a2a.NamedSecuritySchemes{
			schemeName: a2a.APIKeySecurityScheme{Name: "session_key", Location: a2a.APIKeySecuritySchemeLocationCookie},
		},
	}

	credStore := NewInMemoryCredentialsStore()
	client, err := NewFromCard(
		ctx,
		card,
		withTestJSONRPCTransport(),
		WithCallInterceptors(&AuthInterceptor{Service: credStore}),
	)
	if err != nil {
		t.Fatalf("a2aclient.NewFromCard() error = %v", err)
	}
	t.Cleanup(func() {
		if err := client.Destroy(); err != nil {
			t.Errorf("client.Destroy() error = %v", err)
		}
	})

	sessionID := SessionID("abcd")
	credStore.Set(sessionID, schemeName, AuthCredential("secret"))

	ctx = AttachSessionID(ctx, sessionID)
	_, err = client.SendMessage(ctx, &a2a.SendMessageRequest{Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("test"))})
	if err != nil {
		t.Fatalf("client.SendMessage() error = %v", err)
	}

	if gotCookie != "session_key=secret" {
		t.Errorf("Cookie header = %q, want %q", gotCookie, "session_key=secret")
	}
}

func TestJSONRPCTransport_NonOKStatus(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		status      int
		contentType string
		body        string
		wantErr     error
		wantMsg     string
	}{
		{
			name:        "HTTP middleware error without JSON-RPC envelope",
			status:      http.StatusUnauthorized,
			contentType: "application/json",
			body:        `{"error":{"code":-31401,"message":"the token was revoked"}}`,
			wantErr:     a2a.ErrUnauthenticated,
			wantMsg:     "the token was revoked",
		},
		{
			name:        "JSON-RPC error from auth middleware",
			status:      http.StatusUnauthorized,
			contentType: "application/json",
			body:        `{"jsonrpc":"2.0","id":null,"error":{"code":-31401,"message":"the token was revoked"}}`,
			wantErr:     a2a.ErrUnauthenticated,
			wantMsg:     "the token was revoked",
		},
		{
			name:        "JSON-RPC error with content type parameters",
			status:      http.StatusRequestEntityTooLarge,
			contentType: "application/json; charset=utf-8",
			body:        `{"jsonrpc":"2.0","id":null,"error":{"code":-32600,"message":"payload too large"}}`,
			wantErr:     a2a.ErrInvalidRequest,
			wantMsg:     "payload too large",
		},
		{
			name:        "non-JSON body",
			status:      http.StatusUnauthorized,
			contentType: "text/plain",
			body:        `{"jsonrpc":"2.0","id":null,"error":{"code":-31401,"message":"the token was revoked"}}`,
			wantMsg:     "unexpected HTTP status: 401 Unauthorized",
		},
		{
			name:        "JSON body without error",
			status:      http.StatusBadGateway,
			contentType: "application/json",
			body:        `{"message":"upstream unavailable"}`,
			wantMsg:     "unexpected HTTP status: 502 Bad Gateway",
		},
		{
			name:        "malformed JSON body",
			status:      http.StatusForbidden,
			contentType: "application/json",
			body:        `{"jsonrpc":"2.0","error":{"code":-31403`,
			wantMsg:     "unexpected HTTP status: 403 Forbidden",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(server.Close)

			transport := NewJSONRPCTransport(server.URL, nil)

			checkErr := func(op string, err error) {
				t.Helper()
				if err == nil {
					t.Fatalf("transport.%s() error = nil, want non-nil", op)
				}
				if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
					t.Fatalf("transport.%s() error = %v, want %v", op, err, tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantMsg) {
					t.Fatalf("transport.%s() error = %v, want message containing %q", op, err, tc.wantMsg)
				}
			}

			_, err := transport.GetTask(t.Context(), ServiceParams{}, &a2a.GetTaskRequest{ID: "task-123"})
			checkErr("GetTask", err)

			var streamErr error
			for _, err := range transport.SendStreamingMessage(t.Context(), ServiceParams{}, &a2a.SendMessageRequest{
				Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("test")),
			}) {
				if err != nil {
					streamErr = err
					break
				}
			}
			checkErr("SendStreamingMessage", streamErr)
		})
	}
}
