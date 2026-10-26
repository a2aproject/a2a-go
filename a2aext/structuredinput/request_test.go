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

package structuredinput

import (
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/jsonschema-go/jsonschema"
)

func TestPendingRequest(t *testing.T) {
	t.Parallel()

	taskInfo := a2a.TaskInfo{TaskID: "task-1", ContextID: "ctx-1"}
	schemaPart := mustPart(t)(NewSchemaPart("req-1", newTestSchema()))

	tests := []struct {
		name    string
		task    *a2a.Task
		want    *Request
		wantErr bool
	}{
		{
			name: "nil task",
		},
		{
			name: "task not in input-required state",
			task: &a2a.Task{Status: a2a.TaskStatus{
				State:   a2a.TaskStateWorking,
				Message: a2a.NewMessageForTask(a2a.MessageRoleAgent, taskInfo, schemaPart),
			}},
		},
		{
			name: "input-required without schema",
			task: &a2a.Task{Status: a2a.TaskStatus{
				State:   a2a.TaskStateInputRequired,
				Message: a2a.NewMessageForTask(a2a.MessageRoleAgent, taskInfo, a2a.NewTextPart("what?")),
			}},
		},
		{
			name: "input-required with schema",
			task: &a2a.Task{Status: a2a.TaskStatus{
				State:   a2a.TaskStateInputRequired,
				Message: a2a.NewMessageForTask(a2a.MessageRoleAgent, taskInfo, a2a.NewTextPart("approve"), schemaPart),
			}},
			want: &Request{ID: "req-1", Schema: newTestSchema()},
		},
		{
			name: "multiple schema parts",
			task: &a2a.Task{Status: a2a.TaskStatus{
				State:   a2a.TaskStateInputRequired,
				Message: a2a.NewMessageForTask(a2a.MessageRoleAgent, taskInfo, schemaPart, schemaPart),
			}},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			task := tc.task
			if task != nil {
				task = jsonRoundTrip(t, task)
			}
			got, err := PendingRequest(task)
			if (err != nil) != tc.wantErr {
				t.Fatalf("PendingInputRequest() error = %v, want error %v", err, tc.wantErr)
			}
			if diff := cmp.Diff(tc.want, got, cmpopts.IgnoreUnexported(jsonschema.Schema{})); diff != "" {
				t.Fatalf("PendingInputRequest() wrong result (-want +got) diff = %s", diff)
			}
		})
	}
}

func TestNewInputRequiredEvent(t *testing.T) {
	t.Parallel()

	taskInfo := a2a.TaskInfo{TaskID: "task-1", ContextID: "ctx-1"}
	req := &Request{ID: "req-1", Schema: newTestSchema()}

	event, err := NewRequestEvent(taskInfo, req, a2a.NewTextPart("approve"))
	if err != nil {
		t.Fatalf("NewInputRequiredEvent() error = %v", err)
	}

	if event.Status.State != a2a.TaskStateInputRequired {
		t.Fatalf("NewInputRequiredEvent() state = %v, want %v", event.Status.State, a2a.TaskStateInputRequired)
	}
	if diff := cmp.Diff(taskInfo, event.TaskInfo()); diff != "" {
		t.Fatalf("NewInputRequiredEvent() wrong task info (-want +got) diff = %s", diff)
	}
	if diff := cmp.Diff([]string{URI}, event.Status.Message.Extensions); diff != "" {
		t.Fatalf("NewInputRequiredEvent() wrong extensions (-want +got) diff = %s", diff)
	}
	if got := event.Status.Message.Parts[0].Text(); got != "approve" {
		t.Fatalf("NewInputRequiredEvent() first part text = %q, want %q", got, "approve")
	}
	got, err := ParseRequest(event.Status.Message)
	if err != nil {
		t.Fatalf("ParseInputRequest() error = %v", err)
	}
	if diff := cmp.Diff(req, got, cmpopts.IgnoreUnexported(jsonschema.Schema{})); diff != "" {
		t.Fatalf("ParseInputRequest() wrong result (-want +got) diff = %s", diff)
	}
}

func TestNewInputMessage(t *testing.T) {
	t.Parallel()

	taskInfo := a2a.TaskInfo{TaskID: "task-1", ContextID: "ctx-1"}
	type approval struct {
		Status     string `json:"status"`
		ApproverID string `json:"approver_id"`
	}

	msg, err := NewResponse(taskInfo, &Request{ID: "req-1"}, approval{Status: "ok", ApproverID: "alice"}, a2a.NewTextPart("approved"))
	if err != nil {
		t.Fatalf("NewInputMessage() error = %v", err)
	}

	want := &a2a.Message{
		ID:         msg.ID,
		TaskID:     taskInfo.TaskID,
		ContextID:  taskInfo.ContextID,
		Role:       a2a.MessageRoleUser,
		Extensions: []string{URI},
		Parts: a2a.ContentParts{
			a2a.NewTextPart("approved"),
			{
				Content:  a2a.Data{Value: map[string]any{"status": "ok", "approver_id": "alice"}},
				Metadata: map[string]any{URI: map[string]any{"type": "structured_input", "id": "req-1"}},
			},
		},
	}
	if diff := cmp.Diff(want, msg); diff != "" {
		t.Fatalf("NewInputMessage() wrong result (-want +got) diff = %s", diff)
	}
}

func TestParseValidationOutput(t *testing.T) {
	t.Parallel()

	output := &ValidationOutput{Errors: []ValidationErrorItem{{KeywordLocation: "/required", Error: "missing approver_id"}}}
	tests := []struct {
		name string
		msg  *a2a.Message
		want *ValidationOutput
	}{
		{
			name: "no validation errors",
			msg:  a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("hi")),
		},
		{
			name: "validation errors",
			msg:  a2a.NewMessage(a2a.MessageRoleAgent, mustPart(t)(NewValidationErrorPart(output))),
			want: output,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseValidationOutput(jsonRoundTrip(t, tc.msg))
			if err != nil {
				t.Fatalf("ParseValidationOutput() error = %v", err)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("ParseValidationOutput() wrong result (-want +got) diff = %s", diff)
			}
		})
	}
}
