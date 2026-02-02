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
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
	"github.com/a2aproject/a2a-go/a2asrv/push"
	"github.com/a2aproject/a2a-go/internal/taskstore"
	"github.com/a2aproject/a2a-go/internal/testutil"
	"github.com/a2aproject/a2a-go/internal/utils"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

var fixedTime = time.Now()

func TestRequestHandler_OnSendMessage(t *testing.T) {
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
				name: "fails if unknown task state",
				input: &a2a.MessageSendParams{
					Message: newUserMessage(taskSeed, "Work"),
				},
				agentEvents: []a2a.Event{newTaskWithStatus(taskSeed, a2a.TaskStateUnknown, "...")},
				wantErr:     fmt.Errorf("unknown task state: unknown"),
			},
			{
				name: "final task overwrites intermediate task events",
				input: &a2a.MessageSendParams{
					Message: newUserMessage(taskSeed, "Work"),
				},
				agentEvents: []a2a.Event{
					newTaskWithMeta(taskSeed, map[string]any{"foo": "bar"}),
					newTaskWithStatus(taskSeed, a2a.TaskStateCompleted, "meta lost"),
				},
				wantResult: newTaskWithStatus(taskSeed, a2a.TaskStateCompleted, "meta lost"),
			},
			{
				name: "final task overwrites intermediate status updates",
				input: &a2a.MessageSendParams{
					Message: newUserMessage(taskSeed, "Work"),
				},
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
				name:    "fails if no message",
				input:   &a2a.MessageSendParams{},
				wantErr: fmt.Errorf("message is required: %w", a2a.ErrInvalidParams),
			},
			{
				name: "fails if no message ID",
				input: &a2a.MessageSendParams{Message: &a2a.Message{
					Parts: a2a.ContentParts{a2a.TextPart{Text: "Test"}},
					Role:  a2a.MessageRoleUser,
				}},
				wantErr: fmt.Errorf("message ID is required: %w", a2a.ErrInvalidParams),
			},
			{
				name: "fails if no message parts",
				input: &a2a.MessageSendParams{Message: &a2a.Message{
					ID:   a2a.NewMessageID(),
					Role: a2a.MessageRoleUser,
				}},
				wantErr: fmt.Errorf("message parts is required: %w", a2a.ErrInvalidParams),
			},
			{
				name: "fails if no message role",
				input: &a2a.MessageSendParams{Message: &a2a.Message{
					ID:    a2a.NewMessageID(),
					Parts: a2a.ContentParts{a2a.TextPart{Text: "Test"}},
				}},
				wantErr: fmt.Errorf("message role is required: %w", a2a.ErrInvalidParams),
			},
			{
				name: "fails on non-existent task reference",
				input: &a2a.MessageSendParams{
					Message: &a2a.Message{
						TaskID: "non-existent",
						ID:     "test-message",
						Parts:  a2a.ContentParts{a2a.TextPart{Text: "Test"}},
						Role:   a2a.MessageRoleUser,
					},
				},
				wantErr: a2a.ErrTaskNotFound,
			},
			{
				name: "fails if contextID not equal to task contextID",
				input: &a2a.MessageSendParams{
					Message: &a2a.Message{TaskID: taskSeed.ID, ContextID: taskSeed.ContextID + "1", ID: "test-message"},
				},
				wantErr: a2a.ErrInvalidParams,
			},
			{
				name: "fails if message references completed task",
				input: &a2a.MessageSendParams{
					Message: newUserMessage(completedTaskSeed, "Test"),
				},
				wantErr: fmt.Errorf("setup failed: task in a terminal state %q: %w", a2a.TaskStateCompleted, a2a.ErrInvalidParams),
			},
		}
	}

	for _, tt := range createTestCases() {
		input := &a2a.MessageSendParams{Message: newUserMessage(taskSeed, "Test")}
		if tt.input != nil {
			input = tt.input
		}

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			store := testutil.NewTestTaskStore().WithTasks(t, taskStoreSeed...)
			executor := newEventReplayAgent(tt.agentEvents, nil)
			handler := NewHandler(executor, WithTaskStore(store))

			result, gotErr := handler.OnSendMessage(ctx, input)
			if tt.wantErr == nil {
				if gotErr != nil {
					t.Errorf("OnSendMessage() error = %v, wantErr nil", gotErr)
					return
				}
				if diff := cmp.Diff(tt.wantResult, result); diff != "" {
					t.Errorf("OnSendMessage() (+got,-want):\ngot = %v\nwant %v\ndiff = %s", result, tt.wantResult, diff)
				}
			} else {
				if gotErr == nil {
					t.Errorf("OnSendMessage() error = nil, wantErr %q", tt.wantErr)
					return
				}
				if gotErr.Error() != tt.wantErr.Error() && !errors.Is(gotErr, tt.wantErr) {
					t.Errorf("OnSendMessage() error = %v, wantErr %v", gotErr, tt.wantErr)
				}
			}
		})
	}

	for _, tt := range createTestCases() {
		input := &a2a.MessageSendParams{Message: newUserMessage(taskSeed, "Test")}
		if tt.input != nil {
			input = tt.input
		}

		t.Run(tt.name+" (streaming)", func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			store := testutil.NewTestTaskStore().WithTasks(t, taskStoreSeed...)
			executor := newEventReplayAgent(tt.agentEvents, nil)
			handler := NewHandler(executor, WithTaskStore(store))

			eventI := 0
			var streamErr error
			for got, gotErr := range handler.OnSendMessageStream(ctx, input) {
				if streamErr != nil {
					t.Errorf("handler.OnSendMessageStream() got (%v, %v) after error, want stream end", got, gotErr)
					return
				}

				if gotErr != nil && tt.wantErr == nil {
					t.Errorf("OnSendMessageStream() error = %v, wantErr nil", gotErr)
					return
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
					return
				}
			}
			if tt.wantErr == nil && eventI != len(tt.agentEvents) {
				t.Errorf("OnSendMessageStream() received %d events, want %d", eventI, len(tt.agentEvents))
				return
			}
			if tt.wantErr != nil && streamErr == nil {
				t.Errorf("OnSendMessageStream() error = nil, want %v", tt.wantErr)
				return
			}
			if tt.wantErr != nil && (streamErr.Error() != tt.wantErr.Error() && !errors.Is(streamErr, tt.wantErr)) {
				t.Errorf("OnSendMessageStream() error = %v, wantErr %v", streamErr, tt.wantErr)
			}
		})
	}
}

