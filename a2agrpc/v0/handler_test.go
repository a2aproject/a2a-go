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
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"net"
	"reflect"
	"sort"
	"testing"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2apb/v0"
	"github.com/a2aproject/a2a-go/a2apb/v0/pbconv"
	"github.com/a2aproject/a2a-go/a2asrv"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

var defaultMockHandler = &mockRequestHandler{
	SendMessageFunc: func(ctx context.Context, req *a2a.SendMessageRequest) (a2a.SendMessageResult, error) {
		if req.Message.ID == "handler-error" {
			return nil, errors.New("handler error")
		}
		taskID := req.Message.TaskID
		if taskID == "" {
			taskID = a2a.NewTaskID()
		}
		return &a2a.Message{
			ID:     fmt.Sprintf("%s-response", req.Message.ID),
			TaskID: taskID,
			Role:   a2a.MessageRoleAgent,
			Parts:  req.Message.Parts,
		}, nil
	},

	SendMessageStreamFunc: func(ctx context.Context, req *a2a.SendMessageRequest) iter.Seq2[a2a.Event, error] {
		if req.Message.ID == "handler-error" {
			return func(yield func(a2a.Event, error) bool) {
				yield(nil, errors.New("handler stream error"))
			}
		}

		taskID := req.Message.TaskID
		if taskID == "" {
			taskID = a2a.NewTaskID()
		}

		task := &a2a.Task{
			ID:        taskID,
			ContextID: req.Message.ContextID,
			Status:    a2a.TaskStatus{State: a2a.TaskStateSubmitted},
		}
		statusUpdate := a2a.NewStatusUpdateEvent(task, a2a.TaskStateWorking, nil)
		finalMessage := &a2a.Message{
			ID:     fmt.Sprintf("%s-response", req.Message.ID),
			TaskID: taskID,
			Role:   a2a.MessageRoleAgent,
			Parts:  req.Message.Parts, // Echo back the parts
		}
		events := []a2a.Event{task, statusUpdate, finalMessage}

		return func(yield func(a2a.Event, error) bool) {
			for _, e := range events {
				if !yield(e, nil) {
					return
				}
			}
		}
	},

	SubscribeToTaskFunc: func(ctx context.Context, req *a2a.SubscribeToTaskRequest) iter.Seq2[a2a.Event, error] {
		if req.ID == "handler-error" {
			return func(yield func(a2a.Event, error) bool) {
				yield(nil, errors.New("handler resubscribe error"))
			}
		}
		task := &a2a.Task{
			ID:        req.ID,
			ContextID: "resubscribe-context",
			Status:    a2a.TaskStatus{State: a2a.TaskStateWorking},
		}
		statusUpdate := a2a.NewStatusUpdateEvent(task, a2a.TaskStateCompleted, nil)
		events := []a2a.Event{task, statusUpdate}

		return func(yield func(a2a.Event, error) bool) {
			for _, e := range events {
				if !yield(e, nil) {
					return
				}
			}
		}
	},
}

// mockRequestHandler is a mock of a2asrv.RequestHandler.
type mockRequestHandler struct {
	tasks       map[a2a.TaskID]*a2a.Task
	pushConfigs map[a2a.TaskID]map[string]*a2a.TaskPushConfig

	// Fields to capture call parameters
	capturedGetTaskRequest              *a2a.GetTaskRequest
	capturedListTasksRequest            *a2a.ListTasksRequest
	capturedCancelTaskRequest           *a2a.CancelTaskRequest
	capturedSendMessageRequest          *a2a.SendMessageRequest
	capturedSendMessageStreamRequest    *a2a.SendMessageRequest
	capturedSubscribeToTaskRequest      *a2a.SubscribeToTaskRequest
	capturedCreateTaskPushConfigRequest *a2a.CreateTaskPushConfigRequest
	capturedGetTaskPushConfigRequest    *a2a.GetTaskPushConfigRequest
	capturedListTaskPushConfigRequest   *a2a.ListTaskPushConfigRequest
	capturedDeleteTaskPushConfigRequest *a2a.DeleteTaskPushConfigRequest

	// Override specific methods
	a2asrv.RequestHandler
	SendMessageFunc       func(ctx context.Context, message *a2a.SendMessageRequest) (a2a.SendMessageResult, error)
	SendMessageStreamFunc func(ctx context.Context, params *a2a.SendMessageRequest) iter.Seq2[a2a.Event, error]
	SubscribeToTaskFunc   func(ctx context.Context, req *a2a.SubscribeToTaskRequest) iter.Seq2[a2a.Event, error]
}

var _ a2asrv.RequestHandler = (*mockRequestHandler)(nil)

func (m *mockRequestHandler) GetTask(ctx context.Context, req *a2a.GetTaskRequest) (*a2a.Task, error) {
	m.capturedGetTaskRequest = req
	if task, ok := m.tasks[req.ID]; ok {
		if req.HistoryLength != nil && *req.HistoryLength > 0 {
			if len(task.History) > int(*req.HistoryLength) {
				task.History = task.History[len(task.History)-int(*req.HistoryLength):]
			}
		}
		return task, nil
	}

	return nil, fmt.Errorf("task not found, taskID: %s", req.ID)
}

func (m *mockRequestHandler) ListTasks(ctx context.Context, req *a2a.ListTasksRequest) (*a2a.ListTasksResponse, error) {
	m.capturedListTasksRequest = req

	var tasks []*a2a.Task
	for _, task := range m.tasks {
		taskCopy := *task
		if req.ContextID != "" && req.ContextID != taskCopy.ContextID {
			continue
		}
		tasks = append(tasks, &taskCopy)
	}
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].ID < tasks[j].ID
	})

	return &a2a.ListTasksResponse{
		Tasks:         tasks,
		TotalSize:     len(tasks),
		NextPageToken: "",
	}, nil
}

