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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/google/go-cmp/cmp"
)

func TestJSONRPCTransport_QueryParamsFromContext(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var gotQuery url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotQuery = r.URL.Query()
		mu.Unlock()

		var req struct {
			ID any `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("Decode() error = %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result": map[string]any{
				"kind":      "task",
				"id":        "task-123",
				"contextId": "ctx-123",
				"status":    map[string]any{"state": "completed"},
			},
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("Encode() error = %v", err)
		}
	}))
	t.Cleanup(server.Close)

	transport := NewJSONRPCTransport(JSONRPCTransportConfig{URL: server.URL})
	ctx := a2aclient.AttachHTTPQueryParams(t.Context(), url.Values{"api_key": []string{"secret"}})

	task, err := transport.GetTask(ctx, a2aclient.ServiceParams{}, &a2a.GetTaskRequest{ID: "task-123"})
	if err != nil {
		t.Fatalf("transport.GetTask() error = %v", err)
	}
	if task.ID != "task-123" {
		t.Fatalf("transport.GetTask().ID = %q, want task-123", task.ID)
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

	transport := NewJSONRPCTransport(JSONRPCTransportConfig{URL: serverURL})
	ctx := a2aclient.AttachHTTPQueryParams(t.Context(), url.Values{"api_key": []string{"super-secret"}})

	_, err := transport.GetTask(ctx, a2aclient.ServiceParams{}, &a2a.GetTaskRequest{ID: "task-123"})
	if err == nil {
		t.Fatalf("transport.GetTask() error = nil, want non-nil")
	}
	if strings.Contains(err.Error(), "super-secret") {
		t.Errorf("transport.GetTask() error = %v, must not contain query params", err)
	}
}

func TestRESTTransport_QueryParamsFromContext(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var gotQuery url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotQuery = r.URL.Query()
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		// v0.3 REST bodies are proto-JSON of google.a2a.v1.
		_, _ = w.Write([]byte(`{"id":"task-123","contextId":"ctx-123","status":{"state":"TASK_STATE_COMPLETED"}}`))
	}))
	t.Cleanup(server.Close)

	transport, err := NewRESTTransport(RESTTransportConfig{URL: server.URL})
	if err != nil {
		t.Fatalf("NewRESTTransport() error = %v", err)
	}
	ctx := a2aclient.AttachHTTPQueryParams(t.Context(), url.Values{"api_key": []string{"secret"}})

	task, err := transport.GetTask(ctx, a2aclient.ServiceParams{}, &a2a.GetTaskRequest{ID: "task-123"})
	if err != nil {
		t.Fatalf("transport.GetTask() error = %v", err)
	}
	if task.ID != "task-123" {
		t.Fatalf("transport.GetTask().ID = %q, want task-123", task.ID)
	}

	want := url.Values{"api_key": []string{"secret"}}
	if diff := cmp.Diff(want, gotQuery); diff != "" {
		t.Errorf("wrong query params (-want +got) diff = %s", diff)
	}
}

func TestRESTTransport_ErrorRedactsQueryParams(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	serverURL := server.URL
	server.Close()

	transport, err := NewRESTTransport(RESTTransportConfig{URL: serverURL})
	if err != nil {
		t.Fatalf("NewRESTTransport() error = %v", err)
	}
	ctx := a2aclient.AttachHTTPQueryParams(t.Context(), url.Values{"api_key": []string{"super-secret"}})

	_, err = transport.GetTask(ctx, a2aclient.ServiceParams{}, &a2a.GetTaskRequest{ID: "task-123"})
	if err == nil {
		t.Fatalf("transport.GetTask() error = nil, want non-nil")
	}
	if strings.Contains(err.Error(), "super-secret") {
		t.Errorf("transport.GetTask() error = %v, must not contain query params", err)
	}
}
