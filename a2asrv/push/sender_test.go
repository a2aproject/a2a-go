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

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/go-cmp/cmp"
)

func TestNewHTTPPushSender(t *testing.T) {
	t.Run("with no timeout provided", func(t *testing.T) {
		sender := NewHTTPPushSender(nil)
		if sender.client == nil {
			t.Fatal("expected a default client to be created, but it was nil")
		}
		if sender.client.Timeout != 30*time.Second {
			t.Errorf("expected default client timeout to be 30s, got %v", sender.client.Timeout)
		}
	})

	t.Run("with custom timeout", func(t *testing.T) {
		customTimeout := 10 * time.Second
		sender := NewHTTPPushSender(&HTTPSenderConfig{Timeout: customTimeout})
		if sender.client.Timeout != customTimeout {
			t.Errorf("expected client timeout to be %v, got %v", customTimeout, sender.client.Timeout)
		}
	})

	t.Run("failOnError defaults to false", func(t *testing.T) {
		sender := NewHTTPPushSender(nil)
		if sender.failOnError {
			t.Errorf("failOnError defaulted to true")
		}
	})
}

func TestHTTPPushSender_SendPushSuccess(t *testing.T) {
	ctx := context.Background()

	var received a2a.StreamResponse
	var receivedHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "can't read body", http.StatusBadRequest)
			return
		}
		if err := json.Unmarshal(body, &received); err != nil {
			http.Error(w, "can't unmarshal body", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tc := []struct {
		name  string
		event a2a.Event
	}{
		{
			name:  "task",
			event: &a2a.Task{ID: "test-task", ContextID: "test-context"},
		},
		{
			name:  "message",
			event: &a2a.Message{ID: "test-message", TaskID: "test-task", Parts: a2a.ContentParts{a2a.NewTextPart("test")}, Role: a2a.MessageRoleUser},
		},
		{
			name:  "status update",
			event: &a2a.TaskStatusUpdateEvent{ContextID: "test-context", TaskID: "test-task", Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}},
		},
		{
			name:  "artifact update",
			event: &a2a.TaskArtifactUpdateEvent{ContextID: "test-context", TaskID: "test-task", Artifact: &a2a.Artifact{ID: "test-artifact", Parts: a2a.ContentParts{a2a.NewTextPart("test")}}},
		},
	}

	for _, tt := range tc {
		t.Run(tt.name+" success with token", func(t *testing.T) {
			config := &a2a.PushConfig{URL: server.URL, Token: "test-token"}
			sender := NewHTTPPushSender(&HTTPSenderConfig{AllowPrivateNetworks: true})

			err := sender.SendPush(ctx, config, tt.event)
			if err != nil {
				t.Fatalf("SendPush() failed: %v", err)
			}
			if diff := cmp.Diff(tt.event, received.Event); diff != "" {
				t.Errorf("Received task mismatch (-want +got):\n%s", diff)
			}
			if got := receivedHeaders.Get("Content-Type"); got != "application/json" {
				t.Errorf("Content-Type header = %q, want %q", got, "application/json")
			}
			if got := receivedHeaders.Get(tokenHeader); got != "test-token" {
				t.Errorf("%q header = %q, want %q", tokenHeader, got, "test-token")
			}
		})

		t.Run(tt.name+" success with bearer auth", func(t *testing.T) {
			config := &a2a.PushConfig{
				URL: server.URL,
				Auth: &a2a.PushAuthInfo{
					Scheme:      "Bearer",
					Credentials: "my-bearer-token",
				},
			}
			sender := NewHTTPPushSender(&HTTPSenderConfig{AllowPrivateNetworks: true})

			err := sender.SendPush(ctx, config, tt.event)
			if err != nil {
				t.Fatalf("SendPush() failed: %v", err)
			}

			if got := receivedHeaders.Get("Authorization"); got != "Bearer my-bearer-token" {
				t.Errorf("Authorization header = %q, want %q", got, "Bearer my-bearer-token")
			}
		})

		t.Run(tt.name+" success with basic auth", func(t *testing.T) {
			config := &a2a.PushConfig{URL: server.URL, Auth: &a2a.PushAuthInfo{Scheme: "Basic", Credentials: "dXNlcjpwYXNz"}}
			sender := NewHTTPPushSender(&HTTPSenderConfig{AllowPrivateNetworks: true})

			err := sender.SendPush(ctx, config, tt.event)
			if err != nil {
				t.Fatalf("SendPush() failed: %v", err)
			}

			if got := receivedHeaders.Get("Authorization"); got != "Basic dXNlcjpwYXNz" {
				t.Errorf("Authorization header = %q, want %q", got, "Basic dXNlcjpwYXNz")
			}
		})

		t.Run(tt.name+" success without token", func(t *testing.T) {
			config := &a2a.PushConfig{URL: server.URL}
			sender := NewHTTPPushSender(&HTTPSenderConfig{AllowPrivateNetworks: true})

			err := sender.SendPush(ctx, config, tt.event)
			if err != nil {
				t.Fatalf("SendPush() failed: %v", err)
			}

			if _, ok := receivedHeaders[tokenHeader]; ok {
				t.Error("%w header should not be set", tokenHeader)
			}
		})
	}
}

