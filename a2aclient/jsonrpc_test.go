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
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
)

func TestJSONRPCTransport_SendMessage(t *testing.T) {
	// Create a mock server that returns a Task
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != "POST" {
			t.Errorf("Expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Expected Content-Type: application/json, got %s", r.Header.Get("Content-Type"))
		}

		// Parse request
		var req jsonrpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Failed to decode request: %v", err)
		}

		if req.JSONRPC != "2.0" {
			t.Errorf("Expected jsonrpc: 2.0, got %s", req.JSONRPC)
		}
		if req.Method != "message/send" {
			t.Errorf("Expected method: message/send, got %s", req.Method)
		}

		// Send response
		resp := jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  json.RawMessage(`{"kind":"task","id":"task-123","contextId":"ctx-123","status":{"state":"submitted"}}`),
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Create transport
	transport := NewJSONRPCTransport(server.URL, nil)

	// Send message
	result, err := transport.SendMessage(context.Background(), &a2a.MessageSendParams{
		Message: a2a.NewMessage(a2a.MessageRoleUser, &a2a.TextPart{Text: "test message"}),
	})

	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}

	task, ok := result.(*a2a.Task)
	if !ok {
		t.Fatalf("Expected Task result, got %T", result)
	}

	if task.ID != "task-123" {
		t.Errorf("Expected task ID task-123, got %s", task.ID)
	}
}

func TestJSONRPCTransport_SendMessage_MessageResult(t *testing.T) {
	// Create a mock server that returns a Message instead of Task
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonrpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Failed to decode request: %v", err)
		}

		if req.Method != "message/send" {
			t.Errorf("Expected method: message/send, got %s", req.Method)
		}

		// Send Message response (has "role" field, not "status" field)
		resp := jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  json.RawMessage(`{"kind":"message","messageId":"msg-123","role":"agent","parts":[{"kind":"text","text":"Hello"}]}`),
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	transport := NewJSONRPCTransport(server.URL, nil)

	result, err := transport.SendMessage(context.Background(), &a2a.MessageSendParams{
		Message: a2a.NewMessage(a2a.MessageRoleUser, &a2a.TextPart{Text: "test message"}),
	})

	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}

	msg, ok := result.(*a2a.Message)
	if !ok {
		t.Fatalf("Expected Message result, got %T", result)
	}

	if msg.ID != "msg-123" {
		t.Errorf("Expected message ID msg-123, got %s", msg.ID)
	}

	if msg.Role != a2a.MessageRoleAgent {
		t.Errorf("Expected role agent, got %s", msg.Role)
	}
}

func TestJSONRPCTransport_GetTask(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonrpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("Failed to decode request: %v", err)
			return
		}

		if req.Method != "tasks/get" {
			t.Errorf("Expected method: tasks/get, got %s", req.Method)
		}

		resp := jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  json.RawMessage(`{"kind":"task","id":"task-123","contextId":"ctx-123","status":{"state":"completed"}}`),
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	transport := NewJSONRPCTransport(server.URL, nil)

	task, err := transport.GetTask(context.Background(), &a2a.TaskQueryParams{
		ID: "task-123",
	})

	if err != nil {
		t.Fatalf("GetTask failed: %v", err)
	}

	if task.ID != "task-123" {
		t.Errorf("Expected task ID task-123, got %s", task.ID)
	}
	if task.Status.State != a2a.TaskStateCompleted {
		t.Errorf("Expected status completed, got %s", task.Status.State)
	}
}

func TestJSONRPCTransport_ErrorHandling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonrpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("Failed to decode request: %v", err)
			return
		}

		resp := jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &jsonrpcError{
				Code:    -32600,
				Message: "Invalid Request",
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	transport := NewJSONRPCTransport(server.URL, nil)

	_, err := transport.GetTask(context.Background(), &a2a.TaskQueryParams{
		ID: "task-123",
	})

	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	jsonrpcErr, ok := err.(*jsonrpcError)
	if !ok {
		t.Fatalf("Expected jsonrpcError, got %T", err)
	}

	if jsonrpcErr.Code != -32600 {
		t.Errorf("Expected error code -32600, got %d", jsonrpcErr.Code)
	}
}

func TestJSONRPCTransport_SendStreamingMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Errorf("Expected Accept: text/event-stream, got %s", r.Header.Get("Accept"))
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
	for event, err := range transport.SendStreamingMessage(context.Background(), &a2a.MessageSendParams{
		Message: a2a.NewMessage(a2a.MessageRoleUser, &a2a.TextPart{Text: "test"}),
	}) {
		if err != nil {
			t.Fatalf("Stream error: %v", err)
		}
		events = append(events, event)
	}

	if len(events) != 3 {
		t.Errorf("Expected 3 events, got %d", len(events))
	}

	// First event should be a Task
	if _, ok := events[0].(*a2a.Task); !ok {
		t.Errorf("Expected first event to be Task, got %T", events[0])
	}

	// Second event should be a Message
	if _, ok := events[1].(*a2a.Message); !ok {
		t.Errorf("Expected second event to be Message, got %T", events[1])
	}

	// Third event should be a Task
	if _, ok := events[2].(*a2a.Task); !ok {
		t.Errorf("Expected third event to be Task, got %T", events[2])
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
		t.Errorf("Expected 2 results, got %d", len(results))
	}
}

