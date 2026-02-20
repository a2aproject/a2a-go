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
	"github.com/a2aproject/a2a-go/a2asrv/push"
	"github.com/a2aproject/a2a-go/a2asrv/taskstore"
	"github.com/a2aproject/a2a-go/internal/testutil"
	"github.com/a2aproject/a2a-go/internal/utils"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

var fixedTime = time.Now()

func TestRequestHandler_SendMessage(t *testing.T) {
	artifactID := a2a.NewArtifactID()
	taskSeed := &a2a.Task{ID: a2a.NewTaskID(), ContextID: a2a.NewContextID()}
	inputRequiredTaskSeed := &a2a.Task{ID: a2a.NewTaskID(), ContextID: a2a.NewContextID(), Status: a2a.TaskStatus{State: a2a.TaskStateInputRequired}}
	completedTaskSeed := &a2a.Task{ID: a2a.NewTaskID(), ContextID: a2a.NewContextID(), Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}}
	taskStoreSeed := []*a2a.Task{taskSeed, inputRequiredTaskSeed, completedTaskSeed}

	type testCase struct {
		name        string
		input       *a2a.SendMessageRequest
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
				input: &a2a.SendMessageRequest{
					Message: newUserMessage(taskSeed, "Work"),
				},
				agentEvents: []a2a.Event{newTaskWithStatus(taskSeed, a2a.TaskStateUnknown, "...")},
				wantErr:     fmt.Errorf("unknown task state: %s", a2a.TaskStateUnknown),
			},
			{
				name: "final task overwrites intermediate task events",
				input: &a2a.SendMessageRequest{
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
				input: &a2a.SendMessageRequest{
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
				name:  "task status update accumulation",
				input: &a2a.SendMessageRequest{Message: newUserMessage(taskSeed, "Syn")},
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
				input: &a2a.SendMessageRequest{Message: newUserMessage(taskSeed, "Syn")},
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
				input: &a2a.SendMessageRequest{Message: newUserMessage(taskSeed, "Syn")},
				agentEvents: []a2a.Event{
					newTaskStatusUpdate(taskSeed, a2a.TaskStateSubmitted, "Ack"),
					newArtifactEvent(taskSeed, artifactID, a2a.NewTextPart("Hello")),
					a2a.NewArtifactUpdateEvent(taskSeed, artifactID, a2a.NewTextPart(", world!")),
					newFinalTaskStatusUpdate(taskSeed, a2a.TaskStateCompleted, "Done!"),
				},
				wantResult: &a2a.Task{
					ID:        taskSeed.ID,
					ContextID: taskSeed.ContextID,
					Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted, Message: newAgentMessage("Done!"), Timestamp: &fixedTime},
					History:   []*a2a.Message{newUserMessage(taskSeed, "Syn"), newAgentMessage("Ack")},
					Artifacts: []*a2a.Artifact{
						{ID: artifactID, Parts: a2a.ContentParts{a2a.NewTextPart("Hello"), a2a.NewTextPart(", world!")}},
					},
				},
			},
			{
				name:  "task with multiple artifacts",
				input: &a2a.SendMessageRequest{Message: newUserMessage(taskSeed, "Syn")},
				agentEvents: []a2a.Event{
					newTaskStatusUpdate(taskSeed, a2a.TaskStateSubmitted, "Ack"),
					newArtifactEvent(taskSeed, artifactID, a2a.NewTextPart("Hello")),
					newArtifactEvent(taskSeed, artifactID+"2", a2a.NewTextPart("World")),
					newFinalTaskStatusUpdate(taskSeed, a2a.TaskStateCompleted, "Done!"),
				},
				wantResult: &a2a.Task{
					ID:        taskSeed.ID,
					ContextID: taskSeed.ContextID,
					Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted, Message: newAgentMessage("Done!"), Timestamp: &fixedTime},
					History:   []*a2a.Message{newUserMessage(taskSeed, "Syn"), newAgentMessage("Ack")},
					Artifacts: []*a2a.Artifact{
						{ID: artifactID, Parts: a2a.ContentParts{a2a.NewTextPart("Hello")}},
						{ID: artifactID + "2", Parts: a2a.ContentParts{a2a.NewTextPart("World")}},
					},
				},
			},
			{
				name: "task continuation",
				input: &a2a.SendMessageRequest{
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
				input:   &a2a.SendMessageRequest{},
				wantErr: fmt.Errorf("message is required: %w", a2a.ErrInvalidParams),
			},
			{
				name: "fails if no message ID",
				input: &a2a.SendMessageRequest{Message: &a2a.Message{
					Parts: a2a.ContentParts{a2a.NewTextPart("Test")},
					Role:  a2a.MessageRoleUser,
				}},
				wantErr: fmt.Errorf("message ID is required: %w", a2a.ErrInvalidParams),
			},
			{
				name: "fails if no message parts",
				input: &a2a.SendMessageRequest{Message: &a2a.Message{
					ID:   a2a.NewMessageID(),
					Role: a2a.MessageRoleUser,
				}},
				wantErr: fmt.Errorf("message parts is required: %w", a2a.ErrInvalidParams),
			},
			{
				name: "fails if no message role",
				input: &a2a.SendMessageRequest{Message: &a2a.Message{
					ID:    a2a.NewMessageID(),
					Parts: a2a.ContentParts{a2a.NewTextPart("Test")},
				}},
				wantErr: fmt.Errorf("message role is required: %w", a2a.ErrInvalidParams),
			},
			{
				name: "fails on non-existent task reference",
				input: &a2a.SendMessageRequest{
					Message: &a2a.Message{
						TaskID: "non-existent",
						ID:     "test-message",
						Parts:  a2a.ContentParts{a2a.NewTextPart("Test")},
						Role:   a2a.MessageRoleUser,
					},
				},
				wantErr: a2a.ErrTaskNotFound,
			},
			{
				name: "fails if contextID not equal to task contextID",
				input: &a2a.SendMessageRequest{
					Message: &a2a.Message{TaskID: taskSeed.ID, ContextID: taskSeed.ContextID + "1", ID: "test-message"},
				},
				wantErr: a2a.ErrInvalidParams,
			},
			{
				name: "fails if message references completed task",
				input: &a2a.SendMessageRequest{
					Message: newUserMessage(completedTaskSeed, "Test"),
				},
				wantErr: fmt.Errorf("setup failed: task in a terminal state %q: %w", a2a.TaskStateCompleted, a2a.ErrInvalidParams),
			},
		}
	}

	for _, tt := range createTestCases() {
		input := &a2a.SendMessageRequest{Message: newUserMessage(taskSeed, "Test")}
		if tt.input != nil {
			input = tt.input
		}

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			store := testutil.NewTestTaskStore().WithTasks(t, taskStoreSeed...)
			executor := newEventReplayAgent(tt.agentEvents, nil)
			handler := NewHandler(executor, WithTaskStore(store))

			result, gotErr := handler.SendMessage(ctx, input)
			if tt.wantErr == nil {
				if gotErr != nil {
					t.Errorf("SendMessage() error = %v, wantErr nil", gotErr)
					return
				}
				if diff := cmp.Diff(tt.wantResult, result); diff != "" {
					t.Errorf("SendMessage() (+got,-want):\ngot = %v\nwant %v\ndiff = %s", result, tt.wantResult, diff)
				}
			} else {
				if gotErr == nil {
					t.Errorf("SendMessage() error = nil, wantErr %q", tt.wantErr)
					return
				}
				if gotErr.Error() != tt.wantErr.Error() && !errors.Is(gotErr, tt.wantErr) {
					t.Errorf("SendMessage() error = %v, wantErr %v", gotErr, tt.wantErr)
				}
			}
		})
	}

	for _, tt := range createTestCases() {
		input := &a2a.SendMessageRequest{Message: newUserMessage(taskSeed, "Test")}
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
			for got, gotErr := range handler.SendStreamingMessage(ctx, input) {
				if streamErr != nil {
					t.Errorf("handler.SendStreamingMessage() got (%v, %v) after error, want stream end", got, gotErr)
					return
				}

				if gotErr != nil && tt.wantErr == nil {
					t.Errorf("SendStreamingMessage() error = %v, wantErr nil", gotErr)
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
					t.Errorf("SendStreamingMessage() (+got,-want):\ngot = %v\nwant %v\ndiff = %s", got, want, diff)
					return
				}
			}
			if tt.wantErr == nil && eventI != len(tt.agentEvents) {
				t.Errorf("SendStreamingMessage() received %d events, want %d", eventI, len(tt.agentEvents))
				return
			}
			if tt.wantErr != nil && streamErr == nil {
				t.Errorf("SendStreamingMessage() error = nil, want %v", tt.wantErr)
				return
			}
			if tt.wantErr != nil && (streamErr.Error() != tt.wantErr.Error() && !errors.Is(streamErr, tt.wantErr)) {
				t.Errorf("SendStreamingMessage() error = %v, wantErr %v", streamErr, tt.wantErr)
			}
		})
	}
}

