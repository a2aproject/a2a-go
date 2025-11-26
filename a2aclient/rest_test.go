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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/a2aproject/a2a-go/a2a"
)

func TestRESTTransport_GetTask(t *testing.T) {
	// Set up a mock HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify Method and Path
		if r.Method != http.MethodGet {
			t.Errorf("expected method GET, got %s", r.Method)
		}
		if r.URL.Path != "/v1/tasks/task-123" {
			t.Errorf("expected path /v1/tasks/task-123, got %s", r.URL.Path)
		}

		// Mock response
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"kind":"task","id":"task-123","contextId":"ctx-123","status":{"state":"completed"}}`))
	}))
	defer server.Close()

	// Create the REST transport pointing to the mock server
	transport := NewRESTTransport(server.URL, server.Client())

	// Call GetTask and log unexpected errors
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

func TestRESTTransport_CancelTask(t *testing.T) {
	// Set up a mock HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify Method and Path
		if r.Method != http.MethodPost {
			t.Errorf("expected method POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/tasks/task-123:cancel" {
			t.Errorf("expected path /v1/tasks/task-123:cancel, got %s", r.URL.Path)
		}

		// Mock response
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"kind":"task","id":"task-123","contextId":"ctx-123","status":{"state":"canceled"}}`))
	}))
	defer server.Close()

	// Create the REST transport pointing to the mock server
	transport := NewRESTTransport(server.URL, server.Client())

	// Call CancelTask and log unexpected errors
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

