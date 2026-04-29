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
	"testing"

	a2alegacy "github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestToV1Payload(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   any
		want    any
		wantErr bool
	}{
		{
			name:  "nil",
			input: nil,
			want:  nil,
		},
		{
			name:  "TaskQueryParams",
			input: &a2alegacy.TaskQueryParams{ID: "t1", HistoryLength: intPtr(5)},
			want:  &a2a.GetTaskRequest{ID: "t1", HistoryLength: intPtr(5)},
		},
		{
			name:  "ListTasksRequest",
			input: &a2alegacy.ListTasksRequest{ContextID: "ctx1", PageSize: 10},
			want: &a2a.ListTasksRequest{
				ContextID:     "ctx1",
				PageSize:      10,
				HistoryLength: intPtr(0),
			},
		},
		{
			name:  "TaskIDParams",
			input: &a2alegacy.TaskIDParams{ID: "t2"},
			want:  &a2a.CancelTaskRequest{ID: "t2"},
		},
		{
			name:  "MessageSendParams",
			input: &a2alegacy.MessageSendParams{Message: &a2alegacy.Message{ID: "m1", Role: a2alegacy.MessageRoleUser}},
			want:  &a2a.SendMessageRequest{Message: &a2a.Message{ID: "m1", Role: a2a.MessageRoleUser}},
		},
		{
			name:  "GetTaskPushConfigParams",
			input: &a2alegacy.GetTaskPushConfigParams{TaskID: "t1", ConfigID: "c1"},
			want:  &a2a.GetTaskPushConfigRequest{TaskID: "t1", ID: "c1"},
		},
		{
			name:  "ListTaskPushConfigParams",
			input: &a2alegacy.ListTaskPushConfigParams{TaskID: "t1"},
			want:  &a2a.ListTaskPushConfigRequest{TaskID: "t1"},
		},
		{
			name:  "DeleteTaskPushConfigParams",
			input: &a2alegacy.DeleteTaskPushConfigParams{TaskID: "t1", ConfigID: "c1"},
			want:  &a2a.DeleteTaskPushConfigRequest{TaskID: "t1", ID: "c1"},
		},
		{
			name: "TaskPushConfig",
			input: &a2alegacy.TaskPushConfig{
				TaskID: "t1",
				Config: a2alegacy.PushConfig{ID: "p1", URL: "http://example.com"},
			},
			want: &a2a.TaskPushConfig{
				TaskID: "t1",
				ID:     "p1",
				URL:    "http://example.com",
			},
		},
		{
			name: "TaskPushConfig slice",
			input: []*a2alegacy.TaskPushConfig{
				{TaskID: "t1", Config: a2alegacy.PushConfig{ID: "p1"}},
				{TaskID: "t2", Config: a2alegacy.PushConfig{ID: "p2"}},
			},
			want: []*a2a.TaskPushConfig{
				{TaskID: "t1", ID: "p1"},
				{TaskID: "t2", ID: "p2"},
			},
		},
		{
			name: "ListTasksResponse",
			input: &a2alegacy.ListTasksResponse{
				Tasks:     []*a2alegacy.Task{{ID: "t1", Status: a2alegacy.TaskStatus{State: a2alegacy.TaskStateCompleted}}},
				TotalSize: 1,
			},
			want: &a2a.ListTasksResponse{
				Tasks:     []*a2a.Task{{ID: "t1", Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}}},
				TotalSize: 1,
			},
		},
		{
			name:  "AgentCard",
			input: &a2alegacy.AgentCard{Name: "agent1", Description: "desc"},
			want:  &a2a.AgentCard{Name: "agent1", Description: "desc"},
		},
		{
			name:  "Event - Task",
			input: &a2alegacy.Task{ID: "t1", Status: a2alegacy.TaskStatus{State: a2alegacy.TaskStateWorking}},
			want:  &a2a.Task{ID: "t1", Status: a2a.TaskStatus{State: a2a.TaskStateWorking}},
		},
		{
			name:    "unsupported type errors",
			input:   "unexpected string",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := toV1Payload(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("toV1Payload() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("toV1Payload() error = %v, want nil", err)
			}
			if diff := cmp.Diff(tc.want, got, cmpopts.EquateEmpty()); diff != "" {
				t.Fatalf("toV1Payload() wrong result (+got,-want) diff = %s", diff)
			}
		})
	}
}

