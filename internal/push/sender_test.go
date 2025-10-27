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

package push

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/google/go-cmp/cmp"
)

func TestNewHTTPPushSender(t *testing.T) {
	t.Run("with no timeout provided", func(t *testing.T) {
		sender := NewHTTPPushSender()
		if sender.client == nil {
			t.Fatal("expected a default client to be created, but it was nil")
		}
		if sender.client.Timeout != 30*time.Second {
			t.Errorf("expected default client timeout to be 30s, got %v", sender.client.Timeout)
		}
	})

	t.Run("with custom timeout", func(t *testing.T) {
		customTimeout := 10 * time.Second
		sender := NewHTTPPushSender(customTimeout)
		if sender.client.Timeout != customTimeout {
			t.Errorf("expected client timeout to be %v, got %v", customTimeout, sender.client.Timeout)
		}
	})
}

func TestHTTPPushSender_SendPush(t *testing.T) {
	ctx := context.Background()
	task := &a2a.Task{ID: "test-task", ContextID: "test-context"}

	var receivedTask a2a.Task
	var receivedHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "can't read body", http.StatusBadRequest)
			return
		}
		if err := json.Unmarshal(body, &receivedTask); err != nil {
			http.Error(w, "can't unmarshal body", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	t.Run("success with token", func(t *testing.T) {
		config := &a2a.PushConfig{URL: server.URL, Token: "test-token"}
		sender := NewHTTPPushSender()

		err := sender.SendPush(ctx, config, task)
		if err != nil {
			t.Fatalf("SendPush() failed: %v", err)
		}

		if diff := cmp.Diff(task, &receivedTask); diff != "" {
			t.Errorf("Received task mismatch (-want +got):\n%s", diff)
		}
		if got := receivedHeaders.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type header = %q, want %q", got, "application/json")
		}
		if got := receivedHeaders.Get(tokenHeader); got != "test-token" {
			t.Errorf("%q header = %q, want %q", tokenHeader, got, "test-token")
		}
	})

	t.Run("success with bearer auth", func(t *testing.T) {
		config := &a2a.PushConfig{
			URL: server.URL,
			Auth: &a2a.PushAuthInfo{
				Schemes:     []string{"Bearer"},
				Credentials: "my-bearer-token",
			},
		}
		sender := NewHTTPPushSender()

		err := sender.SendPush(ctx, config, task)
		if err != nil {
			t.Fatalf("SendPush() failed: %v", err)
		}

		if got := receivedHeaders.Get("Authorization"); got != "Bearer my-bearer-token" {
			t.Errorf("Authorization header = %q, want %q", got, "Bearer my-bearer-token")
		}
	})

	t.Run("success with basic auth", func(t *testing.T) {
		config := &a2a.PushConfig{URL: server.URL, Auth: &a2a.PushAuthInfo{Schemes: []string{"Basic"}, Credentials: "dXNlcjpwYXNz"}}
		sender := NewHTTPPushSender()

		err := sender.SendPush(ctx, config, task)
		if err != nil {
			t.Fatalf("SendPush() failed: %v", err)
		}

		if got := receivedHeaders.Get("Authorization"); got != "Basic dXNlcjpwYXNz" {
			t.Errorf("Authorization header = %q, want %q", got, "Basic dXNlcjpwYXNz")
		}
	})

	t.Run("success without token", func(t *testing.T) {
		config := &a2a.PushConfig{URL: server.URL}
		sender := NewHTTPPushSender()

		err := sender.SendPush(ctx, config, task)
		if err != nil {
			t.Fatalf("SendPush() failed: %v", err)
		}

		if _, ok := receivedHeaders[tokenHeader]; ok {
			t.Error("%w header should not be set", tokenHeader)
		}
	})

	t.Run("json marshal fails", func(t *testing.T) {
		badTask := &a2a.Task{Metadata: map[string]any{"a": make(chan int)}}
		config := &a2a.PushConfig{URL: server.URL}
		sender := NewHTTPPushSender()

		err := sender.SendPush(ctx, config, badTask)
		if err == nil || !strings.Contains(err.Error(), "failed to serialize event to JSON") {
			t.Errorf("SendPush() error = %v, want JSON marshal error", err)
		}
	})

	t.Run("invalid request URL", func(t *testing.T) {
		config := &a2a.PushConfig{URL: "::"}
		sender := NewHTTPPushSender()

		err := sender.SendPush(ctx, config, task)
		if err == nil || !strings.Contains(err.Error(), "failed to create HTTP request") {
			t.Errorf("SendPush() error = %v, want request creation error", err)
		}
	})

	t.Run("http client fails", func(t *testing.T) {
		config := &a2a.PushConfig{URL: "http://localhost:1"}
		sender := NewHTTPPushSender()

		err := sender.SendPush(ctx, config, task)
		if err == nil || !strings.Contains(err.Error(), "failed to send push notification") {
			t.Errorf("SendPush() error = %v, want client execution error", err)
		}
	})

	t.Run("non-success status code", func(t *testing.T) {
		errorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "internal server error", http.StatusInternalServerError)
		}))
		defer errorServer.Close()

		config := &a2a.PushConfig{URL: errorServer.URL}
		sender := NewHTTPPushSender()

		err := sender.SendPush(ctx, config, task)
		if err == nil || !strings.Contains(err.Error(), "push notification endpoint returned non-success status: 500 Internal Server Error") {
			t.Errorf("SendPush() error = %v, want non-success status error", err)
		}
	})

	t.Run("context canceled", func(t *testing.T) {
		blocker := make(chan struct{})
		slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			<-blocker
			w.WriteHeader(http.StatusOK)
		}))
		defer slowServer.Close()
		defer close(blocker)

		canceledCtx, cancel := context.WithCancel(ctx)
		cancel()

		config := &a2a.PushConfig{URL: slowServer.URL}
		sender := NewHTTPPushSender()

		err := sender.SendPush(canceledCtx, config, task)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("SendPush() error = %v, want context.Canceled", err)
		}
	})
}