func TestRESTTransport_SendMessage(t *testing.T) {
	// Set up a mock HTTP
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify Method and Path
		if r.Method != http.MethodPost {
			t.Errorf("expected method POST, got %s", r.Method)
		}

		if r.URL.Path != "/v1/message:send" {
			t.Errorf("expected path /v1/message:send, got %s", r.URL.Path)
		}

		// Mock response
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"kind":"task","id":"task-123","contextId":"ctx-123","status":{"state":"submitted"}}`))
	}))
	defer server.Close()

	// Create the REST transport pointing to the mock server
	transport := NewRESTTransport(server.URL, server.Client())

	// Call SendMessage and log unexpected errors
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

func TestRESTTransport_ResubscribeToTask(t *testing.T) {
	// Set up a mock HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify Method, Path and Headers
		if r.Method != http.MethodPost {
			t.Errorf("expected method POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/tasks/task-123:subscribe" {
			t.Errorf("expected path /v1/tasks/task-123:subscribe, got %s", r.URL.Path)
		}
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Errorf("got Accept %s, want text/event-stream", r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "text/event-stream")

		// Send task updates via SSE
		events := []string{
			`data: {"kind":"task","id":"task-123","contextId":"ctx-123","status":{"state":"working"}}`,
			``,
			`data: {"kind":"status-update","taskId":"task-123","contextId":"ctx-123","final":false,"status":{"state":"completed"}}`,
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

	// Create the REST transport pointing to the mock server
	transport := NewRESTTransport(server.URL, server.Client())

	// Call ResubscribeToTask
	events := []a2a.Event{}
	for event, err := range transport.ResubscribeToTask(t.Context(), &a2a.TaskIDParams{
		ID: "task-123",
	}) {
		if err != nil {
			t.Fatalf("Stream error: %v", err)
		}
		events = append(events, event)
	}

	// Verify received events, log unexpected results
	if len(events) != 2 {
		t.Errorf("got %d events, want 2", len(events))
	}

	// Verify first event is Task
	if _, ok := events[0].(*a2a.Task); !ok {
		t.Errorf("got events[0] type %T, want *Task", events[0])
	}

	// Verify second event is TaskStatusUpdateEvent
	if _, ok := events[1].(*a2a.TaskStatusUpdateEvent); !ok {
		t.Errorf("got events[1] type %T, want *TaskStatusUpdateEvent", events[1])
	}
}

func TestRESTTransport_SendStreamingMessage(t *testing.T) {
	// Set up a mock HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify Method, Path and Headers
		if r.Method != http.MethodPost {
			t.Errorf("expected method POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/message:stream" {
			t.Errorf("expected path /v1/message:stream, got %s", r.URL.Path)
		}
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Errorf("got Accept %s, want text/event-stream", r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "text/event-stream")

		// Send task updates via SSE
		events := []string{
			`data: {"kind":"task", "id":"task-123","contextId":"ctx-123","status":{"state":"working"}}`,
			``,
			`data: {"kind":"message","messageId":"msg-1","role":"agent","parts":[{"kind":"text","text":"Processing..."}]}`,
			``,
			`data: {"kind":"task","id":"task-123","contextId":"ctx-123","status":{"state":"completed"}}`,
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

	// Create the REST transport pointing to the mock server
	transport := NewRESTTransport(server.URL, server.Client())

	//Call SendStreamingMessage
	events := []a2a.Event{}
	for event, err := range transport.SendStreamingMessage(t.Context(), &a2a.MessageSendParams{
		Message: a2a.NewMessage(a2a.MessageRoleUser, &a2a.TextPart{Text: "test message"}),
	}) {
		if err != nil {
			t.Fatalf("Stream error: %v", err)
		}
		events = append(events, event)
	}

	// Verify received events, log unexpected results
	if len(events) != 3 {
		t.Errorf("got %d events, want 3", len(events))
	}

	//Verify first event is Task
	if _, ok := events[0].(*a2a.Task); !ok {
		t.Errorf("got events[0] type %T, want *Task", events[0])
	}

	// Verify second event is Message
	if _, ok := events[1].(*a2a.Message); !ok {
		t.Errorf("got events[1] type %T, want *Message", events[1])
	}

	// Verify third event is Task
	if _, ok := events[2].(*a2a.Task); !ok {
		t.Errorf("got events[2] type %T, want *Task", events[2])
	}
}

func TestRESTTransport_GetTaskPushConfig(t *testing.T) {
	// Set up a mock HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify Method and Path
		if r.Method != http.MethodGet {
			t.Errorf("expected method GET, got %s", r.Method)
		}
		if r.URL.Path != "/v1/tasks/task-123/pushNotificationConfigs/config-123" {
			t.Errorf("expected path /v1/tasks/task-123/pushNotificationConfigs/config-123, got %s", r.URL.Path)
		}

		// Mock response
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"taskId":"task-123","pushNotificationConfig":{"id":"config-123","url":"https://webhook.example.com"}}`))
	}))
	defer server.Close()

	// Create the REST transport pointing to the mock server
	transport := NewRESTTransport(server.URL, server.Client())

	//Call GetTaskPushConfig and log unexpected errors
	config, err := transport.GetTaskPushConfig(t.Context(), &a2a.GetTaskPushConfigParams{
		TaskID:   a2a.TaskID("task-123"),
		ConfigID: "config-123",
	})

	if err != nil {
		t.Fatalf("GetTaskPushConfig failed: %v", err)
	}

	if config.TaskID != "task-123" {
		t.Errorf("got TaskID %s, want task-123", config.TaskID)
	}
	if config.Config.ID != "config-123" {
		t.Errorf("got Config ID %s, want config-123", config.Config.ID)
	}
	if config.Config.URL != "https://webhook.example.com" {
		t.Errorf("got Config URL %s, want https://webhook.example.com", config.Config.URL)
	}
}

