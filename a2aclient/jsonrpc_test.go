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
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/internal/jsonrpc"
	"github.com/google/go-cmp/cmp"
)

func mustDecodeJSONRPC(t *testing.T, httpReq *http.Request, method string) jsonrpcRequest {
	t.Helper()
	if httpReq.Method != "POST" {
		t.Errorf("got %s, want POST", httpReq.Method)
	}
	if httpReq.Header.Get("Content-Type") != "application/json" {
		t.Errorf("got Content-Type %s, want application/json", httpReq.Header.Get("Content-Type"))
	}

	var req jsonrpcRequest
	if err := json.NewDecoder(httpReq.Body).Decode(&req); err != nil {
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

func newResponse(req jsonrpcRequest, msg json.RawMessage) jsonrpcResponse {
	return jsonrpcResponse{JSONRPC: "2.0", ID: req.ID, Result: msg}
}

func TestJSONRPCTransport_SendMessage(t *testing.T) {
	// Create a mock server that returns a Task
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := mustDecodeJSONRPC(t, r, "message/send")

		resp := newResponse(
			req,
			json.RawMessage(`{"kind":"task","id":"task-123","contextId":"ctx-123","status":{"state":"submitted"}}`),
		)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Create transport
	transport := NewJSONRPCTransport(server.URL, nil)

	// Send message
	result, err := transport.SendMessage(t.Context(), &a2a.MessageSendParams{
		Message: a2a.NewMessage(a2a.MessageRoleUser, &a2a.TextPart{Text: "test message"}),
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
		req := mustDecodeJSONRPC(t, r, "message/send")

		// Send Message response (has "role" field, not "status" field)
		resp := newResponse(
			req,
			json.RawMessage(`{"kind":"message","messageId":"msg-123","role":"agent","parts":[{"kind":"text","text":"Hello"}]}`),
		)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	transport := NewJSONRPCTransport(server.URL, nil)

	result, err := transport.SendMessage(t.Context(), &a2a.MessageSendParams{
		Message: a2a.NewMessage(a2a.MessageRoleUser, &a2a.TextPart{Text: "test message"}),
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

func TestJSONRPCTransport_CallMetaHeaders(t *testing.T) {
	wantValues := []string{"bar", "baz"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := mustDecodeJSONRPC(t, r, "tasks/get")

		if diff := cmp.Diff(wantValues, r.Header.Values("foo")); diff != "" {
			t.Fatalf("r.Header.Get() wrong result (+got,-want) diff = %s", diff)
		}

		resp := newResponse(req, json.RawMessage(`{"kind":"task"}`))
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	transport := NewJSONRPCTransport(server.URL, nil)

	ctx := withCallMeta(t.Context(), CallMeta{"foo": wantValues})

	_, err := transport.GetTask(ctx, &a2a.TaskQueryParams{})
	if err != nil {
		t.Fatalf("GetTask failed: %v", err)
	}
}

func TestJSONRPCTransport_GetTask(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := mustDecodeJSONRPC(t, r, "tasks/get")

		resp := newResponse(
			req,
			json.RawMessage(`{"kind":"task","id":"task-123","contextId":"ctx-123","status":{"state":"completed"}}`),
		)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	transport := NewJSONRPCTransport(server.URL, nil)

	task, err := transport.GetTask(t.Context(), &a2a.TaskQueryParams{
		ID: "task-123",
	})

	if err != nil {
		t.Fatalf("GetTask failed: %v", err)
	}

	if task.ID != "task-123" {
		t.Errorf("got task ID %s, want task-123", task.ID)
	}
	if task.Status.State != a2a.TaskStateCompleted {
		t.Errorf("got status %s, want completed", task.Status.State)
	}
}

func TestJSONRPCTransport_ErrorHandling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := mustDecodeJSONRPC(t, r, "tasks/get")

		resp := jsonrpcResponse{
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

	_, err := transport.GetTask(t.Context(), &a2a.TaskQueryParams{
		ID: "task-123",
	})

	if !errors.Is(err, a2a.ErrInvalidRequest) {
		t.Fatalf("got error = %v, want %v", err, a2a.ErrInvalidRequest)
	}
}

func TestJSONRPCTransport_SendStreamingMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Errorf("got Accept %s, want text/event-stream", r.Header.Get("Accept"))
		}

		w.Header().Set("Content-Type", "text/event-stream")

		// Send multiple SSE events
		events := []string{
			`data: {"jsonrpc":"2.0","id":"test","result":{"kind":"task","id":"task-123","contextId":"ctx-123","status":{"state":"working"}}}`,
			``,
			`data: {"jsonrpc":"2.0","id":"test","result":{"kind":"message","messageId":"msg-1","role":"agent","parts":[{"kind":"text","text":"Processing..."}]}}`,
			``,
			`data: {"jsonrpc":"2.0","id":"test","result":{"kind":"task","id":"task-123","contextId":"ctx-123","status":{"state":"completed"}}}`,
			``,
		}

		for _, event := range events {
			_, _ = w.Write([]byte(event + "\n"))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}))
	defer server.Close()

	transport := NewJSONRPCTransport(server.URL, nil)

	events := []a2a.Event{}
	for event, err := range transport.SendStreamingMessage(t.Context(), &a2a.MessageSendParams{
		Message: a2a.NewMessage(a2a.MessageRoleUser, &a2a.TextPart{Text: "test"}),
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
	sseData := `data: {"jsonrpc":"2.0","id":"1","result":{"id":"task-1"}}

data: {"jsonrpc":"2.0","id":"2","result":{"role":"agent"}}

`
	body := io.NopCloser(bytes.NewBufferString(sseData))

	results := []json.RawMessage{}
	for result, err := range parseSSEStream(body) {
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
		_ = mustDecodeJSONRPC(t, r, "tasks/resubscribe")

		if r.Header.Get("Accept") != "text/event-stream" {
			t.Errorf("got Accept %s, want text/event-stream", r.Header.Get("Accept"))
		}

		w.Header().Set("Content-Type", "text/event-stream")

		// Send task updates via SSE
		events := []string{
			`data: {"jsonrpc":"2.0","id":"test","result":{"kind":"task","id":"task-123","contextId":"ctx-123","status":{"state":"working"}}}`,
			``,
			`data: {"jsonrpc":"2.0","id":"test","result":{"kind":"status-update","taskId":"task-123","contextId":"ctx-123","final":false,"status":{"state":"completed"}}}`,
			``,
		}

		for _, event := range events {
			_, _ = w.Write([]byte(event + "\n"))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}))
	defer server.Close()

	transport := NewJSONRPCTransport(server.URL, nil)

	events := []a2a.Event{}
	for event, err := range transport.ResubscribeToTask(t.Context(), &a2a.TaskIDParams{
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
	t.Run("basic card without extended support", func(t *testing.T) {
		card := &a2a.AgentCard{
			Name:                              "Test Agent",
			URL:                               "http://example.com",
			SupportsAuthenticatedExtendedCard: false,
		}

		transport := NewJSONRPCTransport("http://example.com", card)

		result, err := transport.GetAgentCard(t.Context())
		if err != nil {
			t.Fatalf("GetAgentCard failed: %v", err)
		}

		if result.Name != "Test Agent" {
			t.Errorf("got name %s, want Test Agent", result.Name)
		}
	})

	t.Run("returns provided card", func(t *testing.T) {
		card := &a2a.AgentCard{
			Name:        "Test Agent",
			URL:         "http://example.com",
			Description: "Test description",
		}

		transport := NewJSONRPCTransport("http://example.com", card)

		result, err := transport.GetAgentCard(t.Context())
		if err != nil {
			t.Fatalf("GetAgentCard failed: %v", err)
		}

		if result.Name != "Test Agent" {
			t.Errorf("got name %s, want Test Agent", result.Name)
		}

		if result.Description != "Test description" {
			t.Errorf("got description %s, want Test description", result.Description)
		}
	})

	t.Run("no card provided", func(t *testing.T) {
		transport := NewJSONRPCTransport("http://example.com", nil)

		_, err := transport.GetAgentCard(t.Context())
		if err == nil {
			t.Fatal("got nil error when no card provided, want error")
		}
	})
}

func TestJSONRPCTransport_CancelTask(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := mustDecodeJSONRPC(t, r, "tasks/cancel")

		resp := newResponse(
			req,
			json.RawMessage(`{"kind":"task","id":"task-123","contextId":"ctx-123","status":{"state":"canceled"}}`),
		)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	transport := NewJSONRPCTransport(server.URL, nil)

	task, err := transport.CancelTask(t.Context(), &a2a.TaskIDParams{
		ID: "task-123",
	})

	if err != nil {
		t.Fatalf("CancelTask failed: %v", err)
	}

	if task.Status.State != a2a.TaskStateCanceled {
		t.Errorf("got status %s, want canceled", task.Status.State)
	}
}

func TestJSONRPCTransport_PushNotificationConfig(t *testing.T) {
	t.Run("Get", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			req := mustDecodeJSONRPC(t, r, "tasks/pushNotificationConfig/get")

			resp := newResponse(
				req,
				json.RawMessage(`{"taskId":"task-123","pushNotificationConfig":{"id":"config-123","url":"https://webhook.example.com"}}`),
			)
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		transport := NewJSONRPCTransport(server.URL, nil)

		config, err := transport.GetTaskPushConfig(t.Context(), &a2a.GetTaskPushConfigParams{
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
			req := mustDecodeJSONRPC(t, r, "tasks/pushNotificationConfig/list")

			resp := newResponse(
				req,
				json.RawMessage(`[{"taskId":"task-1","pushNotificationConfig":{"id":"config-1","url":"https://webhook1.example.com"}},{"taskId":"task-2","pushNotificationConfig":{"id":"config-2","url":"https://webhook2.example.com"}}]`),
			)
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		transport := NewJSONRPCTransport(server.URL, nil)

		configs, err := transport.ListTaskPushConfig(t.Context(), &a2a.ListTaskPushConfigParams{})

		if err != nil {
			t.Fatalf("ListTaskPushConfig failed: %v", err)
		}

		if len(configs) != 2 {
			t.Errorf("got %d configs, want 2", len(configs))
		}
	})

	t.Run("Set", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			req := mustDecodeJSONRPC(t, r, "tasks/pushNotificationConfig/set")

			resp := newResponse(
				req,
				json.RawMessage(`{"taskId":"task-123","pushNotificationConfig":{"id":"config-123","url":"https://webhook.example.com"}}`),
			)
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		transport := NewJSONRPCTransport(server.URL, nil)

		config, err := transport.SetTaskPushConfig(t.Context(), &a2a.TaskPushConfig{
			TaskID: "task-123",
			Config: a2a.PushConfig{
				ID:  "config-123",
				URL: "https://webhook.example.com",
			},
		})

		if err != nil {
			t.Fatalf("SetTaskPushConfig failed: %v", err)
		}

		if config.TaskID != "task-123" {
			t.Errorf("got taskId %s, want task-123", config.TaskID)
		}

		if config.Config.URL != "https://webhook.example.com" {
			t.Errorf("got URL %s, want https://webhook.example.com", config.Config.URL)
		}
	})

	t.Run("Delete", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			req := mustDecodeJSONRPC(t, r, "tasks/pushNotificationConfig/delete")

			resp := newResponse(req, json.RawMessage(`{}`))
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		transport := NewJSONRPCTransport(server.URL, nil)

		err := transport.DeleteTaskPushConfig(t.Context(), &a2a.DeleteTaskPushConfigParams{
			TaskID: "task-123",
		})

		if err != nil {
			t.Fatalf("DeleteTaskPushConfig failed: %v", err)
		}
	})
}

func TestJSONRPCTransport_DefaultTimeout(t *testing.T) {
	// Test that default transport has 5-second timeout (matching Python SDK)
	transport := NewJSONRPCTransport("http://example.com", nil)

	// Access internal transport to check HTTP client timeout
	jt := transport.(*jsonrpcTransport)
	expectedTimeout := 5 * time.Second

	if jt.httpClient.Timeout != expectedTimeout {
		t.Errorf("got timeout %v, want %v", jt.httpClient.Timeout, expectedTimeout)
	}
}

func TestJSONRPCTransport_WithHTTPClient(t *testing.T) {
	customClient := &http.Client{
		Timeout: 10 * time.Second,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonrpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("Failed to decode request: %v", err)
			return
		}

		resp := newResponse(
			req,
			json.RawMessage(`{"id":"task-123","contextId":"ctx-123","status":{"state":"completed"}}`),
		)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	transport := NewJSONRPCTransport(server.URL, nil, WithHTTPClient(customClient))

	// Verify custom client is used
	jt := transport.(*jsonrpcTransport)
	if jt.httpClient.Timeout != 10*time.Second {
		t.Errorf("got timeout %v, want 10s", jt.httpClient.Timeout)
	}

	task, err := transport.GetTask(t.Context(), &a2a.TaskQueryParams{
		ID: "task-123",
	})

	if err != nil {
		t.Fatalf("GetTask failed: %v", err)
	}

	if task.ID != "task-123" {
		t.Errorf("got task ID %s, want task-123", task.ID)
	}
}
