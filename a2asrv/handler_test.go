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
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
	"github.com/a2aproject/a2a-go/internal/taskstore"
	"github.com/google/go-cmp/cmp"
)

var (
	taskID             = a2a.TaskID("test-task")
	storeGetFailTaskID = a2a.TaskID("store-get-fails")
	notExistsTaskID    = a2a.TaskID("not-exists")
)

var fixedTime = time.Now()

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

// mockQueueManager is a mock of eventqueue.Manager
type mockQueueManager struct {
	GetOrCreateFunc func(ctx context.Context, taskID a2a.TaskID) (eventqueue.Queue, error)
	GetFunc         func(ctx context.Context, taskID a2a.TaskID) (eventqueue.Queue, bool)
	DestroyFunc     func(ctx context.Context, taskID a2a.TaskID) error
}

func (m *mockQueueManager) GetOrCreate(ctx context.Context, taskID a2a.TaskID) (eventqueue.Queue, error) {
	if m.GetOrCreateFunc != nil {
		return m.GetOrCreateFunc(ctx, taskID)
	}
	return nil, errors.New("GetOrCreate() not implemented")
}

func (m *mockQueueManager) Get(ctx context.Context, taskID a2a.TaskID) (eventqueue.Queue, bool) {
	if m.GetFunc != nil {
		return m.GetFunc(ctx, taskID)
	}
	return nil, false
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
	return nil
}

func (m *mockEventQueue) Close() error {
	if m.CloseFunc != nil {
		return m.CloseFunc()
	}
	return nil
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

// mockTaskStore is a mock of TaskStore
type mockTaskStore struct {
	SaveFunc func(ctx context.Context, task *a2a.Task) error
	GetFunc  func(ctx context.Context, taskID a2a.TaskID) (*a2a.Task, error)
}

func (m *mockTaskStore) Save(ctx context.Context, task *a2a.Task) error {
	if m.SaveFunc != nil {
		return m.SaveFunc(ctx, task)
	}
	return errors.New("Save() not implemented")
}

func (m *mockTaskStore) Get(ctx context.Context, taskID a2a.TaskID) (*a2a.Task, error) {
	if m.GetFunc != nil {
		return m.GetFunc(ctx, taskID)
	}
	return nil, errors.New("Get() not implemented")
}

func newTestHandler(opts ...RequestHandlerOption) RequestHandler {
	mockExec := &mockAgentExecutor{
		ExecuteFunc: func(ctx context.Context, reqCtx *RequestContext, q eventqueue.Queue) error {
			return nil
		},
	}
	return NewHandler(mockExec, opts...)
}

func newMemTaskStore(t *testing.T, seed []*a2a.Task) TaskStore {
	t.Helper()
	store := taskstore.NewMem()
	for _, task := range seed {
		if err := store.Save(t.Context(), task); err != nil {
			t.Errorf("store.Save() error = %v", err)
		}
	}
	return store
}

func newUserMessage(task *a2a.Task, text string) *a2a.Message {
	return &a2a.Message{
		ID:     "message-id",
		Parts:  []a2a.Part{a2a.TextPart{Text: text}},
		Role:   a2a.MessageRoleUser,
		TaskID: task.ID,
	}
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

func newArtifactEvent(task *a2a.Task, aid a2a.ArtifactID, parts ...a2a.Part) *a2a.TaskArtifactUpdateEvent {
	ev := a2a.NewArtifactEvent(task, parts...)
	ev.Artifact.ID = aid
	return ev
}

func TestDefaultRequestHandler_OnSendMessage_NoTaskCreated(t *testing.T) {
	ctx := t.Context()
	getCalled := 0
	savedCalled := 0
	mockStore := &mockTaskStore{
		GetFunc: func(ctx context.Context, taskID a2a.TaskID) (*a2a.Task, error) {
			getCalled += 1
			return nil, nil
		},
		SaveFunc: func(ctx context.Context, task *a2a.Task) error {
			savedCalled += 1
			return nil
		},
	}
	executor := newEventReplayAgent([]a2a.Event{newAgentMessage("hello")}, nil)
	handler := NewHandler(executor, WithTaskStore(mockStore))

	result, gotErr := handler.OnSendMessage(ctx, &a2a.MessageSendParams{Message: &a2a.Message{}})
	if gotErr != nil {
		t.Fatalf("OnSendMessage() error = %v, wantErr nil", gotErr)
	}
	if _, ok := result.(*a2a.Message); !ok {
		t.Fatalf("OnSendMessage() = %v, want a2a.Message", result)
	}

	if getCalled > 0 {
		t.Fatalf("OnSendMessage() TaskStore.Get called %d times, want 0", getCalled)
	}
	if savedCalled > 0 {
		t.Fatalf("OnSendMessage() TaskStore.Save called %d times, want 0", savedCalled)
	}
}

func TestDefaultRequestHandler_OnSendMessage_NewTaskHistory(t *testing.T) {
	ctx := t.Context()
	mockStore := taskstore.NewMem()
	executor := &mockAgentExecutor{
		ExecuteFunc: func(ctx context.Context, reqCtx *RequestContext, q eventqueue.Queue) error {
			event := a2a.NewStatusUpdateEvent(reqCtx.Task, a2a.TaskStateCompleted, nil)
			event.Final = true
			return q.Write(ctx, event)
		},
	}
	handler := NewHandler(executor, WithTaskStore(mockStore))

	msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: "Complete the task!"})
	result, gotErr := handler.OnSendMessage(ctx, &a2a.MessageSendParams{Message: msg})
	if gotErr != nil {
		t.Fatalf("OnSendMessage() error = %v, wantErr nil", gotErr)
	}
	if task, ok := result.(*a2a.Task); ok {
		if diff := cmp.Diff([]*a2a.Message{msg}, task.History); diff != "" {
			t.Fatalf("OnSendMessage() wrong result (+got,-want):\ngot = %v\nwant = %v\ndiff = %s", task.History, []*a2a.Message{msg}, diff)
		}
	} else {
		t.Fatalf("OnSendMessage() = %v, want a2a.Task", result)
	}
}

