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
	"iter"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
	"github.com/a2aproject/a2a-go/internal/testutil"
	"github.com/google/go-cmp/cmp"
)

var fixedTime = time.Now()

func TestDefaultRequestHandler_OnSendMessage(t *testing.T) {
	artifactID := a2a.NewArtifactID()
	taskSeed := &a2a.Task{ID: a2a.NewTaskID(), ContextID: a2a.NewContextID()}
	completedTaskSeed := &a2a.Task{ID: a2a.NewTaskID(), ContextID: a2a.NewContextID(), Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}}

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
			wantErr:     fmt.Errorf("unknown task state: unknown"),
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
			name: "task artifact streaming",
			agentEvents: []a2a.Event{
				newTaskStatusUpdate(taskSeed, a2a.TaskStateSubmitted, "Ack"),
				newArtifactEvent(taskSeed, artifactID, a2a.TextPart{Text: "Hello"}),
				a2a.NewArtifactUpdateEvent(taskSeed, artifactID, a2a.TextPart{Text: ", world!"}),
				newFinalTaskStatusUpdate(taskSeed, a2a.TaskStateCompleted, "Done!"),
			},
			wantResult: &a2a.Task{
				ID:        taskSeed.ID,
				ContextID: taskSeed.ContextID,
				Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted, Message: newAgentMessage("Done!"), Timestamp: &fixedTime},
				History:   []*a2a.Message{newAgentMessage("Ack")},
				Artifacts: []*a2a.Artifact{
					{ID: artifactID, Parts: a2a.ContentParts{a2a.TextPart{Text: "Hello"}, a2a.TextPart{Text: ", world!"}}},
				},
			},
		},
		{
			name: "task with multiple artifacts",
			agentEvents: []a2a.Event{
				newTaskStatusUpdate(taskSeed, a2a.TaskStateSubmitted, "Ack"),
				newArtifactEvent(taskSeed, artifactID, a2a.TextPart{Text: "Hello"}),
				newArtifactEvent(taskSeed, artifactID+"2", a2a.TextPart{Text: "World"}),
				newFinalTaskStatusUpdate(taskSeed, a2a.TaskStateCompleted, "Done!"),
			},
			wantResult: &a2a.Task{
				ID:        taskSeed.ID,
				ContextID: taskSeed.ContextID,
				Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted, Message: newAgentMessage("Done!"), Timestamp: &fixedTime},
				History:   []*a2a.Message{newAgentMessage("Ack")},
				Artifacts: []*a2a.Artifact{
					{ID: artifactID, Parts: a2a.ContentParts{a2a.TextPart{Text: "Hello"}}},
					{ID: artifactID + "2", Parts: a2a.ContentParts{a2a.TextPart{Text: "World"}}},
				},
			},
		},
		{
			name:    "missing message",
			input:   &a2a.MessageSendParams{},
			wantErr: fmt.Errorf("message is required: %w", a2a.ErrInvalidRequest),
		},
		{
			name:    "fails on non-existent task reference",
			input:   &a2a.MessageSendParams{Message: &a2a.Message{TaskID: "non-existent", ID: "test-message"}},
			wantErr: a2a.ErrTaskNotFound,
		},
		{
			name: "fails if contextID not equal to task contextID",
			input: &a2a.MessageSendParams{
				Message: &a2a.Message{TaskID: taskSeed.ID, ContextID: taskSeed.ContextID + "1", ID: "test-message"},
			},
			wantErr: a2a.ErrInvalidRequest,
		},
		{
			name: "fails if message references non-existent task",
			input: &a2a.MessageSendParams{
				Message: &a2a.Message{TaskID: taskSeed.ID + "1", ContextID: taskSeed.ContextID, ID: "test-message"},
			},
			wantErr: a2a.ErrTaskNotFound,
		},
		{
			name: "fails if message references completed task",
			input: &a2a.MessageSendParams{
				Message: &a2a.Message{TaskID: completedTaskSeed.ID, ContextID: completedTaskSeed.ContextID, ID: "test-message"},
			},
			wantErr: fmt.Errorf("%w: task in a terminal state %q", a2a.ErrInvalidRequest, a2a.TaskStateCompleted),
		},
	}

	for _, tt := range tests {
		input := &a2a.MessageSendParams{Message: &a2a.Message{TaskID: taskSeed.ID}}
		if tt.input != nil {
			input = tt.input
		}

		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			store := testutil.NewTestTaskStore().WithTasks(t, taskSeed, completedTaskSeed)
			executor := newEventReplayAgent(tt.agentEvents, nil)
			handler := NewHandler(executor, WithTaskStore(store))

			result, gotErr := handler.OnSendMessage(ctx, input)
			if tt.wantErr == nil {
				if gotErr != nil {
					t.Fatalf("OnSendMessage() error = %v, wantErr nil", gotErr)
				}
				if diff := cmp.Diff(tt.wantResult, result); diff != "" {
					t.Errorf("OnSendMessage() (+got,-want):\ngot = %v\nwant %v\ndiff = %s", result, tt.wantResult, diff)
				}
			} else {
				if gotErr == nil {
					t.Fatalf("OnSendMessage() error = nil, wantErr %q", tt.wantErr)
				}
				if gotErr.Error() != tt.wantErr.Error() && !errors.Is(gotErr, tt.wantErr) {
					t.Errorf("OnSendMessage() error = %v, wantErr %v", gotErr, tt.wantErr)
				}
			}
		})

		t.Run(tt.name+" (streaming)", func(t *testing.T) {
			ctx := t.Context()
			store := testutil.NewTestTaskStore().WithTasks(t, taskSeed, completedTaskSeed)
			executor := newEventReplayAgent(tt.agentEvents, nil)
			handler := NewHandler(executor, WithTaskStore(store))

			eventI := 0
			var streamErr error
			for got, gotErr := range handler.OnSendMessageStream(ctx, input) {
				if streamErr != nil {
					t.Errorf("handler.OnSendMessageStream() got (%v, %v) after error, want stream end", got, gotErr)
				}

				if gotErr != nil && tt.wantErr == nil {
					t.Fatalf("OnSendMessageStream() error = %v, wantErr nil", gotErr)
				}
				if gotErr != nil {
					streamErr = gotErr
					continue
				}

				var want a2a.Event
				if eventI < len(tt.agentEvents) {
					want = tt.agentEvents[eventI]
					eventI++
				}
				if diff := cmp.Diff(want, got); diff != "" {
					t.Errorf("OnSendMessageStream() (+got,-want):\ngot = %v\nwant %v\ndiff = %s", got, want, diff)
				}
			}
			if tt.wantErr == nil && eventI != len(tt.agentEvents) {
				t.Errorf("OnSendMessageStream() received %d events, want %d", eventI, len(tt.agentEvents))
			}
			if tt.wantErr != nil && streamErr == nil {
				t.Errorf("OnSendMessageStream() error = nil, want %v", tt.wantErr)
			}
			if tt.wantErr != nil && (streamErr.Error() != tt.wantErr.Error() && !errors.Is(streamErr, tt.wantErr)) {
				t.Errorf("OnSendMessageStream() error = %v, wantErr %v", streamErr, tt.wantErr)
			}
		})
	}
}