func TestJSONRPCTransport_ResubscribeToTask(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonrpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("Failed to decode request: %v", err)
			return
		}

		if req.Method != "tasks/resubscribe" {
			t.Errorf("Expected method: tasks/resubscribe, got %s", req.Method)
		}

		if r.Header.Get("Accept") != "text/event-stream" {
			t.Errorf("Expected Accept: text/event-stream, got %s", r.Header.Get("Accept"))
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
	for event, err := range transport.ResubscribeToTask(context.Background(), &a2a.TaskIDParams{
		ID: "task-123",
	}) {
		if err != nil {
			t.Fatalf("Stream error: %v", err)
		}
		events = append(events, event)
	}

	if len(events) != 2 {
		t.Errorf("Expected 2 events, got %d", len(events))
	}

	// First event should be a Task
	if _, ok := events[0].(*a2a.Task); !ok {
		t.Errorf("Expected first event to be Task, got %T", events[0])
	}

	// Second event should be a TaskStatusUpdateEvent
	if _, ok := events[1].(*a2a.TaskStatusUpdateEvent); !ok {
		t.Errorf("Expected second event to be TaskStatusUpdateEvent, got %T", events[1])
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

		result, err := transport.GetAgentCard(context.Background())
		if err != nil {
			t.Fatalf("GetAgentCard failed: %v", err)
		}

		if result.Name != "Test Agent" {
			t.Errorf("Expected name 'Test Agent', got %s", result.Name)
		}
	})

	t.Run("returns provided card", func(t *testing.T) {
		card := &a2a.AgentCard{
			Name:        "Test Agent",
			URL:         "http://example.com",
			Description: "Test description",
		}

		transport := NewJSONRPCTransport("http://example.com", card)

		result, err := transport.GetAgentCard(context.Background())
		if err != nil {
			t.Fatalf("GetAgentCard failed: %v", err)
		}

		if result.Name != "Test Agent" {
			t.Errorf("Expected name 'Test Agent', got %s", result.Name)
		}

		if result.Description != "Test description" {
			t.Errorf("Expected description 'Test description', got %s", result.Description)
		}
	})

	t.Run("no card provided", func(t *testing.T) {
		transport := NewJSONRPCTransport("http://example.com", nil)

		_, err := transport.GetAgentCard(context.Background())
		if err == nil {
			t.Fatal("Expected error when no card provided, got nil")
		}
	})
}

func TestJSONRPCTransport_CancelTask(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonrpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("Failed to decode request: %v", err)
			return
		}

		if req.Method != "tasks/cancel" {
			t.Errorf("Expected method: tasks/cancel, got %s", req.Method)
		}

		resp := jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  json.RawMessage(`{"kind":"task","id":"task-123","contextId":"ctx-123","status":{"state":"canceled"}}`),
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	transport := NewJSONRPCTransport(server.URL, nil)

	task, err := transport.CancelTask(context.Background(), &a2a.TaskIDParams{
		ID: "task-123",
	})

	if err != nil {
		t.Fatalf("CancelTask failed: %v", err)
	}

	if task.Status.State != a2a.TaskStateCanceled {
		t.Errorf("Expected status canceled, got %s", task.Status.State)
	}
}