func TestDefaultRequestHandler_OnSendMessage(t *testing.T) {
	artifactID := a2a.NewArtifactID()
	taskSeed := &a2a.Task{ID: a2a.NewTaskID(), ContextID: a2a.NewContextID()}
	inputRequiredTaskSeed := &a2a.Task{ID: a2a.NewTaskID(), ContextID: a2a.NewContextID(), Status: a2a.TaskStatus{State: a2a.TaskStateInputRequired}}
	completedTaskSeed := &a2a.Task{ID: a2a.NewTaskID(), ContextID: a2a.NewContextID(), Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}}
	taskStoreSeed := []*a2a.Task{taskSeed, inputRequiredTaskSeed, completedTaskSeed}

	type testCase struct {
		name        string
		input       *a2a.MessageSendParams
		agentEvents []a2a.Event
		wantResult  a2a.SendMessageResult
		wantErr     error
	}

	createTestCases := func() []testCase {
		return []testCase{
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
				name: "final task overwrites intermediate status updates",
				agentEvents: []a2a.Event{
					newTaskStatusUpdate(taskSeed, a2a.TaskStateSubmitted, "Ack"),
					newTaskStatusUpdate(taskSeed, a2a.TaskStateWorking, "Working..."),
					newTaskWithStatus(taskSeed, a2a.TaskStateCompleted, "no status change history"),
				},
				wantResult: newTaskWithStatus(taskSeed, a2a.TaskStateCompleted, "no status change history"),
			},
			{
				name:  "event final flag takes precedence over task state",
				input: &a2a.MessageSendParams{Message: newUserMessage(taskSeed, "Work")},
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
					History: []*a2a.Message{newUserMessage(taskSeed, "Work"), newAgentMessage("Working...")},
				},
			},
			{
				name:  "task status update accumulation",
				input: &a2a.MessageSendParams{Message: newUserMessage(taskSeed, "Syn")},
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
						newUserMessage(taskSeed, "Syn"),
						newAgentMessage("Ack"),
						newAgentMessage("Working..."),
					},
				},
			},
			{
				name:  "input-required task status update",
				input: &a2a.MessageSendParams{Message: newUserMessage(taskSeed, "Syn")},
				agentEvents: []a2a.Event{
					newTaskStatusUpdate(taskSeed, a2a.TaskStateSubmitted, "Ack"),
					newTaskStatusUpdate(taskSeed, a2a.TaskStateWorking, "Working..."),
					newFinalTaskStatusUpdate(taskSeed, a2a.TaskStateInputRequired, "Need more input!"),
				},
				wantResult: &a2a.Task{
					ID:        taskSeed.ID,
					ContextID: taskSeed.ContextID,
					Status: a2a.TaskStatus{
						State:     a2a.TaskStateInputRequired,
						Message:   newAgentMessage("Need more input!"),
						Timestamp: &fixedTime,
					},
					History: []*a2a.Message{
						newUserMessage(taskSeed, "Syn"),
						newAgentMessage("Ack"),
						newAgentMessage("Working..."),
					},
				},
			},
			{
				name:  "task artifact streaming",
				input: &a2a.MessageSendParams{Message: newUserMessage(taskSeed, "Syn")},
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
					History:   []*a2a.Message{newUserMessage(taskSeed, "Syn"), newAgentMessage("Ack")},
					Artifacts: []*a2a.Artifact{
						{ID: artifactID, Parts: a2a.ContentParts{a2a.TextPart{Text: "Hello"}, a2a.TextPart{Text: ", world!"}}},
					},
				},
			},
			{
				name:  "task with multiple artifacts",
				input: &a2a.MessageSendParams{Message: newUserMessage(taskSeed, "Syn")},
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
					History:   []*a2a.Message{newUserMessage(taskSeed, "Syn"), newAgentMessage("Ack")},
					Artifacts: []*a2a.Artifact{
						{ID: artifactID, Parts: a2a.ContentParts{a2a.TextPart{Text: "Hello"}}},
						{ID: artifactID + "2", Parts: a2a.ContentParts{a2a.TextPart{Text: "World"}}},
					},
				},
			},
			{
				name: "task continuation",
				input: &a2a.MessageSendParams{
					Message: newUserMessage(inputRequiredTaskSeed, "continue"),
				},
				agentEvents: []a2a.Event{
					newTaskStatusUpdate(inputRequiredTaskSeed, a2a.TaskStateWorking, "Working..."),
					newFinalTaskStatusUpdate(inputRequiredTaskSeed, a2a.TaskStateCompleted, "Done!"),
				},
				wantResult: &a2a.Task{
					ID:        inputRequiredTaskSeed.ID,
					ContextID: inputRequiredTaskSeed.ContextID,
					Status: a2a.TaskStatus{
						State:     a2a.TaskStateCompleted,
						Message:   newAgentMessage("Done!"),
						Timestamp: &fixedTime,
					},
					History: []*a2a.Message{
						newUserMessage(inputRequiredTaskSeed, "continue"),
						newAgentMessage("Working..."),
					},
				},
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
				wantErr: fmt.Errorf("task in a terminal state %q: %w", a2a.TaskStateCompleted, a2a.ErrInvalidRequest),
			},
		}
	}

	for _, tt := range createTestCases() {
		input := &a2a.MessageSendParams{Message: &a2a.Message{TaskID: taskSeed.ID}}
		if tt.input != nil {
			input = tt.input
		}

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			store := newMemTaskStore(t, taskStoreSeed)
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
	}

	for _, tt := range createTestCases() {
		input := &a2a.MessageSendParams{Message: &a2a.Message{TaskID: taskSeed.ID}}
		if tt.input != nil {
			input = tt.input
		}

		t.Run(tt.name+" (streaming)", func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			store := newMemTaskStore(t, taskStoreSeed)
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

func TestDefaultRequestHandler_OnSendMessage_AuthRequired(t *testing.T) {
	ctx := t.Context()
	mockStore := taskstore.NewMem()
	authCredentialsChan := make(chan struct{})
	executor := &mockAgentExecutor{
		ExecuteFunc: func(ctx context.Context, reqCtx *RequestContext, q eventqueue.Queue) error {
			if err := q.Write(ctx, a2a.NewStatusUpdateEvent(reqCtx.Task, a2a.TaskStateAuthRequired, nil)); err != nil {
				return err
			}
			<-authCredentialsChan
			result := a2a.NewStatusUpdateEvent(reqCtx.Task, a2a.TaskStateCompleted, nil)
			result.Final = true
			return q.Write(ctx, result)
		},
	}
	handler := NewHandler(executor, WithTaskStore(mockStore))

	msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: "perform protected operation"})
	result, err := handler.OnSendMessage(ctx, &a2a.MessageSendParams{Message: msg})
	if err != nil {
		t.Fatalf("OnSendMessage() error = %v, wantErr nil", err)
	}
	var taskID a2a.TaskID
	if task, ok := result.(*a2a.Task); ok {
		if task.Status.State != a2a.TaskStateAuthRequired {
			t.Fatalf("OnSendMessage() = %v, want a2a.Task in %q state", result, a2a.TaskStateAuthRequired)
		}
		msg.TaskID = task.ID
		taskID = task.ID
	} else {
		t.Fatalf("OnSendMessage() = %v, want a2a.Task", result)
	}

	_, err = handler.OnSendMessage(ctx, &a2a.MessageSendParams{Message: msg})
	if !strings.Contains(err.Error(), "execution is already in progress") {
		t.Fatalf("OnSendMessage() error = %v, want err to contain 'execution is already in progress'", err)
	}

	authCredentialsChan <- struct{}{}
	time.Sleep(time.Millisecond * 10)

	task, err := handler.OnGetTask(ctx, &a2a.TaskQueryParams{ID: taskID})
	if task.Status.State != a2a.TaskStateCompleted {
		t.Fatalf("handler.OnGetTask() = (%v, %v), want a task in state %q", task, err, a2a.TaskStateCompleted)
	}
}