func TestRequestHandler_SendMessage_AuthRequired(t *testing.T) {
	ctx := t.Context()
	ts := testutil.NewTestTaskStore()
	authCredentialsChan := make(chan struct{})
	executor := &mockAgentExecutor{
		ExecuteFunc: func(ctx context.Context, execCtx *ExecutorContext) iter.Seq2[a2a.Event, error] {
			return func(yield func(a2a.Event, error) bool) {
				if !yield(a2a.NewSubmittedTask(execCtx, execCtx.Message), nil) {
					return
				}
				if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateAuthRequired, nil), nil) {
					return
				}
				<-authCredentialsChan
				yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCompleted, nil), nil)
			}
		},
	}
	handler := NewHandler(executor, WithTaskStore(ts))

	msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("perform protected operation"))
	result, err := handler.SendMessage(ctx, &a2a.SendMessageRequest{Message: msg})
	if err != nil {
		t.Fatalf("SendMessage() error = %v, wantErr nil", err)
	}
	var taskID a2a.TaskID
	if task, ok := result.(*a2a.Task); ok {
		if task.Status.State != a2a.TaskStateAuthRequired {
			t.Fatalf("SendMessage() = %v, want a2a.Task in %q state", result, a2a.TaskStateAuthRequired)
		}
		msg.TaskID = task.ID
		taskID = task.ID
	} else {
		t.Fatalf("SendMessage() = %v, want a2a.Task", result)
	}

	_, err = handler.SendMessage(ctx, &a2a.SendMessageRequest{Message: msg})
	if !strings.Contains(err.Error(), "execution is already in progress") {
		t.Fatalf("SendMessage() error = %v, want err to contain 'execution is already in progress'", err)
	}

	authCredentialsChan <- struct{}{}
	time.Sleep(time.Millisecond * 10)

	task, err := handler.GetTask(ctx, &a2a.GetTaskRequest{ID: taskID})
	if task.Status.State != a2a.TaskStateCompleted {
		t.Fatalf("handler.OnGetTask() = (%v, %v), want a task in state %q", task, err, a2a.TaskStateCompleted)
	}
}