func TestHTTPPushSender_SendPushError(t *testing.T) {
	ctx := context.Background()
	task := &a2a.Task{ID: "test-task", ContextID: "test-context"}
	message := &a2a.Message{ID: "test-message", TaskID: "test-task", Parts: a2a.ContentParts{a2a.NewTextPart("test")}, Role: a2a.MessageRoleUser}
	statusUpdate := &a2a.TaskStatusUpdateEvent{ContextID: "test-context", TaskID: "test-task", Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}}
	artifactUpdate := &a2a.TaskArtifactUpdateEvent{ContextID: "test-context", TaskID: "test-task", Artifact: &a2a.Artifact{ID: "test-artifact", Parts: a2a.ContentParts{a2a.NewTextPart("test")}}}
	events := []a2a.Event{task, message, statusUpdate, artifactUpdate}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token := r.Header.Get(tokenHeader); token == "fail" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	failedPushConfig := &a2a.PushConfig{URL: server.URL, ID: "test-task", Token: "fail"}

	testCases := []struct {
		name    string
		event   []a2a.Event
		config  *a2a.PushConfig
		wantErr string
	}{
		{
			name: "json marshal fails",
			event: []a2a.Event{
				&a2a.Task{ID: "test-task", Metadata: map[string]any{"a": make(chan int)}},
				&a2a.Message{ID: "test-message", TaskID: "test-task", Metadata: map[string]any{"a": func() {}}},
				&a2a.TaskStatusUpdateEvent{ContextID: "test-context", TaskID: "test-task", Metadata: map[string]any{"a": make(chan int)}},
				&a2a.TaskArtifactUpdateEvent{ContextID: "test-context", TaskID: "test-task", Metadata: map[string]any{"a": func() {}}},
			},
			wantErr: "failed to serialize event to JSON",
		},
		{
			name:    "invalid request URL",
			event:   events,
			config:  &a2a.PushConfig{URL: "::"},
			wantErr: "failed to create HTTP request",
		},
		{
			name:    "http client fails",
			event:   events,
			config:  &a2a.PushConfig{URL: "http://localhost:1"},
			wantErr: "failed to send push notification",
		},
		{
			name:    "non-success status code",
			event:   events,
			config:  failedPushConfig,
			wantErr: "push notification endpoint returned non-success status: 500 Internal Server Error",
		},
	}

	for _, failOnError := range []bool{true, false} {
		for _, tc := range testCases {
			name := tc.name
			if failOnError {
				name = tc.name + " (fail on error)"
			}
			t.Run(name, func(t *testing.T) {
				sender := NewHTTPPushSender(&HTTPSenderConfig{FailOnError: failOnError, AllowPrivateNetworks: true})
				for _, event := range tc.event {
					err := sender.SendPush(ctx, tc.config, event)
					if failOnError {
						if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
							t.Errorf("SendPush() error = %v, want error containing %s", err, tc.wantErr)
						}
					} else if err != nil {
						t.Errorf("SendPush() error = %v, want nil when failOnError false", err)
					}
				}
			})
		}
	}

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
		sender := NewHTTPPushSender(&HTTPSenderConfig{FailOnError: true, AllowPrivateNetworks: true})

		for _, event := range events {
			err := sender.SendPush(canceledCtx, config, event)
			if !errors.Is(err, context.Canceled) {
				t.Errorf("SendPush() error = %v, want context.Canceled", err)
			}
		}

		sender = NewHTTPPushSender(&HTTPSenderConfig{AllowPrivateNetworks: true})
		for _, event := range events {
			if err := sender.SendPush(canceledCtx, config, event); err != nil {
				t.Errorf("SendPush() error = %v, want nil when FailOnError false", err)
			}
		}
	})
}