func TestDefaultRequestHandler_OnSendMessageStreaming_AuthRequired(t *testing.T) {
	ctx := t.Context()
	mockStore := taskstore.NewMem()
	authCredentialsChan := make(chan struct{})
	executor := &mockAgentExecutor{
		ExecuteFunc: func(ctx context.Context, reqCtx *RequestContext, q eventqueue.Queue) error {
			if err := q.Write(ctx, a2a.NewStatusUpdateEvent(reqCtx.Task, a2a.TaskStateAuthRequired, nil)); err != nil {
				return err
			}
			<-authCredentialsChan
			result := a2a.NewStatusUpdateEvent(reqCtx.Task, a2a.TaskStateCompleted, nil)
			result.Final = true
			return q.Write(ctx, result)
		},
	}
	handler := NewHandler(executor, WithTaskStore(mockStore))

	var lastEvent a2a.Event
	msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: "perform protected operation"})
	for event, err := range handler.OnSendMessageStream(ctx, &a2a.MessageSendParams{Message: msg}) {
		if upd, ok := event.(*a2a.TaskStatusUpdateEvent); ok && upd.Status.State == a2a.TaskStateAuthRequired {
			go func() { authCredentialsChan <- struct{}{} }()
		}
		if err != nil {
			t.Fatalf("OnSendMessageStream() error = %v, wantErr nil", err)
		}
		lastEvent = event
	}

	if task, ok := lastEvent.(*a2a.TaskStatusUpdateEvent); ok {
		if task.Status.State != a2a.TaskStateCompleted {
			t.Fatalf("OnSendMessageStream() = %v, want status update with state %q", lastEvent, a2a.TaskStateAuthRequired)
		}
	} else {
		t.Fatalf("OnSendMessageStream() = %v, want a2a.TaskStatusUpdateEvent", lastEvent)
	}
}

