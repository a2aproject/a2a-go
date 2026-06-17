// Copyright 2026 The A2A Authors
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

package eventqueue_test

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/a2asrv/eventqueue"
	"github.com/a2aproject/a2a-go/v2/a2asrv/workqueue"
	"github.com/a2aproject/a2a-go/v2/internal/testutil"
	"github.com/a2aproject/a2a-go/v2/internal/testutil/testexecutor"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestPullQueue_SubscribeToTask(t *testing.T) {
	t.Parallel()

	mockPullerErr := errors.New("puller error on snapshot fetch")

	testCases := []struct {
		name         string
		snapshotFn   func(taskID a2a.TaskID) *a2a.Task
		eventsFn     func(taskID a2a.TaskID) []*eventqueue.Message
		wantEventsFn func(taskID a2a.TaskID) []a2a.Event
		wantErr      error
		snapshotErr  error
		accessCheck  func(context.Context, *a2a.Task) error
	}{
		{
			name: "snapshot then final completed event",
			snapshotFn: func(taskID a2a.TaskID) *a2a.Task {
				return &a2a.Task{
					ID:     taskID,
					Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted},
				}
			},
			eventsFn: func(taskID a2a.TaskID) []*eventqueue.Message {
				return []*eventqueue.Message{
					{
						Event:       newFinalEvent(taskID, "Done"),
						TaskVersion: 2,
					},
				}
			},
			wantEventsFn: func(taskID a2a.TaskID) []a2a.Event {
				return []a2a.Event{
					&a2a.Task{ID: taskID, Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}},
					newFinalEvent(taskID, "Done"),
				}
			},
		},
		{
			name: "multiple events",
			snapshotFn: func(taskID a2a.TaskID) *a2a.Task {
				return &a2a.Task{
					ID:     taskID,
					Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted},
				}
			},
			eventsFn: func(taskID a2a.TaskID) []*eventqueue.Message {
				return []*eventqueue.Message{
					{
						Event:       &a2a.TaskStatusUpdateEvent{TaskID: taskID, Status: a2a.TaskStatus{State: a2a.TaskStateWorking}},
						TaskVersion: 2,
					},
					{
						Event:       &a2a.TaskArtifactUpdateEvent{TaskID: taskID, Artifact: &a2a.Artifact{ID: "artifactId", Parts: []*a2a.Part{a2a.NewDataPart(map[string]string{"foo": "bar"})}}},
						TaskVersion: 3,
					},
					{
						Event:       newFinalEvent(taskID, "Done"),
						TaskVersion: 4,
					},
				}
			},
			wantEventsFn: func(taskID a2a.TaskID) []a2a.Event {
				return []a2a.Event{
					&a2a.Task{ID: taskID, Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}},
					&a2a.TaskStatusUpdateEvent{TaskID: taskID, Status: a2a.TaskStatus{State: a2a.TaskStateWorking}},
					&a2a.TaskArtifactUpdateEvent{TaskID: taskID, Artifact: &a2a.Artifact{ID: "artifactId", Parts: []*a2a.Part{a2a.NewDataPart(map[string]string{"foo": "bar"})}}},
					newFinalEvent(taskID, "Done"),
				}
			},
		},
		{
			name: "terminal snapshot",
			snapshotFn: func(taskID a2a.TaskID) *a2a.Task {
				return &a2a.Task{
					ID:     taskID,
					Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
				}
			},
			wantEventsFn: func(taskID a2a.TaskID) []a2a.Event {
				return []a2a.Event{
					&a2a.Task{ID: taskID, Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}},
				}
			},
		},
		{
			name: "input required snapshot",
			snapshotFn: func(taskID a2a.TaskID) *a2a.Task {
				return &a2a.Task{
					ID:     taskID,
					Status: a2a.TaskStatus{State: a2a.TaskStateInputRequired},
				}
			},
			wantEventsFn: func(taskID a2a.TaskID) []a2a.Event {
				return []a2a.Event{
					&a2a.Task{ID: taskID, Status: a2a.TaskStatus{State: a2a.TaskStateInputRequired}},
				}
			},
			wantErr: eventqueue.ErrQueueClosed,
		},
		{
			name: "stream ending with message",
			snapshotFn: func(taskID a2a.TaskID) *a2a.Task {
				return &a2a.Task{
					ID:     taskID,
					Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted},
				}
			},
			eventsFn: func(taskID a2a.TaskID) []*eventqueue.Message {
				return []*eventqueue.Message{
					{
						Event:       a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("hi")),
						TaskVersion: 2,
					},
				}
			},
			wantEventsFn: func(taskID a2a.TaskID) []a2a.Event {
				return []a2a.Event{
					&a2a.Task{ID: taskID, Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}},
					a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("hi")),
				}
			},
		},
		{
			name: "snapshot error",
			snapshotFn: func(taskID a2a.TaskID) *a2a.Task {
				return &a2a.Task{
					ID:     taskID,
					Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted},
				}
			},
			snapshotErr: mockPullerErr,
			wantErr:     mockPullerErr,
		},
		{
			name: "access check fails",
			// remoteSubscription yields the snapshot before the Read loop,
			// then AccessCheck rejects on the first Read and prevents subsequent events
			// from reaching the subscriber.
			snapshotFn: func(taskID a2a.TaskID) *a2a.Task {
				return &a2a.Task{
					ID:     taskID,
					Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted},
				}
			},
			eventsFn: func(taskID a2a.TaskID) []*eventqueue.Message {
				return []*eventqueue.Message{
					{
						Event:       a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("hi")),
						TaskVersion: 2,
					},
				}
			},
			wantEventsFn: func(taskID a2a.TaskID) []a2a.Event {
				return []a2a.Event{
					&a2a.Task{ID: taskID, Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}},
				}
			},
			accessCheck: func(ctx context.Context, task *a2a.Task) error {
				return a2a.ErrUnauthorized
			},
			wantErr: a2a.ErrUnauthorized,
		},
	}
	wantCloseCount := int32(1)
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()

			taskID := a2a.NewTaskID()
			var snapshot *a2a.Task
			var events []*eventqueue.Message
			var wantEvents []a2a.Event
			if tc.snapshotFn != nil {
				snapshot = tc.snapshotFn(taskID)
			}
			if tc.eventsFn != nil {
				events = tc.eventsFn(taskID)
			}
			if tc.wantEventsFn != nil {
				wantEvents = tc.wantEventsFn(taskID)
			}

			env := setupTest(t, &testEnvOptions{
				snapshot:    snapshot,
				events:      events,
				snapshotErr: tc.snapshotErr,
				accessCheck: tc.accessCheck,
			})
			reqHandler := *env.handler
			var gotEvents []a2a.Event
			var gotErr error
			for ev, err := range reqHandler.SubscribeToTask(ctx, &a2a.SubscribeToTaskRequest{ID: taskID}) {
				if err != nil {
					gotErr = err
					break
				}
				gotEvents = append(gotEvents, ev)
			}

			if !errors.Is(gotErr, tc.wantErr) {
				t.Fatalf("SubscribeToTask() error = %v, want %v", gotErr, tc.wantErr)
			}

			opts := []cmp.Option{
				cmpopts.IgnoreFields(a2a.Message{}, "ID"),
			}
			if diff := cmp.Diff(wantEvents, gotEvents, opts...); diff != "" {
				t.Fatalf("SubscribeToTask() events mismatch (-want +got):\n%s", diff)
			}
			gotCloseCount := env.puller.closeCount.Load()
			if gotCloseCount != wantCloseCount {
				t.Fatalf("SubscribeToTask() close count = %d, want %d", gotCloseCount, wantCloseCount)
			}
		})
	}
}