func TestRESTTransport_ListTaskPushConfig(t *testing.T) {
	// Set up a mock HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify Method and Path
		if r.Method != http.MethodGet {
			t.Errorf("expected method GET, got %s", r.Method)
		}
		if r.URL.Path != "/v1/tasks/task-123/pushNotificationConfigs" {
			t.Errorf("expected path /v1/tasks/task-123/pushNotificationConfigs, got %s", r.URL.Path)
		}

		// Mock response
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"taskId":"task-123","pushNotificationConfig":{"id":"config-1","url":"https://webhook1.example.com"}},
			{"taskId":"task-123","pushNotificationConfig":{"id":"config-2","url":"https://webhook2.example.com"}}
		]`))
	}))
	defer server.Close()

	// Create the REST transport pointing to the mock server
	transport := NewRESTTransport(server.URL, server.Client())

	// Call ListTaskPushConfig and log unexpected errors
	configs, err := transport.ListTaskPushConfig(t.Context(), &a2a.ListTaskPushConfigParams{
		TaskID: a2a.TaskID("task-123"),
	})
	if err != nil {
		t.Fatalf("ListTaskPushConfig failed: %v", err)
	}

	if len(configs) != 2 {
		t.Errorf("got %d configs, want 2", len(configs))
	}
	if configs[0].TaskID != "task-123" || configs[0].Config.ID != "config-1" {
		t.Errorf("got first config %+v, want taskId task-1 and configId config-1", configs[0])
	}
	if configs[1].TaskID != "task-123" || configs[1].Config.ID != "config-2" {
		t.Errorf("got second config %+v, want taskId task-2 and configId config-2", configs[1])
	}
}

func TestRESTTransport_SetTaskPushConfig(t *testing.T) {
	// Set up a mock HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify Method and Path
		if r.Method != http.MethodPost {
			t.Errorf("expected method POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/tasks/task-123/pushNotificationConfigs" {
			t.Errorf("expected path /v1/tasks/task-123/pushNotificationConfigs, got %s", r.URL.Path)
		}

		// Mock response
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"taskId":"task-123","pushNotificationConfig":{"id":"config-123","url":"https://webhook.example.com"}}`))
	}))
	defer server.Close()

	// Create the REST transport pointing to the mock server
	transport := NewRESTTransport(server.URL, server.Client())

	// Call SetTaskPushConfig and log unexpected errors
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
	if config.Config.ID != "config-123" {
		t.Errorf("got config ID %s, want config-123", config.Config.ID)
	}
	if config.Config.URL != "https://webhook.example.com" {
		t.Errorf("got config URL %s, want https://webhook.example.com", config.Config.URL)
	}
}

func TestRESTTransport_DeleteTaskPushConfig(t *testing.T) {
	// Set up a mock HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify Method and Path
		if r.Method != http.MethodDelete {
			t.Errorf("expected method DELETE, got %s", r.Method)
		}
		if r.URL.Path != "/v1/tasks/task-123/pushNotificationConfigs/config-123" {
			t.Errorf("expected path /v1/tasks/task-123/pushNotificationConfigs/config-123, got %s", r.URL.Path)
		}
	}))
	defer server.Close()

	// Create the REST transport pointing to the mock server
	transport := NewRESTTransport(server.URL, server.Client())

	// Call DeleteTaskPushConfig and log unexpected errors
	err := transport.DeleteTaskPushConfig(t.Context(), &a2a.DeleteTaskPushConfigParams{
		TaskID:   a2a.TaskID("task-123"),
		ConfigID: "config-123",
	})

	if err != nil {
		t.Fatalf("DeleteTaskPushConfig failed: %v", err)
	}
}

func TestRESTTransport_GetAgentCard(t *testing.T) {
	// Set up a mock HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify Method and Path
		if r.Method != http.MethodGet {
			t.Errorf("expected method GET, got %s", r.Method)
		}
		if r.URL.Path != "/v1/card" {
			t.Errorf("expected path /v1/card, got %s", r.URL.Path)
		}

		// Mock response
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"url": "http://example.com", "name": "Test agent", "description":"test"}`))
	}))
	defer server.Close()

	// Create the REST transport pointing to the mock server
	transport := NewRESTTransport(server.URL, server.Client())

	// Call GetAgentCard and log unexpected errors
	card, err := transport.GetAgentCard(t.Context())
	if err != nil {
		t.Fatalf("GetAgentCard failed: %v", err)
	}

	if card.URL != "http://example.com" {
		t.Errorf("got card URL %s, want http://example.com", card.URL)
	}
	if card.Name != "Test agent" {
		t.Errorf("got card Name %s, want Test agent", card.Name)
	}
	if card.Description != "test" {
		t.Errorf("got card Description %s, want test", card.Description)
	}
}
