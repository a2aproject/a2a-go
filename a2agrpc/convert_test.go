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

func TestFromProtoConversion(t *testing.T) {
	t.Run("fromProtoSendMessageRequest", func(t *testing.T) {
		pMsg := &a2apb.Message{
			MessageId: "msg1",
			TaskId:    "task1",
			Role:      a2apb.Role_ROLE_USER,
			Content: []*a2apb.Part{
				{Part: &a2apb.Part_Text{Text: "hello"}},
			},
		}
		a2aMsg := a2a.Message{
			ID:     "msg1",
			TaskID: "task1",
			Role:   a2a.MessageRoleUser,
			Parts:  []a2a.Part{a2a.TextPart{Text: "hello"}},
		}
		historyLen := int32(10)
		a2aHistoryLen := int(historyLen)

		pConf := &a2apb.SendMessageConfiguration{
			Blocking:      true,
			HistoryLength: historyLen,
		}
		a2aConf := &a2a.MessageSendConfig{
			Blocking:      true,
			HistoryLength: &a2aHistoryLen,
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
				wantErr: false,
			},
			{
				name: "request only message",
				req: &a2apb.SendMessageRequest{
					Request: pMsg,
				},
				want: a2a.MessageSendParams{
					Message: a2aMsg,
				},
				wantErr: false,
			},
			{
				name:    "nil request message",
				req:     &a2apb.SendMessageRequest{},
				wantErr: true,
			},
			{
				name: "bad part in message",
				req: &a2apb.SendMessageRequest{
					Request: &a2apb.Message{
						Content: []*a2apb.Part{
							{Part: nil},
						},
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
					t.Errorf("fromProtoSendMessageRequest() = %+v, want %+v", got, tt.want)
				}
			})
		}
	})

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
					MimeType: "text/plain",
					File:     a2a.FileBytes{Bytes: "content"},
				},
			},
			{
				name: "file with uri",
				p: &a2apb.Part{Part: &a2apb.Part_File{File: &a2apb.FilePart{
					MimeType: "text/plain",
					File:     &a2apb.FilePart_FileWithUri{FileWithUri: "http://example.com/file"},
				}}},
				want: a2a.FilePart{
					MimeType: "text/plain",
					File:     a2a.FileURI{URI: "http://example.com/file"},
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
			{"user", a2apb.Role_ROLE_USER, a2a.MessageRoleUser},
			{"agent", a2apb.Role_ROLE_AGENT, a2a.MessageRoleAgent},
			{"unspecified", a2apb.Role_ROLE_UNSPECIFIED, ""},
			{"invalid", 99, ""},
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

		pConf := &a2apb.SendMessageConfiguration{
			AcceptedOutputModes: []string{"text/plain"},
			Blocking:            true,
			HistoryLength:       historyLen,
			PushNotification: &a2apb.PushNotificationConfig{
				Id:    "push-1",
				Url:   "http://example.com/hook",
				Token: "secret",
				Authentication: &a2apb.AuthenticationInfo{
					Schemes:     []string{"Bearer"},
					Credentials: "token",
				},
			},
		}

		want := &a2a.MessageSendConfig{
			AcceptedOutputModes: []string{"text/plain"},
			Blocking:            true,
			HistoryLength:       &a2aHistoryLen,
			PushConfig: &a2a.PushConfig{
				ID:    "push-1",
				URL:   "http://example.com/hook",
				Token: "secret",
				Auth: &a2a.PushAuthInfo{
					Schemes:     []string{"Bearer"},
					Credentials: "token",
				},
			},
		}

		got := fromProtoSendMessageConfig(pConf)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("fromProtoSendMessageConfig() got = %+v, want %+v", got, want)
		}

		if fromProtoSendMessageConfig(nil) != nil {
			t.Error("fromProtoSendMessageConfig(nil) should be nil")
		}
	})
}