func TestDefaultRequestHandler_OnSendMessage_QueueCreationFails(t *testing.T) {
	ctx := t.Context()
	wantErr := errors.New("failed to create a queue")
	qm := testutil.NewTestQueueManager().SetGetOrCreateOverride(nil, wantErr)
	handler := newTestHandler(WithEventQueueManager(qm))

	result, err := handler.OnSendMessage(ctx, &a2a.MessageSendParams{Message: &a2a.Message{}})

	if result != nil || !errors.Is(err, wantErr) {
		t.Fatalf("handler.OnSendMessage() = (%v, %v), want error %v", result, err, wantErr)
	}
}

func TestDefaultRequestHandler_OnSendMessage_QueueReadFails(t *testing.T) {
	ctx := t.Context()
	wantErr := errors.New("Read() failed")
	queue := testutil.NewTestEventQueue().SetReadOverride(nil, wantErr)
	qm := testutil.NewTestQueueManager().SetGetOrCreateOverride(queue, nil)
	handler := newTestHandler(WithEventQueueManager(qm))

	result, err := handler.OnSendMessage(ctx, &a2a.MessageSendParams{Message: &a2a.Message{}})

	if result != nil || !errors.Is(err, wantErr) {
		t.Fatalf("handler.OnSendMessage() = (%v, %v), want error %v", result, err, wantErr)
	}
}

