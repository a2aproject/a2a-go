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

package pbjson_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2apb/v0/pbjson"
	"github.com/google/go-cmp/cmp"
)

// The tests below pin the wire shape of the A2A v0.3 REST binding.
// That binding is proto-JSON of google.a2a.v1 with the following important
// quirks:
//
//   - SendMessageRequest.request is JSON-named "message"
//   - SendMessageResponse.msg and StreamResponse.msg are JSON-named "message"
//   - TaskStatus.update is JSON-named "message"
//   - Message.content is the container for parts (not "parts")
//   - Part is a oneof of {text, file, data}; file wraps FilePart with
//     {fileWithUri | fileWithBytes, mimeType, name}
//   - Enums are stringified (e.g. Role "ROLE_USER", TaskState "TASK_STATE_WORKING")
//   - PushConfig lookups use resource names: tasks/{taskId}/pushNotificationConfigs/{configId}
//   - CreateTaskPushNotificationConfig body wraps under
//     TaskPushNotificationConfig{ name, pushNotificationConfig }

func TestToProtoSendMessageRequest_TextPart(t *testing.T) {
	t.Parallel()
	msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hello"))
	msg.ID = "m1"
	msg.ContextID = "c1"
	req := &a2a.SendMessageRequest{Message: msg}

	got, err := pbjson.ToProtoSendMessageRequest(req)
	if err != nil {
		t.Fatalf("ToProtoSendMessageRequest() error = %v, want nil", err)
	}

	want := map[string]any{
		"message": map[string]any{
			"messageId": "m1",
			"contextId": "c1",
			"role":      "ROLE_USER",
			"content": []any{
				map[string]any{"text": "hello"},
			},
		},
	}
	assertJSONEqual(t, got, want)
}

func TestToProtoSendMessageRequest_FilePart(t *testing.T) {
	t.Parallel()

	filePart := a2a.NewFileURLPart(a2a.URL("https://example.com/f.png"), "image/png")
	filePart.Filename = "f.png"

	msg := a2a.NewMessage(a2a.MessageRoleUser, filePart)
	msg.ID = "m1"
	req := &a2a.SendMessageRequest{Message: msg}

	got, err := pbjson.ToProtoSendMessageRequest(req)
	if err != nil {
		t.Fatalf("ToProtoSendMessageRequest() error = %v, want nil", err)
	}

	want := map[string]any{
		"message": map[string]any{
			"messageId": "m1",
			"role":      "ROLE_USER",
			"content": []any{
				map[string]any{
					"file": map[string]any{
						"fileWithUri": "https://example.com/f.png",
						"mimeType":    "image/png",
						"name":        "f.png",
					},
				},
			},
		},
	}
	assertJSONEqual(t, got, want)
}

func TestToProtoSendMessageRequest_RawFilePart(t *testing.T) {
	t.Parallel()

	rawPart := a2a.NewRawPart([]byte("abc"))
	rawPart.MediaType = "application/octet-stream"
	rawPart.Filename = "blob.bin"

	msg := a2a.NewMessage(a2a.MessageRoleUser, rawPart)
	msg.ID = "m1"
	req := &a2a.SendMessageRequest{Message: msg}

	got, err := pbjson.ToProtoSendMessageRequest(req)
	if err != nil {
		t.Fatalf("ToProtoSendMessageRequest() error = %v, want nil", err)
	}

	// Raw bytes are double base64-encoded on the wire (outer for proto-JSON
	// transport of bytes fields, inner for the application-level base64
	// string that types.FileWithBytes.bytes carries after v0.3 peers parse
	// the proto). "abc" -> inner base64 "YWJj" -> outer base64 "WVdKag==".
	want := map[string]any{
		"message": map[string]any{
			"messageId": "m1",
			"role":      "ROLE_USER",
			"content": []any{
				map[string]any{
					"file": map[string]any{
						"fileWithBytes": "WVdKag==",
						"mimeType":      "application/octet-stream",
						"name":          "blob.bin",
					},
				},
			},
		},
	}
	assertJSONEqual(t, got, want)
}

func TestToProtoSendMessageRequest_DataPart(t *testing.T) {
	t.Parallel()

	dataPart := a2a.NewDataPart(map[string]any{"key": "value", "n": 42.0})
	msg := a2a.NewMessage(a2a.MessageRoleAgent, dataPart)
	msg.ID = "m1"
	req := &a2a.SendMessageRequest{Message: msg}

	got, err := pbjson.ToProtoSendMessageRequest(req)
	if err != nil {
		t.Fatalf("ToProtoSendMessageRequest() error = %v, want nil", err)
	}

	want := map[string]any{
		"message": map[string]any{
			"messageId": "m1",
			"role":      "ROLE_AGENT",
			"content": []any{
				map[string]any{
					"data": map[string]any{
						"data": map[string]any{"key": "value", "n": 42.0},
					},
				},
			},
		},
	}
	assertJSONEqual(t, got, want)
}