func TestPullQueue_InactivityTimeout(t *testing.T) {
	t.Parallel()

	ctx, cancelTimeout := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancelTimeout()

	tid := a2a.NewTaskID()
	testEnv := setupTest(t, &testEnvOptions{
		snapshot:          &a2a.Task{ID: tid},
		inactivityTimeout: 5 * time.Millisecond,
	})
	reqHandler := *testEnv.handler

	eventCount := 0
	req := &a2a.SubscribeToTaskRequest{ID: tid}
	var gotErr error
	for ev, err := range reqHandler.SubscribeToTask(ctx, req) {
		if err != nil {
			gotErr = err
		} else if ev != nil {
			eventCount++
		}
	}

	if eventCount != 1 {
		t.Fatalf("expected 1 event (snapshot), got %d", eventCount)
	}
	if !errors.Is(gotErr, eventqueue.ErrInactivityTimeout) {
		t.Fatalf("reqHandler.OnResubscribeToTask() error = %v, want %v", gotErr, eventqueue.ErrInactivityTimeout)
	}
}

func TestPullQueue_SubscribeToNonExistentTask(t *testing.T) {
	t.Parallel()

	taskID := a2a.NewTaskID()
	snapshot := &a2a.Task{ID: taskID}
	testEnv := setupTest(t, &testEnvOptions{
		snapshot: snapshot,
	})
	reqHandler := *testEnv.handler

	req := &a2a.SubscribeToTaskRequest{ID: "non-existent-task"}

	var gotErr error
	for _, err := range reqHandler.SubscribeToTask(t.Context(), req) {
		if err != nil {
			gotErr = err
			break
		}
	}

	if !errors.Is(gotErr, a2a.ErrTaskNotFound) {
		t.Fatalf("reqHandler.OnResubscribeToTask() expected error for non-existent task, got %v", gotErr)
	}
}