func TestDefaultRequestHandler_OnSendMessage_RelatedTaskLoading(t *testing.T) {
	existingTask := &a2a.Task{ID: a2a.NewTaskID(), ContextID: a2a.NewContextID()}
	ctx := t.Context()
	ts := testutil.NewTestTaskStore().WithTasks(t, existingTask)
	executor := newEventReplayAgent([]a2a.Event{a2a.NewMessage(a2a.MessageRoleAgent, a2a.TextPart{Text: "Hello!"})}, nil)
	handler := NewHandler(executor, WithRequestContextInterceptor(&ReferencedTasksLoader{Store: ts}))

	request := &a2a.MessageSendParams{Message: &a2a.Message{ReferenceTasks: []a2a.TaskID{a2a.NewTaskID(), existingTask.ID}}}
	_, err := handler.OnSendMessage(ctx, request)
	if err != nil {
		t.Fatalf("handler.OnSendMessage() failed: %v", err)
	}

	capturedReqContext := executor.capturedReqContext
	if len(capturedReqContext.RelatedTasks) != 1 || capturedReqContext.RelatedTasks[0].ID != existingTask.ID {
		t.Fatalf("RequestContext.RelatedTasks = %v, want [%v]", capturedReqContext.RelatedTasks, existingTask)
	}
}

func TestDefaultRequestHandler_OnSendMessage_AgentExecutionFails(t *testing.T) {
	ctx := t.Context()
	wantErr := errors.New("failed to create a queue")
	executor := newEventReplayAgent([]a2a.Event{}, wantErr)
	handler := NewHandler(executor)

	result, err := handler.OnSendMessage(ctx, &a2a.MessageSendParams{Message: &a2a.Message{}})

	if result != nil || !errors.Is(err, wantErr) {
		t.Fatalf("handler.OnSendMessage() = (%v, %v), want error %v", result, err, wantErr)
	}
}

func TestDefaultRequestHandler_OnGetTask(t *testing.T) {
	ptr := func(i int) *int {
		return &i
	}

	existingTaskID := a2a.NewTaskID()
	history := []*a2a.Message{{ID: "test-message-1"}, {ID: "test-message-2"}, {ID: "test-message-3"}}

	tests := []struct {
		name    string
		query   *a2a.TaskQueryParams
		want    *a2a.Task
		wantErr error
	}{
		{
			name:  "success with TaskID and full history",
			query: &a2a.TaskQueryParams{ID: existingTaskID},
			want:  &a2a.Task{ID: existingTaskID, History: history},
		},
		{
			name:    "missing TaskID",
			query:   &a2a.TaskQueryParams{ID: ""},
			wantErr: fmt.Errorf("%w: missing TaskID", a2a.ErrInvalidRequest),
		},
		{
			name:    "task not found",
			query:   &a2a.TaskQueryParams{ID: a2a.NewTaskID()},
			wantErr: fmt.Errorf("failed to get task: %w", a2a.ErrTaskNotFound),
		},
		{
			name:  "get task with limited HistoryLength",
			query: &a2a.TaskQueryParams{ID: existingTaskID, HistoryLength: ptr(len(history) - 1)},
			want:  &a2a.Task{ID: existingTaskID, History: history[1:]},
		},
		{
			name:  "get task with larger than available HistoryLength",
			query: &a2a.TaskQueryParams{ID: existingTaskID, HistoryLength: ptr(len(history) + 1)},
			want:  &a2a.Task{ID: existingTaskID, History: history},
		},
		{
			name:  "get task with zero HistoryLength",
			query: &a2a.TaskQueryParams{ID: existingTaskID, HistoryLength: ptr(0)},
			want:  &a2a.Task{ID: existingTaskID, History: make([]*a2a.Message, 0)},
		},
		{
			name:  "get task with negative HistoryLength",
			query: &a2a.TaskQueryParams{ID: existingTaskID, HistoryLength: ptr(-1)},
			want:  &a2a.Task{ID: existingTaskID, History: make([]*a2a.Message, 0)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			ts := testutil.NewTestTaskStore().WithTasks(t, &a2a.Task{ID: existingTaskID, History: history})
			handler := newTestHandler(WithTaskStore(ts))
			result, err := handler.OnGetTask(ctx, tt.query)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("OnGetTask() error = %v, wantErr nil", err)
				}

				if diff := cmp.Diff(result, tt.want); diff != "" {
					t.Errorf("OnGetTask() got = %v, want %v", result, tt.want)
				}
			} else {
				if err == nil {
					t.Fatalf("OnGetTask() error = nil, wantErr %q", tt.wantErr)
				}
				if err.Error() != tt.wantErr.Error() {
					t.Errorf("OnGetTask() error = %v, wantErr %v", err, tt.wantErr)
				}
			}
		})
	}
}