func TestHTTPPushSender_SSRFProtection(t *testing.T) {
	ctx := context.Background()
	event := &a2a.Task{ID: "test-task", ContextID: "test-context"}

	t.Run("blocks private and loopback targets by default", func(t *testing.T) {
		blocked := []string{
			"http://127.0.0.1:8080/webhook",            // IPv4 loopback
			"https://127.0.0.1:8443/webhook",           // HTTPS also dials through the guard
			"http://localhost:8080/webhook",            // loopback by name
			"http://[::1]:8080/webhook",                // IPv6 loopback
			"http://169.254.169.254/latest/meta-data/", // cloud metadata (link-local)
			"http://10.0.0.5/webhook",                  // RFC 1918
			"http://192.168.1.10/webhook",              // RFC 1918
			"http://0.0.0.0:8080/webhook",              // unspecified
		}
		sender := NewHTTPPushSender(&HTTPSenderConfig{FailOnError: true})
		for _, url := range blocked {
			err := sender.SendPush(ctx, &a2a.PushConfig{URL: url}, event)
			if err == nil {
				t.Errorf("SendPush(%q) = nil, want an SSRF block error", url)
				continue
			}
			if !strings.Contains(err.Error(), "blocked address range") {
				t.Errorf("SendPush(%q) error = %v, want a blocked-address-range error", url, err)
			}
		}
	})

	t.Run("does not cancel execution when FailOnError is false", func(t *testing.T) {
		sender := NewHTTPPushSender(nil) // guard on, FailOnError off
		if err := sender.SendPush(ctx, &a2a.PushConfig{URL: "http://127.0.0.1:8080/"}, event); err != nil {
			t.Errorf("SendPush() = %v, want nil when FailOnError is false", err)
		}
	})

	t.Run("AllowPrivateNetworks opts out of the guard", func(t *testing.T) {
		delivered := false
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			delivered = true
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		sender := NewHTTPPushSender(&HTTPSenderConfig{FailOnError: true, AllowPrivateNetworks: true})
		if err := sender.SendPush(ctx, &a2a.PushConfig{URL: server.URL}, event); err != nil {
			t.Fatalf("SendPush() with AllowPrivateNetworks failed: %v", err)
		}
		if !delivered {
			t.Error("push was not delivered to the local server despite AllowPrivateNetworks")
		}
	})

	t.Run("strips notification token on cross-host redirect", func(t *testing.T) {
		var finalToken string
		var finalReached bool
		finalServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			finalReached = true
			finalToken = r.Header.Get(tokenHeader)
			w.WriteHeader(http.StatusOK)
		}))
		defer finalServer.Close()

		redirectServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, finalServer.URL, http.StatusTemporaryRedirect)
		}))
		defer redirectServer.Close()

		// AllowPrivateNetworks lets the loopback hops through; redirect hygiene
		// (token stripping across hosts) still applies.
		sender := NewHTTPPushSender(&HTTPSenderConfig{FailOnError: true, AllowPrivateNetworks: true})
		config := &a2a.PushConfig{URL: redirectServer.URL, Token: "secret-token"}
		if err := sender.SendPush(ctx, config, event); err != nil {
			t.Fatalf("SendPush() across redirect failed: %v", err)
		}
		if !finalReached {
			t.Fatal("redirect target was not reached")
		}
		if finalToken != "" {
			t.Errorf("notification token leaked to a different host on redirect: %q, want empty", finalToken)
		}
	})
}