func TestToProtoSendMessageRequest_WithConfig(t *testing.T) {
	t.Parallel()

	msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hi"))
	msg.ID = "m1"

	push := &a2a.PushConfig{
		ID:    "pc1",
		URL:   "http://cb/notify",
		Token: "tk",
	}

	req := &a2a.SendMessageRequest{
		Message: msg,
		Config: &a2a.SendMessageConfig{
			AcceptedOutputModes: []string{"text/plain"},
			ReturnImmediately:   true,
			PushConfig:          push,
		},
	}

	got, err := pbjson.ToProtoSendMessageRequest(req)
	if err != nil {
		t.Fatalf("ToProtoSendMessageRequest() error = %v, want nil", err)
	}

	want := map[string]any{
		"message": map[string]any{
			"messageId": "m1",
			"role":      "ROLE_USER",
			"content":   []any{map[string]any{"text": "hi"}},
		},
		"configuration": map[string]any{
			"acceptedOutputModes": []any{"text/plain"},
			"blocking":            false,
			"pushNotification": map[string]any{
				"id":    "pc1",
				"url":   "http://cb/notify",
				"token": "tk",
			},
		},
	}
	assertJSONEqual(t, got, want)
}

func TestFromProtoSendMessageResponse_Message(t *testing.T) {
	t.Parallel()

	body := []byte(`{"message": {"messageId": "r1", "role": "ROLE_AGENT", "content": [{"text":"hi"}]}}`)
	got, err := pbjson.FromProtoSendMessageResponse(body)
	if err != nil {
		t.Fatalf("FromProtoSendMessageResponse() error = %v, want nil", err)
	}
	msg, ok := got.(*a2a.Message)
	if !ok {
		t.Fatalf("FromProtoSendMessageResponse() = %T, want *a2a.Message", got)
	}
	if msg.ID != "r1" {
		t.Fatalf("Message.ID = %q, want %q", msg.ID, "r1")
	}
	if msg.Role != a2a.MessageRoleAgent {
		t.Fatalf("Message.Role = %q, want %q", msg.Role, a2a.MessageRoleAgent)
	}
	if len(msg.Parts) != 1 || msg.Parts[0].Text() != "hi" {
		t.Fatalf("Message.Parts = %+v, want single text part 'hi'", msg.Parts)
	}
}

func TestFromProtoSendMessageResponse_Task(t *testing.T) {
	t.Parallel()

	body := []byte(`{"task": {"id": "t1", "contextId": "c1", "status": {"state": "TASK_STATE_WORKING"}}}`)
	got, err := pbjson.FromProtoSendMessageResponse(body)
	if err != nil {
		t.Fatalf("FromProtoSendMessageResponse() error = %v, want nil", err)
	}
	task, ok := got.(*a2a.Task)
	if !ok {
		t.Fatalf("FromProtoSendMessageResponse() = %T, want *a2a.Task", got)
	}
	if task.ID != "t1" {
		t.Fatalf("Task.ID = %q, want %q", task.ID, a2a.TaskID("t1"))
	}
	if task.Status.State != a2a.TaskStateWorking {
		t.Fatalf("Task.Status.State = %q, want %q", task.Status.State, a2a.TaskStateWorking)
	}
}

func TestFromProtoStreamEvent_StatusUpdate(t *testing.T) {
	t.Parallel()

	body := []byte(`{"statusUpdate": {"taskId": "t1", "contextId": "c1", "status": {"state":"TASK_STATE_COMPLETED"}, "final": true}}`)
	got, err := pbjson.FromProtoStreamEvent(body)
	if err != nil {
		t.Fatalf("FromProtoStreamEvent() error = %v, want nil", err)
	}
	evt, ok := got.(*a2a.TaskStatusUpdateEvent)
	if !ok {
		t.Fatalf("FromProtoStreamEvent() = %T, want *a2a.TaskStatusUpdateEvent", got)
	}
	if evt.TaskID != "t1" {
		t.Fatalf("TaskStatusUpdateEvent.TaskID = %q, want %q", evt.TaskID, a2a.TaskID("t1"))
	}
	if evt.Status.State != a2a.TaskStateCompleted {
		t.Fatalf("TaskStatusUpdateEvent.Status.State = %q, want %q", evt.Status.State, a2a.TaskStateCompleted)
	}
}