func TestDefaultRequestHandler_OnGetTask_StoreGetFails(t *testing.T) {
	ctx := t.Context()
	wantErr := errors.New("failed to get task: store get failed")
	ts := testutil.NewTestTaskStore().SetGetOverride(nil, wantErr)
	handler := newTestHandler(WithTaskStore(ts))

	result, err := handler.OnGetTask(ctx, &a2a.TaskQueryParams{ID: a2a.NewTaskID()})
	if result != nil || !errors.Is(err, wantErr) {
		t.Fatalf("OnGetTask() = (%v, %v), want error %v", result, err, wantErr)
	}
}

func TestDefaultRequestHandler_OnCancelTask(t *testing.T) {
	taskToCancel := &a2a.Task{ID: a2a.NewTaskID(), ContextID: a2a.NewContextID(), Status: a2a.TaskStatus{State: a2a.TaskStateWorking}}
	completedTask := &a2a.Task{ID: a2a.NewTaskID(), ContextID: a2a.NewContextID(), Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}}
	canceledTask := &a2a.Task{ID: a2a.NewTaskID(), ContextID: a2a.NewContextID(), Status: a2a.TaskStatus{State: a2a.TaskStateCanceled}}

	tests := []struct {
		name    string
		params  *a2a.TaskIDParams
		want    *a2a.Task
		wantErr error
	}{
		{
			name:   "success",
			params: &a2a.TaskIDParams{ID: taskToCancel.ID},
			want:   newTaskWithStatus(taskToCancel, a2a.TaskStateCanceled, "Cancelled"),
		},
		{
			name:    "nil params",
			params:  nil,
			wantErr: a2a.ErrInvalidRequest,
		},
		{
			name:    "task not found",
			params:  &a2a.TaskIDParams{ID: a2a.NewTaskID()},
			wantErr: fmt.Errorf("failed to cancel: cancelation failed: failed to load a task: %w", a2a.ErrTaskNotFound),
		},
		{
			name:    "task already completed",
			params:  &a2a.TaskIDParams{ID: completedTask.ID},
			wantErr: fmt.Errorf("failed to cancel: cancelation failed: task in non-cancelable state %s: %w", a2a.TaskStateCompleted, a2a.ErrTaskNotCancelable),
		},
		{
			name:   "task already canceled",
			params: &a2a.TaskIDParams{ID: canceledTask.ID},
			want:   canceledTask,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			store := testutil.NewTestTaskStore().WithTasks(t, taskToCancel, completedTask, canceledTask)
			executor := &mockAgentExecutor{
				CancelFunc: func(ctx context.Context, reqCtx *RequestContext, q eventqueue.Queue) error {
					event := newFinalTaskStatusUpdate(taskToCancel, a2a.TaskStateCanceled, "Cancelled")
					return q.Write(ctx, event)
				},
			}
			handler := NewHandler(executor, WithTaskStore(store))

			result, err := handler.OnCancelTask(ctx, tt.params)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("OnCancelTask() error = %v, wantErr nil", err)
				}

				if diff := cmp.Diff(result, tt.want); diff != "" {
					t.Errorf("OnCancelTask() got = %v, want %v", result, tt.want)
				}
			} else {
				if err == nil {
					t.Fatalf("OnCancelTask() error = nil, wantErr %q", tt.wantErr)
				}
				if err.Error() != tt.wantErr.Error() {
					t.Errorf("OnCancelTask() error = %v, wantErr %v", err, tt.wantErr)
				}
			}
		})
	}
}