func TestRequestHandler_OnSendMessage_AuthRequired(t *testing.T) {
	ctx := t.Context()
	ts := testutil.NewTestTaskStore()
	authCredentialsChan := make(chan struct{})
	executor := &mockAgentExecutor{
		ExecuteFunc: func(ctx context.Context, reqCtx *RequestContext, q eventqueue.Queue) error {
			if err := q.Write(ctx, a2a.NewStatusUpdateEvent(reqCtx, a2a.TaskStateAuthRequired, nil)); err != nil {
				return err
			}
			<-authCredentialsChan
			result := a2a.NewStatusUpdateEvent(reqCtx, a2a.TaskStateCompleted, nil)
			result.Final = true
			return q.Write(ctx, result)
		},
	}
	handler := NewHandler(executor, WithTaskStore(ts))

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

func TestRequestHandler_OnSendMessage_NonBlocking(t *testing.T) {
	taskSeed := &a2a.Task{ID: a2a.NewTaskID(), ContextID: a2a.NewContextID(), Status: a2a.TaskStatus{State: a2a.TaskStateInputRequired}}

	createExecutor := func(generateEvent func(reqCtx *RequestContext) []a2a.Event) (*mockAgentExecutor, chan struct{}) {
		waitingChan := make(chan struct{})
		return &mockAgentExecutor{
			ExecuteFunc: func(ctx context.Context, reqCtx *RequestContext, q eventqueue.Queue) error {
				for i, event := range generateEvent(reqCtx) {
					if i > 0 {
						select {
						case <-waitingChan:
						case <-ctx.Done():
							return ctx.Err()
						}
					}
					if err := q.Write(ctx, event); err != nil {
						return err
					}
				}
				return nil
			},
		}, waitingChan
	}

	type testCase struct {
		name        string
		blocking    bool
		input       *a2a.MessageSendParams
		agentEvents func(reqCtx *RequestContext) []a2a.Event
		wantState   a2a.TaskState
		wantEvents  int
	}

	createTestCases := func() []testCase {
		return []testCase{
			{
				name:     "defaults to blocking",
				blocking: true,
				input:    &a2a.MessageSendParams{Message: newUserMessage(taskSeed, "Work"), Config: &a2a.MessageSendConfig{}},
				agentEvents: func(reqCtx *RequestContext) []a2a.Event {
					return []a2a.Event{
						newTaskWithStatus(reqCtx, a2a.TaskStateWorking, "Working..."),
						newTaskWithStatus(reqCtx, a2a.TaskStateCompleted, "Done"),
					}
				},
				wantState:  a2a.TaskStateCompleted,
				wantEvents: 2,
			},
			{
				name:  "non-terminal task state",
				input: &a2a.MessageSendParams{Message: newUserMessage(taskSeed, "Work"), Config: &a2a.MessageSendConfig{Blocking: utils.Ptr(false)}},
				agentEvents: func(reqCtx *RequestContext) []a2a.Event {
					return []a2a.Event{
						newTaskWithStatus(reqCtx, a2a.TaskStateWorking, "Working..."),
						newTaskWithStatus(reqCtx, a2a.TaskStateCompleted, "Done"),
					}
				},
				wantState:  a2a.TaskStateWorking,
				wantEvents: 2,
			},
			{
				name:  "non-final status update",
				input: &a2a.MessageSendParams{Message: newUserMessage(taskSeed, "Work"), Config: &a2a.MessageSendConfig{Blocking: utils.Ptr(false)}},
				agentEvents: func(reqCtx *RequestContext) []a2a.Event {
					return []a2a.Event{
						newTaskStatusUpdate(reqCtx, a2a.TaskStateWorking, "Working..."),
						newFinalTaskStatusUpdate(reqCtx, a2a.TaskStateCompleted, "Done!"),
					}
				},
				wantState:  a2a.TaskStateWorking,
				wantEvents: 2,
			},
			{
				name:  "artifact update update",
				input: &a2a.MessageSendParams{Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: "Work"}), Config: &a2a.MessageSendConfig{Blocking: utils.Ptr(false)}},
				agentEvents: func(reqCtx *RequestContext) []a2a.Event {
					return []a2a.Event{
						newArtifactEvent(reqCtx, a2a.NewArtifactID()),
						newFinalTaskStatusUpdate(reqCtx, a2a.TaskStateCompleted, "Done!"),
					}
				},
				wantState:  a2a.TaskStateSubmitted,
				wantEvents: 2,
			},
			{
				name:  "message for existing task",
				input: &a2a.MessageSendParams{Message: newUserMessage(taskSeed, "Work"), Config: &a2a.MessageSendConfig{Blocking: utils.Ptr(false)}},
				agentEvents: func(reqCtx *RequestContext) []a2a.Event {
					return []a2a.Event{
						newTaskStatusUpdate(taskSeed, a2a.TaskStateWorking, "Working..."),
						newFinalTaskStatusUpdate(taskSeed, a2a.TaskStateCompleted, "Done!"),
					}
				},
				wantState:  a2a.TaskStateWorking,
				wantEvents: 2,
			},
			{
				name:  "message",
				input: &a2a.MessageSendParams{Message: newUserMessage(taskSeed, "Work"), Config: &a2a.MessageSendConfig{Blocking: utils.Ptr(false)}},
				agentEvents: func(reqCtx *RequestContext) []a2a.Event {
					return []a2a.Event{
						a2a.NewMessageForTask(a2a.MessageRoleAgent, reqCtx, a2a.TextPart{Text: "Done"}),
						a2a.NewMessageForTask(a2a.MessageRoleAgent, reqCtx, a2a.TextPart{Text: "Done-2"}),
					}
				},
				wantEvents: 1, // streaming processing stops imeddiately after the first message
			},
		}
	}

	for _, tt := range createTestCases() {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			store := testutil.NewTestTaskStore().WithTasks(t, taskSeed)
			executor, waitingChan := createExecutor(tt.agentEvents)
			if tt.blocking {
				close(waitingChan)
			}
			handler := NewHandler(executor, WithTaskStore(store))

			result, gotErr := handler.OnSendMessage(ctx, tt.input)
			if !tt.blocking {
				close(waitingChan)
			}
			if gotErr != nil {
				t.Errorf("OnSendMessage() error = %v, wantErr nil", gotErr)
				return
			}
			if tt.wantState != a2a.TaskStateUnspecified {
				task, ok := result.(*a2a.Task)
				if !ok {
					t.Errorf("OnSendMessage() returned %T, want a2a.Task", result)
					return
				}
				if task.Status.State != tt.wantState {
					t.Errorf("OnSendMessage() task.State = %v, want %v", task.Status.State, tt.wantState)
				}
			} else {
				if _, ok := result.(*a2a.Message); !ok {
					t.Errorf("OnSendMessage() returned %T, want a2a.Message", result)
				}
			}
		})
	}

	for _, tt := range createTestCases() {
		t.Run(tt.name+" (streaming)", func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			store := testutil.NewTestTaskStore().WithTasks(t, taskSeed)
			executor, waitingChan := createExecutor(tt.agentEvents)
			close(waitingChan)
			handler := NewHandler(executor, WithTaskStore(store))

			gotEvents := 0
			for _, gotErr := range handler.OnSendMessageStream(ctx, tt.input) {
				if gotErr != nil {
					t.Errorf("OnSendMessageStream() error = %v, wantErr nil", gotErr)
				}
				gotEvents++
			}
			if gotEvents != tt.wantEvents {
				t.Errorf("OnSendMessageStream() event count = %d, want %d", gotEvents, tt.wantEvents)
			}
		})
	}
}

