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
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
	"github.com/a2aproject/a2a-go/internal/taskstore"
)

var fixedTime = time.Now()

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
	GetOrCreateFunc func(ctx context.Context, taskId a2a.TaskID) (eventqueue.Queue, error)
	GetFunc         func(ctx context.Context, taskId a2a.TaskID) (eventqueue.Queue, bool)
	DestroyFunc     func(ctx context.Context, taskId a2a.TaskID) error
}

func (m *mockQueueManager) GetOrCreate(ctx context.Context, taskId a2a.TaskID) (eventqueue.Queue, error) {
	if m.GetOrCreateFunc != nil {
		return m.GetOrCreateFunc(ctx, taskId)
	}
	return nil, errors.New("GetOrCreate() not implemented")
}

func (m *mockQueueManager) Get(ctx context.Context, taskId a2a.TaskID) (eventqueue.Queue, bool) {
	if m.GetFunc != nil {
		return m.GetFunc(ctx, taskId)
	}
	return nil, false
}

func (m *mockQueueManager) Destroy(ctx context.Context, taskId a2a.TaskID) error {
	if m.DestroyFunc != nil {
		return m.DestroyFunc(ctx, taskId)
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

func newEventReplayQueueManager(toSend ...a2a.Event) eventqueue.Manager {
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
			return mockQ, nil
		},
	}
}

func newTestHandler(opts ...RequestHandlerOption) RequestHandler {
	mockExec := &mockAgentExecutor{
		ExecuteFunc: func(ctx context.Context, reqCtx RequestContext, q eventqueue.Queue) error {
			return nil
		},
	}
	return NewHandler(mockExec, opts...)
}

func newAgentMessage(text string) *a2a.Message {
	return &a2a.Message{ID: "message-id", Parts: []a2a.Part{a2a.TextPart{Text: text}}, Role: a2a.MessageRoleAgent}
}

func newTaskStatusUpdate(task *a2a.Task, state a2a.TaskState, msg string) *a2a.TaskStatusUpdateEvent {
	ue := a2a.NewStatusUpdateEvent(task, state, newAgentMessage(msg))
	ue.Status.Timestamp = &fixedTime
	return ue
}

func newFinalTaskStatusUpdate(task *a2a.Task, state a2a.TaskState, msg string) *a2a.TaskStatusUpdateEvent {
	res := newTaskStatusUpdate(task, state, msg)
	res.Final = true
	return res
}

func newTaskWithStatus(task *a2a.Task, state a2a.TaskState, msg string) *a2a.Task {
	status := a2a.TaskStatus{State: state, Message: newAgentMessage(msg)}
	return &a2a.Task{ID: task.ID, ContextID: task.ContextID, Status: status}
}

func newTaskWithMeta(task *a2a.Task, meta map[string]any) *a2a.Task {
	return &a2a.Task{ID: task.ID, ContextID: task.ContextID, Metadata: meta}
}

