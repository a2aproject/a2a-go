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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/log"
	"github.com/google/go-cmp/cmp"
)

func TestHTTPTransports_UnknownStreamResponses(t *testing.T) {
	t.Parallel()
	task := &a2a.Task{ID: "task-123", ContextID: "ctx-123", Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}}
	working := a2a.NewStatusUpdateEvent(task, a2a.TaskStateWorking, nil)
	working.Status.Timestamp = nil
	completed := a2a.NewStatusUpdateEvent(task, a2a.TaskStateCompleted, nil)
	encode := func(event a2a.Event) string {
		t.Helper()
		data, err := json.Marshal(a2a.StreamResponse{Event: event})
		if err != nil {
			t.Fatalf("json.Marshal() error = %v, want nil", err)
		}
		return string(data)
	}
	taskJSON, workingJSON, completedJSON := encode(task), encode(working), encode(completed)
	unknown := `{"futureEvent":{"taskId":"task-123","generation":42}}`
	serverError := `{"error":{"code":400,"status":"INVALID_ARGUMENT","message":"bad request",` +
		`"details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo",` +
		`"reason":"INVALID_REQUEST","domain":"a2a-protocol.org"}]}}`
	cases := []struct {
		name               string
		payloads           []string
		want               []a2a.Event
		wantErr            error
		wantErrText        string
		wantJSONRPCErrText string
		truncated          bool
	}{
		{
			name:     "known events",
			payloads: []string{taskJSON, workingJSON, workingJSON, completedJSON},
			want:     []a2a.Event{task, working, working, completed},
		},
		{
			name: "known event with unknown fields",
			payloads: []string{
				taskJSON,
				`{"statusUpdate":{"taskId":"task-123","contextId":"ctx-123",` +
					`"status":{"state":"TASK_STATE_WORKING"},"futureField":42},"futureEvent":{}}`,
				completedJSON,
			},
			want: []a2a.Event{task, working, completed},
		},
		{
			name:     "unknown between updates",
			payloads: []string{taskJSON, workingJSON, unknown, workingJSON, completedJSON},
			want:     []a2a.Event{task, working, working, completed},
		},
		{
			name:     "unknown first consecutive and last",
			payloads: []string{unknown, `{"futureScalar":false}`, taskJSON, unknown, unknown, completedJSON, unknown},
			want:     []a2a.Event{task, completed},
		},
		{
			name:     "unknown only",
			payloads: []string{unknown, unknown},
		},
		{
			name:               "malformed known after unknown",
			payloads:           []string{taskJSON, unknown, `{"statusUpdate":{"status":42}}`, completedJSON},
			want:               []a2a.Event{task},
			wantErrText:        "failed to unmarshal stream response",
			wantJSONRPCErrText: "failed to unmarshal event",
		},
		{
			name:               "invalid JSON after unknown",
			payloads:           []string{taskJSON, unknown, `{"futureEvent":`, completedJSON},
			want:               []a2a.Event{task},
			wantErrText:        "failed to unmarshal stream response",
			wantJSONRPCErrText: "failed to parse SSE data",
		},
		{
			name:        "empty wrapper after unknown",
			payloads:    []string{taskJSON, unknown, `{}`, completedJSON},
			want:        []a2a.Event{task},
			wantErrText: "stream response has no payload",
		},
		{
			name:        "null known after unknown",
			payloads:    []string{taskJSON, unknown, `{"task":null}`, completedJSON},
			want:        []a2a.Event{task},
			wantErrText: "stream response has no payload",
		},
		{
			name:               "multiple known after unknown",
			payloads:           []string{taskJSON, unknown, `{"task":{},"statusUpdate":{}}`, completedJSON},
			want:               []a2a.Event{task},
			wantErrText:        "expected exactly one stream response type",
			wantJSONRPCErrText: "expected exactly one event type",
		},
		{
			name:     "server error after unknown",
			payloads: []string{taskJSON, unknown, serverError, completedJSON},
			want:     []a2a.Event{task},
			wantErr:  a2a.ErrInvalidRequest,
		},
		{
			name:      "transport error after unknown",
			payloads:  []string{taskJSON, unknown},
			want:      []a2a.Event{task},
			wantErr:   io.ErrUnexpectedEOF,
			truncated: true,
		},
		{
			name:     "known null with unknown",
			payloads: []string{taskJSON, `{"task":null,"futureEvent":{}}`, completedJSON},
			want:     []a2a.Event{task, completed},
		},
		{
			name:        "null only after unknown",
			payloads:    []string{taskJSON, unknown, `{"task":null,"futureEvent":null}`, completedJSON},
			want:        []a2a.Event{task},
			wantErrText: "stream response has no payload",
		},
	}
	for _, protocol := range []string{"REST", "JSONRPC"} {
		for _, subscribe := range []bool{false, true} {
			for _, tc := range cases {
				t.Run(fmt.Sprintf("%s/subscribe=%t/%s", protocol, subscribe, tc.name), func(t *testing.T) {
					t.Parallel()
					var body strings.Builder
					for _, payload := range tc.payloads {
						if protocol == "JSONRPC" {
							if payload == serverError {
								payload = `{"jsonrpc":"2.0","id":"test","error":{"code":-32600,"message":"bad request"}}`
							} else {
								payload = `{"jsonrpc":"2.0","id":"test","result":` + payload + `}`
							}
						}
						fmt.Fprintf(&body, "data: %s\n\n", payload)
					}
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						w.Header().Set("Content-Type", "text/event-stream")
						if tc.truncated {
							w.Header().Set("Content-Length", fmt.Sprint(body.Len()+1))
						}
						if _, err := io.WriteString(w, body.String()); err != nil {
							t.Errorf("io.WriteString() error = %v, want nil", err)
						}
					}))
					defer server.Close()
					transport := newRESTTransport(t, server)
					if protocol == "JSONRPC" {
						transport = NewJSONRPCTransport(server.URL, server.Client())
					}
					stream := transport.SendStreamingMessage(t.Context(), nil, &a2a.SendMessageRequest{Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("test"))})
					if subscribe {
						stream = transport.SubscribeToTask(t.Context(), nil, &a2a.SubscribeToTaskRequest{ID: task.ID})
					}
					var got []a2a.Event
					var gotErr error
					for event, err := range stream {
						if err != nil {
							if event != nil || gotErr != nil {
								t.Fatalf("stream = (%v, %v), want a single error with no event", event, err)
							}
							gotErr = err
							continue
						}
						got = append(got, event)
					}
					wantErrText := tc.wantErrText
					if protocol == "JSONRPC" && tc.wantJSONRPCErrText != "" {
						wantErrText = tc.wantJSONRPCErrText
					}
					if wantErrText != "" {
						if gotErr == nil || !strings.Contains(gotErr.Error(), wantErrText) {
							t.Fatalf("stream error = %v, want %q", gotErr, wantErrText)
						}
					} else if !errors.Is(gotErr, tc.wantErr) {
						t.Fatalf("stream error = %v, want %v", gotErr, tc.wantErr)
					}
					if diff := cmp.Diff(tc.want, got); diff != "" {
						t.Fatalf("stream wrong result (-want +got) diff = %s", diff)
					}
				})
			}
		}
	}
}