func TestTaskStateCanceledSpelling(t *testing.T) {
	t.Parallel()

	got, err := pbjson.ToProtoTask(&a2a.Task{
		ID:     "t1",
		Status: a2a.TaskStatus{State: a2a.TaskStateCanceled},
	})
	if err != nil {
		t.Fatalf("ToProtoTask() error = %v, want nil", err)
	}
	// The v0.3 proto spells cancellation "TASK_STATE_CANCELLED" (double L);
	// mismatch is silently rejected by the server.
	if !strings.Contains(string(got), `"state":"TASK_STATE_CANCELLED"`) {
		t.Fatalf("Task JSON did not contain British 'CANCELLED': %s", string(got))
	}

	back, err := pbjson.FromProtoTask([]byte(`{"id":"t1","status":{"state":"TASK_STATE_CANCELLED"}}`))
	if err != nil {
		t.Fatalf("FromProtoTask() error = %v, want nil", err)
	}
	if back.Status.State != a2a.TaskStateCanceled {
		t.Fatalf("Task.Status.State = %q, want %q", back.Status.State, a2a.TaskStateCanceled)
	}
}

func TestToProtoTask_WithTimestamp(t *testing.T) {
	t.Parallel()

	ts := time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC)
	task := &a2a.Task{
		ID:        "t1",
		ContextID: "c1",
		Status: a2a.TaskStatus{
			State:     a2a.TaskStateWorking,
			Timestamp: &ts,
		},
	}
	got, err := pbjson.ToProtoTask(task)
	if err != nil {
		t.Fatalf("ToProtoTask() error = %v, want nil", err)
	}
	want := map[string]any{
		"id":        "t1",
		"contextId": "c1",
		"status": map[string]any{
			"state":     "TASK_STATE_WORKING",
			"timestamp": "2026-07-13T08:00:00Z",
		},
	}
	assertJSONEqual(t, got, want)
}

func TestToProtoStreamEvent_Message(t *testing.T) {
	t.Parallel()

	msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("hello"))
	msg.ID = "m1"
	got, err := pbjson.ToProtoStreamEvent(msg)
	if err != nil {
		t.Fatalf("ToProtoStreamEvent(Message) error = %v, want nil", err)
	}
	want := map[string]any{
		"message": map[string]any{
			"messageId": "m1",
			"role":      "ROLE_AGENT",
			"content":   []any{map[string]any{"text": "hello"}},
		},
	}
	assertJSONEqual(t, got, want)
}

func TestToProtoCreatePushConfigRequest(t *testing.T) {
	t.Parallel()

	pc := &a2a.PushConfig{
		TaskID: "t1",
		ID:     "pc1",
		URL:    "http://cb/notify",
		Token:  "tk",
	}
	got, err := pbjson.ToProtoCreatePushConfigRequest(pc)
	if err != nil {
		t.Fatalf("ToProtoCreatePushConfigRequest() error = %v, want nil", err)
	}
	want := map[string]any{
		"parent":   "tasks/t1",
		"configId": "pc1",
		"config": map[string]any{
			"name": "tasks/t1/pushNotificationConfigs/pc1",
			"pushNotificationConfig": map[string]any{
				"id":    "pc1",
				"url":   "http://cb/notify",
				"token": "tk",
			},
		},
	}
	assertJSONEqual(t, got, want)
}

func TestFromProtoPushConfigResponse(t *testing.T) {
	t.Parallel()

	body := []byte(`{
		"name": "tasks/t1/pushNotificationConfigs/pc1",
		"pushNotificationConfig": {"id": "pc1", "url": "http://cb/notify", "token": "tk"}
	}`)
	got, err := pbjson.FromProtoPushConfigResponse(body)
	if err != nil {
		t.Fatalf("FromProtoPushConfigResponse() error = %v, want nil", err)
	}
	if got.TaskID != "t1" {
		t.Fatalf("PushConfig.TaskID = %q, want %q", got.TaskID, a2a.TaskID("t1"))
	}
	if got.ID != "pc1" {
		t.Fatalf("PushConfig.ID = %q, want %q", got.ID, "pc1")
	}
	if got.URL != "http://cb/notify" {
		t.Fatalf("PushConfig.URL = %q, want %q", got.URL, "http://cb/notify")
	}
	if got.Token != "tk" {
		t.Fatalf("PushConfig.Token = %q, want %q", got.Token, "tk")
	}
}

func assertJSONEqual(t *testing.T, gotBytes []byte, want any) {
	t.Helper()
	var got any
	if err := json.Unmarshal(gotBytes, &got); err != nil {
		t.Fatalf("failed to unmarshal marshaled bytes: %v\nbytes: %s", err, string(gotBytes))
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("marshaled JSON wrong result (-want +got) diff = %s\nraw = %s", diff, string(gotBytes))
	}
}
