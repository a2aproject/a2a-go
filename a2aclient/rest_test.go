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
	"github.com/google/go-cmp/cmp"
)

func TestRESTTransport_GetTask(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected method GET, got %s", r.Method)
		}
		if r.URL.String() != "/tasks/task-123?historyLength=2" {
			t.Errorf("expected path /tasks/task-123?historyLength=2, got %s", r.URL.String())
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"kind":"task","id":"task-123","contextId":"ctx-123","status":{"state":"COMPLETED"},"history":[{"state":"COMPLETED"},{"state":"WORKING"}]}`))
	}))
	defer server.Close()
	transport := NewRESTTransport(server.URL, server.Client())
	historyLength := 2
	task, err := transport.GetTask(t.Context(), ServiceParams{}, &a2a.GetTaskRequest{
		ID:            "task-123",
		HistoryLength: &historyLength,
	})

	if err != nil {
		t.Fatalf("GetTask failed: %v", err)
	}

	if task.ID != "task-123" {
		t.Errorf("got task ID %s, want task-123", task.ID)
	}
	if len(task.History) != historyLength {
		t.Errorf("got history length %d, want %d", len(task.History), historyLength)
	}
	if task.Status.State != a2a.TaskStateCompleted {
		t.Errorf("got status %s, want completed", task.Status.State)
	}
}

func TestRESTTransport_ListTasks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected method GET, got %s", r.Method)
		}
		if r.URL.Path != "/tasks" {
			t.Errorf("expected path /tasks, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(
			`{
					"tasks":[
						{"kind":"task","id":"task-1","contextId":"ctx-1","status":{"state":"COMPLETED"}},
						{"kind":"task","id":"task-2","contextId":"ctx-2","status":{"state":"WORKING"}}
					], 
					"totalSize": 2, 
					"pageSize": 50, 
					"nextPageToken": "test-page-token"
				}`,
		),
		)
	}))
	defer server.Close()

	transport := NewRESTTransport(server.URL, server.Client())
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
		PageSize:      50,
		NextPageToken: "test-page-token",
	}
	listTasksResult, err := transport.ListTasks(t.Context(), ServiceParams{}, &a2a.ListTasksRequest{})
	if err != nil {
		t.Fatalf("ListTasks failed: %v", err)
	}

	if diff := cmp.Diff(listTasksResult, wantResult); diff != "" {
		t.Errorf("ListTasks() mismatch (+got -want):\n%s", diff)
	}
}

func TestRESTTransport_CancelTask(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected method POST, got %s", r.Method)
		}
		if r.URL.Path != "/tasks/task-123:cancel" {
			t.Errorf("expected path /tasks/task-123:cancel, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"kind":"task","id":"task-123","contextId":"ctx-123","status":{"state":"CANCELED"}}`))
	}))
	defer server.Close()
	transport := NewRESTTransport(server.URL, server.Client())

	task, err := transport.CancelTask(t.Context(), ServiceParams{}, &a2a.CancelTaskRequest{
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected method POST, got %s", r.Method)
		}

		if r.URL.Path != "/message:send" {
			t.Errorf("expected path /message:send, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"task":{"id":"task-123","contextId":"ctx-123","status":{"state":"SUBMITTED"}}}`))
	}))
	defer server.Close()
	transport := NewRESTTransport(server.URL, server.Client())

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

func TestRESTTransport_ResubscribeToTask(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected method POST, got %s", r.Method)
		}
		if r.URL.Path != "/tasks/task-123:subscribe" {
			t.Errorf("expected path /tasks/task-123:subscribe, got %s", r.URL.Path)
		}
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Errorf("got Accept %s, want text/event-stream", r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "text/event-stream")

		events := []string{
			`data: {"task":{"id":"task-123","contextId":"ctx-123","status":{"state":"WORKING"}}}`,
			``,
			`data: {"statusUpdate":{"taskId":"task-123","contextId":"ctx-123","final":false,"status":{"state":"COMPLETED"}}}`,
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

	transport := NewRESTTransport(server.URL, server.Client())

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
	if _, ok := events[0].(*a2a.Task); !ok {
		t.Errorf("got events[0] type %T, want *Task", events[0])
	}
	if _, ok := events[1].(*a2a.TaskStatusUpdateEvent); !ok {
		t.Errorf("got events[1] type %T, want *TaskStatusUpdateEvent", events[1])
	}
}

