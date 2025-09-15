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

package a2asrv

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
)

var (
	taskID                = a2a.TaskID("test-task")
	getOrCreateFailTaskID = a2a.TaskID("get-or-create-fails")
	executeFailTaskID     = a2a.TaskID("execute-fails")
)

// mockAgentExecutor is a mock of AgentExecutor.
type mockAgentExecutor struct {
	ExecuteFunc func(ctx context.Context, reqCtx RequestContext, queue eventqueue.Queue) error
	CancelFunc  func(ctx context.Context, reqCtx RequestContext, queue eventqueue.Queue) error
}

func (m *mockAgentExecutor) Execute(ctx context.Context, reqCtx RequestContext, queue eventqueue.Queue) error {
	if m.ExecuteFunc != nil {
		return m.ExecuteFunc(ctx, reqCtx, queue)
	}
	return nil
}

func (m *mockAgentExecutor) Cancel(ctx context.Context, reqCtx RequestContext, queue eventqueue.Queue) error {
	if m.CancelFunc != nil {
		return m.CancelFunc(ctx, reqCtx, queue)
	}
	return errors.New("Cancel() not implemented")
}

// mockQueueManager is a mock of eventqueue.Manager
type mockQueueManager struct {
	GetOrCreateFunc func(ctx context.Context, taskID a2a.TaskID) (eventqueue.Queue, error)
	DestroyFunc     func(ctx context.Context, taskID a2a.TaskID) error
}

func (m *mockQueueManager) GetOrCreate(ctx context.Context, taskID a2a.TaskID) (eventqueue.Queue, error) {
	if m.GetOrCreateFunc != nil {
		return m.GetOrCreateFunc(ctx, taskID)
	}
	return nil, errors.New("GetOrCreate() not implemented")
}

func (m *mockQueueManager) Destroy(ctx context.Context, taskID a2a.TaskID) error {
	if m.DestroyFunc != nil {
		return m.DestroyFunc(ctx, taskID)
	}
	return errors.New("Destroy() not implemented")
}

// mockEventQueue is a mock of eventqueue.Queue
type mockEventQueue struct {
	ReadFunc  func(ctx context.Context) (a2a.Event, error)
	WriteFunc func(ctx context.Context, event a2a.Event) error
	CloseFunc func() error
}

func (m *mockEventQueue) Read(ctx context.Context) (a2a.Event, error) {
	if m.ReadFunc != nil {
		return m.ReadFunc(ctx)
	}
	return nil, errors.New("Read() not implemented")
}

func (m *mockEventQueue) Write(ctx context.Context, event a2a.Event) error {
	if m.WriteFunc != nil {
		return m.WriteFunc(ctx, event)
	}
	return errors.New("Write() not implemented")
}

func (m *mockEventQueue) Close() error {
	if m.CloseFunc != nil {
		return m.CloseFunc()
	}
	return errors.New("Close() not implemented")
}

func newEventReplayQueueManager(t *testing.T, toSend ...a2a.Event) eventqueue.Manager {
	i := 0
	mockQ := &mockEventQueue{
		ReadFunc: func(ctx context.Context) (a2a.Event, error) {
			if i >= len(toSend) {
				return nil, fmt.Errorf("The number of ReadFunc exceeded the number of events: %d", i)
			}
			e := toSend[i]
			i++
			return e, nil
		},
	}
	return &mockQueueManager{
		GetOrCreateFunc: func(ctx context.Context, id a2a.TaskID) (eventqueue.Queue, error) {
			if id == getOrCreateFailTaskID {
				return nil, errors.New("get or create failed")
			}
			return mockQ, nil
		},
	}
}

func newTestHandler(opts ...RequestHandlerOption) RequestHandler {
	mockExec := &mockAgentExecutor{
		ExecuteFunc: func(ctx context.Context, reqCtx RequestContext, q eventqueue.Queue) error {
			if reqCtx.TaskID == executeFailTaskID {
				return errors.New("execute failed")
			}
			return nil
		},
	}
	return NewHandler(mockExec, opts...)
}

