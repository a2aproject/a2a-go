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
	"github.com/a2aproject/a2a-go/a2apb"
	"github.com/a2aproject/a2a-go/a2apb/pbconv"
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
	OnSendMessageFunc: func(ctx context.Context, params *a2a.MessageSendParams) (a2a.SendMessageResult, error) {
		if params.Message.ID == "handler-error" {
			return nil, errors.New("handler error")
		}
		taskID := params.Message.TaskID
		if taskID == "" {
			taskID = a2a.NewTaskID()
		}
		return &a2a.Message{
			ID:     fmt.Sprintf("%s-response", params.Message.ID),
			TaskID: taskID,
			Role:   a2a.MessageRoleAgent,
			Parts:  params.Message.Parts, // Echo back the parts
		}, nil
	},

	OnSendMessageStreamFunc: func(ctx context.Context, params *a2a.MessageSendParams) iter.Seq2[a2a.Event, error] {
		if params.Message.ID == "handler-error" {
			return func(yield func(a2a.Event, error) bool) {
				yield(nil, errors.New("handler stream error"))
			}
		}

		taskID := params.Message.TaskID
		if taskID == "" {
			taskID = a2a.NewTaskID()
		}

		task := &a2a.Task{
			ID:        taskID,
			ContextID: params.Message.ContextID,
			Status:    a2a.TaskStatus{State: a2a.TaskStateSubmitted},
		}
		statusUpdate := a2a.NewStatusUpdateEvent(task, a2a.TaskStateWorking, nil)
		finalMessage := &a2a.Message{
			ID:     fmt.Sprintf("%s-response", params.Message.ID),
			TaskID: taskID,
			Role:   a2a.MessageRoleAgent,
			Parts:  params.Message.Parts, // Echo back the parts
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

	OnResubscribeToTaskFunc: func(ctx context.Context, id *a2a.TaskIDParams) iter.Seq2[a2a.Event, error] {
		if id.ID == "handler-error" {
			return func(yield func(a2a.Event, error) bool) {
				yield(nil, errors.New("handler resubscribe error"))
			}
		}
		task := &a2a.Task{
			ID:        id.ID,
			ContextID: "resubscribe-context",
			Status:    a2a.TaskStatus{State: a2a.TaskStateWorking},
		}
		statusUpdate := a2a.NewStatusUpdateEvent(task, a2a.TaskStateCompleted, nil)
		statusUpdate.Final = true
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
	capturedGetTaskQuery               *a2a.TaskQueryParams
	capturedCancelTaskIDParams         *a2a.TaskIDParams
	capturedSendMessageParams          *a2a.MessageSendParams
	capturedSendMessageStreamParams    *a2a.MessageSendParams
	capturedResubscribeToTaskIDParams  *a2a.TaskIDParams
	capturedSetTaskPushConfig          *a2a.TaskPushConfig
	capturedGetTaskPushConfigParams    *a2a.GetTaskPushConfigParams
	capturedListTaskPushConfigParams   *a2a.ListTaskPushConfigParams
	capturedDeleteTaskPushConfigParams *a2a.DeleteTaskPushConfigParams

	// Override specific methods
	a2asrv.RequestHandler
	OnSendMessageFunc       func(ctx context.Context, message *a2a.MessageSendParams) (a2a.SendMessageResult, error)
	OnSendMessageStreamFunc func(ctx context.Context, params *a2a.MessageSendParams) iter.Seq2[a2a.Event, error]
	OnResubscribeToTaskFunc func(ctx context.Context, id *a2a.TaskIDParams) iter.Seq2[a2a.Event, error]
}

func (m *mockRequestHandler) OnGetTask(ctx context.Context, query *a2a.TaskQueryParams) (*a2a.Task, error) {
	m.capturedGetTaskQuery = query
	if task, ok := m.tasks[query.ID]; ok {
		if query.HistoryLength != nil && *query.HistoryLength > 0 {
			if len(task.History) > int(*query.HistoryLength) {
				task.History = task.History[len(task.History)-int(*query.HistoryLength):]
			}
		}
		return task, nil
	}

	return nil, fmt.Errorf("task not found, taskID: %s", query.ID)
}

func (m *mockRequestHandler) OnCancelTask(ctx context.Context, id *a2a.TaskIDParams) (*a2a.Task, error) {
	m.capturedCancelTaskIDParams = id
	if task, ok := m.tasks[id.ID]; ok {
		task.Status = a2a.TaskStatus{
			State: a2a.TaskStateCanceled,
		}
		m.tasks[id.ID] = task
		return task, nil
	}
	return nil, fmt.Errorf("task not found, taskID: %s", id.ID)
}

func (m *mockRequestHandler) OnSendMessage(ctx context.Context, message *a2a.MessageSendParams) (a2a.SendMessageResult, error) {
	m.capturedSendMessageParams = message
	if m.OnSendMessageFunc != nil {
		return m.OnSendMessageFunc(ctx, message)
	}
	return nil, errors.New("OnSendMessage not implemented")
}

func (m *mockRequestHandler) OnSendMessageStream(ctx context.Context, params *a2a.MessageSendParams) iter.Seq2[a2a.Event, error] {
	m.capturedSendMessageStreamParams = params
	if m.OnSendMessageStreamFunc != nil {
		return m.OnSendMessageStreamFunc(ctx, params)
	}
	return func(yield func(a2a.Event, error) bool) {
		yield(nil, errors.New("OnSendMessageStream not implemented"))
	}
}

func (m *mockRequestHandler) OnResubscribeToTask(ctx context.Context, id *a2a.TaskIDParams) iter.Seq2[a2a.Event, error] {
	m.capturedResubscribeToTaskIDParams = id
	if m.OnResubscribeToTaskFunc != nil {
		return m.OnResubscribeToTaskFunc(ctx, id)
	}
	return func(yield func(a2a.Event, error) bool) {
		yield(nil, errors.New("OnResubscribeToTask not implemented"))
	}
}

func (m *mockRequestHandler) OnSetTaskPushConfig(ctx context.Context, params *a2a.TaskPushConfig) (*a2a.TaskPushConfig, error) {
	m.capturedSetTaskPushConfig = params
	if _, ok := m.tasks[params.TaskID]; ok {
		if _, ok := m.pushConfigs[params.TaskID]; !ok {
			m.pushConfigs[params.TaskID] = make(map[string]*a2a.TaskPushConfig)
		}
		m.pushConfigs[params.TaskID][params.Config.ID] = params
		return params, nil
	}

	return nil, fmt.Errorf("task for push config not found, taskID: %s", params.TaskID)
}

func (m *mockRequestHandler) OnGetTaskPushConfig(ctx context.Context, params *a2a.GetTaskPushConfigParams) (*a2a.TaskPushConfig, error) {
	m.capturedGetTaskPushConfigParams = params
	if _, ok := m.tasks[params.TaskID]; ok {
		if pushConfigs, ok := m.pushConfigs[params.TaskID]; ok {
			return pushConfigs[params.ConfigID], nil
		}
		return nil, fmt.Errorf("push config not found, taskID: %s, configID: %s", params.TaskID, params.ConfigID)
	}

	return nil, fmt.Errorf("task for push config not found, taskID: %s", params.TaskID)
}

func (m *mockRequestHandler) OnListTaskPushConfig(ctx context.Context, params *a2a.ListTaskPushConfigParams) ([]*a2a.TaskPushConfig, error) {
	m.capturedListTaskPushConfigParams = params
	if _, ok := m.tasks[params.TaskID]; ok {
		if pushConfigs, ok := m.pushConfigs[params.TaskID]; ok {
			var result []*a2a.TaskPushConfig
			for _, v := range pushConfigs {
				result = append(result, v)
			}
			return result, nil
		}
		return []*a2a.TaskPushConfig{}, nil // no configs for task id
	}

	return []*a2a.TaskPushConfig{}, fmt.Errorf("task for push config not found, taskID: %s", params.TaskID)
}

func (m *mockRequestHandler) OnDeleteTaskPushConfig(ctx context.Context, params *a2a.DeleteTaskPushConfigParams) error {
	m.capturedDeleteTaskPushConfigParams = params
	if _, ok := m.tasks[params.TaskID]; ok {
		if pushConfigs, ok := m.pushConfigs[params.TaskID]; ok {
			if _, ok := pushConfigs[params.ConfigID]; ok {
				delete(pushConfigs, params.ConfigID)
				return nil
			}
		}
		return nil
	}

	return fmt.Errorf("task for push config not found, taskID: %s", params.TaskID)
}

type mockAgentCardProducer struct {
	card *a2a.AgentCard
}

func (m *mockAgentCardProducer) Card() *a2a.AgentCard {
	return m.card
}

var defaultMockCardProducer = &mockAgentCardProducer{
	card: nil,
}

func startTestServer(t *testing.T, handler a2asrv.RequestHandler, cardProducer a2asrv.AgentCardProducer) a2apb.A2AServiceClient {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)
	s := grpc.NewServer()
	grpcHandler := NewHandler(cardProducer, handler)
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
	client := startTestServer(t, mockHandler, defaultMockCardProducer)

	tests := []struct {
		name      string
		req       *a2apb.GetTaskRequest
		want      *a2apb.Task
		wantQuery *a2a.TaskQueryParams
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
			wantQuery: &a2a.TaskQueryParams{ID: taskID},
		},
		{
			name: "success with history",
			req:  &a2apb.GetTaskRequest{Name: fmt.Sprintf("tasks/%s", taskID), HistoryLength: 10},
			want: &a2apb.Task{
				Id:        string(taskID),
				ContextId: "test-context",
				Status:    &a2apb.TaskStatus{State: a2apb.TaskState_TASK_STATE_SUBMITTED},
			},
			wantQuery: &a2a.TaskQueryParams{ID: taskID, HistoryLength: &historyLen},
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
			mockHandler.capturedGetTaskQuery = nil
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
				if tt.wantQuery != nil && !reflect.DeepEqual(mockHandler.capturedGetTaskQuery, tt.wantQuery) {
					t.Errorf("OnGetTask() query got = %v, want %v", mockHandler.capturedGetTaskQuery, tt.wantQuery)
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
	client := startTestServer(t, mockHandler, defaultMockCardProducer)

	tests := []struct {
		name       string
		req        *a2apb.CancelTaskRequest
		want       *a2apb.Task
		wantParams *a2a.TaskIDParams
		wantErr    codes.Code
	}{
		{
			name:       "success",
			req:        &a2apb.CancelTaskRequest{Name: fmt.Sprintf("tasks/%s", taskID)},
			want:       &a2apb.Task{Id: string(taskID), ContextId: "test-context", Status: &a2apb.TaskStatus{State: a2apb.TaskState_TASK_STATE_CANCELLED}},
			wantParams: &a2a.TaskIDParams{ID: taskID},
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
			mockHandler.capturedCancelTaskIDParams = nil
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
				if tt.wantParams != nil && !reflect.DeepEqual(mockHandler.capturedCancelTaskIDParams, tt.wantParams) {
					t.Errorf("OnCancelTask() params got = %v, want %v", mockHandler.capturedCancelTaskIDParams, tt.wantParams)
				}
			}
		})
	}
}

func TestGrpcHandler_SendMessage(t *testing.T) {
	ctx := t.Context()
	mockHandler := defaultMockHandler
	client := startTestServer(t, mockHandler, defaultMockCardProducer)

	tests := []struct {
		name       string
		req        *a2apb.SendMessageRequest
		want       *a2apb.SendMessageResponse
		wantParams *a2a.MessageSendParams
		wantErr    codes.Code
	}{
		{
			name: "message sent successfully without config",
			req: &a2apb.SendMessageRequest{
				Request: &a2apb.Message{
					MessageId: "req-msg-123",
					TaskId:    "test-task-123",
					Role:      a2apb.Role_ROLE_USER,
					Content: []*a2apb.Part{
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
						Content: []*a2apb.Part{
							{
								Part: &a2apb.Part_Text{Text: "Hello Agent"},
							},
						},
					},
				},
			},
			wantParams: &a2a.MessageSendParams{
				Message: &a2a.Message{
					ID:     "req-msg-123",
					TaskID: "test-task-123",
					Role:   a2a.MessageRoleUser,
					Parts:  []a2a.Part{a2a.TextPart{Text: "Hello Agent"}},
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
					Content: []*a2apb.Part{{Part: nil}},
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
			mockHandler.capturedSendMessageParams = nil
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
				if tt.wantParams != nil && !reflect.DeepEqual(mockHandler.capturedSendMessageParams, tt.wantParams) {
					t.Errorf("OnSendMessage() params got = %+v, want %+v", mockHandler.capturedSendMessageParams, tt.wantParams)
				}
			}
		})
	}
}

func TestGrpcHandler_SendStreamingMessage(t *testing.T) {
	ctx := t.Context()
	mockHandler := defaultMockHandler
	client := startTestServer(t, mockHandler, defaultMockCardProducer)

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
		wantParams *a2a.MessageSendParams
		wantErr    codes.Code
	}{
		{
			name: "success",
			req: &a2apb.SendMessageRequest{
				Request: &a2apb.Message{
					MessageId: msgID,
					TaskId:    string(taskID),
					ContextId: contextID,
					Content:   parts,
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
							Content:   parts,
						},
					},
				},
			},
			wantParams: &a2a.MessageSendParams{
				Message: &a2a.Message{
					ID:        msgID,
					TaskID:    taskID,
					ContextID: contextID,
					Role:      a2a.MessageRoleUser,
					Parts:     []a2a.Part{a2a.TextPart{Text: "streaming hello"}},
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
					Content: []*a2apb.Part{{Part: nil}},
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
			mockHandler.capturedSendMessageStreamParams = nil
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

			if tt.wantParams != nil && !reflect.DeepEqual(mockHandler.capturedSendMessageStreamParams, tt.wantParams) {
				t.Errorf("OnSendMessageStream() params got = %+v, want %+v", mockHandler.capturedSendMessageStreamParams, tt.wantParams)
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
	client := startTestServer(t, mockHandler, defaultMockCardProducer)
	taskID := a2a.TaskID("resub-task-456")
	tests := []struct {
		name       string
		req        *a2apb.TaskSubscriptionRequest
		want       []*a2apb.StreamResponse
		wantParams *a2a.TaskIDParams
		wantErr    codes.Code
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
							Final:     true,
							Status:    &a2apb.TaskStatus{State: a2apb.TaskState_TASK_STATE_COMPLETED},
						},
					},
				},
			},
			wantParams: &a2a.TaskIDParams{ID: taskID},
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
			mockHandler.capturedResubscribeToTaskIDParams = nil
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

			if tt.wantParams != nil && !reflect.DeepEqual(mockHandler.capturedResubscribeToTaskIDParams, tt.wantParams) {
				t.Errorf("OnResubscribeToTask() params got = %v, want %v", mockHandler.capturedResubscribeToTaskIDParams, tt.wantParams)
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
	client := startTestServer(t, mockHandler, defaultMockCardProducer)

	tests := []struct {
		name       string
		req        *a2apb.CreateTaskPushNotificationConfigRequest
		want       *a2apb.TaskPushNotificationConfig
		wantParams *a2a.TaskPushConfig
		wantErr    codes.Code
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
			wantParams: &a2a.TaskPushConfig{
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
			mockHandler.capturedSetTaskPushConfig = nil
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
				if tt.wantParams != nil && !reflect.DeepEqual(mockHandler.capturedSetTaskPushConfig, tt.wantParams) {
					t.Errorf("OnSetTaskPushConfig() params got = %v, want %v", mockHandler.capturedSetTaskPushConfig, tt.wantParams)
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
	client := startTestServer(t, mockHandler, defaultMockCardProducer)

	tests := []struct {
		name       string
		req        *a2apb.GetTaskPushNotificationConfigRequest
		want       *a2apb.TaskPushNotificationConfig
		wantParams *a2a.GetTaskPushConfigParams
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
			wantParams: &a2a.GetTaskPushConfigParams{
				TaskID:   taskID,
				ConfigID: configID,
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
			mockHandler.capturedGetTaskPushConfigParams = nil
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
				if tt.wantParams != nil && !reflect.DeepEqual(mockHandler.capturedGetTaskPushConfigParams, tt.wantParams) {
					t.Errorf("OnGetTaskPushConfig() params got = %v, want %v", mockHandler.capturedGetTaskPushConfigParams, tt.wantParams)
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
	client := startTestServer(t, mockHandler, defaultMockCardProducer)

	tests := []struct {
		name       string
		req        *a2apb.ListTaskPushNotificationConfigRequest
		want       *a2apb.ListTaskPushNotificationConfigResponse
		wantParams *a2a.ListTaskPushConfigParams
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
			wantParams: &a2a.ListTaskPushConfigParams{TaskID: taskID},
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
			mockHandler.capturedListTaskPushConfigParams = nil
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
				if tt.wantParams != nil && !reflect.DeepEqual(mockHandler.capturedListTaskPushConfigParams, tt.wantParams) {
					t.Errorf("OnListTaskPushConfig() params got = %v, want %v", mockHandler.capturedListTaskPushConfigParams, tt.wantParams)
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
	client := startTestServer(t, mockHandler, defaultMockCardProducer)

	tests := []struct {
		name       string
		req        *a2apb.DeleteTaskPushNotificationConfigRequest
		want       *emptypb.Empty
		wantParams *a2a.DeleteTaskPushConfigParams
		wantErr    codes.Code
	}{
		{
			name: "success",
			req: &a2apb.DeleteTaskPushNotificationConfigRequest{
				Name: fmt.Sprintf("tasks/%s/pushNotificationConfigs/%s", taskID, configID),
			},
			want: &emptypb.Empty{},
			wantParams: &a2a.DeleteTaskPushConfigParams{
				TaskID:   taskID,
				ConfigID: configID,
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
			mockHandler.capturedDeleteTaskPushConfigParams = nil
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
				if tt.wantParams != nil && !reflect.DeepEqual(mockHandler.capturedDeleteTaskPushConfigParams, tt.wantParams) {
					t.Errorf("OnDeleteTaskPushConfig() params got = %v, want %v", mockHandler.capturedDeleteTaskPushConfigParams, tt.wantParams)
				}
			}
		})
	}
}

func TestGrpcHandler_GetAgentCard(t *testing.T) {
	ctx := t.Context()

	a2aCard := &a2a.AgentCard{
		ProtocolVersion: "1.0",
		Name:            "Test Agent",
	}
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
			name:         "success",
			cardProducer: &mockAgentCardProducer{card: a2aCard},
			want:         pCard,
		},
		{
			name:         "nil producer",
			cardProducer: nil,
			wantErr:      codes.Unimplemented,
		},
		{
			name:         "producer returns nil card",
			cardProducer: &mockAgentCardProducer{card: nil},
			want:         &a2apb.AgentCard{},
		},
		{
			name:         "producer returns bad card",
			cardProducer: &mockAgentCardProducer{card: badCard},
			wantErr:      codes.Internal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := startTestServer(t, defaultMockHandler, tt.cardProducer)
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