func TestDefaultRequestHandler_OnResubscribeToTask_Success(t *testing.T) {
	ctx := t.Context()
	taskSeed := &a2a.Task{ID: a2a.NewTaskID(), ContextID: a2a.NewContextID()}
	wantEvents := []a2a.Event{
		newTaskStatusUpdate(taskSeed, a2a.TaskStateSubmitted, "Starting"),
		newTaskStatusUpdate(taskSeed, a2a.TaskStateWorking, "Working..."),
		newFinalTaskStatusUpdate(taskSeed, a2a.TaskStateCompleted, "Done"),
	}

	ts := testutil.NewTestTaskStore().WithTasks(t, taskSeed)
	executor := newEventReplayAgent(wantEvents, nil)
	handler := NewHandler(executor, WithTaskStore(ts))
	executionStarted := make(chan struct{})
	originalExecuteFunc := executor.ExecuteFunc
	executor.ExecuteFunc = func(ctx context.Context, reqCtx *RequestContext, queue eventqueue.Queue) error {
		close(executionStarted)
		time.Sleep(1 * time.Millisecond)
		return originalExecuteFunc(ctx, reqCtx, queue)
	}

	go handler.OnSendMessageStream(ctx, &a2a.MessageSendParams{Message: &a2a.Message{TaskID: taskSeed.ID}})

	<-executionStarted

	seq := handler.OnResubscribeToTask(ctx, &a2a.TaskIDParams{ID: taskSeed.ID})
	gotEvents, err := collectEvents(seq)
	if err != nil {
		t.Fatalf("collectEvents() failed: %v", err)
	}

	if diff := cmp.Diff(wantEvents, gotEvents); diff != "" {
		t.Errorf("OnResubscribeToTask() events mismatch (-want +got):\n%s", diff)
	}
}

func TestDefaultRequestHandler_OnResubscribeToTask_NotFound(t *testing.T) {
	ctx := t.Context()
	taskID := a2a.NewTaskID()
	wantErr := a2a.ErrTaskNotFound
	executor := &mockAgentExecutor{}
	handler := NewHandler(executor)

	result, err := collectEvents(handler.OnResubscribeToTask(ctx, &a2a.TaskIDParams{ID: taskID}))

	if result != nil || !errors.Is(err, wantErr) {
		t.Errorf("OnResubscribeToTask() = (%v, %v), want error %v", result, err, wantErr)
	}
}

func TestDefaultRequestHandler_OnCancelTask_AgentCancelFails(t *testing.T) {
	ctx := t.Context()
	taskToCancel := &a2a.Task{ID: a2a.NewTaskID(), ContextID: a2a.NewContextID(), Status: a2a.TaskStatus{State: a2a.TaskStateWorking}}
	wantErr := fmt.Errorf("failed to cancel: cancelation failed: agent cancel error")
	store := testutil.NewTestTaskStore().WithTasks(t, taskToCancel)
	executor := &mockAgentExecutor{
		CancelFunc: func(ctx context.Context, reqCtx *RequestContext, q eventqueue.Queue) error {
			return wantErr
		},
	}
	handler := NewHandler(executor, WithTaskStore(store))

	result, err := handler.OnCancelTask(ctx, &a2a.TaskIDParams{ID: taskToCancel.ID})
	if result != nil || !errors.Is(err, wantErr) {
		t.Errorf("OnCancelTask() error = %v, wantErr %v", err, wantErr)
	}
}

func TestDefaultRequestHandler_MultipleRequestContextInterceptors(t *testing.T) {
	ctx := t.Context()
	executor := newEventReplayAgent([]a2a.Event{a2a.NewMessage(a2a.MessageRoleAgent, a2a.TextPart{Text: "Hello!"})}, nil)
	type key1Type struct{}
	key1, val1 := key1Type{}, 2
	interceptor1 := interceptReqCtxFn(func(ctx context.Context, reqCtx *RequestContext) (context.Context, error) {
		return context.WithValue(ctx, key1, val1), nil
	})
	type key2Type struct{}
	key2, val2 := key2Type{}, 43
	interceptor2 := interceptReqCtxFn(func(ctx context.Context, reqCtx *RequestContext) (context.Context, error) {
		return context.WithValue(ctx, key2, val2), nil
	})
	handler := NewHandler(
		executor,
		WithRequestContextInterceptor(interceptor1),
		WithRequestContextInterceptor(interceptor2),
	)

	_, err := handler.OnSendMessage(ctx, &a2a.MessageSendParams{Message: &a2a.Message{}})
	if err != nil {
		t.Fatalf("handler.OnSendMessage() failed: %v", err)
	}

	capturedContext := executor.capturedContext
	if capturedContext.Value(key1) != val1 || capturedContext.Value(key2) != val2 {
		t.Fatalf("Execute() context = %+v, want to have interceptor attached values", capturedContext)
	}
}

func TestDefaultRequestHandler_RequestContextInterceptorRejectsRequest(t *testing.T) {
	ctx := t.Context()
	executor := newEventReplayAgent([]a2a.Event{a2a.NewMessage(a2a.MessageRoleAgent, a2a.TextPart{Text: "Hello!"})}, nil)
	wantErr := errors.New("rejected")
	interceptor := interceptReqCtxFn(func(ctx context.Context, reqCtx *RequestContext) (context.Context, error) {
		return ctx, wantErr
	})
	handler := NewHandler(executor, WithRequestContextInterceptor(interceptor))

	_, err := handler.OnSendMessage(ctx, &a2a.MessageSendParams{Message: &a2a.Message{}})

	if !errors.Is(err, wantErr) {
		t.Fatalf("handler.OnSendMessage() error = %v, want %v", err, wantErr)
	}
	if executor.executeCalled {
		t.Fatal("want agent executor to no be called")
	}
}