type testEnv struct {
	handler *a2asrv.RequestHandler
	puller  *mockPuller
}

var _ eventqueue.Puller = (*mockPuller)(nil)

type mockPuller struct {
	snapshot    *a2a.Task
	events      []*eventqueue.Message
	snapshotErr error
	closeCount  atomic.Int32
}

func newMockPuller(snapshot *a2a.Task, events []*eventqueue.Message, snapshotErr error) *mockPuller {
	return &mockPuller{snapshot: snapshot, events: events, snapshotErr: snapshotErr}
}

func (m *mockPuller) Pull(ctx context.Context, taskID a2a.TaskID, cursor eventqueue.PullCursor) (*eventqueue.PullResponse, error) {
	if m.snapshotErr != nil {
		return nil, m.snapshotErr
	}
	if cursor == nil {
		return &eventqueue.PullResponse{
			Messages: []*eventqueue.Message{{Event: m.snapshot, TaskVersion: 1}},
			Cursor:   1,
		}, nil
	}
	idx, ok := cursor.(int)
	if !ok {
		return nil, fmt.Errorf("invalid cursor type %T", cursor)
	}
	if idx-1 >= len(m.events) {
		return &eventqueue.PullResponse{Messages: nil, Cursor: idx}, nil
	}
	return &eventqueue.PullResponse{
		Messages: []*eventqueue.Message{m.events[idx-1]},
		Cursor:   idx + 1,
	}, nil
}

func (m *mockPuller) Close(ctx context.Context) error {
	m.closeCount.Add(1)
	return nil
}

type testEnvOptions struct {
	snapshot          *a2a.Task
	events            []*eventqueue.Message
	snapshotErr       error
	inactivityTimeout time.Duration
	accessCheck       func(context.Context, *a2a.Task) error
}

func setupTest(t *testing.T, opts *testEnvOptions) *testEnv {
	t.Helper()
	if opts == nil {
		opts = &testEnvOptions{}
	}
	store := testutil.NewTestTaskStore()
	if opts.snapshot != nil {
		store.WithTasks(t, opts.snapshot)
	}
	puller := newMockPuller(opts.snapshot, opts.events, opts.snapshotErr)
	pp := eventqueue.NewStaticPullerProvider(puller)

	pullQueueManager := eventqueue.NewPullQueueManager(pp, eventqueue.PullConfig{
		InactivityTimeout: opts.inactivityTimeout,
		PollInterval:      5 * time.Millisecond,
		UseInMemory:       useInMemory,
		AccessCheck:       opts.accessCheck,
	})
	wq := workqueue.NewInMemory(nil)
	executor := &testexecutor.TestAgentExecutor{}
	reqHandler := a2asrv.NewHandler(
		executor,
		a2asrv.WithClusterMode(
			a2asrv.ClusterConfig{
				QueueManager: pullQueueManager,
				WorkQueue:    wq,
				TaskStore:    store,
			}),
	)
	return &testEnv{
		handler: &reqHandler,
		puller:  puller,
	}
}

func useInMemory(ctx context.Context) bool {
	cc, ok := a2asrv.CallContextFrom(ctx)
	if !ok {
		return true
	}
	return cc.Method() != "SubscribeToTask"
}

func newFinalEvent(taskID a2a.TaskID, text string) *a2a.TaskStatusUpdateEvent {
	return &a2a.TaskStatusUpdateEvent{
		TaskID: taskID,
		Status: a2a.TaskStatus{State: a2a.TaskStateCompleted, Message: a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart(text))},
	}
}
