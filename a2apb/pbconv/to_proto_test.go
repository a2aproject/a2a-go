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
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2apb"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestToProto_toProtoMessage(t *testing.T) {
	a2aMeta := map[string]any{"key": "value"}
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
			name: "nil message",
			msg:  nil,
			want: nil,
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
}

func TestToProto_toProtoMessges(t *testing.T) {
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
}

func TestToProto_toProtoPart(t *testing.T) {
	pData, _ := structpb.NewStruct(map[string]any{"key": "value"})
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
			p:    a2a.DataPart{Data: map[string]any{"key": "value"}},
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
				Data: map[string]any{"bad": func() {}},
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
}

func TestToProto_toProtoRole(t *testing.T) {
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
}

func TestToProto_toProtoTaskState(t *testing.T) {
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
}

func TestToProto_toProtoTaskStatus(t *testing.T) {
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
}

func TestToProto_toProtoTaskPushConfig(t *testing.T) {
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
				Config: a2a.PushConfig{
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
				Config: a2a.PushConfig{ID: "c1", URL: "http://a.com"},
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
			name:   "nil config",
			config: nil,
			want:   nil,
		},
		{
			name: "empty inner push config",
			config: &a2a.TaskPushConfig{
				TaskID: "test-task",
				Config: a2a.PushConfig{},
			},
			want: &a2apb.TaskPushNotificationConfig{
				Name:                   "tasks/test-task/pushConfigs/",
				PushNotificationConfig: &a2apb.PushNotificationConfig{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ToProtoTaskPushConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("toProtoTaskPushConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && !proto.Equal(got, tt.want) {
				t.Errorf("toProtoTaskPushConfig() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestToProto_toProtoListTaskPushConfig(t *testing.T) {
	configs := []*a2a.TaskPushConfig{
		{TaskID: "test-task", Config: a2a.PushConfig{ID: "test-config1"}},
		{TaskID: "test-task", Config: a2a.PushConfig{ID: "test-config2"}},
	}
	pConf1, _ := ToProtoTaskPushConfig(configs[0])
	pConf2, _ := ToProtoTaskPushConfig(configs[1])

	tests := []struct {
		name    string
		configs []*a2a.TaskPushConfig
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
			configs: []*a2a.TaskPushConfig{},
			want: &a2apb.ListTaskPushNotificationConfigResponse{
				Configs: []*a2apb.TaskPushNotificationConfig{},
			},
		},
		{
			name:    "conversion error",
			configs: []*a2a.TaskPushConfig{{TaskID: "", Config: a2a.PushConfig{ID: "test"}}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ToProtoListTaskPushConfig(tt.configs)
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
}
func TestToProto_toProtoTask(t *testing.T) {
	now := time.Now()
	pNow := timestamppb.New(now)
	a2aTaskID := a2a.TaskID("task-test")
	a2aContextID := "ctx-test"
	a2aMsgID := "msg-test"
	a2aArtifactID := a2a.ArtifactID("art-abc")

	a2aMeta := map[string]any{"task_key": "task_val"}
	pMeta, _ := structpb.NewStruct(a2aMeta)

	a2aHistory := []*a2a.Message{
		{ID: a2aMsgID, Role: a2a.MessageRoleUser, Parts: []a2a.Part{a2a.TextPart{Text: "history"}}},
	}
	pHistory := []*a2apb.Message{
		{MessageId: a2aMsgID, Role: a2apb.Role_ROLE_USER, Content: []*a2apb.Part{{Part: &a2apb.Part_Text{Text: "history"}}}},
	}

	a2aArtifacts := []*a2a.Artifact{
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
			name: "nil task",
			task: nil,
			want: nil,
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
				Artifacts: []*a2a.Artifact{
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
			got, err := ToProtoTask(tt.task)
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
}
func TestToProto_toProtoAgentCard(t *testing.T) {
	a2aCard := &a2a.AgentCard{
		ProtocolVersion:    "1.0",
		Name:               "Test Agent",
		Description:        "An agent for testing.",
		URL:                "https://example.com/agent",
		PreferredTransport: a2a.TransportProtocolGRPC,
		AdditionalInterfaces: []a2a.AgentInterface{
			{Transport: a2a.TransportProtocolJSONRPC, URL: "https://example.com/agent/jsonrpc"},
		},
		Provider: &a2a.AgentProvider{
			Org: "Test Org",
			URL: "https://example.com/org",
		},
		Version:          "0.1.0",
		DocumentationURL: "https://example.com/docs",
		Capabilities: a2a.AgentCapabilities{
			Streaming:         true,
			PushNotifications: true,
			Extensions: []a2a.AgentExtension{
				{URI: "ext-uri", Description: "ext-desc", Required: true, Params: map[string]any{"key": "val"}},
			},
		},
		SecuritySchemes: a2a.NamedSecuritySchemes{
			"apiKey": a2a.APIKeySecurityScheme{
				Name:        "X-API-KEY",
				In:          a2a.APIKeySecuritySchemeInHeader,
				Description: "API Key",
			},
			"oauth2": a2a.OAuth2SecurityScheme{
				Description: "OAuth2",
				Flows: a2a.OAuthFlows{
					AuthorizationCode: &a2a.AuthorizationCodeOAuthFlow{
						AuthorizationURL: "https://example.com/auth",
						TokenURL:         "https://example.com/token",
						Scopes:           map[string]string{"read": "read scope"},
					},
				},
			},
		},
		Security: []a2a.SecurityRequirements{
			{"apiKey": {}},
			{"oauth2": {"read"}},
		},
		DefaultInputModes:  []string{"text/plain"},
		DefaultOutputModes: []string{"text/plain"},
		Skills: []a2a.AgentSkill{
			{
				ID:          "skill1",
				Name:        "Test Skill",
				Description: "A skill for testing.",
				Tags:        []string{"test"},
				Examples:    []string{"do a test"},
				InputModes:  []string{"text/markdown"},
				OutputModes: []string{"text/markdown"},
			},
		},
		SupportsAuthenticatedExtendedCard: true,
	}

	extParams, _ := structpb.NewStruct(map[string]any{"key": "val"})
	pCard := &a2apb.AgentCard{
		ProtocolVersion:    "1.0",
		Name:               "Test Agent",
		Description:        "An agent for testing.",
		Url:                "https://example.com/agent",
		PreferredTransport: string(a2a.TransportProtocolGRPC),
		AdditionalInterfaces: []*a2apb.AgentInterface{
			{Transport: string(a2a.TransportProtocolJSONRPC), Url: "https://example.com/agent/jsonrpc"},
		},
		Provider: &a2apb.AgentProvider{
			Organization: "Test Org",
			Url:          "https://example.com/org",
		},
		Version:          "0.1.0",
		DocumentationUrl: "https://example.com/docs",
		Capabilities: &a2apb.AgentCapabilities{
			Streaming:         true,
			PushNotifications: true,
			Extensions: []*a2apb.AgentExtension{
				{Uri: "ext-uri", Description: "ext-desc", Required: true, Params: extParams},
			},
		},
		SecuritySchemes: map[string]*a2apb.SecurityScheme{
			"apiKey": {
				Scheme: &a2apb.SecurityScheme_ApiKeySecurityScheme{
					ApiKeySecurityScheme: &a2apb.APIKeySecurityScheme{
						Name:        "X-API-KEY",
						Location:    string(a2a.APIKeySecuritySchemeInHeader),
						Description: "API Key",
					},
				},
			},
			"oauth2": {
				Scheme: &a2apb.SecurityScheme_Oauth2SecurityScheme{
					Oauth2SecurityScheme: &a2apb.OAuth2SecurityScheme{
						Description: "OAuth2",
						Flows: &a2apb.OAuthFlows{
							Flow: &a2apb.OAuthFlows_AuthorizationCode{
								AuthorizationCode: &a2apb.AuthorizationCodeOAuthFlow{
									AuthorizationUrl: "https://example.com/auth",
									TokenUrl:         "https://example.com/token",
									Scopes:           map[string]string{"read": "read scope"},
								},
							},
						},
					},
				},
			},
		},
		Security: []*a2apb.Security{
			{Schemes: map[string]*a2apb.StringList{"apiKey": {List: []string{}}}},
			{Schemes: map[string]*a2apb.StringList{"oauth2": {List: []string{"read"}}}},
		},
		DefaultInputModes:  []string{"text/plain"},
		DefaultOutputModes: []string{"text/plain"},
		Skills: []*a2apb.AgentSkill{
			{
				Id:          "skill1",
				Name:        "Test Skill",
				Description: "A skill for testing.",
				Tags:        []string{"test"},
				Examples:    []string{"do a test"},
				InputModes:  []string{"text/markdown"},
				OutputModes: []string{"text/markdown"},
			},
		},
		SupportsAuthenticatedExtendedCard: true,
	}

	tests := []struct {
		name    string
		card    *a2a.AgentCard
		want    *a2apb.AgentCard
		wantErr bool
	}{
		{
			name: "success",
			card: a2aCard,
			want: pCard,
		},
		{
			name: "nil card",
			card: nil,
			want: nil,
		},
		{
			name: "bad capabilities",
			card: &a2a.AgentCard{
				Capabilities: a2a.AgentCapabilities{
					Extensions: []a2a.AgentExtension{
						{Params: map[string]any{"bad": func() {}}},
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ToProtoAgentCard(tt.card)
			if (err != nil) != tt.wantErr {
				t.Errorf("toProtoAgentCard() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if !proto.Equal(got, tt.want) {
					t.Errorf("toProtoAgentCard() got = %v, want %v", got, tt.want)
					if !proto.Equal(got, tt.want) {
						t.Errorf("toProtoAgentCard() got = %v, want %v", got, tt.want)
					}
				}
			}
		})
	}
}

func TestToProto_toProtoOAuthFlows(t *testing.T) {
	tests := []struct {
		name    string
		flows   a2a.OAuthFlows
		want    *a2apb.OAuthFlows
		wantErr bool
	}{
		{
			name: "authorization code flow",
			flows: a2a.OAuthFlows{
				AuthorizationCode: &a2a.AuthorizationCodeOAuthFlow{
					AuthorizationURL: "https://auth.com/auth",
					TokenURL:         "https://auth.com/token",
					RefreshURL:       "https://auth.com/refresh",
					Scopes:           map[string]string{"read": "read data"},
				},
			},
			want: &a2apb.OAuthFlows{
				Flow: &a2apb.OAuthFlows_AuthorizationCode{
					AuthorizationCode: &a2apb.AuthorizationCodeOAuthFlow{
						AuthorizationUrl: "https://auth.com/auth",
						TokenUrl:         "https://auth.com/token",
						RefreshUrl:       "https://auth.com/refresh",
						Scopes:           map[string]string{"read": "read data"},
					},
				},
			},
		},
		{
			name: "client credentials flow",
			flows: a2a.OAuthFlows{
				ClientCredentials: &a2a.ClientCredentialsOAuthFlow{
					TokenURL:   "https://auth.com/token",
					RefreshURL: "https://auth.com/refresh",
					Scopes:     map[string]string{"write": "write data"},
				},
			},
			want: &a2apb.OAuthFlows{
				Flow: &a2apb.OAuthFlows_ClientCredentials{
					ClientCredentials: &a2apb.ClientCredentialsOAuthFlow{
						TokenUrl:   "https://auth.com/token",
						RefreshUrl: "https://auth.com/refresh",
						Scopes:     map[string]string{"write": "write data"},
					},
				},
			},
		},
		{
			name: "implicit flow",
			flows: a2a.OAuthFlows{
				Implicit: &a2a.ImplicitOAuthFlow{
					AuthorizationURL: "https://auth.com/auth",
					RefreshURL:       "https://auth.com/refresh",
					Scopes:           map[string]string{"profile": "read profile"},
				},
			},
			want: &a2apb.OAuthFlows{
				Flow: &a2apb.OAuthFlows_Implicit{
					Implicit: &a2apb.ImplicitOAuthFlow{
						AuthorizationUrl: "https://auth.com/auth",
						RefreshUrl:       "https://auth.com/refresh",
						Scopes:           map[string]string{"profile": "read profile"},
					},
				},
			},
		},
		{
			name: "password flow",
			flows: a2a.OAuthFlows{
				Password: &a2a.PasswordOAuthFlow{
					TokenURL:   "https://auth.com/token",
					RefreshURL: "https://auth.com/refresh",
					Scopes:     map[string]string{"user": "user info"},
				},
			},
			want: &a2apb.OAuthFlows{
				Flow: &a2apb.OAuthFlows_Password{
					Password: &a2apb.PasswordOAuthFlow{
						TokenUrl:   "https://auth.com/token",
						RefreshUrl: "https://auth.com/refresh",
						Scopes:     map[string]string{"user": "user info"},
					},
				},
			},
		},
		{
			name:    "no flows specified",
			flows:   a2a.OAuthFlows{},
			wantErr: true,
		},
		{
			name: "multiple flows specified",
			flows: a2a.OAuthFlows{
				ClientCredentials: &a2a.ClientCredentialsOAuthFlow{
					TokenURL:   "https://auth.com/token",
					RefreshURL: "https://auth.com/refresh",
					Scopes:     map[string]string{"write": "write data"},
				},
				Password: &a2a.PasswordOAuthFlow{
					TokenURL:   "https://auth.com/token",
					RefreshURL: "https://auth.com/refresh",
					Scopes:     map[string]string{"user": "user info"},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := toProtoOAuthFlows(tt.flows)
			if (err != nil) != tt.wantErr {
				t.Errorf("toProtoOAuthFlows() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if !proto.Equal(got, tt.want) {
					t.Errorf("toProtoOAuthFlows() got = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestToProto_toProtoSecurityScheme(t *testing.T) {
	tests := []struct {
		name    string
		scheme  a2a.SecurityScheme
		want    *a2apb.SecurityScheme
		wantErr bool
	}{
		{
			name: "api key scheme",
			scheme: a2a.APIKeySecurityScheme{
				Name:        "X-API-KEY",
				In:          a2a.APIKeySecuritySchemeInHeader,
				Description: "API Key",
			},
			want: &a2apb.SecurityScheme{
				Scheme: &a2apb.SecurityScheme_ApiKeySecurityScheme{
					ApiKeySecurityScheme: &a2apb.APIKeySecurityScheme{
						Name:        "X-API-KEY",
						Location:    string(a2a.APIKeySecuritySchemeInHeader),
						Description: "API Key",
					},
				},
			},
		},
		{
			name: "http auth scheme",
			scheme: a2a.HTTPAuthSecurityScheme{
				Scheme:       "Bearer",
				BearerFormat: "JWT",
				Description:  "HTTP Bearer",
			},
			want: &a2apb.SecurityScheme{
				Scheme: &a2apb.SecurityScheme_HttpAuthSecurityScheme{
					HttpAuthSecurityScheme: &a2apb.HTTPAuthSecurityScheme{
						Scheme:       "Bearer",
						BearerFormat: "JWT",
						Description:  "HTTP Bearer",
					},
				},
			},
		},
		{
			name: "openid connect scheme",
			scheme: a2a.OpenIDConnectSecurityScheme{
				OpenIDConnectURL: "https://oidc.com/.well-known",
				Description:      "OIDC",
			},
			want: &a2apb.SecurityScheme{
				Scheme: &a2apb.SecurityScheme_OpenIdConnectSecurityScheme{
					OpenIdConnectSecurityScheme: &a2apb.OpenIdConnectSecurityScheme{
						OpenIdConnectUrl: "https://oidc.com/.well-known",
						Description:      "OIDC",
					},
				},
			},
		},
		{
			name:   "mutual tls scheme",
			scheme: a2a.MutualTLSSecurityScheme{},
			want:   nil, // This is expected to return nil, nil
		},
		{
			name: "oauth2 scheme",
			scheme: a2a.OAuth2SecurityScheme{
				Description: "OAuth2",
				Flows: a2a.OAuthFlows{
					AuthorizationCode: &a2a.AuthorizationCodeOAuthFlow{
						AuthorizationURL: "https://auth.com/auth",
						TokenURL:         "https://auth.com/token",
						Scopes:           map[string]string{"read": "read data"},
					},
				},
			},
			want: &a2apb.SecurityScheme{
				Scheme: &a2apb.SecurityScheme_Oauth2SecurityScheme{
					Oauth2SecurityScheme: &a2apb.OAuth2SecurityScheme{
						Description: "OAuth2",
						Flows: &a2apb.OAuthFlows{
							Flow: &a2apb.OAuthFlows_AuthorizationCode{
								AuthorizationCode: &a2apb.AuthorizationCodeOAuthFlow{
									AuthorizationUrl: "https://auth.com/auth",
									TokenUrl:         "https://auth.com/token",
									Scopes:           map[string]string{"read": "read data"},
								},
							},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := toProtoSecurityScheme(tt.scheme)
			if (err != nil) != tt.wantErr {
				t.Errorf("toProtoSecurityScheme() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if !proto.Equal(got, tt.want) {
					t.Errorf("toProtoSecurityScheme() got = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestToProto_toProtoSendMessageResponse(t *testing.T) {
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
			got, err := ToProtoSendMessageResponse(tt.result)
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
}

func TestToProto_toProtoStreamResponse(t *testing.T) {
	now := time.Now()
	pNow := timestamppb.New(now)
	a2aMsg := &a2a.Message{ID: "test-message"}
	pMsg, _ := toProtoMessage(a2aMsg)
	a2aTask := &a2a.Task{ID: "test-task", Status: a2a.TaskStatus{State: a2a.TaskStateWorking}}
	pTask, _ := ToProtoTask(a2aTask)
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
		Artifact:  &a2a.Artifact{ID: "art1"},
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
			got, err := ToProtoStreamResponse(tt.event)
			if (err != nil) != tt.wantErr {
				t.Errorf("toProtoStreamResponse() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && !proto.Equal(got, tt.want) {
				t.Errorf("toProtoStreamResponse() = %v, want %v", got, tt.want)
			}
		})
	}
}