func (m *mockRequestHandler) CancelTask(ctx context.Context, req *a2a.CancelTaskRequest) (*a2a.Task, error) {
	m.capturedCancelTaskRequest = req
	if task, ok := m.tasks[req.ID]; ok {
		task.Status = a2a.TaskStatus{
			State: a2a.TaskStateCanceled,
		}
		m.tasks[req.ID] = task
		return task, nil
	}
	return nil, fmt.Errorf("task not found, taskID: %s", req.ID)
}

func (m *mockRequestHandler) SendMessage(ctx context.Context, req *a2a.SendMessageRequest) (a2a.SendMessageResult, error) {
	m.capturedSendMessageRequest = req
	if m.SendMessageFunc != nil {
		return m.SendMessageFunc(ctx, req)
	}
	return nil, errors.New("SendMessage not implemented")
}

func (m *mockRequestHandler) SendStreamingMessage(ctx context.Context, req *a2a.SendMessageRequest) iter.Seq2[a2a.Event, error] {
	m.capturedSendMessageStreamRequest = req
	if m.SendMessageStreamFunc != nil {
		return m.SendMessageStreamFunc(ctx, req)
	}
	return func(yield func(a2a.Event, error) bool) {
		yield(nil, errors.New("OnSendMessageStream not implemented"))
	}
}

func (m *mockRequestHandler) SubscribeToTask(ctx context.Context, req *a2a.SubscribeToTaskRequest) iter.Seq2[a2a.Event, error] {
	m.capturedSubscribeToTaskRequest = req
	if m.SubscribeToTaskFunc != nil {
		return m.SubscribeToTaskFunc(ctx, req)
	}
	return func(yield func(a2a.Event, error) bool) {
		yield(nil, errors.New("OnResubscribeToTask not implemented"))
	}
}

func (m *mockRequestHandler) CreateTaskPushConfig(ctx context.Context, req *a2a.CreateTaskPushConfigRequest) (*a2a.TaskPushConfig, error) {
	m.capturedCreateTaskPushConfigRequest = req
	if _, ok := m.tasks[req.TaskID]; ok {
		if _, ok := m.pushConfigs[req.TaskID]; !ok {
			m.pushConfigs[req.TaskID] = make(map[string]*a2a.TaskPushConfig)
		}
		taskPushConfig := &a2a.TaskPushConfig{
			TaskID: req.TaskID,
			ID:     req.Config.ID,
			Config: req.Config,
		}
		m.pushConfigs[req.TaskID][req.Config.ID] = taskPushConfig
		return taskPushConfig, nil
	}

	return nil, fmt.Errorf("task for push config not found, taskID: %s", req.TaskID)
}

func (m *mockRequestHandler) GetTaskPushConfig(ctx context.Context, req *a2a.GetTaskPushConfigRequest) (*a2a.TaskPushConfig, error) {
	m.capturedGetTaskPushConfigRequest = req
	if _, ok := m.tasks[req.TaskID]; ok {
		if pushConfigs, ok := m.pushConfigs[req.TaskID]; ok {
			return pushConfigs[req.ID], nil
		}
		return nil, fmt.Errorf("push config not found, taskID: %s, configID: %s", req.TaskID, req.ID)
	}

	return nil, fmt.Errorf("task for push config not found, taskID: %s", req.TaskID)
}

func (m *mockRequestHandler) ListTaskPushConfig(ctx context.Context, req *a2a.ListTaskPushConfigRequest) ([]*a2a.TaskPushConfig, error) {
	m.capturedListTaskPushConfigRequest = req
	if _, ok := m.tasks[req.TaskID]; ok {
		if pushConfigs, ok := m.pushConfigs[req.TaskID]; ok {
			var result []*a2a.TaskPushConfig
			for _, v := range pushConfigs {
				result = append(result, v)
			}
			return result, nil
		}
		return []*a2a.TaskPushConfig{}, nil // no configs for task id
	}

	return []*a2a.TaskPushConfig{}, fmt.Errorf("task for push config not found, taskID: %s", req.TaskID)
}

func (m *mockRequestHandler) DeleteTaskPushConfig(ctx context.Context, req *a2a.DeleteTaskPushConfigRequest) error {
	m.capturedDeleteTaskPushConfigRequest = req
	if _, ok := m.tasks[req.TaskID]; ok {
		if pushConfigs, ok := m.pushConfigs[req.TaskID]; ok {
			if _, ok := pushConfigs[req.ID]; ok {
				delete(pushConfigs, req.ID)
				return nil
			}
		}
		return nil
	}

	return fmt.Errorf("task for push config not found, taskID: %s", req.TaskID)
}

func startTestServer(t *testing.T, handler a2asrv.RequestHandler) a2apb.A2AServiceClient {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)
	s := grpc.NewServer()
	grpcHandler := NewHandler(handler)
	grpcHandler.RegisterWith(s)

	go func() {
		if err := s.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			t.Logf("Server exited with error: %v", err)
		}
	}()
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to dial bufnet: %v", err)
	}

	client := a2apb.NewA2AServiceClient(conn)

	t.Cleanup(
		func() {
			s.Stop()
			_ = conn.Close()
		},
	)

	return client
}

