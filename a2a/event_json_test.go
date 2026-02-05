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

package a2a

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestEventMarshalJSON tests that Event types marshal with the correct 'kind' field.
func TestEventMarshalJSON(t *testing.T) {
	now := time.Now()

	testCases := []struct {
		name           string
		event          Event
		wantKind       string
		wantSubstrings []string
	}{
		{
			name: "Message",
			event: &Message{
				ID:   "msg-123",
				Role: MessageRoleUser,
				Parts: ContentParts{
					TextPart{Text: "hello"},
				},
			},
			wantKind:       "message",
			wantSubstrings: []string{`"message":{"messageId": "msg-123"`, `"messageId":"msg-123"`},
		},
		{
			name: "Task",
			event: &Task{
				ID:        "task-123",
				ContextID: "ctx-123",
				Status: TaskStatus{
					State:     TaskStateSubmitted,
					Timestamp: &now,
				},
			},
			wantKind:       "task",
			wantSubstrings: []string{`"kind":"task"`, `"id":"task-123"`},
		},
		{
			name: "TaskStatusUpdateEvent",
			event: &TaskStatusUpdateEvent{
				TaskID:    "task-123",
				ContextID: "ctx-123",
				Final:     false,
				Status: TaskStatus{
					State:     TaskStateWorking,
					Timestamp: &now,
				},
			},
			wantKind:       "status-update",
			wantSubstrings: []string{`"status-update":{" "taskId":"task-123"}`},
		},
		{
			name: "TaskArtifactUpdateEvent",
			event: &TaskArtifactUpdateEvent{
				TaskID:    "task-123",
				ContextID: "ctx-123",
				Artifact: &Artifact{
					ID:    "art-123",
					Parts: ContentParts{TextPart{Text: "result"}},
				},
			},
			wantKind:       "artifact-update",
			wantSubstrings: []string{`"kind":"artifact-update"`, `"taskId":"task-123"`},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			jsonBytes, err := json.Marshal(tc.event)
			if err != nil {
				t.Fatalf("Marshal() failed: %v", err)
			}

			jsonStr := string(jsonBytes)

			// Verify kind field is present
			var kindCheck struct {
				Kind string `json:"kind"`
			}
			if err := json.Unmarshal(jsonBytes, &kindCheck); err != nil {
				t.Fatalf("Failed to unmarshal kind field: %v", err)
			}

			if kindCheck.Kind != tc.wantKind {
				t.Errorf("got kind %q, want %q", kindCheck.Kind, tc.wantKind)
			}

			// Check for required substrings
			for _, substr := range tc.wantSubstrings {
				if !strings.Contains(jsonStr, substr) {
					t.Errorf("JSON missing %q: %s", substr, jsonStr)
				}
			}
		})
	}
}

