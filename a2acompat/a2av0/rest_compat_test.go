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

package a2av0

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"iter"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

// --- server tests ---

// v0.3 REST bodies are proto-JSON of google.a2a.v1 (see rest_proto_json.go).
// The request wire shape for message:send/stream is:
//
//	{"message": {"messageId": "...", "role": "ROLE_USER", "content": [{"text": "hello"}]}}
const protoJSONSendMessageBody = `{"message":{"messageId":"m1","role":"ROLE_USER","content":[{"text":"hello"}]}}`

func TestREST_ServerSendMessage(t *testing.T) {
	t.Parallel()
	mock := &mockRESTHandler{}
	handler := NewRESTHandler(mock)
	server := httptest.NewServer(handler)
	defer server.Close()

	req, _ := http.NewRequest("POST", server.URL+"/v1/message:send", strings.NewReader(protoJSONSendMessageBody))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v, want nil", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("Close() error = %v, want nil", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("StatusCode = %d, want 200; body = %s", resp.StatusCode, string(body))
	}

	// Response is a SendMessageResponse envelope with proto-JSON keys.
	var raw map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatalf("Decode() error = %v, want nil", err)
	}
	msg, ok := raw["message"].(map[string]any)
	if !ok {
		t.Fatalf("response has no 'message' envelope, got: %v", raw)
	}
	if _, ok := msg["messageId"]; !ok {
		t.Fatalf("response 'message' has no 'messageId', got: %v", msg)
	}
	if role, _ := msg["role"].(string); role != "ROLE_AGENT" {
		t.Fatalf("response 'message.role' = %q, want %q", role, "ROLE_AGENT")
	}
}

func TestREST_ServerGetTask(t *testing.T) {
	t.Parallel()
	mock := &mockRESTHandler{}
	handler := NewRESTHandler(mock)
	server := httptest.NewServer(handler)
	defer server.Close()

	req, _ := http.NewRequest("GET", server.URL+"/v1/tasks/task-123", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v, want nil", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("Close() error = %v, want nil", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want 200", resp.StatusCode)
	}

	var raw map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatalf("Decode() error = %v, want nil", err)
	}
	if id, _ := raw["id"].(string); id != "task-123" {
		t.Fatalf("Task.id = %q, want %q", id, "task-123")
	}
	status, _ := raw["status"].(map[string]any)
	if state, _ := status["state"].(string); state != "TASK_STATE_COMPLETED" {
		t.Fatalf("Task.status.state = %q, want %q", state, "TASK_STATE_COMPLETED")
	}
}

func TestREST_ServerCancelTask(t *testing.T) {
	t.Parallel()
	mock := &mockRESTHandler{}
	handler := NewRESTHandler(mock)
	server := httptest.NewServer(handler)
	defer server.Close()

	req, _ := http.NewRequest("POST", server.URL+"/v1/tasks/task-42:cancel", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v, want nil", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("Close() error = %v, want nil", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("StatusCode = %d, want 200; body = %s", resp.StatusCode, string(body))
	}
	var raw map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatalf("Decode() error = %v, want nil", err)
	}
	if id, _ := raw["id"].(string); id != "task-42" {
		t.Fatalf("Task.id = %q, want %q", id, "task-42")
	}
}

func TestREST_ServerSubscribeTask_UsesGET(t *testing.T) {
	t.Parallel()
	mock := &mockStreamingRESTHandler{}
	handler := NewRESTHandler(mock)
	server := httptest.NewServer(handler)
	defer server.Close()

	req, _ := http.NewRequest("GET", server.URL+"/v1/tasks/task-9:subscribe", nil)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v, want nil", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("Close() error = %v, want nil", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("StatusCode = %d, want 200; body = %s", resp.StatusCode, string(body))
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream prefix", ct)
	}
}

func TestREST_ServerExtensionsFrom(t *testing.T) {
	t.Parallel()
	mock := &mockExtensionRESTHandler{}
	handler := NewRESTHandler(mock)
	server := httptest.NewServer(handler)
	defer server.Close()

	legacyKey := "x-" + strings.ToLower(a2a.SvcParamExtensions)
	req, _ := http.NewRequest("POST", server.URL+"/v1/message:send", strings.NewReader(protoJSONSendMessageBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(legacyKey, "uri1")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v, want nil", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("Close() error = %v, want nil", err)
		}
	}()

	if len(mock.lastRequestedURIs) != 1 || mock.lastRequestedURIs[0] != "uri1" {
		t.Fatalf("mock.lastRequestedURIs = %v, want [uri1]", mock.lastRequestedURIs)
	}
}