func TestGrpcHandler_GetTask(t *testing.T) {
	ctx := t.Context()
	taskID := a2a.TaskID("test-task")
	historyLen := int(10)
	mockHandler := &mockRequestHandler{
		tasks: map[a2a.TaskID]*a2a.Task{
			taskID: {ID: taskID, ContextID: "test-context", Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}},
		},
	}
	client := startTestServer(t, mockHandler)

	tests := []struct {
		name      string
		req       *a2apb.GetTaskRequest
		want      *a2apb.Task
		wantQuery *a2a.GetTaskRequest
		wantErr   codes.Code
	}{
		{
			name: "success",
			req:  &a2apb.GetTaskRequest{Name: fmt.Sprintf("tasks/%s", taskID)},
			want: &a2apb.Task{
				Id:        string(taskID),
				ContextId: "test-context",
				Status:    &a2apb.TaskStatus{State: a2apb.TaskState_TASK_STATE_SUBMITTED},
			},
			wantQuery: &a2a.GetTaskRequest{ID: taskID},
		},
		{
			name: "success with history",
			req:  &a2apb.GetTaskRequest{Name: fmt.Sprintf("tasks/%s", taskID), HistoryLength: 10},
			want: &a2apb.Task{
				Id:        string(taskID),
				ContextId: "test-context",
				Status:    &a2apb.TaskStatus{State: a2apb.TaskState_TASK_STATE_SUBMITTED},
			},
			wantQuery: &a2a.GetTaskRequest{ID: taskID, HistoryLength: &historyLen},
		},
		{
			name:    "invalid name",
			req:     &a2apb.GetTaskRequest{Name: "invalid/name"},
			wantErr: codes.InvalidArgument,
		},
		{
			name:    "handler error",
			req:     &a2apb.GetTaskRequest{Name: "tasks/handler-error"},
			wantErr: codes.Internal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockHandler.capturedGetTaskRequest = nil
			resp, err := client.GetTask(ctx, tt.req)
			if tt.wantErr != codes.OK {
				if err == nil {
					t.Fatal("GetTask() expected error, got nil")
				}
				st, ok := status.FromError(err)
				if !ok {
					t.Fatalf("GetTask() error is not a gRPC status error: %v", err)
				}
				if st.Code() != tt.wantErr {
					t.Errorf("GetTask() got error code %v, want %v", st.Code(), tt.wantErr)
				}
			} else {
				if err != nil {
					t.Fatalf("GetTask() got unexpected error: %v", err)
				}
				if !proto.Equal(resp, tt.want) {
					t.Fatalf("GetTask() got = %v, want %v", resp, tt.want)
				}
				if tt.wantQuery != nil && !reflect.DeepEqual(mockHandler.capturedGetTaskRequest, tt.wantQuery) {
					t.Errorf("OnGetTask() query got = %v, want %v", mockHandler.capturedGetTaskRequest, tt.wantQuery)
				}
			}
		})
	}
}

func TestGrpcHandler_ListTasks(t *testing.T) {
	ctx := t.Context()
	taskID1, taskID2, taskID3 := a2a.NewTaskID(), a2a.NewTaskID(), a2a.NewTaskID()
	mockHandler := &mockRequestHandler{
		tasks: map[a2a.TaskID]*a2a.Task{
			taskID1: {ID: taskID1, ContextID: "test-context1", Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}},
			taskID2: {ID: taskID2, ContextID: "test-context2", Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}},
			taskID3: {ID: taskID3, ContextID: "test-context1", Status: a2a.TaskStatus{State: a2a.TaskStateWorking}},
		},
	}
	client := startTestServer(t, mockHandler)

	tests := []struct {
		name       string
		req        *a2apb.ListTasksRequest
		want       *a2apb.ListTasksResponse
		wantParams *a2a.ListTasksRequest
		wantErr    codes.Code
	}{
		{
			name: "success",
			req:  &a2apb.ListTasksRequest{},
			want: &a2apb.ListTasksResponse{
				Tasks: []*a2apb.Task{
					{Id: string(taskID1), ContextId: "test-context1", Status: &a2apb.TaskStatus{State: a2apb.TaskState_TASK_STATE_SUBMITTED}},
					{Id: string(taskID2), ContextId: "test-context2", Status: &a2apb.TaskStatus{State: a2apb.TaskState_TASK_STATE_COMPLETED}},
					{Id: string(taskID3), ContextId: "test-context1", Status: &a2apb.TaskStatus{State: a2apb.TaskState_TASK_STATE_WORKING}},
				},
				TotalSize:     3,
				NextPageToken: "",
			},
			wantParams: &a2a.ListTasksRequest{
				Status: a2a.TaskStateUnspecified,
			},
		},
		{
			name: "success with context filter",
			req: &a2apb.ListTasksRequest{
				ContextId: "test-context1",
			},
			want: &a2apb.ListTasksResponse{
				Tasks: []*a2apb.Task{
					{Id: string(taskID1), ContextId: "test-context1", Status: &a2apb.TaskStatus{State: a2apb.TaskState_TASK_STATE_SUBMITTED}},
					{Id: string(taskID3), ContextId: "test-context1", Status: &a2apb.TaskStatus{State: a2apb.TaskState_TASK_STATE_WORKING}},
				},
				TotalSize:     2,
				NextPageToken: "",
			},
			wantParams: &a2a.ListTasksRequest{
				ContextID: "test-context1",
				Status:    a2a.TaskStateUnspecified,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockHandler.capturedListTasksRequest = nil
			resp, err := client.ListTasks(ctx, tt.req)
			if tt.wantErr != codes.OK {
				if err == nil {
					t.Fatal("ListTasks() expected error, got nil")
				}
				st, ok := status.FromError(err)
				if !ok {
					t.Fatalf("ListTasks() error is not a gRPC status error: %v", err)
				}
				if st.Code() != tt.wantErr {
					t.Errorf("ListTasks() got error code %v, want %v", st.Code(), tt.wantErr)
				}
			} else {
				if err != nil {
					t.Fatalf("ListTasks() got unexpected error: %v", err)
				}
				if !proto.Equal(resp, tt.want) {
					t.Fatalf("ListTasks() got = %v, want %v", resp, tt.want)
				}
				if tt.wantParams != nil && !reflect.DeepEqual(mockHandler.capturedListTasksRequest, tt.wantParams) {
					t.Errorf("OnListTasks() query got = %v, want %v", mockHandler.capturedListTasksRequest, tt.wantParams)
				}
			}
		})
	}
}

