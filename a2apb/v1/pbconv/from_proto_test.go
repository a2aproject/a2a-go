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

package pbconv

import (
	"reflect"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2apb/v1"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestFromProto_fromProtoPart(t *testing.T) {
	pData, _ := structpb.NewValue(map[string]any{"key": "value"})
	tests := []struct {
		name    string
		p       *a2apb.Part
		want    a2a.Part
		wantErr bool
	}{
		{
			name: "text",
			p:    &a2apb.Part{Content: &a2apb.Part_Text{Text: "hello"}},
			want: *a2a.NewTextPart("hello"),
		},
		{
			name: "data",
			p:    &a2apb.Part{Content: &a2apb.Part_Data{Data: pData}},
			want: *a2a.NewDataPart(map[string]any{"key": "value"}),
		},
		{
			name: "file with bytes",
			p: &a2apb.Part{Content: &a2apb.Part_Raw{Raw: []byte("content")}, MediaType: "text/plain", Filename: "Test File"},
			want: a2a.Part{
				Content: a2a.Raw([]byte("content")),
				Filename: "Test File",
				MediaType: "text/plain",
			},
		},
		{
			name: "file with uri",
			p: &a2apb.Part{Content: &a2apb.Part_Url{Url: "http://example.com/file"},
				Filename: "example",
				MediaType: "text/plain",
			},
			want: a2a.Part{
				Content: a2a.URL("http://example.com/file"),
				Filename: "example",
				MediaType: "text/plain",
			},
		},
		{
			name:    "unsupported",
			p:       &a2apb.Part{Content: nil},
			wantErr: true,
		},
		{
			name: "text with meta",
			p: &a2apb.Part{
				Content: &a2apb.Part_Text{Text: "hello"},
				Metadata: mustMakeProtoMetadata(t, map[string]any{"hello": "world"}),
			},
			want: a2a.Part{Content: a2a.Text("hello"), Metadata: map[string]any{"hello": "world"}},
		},
		{
			name: "data with meta",
			p: &a2apb.Part{
				Content: &a2apb.Part_Data{Data: pData},
				Metadata: mustMakeProtoMetadata(t, map[string]any{"hello": "world"}),
			},
			want: a2a.Part{Content: a2a.Data(map[string]any{"key": "value"}), Metadata: map[string]any{"hello": "world"}},
		},
		{
			name: "file with meta",
			p: &a2apb.Part{
				Content: &a2apb.Part_Raw{Raw: []byte("content")},
				Filename: "Test File",
				MediaType: "text/plain",
				Metadata: mustMakeProtoMetadata(t, map[string]any{"hello": "world"}),
			},
			want: a2a.Part{Content: a2a.Raw([]byte("content")), Filename: "Test File", MediaType: "text/plain", Metadata: map[string]any{"hello": "world"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := fromProtoPart(tt.p)
			if (err != nil) != tt.wantErr {
				t.Errorf("fromProtoPart() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("fromProtoPart() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFromProto_fromProtoRole(t *testing.T) {
	tests := []struct {
		name string
		in   a2apb.Role
		want a2a.MessageRole
	}{
		{
			name: "user",
			in:   a2apb.Role_ROLE_USER,
			want: a2a.MessageRoleUser,
		},
		{
			name: "agent",
			in:   a2apb.Role_ROLE_AGENT,
			want: a2a.MessageRoleAgent,
		},
		{
			name: "unspecified",
			in:   a2apb.Role_ROLE_UNSPECIFIED,
			want: "",
		},
		{
			name: "invalid",
			in:   99,
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fromProtoRole(tt.in); got != tt.want {
				t.Errorf("fromProtoRole() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFromProto_fromProtoSendMessageConfig(t *testing.T) {
	historyLen := int32(10)
	zeroHistoryLen := int32(0)
	a2aHistoryLen := int(historyLen)
	a2aZeroHistoryLen := int(zeroHistoryLen)

	tests := []struct {
		name    string
		in      *a2apb.SendMessageConfiguration
		want    *a2a.SendMessageConfig
		wantErr bool
	}{
		{
			name: "full config",
			in: &a2apb.SendMessageConfiguration{
				AcceptedOutputModes: []string{"text/plain"},
				Blocking:            true,
				HistoryLength:       &historyLen,
				PushNotificationConfig: &a2apb.PushNotificationConfig{
					Id:    "test-push-config",
					Url:   "http://example.com/hook",
					Token: "secret",
					Authentication: &a2apb.AuthenticationInfo{
						Scheme:     "Bearer",
						Credentials: "token",
					},
				},
			},
			want: &a2a.SendMessageConfig{
				AcceptedOutputModes: []string{"text/plain"},
				Blocking:            proto.Bool(true),
				HistoryLength:       &a2aHistoryLen,
				PushConfig: &a2a.PushConfig{
					ID:    "test-push-config",
					URL:   "http://example.com/hook",
					Token: "secret",
					Auth: &a2a.PushAuthInfo{
						Scheme:     "Bearer",
						Credentials: "token",
					},
				},
			},
		},
		{
			name: "config with unlimited history only",
			in: &a2apb.SendMessageConfiguration{
			},
			want: &a2a.SendMessageConfig{Blocking: proto.Bool(false), HistoryLength: nil},
		},
		{
			name: "config with zero history",
			in: &a2apb.SendMessageConfiguration{
				HistoryLength: &zeroHistoryLen,
			},
			want: &a2a.SendMessageConfig{Blocking: proto.Bool(false), HistoryLength: &a2aZeroHistoryLen},
		},
		{
			name: "config with no push notification",
			in: &a2apb.SendMessageConfiguration{
				PushNotificationConfig: nil,
			},
			want: &a2a.SendMessageConfig{Blocking: proto.Bool(false)},
		},
		{
			name: "nil config",
			in:   nil,
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := fromProtoSendMessageConfig(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("fromProtoSendMessageConfig() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("fromProtoSendMessageConfig() wrong result (+got,-want)\ngot = %v\n want %v\ndiff = %s", got, tt.want, diff)
			}
		})
	}
}

func TestFromProto_fromProtoSendMessageRequest(t *testing.T) {
	pMsg := &a2apb.Message{
		MessageId: "test-msg",
		TaskId:    "test-task",
		Role:      a2apb.Role_ROLE_USER,
		Parts: []*a2apb.Part{
			{Content: &a2apb.Part_Text{Text: "hello"}},
		},
	}
	a2aMsg := a2a.Message{
		ID:     "test-msg",
		TaskID: "test-task",
		Role:   a2a.MessageRoleUser,
		Parts:  a2a.ContentParts{{Content: a2a.Text("hello")}},
	}
	historyLen := int32(10)
	a2aHistoryLen := int(historyLen)

	pConf := &a2apb.SendMessageConfiguration{
		Blocking:      true,
		HistoryLength: &historyLen,
		PushNotificationConfig: &a2apb.PushNotificationConfig{
			Id:    "push-config",
			Url:   "http://example.com/hook",
			Token: "secret",
		},
	}
	a2aConf := &a2a.SendMessageConfig{
		Blocking:      proto.Bool(true),
		HistoryLength: &a2aHistoryLen,
		PushConfig: &a2a.PushConfig{
			ID:    "push-config",
			URL:   "http://example.com/hook",
			Token: "secret",
		},
	}

	pMeta, _ := structpb.NewStruct(map[string]any{"meta_key": "meta_val"})
	a2aMeta := map[string]any{"meta_key": "meta_val"}

	tests := []struct {
		name    string
		req     *a2apb.SendMessageRequest
		want    *a2a.SendMessageRequest
		wantErr bool
	}{
		{
			name: "full request",
			req: &a2apb.SendMessageRequest{
				Message:       pMsg,
				Configuration: pConf,
				Metadata:      pMeta,
			},
			want: &a2a.SendMessageRequest{
				Message:  &a2aMsg,
				Config:   a2aConf,
				Metadata: a2aMeta,
			},
		},
		{
			name: "missing metadata",
			req:  &a2apb.SendMessageRequest{Message: pMsg, Configuration: pConf},
			want: &a2a.SendMessageRequest{Message: &a2aMsg, Config: a2aConf},
		},
		{
			name: "missing config",
			req:  &a2apb.SendMessageRequest{Message: pMsg, Metadata: pMeta},
			want: &a2a.SendMessageRequest{Message: &a2aMsg, Metadata: a2aMeta},
		},
		{
			name:    "nil request message",
			req:     &a2apb.SendMessageRequest{},
			wantErr: true,
		},
		{
			name: "nil part in message",
			req: &a2apb.SendMessageRequest{
				Message: &a2apb.Message{
					Parts: []*a2apb.Part{nil}},
			},
			wantErr: true,
		},
		{
			name: "config with missing id",
			req: &a2apb.SendMessageRequest{
				Message: pMsg,
				Configuration: &a2apb.SendMessageConfiguration{
					PushNotificationConfig: &a2apb.PushNotificationConfig{
						Url: "http://example.com/hook",
					},
				},
			},
			want: &a2a.SendMessageRequest{
				Message: &a2aMsg,
				Config:  &a2a.SendMessageConfig{PushConfig: &a2a.PushConfig{URL: "http://example.com/hook"}, Blocking: proto.Bool(false)},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FromProtoSendMessageRequest(tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("fromProtoSendMessageRequest() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("fromProtoSendMessageRequest() = %v, want %v", got, tt.want)
				return
			}

			gotBack, err := ToProtoSendMessageRequest(got)
			if err != nil {
				t.Errorf("ToProtoSendMessageRequest() error = %v", err)
				return
			}
			if !reflect.DeepEqual(gotBack, tt.req) {
				t.Errorf("ToProtoSendMessageRequest() = %v, want %v", gotBack, tt.req)
			}
		})
	}
}

func TestFromProto_fromProtoGetTaskRequest(t *testing.T) {
	historyLen := int(10)
	tests := []struct {
		name    string
		req     *a2apb.GetTaskRequest
		want    *a2a.GetTaskRequest
		wantErr bool
	}{
		{
			name: "with history",
			req:  &a2apb.GetTaskRequest{Id: "test", HistoryLength: proto.Int32(int32(historyLen))},
			want: &a2a.GetTaskRequest{ID: "test", HistoryLength: &historyLen},
		},
		{
			name: "without history",
			req:  &a2apb.GetTaskRequest{Id: "test"},
			want: &a2a.GetTaskRequest{ID: "test"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FromProtoGetTaskRequest(tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("fromProtoGetTaskRequest() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("fromProtoGetTaskRequest() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFromProto_fromProtoListTasksRequest(t *testing.T) {
	cutOffTime := time.Date(2025, 12, 15, 13, 17, 22, 0, time.UTC)
	tests := []struct {
		name string
		req  *a2apb.ListTasksRequest
		want *a2a.ListTasksRequest
	}{
		{
			name: "with pageSize",
			req:  &a2apb.ListTasksRequest{PageSize: proto.Int32(10)},
			want: &a2a.ListTasksRequest{PageSize: 10},
		},
		{
			name: "with pageToken",
			req:  &a2apb.ListTasksRequest{PageToken: "test"},
			want: &a2a.ListTasksRequest{PageToken: "test"},
		},
		{
			name: "with historyLength",
			req:  &a2apb.ListTasksRequest{HistoryLength: proto.Int32(10)},
			want: &a2a.ListTasksRequest{HistoryLength: 10},
		},
		{
			name: "with lastUpdatedAfter",
			req:  &a2apb.ListTasksRequest{StatusTimestampAfter: timestamppb.New(cutOffTime)},
			want: &a2a.ListTasksRequest{StatusTimestampAfter: &cutOffTime},
		},
		{
			name: "with includeArtifacts",
			req:  &a2apb.ListTasksRequest{IncludeArtifacts: proto.Bool(true)},
			want: &a2a.ListTasksRequest{IncludeArtifacts: true},
		},
		{
			name: "with all filters",
			req: &a2apb.ListTasksRequest{
				PageSize:             proto.Int32(10),
				PageToken:            "test",
				HistoryLength:        proto.Int32(10),
				IncludeArtifacts:     proto.Bool(true),
				StatusTimestampAfter: timestamppb.New(cutOffTime),
			},
			want: &a2a.ListTasksRequest{
				PageSize:             10,
				PageToken:            "test",
				HistoryLength:        10,
				IncludeArtifacts:     true,
				StatusTimestampAfter: &cutOffTime,
			},
		},
		{
			name: "without filters",
			req:  &a2apb.ListTasksRequest{},
			want: &a2a.ListTasksRequest{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FromProtoListTasksRequest(tt.req)
			if err != nil {
				t.Errorf("fromProtoListTasksRequest() error = %v", err)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("fromProtoListTasksRequest() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFromProto_fromProtoListTasksResponse(t *testing.T) {
	taskID := a2a.NewTaskID()
	tests := []struct {
		name string
		req  *a2apb.ListTasksResponse
		want *a2a.ListTasksResponse
	}{
		{
			name: "success",
			req: &a2apb.ListTasksResponse{
				Tasks: []*a2apb.Task{
					{
						Id:        string(taskID),
						ContextId: "test-context",
						Status:    &a2apb.TaskStatus{State: a2apb.TaskState_TASK_STATE_WORKING},
					},
				},
				TotalSize:     1,
				PageSize:      1,
				NextPageToken: "test",
			},
			want: &a2a.ListTasksResponse{
				Tasks: []*a2a.Task{
					{
						ID:        taskID,
						Status:    a2a.TaskStatus{State: a2a.TaskStateWorking},
						History:   []*a2a.Message{},
						Artifacts: []*a2a.Artifact{},
						ContextID: "test-context",
					},
				},
				TotalSize:     1,
				PageSize:      1,
				NextPageToken: "test",
			},
		},
		{
			name: "empty",
			req: &a2apb.ListTasksResponse{
				Tasks:     []*a2apb.Task{},
				TotalSize: 0,
			},
			want: &a2a.ListTasksResponse{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FromProtoListTasksResponse(tt.req)
			if err != nil {
				t.Errorf("fromProtoListTasksResponse() error = %v", err)
				return
			}
			if diff := cmp.Diff(got, tt.want); diff != "" {
				t.Errorf("fromProtoListTasksResponse() mismatch (+got -want):\n%s", diff)
			}
		})
	}
}

func TestFromProto_fromProtoCreateTaskPushConfigRequest(t *testing.T) {
	tests := []struct {
		name    string
		req     *a2apb.CreateTaskPushNotificationConfigRequest
		want    *a2a.CreateTaskPushConfigRequest
		wantErr bool
	}{
		{
			name: "success",
			req: &a2apb.CreateTaskPushNotificationConfigRequest{
				TaskId: "test-task",
				Config: &a2apb.PushNotificationConfig{
					Url: "http://example.com/hook",
					Id:  "test-config",
				},
			},
			want: &a2a.CreateTaskPushConfigRequest{TaskID: "test-task", Config: a2a.PushConfig{ID: "test-config", URL: "http://example.com/hook"}},
		},
		{
			name: "nil config",
			req: &a2apb.CreateTaskPushNotificationConfigRequest{
				TaskId: "test",
			},
			wantErr: true,
		},
		{
			name: "nil push config",
			req: &a2apb.CreateTaskPushNotificationConfigRequest{
				TaskId: "test",
				Config: &a2apb.PushNotificationConfig{},
			},
			wantErr: true,
		},
		{
			name: "empty optional ID conversion push config conversion",
			req: &a2apb.CreateTaskPushNotificationConfigRequest{
				TaskId: "t1",
				Config: &a2apb.PushNotificationConfig{Id: "", Url: "http://example.com/hook"},
			},
			want: &a2a.CreateTaskPushConfigRequest{TaskID: "t1", Config: a2a.PushConfig{URL: "http://example.com/hook"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FromProtoCreateTaskPushConfigRequest(tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("fromProtoCreateTaskPushConfigRequest() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("fromProtoCreateTaskPushConfigRequest() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFromProto_fromProtoGetTaskPushConfigRequest(t *testing.T) {
	tests := []struct {
		name    string
		req     *a2apb.GetTaskPushNotificationConfigRequest
		want    *a2a.GetTaskPushConfigRequest
		wantErr bool
	}{
		{
			name: "success",
			req: &a2apb.GetTaskPushNotificationConfigRequest{
				TaskId: "test-task",
				Id:     "test-config",
			},
			want: &a2a.GetTaskPushConfigRequest{
				TaskID: "test-task",
				ID:     "test-config",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FromProtoGetTaskPushConfigRequest(tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("fromProtoGetTaskPushConfigRequest() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("fromProtoGetTaskPushConfigRequest() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFromProto_fromProtoDeleteTaskPushConfigRequest(t *testing.T) {
	tests := []struct {
		name    string
		req     *a2apb.DeleteTaskPushNotificationConfigRequest
		want    *a2a.DeleteTaskPushConfigRequest
		wantErr bool
	}{
		{
			name: "success",
			req: &a2apb.DeleteTaskPushNotificationConfigRequest{
				TaskId: "test-task",
				Id:     "test-config",
			},
			want: &a2a.DeleteTaskPushConfigRequest{
				TaskID: "test-task",
				ID:     "test-config",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FromProtoDeleteTaskPushConfigRequest(tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("fromProtoDeleteTaskPushConfigRequest() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("fromProtoDeleteTaskPushConfigRequest() = %v, want %v", got, tt.want)
			}
		})
	}
}