func TestREST_ServerStreamMessage(t *testing.T) {
	t.Parallel()
	mock := &mockStreamingRESTHandler{}
	handler := NewRESTHandler(mock)
	server := httptest.NewServer(handler)
	defer server.Close()

	req, _ := http.NewRequest("POST", server.URL+"/v1/message:stream", strings.NewReader(protoJSONSendMessageBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v, want nil", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("Close() error = %v, want nil", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream prefix", ct)
	}

	// Read one SSE event
	scanner := bufio.NewScanner(resp.Body)
	var dataLine string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data:") {
			dataLine = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			break
		}
	}
	if dataLine == "" {
		t.Fatal("expected at least one SSE data event")
	}
	// SSE event body is a StreamResponse envelope: {"message": {...}}
	var raw map[string]any
	if err := json.Unmarshal([]byte(dataLine), &raw); err != nil {
		t.Fatalf("Unmarshal(SSE) error = %v, want nil", err)
	}
	msg, ok := raw["message"].(map[string]any)
	if !ok {
		t.Fatalf("SSE event has no 'message' envelope, got: %v", raw)
	}
	if _, ok := msg["messageId"]; !ok {
		t.Fatalf("message has no 'messageId', got: %v", msg)
	}
}

func TestREST_ServerListTasks(t *testing.T) {
	t.Parallel()
	mock := &mockRESTHandler{}
	server := httptest.NewServer(NewRESTHandler(mock))
	defer server.Close()

	resp, err := http.Get(server.URL + "/v1/tasks?contextId=c1&pageSize=5")
	if err != nil {
		t.Fatalf("Get() error = %v, want nil", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("Close() error = %v, want nil", err)
		}
	}()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("StatusCode = %d, want 200; body = %s", resp.StatusCode, string(body))
	}
	var raw struct {
		Tasks         []map[string]any `json:"tasks"`
		NextPageToken string           `json:"nextPageToken"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatalf("Decode() error = %v, want nil", err)
	}
	if len(raw.Tasks) != 2 {
		t.Fatalf("len(Tasks) = %d, want 2", len(raw.Tasks))
	}
	if raw.NextPageToken != "np" {
		t.Fatalf("NextPageToken = %q, want %q", raw.NextPageToken, "np")
	}
}

func TestREST_ServerListPushConfigs(t *testing.T) {
	t.Parallel()
	mock := &mockRESTHandler{}
	server := httptest.NewServer(NewRESTHandler(mock))
	defer server.Close()

	resp, err := http.Get(server.URL + "/v1/tasks/t1/pushNotificationConfigs")
	if err != nil {
		t.Fatalf("Get() error = %v, want nil", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("Close() error = %v, want nil", err)
		}
	}()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("StatusCode = %d, want 200; body = %s", resp.StatusCode, string(body))
	}
	var raw struct {
		Configs []map[string]any `json:"configs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatalf("Decode() error = %v, want nil", err)
	}
	if len(raw.Configs) != 2 {
		t.Fatalf("len(Configs) = %d, want 2", len(raw.Configs))
	}
	if raw.Configs[0]["name"] != "tasks/t1/pushNotificationConfigs/pc1" {
		t.Fatalf("Configs[0].name = %v, want tasks/t1/pushNotificationConfigs/pc1", raw.Configs[0]["name"])
	}
}

func TestREST_ServerDeleteTaskPushConfig(t *testing.T) {
	t.Parallel()
	mock := &mockRESTHandler{}
	server := httptest.NewServer(NewRESTHandler(mock))
	defer server.Close()

	req, _ := http.NewRequest("DELETE", server.URL+"/v1/tasks/t1/pushNotificationConfigs/pc1", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v, want nil", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("Close() error = %v, want nil", err)
		}
	}()
	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("StatusCode = %d, want 204; body = %s", resp.StatusCode, string(body))
	}
}

// --- client round-trip tests ---