func TestGrpcHandler_CancelTask(t *testing.T) {
	ctx := t.Context()
	taskID := a2a.TaskID("test-task")
	mockHandler := &mockRequestHandler{
		tasks: map[a2a.TaskID]*a2a.Task{
			taskID: {ID: taskID, ContextID: "test-context"},
		},
	}
	client := startTestServer(t, mockHandler)

	tests := []struct {
		name       string
		req        *a2apb.CancelTaskRequest
		want       *a2apb.Task
		wantParams *a2a.CancelTaskRequest
		wantErr    codes.Code
	}{
		{
			name:       "success",
			req:        &a2apb.CancelTaskRequest{Name: fmt.Sprintf("tasks/%s", taskID)},
			want:       &a2apb.Task{Id: string(taskID), ContextId: "test-context", Status: &a2apb.TaskStatus{State: a2apb.TaskState_TASK_STATE_CANCELLED}},
			wantParams: &a2a.CancelTaskRequest{ID: taskID},
		},
		{
			name:    "invalid name",
			req:     &a2apb.CancelTaskRequest{Name: "invalid/name"},
			wantErr: codes.InvalidArgument,
		},
		{
			name:    "handler error",
			req:     &a2apb.CancelTaskRequest{Name: "tasks/handler-error"},
			wantErr: codes.Internal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockHandler.capturedCancelTaskRequest = nil
			resp, err := client.CancelTask(ctx, tt.req)
			if tt.wantErr != codes.OK {
				if err == nil {
					t.Fatal("CancelTask() expected error, got nil")
				}
				st, ok := status.FromError(err)
				if !ok {
					t.Fatalf("CancelTask() error is not a gRPC status error: %v", err)
				}
				if st.Code() != tt.wantErr {
					t.Errorf("CancelTask() got error code %v, want %v", st.Code(), tt.wantErr)
				}
			} else {
				if err != nil {
					t.Fatalf("CancelTask() got unexpected error: %v", err)
				}
				if !proto.Equal(resp, tt.want) {
					t.Fatalf("CancelTask() got = %v, want %v", resp, tt.want)
				}
				if tt.wantParams != nil && !reflect.DeepEqual(mockHandler.capturedCancelTaskRequest, tt.wantParams) {
					t.Errorf("OnCancelTask() params got = %v, want %v", mockHandler.capturedCancelTaskRequest, tt.wantParams)
				}
			}
		})
	}
}

func TestGrpcHandler_SendMessage(t *testing.T) {
	ctx := t.Context()
	mockHandler := defaultMockHandler
	client := startTestServer(t, mockHandler)

	tests := []struct {
		name       string
		req        *a2apb.SendMessageRequest
		want       *a2apb.SendMessageResponse
		wantParams *a2a.SendMessageRequest
		wantErr    codes.Code
	}{
		{
			name: "message sent successfully without config",
			req: &a2apb.SendMessageRequest{
				Request: &a2apb.Message{
					MessageId: "req-msg-123",
					TaskId:    "test-task-123",
					Role:      a2apb.Role_ROLE_USER,
					Parts: []*a2apb.Part{
						{
							Part: &a2apb.Part_Text{Text: "Hello Agent"},
						},
					},
				},
			},
			want: &a2apb.SendMessageResponse{
				Payload: &a2apb.SendMessageResponse_Msg{
					Msg: &a2apb.Message{
						MessageId: "req-msg-123-response",
						TaskId:    "test-task-123",
						Role:      a2apb.Role_ROLE_AGENT,
						Parts: []*a2apb.Part{
							{
								Part: &a2apb.Part_Text{Text: "Hello Agent"},
							},
						},
					},
				},
			},
			wantParams: &a2a.SendMessageRequest{
				Message: &a2a.Message{
					ID:     "req-msg-123",
					TaskID: "test-task-123",
					Role:   a2a.MessageRoleUser,
					Parts:  a2a.ContentParts{a2a.NewTextPart("Hello Agent")},
				},
			},
		},
		{
			name:    "nil request message",
			req:     &a2apb.SendMessageRequest{},
			wantErr: codes.InvalidArgument,
		},
		{
			name: "invalid request",
			req: &a2apb.SendMessageRequest{
				Request: &a2apb.Message{
					Parts: []*a2apb.Part{{Part: nil}},
				},
			},
			wantErr: codes.InvalidArgument,
		},
		{
			name: "handler error",
			req: &a2apb.SendMessageRequest{
				Request: &a2apb.Message{MessageId: "handler-error"},
			},
			wantErr: codes.Internal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockHandler.capturedSendMessageRequest = nil
			resp, err := client.SendMessage(ctx, tt.req)
			if tt.wantErr != codes.OK {
				if err == nil {
					t.Fatal("SendMessage() expected error, got nil")
				}
				st, ok := status.FromError(err)
				if !ok {
					t.Fatalf("SendMessage() error is not a gRPC status error: %v", err)
				}
				if st.Code() != tt.wantErr {
					t.Errorf("SendMessage() got error code %v, want %v", st.Code(), tt.wantErr)
				}
			} else {
				if err != nil {
					t.Fatalf("SendMessage() got unexpected error: %v", err)
				}
				if !proto.Equal(resp, tt.want) {
					t.Fatalf("SendMessage() got = %v, want %v", resp, tt.want)
				}
				if tt.wantParams != nil && !reflect.DeepEqual(mockHandler.capturedSendMessageRequest, tt.wantParams) {
					t.Errorf("OnSendMessage() params got = %+v, want %+v", mockHandler.capturedSendMessageRequest, tt.wantParams)
				}
			}
		})
	}
}