func TestDefaultRequestHandler_OnGetTask(t *testing.T) {
	ptr := func(i int) *int {
		return &i
	}

	history := []*a2a.Message{{ID: "test-message-1"}, {ID: "test-message-2"}, {ID: "test-message-3"}}

	tests := []struct {
		name      string
		query     *a2a.TaskQueryParams
		wantEvent a2a.Event
		wantErr   error
	}{
		{
			name:      "success with TaskID",
			query:     &a2a.TaskQueryParams{ID: taskID},
			wantEvent: &a2a.Task{ID: taskID, History: history},
		},
		{
			name:    "missing TaskID",
			query:   &a2a.TaskQueryParams{ID: ""},
			wantErr: fmt.Errorf("missing TaskID: %w", a2a.ErrInvalidRequest),
		},
		{
			name:    "store Get() fails",
			query:   &a2a.TaskQueryParams{ID: storeGetFailTaskID},
			wantErr: errors.New("failed to get task: store get failed"),
		},
		{
			name:    "task not found",
			query:   &a2a.TaskQueryParams{ID: notExistsTaskID},
			wantErr: fmt.Errorf("failed to get task: %w", a2a.ErrTaskNotFound),
		},
		{
			name:      "get task with limited HistoryLength",
			query:     &a2a.TaskQueryParams{ID: taskID, HistoryLength: ptr(len(history) - 1)},
			wantEvent: &a2a.Task{ID: taskID, History: history[1:]},
		},
		{
			name:      "get task with larger than available HistoryLength",
			query:     &a2a.TaskQueryParams{ID: taskID, HistoryLength: ptr(len(history) + 1)},
			wantEvent: &a2a.Task{ID: taskID, History: history},
		},
		{
			name:      "get task with zero HistoryLength",
			query:     &a2a.TaskQueryParams{ID: taskID, HistoryLength: ptr(0)},
			wantEvent: &a2a.Task{ID: taskID, History: make([]*a2a.Message, 0)},
		},
		{
			name:      "get task with negative HistoryLength",
			query:     &a2a.TaskQueryParams{ID: taskID, HistoryLength: ptr(-1)},
			wantEvent: &a2a.Task{ID: taskID, History: make([]*a2a.Message, 0)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			taskStore := &mockTaskStore{
				GetFunc: func(ctx context.Context, taskID a2a.TaskID) (*a2a.Task, error) {
					if taskID == storeGetFailTaskID {
						return nil, errors.New("store get failed")
					}
					if taskID == notExistsTaskID {
						return nil, a2a.ErrTaskNotFound
					}
					return &a2a.Task{ID: taskID, History: history}, nil
				},
			}
			handler := newTestHandler(WithTaskStore(taskStore))

			result, gotErr := handler.OnGetTask(ctx, tt.query)

			if tt.wantErr == nil {
				if gotErr != nil {
					t.Fatalf("OnGetTask() error = %v, wantErr nil", gotErr)
				}

				if diff := cmp.Diff(result, tt.wantEvent); diff != "" {
					t.Errorf("OnGetTask() got = %v, want %v", result, tt.wantEvent)
				}
			} else {
				if gotErr == nil {
					t.Fatalf("OnGetTask() error = nil, wantErr %q", tt.wantErr)
				}
				if gotErr.Error() != tt.wantErr.Error() {
					t.Errorf("OnGetTask() error = %v, wantErr %v", gotErr, tt.wantErr)
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
	handler := NewHandler(&mockAgentExecutor{}, WithEventQueueManager(qm))

	result, err := handler.OnSendMessage(ctx, &a2a.MessageSendParams{Message: &a2a.Message{}})

	if result != nil || !errors.Is(err, wantErr) {
		t.Fatalf("handler.OnSendMessage() = (%v, %v), want error %v", result, err, wantErr)
	}
}

func TestDefaultRequestHandler_OnSendMessage_QueueReadFails(t *testing.T) {
	ctx := t.Context()
	wantErr := errors.New("Read() failed")
	queue := &mockEventQueue{
		ReadFunc: func(context.Context) (a2a.Event, error) {
			return nil, wantErr
		},
	}
	qm := &mockQueueManager{
		GetOrCreateFunc: func(ctx context.Context, id a2a.TaskID) (eventqueue.Queue, error) {
			return queue, nil
		},
	}
	handler := NewHandler(&mockAgentExecutor{}, WithEventQueueManager(qm))

	result, err := handler.OnSendMessage(ctx, &a2a.MessageSendParams{Message: &a2a.Message{}})

	if result != nil || !errors.Is(err, wantErr) {
		t.Fatalf("handler.OnSendMessage() = (%v, %v), want error %v", result, err, wantErr)
	}
}

func TestDefaultRequestHandler_OnSendMessage_RelatedTaskLoading(t *testing.T) {
	existingTask := &a2a.Task{ID: a2a.NewTaskID(), ContextID: a2a.NewContextID()}
	ctx := t.Context()
	store := taskstore.NewMem()
	_ = store.Save(ctx, existingTask)
	executor := newEventReplayAgent([]a2a.Event{a2a.NewMessage(a2a.MessageRoleAgent, a2a.TextPart{Text: "Hello!"})}, nil)
	handler := NewHandler(executor, WithRequestContextInterceptor(&ReferencedTasksLoader{Store: store}))

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

func TestDefaultRequestHandler_OnSetTaskPushConfig(t *testing.T) {
	ctx := t.Context()
	taskID := a2a.TaskID("test-task")

	testCases := []struct {
		name       string
		params     *a2a.TaskPushConfig
		wantErrStr string
	}{
		{
			name: "valid config with id",
			params: &a2a.TaskPushConfig{
				TaskID: taskID,
				Config: a2a.PushConfig{ID: "config-1", URL: "https://example.com/push"},
			},
		},
		{
			name: "valid config without id",
			params: &a2a.TaskPushConfig{
				TaskID: taskID,
				Config: a2a.PushConfig{URL: "https://example.com/push-no-id"},
			},
		},
		{
			name: "invalid config - empty URL",
			params: &a2a.TaskPushConfig{
				TaskID: taskID,
				Config: a2a.PushConfig{ID: "config-invalid"},
			},
			wantErrStr: "push config endpoint cannot be empty",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			handler := newTestHandler()
			got, err := handler.OnSetTaskPushConfig(ctx, tc.params)

			if tc.wantErrStr != "" {
				if err == nil || err.Error() != tc.wantErrStr {
					t.Fatalf("OnSetTaskPushConfig() error = %v, want %v", err, tc.wantErrStr)
				}
				return
			}

			if err != nil {
				t.Fatalf("OnSetTaskPushConfig() failed: %v", err)
			}

			if got.Config.ID == "" {
				t.Error("OnSetTaskPushConfig() expected a generated ID, but it was empty")
			}

			if diff := cmp.Diff(tc.params, got); diff != "" {
				t.Errorf("OnSetTaskPushConfig() mismatch (-want +got):\n%s", diff)
			}

			stored, err := handler.OnGetTaskPushConfig(ctx, &a2a.GetTaskPushConfigParams{TaskID: taskID, ConfigID: got.Config.ID})
			if err != nil {
				t.Fatalf("OnGetTaskPushConfig() for verification failed: %v", err)
			}
			if diff := cmp.Diff(got, stored); diff != "" {
				t.Errorf("Stored config mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDefaultRequestHandler_OnGetTaskPushConfig(t *testing.T) {
	ctx := t.Context()
	taskID := a2a.TaskID("test-task")
	config1 := a2a.PushConfig{ID: "config-1", URL: "https://example.com/push1"}

	handler := newTestHandler()
	_, err := handler.OnSetTaskPushConfig(ctx, &a2a.TaskPushConfig{TaskID: taskID, Config: config1})
	if err != nil {
		t.Fatalf("Setup: OnSetTaskPushConfig() failed: %v", err)
	}

	testCases := []struct {
		name    string
		params  *a2a.GetTaskPushConfigParams
		want    *a2a.TaskPushConfig
		wantErr error
	}{
		{
			name:   "success",
			params: &a2a.GetTaskPushConfigParams{TaskID: taskID, ConfigID: config1.ID},
			want:   &a2a.TaskPushConfig{TaskID: taskID, Config: config1},
		},
		{
			name:    "non-existent config",
			params:  &a2a.GetTaskPushConfigParams{TaskID: taskID, ConfigID: "non-existent"},
			wantErr: a2a.ErrPushConfigNotFound,
		},
		{
			name:    "non-existent task",
			params:  &a2a.GetTaskPushConfigParams{TaskID: "non-existent-task", ConfigID: config1.ID},
			wantErr: a2a.ErrPushConfigNotFound,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := handler.OnGetTaskPushConfig(ctx, tc.params)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("OnGetTaskPushConfig() error = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr == nil {
				if diff := cmp.Diff(tc.want, got); diff != "" {
					t.Errorf("OnGetTaskPushConfig() mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

func TestDefaultRequestHandler_OnListTaskPushConfig(t *testing.T) {
	ctx := t.Context()
	taskID := a2a.TaskID("test-task")
	config1 := a2a.PushConfig{ID: "config-1", URL: "https://example.com/push1"}
	config2 := a2a.PushConfig{ID: "config-2", URL: "https://example.com/push2"}
	emptyTaskID := a2a.TaskID("empty-task")

	handler := newTestHandler()
	if _, err := handler.OnSetTaskPushConfig(ctx, &a2a.TaskPushConfig{TaskID: taskID, Config: config1}); err != nil {
		t.Fatalf("Setup: OnSetTaskPushConfig() for config1 failed: %v", err)
	}
	if _, err := handler.OnSetTaskPushConfig(ctx, &a2a.TaskPushConfig{TaskID: taskID, Config: config2}); err != nil {
		t.Fatalf("Setup: OnSetTaskPushConfig() for config2 failed: %v", err)
	}
	if _, err := handler.OnSetTaskPushConfig(ctx, &a2a.TaskPushConfig{TaskID: emptyTaskID, Config: config1}); err != nil {
		t.Fatalf("Setup: OnSetTaskPushConfig() for empty task failed: %v", err)
	}
	if err := handler.OnDeleteTaskPushConfig(ctx, &a2a.DeleteTaskPushConfigParams{TaskID: emptyTaskID, ConfigID: config1.ID}); err != nil {
		t.Fatalf("Setup: OnDeleteTaskPushConfig() for empty task failed: %v", err)
	}

	testCases := []struct {
		name   string
		params *a2a.ListTaskPushConfigParams
		want   []*a2a.TaskPushConfig
	}{
		{
			name:   "list existing",
			params: &a2a.ListTaskPushConfigParams{TaskID: taskID},
			want: []*a2a.TaskPushConfig{
				{TaskID: taskID, Config: config1},
				{TaskID: taskID, Config: config2},
			},
		},
		{
			name:   "list with empty task",
			params: &a2a.ListTaskPushConfigParams{TaskID: emptyTaskID},
			want:   []*a2a.TaskPushConfig{},
		},
		{
			name:   "list non-existent task",
			params: &a2a.ListTaskPushConfigParams{TaskID: "non-existent-task"},
			want:   []*a2a.TaskPushConfig{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := handler.OnListTaskPushConfig(ctx, tc.params)
			if err != nil {
				t.Fatalf("OnListTaskPushConfig() failed: %v", err)
			}
			sort.Slice(got, func(i, j int) bool { return got[i].Config.ID < got[j].Config.ID })
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("OnListTaskPushConfig() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDefaultRequestHandler_OnDeleteTaskPushConfig(t *testing.T) {
	ctx := t.Context()
	taskID := a2a.TaskID("test-task")
	config1 := a2a.PushConfig{ID: "config-1", URL: "https://example.com/push1"}
	config2 := a2a.PushConfig{ID: "config-2", URL: "https://example.com/push2"}

	testCases := []struct {
		name       string
		params     *a2a.DeleteTaskPushConfigParams
		wantRemain []*a2a.TaskPushConfig
	}{
		{
			name:       "delete existing",
			params:     &a2a.DeleteTaskPushConfigParams{TaskID: taskID, ConfigID: config1.ID},
			wantRemain: []*a2a.TaskPushConfig{{TaskID: taskID, Config: config2}},
		},
		{
			name:   "delete non-existent config",
			params: &a2a.DeleteTaskPushConfigParams{TaskID: taskID, ConfigID: "non-existent"},
			wantRemain: []*a2a.TaskPushConfig{
				{TaskID: taskID, Config: config1},
				{TaskID: taskID, Config: config2},
			},
		},
		{
			name:   "delete from non-existent task",
			params: &a2a.DeleteTaskPushConfigParams{TaskID: "non-existent-task", ConfigID: config1.ID},
			wantRemain: []*a2a.TaskPushConfig{
				{TaskID: taskID, Config: config1},
				{TaskID: taskID, Config: config2},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			handler := newTestHandler()
			if _, err := handler.OnSetTaskPushConfig(ctx, &a2a.TaskPushConfig{TaskID: taskID, Config: config1}); err != nil {
				t.Fatalf("Setup: OnSetTaskPushConfig() for config1 failed: %v", err)
			}
			if _, err := handler.OnSetTaskPushConfig(ctx, &a2a.TaskPushConfig{TaskID: taskID, Config: config2}); err != nil {
				t.Fatalf("Setup: OnSetTaskPushConfig() for config2 failed: %v", err)
			}

			err := handler.OnDeleteTaskPushConfig(ctx, tc.params)
			if err != nil {
				t.Fatalf("OnDeleteTaskPushConfig() failed: %v", err)
			}

			got, err := handler.OnListTaskPushConfig(ctx, &a2a.ListTaskPushConfigParams{TaskID: taskID})
			if err != nil {
				t.Fatalf("OnListTaskPushConfig() for verification failed: %v", err)
			}

			sort.Slice(got, func(i, j int) bool { return got[i].Config.ID < got[j].Config.ID })
			if diff := cmp.Diff(tc.wantRemain, got); diff != "" {
				t.Errorf("Remaining configs mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