func TestToProtoConversion(t *testing.T) {
	t.Run("toProtoSendMessageResponse", func(t *testing.T) {
		a2aMsg := &a2a.Message{
			ID:   "msg1",
			Role: a2a.MessageRoleAgent,
			Parts: []a2a.Part{
				a2a.TextPart{Text: "response"},
			},
		}
		pMsg := &a2apb.Message{
			MessageId: "msg1",
			Role:      a2apb.Role_ROLE_AGENT,
			Content: []*a2apb.Part{
				{Part: &a2apb.Part_Text{Text: "response"}},
			},
		}

		now := time.Now()
		a2aTask := &a2a.Task{
			ID:        "task1",
			ContextID: "ctx1",
			Status: a2a.TaskStatus{
				State:     a2a.TaskStateCompleted,
				Timestamp: &now,
				Message:   a2aMsg,
			},
		}
		pTask := &a2apb.Task{
			Id:        "task1",
			ContextId: "ctx1",
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
					MimeType: "text/plain",
					File:     a2a.FileBytes{Bytes: "content"},
				},
				want: &a2apb.Part{Part: &a2apb.Part_File{File: &a2apb.FilePart{
					MimeType: "text/plain",
					File:     &a2apb.FilePart_FileWithBytes{FileWithBytes: []byte("content")},
				}}},
			},
			{
				name: "file with uri",
				p: a2a.FilePart{
					MimeType: "text/plain",
					File:     a2a.FileURI{URI: "http://example.com/file"},
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
					t.Errorf("toProtoPart() error = %v, wantErr %v", err, tt.wantErr)
					return
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
			{"user", a2a.MessageRoleUser, a2apb.Role_ROLE_USER},
			{"agent", a2a.MessageRoleAgent, a2apb.Role_ROLE_AGENT},
			{"empty", "", a2apb.Role_ROLE_UNSPECIFIED},
			{"invalid", "invalid-role", a2apb.Role_ROLE_UNSPECIFIED},
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
			{string(a2a.TaskStateAuthRequired), a2a.TaskStateAuthRequired, a2apb.TaskState_TASK_STATE_AUTH_REQUIRED},
			{string(a2a.TaskStateCanceled), a2a.TaskStateCanceled, a2apb.TaskState_TASK_STATE_CANCELLED},
			{string(a2a.TaskStateCompleted), a2a.TaskStateCompleted, a2apb.TaskState_TASK_STATE_COMPLETED},
			{string(a2a.TaskStateFailed), a2a.TaskStateFailed, a2apb.TaskState_TASK_STATE_FAILED},
			{string(a2a.TaskStateInputRequired), a2a.TaskStateInputRequired, a2apb.TaskState_TASK_STATE_INPUT_REQUIRED},
			{string(a2a.TaskStateRejected), a2a.TaskStateRejected, a2apb.TaskState_TASK_STATE_REJECTED},
			{string(a2a.TaskStateSubmitted), a2a.TaskStateSubmitted, a2apb.TaskState_TASK_STATE_SUBMITTED},
			{string(a2a.TaskStateWorking), a2a.TaskStateWorking, a2apb.TaskState_TASK_STATE_WORKING},
			{"unknown", "unknown", a2apb.TaskState_TASK_STATE_UNSPECIFIED},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				if got := toProtoTaskState(tt.state); got != tt.want {
					t.Errorf("toProtoTaskState() = %v, want %v", got, tt.want)
				}
			})
		}
	})
}