func TestRequestHandler_SendMessage_NonBlocking(t *testing.T) {
	taskSeed := &a2a.Task{ID: a2a.NewTaskID(), ContextID: a2a.NewContextID(), Status: a2a.TaskStatus{State: a2a.TaskStateInputRequired}}

	createExecutor := func(generateEvent func(execCtx *ExecutorContext) []a2a.Event) (*mockAgentExecutor, chan struct{}) {
		waitingChan := make(chan struct{})
		return &mockAgentExecutor{
			ExecuteFunc: func(ctx context.Context, execCtx *ExecutorContext) iter.Seq2[a2a.Event, error] {
				return func(yield func(a2a.Event, error) bool) {
					for i, event := range generateEvent(execCtx) {
						if i > 0 {
							select {
							case <-waitingChan:
							case <-ctx.Done():
								yield(nil, ctx.Err())
								return
							}
						}
						if !yield(event, nil) {
							return
						}
					}
				}
			},
		}, waitingChan
	}

	type testCase struct {
		name        string
		blocking    bool
		input       *a2a.SendMessageRequest
		agentEvents func(execCtx *ExecutorContext) []a2a.Event
		wantState   a2a.TaskState
		wantEvents  int
	}

	createTestCases := func() []testCase {
		return []testCase{
			{
				name:     "defaults to blocking",
				blocking: true,
				input:    &a2a.SendMessageRequest{Message: newUserMessage(taskSeed, "Work"), Config: &a2a.SendMessageConfig{}},
				agentEvents: func(execCtx *ExecutorContext) []a2a.Event {
					return []a2a.Event{
						newTaskWithStatus(execCtx, a2a.TaskStateWorking, "Working..."),
						newTaskWithStatus(execCtx, a2a.TaskStateCompleted, "Done"),
					}
				},
				wantState:  a2a.TaskStateCompleted,
				wantEvents: 2,
			},
			{
				name:  "non-terminal task state",
				input: &a2a.SendMessageRequest{Message: newUserMessage(taskSeed, "Work"), Config: &a2a.SendMessageConfig{Blocking: utils.Ptr(false)}},
				agentEvents: func(execCtx *ExecutorContext) []a2a.Event {
					return []a2a.Event{
						newTaskWithStatus(execCtx, a2a.TaskStateWorking, "Working..."),
						newTaskWithStatus(execCtx, a2a.TaskStateCompleted, "Done"),
					}
				},
				wantState:  a2a.TaskStateWorking,
				wantEvents: 2,
			},
			{
				name:  "non-final status update",
				input: &a2a.SendMessageRequest{Message: newUserMessage(taskSeed, "Work"), Config: &a2a.SendMessageConfig{Blocking: utils.Ptr(false)}},
				agentEvents: func(execCtx *ExecutorContext) []a2a.Event {
					return []a2a.Event{
						newTaskStatusUpdate(execCtx, a2a.TaskStateWorking, "Working..."),
						newFinalTaskStatusUpdate(execCtx, a2a.TaskStateCompleted, "Done!"),
					}
				},
				wantState:  a2a.TaskStateWorking,
				wantEvents: 2,
			},
			{
				name:  "artifact update",
				input: &a2a.SendMessageRequest{Message: newUserMessage(taskSeed, "Work"), Config: &a2a.SendMessageConfig{Blocking: utils.Ptr(false)}},
				agentEvents: func(execCtx *ExecutorContext) []a2a.Event {
					return []a2a.Event{
						newArtifactEvent(execCtx, a2a.NewArtifactID(), a2a.NewTextPart("Artifact")),
						newFinalTaskStatusUpdate(execCtx, a2a.TaskStateCompleted, "Done!"),
					}
				},
				wantState:  a2a.TaskStateInputRequired,
				wantEvents: 2,
			},
			{
				name:  "message for existing task",
				input: &a2a.SendMessageRequest{Message: newUserMessage(taskSeed, "Work"), Config: &a2a.SendMessageConfig{Blocking: utils.Ptr(false)}},
				agentEvents: func(execCtx *ExecutorContext) []a2a.Event {
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
				input: &a2a.SendMessageRequest{Message: newUserMessage(taskSeed, "Work"), Config: &a2a.SendMessageConfig{Blocking: utils.Ptr(false)}},
				agentEvents: func(execCtx *ExecutorContext) []a2a.Event {
					return []a2a.Event{
						a2a.NewMessageForTask(a2a.MessageRoleAgent, execCtx, a2a.NewTextPart("Done")),
						a2a.NewMessageForTask(a2a.MessageRoleAgent, execCtx, a2a.NewTextPart("Done-2")),
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

			result, gotErr := handler.SendMessage(ctx, tt.input)
			if !tt.blocking {
				close(waitingChan)
			}
			if gotErr != nil {
				t.Errorf("SendMessage() error = %v, wantErr nil", gotErr)
				return
			}
			if tt.wantState != a2a.TaskStateUnspecified {
				task, ok := result.(*a2a.Task)
				if !ok {
					t.Errorf("SendMessage() returned %T, want a2a.Task", result)
					return
				}
				if task.Status.State != tt.wantState {
					t.Errorf("SendMessage() task.State = %v, want %v", task.Status.State, tt.wantState)
				}
			} else {
				if _, ok := result.(*a2a.Message); !ok {
					t.Errorf("SendMessage() returned %T, want a2a.Message", result)
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
			for _, gotErr := range handler.SendStreamingMessage(ctx, tt.input) {
				if gotErr != nil {
					t.Errorf("SendStreamingMessage() error = %v, wantErr nil", gotErr)
				}
				gotEvents++
			}
			if gotEvents != tt.wantEvents {
				t.Errorf("SendStreamingMessage() event count = %d, want %d", gotEvents, tt.wantEvents)
			}
		})
	}
}

func TestRequestHandler_SendMessageStreaming_AuthRequired(t *testing.T) {
	ctx := t.Context()
	ts := testutil.NewTestTaskStore()
	authCredentialsChan := make(chan struct{})
	executor := &mockAgentExecutor{
		ExecuteFunc: func(ctx context.Context, execCtx *ExecutorContext) iter.Seq2[a2a.Event, error] {
			return func(yield func(a2a.Event, error) bool) {
				if !yield(a2a.NewSubmittedTask(execCtx, execCtx.Message), nil) {
					return
				}
				if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateAuthRequired, nil), nil) {
					return
				}
				<-authCredentialsChan
				yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCompleted, nil), nil)
			}
		},
	}
	handler := NewHandler(executor, WithTaskStore(ts))

	var lastEvent a2a.Event
	msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("perform protected operation"))
	for event, err := range handler.SendStreamingMessage(ctx, &a2a.SendMessageRequest{Message: msg}) {
		if upd, ok := event.(*a2a.TaskStatusUpdateEvent); ok && upd.Status.State == a2a.TaskStateAuthRequired {
			go func() { authCredentialsChan <- struct{}{} }()
		}
		if err != nil {
			t.Fatalf("SendStreamingMessage() error = %v, wantErr nil", err)
		}
		lastEvent = event
	}

	if task, ok := lastEvent.(*a2a.TaskStatusUpdateEvent); ok {
		if task.Status.State != a2a.TaskStateCompleted {
			t.Fatalf("SendStreamingMessage() = %v, want status update with state %q", lastEvent, a2a.TaskStateAuthRequired)
		}
	} else {
		t.Fatalf("SendStreamingMessage() = %v, want a2a.TaskStatusUpdateEvent", lastEvent)
	}
}

func TestRequestHandler_SendMessage_PushNotifications(t *testing.T) {
	ctx := t.Context()

	taskSeed := &a2a.Task{ID: a2a.NewTaskID(), ContextID: a2a.NewContextID()}
	pushConfig := &a2a.PushConfig{URL: "https://example.com/push"}
	input := &a2a.SendMessageRequest{
		Message: newUserMessage(taskSeed, "work"),
		Config: &a2a.SendMessageConfig{
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

	result, err := handler.SendMessage(ctx, input)
	if err != nil {
		t.Fatalf("SendMessage() failed: %v", err)
	}
	if diff := cmp.Diff(wantResult, result); diff != "" {
		t.Fatalf("SendMessage() mismatch (-want +got):\n%s", diff)
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
	input := &a2a.SendMessageRequest{
		Message: newUserMessage(taskSeed, "work"),
		Config:  &a2a.SendMessageConfig{PushConfig: pushConfig},
	}
	agentEvents := []a2a.Event{
		newFinalTaskStatusUpdate(taskSeed, a2a.TaskStateCompleted, "Done!"),
	}
	wantResult := newTaskWithStatus(taskSeed, a2a.TaskStateCompleted, "Done!")
	wantResult.History = []*a2a.Message{input.Message}

	store := testutil.NewTestTaskStore().WithTasks(t, taskSeed)
	executor := newEventReplayAgent(agentEvents, nil)
	handler := NewHandler(executor, WithTaskStore(store), WithPushNotifications(pushConfigStore, sender))

	result, err := handler.SendMessage(ctx, input)
	if err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}
	task, ok := result.(*a2a.Task)
	if !ok {
		t.Fatalf("SendMessage() result type = %T, want *a2a.Task", result)
	}
	if task.Status.State != a2a.TaskStateFailed {
		t.Fatalf("SendMessage() result = %+v, want state %q", result, a2a.TaskStateFailed)
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
				input := &a2a.SendMessageRequest{Message: newUserMessage(taskSeed, "work")}

				store := testutil.NewTestTaskStore().WithTasks(t, taskSeed)
				executor := newEventReplayAgent([]a2a.Event{tc.event}, nil)
				handler := NewHandler(executor, WithTaskStore(store))

				var result a2a.Event
				if streaming {
					for event, err := range handler.SendStreamingMessage(ctx, input) {
						if err != nil {
							t.Fatalf("SendStreamingMessage() error = %v", err)
						}
						result = event
					}
				} else {
					localResult, err := handler.SendMessage(ctx, input)
					if err != nil {
						t.Fatalf("SendMessage() error = %v", err)
					}
					result = localResult
				}

				task, ok := result.(*a2a.Task)
				if !ok {
					t.Fatalf("SendMessage() result type = %T, want *a2a.Task", result)
				}
				if task.Status.State != a2a.TaskStateFailed {
					t.Fatalf("SendMessage() result = %+v, want state %q", result, a2a.TaskStateFailed)
				}
			})
		}
	}
}