func TestGrpcHandler_SendStreamingMessage(t *testing.T) {
	ctx := t.Context()
	mockHandler := defaultMockHandler
	client := startTestServer(t, mockHandler)

	taskID := a2a.TaskID("stream-task-123")
	msgID := "stream-req-1"
	contextID := "stream-context-abc"
	parts := []*a2apb.Part{
		{Part: &a2apb.Part_Text{Text: "streaming hello"}},
	}

	tests := []struct {
		name       string
		req        *a2apb.SendMessageRequest
		want       []*a2apb.StreamResponse
		wantParams *a2a.SendMessageRequest
		wantErr    codes.Code
	}{
		{
			name: "success",
			req: &a2apb.SendMessageRequest{
				Request: &a2apb.Message{
					MessageId: msgID,
					TaskId:    string(taskID),
					ContextId: contextID,
					Parts:     parts,
					Role:      a2apb.Role_ROLE_USER,
				},
			},
			want: []*a2apb.StreamResponse{
				{
					Payload: &a2apb.StreamResponse_Task{
						Task: &a2apb.Task{
							Id:        string(taskID),
							ContextId: contextID,
							Status:    &a2apb.TaskStatus{State: a2apb.TaskState_TASK_STATE_SUBMITTED},
						},
					},
				},
				{
					Payload: &a2apb.StreamResponse_StatusUpdate{
						StatusUpdate: &a2apb.TaskStatusUpdateEvent{
							TaskId:    string(taskID),
							ContextId: contextID,
							Status:    &a2apb.TaskStatus{State: a2apb.TaskState_TASK_STATE_WORKING},
						},
					},
				},
				{
					Payload: &a2apb.StreamResponse_Msg{
						Msg: &a2apb.Message{
							MessageId: fmt.Sprintf("%s-response", msgID),
							TaskId:    string(taskID),
							Role:      a2apb.Role_ROLE_AGENT,
							Parts:     parts,
						},
					},
				},
			},
			wantParams: &a2a.SendMessageRequest{
				Message: &a2a.Message{
					ID:        msgID,
					TaskID:    taskID,
					ContextID: contextID,
					Role:      a2a.MessageRoleUser,
					Parts:     a2a.ContentParts{a2a.NewTextPart("streaming hello")},
				},
			},
		},
		{
			name:    "nil request message",
			req:     &a2apb.SendMessageRequest{},
			wantErr: codes.InvalidArgument,
		},
		{
			name: "invalid request",
			req: &a2apb.SendMessageRequest{
				Request: &a2apb.Message{
					Parts: []*a2apb.Part{{Part: nil}},
				},
			},
			wantErr: codes.InvalidArgument,
		},
		{
			name: "handler error",
			req: &a2apb.SendMessageRequest{
				Request: &a2apb.Message{MessageId: "handler-error"},
			},
			wantErr: codes.Internal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockHandler.capturedSendMessageRequest = nil
			stream, err := client.SendStreamingMessage(ctx, tt.req)
			if err != nil {
				t.Fatalf("SendStreamingMessage() got unexpected error on client setup: %v", err)
			}

			var received []*a2apb.StreamResponse
			for {
				resp, err := stream.Recv()
				if err == io.EOF {
					break
				}
				if err != nil {
					if tt.wantErr != codes.OK {
						st, ok := status.FromError(err)
						if !ok {
							t.Fatalf("stream.Recv() error is not a gRPC status error: %v", err)
						}
						if st.Code() != tt.wantErr {
							t.Errorf("stream.Recv() got error code %v, want %v", st.Code(), tt.wantErr)
						}
					} else {
						t.Fatalf("stream.Recv() got unexpected error: %v", err)
					}
					return
				}
				received = append(received, resp)
			}

			if tt.wantErr != codes.OK {
				t.Fatal("SendStreamingMessage() expected error, but stream completed successfully")
			}

			if len(received) != len(tt.want) {
				t.Fatalf("SendStreamingMessage() received %d events, want %d", len(received), len(tt.want))
			}

			if tt.wantParams != nil && !reflect.DeepEqual(mockHandler.capturedSendMessageStreamRequest, tt.wantParams) {
				t.Errorf("SendStreamingMessage() params got = %+v, want %+v", mockHandler.capturedSendMessageStreamRequest, tt.wantParams)
			}

			for i, wantResp := range tt.want {
				// ignoring timestamp for want/got check
				if r, ok := received[i].GetPayload().(*a2apb.StreamResponse_StatusUpdate); ok {
					if r.StatusUpdate.GetStatus() != nil {
						r.StatusUpdate.Status.Timestamp = nil
					}
				}
				if !proto.Equal(received[i], wantResp) {
					t.Errorf("SendStreamingMessage() event %d got = %v, want %v", i, received[i], wantResp)
				}
			}
		})
	}
}

func TestGrpcHandler_TaskSubscription(t *testing.T) {
	ctx := t.Context()
	mockHandler := defaultMockHandler
	client := startTestServer(t, mockHandler)
	taskID := a2a.TaskID("resub-task-456")
	tests := []struct {
		name        string
		req         *a2apb.TaskSubscriptionRequest
		want        []*a2apb.StreamResponse
		wantRequest *a2a.SubscribeToTaskRequest
		wantErr     codes.Code
	}{
		{
			name: "success",
			req:  &a2apb.TaskSubscriptionRequest{Name: fmt.Sprintf("tasks/%s", taskID)},
			want: []*a2apb.StreamResponse{
				{
					Payload: &a2apb.StreamResponse_Task{
						Task: &a2apb.Task{
							Id:        string(taskID),
							ContextId: "resubscribe-context",
							Status:    &a2apb.TaskStatus{State: a2apb.TaskState_TASK_STATE_WORKING},
						},
					},
				},
				{
					Payload: &a2apb.StreamResponse_StatusUpdate{
						StatusUpdate: &a2apb.TaskStatusUpdateEvent{
							TaskId:    string(taskID),
							ContextId: "resubscribe-context",
							Status:    &a2apb.TaskStatus{State: a2apb.TaskState_TASK_STATE_COMPLETED},
						},
					},
				},
			},
			wantRequest: &a2a.SubscribeToTaskRequest{ID: taskID},
		},
		{
			name:    "invalid name",
			req:     &a2apb.TaskSubscriptionRequest{Name: "invalid/name"},
			wantErr: codes.InvalidArgument,
		},
		{
			name:    "handler error",
			req:     &a2apb.TaskSubscriptionRequest{Name: "tasks/handler-error"},
			wantErr: codes.Internal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockHandler.capturedSubscribeToTaskRequest = nil
			stream, err := client.TaskSubscription(ctx, tt.req)
			if err != nil {
				t.Fatalf("TaskSubscription() got unexpected error on client setup: %v", err)
			}

			var received []*a2apb.StreamResponse
			for {
				resp, err := stream.Recv()
				if err == io.EOF {
					break
				}
				if err != nil {
					if tt.wantErr != codes.OK {
						st, ok := status.FromError(err)
						if !ok {
							t.Fatalf("stream.Recv() error is not a gRPC status error: %v", err)
						}
						if st.Code() != tt.wantErr {
							t.Errorf("stream.Recv() got error code %v, want %v", st.Code(), tt.wantErr)
						}
					} else {
						t.Fatalf("stream.Recv() got unexpected error: %v", err)
					}
					return
				}
				received = append(received, resp)
			}

			if tt.wantErr != codes.OK {
				t.Fatal("TaskSubscription() expected error, but stream completed successfully")
			}

			if len(received) != len(tt.want) {
				t.Fatalf("TaskSubscription() received %d events, want %d", len(received), len(tt.want))
			}

			if tt.wantRequest != nil && !reflect.DeepEqual(mockHandler.capturedSubscribeToTaskRequest, tt.wantRequest) {
				t.Errorf("OnResubscribeToTask() params got = %v, want %v", mockHandler.capturedSubscribeToTaskRequest, tt.wantRequest)
			}

			for i, wantResp := range tt.want {
				// ignoring timestamp for want/got check
				if r, ok := received[i].GetPayload().(*a2apb.StreamResponse_StatusUpdate); ok {
					if r.StatusUpdate.GetStatus() != nil {
						r.StatusUpdate.Status.Timestamp = nil
					}
				}
				if !proto.Equal(received[i], wantResp) {
					t.Errorf("TaskSubscription() event %d got = %v, want %v", i, received[i], wantResp)
				}
			}
		})
	}
}