func TestToProtoMessage(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		a2aMeta := map[string]interface{}{"key": "value"}
		pMeta, _ := structpb.NewStruct(a2aMeta)
		msg := &a2a.Message{
			ID:        "msg-1",
			ContextID: "ctx-1",
			TaskID:    "task-1",
			Role:      a2a.MessageRoleUser,
			Parts:     []a2a.Part{a2a.TextPart{Text: "hello"}},
			Metadata:  a2aMeta,
		}
		want := &a2apb.Message{
			MessageId: "msg-1",
			ContextId: "ctx-1",
			TaskId:    "task-1",
			Role:      a2apb.Role_ROLE_USER,
			Content:   []*a2apb.Part{{Part: &a2apb.Part_Text{Text: "hello"}}},
			Metadata:  pMeta,
		}

		got, err := toProtoMessage(msg)
		if err != nil {
			t.Fatalf("toProtoMessage() failed: %v", err)
		}
		if !proto.Equal(got, want) {
			t.Errorf("toProtoMessage() got = %v, want %v", got, want)
		}
	})

	t.Run("nil message", func(t *testing.T) {
		_, err := toProtoMessage(nil)
		if err == nil {
			t.Error("toProtoMessage(nil) should have returned an error")
		}
	})

	t.Run("bad metadata", func(t *testing.T) {
		msg := &a2a.Message{
			Metadata: map[string]any{
				"bad": func() {},
			},
		}
		_, err := toProtoMessage(msg)
		if err == nil {
			t.Error("toProtoMessage with bad metadata should have failed")
		}
	})
}

func TestToProtoTask(t *testing.T) {
	now := time.Now()
	pNow := timestamppb.New(now)
	a2aTaskID := a2a.TaskID("task-123")
	a2aContextID := "ctx-456"
	a2aMsgID := "msg-789"
	a2aArtifactID := a2a.ArtifactID("art-abc")

	a2aMeta := map[string]interface{}{"task_key": "task_val"}
	pMeta, _ := structpb.NewStruct(a2aMeta)

	a2aHistory := []a2a.Message{
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

	a2aTask := &a2a.Task{
		ID:        a2aTaskID,
		ContextID: a2aContextID,
		Status:    a2aStatus,
		Artifacts: a2aArtifacts,
		History:   a2aHistory,
		Metadata:  a2aMeta,
	}

	pTask := &a2apb.Task{
		Id:        string(a2aTaskID),
		ContextId: a2aContextID,
		Status:    pStatus,
		Artifacts: pArtifacts,
		History:   pHistory,
		Metadata:  pMeta,
	}

	t.Run("success", func(t *testing.T) {
		got, err := toProtoTask(a2aTask)
		if err != nil {
			t.Fatalf("toProtoTask() failed: %v", err)
		}
		if !proto.Equal(got, pTask) {
			t.Errorf("toProtoTask() got = %v, want %v", got, pTask)
		}
	})

	t.Run("nil task", func(t *testing.T) {
		_, err := toProtoTask(nil)
		if err == nil {
			t.Error("toProtoTask(nil) should have returned an error")
		}
	})

	t.Run("bad status", func(t *testing.T) {
		badTask := *a2aTask
		badTask.Status.Message = &a2a.Message{Metadata: map[string]any{"bad": func() {}}}
		_, err := toProtoTask(&badTask)
		if err == nil {
			t.Error("toProtoTask with bad status should have failed")
		}
	})

	t.Run("bad artifacts", func(t *testing.T) {
		badTask := *a2aTask
		badTask.Artifacts = []a2a.Artifact{{Metadata: map[string]any{"bad": func() {}}}}
		_, err := toProtoTask(&badTask)
		if err == nil {
			t.Error("toProtoTask with bad artifacts should have failed")
		}
	})

	t.Run("bad history", func(t *testing.T) {
		badTask := *a2aTask
		badTask.History = []a2a.Message{{Metadata: map[string]any{"bad": func() {}}}}
		_, err := toProtoTask(&badTask)
		if err == nil {
			t.Error("toProtoTask with bad history should have failed")
		}
	})

	t.Run("bad metadata", func(t *testing.T) {
		badTask := *a2aTask
		badTask.Metadata = map[string]any{"bad": func() {}}
		_, err := toProtoTask(&badTask)
		if err == nil {
			t.Error("toProtoTask with bad metadata should have failed")
		}
	})
}