func TestDefaultRequestHandler_OnSendMessage(t *testing.T) {
	tests := []struct {
		name      string
		message   a2a.MessageSendParams
		wantEvent a2a.Event
		wantErr   error
	}{
		{
			name: "success with TaskID",
			message: a2a.MessageSendParams{
				Message: a2a.Message{TaskID: taskID, ID: "test-message"},
			},
			wantEvent: &a2a.Message{TaskID: taskID, ID: "test-message"},
		},
		{
			name: "missing TaskID",
			message: a2a.MessageSendParams{
				Message: a2a.Message{ID: "test-message"},
			},
			wantErr: errors.New("message is missing TaskID"),
		},
		{
			name: "type assertion fails",
			message: a2a.MessageSendParams{
				Message: a2a.Message{TaskID: taskID, ID: "test-message"},
			},
			wantEvent: &a2a.TaskStatusUpdateEvent{TaskID: taskID},
			wantErr:   errors.New("unexpected event type: *a2a.TaskStatusUpdateEvent"),
		},
		{
			name: "GetOrCreate() fails",
			message: a2a.MessageSendParams{
				Message: a2a.Message{TaskID: getOrCreateFailTaskID, ID: "test-message"},
			},
			wantErr: errors.New("failed to retrieve queue: get or create failed"),
		},
		{
			name: "executor Execute() fails",
			message: a2a.MessageSendParams{
				Message: a2a.Message{TaskID: executeFailTaskID, ID: "test-message"},
			},
			wantErr: errors.New("execute failed"),
		},
		{
			name: "queue Read() fails",
			message: a2a.MessageSendParams{
				Message: a2a.Message{TaskID: taskID, ID: "test-message"},
			},
			wantErr: errors.New("failed to read event from queue: The number of ReadFunc exceeded the number of events: 0"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			var qm eventqueue.Manager
			if tt.wantEvent == nil {
				qm = newEventReplayQueueManager(t)
			} else {
				qm = newEventReplayQueueManager(t, tt.wantEvent)
			}
			handler := newTestHandler(WithEventQueueManager(qm))
			result, gotErr := handler.OnSendMessage(ctx, tt.message)
			if tt.wantErr == nil {
				if gotErr != nil {
					t.Fatalf("OnSendMessage() error = %v, wantErr nil", gotErr)
				}
				if !reflect.DeepEqual(result, tt.wantEvent) {
					t.Errorf("OnSendMessage() got = %v, want %v", result, tt.wantEvent)
				}
			} else {
				if gotErr == nil {
					t.Fatalf("OnSendMessage() error = nil, wantErr %q", tt.wantErr)
				}
				if gotErr.Error() != tt.wantErr.Error() {
					t.Errorf("OnSendMessage() error = %v, wantErr %v", gotErr, tt.wantErr)
				}

			}
		})
	}
}

func TestDefaultRequestHandler_Unimplemented(t *testing.T) {
	handler := NewHandler(&mockAgentExecutor{})
	ctx := t.Context()

	if _, err := handler.OnGetTask(ctx, a2a.TaskQueryParams{}); !errors.Is(err, errUnimplemented) {
		t.Errorf("OnGetTask: expected unimplemented error, got %v", err)
	}
	if _, err := handler.OnCancelTask(ctx, a2a.TaskIDParams{}); !errors.Is(err, errUnimplemented) {
		t.Errorf("OnCancelTask: expected unimplemented error, got %v", err)
	}
	if seq := handler.OnResubscribeToTask(ctx, a2a.TaskIDParams{}); seq != nil {
		t.Error("OnResubscribeToTask: expected nil iterator, got non-nil")
	}
	if seq := handler.OnSendMessageStream(ctx, a2a.MessageSendParams{}); seq != nil {
		t.Error("OnSendMessageStream: expected nil iterator, got non-nil")
	}
	if _, err := handler.OnGetTaskPushConfig(ctx, a2a.GetTaskPushConfigParams{}); !errors.Is(err, errUnimplemented) {
		t.Errorf("OnGetTaskPushConfig: expected unimplemented error, got %v", err)
	}
	if _, err := handler.OnListTaskPushConfig(ctx, a2a.ListTaskPushConfigParams{}); !errors.Is(err, errUnimplemented) {
		t.Errorf("OnListTaskPushConfig: expected unimplemented error, got %v", err)
	}
	if _, err := handler.OnSetTaskPushConfig(ctx, a2a.TaskPushConfig{}); !errors.Is(err, errUnimplemented) {
		t.Errorf("OnSetTaskPushConfig: expected unimplemented error, got %v", err)
	}
	if err := handler.OnDeleteTaskPushConfig(ctx, a2a.DeleteTaskPushConfigParams{}); !errors.Is(err, errUnimplemented) {
		t.Errorf("OnDeleteTaskPushConfig: expected unimplemented error, got %v", err)
	}
}
