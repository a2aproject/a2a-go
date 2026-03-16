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
	"iter"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	a2alegacy "github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

// --- server tests ---

func TestREST_ServerSendMessage(t *testing.T) {
	mock := &mockRESTHandler{}
	handler := NewRESTHandler(mock)
	server := httptest.NewServer(handler)
	defer server.Close()

	// v0.3 REST uses snake_case JSON keys
	body := `{"message":{"message_id":"m1","role":"user","parts":[{"kind":"text","text":"hello"}]}}`
	req, _ := http.NewRequest("POST", server.URL+"/message:send", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("failed to close body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	// Response should be a v0.3 event (Message or Task), not a StreamResponse wrapper.
	var raw map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	// The v0.3 REST response has snake_case keys: "message_id" and "role".
	if _, ok := raw["message_id"]; !ok {
		t.Errorf("expected v0.3 message response with 'message_id' field, got: %v", raw)
	}
}

func TestREST_ServerGetTask(t *testing.T) {
	mock := &mockRESTHandler{}
	handler := NewRESTHandler(mock)
	server := httptest.NewServer(handler)
	defer server.Close()

	req, _ := http.NewRequest("GET", server.URL+"/tasks/task-123", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("failed to close body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var raw map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if id, _ := raw["id"].(string); id != "task-123" {
		t.Errorf("expected task id 'task-123', got %q", id)
	}
}

func TestREST_ServerExtensionsFrom(t *testing.T) {
	mock := &mockExtensionRESTHandler{}
	handler := NewRESTHandler(mock)
	server := httptest.NewServer(handler)
	defer server.Close()

	legacyKey := "x-" + strings.ToLower(a2a.SvcParamExtensions)
	// v0.3 REST uses snake_case JSON keys
	body := `{"message":{"message_id":"m1","role":"user","parts":[{"kind":"text","text":"hello"}]}}`
	req, _ := http.NewRequest("POST", server.URL+"/message:send", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(legacyKey, "uri1")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("failed to close body: %v", err)
		}
	}()

	if len(mock.lastRequestedURIs) != 1 || mock.lastRequestedURIs[0] != "uri1" {
		t.Errorf("expected RequestedURIs [uri1], got %v", mock.lastRequestedURIs)
	}
}

func TestREST_ServerStreamMessage(t *testing.T) {
	mock := &mockStreamingRESTHandler{}
	handler := NewRESTHandler(mock)
	server := httptest.NewServer(handler)
	defer server.Close()

	// v0.3 REST uses snake_case JSON keys
	body := `{"message":{"message_id":"m1","role":"user","parts":[{"kind":"text","text":"hello"}]}}`
	req, _ := http.NewRequest("POST", server.URL+"/message:stream", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("failed to close body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("expected text/event-stream content type, got %q", ct)
	}

	// Read one SSE event
	scanner := bufio.NewScanner(resp.Body)
	var dataLine string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data:") {
			dataLine = strings.TrimPrefix(line, "data:")
			dataLine = strings.TrimSpace(dataLine)
			break
		}
	}
	if dataLine == "" {
		t.Fatal("expected at least one SSE data event")
	}
	// Should be a v0.3 event (not a StreamResponse wrapper)
	var raw map[string]any
	if err := json.Unmarshal([]byte(dataLine), &raw); err != nil {
		t.Fatalf("failed to parse SSE data: %v", err)
	}
	if _, ok := raw["message_id"]; !ok {
		t.Errorf("expected v0.3 message in SSE event with 'message_id' field, got: %v", raw)
	}
}

// --- client round-trip test ---

func TestREST_ClientSendMessage(t *testing.T) {
	// Spin up a fake v0.3 REST server that responds with a legacy Message
	legacyMsg := a2alegacy.Message{
		ID:   "resp-1",
		Role: a2alegacy.MessageRoleAgent,
		Parts: a2alegacy.ContentParts{
			a2alegacy.TextPart{Text: "hi from v0.3"},
		},
	}
	fakeServer := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		// v0.3 REST server responds with snake_case JSON
		data, _ := marshalSnakeCase(legacyMsg)
		_, _ = rw.Write(data)
	}))
	defer fakeServer.Close()

	transport, err := NewRESTTransport(RESTTransportConfig{URL: fakeServer.URL})
	if err != nil {
		t.Fatalf("failed to create transport: %v", err)
	}

	sendMsg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hello"))
	sendMsg.ID = "m1"
	result, err := transport.SendMessage(context.Background(), a2aclient.ServiceParams{}, &a2a.SendMessageRequest{
		Message: sendMsg,
	})
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}
	msg, ok := result.(*a2a.Message)
	if !ok {
		t.Fatalf("expected *a2a.Message, got %T", result)
	}
	if msg.ID != "resp-1" {
		t.Errorf("expected message id 'resp-1', got %q", msg.ID)
	}
}

func TestREST_ClientGetTask(t *testing.T) {
	legacyTask := a2alegacy.Task{
		ID:     "task-456",
		Status: a2alegacy.TaskStatus{State: a2alegacy.TaskStateCompleted},
	}
	fakeServer := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		// v0.3 REST server responds with snake_case JSON
		data, _ := marshalSnakeCase(legacyTask)
		_, _ = rw.Write(data)
	}))
	defer fakeServer.Close()

	transport, err := NewRESTTransport(RESTTransportConfig{URL: fakeServer.URL})
	if err != nil {
		t.Fatalf("failed to create transport: %v", err)
	}

	task, err := transport.GetTask(context.Background(), a2aclient.ServiceParams{}, &a2a.GetTaskRequest{
		ID: "task-456",
	})
	if err != nil {
		t.Fatalf("GetTask failed: %v", err)
	}
	if task.ID != "task-456" {
		t.Errorf("expected task id 'task-456', got %q", task.ID)
	}
	if task.Status.State != a2a.TaskStateCompleted {
		t.Errorf("expected completed state, got %q", task.Status.State)
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