func TestRequestHandler_SendMessage_FailsToStoreFailedState(t *testing.T) {
	ctx := t.Context()

	taskSeed := &a2a.Task{ID: a2a.NewTaskID(), ContextID: a2a.NewContextID()}
	store := testutil.NewTestTaskStore().WithTasks(t, taskSeed)
	store.UpdateFunc = func(ctx context.Context, req *taskstore.UpdateRequest) (taskstore.TaskVersion, error) {
		if req.Task.Status.State == a2a.TaskStateFailed {
			return taskstore.TaskVersionMissing, fmt.Errorf("exploded")
		}
		return store.InMemory.Update(ctx, req)
	}
	input := &a2a.SendMessageRequest{Message: newUserMessage(taskSeed, "work")}

	executor := newEventReplayAgent([]a2a.Event{&a2a.Task{ID: "wrong id", ContextID: a2a.NewContextID()}}, nil)
	handler := NewHandler(executor, WithTaskStore(store))

	wantErr := "wrong id"
	_, err := handler.SendMessage(ctx, input)
	if !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("SendMessage() err = %v, want to contain %q", err, wantErr)
	}
}

func TestRequestHandler_SendMessage_TaskVersion(t *testing.T) {
	ctx := t.Context()

	gotPrevVersions := make([]taskstore.TaskVersion, 0)
	store := testutil.NewTestTaskStore()
	store.UpdateFunc = func(ctx context.Context, req *taskstore.UpdateRequest) (taskstore.TaskVersion, error) {
		gotPrevVersions = append(gotPrevVersions, req.PrevVersion)
		return store.InMemory.Update(ctx, req)
	}

	statusUpdates := [][]a2a.TaskState{
		{a2a.TaskStateSubmitted, a2a.TaskStateInputRequired}, // first run
		{a2a.TaskStateWorking, a2a.TaskStateCompleted},       //second run
	}
	executor := &mockAgentExecutor{
		ExecuteFunc: func(ctx context.Context, execCtx *ExecutorContext) iter.Seq2[a2a.Event, error] {
			return func(yield func(a2a.Event, error) bool) {
				if execCtx.StoredTask == nil {
					if !yield(a2a.NewSubmittedTask(execCtx, execCtx.Message), nil) {
						return
					}
				}
				events := statusUpdates[0]
				for _, state := range events {
					event := a2a.NewStatusUpdateEvent(execCtx, state, nil)
					if !yield(event, nil) {
						return
					}
				}
				statusUpdates = statusUpdates[1:]
			}
		},
	}
	handler := NewHandler(executor, WithTaskStore(store))

	wantPrevVersions := [][]taskstore.TaskVersion{
		{taskstore.TaskVersion(1), taskstore.TaskVersion(2)},                           // Save newly created task and update to input-required
		{taskstore.TaskVersion(3), taskstore.TaskVersion(4), taskstore.TaskVersion(5)}, // Update task history, move to working, move to completed
	}

	var existingTask *a2a.Task
	for _, wantPrev := range wantPrevVersions {
		var msg *a2a.Message
		if existingTask == nil {
			msg = a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("Hi!"))
		} else {
			msg = a2a.NewMessageForTask(a2a.MessageRoleUser, existingTask, a2a.NewTextPart("Hi!"))
		}
		res, err := handler.SendMessage(ctx, &a2a.SendMessageRequest{Message: msg})
		if err != nil {
			t.Fatalf("SendMessage() error = %v", err)
		}
		task, ok := res.(*a2a.Task)
		if !ok {
			t.Fatalf("SendMessage() returned %T, want *a2a.Task", res)
		}
		existingTask = task

		if diff := cmp.Diff(wantPrev, gotPrevVersions); diff != "" {
			t.Fatalf("Save() was called with %v, want %v", gotPrevVersions, wantPrev)
		}
		gotPrevVersions = make([]taskstore.TaskVersion, 0)
	}

}

func TestRequestHandler_SendMessage_AgentExecutorPanicFailsTask(t *testing.T) {
	ctx := t.Context()

	taskSeed := &a2a.Task{ID: a2a.NewTaskID(), ContextID: a2a.NewContextID()}
	store := testutil.NewTestTaskStore().WithTasks(t, taskSeed)
	input := &a2a.SendMessageRequest{Message: newUserMessage(taskSeed, "work")}

	executor := &mockAgentExecutor{
		ExecuteFunc: func(ctx context.Context, execCtx *ExecutorContext) iter.Seq2[a2a.Event, error] {
			return func(yield func(a2a.Event, error) bool) {
				panic("problem")
			}
		},
	}
	handler := NewHandler(executor, WithTaskStore(store))

	result, err := handler.SendMessage(ctx, input)
	if err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}
	task, ok := result.(*a2a.Task)
	if !ok {
		t.Fatalf("SendMessage() result type = %T, want *a2a.Task", result)
	}
	if task.Status.State != a2a.TaskStateFailed {
		t.Fatalf("SendMessage() result = %+v, want state %q", result, a2a.TaskStateFailed)
	}
}

func TestRequestHandler_GetAgentCard(t *testing.T) {
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
			option: WithExtendedAgentCardProducer(ExtendedAgentCardProducerFn(func(context.Context, *a2a.GetExtendedAgentCardRequest) (*a2a.AgentCard, error) {
				return card, nil
			})),
			wantCard: card,
		},
		{
			name: "dynamic error",
			option: WithExtendedAgentCardProducer(ExtendedAgentCardProducerFn(func(context.Context, *a2a.GetExtendedAgentCardRequest) (*a2a.AgentCard, error) {
				return nil, fmt.Errorf("failed")
			})),
			wantErr: fmt.Errorf("failed"),
		},
		{
			name:    "not configured",
			wantErr: a2a.ErrExtendedCardNotConfigured,
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

			result, gotErr := handler.GetExtendedAgentCard(ctx, &a2a.GetExtendedAgentCardRequest{})

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

func TestRequestHandler_SendMessage_QueueCreationFails(t *testing.T) {
	ctx := t.Context()
	wantErr := errors.New("failed to create a queue")
	qm := testutil.NewTestQueueManager().SetError(wantErr)
	handler := newTestHandler(WithEventQueueManager(qm))

	result, err := handler.SendMessage(ctx, &a2a.SendMessageRequest{
		Message: a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("Work")),
	})

	if result != nil || !errors.Is(err, wantErr) {
		t.Fatalf("handler.SendMessage() = (%v, %v), want error %v", result, err, wantErr)
	}
}

func TestRequestHandler_SendMessage_QueueReadFails(t *testing.T) {
	ctx := t.Context()
	wantErr := errors.New("Read() failed")
	queue := testutil.NewTestEventQueue().SetReadOverride(nil, wantErr)
	qm := testutil.NewTestQueueManager().SetQueue(queue)
	handler := newTestHandler(WithEventQueueManager(qm))

	result, err := handler.SendMessage(ctx, &a2a.SendMessageRequest{
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("Work")),
	})
	if result != nil || !errors.Is(err, wantErr) {
		t.Fatalf("handler.SendMessage() = (%v, %v), want error %v", result, err, wantErr)
	}
}

