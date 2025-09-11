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

package a2agrpc

import (
	"reflect"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2apb"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestPathExtractors(t *testing.T) {
	t.Run("extractTaskID", func(t *testing.T) {
		tests := []struct {
			name    string
			path    string
			want    a2a.TaskID
			wantErr bool
		}{
			{
				name: "simple path",
				path: "tasks/12345",
				want: "12345",
			},
			{
				name: "complex path",
				path: "projects/p/locations/l/tasks/abc-def",
				want: "abc-def",
			},
			{
				name:    "missing value",
				path:    "tasks/",
				wantErr: true,
			},
			{
				name:    "missing keyword in path",
				path:    "configs/123",
				wantErr: true,
			},
			{
				name:    "empty path",
				wantErr: true,
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got, err := extractTaskID(tt.path)
				if (err != nil) != tt.wantErr {
					t.Errorf("extractTaskID() error = %v, wantErr %v", err, tt.wantErr)
					return
				}
				if got != tt.want {
					t.Errorf("extractTaskID() = %v, want %v", got, tt.want)
				}
			})
		}
	})

	t.Run("extractConfigID", func(t *testing.T) {
		tests := []struct {
			name    string
			path    string
			want    string
			wantErr bool
		}{
			{
				name: "simple path",
				path: "pushConfigs/abc-123",
				want: "abc-123",
			},
			{
				name: "complex path",
				path: "tasks/12345/pushConfigs/abc-123",
				want: "abc-123",
			},
			{
				name:    "missing value",
				path:    "pushConfigs/",
				wantErr: true,
			},
			{
				name:    "missing keyword in path",
				path:    "tasks/123",
				wantErr: true,
			},
			{
				name:    "empty path",
				wantErr: true,
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got, err := extractConfigID(tt.path)
				if (err != nil) != tt.wantErr {
					t.Errorf("extractConfigID() error = %v, wantErr %v", err, tt.wantErr)
					return
				}
				if got != tt.want {
					t.Errorf("extractConfigID() = %v, want %v", got, tt.want)
				}
			})
		}
	})
}

func TestFromProtoConversion(t *testing.T) {
	t.Run("fromProtoPart", func(t *testing.T) {
		pData, _ := structpb.NewStruct(map[string]interface{}{"key": "value"})
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
				want: a2a.DataPart{Data: map[string]interface{}{"key": "value"}},
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
	})

	t.Run("fromProtoRole", func(t *testing.T) {
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
	})

	t.Run("fromProtoSendMessageConfig", func(t *testing.T) {
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
				name: "config with no history length",
				in: &a2apb.SendMessageConfiguration{
					HistoryLength: 0,
				},
				wantErr: true,
			},
			{
				name: "config with no push notification",
				in: &a2apb.SendMessageConfiguration{
					PushNotification: nil,
				},
				wantErr: true,
			},
			{
				name: "bad push config",
				in: &a2apb.SendMessageConfiguration{
					PushNotification: &a2apb.PushNotificationConfig{Id: ""}, // Invalid
				},
				wantErr: true,
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
	})
}