func TestREST_ClientSendMessage(t *testing.T) {
	t.Parallel()
	var (
		gotPath   string
		gotMethod string
		gotBody   []byte
	)
	fakeServer := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		gotPath = req.URL.Path
		gotMethod = req.Method
		gotBody, _ = io.ReadAll(req.Body)
		rw.Header().Set("Content-Type", "application/json")
		_, _ = rw.Write([]byte(`{"message":{"messageId":"resp-1","role":"ROLE_AGENT","content":[{"text":"hi from v0.3"}]}}`))
	}))
	defer fakeServer.Close()

	transport, err := NewRESTTransport(RESTTransportConfig{URL: fakeServer.URL})
	if err != nil {
		t.Fatalf("NewRESTTransport() error = %v, want nil", err)
	}

	sendMsg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hello"))
	sendMsg.ID = "m1"
	result, err := transport.SendMessage(context.Background(), a2aclient.ServiceParams{}, &a2a.SendMessageRequest{
		Message: sendMsg,
	})
	if err != nil {
		t.Fatalf("SendMessage() error = %v, want nil", err)
	}
	if gotPath != "/v1/message:send" {
		t.Fatalf("client sent path %q, want %q", gotPath, "/v1/message:send")
	}
	if gotMethod != "POST" {
		t.Fatalf("client used %q, want POST", gotMethod)
	}
	// Confirm the request body uses "content" (not "parts") and proto-JSON role names.
	if !strings.Contains(string(gotBody), `"content":`) {
		t.Fatalf("request body missing 'content' key: %s", string(gotBody))
	}
	if !strings.Contains(string(gotBody), `"role":"ROLE_USER"`) {
		t.Fatalf("request body missing 'role\":\"ROLE_USER\"': %s", string(gotBody))
	}
	msg, ok := result.(*a2a.Message)
	if !ok {
		t.Fatalf("SendMessage() = %T, want *a2a.Message", result)
	}
	if msg.ID != "resp-1" {
		t.Fatalf("Message.ID = %q, want %q", msg.ID, "resp-1")
	}
	if msg.Role != a2a.MessageRoleAgent {
		t.Fatalf("Message.Role = %q, want %q", msg.Role, a2a.MessageRoleAgent)
	}
}

func TestREST_ClientGetTask(t *testing.T) {
	t.Parallel()
	var gotPath string
	fakeServer := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		gotPath = req.URL.Path
		rw.Header().Set("Content-Type", "application/json")
		_, _ = rw.Write([]byte(`{"id":"task-456","status":{"state":"TASK_STATE_COMPLETED"}}`))
	}))
	defer fakeServer.Close()

	transport, err := NewRESTTransport(RESTTransportConfig{URL: fakeServer.URL})
	if err != nil {
		t.Fatalf("NewRESTTransport() error = %v, want nil", err)
	}

	task, err := transport.GetTask(context.Background(), a2aclient.ServiceParams{}, &a2a.GetTaskRequest{
		ID: "task-456",
	})
	if err != nil {
		t.Fatalf("GetTask() error = %v, want nil", err)
	}
	if gotPath != "/v1/tasks/task-456" {
		t.Fatalf("client sent path %q, want %q", gotPath, "/v1/tasks/task-456")
	}
	if task.ID != "task-456" {
		t.Fatalf("Task.ID = %q, want %q", task.ID, a2a.TaskID("task-456"))
	}
	if task.Status.State != a2a.TaskStateCompleted {
		t.Fatalf("Task.Status.State = %q, want %q", task.Status.State, a2a.TaskStateCompleted)
	}
}

func TestREST_ClientSubscribeToTask_UsesGET(t *testing.T) {
	t.Parallel()
	var gotMethod, gotPath string
	fakeServer := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		gotMethod = req.Method
		gotPath = req.URL.Path
		rw.Header().Set("Content-Type", "text/event-stream")
		rw.WriteHeader(http.StatusOK)
		_, _ = rw.Write([]byte("data: {\"statusUpdate\":{\"taskId\":\"t9\",\"contextId\":\"c1\",\"status\":{\"state\":\"TASK_STATE_COMPLETED\"},\"final\":true}}\n\n"))
	}))
	defer fakeServer.Close()

	transport, err := NewRESTTransport(RESTTransportConfig{URL: fakeServer.URL})
	if err != nil {
		t.Fatalf("NewRESTTransport() error = %v, want nil", err)
	}
	events := transport.SubscribeToTask(context.Background(), a2aclient.ServiceParams{}, &a2a.SubscribeToTaskRequest{ID: "t9"})
	var got a2a.Event
	for evt, err := range events {
		if err != nil {
			t.Fatalf("SubscribeToTask stream error = %v, want nil", err)
		}
		got = evt
		break
	}
	if gotMethod != "GET" {
		t.Fatalf("subscribe method = %q, want GET", gotMethod)
	}
	if gotPath != "/v1/tasks/t9:subscribe" {
		t.Fatalf("subscribe path = %q, want %q", gotPath, "/v1/tasks/t9:subscribe")
	}
	su, ok := got.(*a2a.TaskStatusUpdateEvent)
	if !ok {
		t.Fatalf("received %T, want *a2a.TaskStatusUpdateEvent", got)
	}
	if su.TaskID != "t9" {
		t.Fatalf("TaskStatusUpdateEvent.TaskID = %q, want %q", su.TaskID, a2a.TaskID("t9"))
	}
}