func TestRequestHandler_OnSendMessageStreaming_AuthRequired(t *testing.T) {
	ctx := t.Context()
	ts := testutil.NewTestTaskStore()
	authCredentialsChan := make(chan struct{})
	executor := &mockAgentExecutor{
		ExecuteFunc: func(ctx context.Context, reqCtx *RequestContext, q eventqueue.Queue) error {
			if err := q.Write(ctx, a2a.NewStatusUpdateEvent(reqCtx, a2a.TaskStateAuthRequired, nil)); err != nil {
				return err
			}
			<-authCredentialsChan
			result := a2a.NewStatusUpdateEvent(reqCtx, a2a.TaskStateCompleted, nil)
			result.Final = true
			return q.Write(ctx, result)
		},
	}
	handler := NewHandler(executor, WithTaskStore(ts))

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

func TestRequestHandler_OnSendMessage_PushNotifications(t *testing.T) {
	ctx := t.Context()

	taskSeed := &a2a.Task{ID: a2a.NewTaskID(), ContextID: a2a.NewContextID()}
	pushConfig := &a2a.PushConfig{URL: "https://example.com/push"}
	input := &a2a.MessageSendParams{
		Message: newUserMessage(taskSeed, "work"),
		Config: &a2a.MessageSendConfig{
			PushConfig: pushConfig,
		},
	}
	agentEvents := []a2a.Event{
		newFinalTaskStatusUpdate(taskSeed, a2a.TaskStateCompleted, "Done!"),
	}
	wantResult := newTaskWithStatus(taskSeed, a2a.TaskStateCompleted, "Done!")
	wantResult.History = []*a2a.Message{input.Message}

	store := testutil.NewTestTaskStore().WithTasks(t, taskSeed)
	executor := newEventReplayAgent(agentEvents, nil)
	ps := testutil.NewTestPushConfigStore()
	pn := testutil.NewTestPushSender(t).SetSendPushError(nil)
	handler := NewHandler(executor, WithTaskStore(store), WithPushNotifications(ps, pn))

	result, err := handler.OnSendMessage(ctx, input)
	if err != nil {
		t.Fatalf("OnSendMessage() failed: %v", err)
	}
	if diff := cmp.Diff(wantResult, result); diff != "" {
		t.Fatalf("OnSendMessage() mismatch (-want +got):\n%s", diff)
	}
	saved, err := ps.List(ctx, taskSeed.ID)
	if err != nil || len(saved) != 1 {
		t.Fatalf("expected push config to be saved, but got %v, %v", saved, err)
	}
	if len(pn.PushedConfigs) != 1 {
		t.Fatal("expected push notification to be sent, but got %w: ", pn.PushedConfigs)
	}
}

func TestRequestHandler_TaskExecutionFailOnPush(t *testing.T) {
	ctx := t.Context()

	pushConfig := &a2a.PushConfig{URL: "http://localhost:1"}
	pushConfigStore := push.NewInMemoryStore()
	sender := push.NewHTTPPushSender(&push.HTTPSenderConfig{FailOnError: true})

	taskSeed := &a2a.Task{ID: a2a.NewTaskID(), ContextID: a2a.NewContextID()}
	input := &a2a.MessageSendParams{
		Message: newUserMessage(taskSeed, "work"),
		Config:  &a2a.MessageSendConfig{PushConfig: pushConfig},
	}
	agentEvents := []a2a.Event{
		newFinalTaskStatusUpdate(taskSeed, a2a.TaskStateCompleted, "Done!"),
	}
	wantResult := newTaskWithStatus(taskSeed, a2a.TaskStateCompleted, "Done!")
	wantResult.History = []*a2a.Message{input.Message}

	store := testutil.NewTestTaskStore().WithTasks(t, taskSeed)
	executor := newEventReplayAgent(agentEvents, nil)
	handler := NewHandler(executor, WithTaskStore(store), WithPushNotifications(pushConfigStore, sender))

	result, err := handler.OnSendMessage(ctx, input)
	if err != nil {
		t.Fatalf("OnSendMessage() error = %v", err)
	}
	task, ok := result.(*a2a.Task)
	if !ok {
		t.Fatalf("OnSendMessage() result type = %T, want *a2a.Task", result)
	}
	if task.Status.State != a2a.TaskStateFailed {
		t.Fatalf("OnSendMessage() result = %+v, want state %q", result, a2a.TaskStateFailed)
	}
}

func TestRequestHandler_TaskExecutionFailOnInvalidEvent(t *testing.T) {
	testCases := []struct {
		name  string
		event *a2a.Task
	}{
		{
			name:  "non-final event",
			event: &a2a.Task{ID: "wrong id", ContextID: a2a.NewContextID()},
		},
		{
			name:  "final event",
			event: &a2a.Task{ID: "wrong id", ContextID: a2a.NewContextID(), Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}},
		},
	}

	for _, tc := range testCases {
		for _, streaming := range []bool{false, true} {
			name := tc.name
			if streaming {
				name = name + " (streaming)"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				ctx := t.Context()
				taskSeed := &a2a.Task{ID: a2a.NewTaskID(), ContextID: a2a.NewContextID()}
				input := &a2a.MessageSendParams{Message: newUserMessage(taskSeed, "work")}

				store := testutil.NewTestTaskStore().WithTasks(t, taskSeed)
				executor := newEventReplayAgent([]a2a.Event{tc.event}, nil)
				handler := NewHandler(executor, WithTaskStore(store))

				var result a2a.Event
				if streaming {
					for event, err := range handler.OnSendMessageStream(ctx, input) {
						if err != nil {
							t.Fatalf("OnSendMessageStream() error = %v", err)
						}
						result = event
					}
				} else {
					localResult, err := handler.OnSendMessage(ctx, input)
					if err != nil {
						t.Fatalf("OnSendMessage() error = %v", err)
					}
					result = localResult
				}

				task, ok := result.(*a2a.Task)
				if !ok {
					t.Fatalf("OnSendMessage() result type = %T, want *a2a.Task", result)
				}
				if task.Status.State != a2a.TaskStateFailed {
					t.Fatalf("OnSendMessage() result = %+v, want state %q", result, a2a.TaskStateFailed)
				}
			})
		}
	}
}