func TestFromProtoRequests(t *testing.T) {
	t.Run("fromProtoSendMessageRequest", func(t *testing.T) {
		pMsg := &a2apb.Message{
			MessageId: "test-msg",
			TaskId:    "test-task",
			Role:      a2apb.Role_ROLE_USER,
			Content: []*a2apb.Part{
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

		pMeta, _ := structpb.NewStruct(map[string]interface{}{"meta_key": "meta_val"})
		a2aMeta := map[string]interface{}{"meta_key": "meta_val"}

		tests := []struct {
			name    string
			req     *a2apb.SendMessageRequest
			want    a2a.MessageSendParams
			wantErr bool
		}{
			{
				name: "full request",
				req: &a2apb.SendMessageRequest{
					Request:       pMsg,
					Configuration: pConf,
					Metadata:      pMeta,
				},
				want: a2a.MessageSendParams{
					Message:  a2aMsg,
					Config:   a2aConf,
					Metadata: a2aMeta,
				},
			},
			{
				name: "missing metadata",
				req: &a2apb.SendMessageRequest{
					Request:       pMsg,
					Configuration: pConf,
				},
				want: a2a.MessageSendParams{
					Message: a2aMsg,
					Config:  a2aConf,
				},
			},
			{
				name: "missing config",
				req: &a2apb.SendMessageRequest{
					Request:  pMsg,
					Metadata: pMeta,
				},
				want: a2a.MessageSendParams{
					Message:  a2aMsg,
					Metadata: a2aMeta,
				},
			},
			{
				name:    "nil request message",
				req:     &a2apb.SendMessageRequest{},
				wantErr: true,
			},
			{
				name: "nil part in message",
				req: &a2apb.SendMessageRequest{
					Request: &a2apb.Message{
						Content: []*a2apb.Part{
							{Part: nil},
						},
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
				wantErr: true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got, err := fromProtoSendMessageRequest(tt.req)
				if (err != nil) != tt.wantErr {
					t.Errorf("fromProtoSendMessageRequest() error = %v, wantErr %v", err, tt.wantErr)
					return
				}
				if !tt.wantErr && !reflect.DeepEqual(got, tt.want) {
					t.Errorf("fromProtoSendMessageRequest() = %v, want %v", got, tt.want)
				}
			})
		}
	})

	t.Run("fromProtoGetTaskRequest", func(t *testing.T) {
		historyLen := int(10)
		tests := []struct {
			name    string
			req     *a2apb.GetTaskRequest
			want    a2a.TaskQueryParams
			wantErr bool
		}{
			{
				name: "with history",
				req: &a2apb.GetTaskRequest{
					Name:          "tasks/test",
					HistoryLength: 10,
				},
				want: a2a.TaskQueryParams{
					ID:            "test",
					HistoryLength: &historyLen,
				},
			},
			{
				name: "without history",
				req: &a2apb.GetTaskRequest{
					Name: "tasks/test",
				},
				want: a2a.TaskQueryParams{
					ID: "test",
				},
			},
			{
				name: "invalid name",
				req: &a2apb.GetTaskRequest{
					Name: "invalid/test",
				},
				wantErr: true,
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got, err := fromProtoGetTaskRequest(tt.req)
				if (err != nil) != tt.wantErr {
					t.Errorf("fromProtoGetTaskRequest() error = %v, wantErr %v", err, tt.wantErr)
					return
				}
				if !reflect.DeepEqual(got, tt.want) {
					t.Errorf("fromProtoGetTaskRequest() = %v, want %v", got, tt.want)
				}
			})
		}
	})

	t.Run("fromProtoCreateTaskPushConfigRequest", func(t *testing.T) {
		tests := []struct {
			name    string
			req     *a2apb.CreateTaskPushNotificationConfigRequest
			want    a2a.TaskPushConfig
			wantErr bool
		}{
			{
				name: "success",
				req: &a2apb.CreateTaskPushNotificationConfigRequest{
					Parent: "tasks/test",
					Config: &a2apb.TaskPushNotificationConfig{PushNotificationConfig: &a2apb.PushNotificationConfig{Id: "test-config"}},
				},
				want: a2a.TaskPushConfig{TaskID: "test", Config: &a2a.PushConfig{ID: "test-config"}},
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
				name: "bad push config conversion",
				req: &a2apb.CreateTaskPushNotificationConfigRequest{
					Parent: "tasks/t1",
					Config: &a2apb.TaskPushNotificationConfig{PushNotificationConfig: &a2apb.PushNotificationConfig{Id: ""}},
				},
				wantErr: true,
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got, err := fromProtoCreateTaskPushConfigRequest(tt.req)
				if (err != nil) != tt.wantErr {
					t.Errorf("fromProtoCreateTaskPushConfigRequest() error = %v, wantErr %v", err, tt.wantErr)
					return
				}
				if !reflect.DeepEqual(got, tt.want) {
					t.Errorf("fromProtoCreateTaskPushConfigRequest() = %v, want %v", got, tt.want)
				}
			})
		}
	})

	t.Run("fromProtoGetTaskPushConfigRequest", func(t *testing.T) {
		tests := []struct {
			name    string
			req     *a2apb.GetTaskPushNotificationConfigRequest
			want    a2a.GetTaskPushConfigParams
			wantErr bool
		}{
			{
				name: "success",
				req: &a2apb.GetTaskPushNotificationConfigRequest{
					Name: "tasks/test-task/pushConfigs/test-config",
				},
				want: a2a.GetTaskPushConfigParams{
					TaskID:   "test-task",
					ConfigID: "test-config",
				},
			},
			{
				name: "bad keyword for task id",
				req: &a2apb.GetTaskPushNotificationConfigRequest{
					Name: "foo/test-task/pushConfigs/test-config",
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
				got, err := fromProtoGetTaskPushConfigRequest(tt.req)
				if (err != nil) != tt.wantErr {
					t.Errorf("fromProtoGetTaskPushConfigRequest() error = %v, wantErr %v", err, tt.wantErr)
					return
				}
				if !reflect.DeepEqual(got, tt.want) {
					t.Errorf("fromProtoGetTaskPushConfigRequest() = %v, want %v", got, tt.want)
				}
			})
		}
	})

	t.Run("fromProtoDeleteTaskPushConfigRequest", func(t *testing.T) {
		tests := []struct {
			name    string
			req     *a2apb.DeleteTaskPushNotificationConfigRequest
			want    a2a.DeleteTaskPushConfigParams
			wantErr bool
		}{
			{
				name: "success",
				req: &a2apb.DeleteTaskPushNotificationConfigRequest{
					Name: "tasks/test-task/pushConfigs/test-config",
				},
				want: a2a.DeleteTaskPushConfigParams{
					TaskID:   "test-task",
					ConfigID: "test-config",
				},
			},
			{
				name: "bad keyword for task id",
				req: &a2apb.DeleteTaskPushNotificationConfigRequest{
					Name: "foo/test-task/pushConfigs/test-config",
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
				got, err := fromProtoDeleteTaskPushConfigRequest(tt.req)
				if (err != nil) != tt.wantErr {
					t.Errorf("fromProtoDeleteTaskPushConfigRequest() error = %v, wantErr %v", err, tt.wantErr)
					return
				}
				if !reflect.DeepEqual(got, tt.want) {
					t.Errorf("fromProtoDeleteTaskPushConfigRequest() = %v, want %v", got, tt.want)
				}
			})
		}
	})
}

func TestToProtoConversion(t *testing.T) {
	t.Run("toProtoMessage", func(t *testing.T) {
		a2aMeta := map[string]interface{}{"key": "value"}
		pMeta, _ := structpb.NewStruct(a2aMeta)

		tests := []struct {
			name    string
			msg     *a2a.Message
			want    *a2apb.Message
			wantErr bool
		}{
			{
				name: "success",
				msg: &a2a.Message{
					ID:        "test-msg",
					ContextID: "test-ctx",
					TaskID:    "test-task",
					Role:      a2a.MessageRoleUser,
					Parts:     []a2a.Part{a2a.TextPart{Text: "hello"}},
					Metadata:  a2aMeta,
				},
				want: &a2apb.Message{
					MessageId: "test-msg",
					ContextId: "test-ctx",
					TaskId:    "test-task",
					Role:      a2apb.Role_ROLE_USER,
					Content:   []*a2apb.Part{{Part: &a2apb.Part_Text{Text: "hello"}}},
					Metadata:  pMeta,
				},
			},
			{
				name:    "nil message",
				msg:     nil,
				wantErr: true,
			},
			{
				name: "bad metdata",
				msg: &a2a.Message{
					Metadata: map[string]any{
						"bad": func() {},
					},
				},
				wantErr: true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got, err := toProtoMessage(tt.msg)
				if (err != nil) != tt.wantErr {
					t.Errorf("toProtoMessage() error = %v, wantErr %v", err, tt.wantErr)
					return
				}
				if !proto.Equal(got, tt.want) {
					t.Errorf("toProtoMessage() got = %v, want %v", got, tt.want)
				}
			})
		}
	})

	t.Run("toProtoMessges", func(t *testing.T) {
		msgs := []*a2a.Message{
			{ID: "test-message", Role: a2a.MessageRoleUser},
			{ID: "msg2", Role: a2a.MessageRoleAgent},
		}
		msg1, _ := toProtoMessage(msgs[0])
		msg2, _ := toProtoMessage(msgs[1])

		tests := []struct {
			name    string
			msgs    []*a2a.Message
			want    []*a2apb.Message
			wantErr bool
		}{
			{
				name: "success",
				msgs: msgs,
				want: []*a2apb.Message{msg1, msg2},
			},
			{
				name: "empty slice",
				msgs: []*a2a.Message{},
				want: []*a2apb.Message{},
			},
			{
				name: "conversion error",
				msgs: []*a2a.Message{
					{ID: "test-message"},
					{Metadata: map[string]any{"bad": func() {}}},
				},
				wantErr: true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got, err := toProtoMessages(tt.msgs)
				if (err != nil) != tt.wantErr {
					t.Fatalf("toProtoMessage() error = %v, wantErr %v", err, tt.wantErr)
				}
				if len(got) != len(tt.want) {
					t.Fatalf("toProtoMessage() got = %v, want %v", got, tt.want)
				}
				for i, msg := range got {
					if !proto.Equal(msg, tt.want[i]) {
						t.Errorf("toProtoMessage() got = %v, want %v", msg, tt.want[i])
					}
				}
			})
		}
	})

	t.Run("toProtoPart", func(t *testing.T) {
		pData, _ := structpb.NewStruct(map[string]interface{}{"key": "value"})
		tests := []struct {
			name    string
			p       a2a.Part
			want    *a2apb.Part
			wantErr bool
		}{
			{
				name: "text",
				p:    a2a.TextPart{Text: "hello"},
				want: &a2apb.Part{Part: &a2apb.Part_Text{Text: "hello"}},
			},
			{
				name: "data",
				p:    a2a.DataPart{Data: map[string]interface{}{"key": "value"}},
				want: &a2apb.Part{Part: &a2apb.Part_Data{Data: &a2apb.DataPart{Data: pData}}},
			},
			{
				name: "file with bytes",
				p: a2a.FilePart{
					File: a2a.FileBytes{
						FileMeta: a2a.FileMeta{
							MimeType: "text/plain",
						},
						Bytes: "content",
					},
				},
				want: &a2apb.Part{Part: &a2apb.Part_File{File: &a2apb.FilePart{
					MimeType: "text/plain",
					File:     &a2apb.FilePart_FileWithBytes{FileWithBytes: []byte("content")},
				}}},
			},
			{
				name: "file with uri",
				p: a2a.FilePart{
					File: a2a.FileURI{
						FileMeta: a2a.FileMeta{
							MimeType: "text/plain",
						},
						URI: "http://example.com/file",
					},
				},
				want: &a2apb.Part{Part: &a2apb.Part_File{File: &a2apb.FilePart{
					MimeType: "text/plain",
					File:     &a2apb.FilePart_FileWithUri{FileWithUri: "http://example.com/file"},
				}}},
			},
			{
				name:    "unsupported",
				p:       (a2a.Part)(nil), // Use a nil a2a.Part to represent an unsupported type
				wantErr: true,
			},
			{
				name: "bad data",
				p: a2a.DataPart{
					Data: map[string]interface{}{"bad": func() {}},
				},
				wantErr: true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got, err := toProtoPart(tt.p)
				if (err != nil) != tt.wantErr {
					t.Fatalf("toProtoPart() error = %v, wantErr %v", err, tt.wantErr)
				}
				if !tt.wantErr {
					if !proto.Equal(got, tt.want) {
						t.Errorf("toProtoPart() = %v, want %v", got, tt.want)
					}
				}
			})
		}
	})

	t.Run("toProtoRole", func(t *testing.T) {
		tests := []struct {
			name string
			in   a2a.MessageRole
			want a2apb.Role
		}{
			{
				name: "user",
				in:   a2a.MessageRoleUser,
				want: a2apb.Role_ROLE_USER},
			{
				name: "agent",
				in:   a2a.MessageRoleAgent,
				want: a2apb.Role_ROLE_AGENT,
			},
			{
				name: "empty",
				in:   "",
				want: a2apb.Role_ROLE_UNSPECIFIED,
			},
			{
				name: "invalid",
				in:   "invalid-role",
				want: a2apb.Role_ROLE_UNSPECIFIED,
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				if got := toProtoRole(tt.in); got != tt.want {
					t.Errorf("toProtoRole() = %v, want %v", got, tt.want)
				}
			})
		}
	})

	t.Run("toProtoTaskState", func(t *testing.T) {
		tests := []struct {
			name  string
			state a2a.TaskState
			want  a2apb.TaskState
		}{
			{
				name:  string(a2a.TaskStateAuthRequired),
				state: a2a.TaskStateAuthRequired,
				want:  a2apb.TaskState_TASK_STATE_AUTH_REQUIRED,
			},
			{
				name:  string(a2a.TaskStateCanceled),
				state: a2a.TaskStateCanceled,
				want:  a2apb.TaskState_TASK_STATE_CANCELLED,
			},
			{
				name:  string(a2a.TaskStateCompleted),
				state: a2a.TaskStateCompleted,
				want:  a2apb.TaskState_TASK_STATE_COMPLETED,
			},
			{
				name:  string(a2a.TaskStateFailed),
				state: a2a.TaskStateFailed,
				want:  a2apb.TaskState_TASK_STATE_FAILED,
			},
			{
				name:  string(a2a.TaskStateInputRequired),
				state: a2a.TaskStateInputRequired,
				want:  a2apb.TaskState_TASK_STATE_INPUT_REQUIRED,
			},
			{
				name:  string(a2a.TaskStateRejected),
				state: a2a.TaskStateRejected,
				want:  a2apb.TaskState_TASK_STATE_REJECTED,
			},
			{
				name:  string(a2a.TaskStateSubmitted),
				state: a2a.TaskStateSubmitted,
				want:  a2apb.TaskState_TASK_STATE_SUBMITTED,
			},
			{
				name:  string(a2a.TaskStateWorking),
				state: a2a.TaskStateWorking,
				want:  a2apb.TaskState_TASK_STATE_WORKING,
			},
			{
				name:  "unknown",
				state: "unknown",
				want:  a2apb.TaskState_TASK_STATE_UNSPECIFIED,
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				if got := toProtoTaskState(tt.state); got != tt.want {
					t.Errorf("toProtoTaskState() = %v, want %v", got, tt.want)
				}
			})
		}
	})

	t.Run("toProtoTaskStatus", func(t *testing.T) {
		now := time.Now()
		pNow := timestamppb.New(now)
		msg := &a2a.Message{ID: "update-msg"}
		pMsg, _ := toProtoMessage(msg)

		tests := []struct {
			name    string
			status  a2a.TaskStatus
			want    *a2apb.TaskStatus
			wantErr bool
		}{
			{
				name: "full status",
				status: a2a.TaskStatus{
					State:     a2a.TaskStateWorking,
					Message:   msg,
					Timestamp: &now,
				},
				want: &a2apb.TaskStatus{
					State:     a2apb.TaskState_TASK_STATE_WORKING,
					Update:    pMsg,
					Timestamp: pNow,
				},
			},
			{
				name: "nil message",
				status: a2a.TaskStatus{
					State:     a2a.TaskStateCompleted,
					Timestamp: &now,
				},
				want: &a2apb.TaskStatus{
					State:     a2apb.TaskState_TASK_STATE_COMPLETED,
					Timestamp: pNow,
				},
			},
			{
				name: "nil timestamp",
				status: a2a.TaskStatus{
					State:   a2a.TaskStateWorking,
					Message: msg,
				},
				want: &a2apb.TaskStatus{
					State:  a2apb.TaskState_TASK_STATE_WORKING,
					Update: pMsg,
				},
			},
			{
				name: "minimal status",
				status: a2a.TaskStatus{
					State: a2a.TaskStateSubmitted,
				},
				want: &a2apb.TaskStatus{
					State: a2apb.TaskState_TASK_STATE_SUBMITTED,
				},
			},
			{
				name: "bad message conversion",
				status: a2a.TaskStatus{
					State: a2a.TaskStateWorking,
					Message: &a2a.Message{
						Metadata: map[string]any{"bad": func() {}}},
				},
				wantErr: true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got, err := toProtoTaskStatus(tt.status)
				if (err != nil) != tt.wantErr {
					t.Errorf("toProtoTaskStatus() error = %v, wantErr %v", err, tt.wantErr)
					return
				}
				if !tt.wantErr && !proto.Equal(got, tt.want) {
					t.Errorf("toProtoTaskStatus() = %v, want %v", got, tt.want)
				}
			})
		}
	})

	t.Run("toProtoTaskPushConfig", func(t *testing.T) {
		tests := []struct {
			name    string
			config  *a2a.TaskPushConfig
			want    *a2apb.TaskPushNotificationConfig
			wantErr bool
		}{
			{
				name: "full config",
				config: &a2a.TaskPushConfig{
					TaskID: "t1",
					Config: &a2a.PushConfig{
						ID:    "c1",
						URL:   "http://a.com",
						Token: "tok",
						Auth: &a2a.PushAuthInfo{
							Schemes:     []string{"Bearer"},
							Credentials: "cred",
						},
					},
				},
				want: &a2apb.TaskPushNotificationConfig{
					Name: "tasks/t1/pushConfigs/c1",
					PushNotificationConfig: &a2apb.PushNotificationConfig{
						Id:    "c1",
						Url:   "http://a.com",
						Token: "tok",
						Authentication: &a2apb.AuthenticationInfo{
							Schemes:     []string{"Bearer"},
							Credentials: "cred",
						},
					},
				},
			},
			{
				name: "config without auth",
				config: &a2a.TaskPushConfig{
					TaskID: "t1",
					Config: &a2a.PushConfig{ID: "c1", URL: "http://a.com"},
				},
				want: &a2apb.TaskPushNotificationConfig{
					Name: "tasks/t1/pushConfigs/c1",
					PushNotificationConfig: &a2apb.PushNotificationConfig{
						Id:  "c1",
						Url: "http://a.com",
					},
				},
			},
			{
				name:    "nil config",
				config:  nil,
				wantErr: true,
			},
			{
				name: "nil inner push config",
				config: &a2a.TaskPushConfig{
					TaskID: "test-task",
					Config: nil,
				},
				wantErr: true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got, err := toProtoTaskPushConfig(tt.config)
				if (err != nil) != tt.wantErr {
					t.Errorf("toProtoTaskPushConfig() error = %v, wantErr %v", err, tt.wantErr)
					return
				}
				if !tt.wantErr && !proto.Equal(got, tt.want) {
					t.Errorf("toProtoTaskPushConfig() = %v, want %v", got, tt.want)
				}
			})
		}
	})

	t.Run("toProtoListTaskPushConfig", func(t *testing.T) {
		configs := []a2a.TaskPushConfig{
			{TaskID: "test-task", Config: &a2a.PushConfig{ID: "test-config1"}},
			{TaskID: "test-task", Config: &a2a.PushConfig{ID: "test-config2"}},
		}
		pConf1, _ := toProtoTaskPushConfig(&configs[0])
		pConf2, _ := toProtoTaskPushConfig(&configs[1])

		tests := []struct {
			name    string
			configs []a2a.TaskPushConfig
			want    *a2apb.ListTaskPushNotificationConfigResponse
			wantErr bool
		}{
			{
				name:    "success",
				configs: configs,
				want: &a2apb.ListTaskPushNotificationConfigResponse{
					Configs: []*a2apb.TaskPushNotificationConfig{pConf1, pConf2},
				},
			},
			{
				name:    "empty slice",
				configs: []a2a.TaskPushConfig{},
				want: &a2apb.ListTaskPushNotificationConfigResponse{
					Configs: []*a2apb.TaskPushNotificationConfig{},
				},
			},
			{
				name: "conversion error",
				configs: []a2a.TaskPushConfig{
					{TaskID: "test-task", Config: nil},
				},
				wantErr: true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got, err := toProtoListTaskPushConfig(tt.configs)
				if (err != nil) != tt.wantErr {
					t.Errorf("toProtoListTaskPushConfig() error = %v, wantErr %v", err, tt.wantErr)
					return
				}
				if !tt.wantErr {
					if !proto.Equal(got, tt.want) {
						t.Errorf("toProtoListTaskPushConfig() got = %v, want %v", got, tt.want)
					}
				}
			})
		}
	})

	t.Run("toProtoTask", func(t *testing.T) {
		now := time.Now()
		pNow := timestamppb.New(now)
		a2aTaskID := a2a.TaskID("task-test")
		a2aContextID := "ctx-test"
		a2aMsgID := "msg-test"
		a2aArtifactID := a2a.ArtifactID("art-abc")

		a2aMeta := map[string]interface{}{"task_key": "task_val"}
		pMeta, _ := structpb.NewStruct(a2aMeta)

		a2aHistory := []*a2a.Message{
			{ID: a2aMsgID, Role: a2a.MessageRoleUser, Parts: []a2a.Part{a2a.TextPart{Text: "history"}}},
		}
		pHistory := []*a2apb.Message{
			{MessageId: a2aMsgID, Role: a2apb.Role_ROLE_USER, Content: []*a2apb.Part{{Part: &a2apb.Part_Text{Text: "history"}}}},
		}

		a2aArtifacts := []a2a.Artifact{
			{ID: a2aArtifactID, Name: "artifact1", Parts: []a2a.Part{a2a.TextPart{Text: "artifact content"}}},
		}
		pArtifacts := []*a2apb.Artifact{
			{ArtifactId: string(a2aArtifactID), Name: "artifact1", Parts: []*a2apb.Part{{Part: &a2apb.Part_Text{Text: "artifact content"}}}},
		}

		a2aStatus := a2a.TaskStatus{
			State:     a2a.TaskStateWorking,
			Message:   &a2a.Message{ID: "status-msg", Role: a2a.MessageRoleAgent},
			Timestamp: &now,
		}
		pStatus := &a2apb.TaskStatus{
			State:     a2apb.TaskState_TASK_STATE_WORKING,
			Update:    &a2apb.Message{MessageId: "status-msg", Role: a2apb.Role_ROLE_AGENT},
			Timestamp: pNow,
		}

		tests := []struct {
			name    string
			task    *a2a.Task
			want    *a2apb.Task
			wantErr bool
		}{
			{
				name: "success",
				task: &a2a.Task{
					ID:        a2aTaskID,
					ContextID: a2aContextID,
					Status:    a2aStatus,
					Artifacts: a2aArtifacts,
					History:   a2aHistory,
					Metadata:  a2aMeta,
				},
				want: &a2apb.Task{
					Id:        string(a2aTaskID),
					ContextId: a2aContextID,
					Status:    pStatus,
					Artifacts: pArtifacts,
					History:   pHistory,
					Metadata:  pMeta,
				},
			},
			{
				name:    "nil task",
				task:    nil,
				wantErr: true,
			},
			{
				name: "bad status",
				task: &a2a.Task{
					ID:        a2aTaskID,
					ContextID: a2aContextID,
					Status: a2a.TaskStatus{
						State: a2a.TaskStateWorking,
						Message: &a2a.Message{
							Metadata: map[string]any{"bad": func() {}},
						},
						Timestamp: &now,
					},
					Artifacts: a2aArtifacts,
					History:   a2aHistory,
					Metadata:  a2aMeta,
				},
				wantErr: true,
			},
			{
				name: "bad artifact",
				task: &a2a.Task{
					ID:        a2aTaskID,
					ContextID: a2aContextID,
					Status:    a2aStatus,
					Artifacts: []a2a.Artifact{
						{Metadata: map[string]any{"bad": func() {}}},
					},
					History:  a2aHistory,
					Metadata: a2aMeta,
				},
				wantErr: true,
			},
			{
				name: "bad history",
				task: &a2a.Task{
					ID:        a2aTaskID,
					ContextID: a2aContextID,
					Status:    a2aStatus,
					Artifacts: a2aArtifacts,
					History:   []*a2a.Message{{Metadata: map[string]any{"bad": func() {}}}},
					Metadata:  a2aMeta,
				},
				wantErr: true,
			},
			{
				name: "bad metadata",
				task: &a2a.Task{
					ID:        a2aTaskID,
					ContextID: a2aContextID,
					Status:    a2aStatus,
					Artifacts: a2aArtifacts,
					History:   a2aHistory,
					Metadata:  map[string]any{"bad": func() {}},
				},
				wantErr: true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got, err := toProtoTask(tt.task)
				if (err != nil) != tt.wantErr {
					t.Errorf("toProtoTask() error = %v, wantErr %v", err, tt.wantErr)
					return
				}
				if !tt.wantErr {
					if !proto.Equal(got, tt.want) {
						t.Errorf("toProtoTask() got = %v, want %v", got, tt.want)
					}
				}
			})
		}
	})
}