func TestREST_ClientSubscribeToTask_ServerAcceptsPOST(t *testing.T) {
	t.Parallel()
	mock := &mockStreamingRESTHandler{}
	handler := NewRESTHandler(mock)
	server := httptest.NewServer(handler)
	defer server.Close()

	req, _ := http.NewRequest("POST", server.URL+"/v1/tasks/t2:subscribe", strings.NewReader(`{"name":"tasks/t2"}`))
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v, want nil", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("Close() error = %v, want nil", err)
		}
	}()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("StatusCode = %d, want 200; body = %s", resp.StatusCode, string(body))
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream prefix", ct)
	}
}

func TestREST_ClientListTasks_RoundTrip(t *testing.T) {
	t.Parallel()
	var gotPath, gotQuery string
	fakeServer := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		gotPath = req.URL.Path
		gotQuery = req.URL.RawQuery
		rw.Header().Set("Content-Type", "application/json")
		_, _ = rw.Write([]byte(`{"tasks":[{"id":"t1","status":{"state":"TASK_STATE_COMPLETED"}},{"id":"t2","status":{"state":"TASK_STATE_WORKING"}}],"nextPageToken":"next"}`))
	}))
	defer fakeServer.Close()

	transport, err := NewRESTTransport(RESTTransportConfig{URL: fakeServer.URL})
	if err != nil {
		t.Fatalf("NewRESTTransport() error = %v, want nil", err)
	}
	resp, err := transport.ListTasks(context.Background(), a2aclient.ServiceParams{}, &a2a.ListTasksRequest{
		ContextID: "ctx-1",
		PageSize:  10,
		PageToken: "pt",
	})
	if err != nil {
		t.Fatalf("ListTasks() error = %v, want nil", err)
	}
	if gotPath != "/v1/tasks" {
		t.Fatalf("client sent to %q, want %q", gotPath, "/v1/tasks")
	}
	if !strings.Contains(gotQuery, "contextId=ctx-1") || !strings.Contains(gotQuery, "pageSize=10") || !strings.Contains(gotQuery, "pageToken=pt") {
		t.Fatalf("query = %q, want to contain contextId=ctx-1, pageSize=10, pageToken=pt", gotQuery)
	}
	if len(resp.Tasks) != 2 || resp.Tasks[0].ID != "t1" || resp.Tasks[1].ID != "t2" {
		t.Fatalf("Tasks = %+v, want two tasks t1 and t2", resp.Tasks)
	}
	if resp.NextPageToken != "next" {
		t.Fatalf("NextPageToken = %q, want %q", resp.NextPageToken, "next")
	}
}

func TestREST_ClientListTasks_AcceptsBareArray(t *testing.T) {
	t.Parallel()
	fakeServer := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		_, _ = rw.Write([]byte(`[{"id":"t1","status":{"state":"TASK_STATE_COMPLETED"}}]`))
	}))
	defer fakeServer.Close()

	transport, err := NewRESTTransport(RESTTransportConfig{URL: fakeServer.URL})
	if err != nil {
		t.Fatalf("NewRESTTransport() error = %v, want nil", err)
	}
	resp, err := transport.ListTasks(context.Background(), a2aclient.ServiceParams{}, &a2a.ListTasksRequest{})
	if err != nil {
		t.Fatalf("ListTasks() error = %v, want nil", err)
	}
	if len(resp.Tasks) != 1 || resp.Tasks[0].ID != "t1" {
		t.Fatalf("Tasks = %+v, want [t1]", resp.Tasks)
	}
}