func TestDefaultRequestHandler_Unimplemented(t *testing.T) {
	handler := NewHandler(&mockAgentExecutor{})
	ctx := t.Context()

	if _, err := handler.OnGetTaskPushConfig(ctx, &a2a.GetTaskPushConfigParams{}); !errors.Is(err, ErrUnimplemented) {
		t.Errorf("OnGetTaskPushConfig: expected unimplemented error, got %v", err)
	}
	if _, err := handler.OnListTaskPushConfig(ctx, &a2a.ListTaskPushConfigParams{}); !errors.Is(err, ErrUnimplemented) {
		t.Errorf("OnListTaskPushConfig: expected unimplemented error, got %v", err)
	}
	if _, err := handler.OnSetTaskPushConfig(ctx, &a2a.TaskPushConfig{}); !errors.Is(err, ErrUnimplemented) {
		t.Errorf("OnSetTaskPushConfig: expected unimplemented error, got %v", err)
	}
	if err := handler.OnDeleteTaskPushConfig(ctx, &a2a.DeleteTaskPushConfigParams{}); !errors.Is(err, ErrUnimplemented) {
		t.Errorf("OnDeleteTaskPushConfig: expected unimplemented error, got %v", err)
	}
}

type interceptReqCtxFn func(context.Context, *RequestContext) (context.Context, error)

func (fn interceptReqCtxFn) Intercept(ctx context.Context, reqCtx *RequestContext) (context.Context, error) {
	return fn(ctx, reqCtx)
}

// mockAgentExecutor is a mock of AgentExecutor.
type mockAgentExecutor struct {
	executeCalled      bool
	capturedContext    context.Context
	capturedReqContext *RequestContext

	ExecuteFunc func(ctx context.Context, reqCtx *RequestContext, queue eventqueue.Queue) error
	CancelFunc  func(ctx context.Context, reqCtx *RequestContext, queue eventqueue.Queue) error
}

func (m *mockAgentExecutor) Execute(ctx context.Context, reqCtx *RequestContext, queue eventqueue.Queue) error {
	m.executeCalled = true
	m.capturedContext = ctx
	m.capturedReqContext = reqCtx
	if m.ExecuteFunc != nil {
		return m.ExecuteFunc(ctx, reqCtx, queue)
	}
	return nil
}

func (m *mockAgentExecutor) Cancel(ctx context.Context, reqCtx *RequestContext, queue eventqueue.Queue) error {
	if m.CancelFunc != nil {
		return m.CancelFunc(ctx, reqCtx, queue)
	}
	return errors.New("Cancel() not implemented")
}

func newEventReplayAgent(toSend []a2a.Event, err error) *mockAgentExecutor {
	return &mockAgentExecutor{
		ExecuteFunc: func(ctx context.Context, reqCtx *RequestContext, q eventqueue.Queue) error {
			for _, event := range toSend {
				if err := q.Write(ctx, event); err != nil {
					return err
				}
			}
			return err
		},
	}
}

func newTestHandler(opts ...RequestHandlerOption) RequestHandler {
	mockExec := newEventReplayAgent([]a2a.Event{}, nil)
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
	status.Timestamp = &fixedTime
	return &a2a.Task{ID: task.ID, ContextID: task.ContextID, Status: status}
}

func newTaskWithMeta(task *a2a.Task, meta map[string]any) *a2a.Task {
	return &a2a.Task{ID: task.ID, ContextID: task.ContextID, Metadata: meta}
}

func newArtifactEvent(task *a2a.Task, aid a2a.ArtifactID, parts ...a2a.Part) *a2a.TaskArtifactUpdateEvent {
	ev := a2a.NewArtifactEvent(task, parts...)
	ev.Artifact.ID = aid
	return ev
}

func collectEvents(seq iter.Seq2[a2a.Event, error]) ([]a2a.Event, error) {
	var events []a2a.Event
	for event, err := range seq {
		if err != nil {
			return events, err
		}
		events = append(events, event)
	}
	return events, nil
}