func TestRESTTransport_SendStreamingMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected method POST, got %s", r.Method)
		}
		if r.URL.Path != "/message:stream" {
			t.Errorf("expected path /message:stream, got %s", r.URL.Path)
		}
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Errorf("got Accept %s, want text/event-stream", r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "text/event-stream")

		events := []string{
			`data: {"task":{"id":"task-123","contextId":"ctx-123","status":{"state":"WORKING"}}}`,
			``,
			`data: {"message":{"messageId":"msg-1","role":"agent","parts":[{"text":"Processing..."}]}}`,
			``,
			`data: {"task":{"id":"task-123","contextId":"ctx-123","status":{"state":"COMPLETED"}}}`,
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

	transport := NewRESTTransport(server.URL, server.Client())

	events := []a2a.Event{}
	for event, err := range transport.SendStreamingMessage(t.Context(), ServiceParams{}, &a2a.SendMessageRequest{
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("test message")),
	}) {
		if err != nil {
			t.Fatalf("Stream error: %v", err)
		}
		events = append(events, event)
	}

	if len(events) != 3 {
		t.Errorf("got %d events, want 3", len(events))
	}
	if _, ok := events[0].(*a2a.Task); !ok {
		t.Errorf("got events[0] type %T, want *Task", events[0])
	}

	if _, ok := events[1].(*a2a.Message); !ok {
		t.Errorf("got events[1] type %T, want *Message", events[1])
	}
	if _, ok := events[2].(*a2a.Task); !ok {
		t.Errorf("got events[2] type %T, want *Task", events[2])
	}
}

func TestRESTTransport_GetTaskPushConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected method GET, got %s", r.Method)
		}
		if r.URL.Path != "/tasks/task-123/pushNotificationConfigs/config-123" {
			t.Errorf("expected path /tasks/task-123/pushNotificationConfigs/config-123, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"taskId":"task-123","config":{"id":"config-123","url":"https://webhook.example.com"}}`))
	}))
	defer server.Close()

	transport := NewRESTTransport(server.URL, server.Client())

	config, err := transport.GetTaskPushConfig(t.Context(), ServiceParams{}, &a2a.GetTaskPushConfigRequest{
		TaskID: a2a.TaskID("task-123"),
		ID:     "config-123",
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected method GET, got %s", r.Method)
		}
		if r.URL.Path != "/tasks/task-123/pushNotificationConfigs" {
			t.Errorf("expected path /tasks/task-123/pushNotificationConfigs, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"taskId":"task-123","config":{"id":"config-1","url":"https://webhook1.example.com"}},
			{"taskId":"task-123","config":{"id":"config-2","url":"https://webhook2.example.com"}}
		]`))
	}))
	defer server.Close()

	transport := NewRESTTransport(server.URL, server.Client())

	configs, err := transport.ListTaskPushConfig(t.Context(), ServiceParams{}, &a2a.ListTaskPushConfigRequest{
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected method POST, got %s", r.Method)
		}
		if r.URL.Path != "/tasks/task-123/pushNotificationConfigs" {
			t.Errorf("expected path /tasks/task-123/pushNotificationConfigs, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"taskId":"task-123","config":{"id":"config-123","url":"https://webhook.example.com"}}`))
	}))
	defer server.Close()

	transport := NewRESTTransport(server.URL, server.Client())

	config, err := transport.CreateTaskPushConfig(t.Context(), ServiceParams{}, &a2a.CreateTaskPushConfigRequest{
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected method DELETE, got %s", r.Method)
		}
		if r.URL.Path != "/tasks/task-123/pushNotificationConfigs/config-123" {
			t.Errorf("expected path /tasks/task-123/pushNotificationConfigs/config-123, got %s", r.URL.Path)
		}
	}))
	defer server.Close()

	transport := NewRESTTransport(server.URL, server.Client())

	err := transport.DeleteTaskPushConfig(t.Context(), ServiceParams{}, &a2a.DeleteTaskPushConfigRequest{
		TaskID: a2a.TaskID("task-123"),
		ID:     "config-123",
	})

	if err != nil {
		t.Fatalf("DeleteTaskPushConfig failed: %v", err)
	}
}

func TestRESTTransport_GetAgentCard(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected method GET, got %s", r.Method)
		}
		if r.URL.Path != "/extendedAgentCard" {
			t.Errorf("expected path /extendedAgentCard, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"supportedInterfaces":[{"url":"http://example.com"}], "name": "Test agent", "description":"test"}`))
	}))
	defer server.Close()

	transport := NewRESTTransport(server.URL, server.Client())

	card, err := transport.GetExtendedAgentCard(t.Context(), ServiceParams{}, &a2a.GetExtendedAgentCardRequest{})
	if err != nil {
		t.Fatalf("GetAgentCard failed: %v", err)
	}

	if card.Name != "Test agent" {
		t.Errorf("got card Name %s, want Test agent", card.Name)
	}
	if card.Description != "test" {
		t.Errorf("got card Description %s, want test", card.Description)
	}
}
