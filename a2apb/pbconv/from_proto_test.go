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

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2apb"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestFromProto_fromProtoPart(t *testing.T) {
	pData, _ := structpb.NewStruct(map[string]any{"key": "value"})
	tests := []struct {
		name    string
		p       *a2apb.Part
		want    a2a.Part
		wantErr bool
	}{
		{
			name: "text",
			p:    &a2apb.Part{Part: &a2apb.Part_Text{Text: "hello"}},
			want: a2a.TextPart{Text: "hello"},
		},
		{
			name: "data",
			p:    &a2apb.Part{Part: &a2apb.Part_Data{Data: &a2apb.DataPart{Data: pData}}},
			want: a2a.DataPart{Data: map[string]any{"key": "value"}},
		},
		{
			name: "file with bytes",
			p: &a2apb.Part{Part: &a2apb.Part_File{File: &a2apb.FilePart{
				MimeType: "text/plain",
				File:     &a2apb.FilePart_FileWithBytes{FileWithBytes: []byte("content")},
			}}},
			want: a2a.FilePart{
				File: a2a.FileBytes{
					FileMeta: a2a.FileMeta{
						MimeType: "text/plain",
					},
					Bytes: "content",
				},
			},
		},
		{
			name: "file with uri",
			p: &a2apb.Part{Part: &a2apb.Part_File{File: &a2apb.FilePart{
				MimeType: "text/plain",
				File:     &a2apb.FilePart_FileWithUri{FileWithUri: "http://example.com/file"},
			}}},
			want: a2a.FilePart{
				File: a2a.FileURI{
					FileMeta: a2a.FileMeta{
						MimeType: "text/plain",
					},
					URI: "http://example.com/file",
				},
			},
		},
		{
			name:    "unsupported",
			p:       &a2apb.Part{Part: nil},
			wantErr: true,
		},
		{
			name: "text with meta",
			p: &a2apb.Part{
				Part:     &a2apb.Part_Text{Text: "hello"},
				Metadata: mustMakeProtoMetadata(t, map[string]any{"hello": "world"}),
			},
			want: a2a.TextPart{Text: "hello", Metadata: map[string]any{"hello": "world"}},
		},
		{
			name: "data with meta",
			p: &a2apb.Part{
				Part:     &a2apb.Part_Data{Data: &a2apb.DataPart{Data: pData}},
				Metadata: mustMakeProtoMetadata(t, map[string]any{"hello": "world"}),
			},
			want: a2a.DataPart{Data: map[string]any{"key": "value"}, Metadata: map[string]any{"hello": "world"}},
		},
		{
			name: "file with meta",
			p: &a2apb.Part{
				Part: &a2apb.Part_File{File: &a2apb.FilePart{
					File: &a2apb.FilePart_FileWithBytes{FileWithBytes: []byte("content")},
				}},
				Metadata: mustMakeProtoMetadata(t, map[string]any{"hello": "world"}),
			},
			want: a2a.FilePart{
				File:     a2a.FileBytes{Bytes: "content"},
				Metadata: map[string]any{"hello": "world"},
			},
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
	a2aHistoryLen := int(historyLen)

	tests := []struct {
		name    string
		in      *a2apb.SendMessageConfiguration
		want    *a2a.MessageSendConfig
		wantErr bool
	}{
		{
			name: "full config",
			in: &a2apb.SendMessageConfiguration{
				AcceptedOutputModes: []string{"text/plain"},
				Blocking:            true,
				HistoryLength:       historyLen,
				PushNotification: &a2apb.PushNotificationConfig{
					Id:    "test-push-config",
					Url:   "http://example.com/hook",
					Token: "secret",
					Authentication: &a2apb.AuthenticationInfo{
						Schemes:     []string{"Bearer"},
						Credentials: "token",
					},
				},
			},
			want: &a2a.MessageSendConfig{
				AcceptedOutputModes: []string{"text/plain"},
				Blocking:            true,
				HistoryLength:       &a2aHistoryLen,
				PushConfig: &a2a.PushConfig{
					ID:    "test-push-config",
					URL:   "http://example.com/hook",
					Token: "secret",
					Auth: &a2a.PushAuthInfo{
						Schemes:     []string{"Bearer"},
						Credentials: "token",
					},
				},
			},
		},
		{
			name: "config with unlimited history only",
			in: &a2apb.SendMessageConfiguration{
				HistoryLength: 0,
			},
			want: &a2a.MessageSendConfig{},
		},
		{
			name: "config with no push notification",
			in: &a2apb.SendMessageConfiguration{
				PushNotification: nil,
			},
			want: &a2a.MessageSendConfig{},
		},
		{
			name: "nil config",
			in:   nil,
			want: nil,
		},
	}
	for _, tt := range tests {
		got, err := fromProtoSendMessageConfig(tt.in)
		if (err != nil) != tt.wantErr {
			t.Fatalf("fromProtoSendMessageConfig() error = %v", err)
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("fromProtoSendMessageConfig() got = %v, want %v", got, tt.want)
		}
	}
}

func TestFromProto_fromProtoSendMessageRequest(t *testing.T) {
	pMsg := &a2apb.Message{
		MessageId: "test-msg",
		TaskId:    "test-task",
		Role:      a2apb.Role_ROLE_USER,
		Parts: []*a2apb.Part{
			{Part: &a2apb.Part_Text{Text: "hello"}},
		},
	}
	a2aMsg := a2a.Message{
		ID:     "test-msg",
		TaskID: "test-task",
		Role:   a2a.MessageRoleUser,
		Parts:  []a2a.Part{a2a.TextPart{Text: "hello"}},
	}
	historyLen := int32(10)
	a2aHistoryLen := int(historyLen)

	pConf := &a2apb.SendMessageConfiguration{
		Blocking:      true,
		HistoryLength: historyLen,
		PushNotification: &a2apb.PushNotificationConfig{
			Id:    "push-config",
			Url:   "http://example.com/hook",
			Token: "secret",
		},
	}
	a2aConf := &a2a.MessageSendConfig{
		Blocking:      true,
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
		want    *a2a.MessageSendParams
		wantErr bool
	}{
		{
			name: "full request",
			req: &a2apb.SendMessageRequest{
				Request:       pMsg,
				Configuration: pConf,
				Metadata:      pMeta,
			},
			want: &a2a.MessageSendParams{
				Message:  &a2aMsg,
				Config:   a2aConf,
				Metadata: a2aMeta,
			},
		},
		{
			name: "missing metadata",
			req:  &a2apb.SendMessageRequest{Request: pMsg, Configuration: pConf},
			want: &a2a.MessageSendParams{Message: &a2aMsg, Config: a2aConf},
		},
		{
			name: "missing config",
			req:  &a2apb.SendMessageRequest{Request: pMsg, Metadata: pMeta},
			want: &a2a.MessageSendParams{Message: &a2aMsg, Metadata: a2aMeta},
		},
		{
			name: "nil request message",
			req:  &a2apb.SendMessageRequest{},
			want: &a2a.MessageSendParams{},
		},
		{
			name: "nil part in message",
			req: &a2apb.SendMessageRequest{
				Request: &a2apb.Message{
					Parts: []*a2apb.Part{{Part: nil}},
				},
			},
			wantErr: true,
		},
		{
			name: "config with missing id",
			req: &a2apb.SendMessageRequest{
				Request: pMsg,
				Configuration: &a2apb.SendMessageConfiguration{
					PushNotification: &a2apb.PushNotificationConfig{},
				},
			},
			want: &a2a.MessageSendParams{
				Message: &a2aMsg,
				Config:  &a2a.MessageSendConfig{PushConfig: &a2a.PushConfig{}},
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
			if !tt.wantErr && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("fromProtoSendMessageRequest() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFromProto_fromProtoGetTaskRequest(t *testing.T) {
	historyLen := int(10)
	tests := []struct {
		name    string
		req     *a2apb.GetTaskRequest
		want    *a2a.TaskQueryParams
		wantErr bool
	}{
		{
			name: "with history",
			req:  &a2apb.GetTaskRequest{Name: "tasks/test", HistoryLength: 10},
			want: &a2a.TaskQueryParams{ID: "test", HistoryLength: &historyLen},
		},
		{
			name: "without history",
			req:  &a2apb.GetTaskRequest{Name: "tasks/test"},
			want: &a2a.TaskQueryParams{ID: "test"},
		},
		{
			name:    "invalid name",
			req:     &a2apb.GetTaskRequest{Name: "invalid/test"},
			wantErr: true,
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

func TestFromProto_fromProtoCreateTaskPushConfigRequest(t *testing.T) {
	tests := []struct {
		name    string
		req     *a2apb.CreateTaskPushNotificationConfigRequest
		want    *a2a.TaskPushConfig
		wantErr bool
	}{
		{
			name: "success",
			req: &a2apb.CreateTaskPushNotificationConfigRequest{
				Parent: "tasks/test",
				Config: &a2apb.TaskPushNotificationConfig{
					PushNotificationConfig: &a2apb.PushNotificationConfig{Id: "test-config"},
				},
			},
			want: &a2a.TaskPushConfig{TaskID: "test", Config: a2a.PushConfig{ID: "test-config"}},
		},
		{
			name: "nil config",
			req: &a2apb.CreateTaskPushNotificationConfigRequest{
				Parent: "tasks/test",
			},
			wantErr: true,
		},
		{
			name: "nil push config",
			req: &a2apb.CreateTaskPushNotificationConfigRequest{
				Parent: "tasks/test",
				Config: &a2apb.TaskPushNotificationConfig{},
			},
			wantErr: true,
		},
		{
			name: "bad parent",
			req: &a2apb.CreateTaskPushNotificationConfigRequest{
				Parent: "foo/bar",
			},
			wantErr: true,
		},
		{
			name: "empty optional ID conversion push config conversion",
			req: &a2apb.CreateTaskPushNotificationConfigRequest{
				Parent: "tasks/t1",
				Config: &a2apb.TaskPushNotificationConfig{
					PushNotificationConfig: &a2apb.PushNotificationConfig{Id: ""},
				},
			},
			want: &a2a.TaskPushConfig{TaskID: "t1", Config: a2a.PushConfig{}},
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
		want    *a2a.GetTaskPushConfigParams
		wantErr bool
	}{
		{
			name: "success",
			req: &a2apb.GetTaskPushNotificationConfigRequest{
				Name: "tasks/test-task/pushNotificationConfigs/test-config",
			},
			want: &a2a.GetTaskPushConfigParams{
				TaskID:   "test-task",
				ConfigID: "test-config",
			},
		},
		{
			name: "bad keyword for task id",
			req: &a2apb.GetTaskPushNotificationConfigRequest{
				Name: "foo/test-task/pushNotificationConfigs/test-config",
			},
			wantErr: true,
		},
		{
			name: "bad keyword for config id",
			req: &a2apb.GetTaskPushNotificationConfigRequest{
				Name: "tasks/test-task/bar/test-config",
			},
			wantErr: true,
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
		want    *a2a.DeleteTaskPushConfigParams
		wantErr bool
	}{
		{
			name: "success",
			req: &a2apb.DeleteTaskPushNotificationConfigRequest{
				Name: "tasks/test-task/pushNotificationConfigs/test-config",
			},
			want: &a2a.DeleteTaskPushConfigParams{
				TaskID:   "test-task",
				ConfigID: "test-config",
			},
		},
		{
			name: "bad keyword for task id",
			req: &a2apb.DeleteTaskPushNotificationConfigRequest{
				Name: "foo/test-task/pushNotificationConfigs/test-config",
			},
			wantErr: true,
		},
		{
			name: "bad keyword for config id",
			req: &a2apb.DeleteTaskPushNotificationConfigRequest{
				Name: "tasks/test-task/bar/test-config",
			},
			wantErr: true,
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