func TestRequestHandler_OnSendMessage_FailsToStoreFailedState(t *testing.T) {
	ctx := t.Context()

	taskSeed := &a2a.Task{ID: a2a.NewTaskID(), ContextID: a2a.NewContextID()}
	store := testutil.NewTestTaskStore().WithTasks(t, taskSeed)
	store.SaveFunc = func(ctx context.Context, task *a2a.Task, event a2a.Event, version a2a.TaskVersion) (a2a.TaskVersion, error) {
		if task.Status.State == a2a.TaskStateFailed {
			return a2a.TaskVersionMissing, fmt.Errorf("exploded")
		}
		return store.Mem.Save(ctx, task, event, version)
	}
	input := &a2a.MessageSendParams{Message: newUserMessage(taskSeed, "work")}

	executor := newEventReplayAgent([]a2a.Event{&a2a.Task{ID: "wrong id", ContextID: a2a.NewContextID()}}, nil)
	handler := NewHandler(executor, WithTaskStore(store))

	wantErr := "wrong id"
	_, err := handler.OnSendMessage(ctx, input)
	if !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("OnSendMessage() err = %v, want to contain %q", err, wantErr)
	}
}

func TestRequestHandler_OnSendMessage_TaskVersion(t *testing.T) {
	ctx := t.Context()

	gotPrevVersions := make([]a2a.TaskVersion, 0)
	store := testutil.NewTestTaskStore()
	store.SaveFunc = func(ctx context.Context, task *a2a.Task, event a2a.Event, version a2a.TaskVersion) (a2a.TaskVersion, error) {
		gotPrevVersions = append(gotPrevVersions, version)
		return store.Mem.Save(ctx, task, event, version)
	}

	statusUpdates := [][]a2a.TaskState{
		{a2a.TaskStateSubmitted, a2a.TaskStateInputRequired}, // first run
		{a2a.TaskStateWorking, a2a.TaskStateCompleted},       //second run
	}
	executor := &mockAgentExecutor{
		ExecuteFunc: func(ctx context.Context, reqCtx *RequestContext, queue eventqueue.Queue) error {
			events := statusUpdates[0]
			for i, state := range events {
				event := a2a.NewStatusUpdateEvent(reqCtx, state, nil)
				event.Final = i == len(events)-1
				if err := queue.Write(ctx, event); err != nil {
					return err
				}
			}
			statusUpdates = statusUpdates[1:]
			return nil
		},
	}
	handler := NewHandler(executor, WithTaskStore(store))

	wantPrevVersions := [][]a2a.TaskVersion{
		{a2a.TaskVersionMissing, a2a.TaskVersion(1)},                 // Save newly created task and update to input-required
		{a2a.TaskVersion(2), a2a.TaskVersion(3), a2a.TaskVersion(4)}, // Update task history, move to working, move to completed
	}

	var existingTask *a2a.Task
	for _, wantPrev := range wantPrevVersions {
		var msg *a2a.Message
		if existingTask == nil {
			msg = a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: "Hi!"})
		} else {
			msg = a2a.NewMessageForTask(a2a.MessageRoleUser, existingTask, a2a.TextPart{Text: "Hi!"})
		}
		res, err := handler.OnSendMessage(ctx, &a2a.MessageSendParams{Message: msg})
		if err != nil {
			t.Fatalf("OnSendMessage() error = %v", err)
		}
		task, ok := res.(*a2a.Task)
		if !ok {
			t.Fatalf("OnSendMessage() returned %T, want *a2a.Task", res)
		}
		existingTask = task

		if diff := cmp.Diff(wantPrev, gotPrevVersions); diff != "" {
			t.Fatalf("Save() was called with %v, want %v", gotPrevVersions, wantPrev)
		}
		gotPrevVersions = make([]a2a.TaskVersion, 0)
	}

}

func TestRequestHandler_OnSendMessage_AgentExecutorPanicFailsTask(t *testing.T) {
	ctx := t.Context()

	taskSeed := &a2a.Task{ID: a2a.NewTaskID(), ContextID: a2a.NewContextID()}
	store := testutil.NewTestTaskStore().WithTasks(t, taskSeed)
	input := &a2a.MessageSendParams{Message: newUserMessage(taskSeed, "work")}

	executor := &mockAgentExecutor{
		ExecuteFunc: func(ctx context.Context, reqCtx *RequestContext, queue eventqueue.Queue) error {
			panic("problem")
		},
	}
	handler := NewHandler(executor, WithTaskStore(store))

	result, err := handler.OnSendMessage(ctx, input)
	if err != nil {
		t.Fatalf("OnSendMessage() error = %v", err)
	}
	task, ok := result.(*a2a.Task)
	if !ok {
		t.Fatalf("OnSendMessage() result type = %T, want *a2a.Task", result)
	}
	if task.Status.State != a2a.TaskStateFailed {
		t.Fatalf("OnSendMessage() result = %+v, want state %q", result, a2a.TaskStateFailed)
	}
}

func TestRequestHandler_OnGetAgentCard(t *testing.T) {
	card := &a2a.AgentCard{Name: "agent"}

	tests := []struct {
		name     string
		option   RequestHandlerOption
		wantCard *a2a.AgentCard
		wantErr  error
	}{
		{
			name:     "static",
			option:   WithExtendedAgentCard(card),
			wantCard: card,
		},
		{
			name: "dynamic",
			option: WithExtendedAgentCardProducer(AgentCardProducerFn(func(context.Context) (*a2a.AgentCard, error) {
				return card, nil
			})),
			wantCard: card,
		},
		{
			name: "dynamic error",
			option: WithExtendedAgentCardProducer(AgentCardProducerFn(func(context.Context) (*a2a.AgentCard, error) {
				return nil, fmt.Errorf("failed")
			})),
			wantErr: fmt.Errorf("failed"),
		},
		{
			name:    "not configured",
			wantErr: a2a.ErrAuthenticatedExtendedCardNotConfigured,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			var options []RequestHandlerOption
			if tt.option != nil {
				options = append(options, tt.option)
			}
			handler := newTestHandler(options...)

			result, gotErr := handler.OnGetExtendedAgentCard(ctx)

			if tt.wantErr == nil {
				if gotErr != nil {
					t.Errorf("OnGetAgentCard() error = %v, wantErr nil", gotErr)
					return
				}
				if diff := cmp.Diff(result, tt.wantCard); diff != "" {
					t.Errorf("OnGetAgentCard() got = %v, want %v", result, tt.wantCard)
				}
			} else {
				if gotErr == nil {
					t.Errorf("OnGetAgentCard() error = nil, wantErr %q", tt.wantErr)
					return
				}
				if gotErr.Error() != tt.wantErr.Error() {
					t.Errorf("OnGetAgentCard() error = %v, wantErr %v", gotErr, tt.wantErr)
				}
			}
		})
	}
}