func TestREST_ClientListTaskPushConfigs_RoundTrip(t *testing.T) {
	t.Parallel()
	var gotPath string
	fakeServer := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		gotPath = req.URL.Path
		rw.Header().Set("Content-Type", "application/json")
		_, _ = rw.Write([]byte(`{"configs":[{"name":"tasks/t1/pushNotificationConfigs/pc1","pushNotificationConfig":{"id":"pc1","url":"http://cb"}}]}`))
	}))
	defer fakeServer.Close()

	transport, err := NewRESTTransport(RESTTransportConfig{URL: fakeServer.URL})
	if err != nil {
		t.Fatalf("NewRESTTransport() error = %v, want nil", err)
	}
	got, err := transport.ListTaskPushConfigs(context.Background(), a2aclient.ServiceParams{}, &a2a.ListTaskPushConfigRequest{
		TaskID: "t1",
	})
	if err != nil {
		t.Fatalf("ListTaskPushConfigs() error = %v, want nil", err)
	}
	if gotPath != "/v1/tasks/t1/pushNotificationConfigs" {
		t.Fatalf("client sent to %q, want %q", gotPath, "/v1/tasks/t1/pushNotificationConfigs")
	}
	if len(got) != 1 || got[0].ID != "pc1" || got[0].TaskID != "t1" {
		t.Fatalf("got = %+v, want single {TaskID=t1, ID=pc1}", got)
	}
}

func TestREST_ClientListTaskPushConfigs_AcceptsBareArray(t *testing.T) {
	t.Parallel()
	fakeServer := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		_, _ = rw.Write([]byte(`[{"name":"tasks/t1/pushNotificationConfigs/pc1","pushNotificationConfig":{"id":"pc1","url":"http://cb"}}]`))
	}))
	defer fakeServer.Close()

	transport, err := NewRESTTransport(RESTTransportConfig{URL: fakeServer.URL})
	if err != nil {
		t.Fatalf("NewRESTTransport() error = %v, want nil", err)
	}
	got, err := transport.ListTaskPushConfigs(context.Background(), a2aclient.ServiceParams{}, &a2a.ListTaskPushConfigRequest{
		TaskID: "t1",
	})
	if err != nil {
		t.Fatalf("ListTaskPushConfigs() error = %v, want nil", err)
	}
	if len(got) != 1 || got[0].ID != "pc1" {
		t.Fatalf("got = %+v, want single pc1", got)
	}
}

func TestREST_ClientDeleteTaskPushConfig_RoundTrip(t *testing.T) {
	t.Parallel()
	var gotMethod, gotPath string
	fakeServer := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		gotMethod = req.Method
		gotPath = req.URL.Path
		rw.WriteHeader(http.StatusNoContent)
	}))
	defer fakeServer.Close()

	transport, err := NewRESTTransport(RESTTransportConfig{URL: fakeServer.URL})
	if err != nil {
		t.Fatalf("NewRESTTransport() error = %v, want nil", err)
	}
	err = transport.DeleteTaskPushConfig(context.Background(), a2aclient.ServiceParams{}, &a2a.DeleteTaskPushConfigRequest{
		TaskID: "t1",
		ID:     "pc1",
	})
	if err != nil {
		t.Fatalf("DeleteTaskPushConfig() error = %v, want nil", err)
	}
	if gotMethod != "DELETE" {
		t.Fatalf("gotMethod = %q, want DELETE", gotMethod)
	}
	if gotPath != "/v1/tasks/t1/pushNotificationConfigs/pc1" {
		t.Fatalf("gotPath = %q, want %q", gotPath, "/v1/tasks/t1/pushNotificationConfigs/pc1")
	}
}

