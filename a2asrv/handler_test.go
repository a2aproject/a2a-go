package a2asrv

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv/events"
)

// mockAgentExecutor is a mock of AgentExecutor.
type mockAgentExecutor struct {
	ExecuteFunc func(ctx context.Context, reqCtx RequestContext, queue events.EventQueue) error
	CancelFunc  func(ctx context.Context, reqCtx RequestContext, queue events.EventQueue) error
}

func (m *mockAgentExecutor) Execute(ctx context.Context, reqCtx RequestContext, queue events.EventQueue) error {
	if m.ExecuteFunc != nil {
		return m.ExecuteFunc(ctx, reqCtx, queue)
	}
	return nil
}

func (m *mockAgentExecutor) Cancel(ctx context.Context, reqCtx RequestContext, queue events.EventQueue) error {
	if m.CancelFunc != nil {
		return m.CancelFunc(ctx, reqCtx, queue)
	}
	return errors.New("cancel not implemented")
}

// mockQueueManager is a mock of events.EventQueueManager.
type mockQueueManager struct {
	GetOrCreateFunc func(ctx context.Context, taskId a2a.TaskID) (events.EventQueue, error)
	DestroyFunc     func(ctx context.Context, taskId a2a.TaskID) error
}

func (m *mockQueueManager) GetOrCreate(ctx context.Context, taskId a2a.TaskID) (events.EventQueue, error) {
	if m.GetOrCreateFunc != nil {
		return m.GetOrCreateFunc(ctx, taskId)
	}
	return nil, errors.New("getorcreate not implemented")
}

func (m *mockQueueManager) Destroy(ctx context.Context, taskId a2a.TaskID) error {
	if m.DestroyFunc != nil {
		return m.DestroyFunc(ctx, taskId)
	}
	return errors.New("destroy not implemented")
}

// mockevents.EventQueue is a mock of events.EventQueue.
type mockEventQueue struct {
	ReadFunc  func(ctx context.Context) (a2a.Event, error)
	WriteFunc func(ctx context.Context, event a2a.Event) error
	CloseFunc func() error
}

func (m *mockEventQueue) Read(ctx context.Context) (a2a.Event, error) {
	if m.ReadFunc != nil {
		return m.ReadFunc(ctx)
	}
	return nil, errors.New("read not implemented")
}

func (m *mockEventQueue) Write(ctx context.Context, event a2a.Event) error {
	if m.WriteFunc != nil {
		return m.WriteFunc(ctx, event)
	}
	return nil
}

func (m *mockEventQueue) Close() error {
	if m.CloseFunc != nil {
		m.CloseFunc()
	}
	return nil
}