func TestRequestHandler_OnSendMessage_QueueCreationFails(t *testing.T) {
	ctx := t.Context()
	wantErr := errors.New("failed to create a queue")
	qm := testutil.NewTestQueueManager().SetGetOrCreateOverride(nil, wantErr)
	handler := newTestHandler(WithEventQueueManager(qm))

	result, err := handler.OnSendMessage(ctx, &a2a.MessageSendParams{
		Message: a2a.NewMessage(a2a.MessageRoleAgent, a2a.TextPart{Text: "Work"}),
	})

	if result != nil || !errors.Is(err, wantErr) {
		t.Fatalf("handler.OnSendMessage() = (%v, %v), want error %v", result, err, wantErr)
	}
}

func TestRequestHandler_OnSendMessage_QueueReadFails(t *testing.T) {
	ctx := t.Context()
	wantErr := errors.New("Read() failed")
	queue := testutil.NewTestEventQueue().SetReadOverride(nil, wantErr)
	qm := testutil.NewTestQueueManager().SetGetOrCreateOverride(queue, nil).SetGetOverride(queue, true)
	handler := newTestHandler(WithEventQueueManager(qm))

	result, err := handler.OnSendMessage(ctx, &a2a.MessageSendParams{
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: "Work"}),
	})
	if result != nil || !errors.Is(err, wantErr) {
		t.Fatalf("handler.OnSendMessage() = (%v, %v), want error %v", result, err, wantErr)
	}
}

func TestRequestHandler_OnSendMessage_RelatedTaskLoading(t *testing.T) {
	existingTask := &a2a.Task{ID: a2a.NewTaskID(), ContextID: a2a.NewContextID()}
	ctx := t.Context()
	ts := testutil.NewTestTaskStore().WithTasks(t, existingTask)
	executor := newEventReplayAgent([]a2a.Event{a2a.NewMessage(a2a.MessageRoleAgent, a2a.TextPart{Text: "Hello!"})}, nil)
	handler := NewHandler(executor, WithRequestContextInterceptor(&ReferencedTasksLoader{Store: ts}))

	request := &a2a.MessageSendParams{
		Message: &a2a.Message{
			ID:             a2a.NewMessageID(),
			Parts:          a2a.ContentParts{a2a.TextPart{Text: "Work"}},
			Role:           a2a.MessageRoleUser,
			ReferenceTasks: []a2a.TaskID{a2a.NewTaskID(), existingTask.ID},
		},
	}
	_, err := handler.OnSendMessage(ctx, request)
	if err != nil {
		t.Fatalf("handler.OnSendMessage() failed: %v", err)
	}

	capturedReqContext := executor.capturedReqContext
	if len(capturedReqContext.RelatedTasks) != 1 || capturedReqContext.RelatedTasks[0].ID != existingTask.ID {
		t.Fatalf("RequestContext.RelatedTasks = %v, want [%v]", capturedReqContext.RelatedTasks, existingTask)
	}
}

func TestRequestHandler_OnSendMessage_AgentExecutionFails(t *testing.T) {
	ctx := t.Context()
	wantErr := errors.New("failed to create a queue")
	executor := newEventReplayAgent([]a2a.Event{}, wantErr)
	handler := NewHandler(executor)

	result, err := handler.OnSendMessage(ctx, &a2a.MessageSendParams{
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: "Work"}),
	})
	if result != nil || !errors.Is(err, wantErr) {
		t.Fatalf("handler.OnSendMessage() = (%v, %v), want error %v", result, err, wantErr)
	}
}

func TestRequestHandler_OnSendMessage_NoTaskCreated(t *testing.T) {
	ctx := t.Context()
	getCalled := 0
	savedCalled := 0
	mockStore := testutil.NewTestTaskStore()
	mockStore.GetFunc = func(ctx context.Context, taskID a2a.TaskID) (*a2a.Task, a2a.TaskVersion, error) {
		getCalled += 1
		return nil, a2a.TaskVersionMissing, a2a.ErrTaskNotFound
	}
	mockStore.SaveFunc = func(ctx context.Context, task *a2a.Task, event a2a.Event, version a2a.TaskVersion) (a2a.TaskVersion, error) {
		savedCalled += 1
		return version, nil
	}

	executor := newEventReplayAgent([]a2a.Event{newAgentMessage("hello")}, nil)
	handler := NewHandler(executor, WithTaskStore(mockStore))

	result, gotErr := handler.OnSendMessage(ctx, &a2a.MessageSendParams{
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: "Work"}),
	})
	if gotErr != nil {
		t.Fatalf("OnSendMessage() error = %v, wantErr nil", gotErr)
	}
	if _, ok := result.(*a2a.Message); !ok {
		t.Fatalf("OnSendMessage() = %v, want a2a.Message", result)
	}

	if getCalled != 1 {
		t.Fatalf("OnSendMessage() TaskStore.Get called %d times, want 1", getCalled)
	}
	if savedCalled > 0 {
		t.Fatalf("OnSendMessage() TaskStore.Save called %d times, want 0", savedCalled)
	}
}

func TestRequestHandler_OnSendMessage_NewTaskHistory(t *testing.T) {
	ctx := t.Context()
	ts := taskstore.NewMem()
	executor := &mockAgentExecutor{
		ExecuteFunc: func(ctx context.Context, reqCtx *RequestContext, q eventqueue.Queue) error {
			event := a2a.NewStatusUpdateEvent(reqCtx, a2a.TaskStateCompleted, nil)
			event.Final = true
			return q.Write(ctx, event)
		},
	}
	handler := NewHandler(executor, WithTaskStore(ts))

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

func TestRequestHandler_OnGetTask(t *testing.T) {
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
			wantErr: fmt.Errorf("missing TaskID: %w", a2a.ErrInvalidParams),
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
					t.Errorf("OnGetTask() error = %v, wantErr nil", err)
					return
				}
				if diff := cmp.Diff(result, tt.want); diff != "" {
					t.Errorf("OnGetTask() got = %v, want %v", result, tt.want)
				}
			} else {
				if err == nil {
					t.Errorf("OnGetTask() error = nil, wantErr %q", tt.wantErr)
					return
				}
				if err.Error() != tt.wantErr.Error() {
					t.Errorf("OnGetTask() error = %v, wantErr %v", err, tt.wantErr)
				}
			}
		})
	}
}