func TestDefaultRequestHandler_OnSendMessage(t *testing.T) {
	taskSeed := &a2a.Task{ID: a2a.NewTaskID(), ContextID: a2a.NewContextID()}

	tests := []struct {
		name        string
		input       *a2a.MessageSendParams
		agentEvents []a2a.Event

		wantResult a2a.SendMessageResult
		wantErr    error
	}{
		{
			name:        "message returned as a result",
			agentEvents: []a2a.Event{newAgentMessage("hello")},
			wantResult:  newAgentMessage("hello"),
		},
		{
			name:        "cancelled",
			agentEvents: []a2a.Event{newTaskWithStatus(taskSeed, a2a.TaskStateCanceled, "cancelled")},
			wantResult:  newTaskWithStatus(taskSeed, a2a.TaskStateCanceled, "cancelled"),
		},
		{
			name:        "failed",
			agentEvents: []a2a.Event{newTaskWithStatus(taskSeed, a2a.TaskStateFailed, "failed")},
			wantResult:  newTaskWithStatus(taskSeed, a2a.TaskStateFailed, "failed"),
		},
		{
			name:        "rejected",
			agentEvents: []a2a.Event{newTaskWithStatus(taskSeed, a2a.TaskStateRejected, "rejected")},
			wantResult:  newTaskWithStatus(taskSeed, a2a.TaskStateRejected, "rejected"),
		},
		{
			name:        "input required",
			agentEvents: []a2a.Event{newTaskWithStatus(taskSeed, a2a.TaskStateInputRequired, "need more input")},
			wantResult:  newTaskWithStatus(taskSeed, a2a.TaskStateInputRequired, "need more input"),
		},
		{
			name:        "fails if unknown task state",
			agentEvents: []a2a.Event{newTaskWithStatus(taskSeed, a2a.TaskStateUnknown, "...")},
			wantErr:     fmt.Errorf("unknown task state"),
		},
		{
			name: "final task overwrites intermediate task events",
			agentEvents: []a2a.Event{
				newTaskWithMeta(taskSeed, map[string]any{"foo": "bar"}),
				newTaskWithStatus(taskSeed, a2a.TaskStateCompleted, "meta lost"),
			},
			wantResult: newTaskWithStatus(taskSeed, a2a.TaskStateCompleted, "meta lost"),
		},
		{
			name: "event final flag takes precedence over task state",
			agentEvents: []a2a.Event{
				newTaskStatusUpdate(taskSeed, a2a.TaskStateCompleted, "Working..."),
				newFinalTaskStatusUpdate(taskSeed, a2a.TaskStateWorking, "Done!"),
			},
			wantResult: &a2a.Task{
				ID:        taskSeed.ID,
				ContextID: taskSeed.ContextID,
				Status: a2a.TaskStatus{
					State:     a2a.TaskStateWorking,
					Message:   newAgentMessage("Done!"),
					Timestamp: &fixedTime,
				},
				History: []*a2a.Message{newAgentMessage("Working...")},
			},
		},
		{
			name: "task status update accumulation",
			agentEvents: []a2a.Event{
				newTaskStatusUpdate(taskSeed, a2a.TaskStateSubmitted, "Ack"),
				newTaskStatusUpdate(taskSeed, a2a.TaskStateWorking, "Working..."),
				newFinalTaskStatusUpdate(taskSeed, a2a.TaskStateCompleted, "Done!"),
			},
			wantResult: &a2a.Task{
				ID:        taskSeed.ID,
				ContextID: taskSeed.ContextID,
				Status: a2a.TaskStatus{
					State:     a2a.TaskStateCompleted,
					Message:   newAgentMessage("Done!"),
					Timestamp: &fixedTime,
				},
				History: []*a2a.Message{
					newAgentMessage("Ack"),
					newAgentMessage("Working..."),
				},
			},
		},
		{
			name: "final task overwrites intermediate status updates",
			agentEvents: []a2a.Event{
				newTaskStatusUpdate(taskSeed, a2a.TaskStateSubmitted, "Ack"),
				newTaskStatusUpdate(taskSeed, a2a.TaskStateWorking, "Working..."),
				newTaskWithStatus(taskSeed, a2a.TaskStateCompleted, "no status change history"),
			},
			wantResult: newTaskWithStatus(taskSeed, a2a.TaskStateCompleted, "no status change history"),
		},
		{
			name:    "fails on non-existent task reference",
			input:   &a2a.MessageSendParams{Message: a2a.Message{TaskID: "non-existent", ID: "test-message"}},
			wantErr: a2a.ErrTaskNotFound,
		},
		{
			name:    "queue read fails",
			wantErr: fmt.Errorf("The number of ReadFunc exceeded the number of events: 0"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			var qm eventqueue.Manager
			if tt.agentEvents == nil {
				qm = newEventReplayQueueManager()
			} else {
				qm = newEventReplayQueueManager(tt.agentEvents...)
			}
			store := taskstore.NewMem()
			_ = store.Save(ctx, taskSeed)
			handler := newTestHandler(
				WithEventQueueManager(qm),
				WithTaskStore(store),
			)
			input := a2a.MessageSendParams{Message: a2a.Message{TaskID: taskSeed.ID}}
			if tt.input != nil {
				input = *tt.input
			}

			result, gotErr := handler.OnSendMessage(ctx, input)
			if tt.wantErr == nil {
				if gotErr != nil {
					t.Fatalf("OnSendMessage() error = %v, wantErr nil", gotErr)
				}
				if !reflect.DeepEqual(result, tt.wantResult) {
					t.Errorf("OnSendMessage() got = %v, want %v", result, tt.wantResult)
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

func TestDefaultRequestHandler_OnSendMessage_QueueCreationFails(t *testing.T) {
	ctx := t.Context()
	wantErr := errors.New("failed to create a queue")
	qm := &mockQueueManager{
		GetOrCreateFunc: func(ctx context.Context, id a2a.TaskID) (eventqueue.Queue, error) {
			return nil, wantErr
		},
	}
	handler := newTestHandler(WithEventQueueManager(qm))

	result, err := handler.OnSendMessage(ctx, a2a.MessageSendParams{})

	if result != nil || !errors.Is(err, wantErr) {
		t.Fatalf("expected OnSendMessage() to fail with %v, got: %v, %v", wantErr, result, err)
	}
}

func TestDefaultRequestHandler_OnSendMessage_AgentExecutionFails(t *testing.T) {
	ctx := t.Context()
	wantErr := errors.New("failed to create a queue")
	executor := &mockAgentExecutor{
		ExecuteFunc: func(ctx context.Context, reqCtx RequestContext, q eventqueue.Queue) error {
			return wantErr
		},
	}
	handler := NewHandler(executor)

	result, err := handler.OnSendMessage(ctx, a2a.MessageSendParams{})

	if result != nil || !errors.Is(err, wantErr) {
		t.Fatalf("expected OnSendMessage() to fail with %v, got: %v, %v", wantErr, result, err)
	}
}

func TestDefaultRequestHandler_Unimplemented(t *testing.T) {
	handler := NewHandler(&mockAgentExecutor{})
	ctx := t.Context()

	if _, err := handler.OnGetTask(ctx, a2a.TaskQueryParams{}); !errors.Is(err, errUnimplemented) {
		t.Errorf("OnGetTask: expected unimplemented error, got %v", err)
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