func TestRequestHandler_SendMessage_RelatedTaskLoading(t *testing.T) {
	existingTask := &a2a.Task{ID: a2a.NewTaskID(), ContextID: a2a.NewContextID()}
	ctx := t.Context()
	ts := testutil.NewTestTaskStore().WithTasks(t, existingTask)
	executor := newEventReplayAgent([]a2a.Event{a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("Hello!"))}, nil)
	handler := NewHandler(executor, WithExecutorContextInterceptor(&ReferencedTasksLoader{Store: ts}))

	request := &a2a.SendMessageRequest{
		Message: &a2a.Message{
			ID:             a2a.NewMessageID(),
			Parts:          a2a.ContentParts{a2a.NewTextPart("Work")},
			Role:           a2a.MessageRoleUser,
			ReferenceTasks: []a2a.TaskID{a2a.NewTaskID(), existingTask.ID},
		},
	}
	_, err := handler.SendMessage(ctx, request)
	if err != nil {
		t.Fatalf("handler.SendMessage() failed: %v", err)
	}

	capturedExecContext := executor.capturedExecContext
	if len(capturedExecContext.RelatedTasks) != 1 || capturedExecContext.RelatedTasks[0].ID != existingTask.ID {
		t.Fatalf("RequestContext.RelatedTasks = %v, want [%v]", capturedExecContext.RelatedTasks, existingTask)
	}
}

func TestRequestHandler_SendMessage_AgentExecutionFails(t *testing.T) {
	ctx := t.Context()
	wantErr := errors.New("failed to create a queue")
	executor := newEventReplayAgent([]a2a.Event{}, wantErr)
	handler := NewHandler(executor)

	result, err := handler.SendMessage(ctx, &a2a.SendMessageRequest{
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("Work")),
	})
	if result != nil || !errors.Is(err, wantErr) {
		t.Fatalf("handler.SendMessage() = (%v, %v), want error %v", result, err, wantErr)
	}
}

func TestRequestHandler_SendMessage_NoTaskCreated(t *testing.T) {
	ctx := t.Context()
	getCalled := 0
	savedCalled := 0
	mockStore := testutil.NewTestTaskStore()
	mockStore.GetFunc = func(ctx context.Context, taskID a2a.TaskID) (*taskstore.StoredTask, error) {
		getCalled += 1
		return nil, a2a.ErrTaskNotFound
	}
	mockStore.CreateFunc = func(ctx context.Context, task *a2a.Task) (taskstore.TaskVersion, error) {
		savedCalled += 1
		return taskstore.TaskVersionMissing, nil
	}

	executor := newEventReplayAgent([]a2a.Event{newAgentMessage("hello")}, nil)
	handler := NewHandler(executor, WithTaskStore(mockStore))

	result, gotErr := handler.SendMessage(ctx, &a2a.SendMessageRequest{
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("Work")),
	})
	if gotErr != nil {
		t.Fatalf("SendMessage() error = %v, wantErr nil", gotErr)
	}
	if _, ok := result.(*a2a.Message); !ok {
		t.Fatalf("SendMessage() = %v, want a2a.Message", result)
	}

	if getCalled != 1 {
		t.Fatalf("SendMessage() TaskStore.Get called %d times, want 1", getCalled)
	}
	if savedCalled > 0 {
		t.Fatalf("SendMessage() TaskStore.Save called %d times, want 0", savedCalled)
	}
}

func TestRequestHandler_SendMessage_NewTaskHistory(t *testing.T) {
	ctx := t.Context()
	ts := taskstore.NewInMemory(nil)
	executor := &mockAgentExecutor{
		ExecuteFunc: func(ctx context.Context, execCtx *ExecutorContext) iter.Seq2[a2a.Event, error] {
			return func(yield func(a2a.Event, error) bool) {
				event := a2a.NewSubmittedTask(execCtx, execCtx.Message)
				event.Status = a2a.TaskStatus{State: a2a.TaskStateCompleted}
				yield(event, nil)
			}
		},
	}
	handler := NewHandler(executor, WithTaskStore(ts))

	msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("Complete the task!"))
	result, gotErr := handler.SendMessage(ctx, &a2a.SendMessageRequest{Message: msg})
	if gotErr != nil {
		t.Fatalf("SendMessage() error = %v, wantErr nil", gotErr)
	}
	if task, ok := result.(*a2a.Task); ok {
		if diff := cmp.Diff([]*a2a.Message{msg}, task.History); diff != "" {
			t.Fatalf("SendMessage() wrong result (+got,-want):\ngot = %v\nwant = %v\ndiff = %s", task.History, []*a2a.Message{msg}, diff)
		}
	} else {
		t.Fatalf("SendMessage() = %v, want a2a.Task", result)
	}
}