func TestRequestHandler_OnGetTask_StoreGetFails(t *testing.T) {
	ctx := t.Context()
	wantErr := errors.New("failed to get task: store get failed")
	ts := testutil.NewTestTaskStore().SetGetOverride(nil, a2a.TaskVersionMissing, wantErr)
	handler := newTestHandler(WithTaskStore(ts))

	result, err := handler.OnGetTask(ctx, &a2a.TaskQueryParams{ID: a2a.NewTaskID()})
	if result != nil || !errors.Is(err, wantErr) {
		t.Fatalf("OnGetTask() = (%v, %v), want error %v", result, err, wantErr)
	}
}

func TestRequestHandler_OnListTasks(t *testing.T) {
	id1, id2, id3 := a2a.NewTaskID(), a2a.NewTaskID(), a2a.NewTaskID()
	startTime := time.Date(2025, time.December, 11, 14, 54, 0, 0, time.UTC)
	cutOffTime := startTime.Add(2 * time.Second)

	tests := []struct {
		name          string
		givenTasks    []*a2a.Task
		request       *a2a.ListTasksRequest
		wantResponse  *a2a.ListTasksResponse
		wantErr       error
		authenticator taskstore.Authenticator
	}{
		{
			name:          "success",
			givenTasks:    []*a2a.Task{{ID: id1}, {ID: id2}, {ID: id3}},
			request:       &a2a.ListTasksRequest{},
			wantResponse:  &a2a.ListTasksResponse{Tasks: []*a2a.Task{{ID: id3}, {ID: id2}, {ID: id1}}, TotalSize: 3, PageSize: 50},
			authenticator: testAuthenticator(),
		},
		{
			name: "success with filters",
			givenTasks: []*a2a.Task{
				{ID: id1, Artifacts: []*a2a.Artifact{{Name: "artifact1"}}, ContextID: "context1", History: []*a2a.Message{{ID: "test-message-1"}, {ID: "test-message-2"}, {ID: "test-message-3"}}, Status: a2a.TaskStatus{State: a2a.TaskStateWorking}},
				{ID: id2, ContextID: "context2", History: []*a2a.Message{{ID: "test-message-4"}, {ID: "test-message-5"}}, Status: a2a.TaskStatus{State: a2a.TaskStateCanceled}},
				{ID: id3, Artifacts: []*a2a.Artifact{{Name: "artifact3"}}, ContextID: "context1", History: []*a2a.Message{{ID: "test-message-6"}, {ID: "test-message-7"}, {ID: "test-message-8"}, {ID: "test-message-9"}}, Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}},
			},
			request:       &a2a.ListTasksRequest{PageSize: 2, ContextID: "context1", Status: a2a.TaskStateCompleted, HistoryLength: 2, LastUpdatedAfter: &cutOffTime, IncludeArtifacts: true},
			wantResponse:  &a2a.ListTasksResponse{Tasks: []*a2a.Task{{ID: id3, Artifacts: []*a2a.Artifact{{Name: "artifact3"}}, ContextID: "context1", History: []*a2a.Message{{ID: "test-message-8"}, {ID: "test-message-9"}}, Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}}}, TotalSize: 1, PageSize: 2},
			authenticator: testAuthenticator(),
		},
		{
			name:          "invalid pageToken",
			givenTasks:    []*a2a.Task{{ID: id1}, {ID: id2}, {ID: id3}},
			request:       &a2a.ListTasksRequest{PageToken: "invalidPageToken"},
			wantErr:       fmt.Errorf("failed to list tasks: %w", a2a.ErrParseError),
			authenticator: testAuthenticator(),
		},
		{
			name:       "unauthorized",
			givenTasks: []*a2a.Task{{ID: id1}, {ID: id2}, {ID: id3}},
			request:    &a2a.ListTasksRequest{},
			wantErr:    fmt.Errorf("failed to list tasks: %w", a2a.ErrUnauthenticated),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			var opts []taskstore.Option
			if tt.authenticator != nil {
				opts = append(opts, taskstore.WithAuthenticator(tt.authenticator))
			}
			ts := testutil.NewTestTaskStore(opts...).WithTasks(t, tt.givenTasks...)
			handler := newTestHandler(WithTaskStore(ts))
			result, err := handler.OnListTasks(ctx, tt.request)

			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("OnListTasks() error = %v, wantErr nil", err)
					return
				}
				if diff := cmp.Diff(result, tt.wantResponse); diff != "" {
					t.Errorf("OnListTasks() mismatch (+got -want): %s", diff)
				}
			} else {
				if err == nil {
					t.Errorf("OnListTasks() error = nil, wantErr %q", tt.wantErr)
					return
				}
				if err.Error() != tt.wantErr.Error() {
					t.Errorf("OnListTasks() error = %v, wantErr %v", err, tt.wantErr)
				}
			}
		})
	}
}

func testAuthenticator() taskstore.Authenticator {
	return func(ctx context.Context) (taskstore.UserName, bool) {
		return "test", true
	}
}

func TestRequestHandler_OnCancelTask(t *testing.T) {
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
			wantErr: a2a.ErrInvalidParams,
		},
		{
			name:    "task not found",
			params:  &a2a.TaskIDParams{ID: a2a.NewTaskID()},
			wantErr: fmt.Errorf("failed to cancel: cancelation failed: setup failed: failed to load a task: %w", a2a.ErrTaskNotFound),
		},
		{
			name:    "task already completed",
			params:  &a2a.TaskIDParams{ID: completedTask.ID},
			wantErr: fmt.Errorf("failed to cancel: cancelation failed: setup failed: task in non-cancelable state %s: %w", a2a.TaskStateCompleted, a2a.ErrTaskNotCancelable),
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
					t.Errorf("OnCancelTask() error = %v, wantErr nil", err)
					return
				}
				if diff := cmp.Diff(result, tt.want); diff != "" {
					t.Errorf("OnCancelTask() got = %v, want %v", result, tt.want)
				}
			} else {
				if err == nil {
					t.Errorf("OnCancelTask() error = nil, wantErr %q", tt.wantErr)
					return
				}
				if err.Error() != tt.wantErr.Error() {
					t.Errorf("OnCancelTask() error = %v, wantErr %v", err, tt.wantErr)
				}
			}
		})
	}
}

func TestRequestHandler_OnResubscribeToTask_Success(t *testing.T) {
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
		time.Sleep(10 * time.Millisecond)
		return originalExecuteFunc(ctx, reqCtx, queue)
	}

	go func() {
		for range handler.OnSendMessageStream(ctx, &a2a.MessageSendParams{
			Message: newUserMessage(taskSeed, "Work"),
		}) {
			// Events have to be consumed to prevent a deadlock.
		}
	}()

	<-executionStarted

	seq := handler.OnResubscribeToTask(ctx, &a2a.TaskIDParams{ID: taskSeed.ID})
	gotEvents, err := collectEvents(seq)
	if err != nil {
		t.Fatalf("collectEvents() failed: %v", err)
	}

	if diff := cmp.Diff(wantEvents, gotEvents); diff != "" {
		t.Fatalf("OnResubscribeToTask() events mismatch (-want +got):\n%s", diff)
	}
}