func TestDefaultRequestHandler_OnHandleSendMessage(t *testing.T) {
	taskID := a2a.TaskID("test-task")
	messageWithTaskID := a2a.MessageSendParams{
		Message: a2a.Message{
			TaskID: &taskID,
		},
	}
	messageWithoutTaskID := a2a.MessageSendParams{
		Message: a2a.Message{
			TaskID: nil,
		},
	}
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		expectedEvent := a2a.Task{ID: taskID, Kind: "task"}

		mockQ := &mockEventQueue{
			ReadFunc: func(ctx context.Context) (a2a.Event, error) {
				return expectedEvent, nil
			},
		}

		mockExec := &mockAgentExecutor{}
		mockQM := &mockQueueManager{
			GetOrCreateFunc: func(ctx context.Context, id a2a.TaskID) (events.EventQueue, error) {
				if id != taskID {
					t.Fatalf("expected taskID %q, got %q", taskID, id)
				}
				return mockQ, nil
			},
		}

		handler := NewHandler(mockExec, WithEventQueueManager(mockQM))

		result, err := handler.OnHandleSendMessage(ctx, messageWithTaskID)
		if err != nil {
			t.Fatalf("OnHandleSendMessage() error = %v, wantErr nil", err)
		}

		if !reflect.DeepEqual(result, expectedEvent) {
			t.Errorf("OnHandleSendMessage() got = %v, want %v", result, expectedEvent)
		}
	})

	t.Run("nil TaskID", func(t *testing.T) {
		handler := NewHandler(&mockAgentExecutor{})

		// This test will panic without the suggested fix.
		// It will pass after applying the fix.
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("The code panicked with nil TaskID: %v", r)
			}
		}()

		_, err := handler.OnHandleSendMessage(ctx, messageWithoutTaskID)
		if err == nil {
			t.Fatal("OnHandleSendMessage() with nil TaskID should have returned an error, but got nil")
		}
		expectedErrStr := "message is missing TaskID"
		if err.Error() != expectedErrStr {
			t.Errorf("OnHandleSendMessage() error = %q, want %q", err.Error(), expectedErrStr)
		}
	})

	t.Run("GetOrCreate fails", func(t *testing.T) {
		expectedErr := errors.New("get or create failed")
		mockExec := &mockAgentExecutor{}
		mockQM := &mockQueueManager{
			GetOrCreateFunc: func(ctx context.Context, taskId a2a.TaskID) (events.EventQueue, error) {
				return nil, expectedErr
			},
		}

		handler := NewHandler(mockExec, WithEventQueueManager(mockQM))

		_, err := handler.OnHandleSendMessage(ctx, messageWithTaskID)
		if !errors.Is(err, expectedErr) {
			t.Errorf("OnHandleSendMessage() error = %v, want %v", err, expectedErr)
		}
	})

	t.Run("executor Execute fails", func(t *testing.T) {
		expectedErr := errors.New("execute failed")
		mockQ := &mockEventQueue{}
		mockExec := &mockAgentExecutor{
			ExecuteFunc: func(ctx context.Context, reqCtx RequestContext, q events.EventQueue) error {
				return expectedErr
			},
		}
		mockQM := &mockQueueManager{
			GetOrCreateFunc: func(ctx context.Context, taskId a2a.TaskID) (events.EventQueue, error) {
				return mockQ, nil
			},
		}

		handler := NewHandler(mockExec, WithEventQueueManager(mockQM))

		_, err := handler.OnHandleSendMessage(ctx, messageWithTaskID)
		if !errors.Is(err, expectedErr) {
			t.Errorf("OnHandleSendMessage() error = %v, want %v", err, expectedErr)
		}
	})

	t.Run("queue Read fails", func(t *testing.T) {
		expectedErr := errors.New("read failed")
		mockQ := &mockEventQueue{
			ReadFunc: func(ctx context.Context) (a2a.Event, error) {
				return nil, expectedErr
			},
		}

		mockExec := &mockAgentExecutor{}
		mockQM := &mockQueueManager{
			GetOrCreateFunc: func(ctx context.Context, taskId a2a.TaskID) (events.EventQueue, error) {
				return mockQ, nil
			},
		}

		handler := NewHandler(mockExec, WithEventQueueManager(mockQM))

		_, err := handler.OnHandleSendMessage(ctx, messageWithTaskID)
		if !errors.Is(err, expectedErr) {
			t.Errorf("OnHandleSendMessage() error = %v, want %v", err, expectedErr)
		}
	})

	t.Run("type assertion fails", func(t *testing.T) {
		invalidEvent := a2a.TaskStatusUpdateEvent{Kind: "status-update"}

		mockQ := &mockEventQueue{
			ReadFunc: func(ctx context.Context) (a2a.Event, error) {
				return invalidEvent, nil
			},
		}
		mockQM := &mockQueueManager{
			GetOrCreateFunc: func(ctx context.Context, taskId a2a.TaskID) (events.EventQueue, error) {
				return mockQ, nil
			},
		}
		handler := NewHandler(&mockAgentExecutor{}, WithEventQueueManager(mockQM))

		defer func() {
			if r := recover(); r == nil {
				t.Errorf("The code did not panic on invalid type assertion")
			}
		}()

		_, _ = handler.OnHandleSendMessage(ctx, messageWithTaskID)
	})
}

func TestDefaultRequestHandler_Unimplemented(t *testing.T) {
	handler := NewHandler(&mockAgentExecutor{})
	ctx := context.Background()

	if _, err := handler.OnHandleGetTask(ctx, a2a.TaskQueryParams{}); !errors.Is(err, errUnimplemented) {
		t.Errorf("OnHandleGetTask: expected unimplemented error, got %v", err)
	}
	if _, err := handler.OnHandleCancelTask(ctx, a2a.TaskIDParams{}); !errors.Is(err, errUnimplemented) {
		t.Errorf("OnHandleCancelTask: expected unimplemented error, got %v", err)
	}
	if seq := handler.OnHandleResubscribeToTask(ctx, a2a.TaskIDParams{}); seq != nil {
		t.Error("OnHandleResubscribeToTask: expected nil iterator, got non-nil")
	}
	if seq := handler.OnHandleSendMessageStream(ctx, a2a.MessageSendParams{}); seq != nil {
		t.Error("OnHandleSendMessageStream: expected nil iterator, got non-nil")
	}
	if _, err := handler.OnHandleGetTaskPushConfig(ctx, a2a.GetTaskPushConfigParams{}); !errors.Is(err, errUnimplemented) {
		t.Errorf("OnHandleGetTaskPushConfig: expected unimplemented error, got %v", err)
	}
	if _, err := handler.OnHandleListTaskPushConfig(ctx, a2a.ListTaskPushConfigParams{}); !errors.Is(err, errUnimplemented) {
		t.Errorf("OnHandleListTaskPushConfig: expected unimplemented error, got %v", err)
	}
	if _, err := handler.OnHandleSetTaskPushConfig(ctx, a2a.TaskPushConfig{}); !errors.Is(err, errUnimplemented) {
		t.Errorf("OnHandleSetTaskPushConfig: expected unimplemented error, got %v", err)
	}
	if err := handler.OnHandleDeleteTaskPushConfig(ctx, a2a.DeleteTaskPushConfigParams{}); !errors.Is(err, errUnimplemented) {
		t.Errorf("OnHandleDeleteTaskPushConfig: expected unimplemented error, got %v", err)
	}
}