func TestRequestHandler_GetTask(t *testing.T) {
	ptr := func(i int) *int {
		return &i
	}

	existingTaskID := a2a.NewTaskID()
	history := []*a2a.Message{{ID: "test-message-1"}, {ID: "test-message-2"}, {ID: "test-message-3"}}

	tests := []struct {
		name    string
		query   *a2a.GetTaskRequest
		want    *a2a.Task
		wantErr error
	}{
		{
			name:  "success with TaskID and full history",
			query: &a2a.GetTaskRequest{ID: existingTaskID},
			want:  &a2a.Task{ID: existingTaskID, History: history},
		},
		{
			name:    "missing TaskID",
			query:   &a2a.GetTaskRequest{ID: ""},
			wantErr: fmt.Errorf("missing TaskID: %w", a2a.ErrInvalidParams),
		},
		{
			name:    "task not found",
			query:   &a2a.GetTaskRequest{ID: a2a.NewTaskID()},
			wantErr: fmt.Errorf("failed to get task: %w", a2a.ErrTaskNotFound),
		},
		{
			name:  "get task with limited HistoryLength",
			query: &a2a.GetTaskRequest{ID: existingTaskID, HistoryLength: ptr(len(history) - 1)},
			want:  &a2a.Task{ID: existingTaskID, History: history[1:]},
		},
		{
			name:  "get task with larger than available HistoryLength",
			query: &a2a.GetTaskRequest{ID: existingTaskID, HistoryLength: ptr(len(history) + 1)},
			want:  &a2a.Task{ID: existingTaskID, History: history},
		},
		{
			name:  "get task with zero HistoryLength",
			query: &a2a.GetTaskRequest{ID: existingTaskID, HistoryLength: ptr(0)},
			want:  &a2a.Task{ID: existingTaskID, History: make([]*a2a.Message, 0)},
		},
		{
			name:  "get task with negative HistoryLength",
			query: &a2a.GetTaskRequest{ID: existingTaskID, HistoryLength: ptr(-1)},
			want:  &a2a.Task{ID: existingTaskID, History: make([]*a2a.Message, 0)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			ts := testutil.NewTestTaskStore().WithTasks(t, &a2a.Task{ID: existingTaskID, History: history})
			handler := newTestHandler(WithTaskStore(ts))
			result, err := handler.GetTask(ctx, tt.query)
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

func TestRequestHandler_GetTask_StoreGetFails(t *testing.T) {
	ctx := t.Context()
	wantErr := errors.New("failed to get task: store get failed")
	ts := testutil.NewTestTaskStore().SetGetOverride(nil, wantErr)
	handler := newTestHandler(WithTaskStore(ts))

	result, err := handler.GetTask(ctx, &a2a.GetTaskRequest{ID: a2a.NewTaskID()})
	if result != nil || !errors.Is(err, wantErr) {
		t.Fatalf("OnGetTask() = (%v, %v), want error %v", result, err, wantErr)
	}
}

func TestRequestHandler_ListTasks(t *testing.T) {
	id1, id2, id3 := a2a.NewTaskID(), a2a.NewTaskID(), a2a.NewTaskID()
	startTime := time.Date(2025, time.December, 11, 14, 54, 0, 0, time.UTC)
	cutOffTime := startTime.Add(2 * time.Second)

	tests := []struct {
		name         string
		givenTasks   []*a2a.Task
		request      *a2a.ListTasksRequest
		wantResponse *a2a.ListTasksResponse
		wantErr      error
		storeConfig  *taskstore.InMemoryStoreConfig
	}{
		{
			name:         "success",
			givenTasks:   []*a2a.Task{{ID: id1}, {ID: id2}, {ID: id3}},
			request:      &a2a.ListTasksRequest{},
			wantResponse: &a2a.ListTasksResponse{Tasks: []*a2a.Task{{ID: id3}, {ID: id2}, {ID: id1}}, TotalSize: 3, PageSize: 50},
			storeConfig:  &taskstore.InMemoryStoreConfig{Authenticator: testAuthenticator()},
		},
		{
			name: "success with filters",
			givenTasks: []*a2a.Task{
				{ID: id1, Artifacts: []*a2a.Artifact{{Name: "artifact1"}}, ContextID: "context1", History: []*a2a.Message{{ID: "test-message-1"}, {ID: "test-message-2"}, {ID: "test-message-3"}}, Status: a2a.TaskStatus{State: a2a.TaskStateWorking}},
				{ID: id2, ContextID: "context2", History: []*a2a.Message{{ID: "test-message-4"}, {ID: "test-message-5"}}, Status: a2a.TaskStatus{State: a2a.TaskStateCanceled}},
				{ID: id3, Artifacts: []*a2a.Artifact{{Name: "artifact3"}}, ContextID: "context1", History: []*a2a.Message{{ID: "test-message-6"}, {ID: "test-message-7"}, {ID: "test-message-8"}, {ID: "test-message-9"}}, Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}},
			},
			request:      &a2a.ListTasksRequest{PageSize: 2, ContextID: "context1", Status: a2a.TaskStateCompleted, HistoryLength: 2, StatusTimestampAfter: &cutOffTime, IncludeArtifacts: true},
			wantResponse: &a2a.ListTasksResponse{Tasks: []*a2a.Task{{ID: id3, Artifacts: []*a2a.Artifact{{Name: "artifact3"}}, ContextID: "context1", History: []*a2a.Message{{ID: "test-message-8"}, {ID: "test-message-9"}}, Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}}}, TotalSize: 1, PageSize: 2},
			storeConfig:  &taskstore.InMemoryStoreConfig{Authenticator: testAuthenticator()},
		},
		{
			name:        "invalid pageToken",
			givenTasks:  []*a2a.Task{{ID: id1}, {ID: id2}, {ID: id3}},
			request:     &a2a.ListTasksRequest{PageToken: "invalidPageToken"},
			wantErr:     fmt.Errorf("failed to list tasks: %w", a2a.ErrParseError),
			storeConfig: &taskstore.InMemoryStoreConfig{Authenticator: testAuthenticator()},
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
			ts := testutil.NewTestTaskStoreWithConfig(tt.storeConfig).WithTasks(t, tt.givenTasks...)
			handler := newTestHandler(WithTaskStore(ts))
			result, err := handler.ListTasks(ctx, tt.request)

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

func testAuthenticator() func(context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		return "test", nil
	}
}

func TestRequestHandler_CancelTask(t *testing.T) {
	taskToCancel := &a2a.Task{ID: a2a.NewTaskID(), ContextID: a2a.NewContextID(), Status: a2a.TaskStatus{State: a2a.TaskStateWorking}}
	completedTask := &a2a.Task{ID: a2a.NewTaskID(), ContextID: a2a.NewContextID(), Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}}
	canceledTask := &a2a.Task{ID: a2a.NewTaskID(), ContextID: a2a.NewContextID(), Status: a2a.TaskStatus{State: a2a.TaskStateCanceled}}

	tests := []struct {
		name    string
		params  *a2a.CancelTaskRequest
		want    *a2a.Task
		wantErr error
	}{
		{
			name:   "success",
			params: &a2a.CancelTaskRequest{ID: taskToCancel.ID},
			want:   newTaskWithStatus(taskToCancel, a2a.TaskStateCanceled, "Cancelled"),
		},
		{
			name:    "nil params",
			params:  nil,
			wantErr: a2a.ErrInvalidParams,
		},
		{
			name:    "task not found",
			params:  &a2a.CancelTaskRequest{ID: a2a.NewTaskID()},
			wantErr: fmt.Errorf("failed to cancel: cancelation failed: setup failed: failed to load a task: %w", a2a.ErrTaskNotFound),
		},
		{
			name:    "task already completed",
			params:  &a2a.CancelTaskRequest{ID: completedTask.ID},
			wantErr: fmt.Errorf("failed to cancel: cancelation failed: setup failed: task in non-cancelable state %s: %w", a2a.TaskStateCompleted, a2a.ErrTaskNotCancelable),
		},
		{
			name:   "task already canceled",
			params: &a2a.CancelTaskRequest{ID: canceledTask.ID},
			want:   canceledTask,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			store := testutil.NewTestTaskStore().WithTasks(t, taskToCancel, completedTask, canceledTask)
			executor := &mockAgentExecutor{
				CancelFunc: func(ctx context.Context, execCtx *ExecutorContext) iter.Seq2[a2a.Event, error] {
					return func(yield func(a2a.Event, error) bool) {
						event := newFinalTaskStatusUpdate(taskToCancel, a2a.TaskStateCanceled, "Cancelled")
						yield(event, nil)
					}
				},
			}
			handler := NewHandler(executor, WithTaskStore(store))

			result, err := handler.CancelTask(ctx, tt.params)
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

func TestRequestHandler_ResubscribeToTask_Success(t *testing.T) {
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
	executor.ExecuteFunc = func(ctx context.Context, execCtx *ExecutorContext) iter.Seq2[a2a.Event, error] {
		return func(yield func(a2a.Event, error) bool) {
			close(executionStarted)
			time.Sleep(10 * time.Millisecond)
			for event, err := range originalExecuteFunc(ctx, execCtx) {
				if !yield(event, err) {
					return
				}
			}
		}
	}

	go func() {
		for range handler.SendStreamingMessage(ctx, &a2a.SendMessageRequest{
			Message: newUserMessage(taskSeed, "Work"),
		}) {
			// Events have to be consumed to prevent a deadlock.
		}
	}()

	<-executionStarted

	seq := handler.SubscribeToTask(ctx, &a2a.SubscribeToTaskRequest{ID: taskSeed.ID})
	gotEvents, err := collectEvents(seq)
	if err != nil {
		t.Fatalf("collectEvents() failed: %v", err)
	}

	if diff := cmp.Diff(wantEvents, gotEvents); diff != "" {
		t.Fatalf("OnResubscribeToTask() events mismatch (-want +got):\n%s", diff)
	}
}

func TestRequestHandler_ResubscribeToTask_NotFound(t *testing.T) {
	ctx := t.Context()
	taskID := a2a.NewTaskID()
	wantErr := a2a.ErrTaskNotFound
	executor := &mockAgentExecutor{}
	handler := NewHandler(executor)

	result, err := collectEvents(handler.SubscribeToTask(ctx, &a2a.SubscribeToTaskRequest{ID: taskID}))

	if result != nil || !errors.Is(err, wantErr) {
		t.Fatalf("OnResubscribeToTask() = (%v, %v), want error %v", result, err, wantErr)
	}
}

func TestRequestHandler_CancelTask_AgentCancelFails(t *testing.T) {
	ctx := t.Context()
	taskToCancel := &a2a.Task{ID: a2a.NewTaskID(), ContextID: a2a.NewContextID(), Status: a2a.TaskStatus{State: a2a.TaskStateWorking}}
	wantErr := fmt.Errorf("failed to cancel: cancelation failed: agent cancel error")
	store := testutil.NewTestTaskStore().WithTasks(t, taskToCancel)
	executor := &mockAgentExecutor{
		CancelFunc: func(ctx context.Context, execCtx *ExecutorContext) iter.Seq2[a2a.Event, error] {
			return func(yield func(a2a.Event, error) bool) { yield(nil, fmt.Errorf("agent cancel error")) }
		},
	}
	handler := NewHandler(executor, WithTaskStore(store))

	result, err := handler.CancelTask(ctx, &a2a.CancelTaskRequest{ID: taskToCancel.ID})
	if result != nil || err.Error() != wantErr.Error() {
		t.Fatalf("OnCancelTask() error = %v, wantErr %v", err, wantErr)
	}
}

func TestRequestHandler_MultipleRequestContextInterceptors(t *testing.T) {
	ctx := t.Context()
	executor := newEventReplayAgent([]a2a.Event{a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("Hello!"))}, nil)
	type key1Type struct{}
	key1, val1 := key1Type{}, 2
	interceptor1 := interceptExecCtxFn(func(ctx context.Context, execCtx *ExecutorContext) (context.Context, error) {
		return context.WithValue(ctx, key1, val1), nil
	})
	type key2Type struct{}
	key2, val2 := key2Type{}, 43
	interceptor2 := interceptExecCtxFn(func(ctx context.Context, execCtx *ExecutorContext) (context.Context, error) {
		return context.WithValue(ctx, key2, val2), nil
	})
	handler := NewHandler(
		executor,
		WithExecutorContextInterceptor(interceptor1),
		WithExecutorContextInterceptor(interceptor2),
	)

	_, err := handler.SendMessage(ctx, &a2a.SendMessageRequest{
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("Work")),
	})
	if err != nil {
		t.Fatalf("handler.SendMessage() failed: %v", err)
	}

	capturedContext := executor.capturedContext
	if capturedContext.Value(key1) != val1 || capturedContext.Value(key2) != val2 {
		t.Fatalf("Execute() context = %+v, want to have interceptor attached values", capturedContext)
	}
}

func TestRequestHandler_RequestContextInterceptorRejectsRequest(t *testing.T) {
	ctx := t.Context()
	executor := newEventReplayAgent([]a2a.Event{a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("Hello!"))}, nil)
	wantErr := errors.New("rejected")
	interceptor := interceptExecCtxFn(func(ctx context.Context, execCtx *ExecutorContext) (context.Context, error) {
		return ctx, wantErr
	})
	handler := NewHandler(executor, WithExecutorContextInterceptor(interceptor))

	_, err := handler.SendMessage(ctx, &a2a.SendMessageRequest{
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("Work")),
	})

	if !errors.Is(err, wantErr) {
		t.Fatalf("handler.SendMessage() error = %v, want %v", err, wantErr)
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
		name            string
		newRequest      func() *a2a.SendMessageRequest
		wantExecCtxMeta map[string]any
		wantStoredTask  *a2a.Task
		wantContextID   string
	}{
		{
			name: "new task",
			newRequest: func() *a2a.SendMessageRequest {
				msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("Hello"))
				msg.Metadata = map[string]any{"foo1": "bar1"}
				return &a2a.SendMessageRequest{
					Message:  msg,
					Metadata: map[string]any{"foo2": "bar2"},
				}
			},
			wantExecCtxMeta: map[string]any{"foo2": "bar2"},
		},
		{
			name: "stored tasks",
			newRequest: func() *a2a.SendMessageRequest {
				msg := a2a.NewMessageForTask(a2a.MessageRoleUser, taskSeed, a2a.NewTextPart("Hello"))
				msg.Metadata = map[string]any{"foo1": "bar1"}
				return &a2a.SendMessageRequest{
					Message:  msg,
					Metadata: map[string]any{"foo2": "bar2"},
				}
			},
			wantStoredTask:  taskSeed,
			wantContextID:   taskSeed.ContextID,
			wantExecCtxMeta: map[string]any{"foo2": "bar2"},
		},
		{
			name: "preserve message context",
			newRequest: func() *a2a.SendMessageRequest {
				msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("Hello"))
				msg.ContextID = ctxID
				return &a2a.SendMessageRequest{Message: msg}
			},
			wantContextID: ctxID,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			executor := newEventReplayAgent([]a2a.Event{a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("Done!"))}, nil)
			var gotExecCtx *ExecutorContext
			handler := NewHandler(
				executor,
				WithTaskStore(testutil.NewTestTaskStore().WithTasks(t, taskSeed)),
				WithExecutorContextInterceptor(interceptExecCtxFn(func(ctx context.Context, execCtx *ExecutorContext) (context.Context, error) {
					gotExecCtx = execCtx
					return ctx, nil
				})),
			)
			request := tc.newRequest()
			_, err := handler.SendMessage(ctx, request)
			if err != nil {
				t.Fatalf("handler.SendMessage() error = %v, want nil", err)
			}
			opts := []cmp.Option{cmpopts.IgnoreFields(a2a.Task{}, "History")}
			if diff := cmp.Diff(tc.wantStoredTask, gotExecCtx.StoredTask, opts...); diff != "" {
				t.Fatalf("wrong request context stored task (+got,-want): diff = %s", diff)
			}
			if diff := cmp.Diff(tc.wantExecCtxMeta, gotExecCtx.Metadata); diff != "" {
				t.Fatalf("wrong request context meta (+got,-want): diff = %s", diff)
			}
			if tc.wantContextID != "" {
				if tc.wantContextID != gotExecCtx.ContextID {
					t.Fatalf("execCtx.contextID = %s, want = %s", gotExecCtx.ContextID, tc.wantContextID)
				}
			}
		})
	}
}