func TestFromV1Payload(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   any
		want    any
		wantErr bool
	}{
		{
			name:  "nil",
			input: nil,
			want:  nil,
		},
		{
			name:  "GetTaskRequest",
			input: &a2a.GetTaskRequest{ID: "t1", HistoryLength: intPtr(5)},
			want:  &a2alegacy.TaskQueryParams{ID: "t1", HistoryLength: intPtr(5)},
		},
		{
			name:  "ListTasksRequest",
			input: &a2a.ListTasksRequest{ContextID: "ctx1", PageSize: 10, HistoryLength: intPtr(3)},
			want:  &a2alegacy.ListTasksRequest{ContextID: "ctx1", PageSize: 10, HistoryLength: 3},
		},
		{
			name:  "CancelTaskRequest",
			input: &a2a.CancelTaskRequest{ID: "t2"},
			want:  &a2alegacy.TaskIDParams{ID: "t2"},
		},
		{
			name:  "SubscribeToTaskRequest",
			input: &a2a.SubscribeToTaskRequest{ID: "t3"},
			want:  &a2alegacy.TaskIDParams{ID: "t3"},
		},
		{
			name:  "SendMessageRequest",
			input: &a2a.SendMessageRequest{Message: &a2a.Message{ID: "m1", Role: a2a.MessageRoleUser}},
			want:  &a2alegacy.MessageSendParams{Message: &a2alegacy.Message{ID: "m1", Role: a2alegacy.MessageRoleUser}},
		},
		{
			name:  "GetExtendedAgentCardRequest returns nil",
			input: &a2a.GetExtendedAgentCardRequest{},
			want:  nil,
		},
		{
			name:  "GetTaskPushConfigRequest",
			input: &a2a.GetTaskPushConfigRequest{TaskID: "t1", ID: "c1"},
			want:  &a2alegacy.GetTaskPushConfigParams{TaskID: "t1", ConfigID: "c1"},
		},
		{
			name:  "ListTaskPushConfigRequest",
			input: &a2a.ListTaskPushConfigRequest{TaskID: "t1"},
			want:  &a2alegacy.ListTaskPushConfigParams{TaskID: "t1"},
		},
		{
			name:  "DeleteTaskPushConfigRequest",
			input: &a2a.DeleteTaskPushConfigRequest{TaskID: "t1", ID: "c1"},
			want:  &a2alegacy.DeleteTaskPushConfigParams{TaskID: "t1", ConfigID: "c1"},
		},
		{
			name:  "CreateTaskPushConfigRequest",
			input: &a2a.PushConfig{TaskID: "t1", ID: "p1", URL: "http://example.com"},
			want: &a2alegacy.TaskPushConfig{
				TaskID: "t1",
				Config: a2alegacy.PushConfig{ID: "p1", URL: "http://example.com"},
			},
		},
		{
			name: "TaskPushConfig",
			input: &a2a.TaskPushConfig{
				TaskID: "t1",
				ID:     "p1",
				URL:    "http://example.com",
			},
			want: &a2alegacy.TaskPushConfig{
				TaskID: "t1",
				Config: a2alegacy.PushConfig{ID: "p1", URL: "http://example.com"},
			},
		},
		{
			name: "TaskPushConfig slice",
			input: []*a2a.TaskPushConfig{
				{TaskID: "t1", ID: "p1"},
				{TaskID: "t2", ID: "p2"},
			},
			want: []*a2alegacy.TaskPushConfig{
				{TaskID: "t1", Config: a2alegacy.PushConfig{ID: "p1"}},
				{TaskID: "t2", Config: a2alegacy.PushConfig{ID: "p2"}},
			},
		},
		{
			name: "ListTasksResponse",
			input: &a2a.ListTasksResponse{
				Tasks:     []*a2a.Task{{ID: "t1", Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}}},
				TotalSize: 1,
			},
			want: &a2alegacy.ListTasksResponse{
				Tasks:     []*a2alegacy.Task{{ID: "t1", Status: a2alegacy.TaskStatus{State: a2alegacy.TaskStateCompleted}}},
				TotalSize: 1,
			},
		},
		{
			name:  "AgentCard",
			input: &a2a.AgentCard{Name: "agent1", Description: "desc"},
			want:  &a2alegacy.AgentCard{Name: "agent1", Description: "desc"},
		},
		{
			name:  "struct{} passthrough",
			input: struct{}{},
			want:  struct{}{},
		},
		{
			name:  "Event - Task",
			input: &a2a.Task{ID: "t1", Status: a2a.TaskStatus{State: a2a.TaskStateWorking}},
			want:  &a2alegacy.Task{ID: "t1", Status: a2alegacy.TaskStatus{State: a2alegacy.TaskStateWorking}},
		},
		{
			name:    "unsupported type errors",
			input:   "unexpected string",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := fromV1Payload(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("fromV1Payload() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("fromV1Payload() error = %v, want nil", err)
			}
			if diff := cmp.Diff(tc.want, got, cmpopts.EquateEmpty()); diff != "" {
				t.Fatalf("fromV1Payload() wrong result (+got,-want) diff = %s", diff)
			}
		})
	}
}

func intPtr(v int) *int {
	return &v
}