// TestUnmarshalEventJSON tests that UnmarshalEventJSON correctly discriminates based on 'kind'.
func TestUnmarshalEventJSON(t *testing.T) {
	testCases := []struct {
		name      string
		json      string
		wantType  string
		checkFunc func(t *testing.T, event Event)
	}{
		{
			name:     "Message",
			json:     `{"kind":"message","messageId":"msg-123","role":"user","parts":[{"kind":"text","text":"hello"}]}`,
			wantType: "*a2a.Message",
			checkFunc: func(t *testing.T, event Event) {
				msg, ok := event.(*Message)
				if !ok {
					t.Fatalf("Expected *Message, got %T", event)
				}
				if msg.ID != "msg-123" {
					t.Errorf("got ID %s, want msg-123", msg.ID)
				}
				if msg.Role != MessageRoleUser {
					t.Errorf("got role %s, want user", msg.Role)
				}
			},
		},
		{
			name:     "Task",
			json:     `{"kind":"task","id":"task-123","contextId":"ctx-123","status":{"state":"submitted"}}`,
			wantType: "*a2a.Task",
			checkFunc: func(t *testing.T, event Event) {
				task, ok := event.(*Task)
				if !ok {
					t.Fatalf("Expected *Task, got %T", event)
				}
				if task.ID != "task-123" {
					t.Errorf("got ID %s, want task-123", task.ID)
				}
				if task.Status.State != TaskStateSubmitted {
					t.Errorf("got state %s, want submitted", task.Status.State)
				}
			},
		},
		{
			name:     "TaskStatusUpdateEvent",
			json:     `{"kind":"status-update","taskId":"task-123","contextId":"ctx-123","final":false,"status":{"state":"working"}}`,
			wantType: "*a2a.TaskStatusUpdateEvent",
			checkFunc: func(t *testing.T, event Event) {
				statusUpdate, ok := event.(*TaskStatusUpdateEvent)
				if !ok {
					t.Fatalf("Expected *TaskStatusUpdateEvent, got %T", event)
				}
				if statusUpdate.TaskID != "task-123" {
					t.Errorf("got taskId %s, want task-123", statusUpdate.TaskID)
				}
				if statusUpdate.Status.State != TaskStateWorking {
					t.Errorf("got state %s, want working", statusUpdate.Status.State)
				}
			},
		},
		{
			name:     "TaskArtifactUpdateEvent",
			json:     `{"kind":"artifact-update","taskId":"task-123","contextId":"ctx-123","artifact":{"artifactId":"art-123","parts":[{"kind":"text","text":"result"}]}}`,
			wantType: "*a2a.TaskArtifactUpdateEvent",
			checkFunc: func(t *testing.T, event Event) {
				artifactUpdate, ok := event.(*TaskArtifactUpdateEvent)
				if !ok {
					t.Fatalf("Expected *TaskArtifactUpdateEvent, got %T", event)
				}
				if artifactUpdate.TaskID != "task-123" {
					t.Errorf("got taskId %s, want task-123", artifactUpdate.TaskID)
				}
				if artifactUpdate.Artifact.ID != "art-123" {
					t.Errorf("got artifact ID %s, want art-123", artifactUpdate.Artifact.ID)
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			event, err := UnmarshalEventJSON([]byte(tc.json))
			if err != nil {
				t.Fatalf("UnmarshalEventJSON() failed: %v", err)
			}

			if tc.checkFunc != nil {
				tc.checkFunc(t, event)
			}
		})
	}
}

// TestUnmarshalEventJSON_Errors tests error cases.
func TestUnmarshalEventJSON_Errors(t *testing.T) {
	testCases := []struct {
		name    string
		json    string
		wantErr string
	}{
		{
			name:    "invalid JSON",
			json:    `{invalid}`,
			wantErr: "failed to unmarshal event",
		},
		{
			name:    "missing kind field",
			json:    `{"id":"task-123"}`,
			wantErr: "unknown event kind",
		},
		{
			name:    "unknown kind",
			json:    `{"kind":"unknown","id":"123"}`,
			wantErr: "unknown event kind: unknown",
		},
		{
			name:    "malformed task",
			json:    `{"kind":"task","id":123}`,
			wantErr: "failed to unmarshal Task event",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := UnmarshalEventJSON([]byte(tc.json))
			if err == nil {
				t.Fatal("Expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("got error %v, want error containing %q", err, tc.wantErr)
			}
		})
	}
}

// TestEventMarshalUnmarshalRoundtrip tests that events can be marshaled and unmarshaled correctly.
func TestEventMarshalUnmarshalRoundtrip(t *testing.T) {
	now := time.Now()

	testCases := []struct {
		name  string
		event Event
	}{
		{
			name: "Message",
			event: &Message{
				ID:   "msg-123",
				Role: MessageRoleUser,
				Parts: ContentParts{
					TextPart{Text: "hello"},
				},
			},
		},
		{
			name: "Task",
			event: &Task{
				ID:        "task-123",
				ContextID: "ctx-123",
				Status: TaskStatus{
					State:     TaskStateSubmitted,
					Timestamp: &now,
				},
			},
		},
		{
			name: "TaskStatusUpdateEvent",
			event: &TaskStatusUpdateEvent{
				TaskID:    "task-123",
				ContextID: "ctx-123",
				Final:     true,
				Status: TaskStatus{
					State:     TaskStateCompleted,
					Timestamp: &now,
				},
			},
		},
		{
			name: "TaskArtifactUpdateEvent",
			event: &TaskArtifactUpdateEvent{
				TaskID:    "task-123",
				ContextID: "ctx-123",
				Artifact: &Artifact{
					ID:    "art-123",
					Parts: ContentParts{TextPart{Text: "result"}},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			original := tc.event
			// Marshal
			jsonBytes, err := json.Marshal(original)
			if err != nil {
				t.Fatalf("Marshal() failed: %v", err)
			}

			// Unmarshal
			decoded, err := UnmarshalEventJSON(jsonBytes)
			if err != nil {
				t.Fatalf("UnmarshalEventJSON() failed: %v", err)
			}

			// Marshal again
			jsonBytes2, err := json.Marshal(decoded)
			if err != nil {
				t.Fatalf("Second Marshal() failed: %v", err)
			}

			// Compare JSON (should be identical)
			if string(jsonBytes) != string(jsonBytes2) {
				t.Errorf("Roundtrip failed:\noriginal: %s\ndecoded:  %s", string(jsonBytes), string(jsonBytes2))
			}
		})
	}
}