func TestRequestHandler_SetTaskPushConfig(t *testing.T) {
	ctx := t.Context()
	taskID := a2a.TaskID("test-task")

	testCases := []struct {
		name    string
		params  *a2a.CreateTaskPushConfigRequest
		wantErr error
	}{
		{
			name: "valid config with id",
			params: &a2a.CreateTaskPushConfigRequest{
				TaskID: taskID,
				Config: a2a.PushConfig{ID: "config-1", URL: "https://example.com/push"},
			},
		},
		{
			name: "valid config without id",
			params: &a2a.CreateTaskPushConfigRequest{
				TaskID: taskID,
				Config: a2a.PushConfig{URL: "https://example.com/push-no-id"},
			},
		},
		{
			name: "invalid config - empty URL",
			params: &a2a.CreateTaskPushConfigRequest{
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
			got, err := handler.CreateTaskPushConfig(ctx, tc.params)

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

			want := &a2a.TaskPushConfig{
				ID:     tc.params.Config.ID,
				Config: tc.params.Config,
				TaskID: tc.params.TaskID,
			}
			if want.ID == "" {
				want.ID = got.ID
			}
			if want.Config.ID == "" {
				want.Config.ID = got.Config.ID
			}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("OnSetTaskPushConfig() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRequestHandler_GetTaskPushConfig(t *testing.T) {
	ctx := t.Context()
	taskID := a2a.TaskID("test-task")
	config1 := &a2a.PushConfig{ID: "config-1", URL: "https://example.com/push1"}
	ps := testutil.NewTestPushConfigStore().WithConfigs(t, taskID, config1)
	pn := testutil.NewTestPushSender(t)
	handler := newTestHandler(WithPushNotifications(ps, pn))

	testCases := []struct {
		name    string
		params  *a2a.GetTaskPushConfigRequest
		want    *a2a.TaskPushConfig
		wantErr error
	}{
		{
			name:   "success",
			params: &a2a.GetTaskPushConfigRequest{TaskID: taskID, ID: config1.ID},
			want:   &a2a.TaskPushConfig{TaskID: taskID, Config: *config1},
		},
		{
			name:    "non-existent config",
			params:  &a2a.GetTaskPushConfigRequest{TaskID: taskID, ID: "non-existent"},
			wantErr: push.ErrPushConfigNotFound,
		},
		{
			name:    "non-existent task",
			params:  &a2a.GetTaskPushConfigRequest{TaskID: "non-existent-task", ID: config1.ID},
			wantErr: push.ErrPushConfigNotFound,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := handler.GetTaskPushConfig(ctx, tc.params)
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

func TestRequestHandler_ListTaskPushConfig(t *testing.T) {
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
		params *a2a.ListTaskPushConfigRequest
		want   []*a2a.TaskPushConfig
	}{
		{
			name:   "list existing",
			params: &a2a.ListTaskPushConfigRequest{TaskID: taskID},
			want: []*a2a.TaskPushConfig{
				{TaskID: taskID, Config: config1},
				{TaskID: taskID, Config: config2},
			},
		},
		{
			name:   "list with empty task",
			params: &a2a.ListTaskPushConfigRequest{TaskID: emptyTaskID},
			want:   []*a2a.TaskPushConfig{},
		},
		{
			name:   "list non-existent task",
			params: &a2a.ListTaskPushConfigRequest{TaskID: "non-existent-task"},
			want:   []*a2a.TaskPushConfig{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := handler.ListTaskPushConfig(ctx, tc.params)
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

func TestRequestHandler_DeleteTaskPushConfig(t *testing.T) {
	ctx := t.Context()
	taskID := a2a.TaskID("test-task")
	config1 := a2a.PushConfig{ID: "config-1", URL: "https://example.com/push1"}
	config2 := a2a.PushConfig{ID: "config-2", URL: "https://example.com/push2"}

	testCases := []struct {
		name       string
		params     *a2a.DeleteTaskPushConfigRequest
		wantRemain []*a2a.TaskPushConfig
	}{
		{
			name:       "delete existing",
			params:     &a2a.DeleteTaskPushConfigRequest{TaskID: taskID, ID: config1.ID},
			wantRemain: []*a2a.TaskPushConfig{{TaskID: taskID, Config: config2}},
		},
		{
			name:   "delete non-existent config",
			params: &a2a.DeleteTaskPushConfigRequest{TaskID: taskID, ID: "non-existent"},
			wantRemain: []*a2a.TaskPushConfig{
				{TaskID: taskID, Config: config1},
				{TaskID: taskID, Config: config2},
			},
		},
		{
			name:   "delete from non-existent task",
			params: &a2a.DeleteTaskPushConfigRequest{TaskID: "non-existent-task", ID: config1.ID},
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
			err := handler.DeleteTaskPushConfig(ctx, tc.params)
			if err != nil {
				t.Errorf("OnDeleteTaskPushConfig() failed: %v", err)
				return
			}

			got, err := handler.ListTaskPushConfig(ctx, &a2a.ListTaskPushConfigRequest{TaskID: taskID})
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

type interceptExecCtxFn func(context.Context, *ExecutorContext) (context.Context, error)

func (fn interceptExecCtxFn) Intercept(ctx context.Context, execCtx *ExecutorContext) (context.Context, error) {
	return fn(ctx, execCtx)
}

// mockAgentExecutor is a mock of AgentExecutor.
type mockAgentExecutor struct {
	executeCalled       bool
	capturedContext     context.Context
	capturedExecContext *ExecutorContext

	ExecuteFunc func(ctx context.Context, execCtx *ExecutorContext) iter.Seq2[a2a.Event, error]
	CancelFunc  func(ctx context.Context, execCtx *ExecutorContext) iter.Seq2[a2a.Event, error]
}

var _ AgentExecutor = (*mockAgentExecutor)(nil)

func (m *mockAgentExecutor) Execute(ctx context.Context, execCtx *ExecutorContext) iter.Seq2[a2a.Event, error] {
	m.executeCalled = true
	m.capturedContext = ctx
	m.capturedExecContext = execCtx
	if m.ExecuteFunc != nil {
		return m.ExecuteFunc(ctx, execCtx)
	}
	return func(yield func(a2a.Event, error) bool) {}
}

func (m *mockAgentExecutor) Cancel(ctx context.Context, execCtx *ExecutorContext) iter.Seq2[a2a.Event, error] {
	if m.CancelFunc != nil {
		return m.CancelFunc(ctx, execCtx)
	}
	return func(yield func(a2a.Event, error) bool) {
		yield(nil, errors.New("Cancel() not implemented"))
	}
}

func newEventReplayAgent(toSend []a2a.Event, err error) *mockAgentExecutor {
	return &mockAgentExecutor{
		ExecuteFunc: func(ctx context.Context, execCtx *ExecutorContext) iter.Seq2[a2a.Event, error] {
			return func(yield func(a2a.Event, error) bool) {
				for _, event := range toSend {
					if !yield(event, nil) {
						return
					}
				}
				if err != nil {
					yield(nil, err)
				}
			}
		},
	}
}

func newTestHandler(opts ...RequestHandlerOption) RequestHandler {
	mockExec := newEventReplayAgent([]a2a.Event{}, nil)
	return NewHandler(mockExec, opts...)
}

func newAgentMessage(text string) *a2a.Message {
	m := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart(text))
	m.ID = "message-id"
	return m
}

func newUserMessage(task *a2a.Task, text string) *a2a.Message {
	m := a2a.NewMessageForTask(a2a.MessageRoleUser, task, a2a.NewTextPart(text))
	m.ID = "message-id"
	return m
}

func newTaskStatusUpdate(task a2a.TaskInfoProvider, state a2a.TaskState, msg string) *a2a.TaskStatusUpdateEvent {
	ue := a2a.NewStatusUpdateEvent(task, state, newAgentMessage(msg))
	ue.Status.Timestamp = &fixedTime
	return ue
}

func newFinalTaskStatusUpdate(task a2a.TaskInfoProvider, state a2a.TaskState, msg string) *a2a.TaskStatusUpdateEvent {
	return newTaskStatusUpdate(task, state, msg)
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

func newArtifactEvent(task a2a.TaskInfoProvider, aid a2a.ArtifactID, parts ...*a2a.Part) *a2a.TaskArtifactUpdateEvent {
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