func TestHTTPPushSender_Deliver(t *testing.T) {
	t.Parallel()
	event := &a2a.Task{ID: "test-task", ContextID: "test-context"}

	testCases := []struct {
		name           string
		failOnError    bool
		status         int
		wantStatusCode int
		wantResultErr  bool
		wantErr        bool
	}{
		{name: "success", status: http.StatusOK, wantStatusCode: http.StatusOK},
		{name: "failure", status: http.StatusInternalServerError, wantStatusCode: http.StatusInternalServerError, wantResultErr: true},
		{name: "failure with fail on error", failOnError: true, status: http.StatusInternalServerError, wantStatusCode: http.StatusInternalServerError, wantResultErr: true, wantErr: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var gotHeaders http.Header
			called := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				gotHeaders = r.Header
				w.WriteHeader(tc.status)
			}))
			defer server.Close()

			sender := NewHTTPPushSender(&HTTPSenderConfig{FailOnError: tc.failOnError, AllowPrivateNetworks: true})
			delivery := &Delivery{
				Config:  &a2a.PushConfig{URL: server.URL, Token: "test-token"},
				Event:   event,
				Headers: http.Header{"X-Custom": {"custom"}, tokenHeader: {"overridden"}},
			}
			result, err := sender.Deliver(t.Context(), delivery)

			if (err != nil) != tc.wantErr {
				t.Fatalf("sender.Deliver() error = %v, want error %v", err, tc.wantErr)
			}
			if (result.Err != nil) != tc.wantResultErr {
				t.Fatalf("sender.Deliver() result.Err = %v, want error %v", result.Err, tc.wantResultErr)
			}
			if result.StatusCode != tc.wantStatusCode {
				t.Fatalf("sender.Deliver() result.StatusCode = %d, want %d", result.StatusCode, tc.wantStatusCode)
			}
			if !called {
				t.Fatal("sender.Deliver() didn't call the endpoint")
			}
			if result.Duration <= 0 {
				t.Fatalf("sender.Deliver() result.Duration = %v, want > 0", result.Duration)
			}
			if got := gotHeaders.Get("X-Custom"); got != "custom" {
				t.Fatalf("X-Custom header = %q, want %q", got, "custom")
			}
			if got := gotHeaders.Values(tokenHeader); !cmp.Equal(got, []string{"test-token"}) {
				t.Fatalf("%q header = %q, want %q", tokenHeader, got, []string{"test-token"})
			}
		})
	}
}

type sendPushFunc func(ctx context.Context, config *a2a.PushConfig, event a2a.Event) error

func (fn sendPushFunc) SendPush(ctx context.Context, config *a2a.PushConfig, event a2a.Event) error {
	return fn(ctx, config, event)
}

func TestNewDeliveryHandler(t *testing.T) {
	t.Parallel()
	sendErr := errors.New("send failed")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	customSender := func(sent *int, err error) Sender {
		return sendPushFunc(func(context.Context, *a2a.PushConfig, a2a.Event) error {
			*sent++
			time.Sleep(time.Millisecond)
			return err
		})
	}
	httpSender := func(*int, error) Sender {
		return NewHTTPPushSender(&HTTPSenderConfig{AllowPrivateNetworks: true})
	}

	testCases := []struct {
		name     string
		sender   func(sent *int, err error) Sender
		sendErr  error
		skip     bool
		wantSent int
	}{
		{name: "custom sender", sender: customSender, wantSent: 1},
		{name: "custom sender failure", sender: customSender, sendErr: sendErr, wantSent: 1},
		{name: "custom sender skip", sender: customSender, skip: true},
		{name: "http sender", sender: httpSender},
		{name: "http sender skip", sender: httpSender, skip: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sent := 0
			deliver := NewDeliveryHandler(tc.sender(&sent, tc.sendErr))

			delivery := &Delivery{Config: &a2a.PushConfig{URL: server.URL}, Event: &a2a.Task{}, Skip: tc.skip}
			result, err := deliver(t.Context(), delivery)

			if !errors.Is(err, tc.sendErr) {
				t.Fatalf("deliver() error = %v, want %v", err, tc.sendErr)
			}
			if !errors.Is(result.Err, tc.sendErr) {
				t.Fatalf("deliver() result.Err = %v, want %v", result.Err, tc.sendErr)
			}
			if sent != tc.wantSent {
				t.Fatalf("sender.SendPush() calls = %d, want %d", sent, tc.wantSent)
			}
			if (result.Duration > 0) == tc.skip {
				t.Fatalf("deliver() result.Duration = %v, want measured %v", result.Duration, !tc.skip)
			}
		})
	}

	if NewDeliveryHandler(nil) != nil {
		t.Fatal("NewDeliveryHandler(nil) = non-nil, want nil")
	}
}
