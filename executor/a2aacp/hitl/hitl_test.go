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

package hitl

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/go-cmp/cmp"
)

var (
	permission = Question{
		ID:    "t1",
		Type:  TypePermission,
		Title: "bash",
		Tool:  "execute",
		Input: map[string]any{"command": "rm x.txt"},
		Options: []Option{
			{ID: OptionOnce, Label: "Allow"},
			{ID: OptionAlways, Label: "Always Allow"},
			{ID: OptionReject, Label: "Reject"},
		},
	}
	form = Question{
		ID:    "elicitation-1",
		Type:  TypeForm,
		Title: "Tell me about you",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":   map[string]any{"type": "string", "description": "Your name"},
				"count":  map[string]any{"type": "integer"},
				"colors": map[string]any{"type": "array", "items": map[string]any{"enum": []any{"red", "blue"}}},
			},
			"required": []any{"name"},
		},
		Options: []Option{{ID: ActionDecline, Label: "Decline"}, {ID: ActionCancel, Label: "Cancel"}},
	}
	taskInfo = a2a.TaskInfo{TaskID: "task-1", ContextID: "ctx-1"}
)

// jsonRoundTrip returns msg as a client decodes it from the wire.
func jsonRoundTrip(t *testing.T, msg *a2a.Message) *a2a.Message {
	t.Helper()
	b, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var out a2a.Message
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	return &out
}

func TestFromStatus(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		status a2a.TaskStatus
		want   Question
		wantOK bool
	}{
		{
			name:   "permission",
			status: a2a.TaskStatus{State: a2a.TaskStateInputRequired, Message: NewStatusMessage(taskInfo, permission, "")},
			want:   permission,
			wantOK: true,
		},
		{
			name:   "form",
			status: a2a.TaskStatus{State: a2a.TaskStateInputRequired, Message: NewStatusMessage(taskInfo, form, "")},
			want:   form,
			wantOK: true,
		},
		{
			name:   "not input required",
			status: a2a.TaskStatus{State: a2a.TaskStateWorking, Message: NewStatusMessage(taskInfo, permission, "")},
		},
		{
			name:   "no question",
			status: a2a.TaskStatus{State: a2a.TaskStateInputRequired, Message: a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("Which one?"))},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.status.Message != nil {
				tc.status.Message = jsonRoundTrip(t, tc.status.Message)
			}

			got, ok := FromStatus(tc.status)

			if ok != tc.wantOK {
				t.Fatalf("FromStatus() ok = %v, want %v", ok, tc.wantOK)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("FromStatus() wrong result (-want +got) diff = %s", diff)
			}
		})
	}
}

func TestQuestion_Text(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		q    Question
		want string
	}{
		{
			name: "permission",
			q:    permission,
			want: "Permission required: bash (rm x.txt).\nReply with one of: once, always, reject.",
		},
		{
			name: "form",
			q:    form,
			want: "Tell me about you\n- colors (array) One of: red, blue.\n- count (integer)\n- name (string, required): Your name\n" +
				"Reply with a JSON object of the fields, or with decline or cancel.",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.q.Text(); got != tc.want {
				t.Fatalf("Question.Text() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseAnswer(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		q       Question
		msg     *a2a.Message
		want    Answer
		wantErr string
	}{
		{
			name: "answer message",
			q:    permission,
			msg:  NewAnswerMessage(taskInfo, "t1", OptionAlways),
			want: Answer{Option: OptionAlways},
		},
		{
			name:    "answer to another question",
			q:       permission,
			msg:     NewAnswerMessage(taskInfo, "t2", OptionAlways),
			wantErr: ErrNoAnswer.Error(),
		},
		{
			name: "label",
			q:    permission,
			msg:  a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("always allow")),
			want: Answer{Option: OptionAlways},
		},
		{
			name: "word",
			q:    permission,
			msg:  a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("Approve!")),
			want: Answer{Option: OptionOnce},
		},
		{
			name:    "unrelated text",
			q:       permission,
			msg:     a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("rename it instead")),
			wantErr: ErrNoAnswer.Error(),
		},
		{
			name: "form fields",
			q:    form,
			msg:  NewAnswerMessage(taskInfo, "elicitation-1", map[string]any{"name": "Anna", "count": "3", "colors": "red, blue"}),
			want: Answer{Option: ActionAccept, Content: map[string]any{"name": "Anna", "count": 3.0, "colors": []any{"red", "blue"}}},
		},
		{
			name:    "invalid form fields",
			q:       form,
			msg:     a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart(`{"name": "Anna", "colors": ["green"]}`)),
			wantErr: `"green" is not a valid choice for colors`,
		},
		{
			name: "form declined",
			q:    form,
			msg:  a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("decline")),
			want: Answer{Option: ActionDecline},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseAnswer(tc.q, jsonRoundTrip(t, tc.msg))

			if tc.wantErr != "" {
				if err == nil || err.Error() != tc.wantErr {
					t.Fatalf("ParseAnswer() error = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseAnswer() error = %v", err)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("ParseAnswer() wrong result (-want +got) diff = %s", diff)
			}
		})
	}
}

func TestParseAnswer_ErrNoAnswer(t *testing.T) {
	t.Parallel()
	_, err := ParseAnswer(permission, a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("later")))
	if !errors.Is(err, ErrNoAnswer) {
		t.Fatalf("ParseAnswer() error = %v, want %v", err, ErrNoAnswer)
	}
}