func TestGrpcHandler_CreateTaskPushNotificationConfig(t *testing.T) {
	ctx := t.Context()
	taskID := a2a.TaskID("test-task")
	mockHandler := &mockRequestHandler{
		pushConfigs: make(map[a2a.TaskID]map[string]*a2a.TaskPushConfig),
		tasks: map[a2a.TaskID]*a2a.Task{
			taskID: {ID: taskID, ContextID: "test-context"},
		},
	}
	client := startTestServer(t, mockHandler)

	tests := []struct {
		name        string
		req         *a2apb.CreateTaskPushNotificationConfigRequest
		want        *a2apb.TaskPushNotificationConfig
		wantRequest *a2a.CreateTaskPushConfigRequest
		wantErr     codes.Code
	}{
		{
			name: "success",
			req: &a2apb.CreateTaskPushNotificationConfigRequest{
				Parent: fmt.Sprintf("tasks/%s", taskID),
				Config: &a2apb.TaskPushNotificationConfig{
					PushNotificationConfig: &a2apb.PushNotificationConfig{Id: "test-config"},
				},
			},
			want: &a2apb.TaskPushNotificationConfig{
				Name:                   fmt.Sprintf("tasks/%s/pushNotificationConfigs/test-config", taskID),
				PushNotificationConfig: &a2apb.PushNotificationConfig{Id: "test-config"},
			},
			wantRequest: &a2a.CreateTaskPushConfigRequest{
				TaskID: taskID,
				Config: a2a.PushConfig{ID: "test-config"},
			},
		},
		{
			name:    "invalid request",
			req:     &a2apb.CreateTaskPushNotificationConfigRequest{Parent: "invalid/parent"},
			wantErr: codes.InvalidArgument,
		},
		{
			name: "handler error",
			req: &a2apb.CreateTaskPushNotificationConfigRequest{
				Parent: "tasks/handler-error",
				Config: &a2apb.TaskPushNotificationConfig{
					PushNotificationConfig: &a2apb.PushNotificationConfig{Id: "test-config"},
				},
			},
			wantErr: codes.Internal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockHandler.capturedCreateTaskPushConfigRequest = nil
			resp, err := client.CreateTaskPushNotificationConfig(ctx, tt.req)
			if tt.wantErr != codes.OK {
				if err == nil {
					t.Fatal("CreateTaskPushNotificationConfig() expected error, got nil")
				}
				st, ok := status.FromError(err)
				if !ok {
					t.Fatalf("CreateTaskPushNotificationConfig() error is not a gRPC status error: %v", err)
				}
				if st.Code() != tt.wantErr {
					t.Errorf("CreateTaskPushNotificationConfig() got error code %v, want %v", st.Code(), tt.wantErr)
				}
			} else {
				if err != nil {
					t.Fatalf("CreateTaskPushNotificationConfig() got unexpected error: %v", err)
				}
				if !proto.Equal(resp, tt.want) {
					t.Fatalf("CreateTaskPushNotificationConfig() got = %v, want %v", resp, tt.want)
				}
				if tt.wantRequest != nil && !reflect.DeepEqual(mockHandler.capturedCreateTaskPushConfigRequest, tt.wantRequest) {
					t.Errorf("OnCreateTaskPushNotificationConfig() request got = %v, want %v", mockHandler.capturedCreateTaskPushConfigRequest, tt.wantRequest)
				}
			}
		})
	}
}