func TestJSONRPCTransport_PushNotificationConfig(t *testing.T) {
	t.Run("Get", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req jsonrpcRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("Failed to decode request: %v", err)
			}

			if req.Method != "tasks/pushNotificationConfig/get" {
				t.Errorf("Expected method: tasks/pushNotificationConfig/get, got %s", req.Method)
			}

			resp := jsonrpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  json.RawMessage(`{"taskId":"task-123","pushNotificationConfig":{"id":"config-123","url":"https://webhook.example.com"}}`),
			}
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		transport := NewJSONRPCTransport(server.URL, nil)

		config, err := transport.GetTaskPushConfig(context.Background(), &a2a.GetTaskPushConfigParams{
			TaskID: "task-123",
		})

		if err != nil {
			t.Fatalf("GetTaskPushConfig failed: %v", err)
		}

		if config.TaskID != "task-123" {
			t.Errorf("Expected taskId task-123, got %s", config.TaskID)
		}
	})

	t.Run("List", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req jsonrpcRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("Failed to decode request: %v", err)
			}

			if req.Method != "tasks/pushNotificationConfig/list" {
				t.Errorf("Expected method: tasks/pushNotificationConfig/list, got %s", req.Method)
			}

			resp := jsonrpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  json.RawMessage(`[{"taskId":"task-1","pushNotificationConfig":{"id":"config-1","url":"https://webhook1.example.com"}},{"taskId":"task-2","pushNotificationConfig":{"id":"config-2","url":"https://webhook2.example.com"}}]`),
			}
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		transport := NewJSONRPCTransport(server.URL, nil)

		configs, err := transport.ListTaskPushConfig(context.Background(), &a2a.ListTaskPushConfigParams{})

		if err != nil {
			t.Fatalf("ListTaskPushConfig failed: %v", err)
		}

		if len(configs) != 2 {
			t.Errorf("Expected 2 configs, got %d", len(configs))
		}
	})

	t.Run("Set", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req jsonrpcRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("Failed to decode request: %v", err)
			}

			if req.Method != "tasks/pushNotificationConfig/set" {
				t.Errorf("Expected method: tasks/pushNotificationConfig/set, got %s", req.Method)
			}

			resp := jsonrpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  json.RawMessage(`{"taskId":"task-123","pushNotificationConfig":{"id":"config-123","url":"https://webhook.example.com"}}`),
			}
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		transport := NewJSONRPCTransport(server.URL, nil)

		config, err := transport.SetTaskPushConfig(context.Background(), &a2a.TaskPushConfig{
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
			t.Errorf("Expected taskId task-123, got %s", config.TaskID)
		}

		if config.Config.URL != "https://webhook.example.com" {
			t.Errorf("Expected URL https://webhook.example.com, got %s", config.Config.URL)
		}
	})

	t.Run("Delete", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req jsonrpcRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("Failed to decode request: %v", err)
			}

			if req.Method != "tasks/pushNotificationConfig/delete" {
				t.Errorf("Expected method: tasks/pushNotificationConfig/delete, got %s", req.Method)
			}

			resp := jsonrpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  json.RawMessage(`{}`),
			}
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		transport := NewJSONRPCTransport(server.URL, nil)

		err := transport.DeleteTaskPushConfig(context.Background(), &a2a.DeleteTaskPushConfigParams{
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
		t.Errorf("Expected default timeout %v, got %v", expectedTimeout, jt.httpClient.Timeout)
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

		resp := jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  json.RawMessage(`{"id":"task-123","contextId":"ctx-123","status":{"state":"completed"}}`),
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	transport := NewJSONRPCTransport(server.URL, nil, WithHTTPClient(customClient))

	// Verify custom client is used
	jt := transport.(*jsonrpcTransport)
	if jt.httpClient.Timeout != 10*time.Second {
		t.Errorf("Expected custom timeout 10s, got %v", jt.httpClient.Timeout)
	}

	task, err := transport.GetTask(context.Background(), &a2a.TaskQueryParams{
		ID: "task-123",
	})

	if err != nil {
		t.Fatalf("GetTask failed: %v", err)
	}

	if task.ID != "task-123" {
		t.Errorf("Expected task ID task-123, got %s", task.ID)
	}
}

func TestJSONRPCTransport_ErrorMethod(t *testing.T) {
	err := &jsonrpcError{
		Code:    -32600,
		Message: "Invalid Request",
		Data:    json.RawMessage(`{"details":"extra info"}`),
	}

	errStr := err.Error()
	if errStr != "jsonrpc error -32600: Invalid Request (data: {\"details\":\"extra info\"})" {
		t.Errorf("Unexpected error string: %s", errStr)
	}

	// Test without data field
	err2 := &jsonrpcError{
		Code:    -32601,
		Message: "Method not found",
	}

	errStr2 := err2.Error()
	if errStr2 != "jsonrpc error -32601: Method not found" {
		t.Errorf("Unexpected error string: %s", errStr2)
	}
}

func TestJSONRPCTransport_GetAgentCard_Concurrent(t *testing.T) {
	// Test that concurrent calls to GetAgentCard don't cause race conditions
	card := &a2a.AgentCard{
		Name:        "Test Agent",
		URL:         "http://example.com",
		Description: "Test description",
	}

	transport := NewJSONRPCTransport("http://example.com", card)

	// Launch 10 concurrent goroutines calling GetAgentCard
	const numGoroutines = 10
	errChan := make(chan error, numGoroutines)
	cardChan := make(chan *a2a.AgentCard, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			result, err := transport.GetAgentCard(context.Background())
			if err != nil {
				errChan <- err
				return
			}
			cardChan <- result
		}()
	}

	// Collect results
	cards := []*a2a.AgentCard{}
	for i := 0; i < numGoroutines; i++ {
		select {
		case err := <-errChan:
			t.Fatalf("GetAgentCard failed: %v", err)
		case card := <-cardChan:
			cards = append(cards, card)
		}
	}

	// Verify all goroutines got the same card
	for i, result := range cards {
		if result.Name != "Test Agent" {
			t.Errorf("Goroutine %d: Expected name 'Test Agent', got %s", i, result.Name)
		}
		if result.Description != "Test description" {
			t.Errorf("Goroutine %d: Expected description 'Test description', got %s", i, result.Description)
		}
	}
}