func TestHTTPTransports_UnknownStreamResponseDiagnostics(t *testing.T) {
	t.Parallel()
	for _, protocol := range []string{"REST", "JSONRPC"} {
		t.Run(protocol, func(t *testing.T) {
			t.Parallel()
			payload := `{"futureEvent":{"secret":"private payload"},"anotherEvent":42,"task":null}`
			if protocol == "JSONRPC" {
				payload = `{"jsonrpc":"2.0","id":"test","result":` + payload + `}`
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
					t.Errorf("fmt.Fprintf() error = %v, want nil", err)
				}
			}))
			defer server.Close()
			transport := newRESTTransport(t, server)
			if protocol == "JSONRPC" {
				transport = NewJSONRPCTransport(server.URL, server.Client())
			}
			var output strings.Builder
			ctx := log.AttachLogger(t.Context(), slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug})))
			for event, err := range transport.SubscribeToTask(ctx, nil, &a2a.SubscribeToTaskRequest{ID: "task-123"}) {
				t.Fatalf("SubscribeToTask() = (%v, %v), want no events or errors", event, err)
			}
			var record struct {
				Fields []string `json:"fields"`
			}
			if err := json.Unmarshal([]byte(output.String()), &record); err != nil {
				t.Fatalf("json.Unmarshal() error = %v, want nil", err)
			}
			if diff := cmp.Diff([]string{"anotherEvent", "futureEvent"}, record.Fields); diff != "" {
				t.Fatalf("log fields wrong result (-want +got) diff = %s", diff)
			}
			if strings.Contains(output.String(), "secret") || strings.Contains(output.String(), "private payload") {
				t.Fatalf("log contains payload: %s", output.String())
			}
		})
	}
}
