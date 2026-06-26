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

package eventqueue

import (
	"encoding/json"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"
	"github.com/google/go-cmp/cmp"
)

func TestMessageNilEventError(t *testing.T) {
	t.Parallel()

	t.Run("MarshalJSON", func(t *testing.T) {
		t.Parallel()
		msg := Message{Event: nil}
		_, err := msg.MarshalJSON()
		if err == nil {
			t.Fatal("MarshalJSON with nil Event should return error")
		}
		if err != ErrNilEvent {
			t.Fatalf("MarshalJSON with nil Event returned %v, want ErrNilEvent", err)
		}
	})

	t.Run("UnmarshalJSON", func(t *testing.T) {
		t.Parallel()
		// JSON missing "event" field entirely
		data := []byte(`{"taskVersion":1,"protocol":"1.0"}`)
		var msg Message
		err := msg.UnmarshalJSON(data)
		if err == nil {
			t.Fatal("UnmarshalJSON without event field should return error")
		}
		if err != ErrNilEvent {
			t.Fatalf("UnmarshalJSON without event field returned %v, want ErrNilEvent", err)
		}
	})
}

func TestMessageJSONRoundtrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		msg  Message
	}{
		{
			name: "TaskStatusUpdateEvent",
			msg: Message{
				Event: &a2a.TaskStatusUpdateEvent{
					TaskID:    "task-1",
					ContextID: "ctx-1",
					Status: a2a.TaskStatus{
						State: a2a.TaskStateWorking,
					},
				},
				TaskVersion: 42,
				Protocol:    "1.0",
			},
		},
		{
			name: "TaskArtifactUpdateEvent",
			msg: Message{
				Event: &a2a.TaskArtifactUpdateEvent{
					TaskID:    "task-2",
					ContextID: "ctx-2",
					Artifact: &a2a.Artifact{
						ID:    "artifact-1",
						Name:  "output",
						Parts: a2a.ContentParts{a2a.NewTextPart("hello")},
					},
				},
				TaskVersion: taskstore.TaskVersionMissing,
				Protocol:    "1.0",
			},
		},
		{
			name: "MessageEvent",
			msg: Message{
				Event:       a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("response")),
				TaskVersion: 7,
				Protocol:    "1.0",
			},
		},
		{
			name: "TaskEvent",
			msg: Message{
				Event: &a2a.Task{
					ID:     "task-3",
					Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
				},
				TaskVersion: 0,
				Protocol:    a2a.Version,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			data, err := json.Marshal(tt.msg)
			if err != nil {
				t.Fatalf("Marshal returned error: %v", err)
			}

			var got Message
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal returned error: %v", err)
			}

			if diff := cmp.Diff(tt.msg, got); diff != "" {
				t.Fatalf("Message JSON roundtrip wrong result (-want +got) diff = %s", diff)
			}
		})
	}
}