func TestToProtoResponses(t *testing.T) {
	t.Run("toProtoSendMessageResponse", func(t *testing.T) {
		a2aMsg := &a2a.Message{
			ID:   "test-message",
			Role: a2a.MessageRoleAgent,
			Parts: []a2a.Part{
				a2a.TextPart{Text: "response"},
			},
		}
		pMsg := &a2apb.Message{
			MessageId: "test-message",
			Role:      a2apb.Role_ROLE_AGENT,
			Content: []*a2apb.Part{
				{Part: &a2apb.Part_Text{Text: "response"}},
			},
		}

		now := time.Now()
		a2aTask := &a2a.Task{
			ID:        "test-task",
			ContextID: "test-ctx",
			Status: a2a.TaskStatus{
				State:     a2a.TaskStateCompleted,
				Timestamp: &now,
				Message:   a2aMsg,
			},
		}
		pTask := &a2apb.Task{
			Id:        "test-task",
			ContextId: "test-ctx",
			Status: &a2apb.TaskStatus{
				State:     a2apb.TaskState_TASK_STATE_COMPLETED,
				Timestamp: timestamppb.New(now),
				Update:    pMsg,
			},
		}

		tests := []struct {
			name    string
			result  a2a.SendMessageResult
			want    *a2apb.SendMessageResponse
			wantErr bool
		}{
			{
				name:   "message response",
				result: a2aMsg,
				want: &a2apb.SendMessageResponse{
					Payload: &a2apb.SendMessageResponse_Msg{Msg: pMsg},
				},
			},
			{
				name:   "task response",
				result: a2aTask,
				want: &a2apb.SendMessageResponse{
					Payload: &a2apb.SendMessageResponse_Task{Task: pTask},
				},
			},
			{
				name:    "nil result",
				result:  nil,
				wantErr: true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got, err := toProtoSendMessageResponse(tt.result)
				if (err != nil) != tt.wantErr {
					t.Errorf("toProtoSendMessageResponse() error = %v, wantErr %v", err, tt.wantErr)
					return
				}
				if !tt.wantErr {
					if !proto.Equal(got, tt.want) {
						t.Errorf("toProtoSendMessageResponse() got = %v, want %v", got, tt.want)
					}
				}
			})
		}
	})

	t.Run("toProtoStreamResponse", func(t *testing.T) {
		now := time.Now()
		pNow := timestamppb.New(now)
		a2aMsg := &a2a.Message{ID: "test-message"}
		pMsg, _ := toProtoMessage(a2aMsg)
		a2aTask := &a2a.Task{ID: "test-task"}
		pTask, _ := toProtoTask(a2aTask)
		a2aStatusEvent := &a2a.TaskStatusUpdateEvent{
			TaskID:    "test-task",
			ContextID: "test-ctx",
			Final:     true,
			Status: a2a.TaskStatus{
				State:     a2a.TaskStateCompleted,
				Timestamp: &now,
			},
		}
		pStatusEvent := &a2apb.StreamResponse_StatusUpdate{
			StatusUpdate: &a2apb.TaskStatusUpdateEvent{
				TaskId:    "test-task",
				ContextId: "test-ctx",
				Final:     true,
				Status: &a2apb.TaskStatus{
					State:     a2apb.TaskState_TASK_STATE_COMPLETED,
					Timestamp: pNow,
				},
			},
		}
		a2aArtifactEvent := &a2a.TaskArtifactUpdateEvent{
			TaskID:    "test-task",
			ContextID: "test-ctx",
			Artifact:  a2a.Artifact{ID: "art1"},
		}
		pArtifactEvent := &a2apb.StreamResponse_ArtifactUpdate{
			ArtifactUpdate: &a2apb.TaskArtifactUpdateEvent{
				TaskId:    "test-task",
				ContextId: "test-ctx",
				Artifact:  &a2apb.Artifact{ArtifactId: "art1"},
			},
		}

		tests := []struct {
			name    string
			event   a2a.Event
			want    *a2apb.StreamResponse
			wantErr bool
		}{
			{
				name:  "message",
				event: a2aMsg,
				want: &a2apb.StreamResponse{
					Payload: &a2apb.StreamResponse_Msg{Msg: pMsg},
				},
			},
			{
				name:  "task",
				event: a2aTask,
				want: &a2apb.StreamResponse{
					Payload: &a2apb.StreamResponse_Task{Task: pTask},
				},
			},
			{
				name:  "status update",
				event: a2aStatusEvent,
				want:  &a2apb.StreamResponse{Payload: pStatusEvent},
			},
			{
				name:  "artifact update",
				event: a2aArtifactEvent,
				want:  &a2apb.StreamResponse{Payload: pArtifactEvent},
			},
			{
				name:    "nil event",
				event:   nil,
				wantErr: true,
			},
			{
				name: "bad message",
				event: &a2a.Message{
					Metadata: map[string]any{"bad": func() {}},
				},
				wantErr: true,
			},
			{
				name: "bad task",
				event: &a2a.Task{
					Metadata: map[string]any{"bad": func() {}},
				},
				wantErr: true,
			},
			{
				name: "bad status update",
				event: &a2a.TaskStatusUpdateEvent{
					Metadata: map[string]any{"bad": func() {}},
				},
				wantErr: true,
			},
			{
				name: "bad artifact update",
				event: &a2a.TaskArtifactUpdateEvent{
					Metadata: map[string]any{"bad": func() {}},
				},
				wantErr: true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got, err := toProtoStreamResponse(tt.event)
				if (err != nil) != tt.wantErr {
					t.Errorf("toProtoStreamResponse() error = %v, wantErr %v", err, tt.wantErr)
					return
				}
				if !tt.wantErr && !proto.Equal(got, tt.want) {
					t.Errorf("toProtoStreamResponse() = %v, want %v", got, tt.want)
				}
			})
		}
	})
}