func TestGrpcHandler_GetTaskPushNotificationConfig(t *testing.T) {
	ctx := t.Context()
	taskID := a2a.TaskID("test-task")
	configID := "test-config"
	mockHandler := &mockRequestHandler{
		pushConfigs: map[a2a.TaskID]map[string]*a2a.TaskPushConfig{
			taskID: {
				configID: {TaskID: taskID, Config: a2a.PushConfig{ID: configID}},
			},
		},
		tasks: map[a2a.TaskID]*a2a.Task{
			taskID: {ID: taskID, ContextID: "test-context"},
		},
	}
	client := startTestServer(t, mockHandler)

	tests := []struct {
		name       string
		req        *a2apb.GetTaskPushNotificationConfigRequest
		want       *a2apb.TaskPushNotificationConfig
		wantParams *a2a.GetTaskPushConfigRequest
		wantErr    codes.Code
	}{
		{
			name: "success",
			req: &a2apb.GetTaskPushNotificationConfigRequest{
				Name: fmt.Sprintf("tasks/%s/pushNotificationConfigs/%s", taskID, configID),
			},
			want: &a2apb.TaskPushNotificationConfig{
				Name:                   fmt.Sprintf("tasks/%s/pushNotificationConfigs/%s", taskID, configID),
				PushNotificationConfig: &a2apb.PushNotificationConfig{Id: configID},
			},
			wantParams: &a2a.GetTaskPushConfigRequest{
				TaskID: taskID,
				ID:     configID,
			},
		},
		{
			name:    "invalid request",
			req:     &a2apb.GetTaskPushNotificationConfigRequest{Name: "tasks/test-task/invalid/test-config"},
			wantErr: codes.InvalidArgument,
		},
		{
			name:    "handler error",
			req:     &a2apb.GetTaskPushNotificationConfigRequest{Name: "tasks/handler-error/pushNotificationConfigs/test-config"},
			wantErr: codes.Internal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockHandler.capturedGetTaskPushConfigRequest = nil
			resp, err := client.GetTaskPushNotificationConfig(ctx, tt.req)
			if tt.wantErr != codes.OK {
				if err == nil {
					t.Fatal("GetTaskPushNotificationConfig() expected error, got nil")
				}
				st, ok := status.FromError(err)
				if !ok {
					t.Fatalf("GetTaskPushNotificationConfig() error is not a gRPC status error: %v", err)
				}
				if st.Code() != tt.wantErr {
					t.Errorf("GetTaskPushNotificationConfig() got error code %v, want %v", st.Code(), tt.wantErr)
				}
			} else {
				if err != nil {
					t.Fatalf("GetTaskPushNotificationConfig() got unexpected error: %v", err)
				}
				if !proto.Equal(resp, tt.want) {
					t.Fatalf("GetTaskPushNotificationConfig() got = %v, want %v", resp, tt.want)
				}
				if tt.wantParams != nil && !reflect.DeepEqual(mockHandler.capturedGetTaskPushConfigRequest, tt.wantParams) {
					t.Errorf("OnGetTaskPushConfig() request got = %v, want %v", mockHandler.capturedGetTaskPushConfigRequest, tt.wantParams)
				}
			}
		})
	}
}

func TestGrpcHandler_ListTaskPushNotificationConfig(t *testing.T) {
	ctx := t.Context()
	taskID := a2a.TaskID("test-task")
	configID := "test-config"
	mockHandler := &mockRequestHandler{
		pushConfigs: map[a2a.TaskID]map[string]*a2a.TaskPushConfig{
			taskID: {
				fmt.Sprintf("%s-1", configID): {TaskID: taskID, Config: a2a.PushConfig{ID: fmt.Sprintf("%s-1", configID)}},
				fmt.Sprintf("%s-2", configID): {TaskID: taskID, Config: a2a.PushConfig{ID: fmt.Sprintf("%s-2", configID)}},
			},
		},
		tasks: map[a2a.TaskID]*a2a.Task{
			taskID: {ID: taskID, ContextID: "test-context"},
		},
	}
	client := startTestServer(t, mockHandler)

	tests := []struct {
		name       string
		req        *a2apb.ListTaskPushNotificationConfigRequest
		want       *a2apb.ListTaskPushNotificationConfigResponse
		wantParams *a2a.ListTaskPushConfigRequest
		wantErr    codes.Code
	}{
		{
			name: "success",
			req:  &a2apb.ListTaskPushNotificationConfigRequest{Parent: fmt.Sprintf("tasks/%s", taskID)},
			want: &a2apb.ListTaskPushNotificationConfigResponse{
				Configs: []*a2apb.TaskPushNotificationConfig{
					{
						Name:                   fmt.Sprintf("tasks/%s/pushNotificationConfigs/%s-1", taskID, configID),
						PushNotificationConfig: &a2apb.PushNotificationConfig{Id: fmt.Sprintf("%s-1", configID)},
					},
					{
						Name:                   fmt.Sprintf("tasks/%s/pushNotificationConfigs/%s-2", taskID, configID),
						PushNotificationConfig: &a2apb.PushNotificationConfig{Id: fmt.Sprintf("%s-2", configID)},
					},
				},
			},
			wantParams: &a2a.ListTaskPushConfigRequest{TaskID: taskID},
		},
		{
			name:    "invalid parent",
			req:     &a2apb.ListTaskPushNotificationConfigRequest{Parent: "invalid/parent"},
			wantErr: codes.InvalidArgument,
		},
		{
			name:    "handler error",
			req:     &a2apb.ListTaskPushNotificationConfigRequest{Parent: "tasks/handler-error"},
			wantErr: codes.Internal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockHandler.capturedListTaskPushConfigRequest = nil
			resp, err := client.ListTaskPushNotificationConfig(ctx, tt.req)
			if tt.wantErr != codes.OK {
				if err == nil {
					t.Fatal("ListTaskPushNotificationConfig() expected error, got nil")
				}
				st, ok := status.FromError(err)
				if !ok {
					t.Fatalf("ListTaskPushNotificationConfig() error is not a gRPC status error: %v", err)
				}
				if st.Code() != tt.wantErr {
					t.Errorf("ListTaskPushNotificationConfig() got error code %v, want %v", st.Code(), tt.wantErr)
				}
			} else {
				if err != nil {
					t.Fatalf("ListTaskPushNotificationConfig() got unexpected error: %v", err)
				}
				sort.Slice(resp.Configs, func(i, j int) bool {
					return resp.Configs[i].Name < resp.Configs[j].Name
				})
				sort.Slice(tt.want.Configs, func(i, j int) bool {
					return tt.want.Configs[i].Name < tt.want.Configs[j].Name
				})
				if !proto.Equal(resp, tt.want) {
					t.Fatalf("ListTaskPushNotificationConfig() got = %v, want %v", resp, tt.want)
				}
				if tt.wantParams != nil && !reflect.DeepEqual(mockHandler.capturedListTaskPushConfigRequest, tt.wantParams) {
					t.Errorf("OnListTaskPushConfig() request got = %v, want %v", mockHandler.capturedListTaskPushConfigRequest, tt.wantParams)
				}
			}
		})
	}
}