func TestRequestHandler_OnResubscribeToTask_NotFound(t *testing.T) {
	ctx := t.Context()
	taskID := a2a.NewTaskID()
	wantErr := a2a.ErrTaskNotFound
	executor := &mockAgentExecutor{}
	handler := NewHandler(executor)

	result, err := collectEvents(handler.OnResubscribeToTask(ctx, &a2a.TaskIDParams{ID: taskID}))

	if result != nil || !errors.Is(err, wantErr) {
		t.Fatalf("OnResubscribeToTask() = (%v, %v), want error %v", result, err, wantErr)
	}
}

func TestRequestHandler_OnCancelTask_AgentCancelFails(t *testing.T) {
	ctx := t.Context()
	taskToCancel := &a2a.Task{ID: a2a.NewTaskID(), ContextID: a2a.NewContextID(), Status: a2a.TaskStatus{State: a2a.TaskStateWorking}}
	wantErr := fmt.Errorf("failed to cancel: cancelation failed: agent cancel error")
	store := testutil.NewTestTaskStore().WithTasks(t, taskToCancel)
	executor := &mockAgentExecutor{
		CancelFunc: func(ctx context.Context, reqCtx *RequestContext, q eventqueue.Queue) error {
			return fmt.Errorf("agent cancel error")
		},
	}
	handler := NewHandler(executor, WithTaskStore(store))

	result, err := handler.OnCancelTask(ctx, &a2a.TaskIDParams{ID: taskToCancel.ID})
	if result != nil || err.Error() != wantErr.Error() {
		t.Fatalf("OnCancelTask() error = %v, wantErr %v", err, wantErr)
	}
}

func TestRequestHandler_MultipleRequestContextInterceptors(t *testing.T) {
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

	_, err := handler.OnSendMessage(ctx, &a2a.MessageSendParams{
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: "Work"}),
	})
	if err != nil {
		t.Fatalf("handler.OnSendMessage() failed: %v", err)
	}

	capturedContext := executor.capturedContext
	if capturedContext.Value(key1) != val1 || capturedContext.Value(key2) != val2 {
		t.Fatalf("Execute() context = %+v, want to have interceptor attached values", capturedContext)
	}
}

func TestRequestHandler_RequestContextInterceptorRejectsRequest(t *testing.T) {
	ctx := t.Context()
	executor := newEventReplayAgent([]a2a.Event{a2a.NewMessage(a2a.MessageRoleAgent, a2a.TextPart{Text: "Hello!"})}, nil)
	wantErr := errors.New("rejected")
	interceptor := interceptReqCtxFn(func(ctx context.Context, reqCtx *RequestContext) (context.Context, error) {
		return ctx, wantErr
	})
	handler := NewHandler(executor, WithRequestContextInterceptor(interceptor))

	_, err := handler.OnSendMessage(ctx, &a2a.MessageSendParams{
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: "Work"}),
	})

	if !errors.Is(err, wantErr) {
		t.Fatalf("handler.OnSendMessage() error = %v, want %v", err, wantErr)
	}
	if executor.executeCalled {
		t.Fatal("want agent executor to no be called")
	}
}

func TestRequestHandler_ExecuteRequestContextLoading(t *testing.T) {
	ctxID := a2a.NewMessageID()
	taskSeed := &a2a.Task{
		ID:        a2a.NewTaskID(),
		ContextID: a2a.NewContextID(),
		Status:    a2a.TaskStatus{State: a2a.TaskStateInputRequired},
	}
	testCases := []struct {
		name           string
		newRequest     func() *a2a.MessageSendParams
		wantReqCtxMeta map[string]any
		wantStoredTask *a2a.Task
		wantContextID  string
	}{
		{
			name: "new task",
			newRequest: func() *a2a.MessageSendParams {
				msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: "Hello"})
				msg.Metadata = map[string]any{"foo1": "bar1"}
				return &a2a.MessageSendParams{
					Message:  msg,
					Metadata: map[string]any{"foo2": "bar2"},
				}
			},
			wantReqCtxMeta: map[string]any{"foo2": "bar2"},
		},
		{
			name: "stored tasks",
			newRequest: func() *a2a.MessageSendParams {
				msg := a2a.NewMessageForTask(a2a.MessageRoleUser, taskSeed, a2a.TextPart{Text: "Hello"})
				msg.Metadata = map[string]any{"foo1": "bar1"}
				return &a2a.MessageSendParams{
					Message:  msg,
					Metadata: map[string]any{"foo2": "bar2"},
				}
			},
			wantStoredTask: taskSeed,
			wantContextID:  taskSeed.ContextID,
			wantReqCtxMeta: map[string]any{"foo2": "bar2"},
		},
		{
			name: "preserve message context",
			newRequest: func() *a2a.MessageSendParams {
				msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: "Hello"})
				msg.ContextID = ctxID
				return &a2a.MessageSendParams{Message: msg}
			},
			wantContextID: ctxID,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			executor := newEventReplayAgent([]a2a.Event{a2a.NewMessage(a2a.MessageRoleAgent, a2a.TextPart{Text: "Done!"})}, nil)
			var gotReqCtx *RequestContext
			handler := NewHandler(
				executor,
				WithTaskStore(testutil.NewTestTaskStore().WithTasks(t, taskSeed)),
				WithRequestContextInterceptor(interceptReqCtxFn(func(ctx context.Context, reqCtx *RequestContext) (context.Context, error) {
					gotReqCtx = reqCtx
					return ctx, nil
				})),
			)
			request := tc.newRequest()
			_, err := handler.OnSendMessage(ctx, request)
			if err != nil {
				t.Fatalf("handler.OnSendMessage() error = %v, want nil", err)
			}
			opts := []cmp.Option{cmpopts.IgnoreFields(a2a.Task{}, "History")}
			if diff := cmp.Diff(tc.wantStoredTask, gotReqCtx.StoredTask, opts...); diff != "" {
				t.Fatalf("wrong request context stored task (+got,-want): diff = %s", diff)
			}
			if diff := cmp.Diff(tc.wantReqCtxMeta, gotReqCtx.Metadata); diff != "" {
				t.Fatalf("wrong request context meta (+got,-want): diff = %s", diff)
			}
			if tc.wantContextID != "" {
				if tc.wantContextID != gotReqCtx.ContextID {
					t.Fatalf("reqCtx.contextID = %s, want = %s", gotReqCtx.ContextID, tc.wantContextID)
				}
			}
		})
	}
}