func TestREST_ClientCreateTaskPushConfig(t *testing.T) {
	t.Parallel()
	var gotBody []byte
	fakeServer := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		gotBody, _ = io.ReadAll(req.Body)
		rw.Header().Set("Content-Type", "application/json")
		_, _ = rw.Write([]byte(`{"name":"tasks/t1/pushNotificationConfigs/pc1","pushNotificationConfig":{"id":"pc1","url":"http://cb","token":"tk"}}`))
	}))
	defer fakeServer.Close()

	transport, err := NewRESTTransport(RESTTransportConfig{URL: fakeServer.URL})
	if err != nil {
		t.Fatalf("NewRESTTransport() error = %v, want nil", err)
	}
	got, err := transport.CreateTaskPushConfig(context.Background(), a2aclient.ServiceParams{}, &a2a.PushConfig{
		TaskID: "t1",
		ID:     "pc1",
		URL:    "http://cb",
		Token:  "tk",
	})
	if err != nil {
		t.Fatalf("CreateTaskPushConfig() error = %v, want nil", err)
	}
	// The request body wraps the PushConfig inside pushNotificationConfig and
	// carries a resource-name field.
	if !strings.Contains(string(gotBody), `"name":"tasks/t1/pushNotificationConfigs/pc1"`) {
		t.Fatalf("request body missing resource name: %s", string(gotBody))
	}
	if !strings.Contains(string(gotBody), `"pushNotificationConfig":`) {
		t.Fatalf("request body missing pushNotificationConfig wrapper: %s", string(gotBody))
	}
	if got.TaskID != "t1" || got.ID != "pc1" {
		t.Fatalf("PushConfig{TaskID=%q, ID=%q}, want {t1, pc1}", got.TaskID, got.ID)
	}
}

// --- mock handlers ---

type mockRESTHandler struct {
	a2asrv.RequestHandler
}

func (h *mockRESTHandler) SendMessage(_ context.Context, req *a2a.SendMessageRequest) (a2a.SendMessageResult, error) {
	msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("ok"))
	msg.ID = req.Message.ID + "-resp"
	return msg, nil
}

func (h *mockRESTHandler) GetTask(_ context.Context, req *a2a.GetTaskRequest) (*a2a.Task, error) {
	return &a2a.Task{
		ID:     req.ID,
		Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
	}, nil
}

func (h *mockRESTHandler) CancelTask(_ context.Context, req *a2a.CancelTaskRequest) (*a2a.Task, error) {
	return &a2a.Task{
		ID:     req.ID,
		Status: a2a.TaskStatus{State: a2a.TaskStateCanceled},
	}, nil
}

func (h *mockRESTHandler) ListTasks(_ context.Context, req *a2a.ListTasksRequest) (*a2a.ListTasksResponse, error) {
	return &a2a.ListTasksResponse{
		Tasks: []*a2a.Task{
			{ID: "t1", ContextID: req.ContextID, Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}},
			{ID: "t2", ContextID: req.ContextID, Status: a2a.TaskStatus{State: a2a.TaskStateWorking}},
		},
		NextPageToken: "np",
	}, nil
}

func (h *mockRESTHandler) ListTaskPushConfigs(_ context.Context, req *a2a.ListTaskPushConfigRequest) (*a2a.ListTaskPushConfigResponse, error) {
	return &a2a.ListTaskPushConfigResponse{
		Configs: []*a2a.PushConfig{
			{TaskID: req.TaskID, ID: "pc1", URL: "http://cb"},
			{TaskID: req.TaskID, ID: "pc2", URL: "http://cb2"},
		},
	}, nil
}

func (h *mockRESTHandler) DeleteTaskPushConfig(_ context.Context, _ *a2a.DeleteTaskPushConfigRequest) error {
	return nil
}

type mockExtensionRESTHandler struct {
	a2asrv.RequestHandler
	lastRequestedURIs []string
}

func (h *mockExtensionRESTHandler) SendMessage(ctx context.Context, _ *a2a.SendMessageRequest) (a2a.SendMessageResult, error) {
	if ext, ok := a2asrv.ExtensionsFrom(ctx); ok {
		h.lastRequestedURIs = ext.RequestedURIs()
	}
	msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("ok"))
	msg.ID = "resp-1"
	return msg, nil
}

type mockStreamingRESTHandler struct {
	a2asrv.RequestHandler
}

func (h *mockStreamingRESTHandler) SendStreamingMessage(_ context.Context, req *a2a.SendMessageRequest) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("streaming ok"))
		msg.ID = req.Message.ID + "-stream"
		yield(msg, nil)
	}
}

func (h *mockStreamingRESTHandler) SubscribeToTask(_ context.Context, req *a2a.SubscribeToTaskRequest) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		yield(&a2a.TaskStatusUpdateEvent{
			TaskID:    req.ID,
			ContextID: "c1",
			Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
		}, nil)
	}
}