func TestGrpcHandler_DeleteTaskPushNotificationConfig(t *testing.T) {
	ctx := t.Context()
	taskID := a2a.TaskID("test-task")
	configID := "test-config"
	mockHandler := &mockRequestHandler{
		pushConfigs: map[a2a.TaskID]map[string]*a2a.TaskPushConfig{
			taskID: {
				configID: {TaskID: taskID, Config: a2a.PushConfig{ID: configID}},
			},
		},
		tasks: map[a2a.TaskID]*a2a.Task{
			taskID: {ID: taskID, ContextID: "test-context"},
		},
	}
	client := startTestServer(t, mockHandler)

	tests := []struct {
		name       string
		req        *a2apb.DeleteTaskPushNotificationConfigRequest
		want       *emptypb.Empty
		wantParams *a2a.DeleteTaskPushConfigRequest
		wantErr    codes.Code
	}{
		{
			name: "success",
			req: &a2apb.DeleteTaskPushNotificationConfigRequest{
				Name: fmt.Sprintf("tasks/%s/pushNotificationConfigs/%s", taskID, configID),
			},
			want: &emptypb.Empty{},
			wantParams: &a2a.DeleteTaskPushConfigRequest{
				TaskID: taskID,
				ID:     configID,
			},
		},
		{
			name: "invalid request",
			req: &a2apb.DeleteTaskPushNotificationConfigRequest{
				Name: fmt.Sprintf("tasks/%s/invalid/%s", taskID, configID),
			},
			wantErr: codes.InvalidArgument,
		},
		{
			name: "handler error",
			req: &a2apb.DeleteTaskPushNotificationConfigRequest{
				Name: fmt.Sprintf("tasks/handler-error/pushNotificationConfigs/%s", configID),
			},
			wantErr: codes.Internal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockHandler.capturedDeleteTaskPushConfigRequest = nil
			resp, err := client.DeleteTaskPushNotificationConfig(ctx, tt.req)
			if tt.wantErr != codes.OK {
				if err == nil {
					t.Fatal("DeleteTaskPushNotificationConfig() expected error, got nil")
				}
				st, ok := status.FromError(err)
				if !ok {
					t.Fatalf("DeleteTaskPushNotificationConfig() error is not a gRPC status error: %v", err)
				}
				if st.Code() != tt.wantErr {
					t.Errorf("DeleteTaskPushNotificationConfig() got error code %v, want %v", st.Code(), tt.wantErr)
				}
			} else {
				if err != nil {
					t.Fatalf("DeleteTaskPushNotificationConfig() got unexpected error: %v", err)
				}
				if !proto.Equal(resp, tt.want) {
					t.Fatalf("DeleteTaskPushNotificationConfig() got = %v, want %v", resp, tt.want)
				}
				if tt.wantParams != nil && !reflect.DeepEqual(mockHandler.capturedDeleteTaskPushConfigRequest, tt.wantParams) {
					t.Errorf("OnDeleteTaskPushConfig() request got = %v, want %v", mockHandler.capturedDeleteTaskPushConfigRequest, tt.wantParams)
				}
			}
		})
	}
}

func TestGrpcHandler_GetAgentCard(t *testing.T) {
	ctx := t.Context()

	a2aCard := &a2a.AgentCard{Name: "Test Agent", SupportedInterfaces: []*a2a.AgentInterface{{ProtocolVersion: a2a.Version}}}
	pCard, err := pbconv.ToProtoAgentCard(a2aCard)
	if err != nil {
		t.Fatalf("failed to convert agent card for test setup: %v", err)
	}

	badCard := &a2a.AgentCard{
		Capabilities: a2a.AgentCapabilities{
			Extensions: []a2a.AgentExtension{{Params: map[string]any{"bad": func() {}}}},
		},
	}

	tests := []struct {
		name         string
		cardProducer a2asrv.AgentCardProducer
		want         *a2apb.AgentCard
		wantErr      codes.Code
	}{
		{
			name: "success",
			cardProducer: a2asrv.AgentCardProducerFn(func(context.Context) (*a2a.AgentCard, error) {
				return a2aCard, nil
			}),
			want: pCard,
		},
		{
			name:         "nil producer",
			cardProducer: nil,
			wantErr:      codes.NotFound,
		},
		{
			name: "producer returns nil card",
			cardProducer: a2asrv.AgentCardProducerFn(func(context.Context) (*a2a.AgentCard, error) {
				return nil, nil
			}),
			want: &a2apb.AgentCard{},
		},
		{
			name: "producer fails",
			cardProducer: a2asrv.AgentCardProducerFn(func(context.Context) (*a2a.AgentCard, error) {
				return badCard, nil
			}),
			wantErr: codes.Internal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := a2asrv.NewHandler(nil, a2asrv.WithExtendedAgentCardProducer(tt.cardProducer))
			client := startTestServer(t, handler)
			resp, err := client.GetAgentCard(ctx, &a2apb.GetAgentCardRequest{})
			if tt.wantErr != codes.OK {
				if err == nil {
					t.Fatal("GetAgentCard() expected error, got nil")
				}
				st, ok := status.FromError(err)
				if !ok {
					t.Fatalf("GetAgentCard() error is not a gRPC status error: %v", err)
				}
				if st.Code() != tt.wantErr {
					t.Errorf("GetAgentCard() got error code %v, want %v", st.Code(), tt.wantErr)
				}
			} else {
				if err != nil {
					t.Fatalf("GetAgentCard() got unexpected error: %v", err)
				}
				if !proto.Equal(resp, tt.want) {
					t.Fatalf("GetAgentCard() got = %v, want %v", resp, tt.want)
				}
			}
		})
	}
}