func TestRequestHandler_OnSetTaskPushConfig(t *testing.T) {
	ctx := t.Context()
	taskID := a2a.TaskID("test-task")

	testCases := []struct {
		name    string
		params  *a2a.TaskPushConfig
		wantErr error
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
			wantErr: fmt.Errorf("failed to save push config: %w: push config endpoint cannot be empty", a2a.ErrInvalidParams),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ps := testutil.NewTestPushConfigStore()
			pn := testutil.NewTestPushSender(t)
			handler := newTestHandler(WithPushNotifications(ps, pn))
			got, err := handler.OnSetTaskPushConfig(ctx, tc.params)

			if tc.wantErr != nil {
				if err == nil || err.Error() != tc.wantErr.Error() {
					t.Errorf("OnSetTaskPushConfig() error = %v, want %v", err, tc.wantErr)
				}
				return
			}

			if err != nil {
				t.Errorf("OnSetTaskPushConfig() failed: %v", err)
				return
			}

			if got.Config.ID == "" {
				t.Errorf("OnSetTaskPushConfig() expected a generated ID, but it was empty")
				return
			}

			if tc.params.Config.ID == "" {
				got.Config.ID = ""
			}

			if diff := cmp.Diff(tc.params, got); diff != "" {
				t.Errorf("OnSetTaskPushConfig() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRequestHandler_OnGetTaskPushConfig(t *testing.T) {
	ctx := t.Context()
	taskID := a2a.TaskID("test-task")
	config1 := &a2a.PushConfig{ID: "config-1", URL: "https://example.com/push1"}
	ps := testutil.NewTestPushConfigStore().WithConfigs(t, taskID, config1)
	pn := testutil.NewTestPushSender(t)
	handler := newTestHandler(WithPushNotifications(ps, pn))

	testCases := []struct {
		name    string
		params  *a2a.GetTaskPushConfigParams
		want    *a2a.TaskPushConfig
		wantErr error
	}{
		{
			name:   "success",
			params: &a2a.GetTaskPushConfigParams{TaskID: taskID, ConfigID: config1.ID},
			want:   &a2a.TaskPushConfig{TaskID: taskID, Config: *config1},
		},
		{
			name:    "non-existent config",
			params:  &a2a.GetTaskPushConfigParams{TaskID: taskID, ConfigID: "non-existent"},
			wantErr: push.ErrPushConfigNotFound,
		},
		{
			name:    "non-existent task",
			params:  &a2a.GetTaskPushConfigParams{TaskID: "non-existent-task", ConfigID: config1.ID},
			wantErr: push.ErrPushConfigNotFound,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := handler.OnGetTaskPushConfig(ctx, tc.params)
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("OnGetTaskPushConfig() error = %v, want %v", err, tc.wantErr)
				return
			}
			if tc.wantErr == nil {
				if diff := cmp.Diff(tc.want, got); diff != "" {
					t.Errorf("OnGetTaskPushConfig() mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

func TestRequestHandler_OnListTaskPushConfig(t *testing.T) {
	ctx := t.Context()
	taskID := a2a.TaskID("test-task")
	config1 := a2a.PushConfig{ID: "config-1", URL: "https://example.com/push1"}
	config2 := a2a.PushConfig{ID: "config-2", URL: "https://example.com/push2"}
	emptyTaskID := a2a.TaskID("empty-task")

	ps := testutil.NewTestPushConfigStore().WithConfigs(t, taskID, &config1, &config2)
	pn := testutil.NewTestPushSender(t)
	handler := newTestHandler(WithPushNotifications(ps, pn))

	if _, err := ps.Save(ctx, emptyTaskID, &config1); err != nil {
		t.Fatalf("Setup: Save() for empty task failed: %v", err)
	}
	if err := ps.DeleteAll(ctx, emptyTaskID); err != nil {
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
				t.Errorf("OnListTaskPushConfig() failed: %v", err)
				return
			}
			sort.Slice(got, func(i, j int) bool { return got[i].Config.ID < got[j].Config.ID })
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("OnListTaskPushConfig() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRequestHandler_OnDeleteTaskPushConfig(t *testing.T) {
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
			ps := testutil.NewTestPushConfigStore().WithConfigs(t, taskID, &config1, &config2)
			pn := testutil.NewTestPushSender(t)
			handler := newTestHandler(WithPushNotifications(ps, pn))
			err := handler.OnDeleteTaskPushConfig(ctx, tc.params)
			if err != nil {
				t.Errorf("OnDeleteTaskPushConfig() failed: %v", err)
				return
			}

			got, err := handler.OnListTaskPushConfig(ctx, &a2a.ListTaskPushConfigParams{TaskID: taskID})
			if err != nil {
				t.Errorf("OnListTaskPushConfig() for verification failed: %v", err)
				return
			}

			sort.Slice(got, func(i, j int) bool { return got[i].Config.ID < got[j].Config.ID })
			if diff := cmp.Diff(tc.wantRemain, got); diff != "" {
				t.Errorf("Remaining configs mismatch (-want +got):\n%s", diff)
			}
		})
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

var _ AgentExecutor = (*mockAgentExecutor)(nil)

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

func newUserMessage(task *a2a.Task, text string) *a2a.Message {
	return &a2a.Message{
		ID:     "message-id",
		Parts:  []a2a.Part{a2a.TextPart{Text: text}},
		Role:   a2a.MessageRoleUser,
		TaskID: task.ID,
	}
}

func newTaskStatusUpdate(task a2a.TaskInfoProvider, state a2a.TaskState, msg string) *a2a.TaskStatusUpdateEvent {
	ue := a2a.NewStatusUpdateEvent(task, state, newAgentMessage(msg))
	ue.Status.Timestamp = &fixedTime
	return ue
}

func newFinalTaskStatusUpdate(task a2a.TaskInfoProvider, state a2a.TaskState, msg string) *a2a.TaskStatusUpdateEvent {
	res := newTaskStatusUpdate(task, state, msg)
	res.Final = true
	return res
}

func newTaskWithStatus(task a2a.TaskInfoProvider, state a2a.TaskState, msg string) *a2a.Task {
	info := task.TaskInfo()
	status := a2a.TaskStatus{State: state, Message: newAgentMessage(msg)}
	status.Timestamp = &fixedTime
	return &a2a.Task{ID: info.TaskID, ContextID: info.ContextID, Status: status}
}

func newTaskWithMeta(task a2a.TaskInfoProvider, meta map[string]any) *a2a.Task {
	return &a2a.Task{ID: task.TaskInfo().TaskID, ContextID: task.TaskInfo().ContextID, Metadata: meta}
}

func newArtifactEvent(task a2a.TaskInfoProvider, aid a2a.ArtifactID, parts ...a2a.Part) *a2a.TaskArtifactUpdateEvent {
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
