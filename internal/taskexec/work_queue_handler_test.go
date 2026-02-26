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

package taskexec

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv/workqueue"
	"github.com/a2aproject/a2a-go/internal/testutil"
	"github.com/google/go-cmp/cmp"
)

func TestClusterBackend(t *testing.T) {
	tid := a2a.NewTaskID()

	tests := []struct {
		name           string
		payload        *workqueue.Payload
		executor       *testExecutor
		canceler       *testCanceler
		createQueueErr error
		wantResult     a2a.SendMessageResult
		wantErrContain string
	}{
		{
			name: "successful execution",
			payload: &workqueue.Payload{
				Type:           workqueue.PayloadTypeExecute,
				TaskID:         tid,
				ExecuteRequest: &a2a.SendMessageRequest{Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("test"))},
			},
			executor: &testExecutor{
				executeCalled: make(chan struct{}),
				emitTask:      &a2a.Task{ID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}},
				testProcessor: &testProcessor{nextEventTerminal: true},
			},
			wantResult: &a2a.Task{ID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}},
		},
		{
			name:           "executor creation failed",
			payload:        &workqueue.Payload{Type: workqueue.PayloadTypeExecute, TaskID: tid, ExecuteRequest: &a2a.SendMessageRequest{}},
			wantErrContain: "setup failed: executor was not provided",
		},
		{
			name:           "malformed execution payload",
			payload:        &workqueue.Payload{Type: workqueue.PayloadTypeExecute, TaskID: tid},
			wantErrContain: workqueue.ErrMalformedPayload.Error(),
		},
		{
			name:    "successful cancellation",
			payload: &workqueue.Payload{Type: workqueue.PayloadTypeCancel, TaskID: tid, CancelRequest: &a2a.CancelTaskRequest{ID: tid}},
			canceler: &testCanceler{
				cancelCalled:  make(chan struct{}),
				emitTask:      &a2a.Task{ID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateCanceled}},
				testProcessor: &testProcessor{nextEventTerminal: true},
			},
			wantResult: &a2a.Task{ID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateCanceled}},
		},
		{
			name:           "canceler creation failed",
			payload:        &workqueue.Payload{Type: workqueue.PayloadTypeCancel, TaskID: tid, CancelRequest: &a2a.CancelTaskRequest{ID: tid}},
			wantErrContain: "setup failed: canceler was not provided",
		},
		{
			name:           "malformed cancelation payload",
			payload:        &workqueue.Payload{Type: workqueue.PayloadTypeCancel, TaskID: tid},
			wantErrContain: workqueue.ErrMalformedPayload.Error(),
		},
		{
			name:    "executor run failed",
			payload: &workqueue.Payload{Type: workqueue.PayloadTypeExecute, TaskID: tid, ExecuteRequest: &a2a.SendMessageRequest{}},
			executor: &testExecutor{
				executeCalled: make(chan struct{}),
				executeErr:    fmt.Errorf("failed to execute"),
				testProcessor: &testProcessor{nextEventTerminal: true},
			},
			wantErrContain: "failed to execute",
		},
		{
			name:    "canceler run failed",
			payload: &workqueue.Payload{Type: workqueue.PayloadTypeCancel, TaskID: tid, CancelRequest: &a2a.CancelTaskRequest{ID: tid}},
			canceler: &testCanceler{
				cancelCalled:  make(chan struct{}),
				cancelErr:     fmt.Errorf("failed to cancel"),
				testProcessor: &testProcessor{},
			},
			wantErrContain: "failed to cancel",
		},
		{
			name:           "queue creation failed",
			payload:        &workqueue.Payload{Type: workqueue.PayloadTypeCancel, TaskID: tid, CancelRequest: &a2a.CancelTaskRequest{ID: tid}},
			canceler:       &testCanceler{},
			createQueueErr: fmt.Errorf("queue creation failed"),
			wantErrContain: "queue creation failed",
		},
		{
			name:           "unknown payload type",
			payload:        &workqueue.Payload{Type: "unknown"},
			wantErrContain: `unknown payload type: "unknown"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			qm := testutil.NewTestQueueManager()
			queue := testutil.NewTestEventQueue()
			queueClosed := false
			queue.CloseFunc = func() error {
				queueClosed = true
				return nil
			}

			queueCreated := false
			if tc.createQueueErr != nil {
				qm.SetError(tc.createQueueErr)
			} else {
				qm.SetQueue(queue)
			}

			factory := newStaticFactory(tc.executor, tc.canceler)
			wq := testutil.NewTestWorkQueue()
			cfg := &DistributedManagerConfig{
				QueueManager: qm,
				TaskStore:    testutil.NewTestTaskStore(),
				Factory:      factory,
				WorkQueue:    wq,
			}
			_ = newWorkQueueHandler(cfg)

			gotResult, gotErr := wq.HandlerFn(t.Context(), tc.payload)

			if gotErr != nil && tc.wantErrContain == "" {
				t.Fatalf("handle() error = %v, want nil", gotErr)
			}
			if gotErr == nil && tc.wantErrContain != "" {
				t.Fatalf("handle() error = nil, want %v", tc.wantErrContain)
			}
			if gotErr != nil && !strings.Contains(gotErr.Error(), tc.wantErrContain) {
				t.Fatalf("handle() error = %v, want %v", gotErr, tc.wantErrContain)
			}
			if gotErr == nil {
				if diff := cmp.Diff(tc.wantResult, gotResult); diff != "" {
					t.Errorf("handle() mismatch (-want +got):\n%s", diff)
				}
			}
			if queueCreated && !queueClosed {
				t.Errorf("queue was not closed")
			}
		})
	}
}

func TestClusterBackend_Heartbeater(t *testing.T) {
	executorBlock := make(chan struct{})
	heartbeats := 0
	heartbeater := &testHeartbeater{
		interval: 5 * time.Millisecond,
		onHeartbeat: func(ctx context.Context) error {
			heartbeats++
			if heartbeats == 3 {
				close(executorBlock)
			}
			return nil
		},
	}

	wantTask := &a2a.Task{ID: a2a.NewTaskID(), Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}}
	executor := newExecutor()
	executor.testProcessor.nextEventTerminal = true
	executor.emitTask = wantTask
	executor.block = executorBlock

	factory := newStaticFactory(executor, nil)
	wq := testutil.NewTestWorkQueue()
	cfg := &DistributedManagerConfig{
		QueueManager: testutil.NewTestQueueManager(),
		TaskStore:    testutil.NewTestTaskStore(),
		Factory:      factory,
		WorkQueue:    wq,
	}
	_ = newWorkQueueHandler(cfg)

	ctx := workqueue.AttachHeartbeater(t.Context(), heartbeater)
	gotResult, gotErr := wq.HandlerFn(ctx, &workqueue.Payload{
		Type:           workqueue.PayloadTypeExecute,
		TaskID:         executor.emitTask.ID,
		ExecuteRequest: &a2a.SendMessageRequest{},
	})
	if gotErr != nil {
		t.Fatalf("handler() error, want nil = %v", gotErr)
	}
	if diff := cmp.Diff(wantTask, gotResult); diff != "" {
		t.Errorf("handle() result mismatch (-want +got):\n%s", diff)
	}
}

type testHeartbeater struct {
	interval    time.Duration
	onHeartbeat func(ctx context.Context) error
}

func (t *testHeartbeater) HeartbeatInterval() time.Duration {
	return t.interval
}

func (t *testHeartbeater) Heartbeat(ctx context.Context) error {
	if t.onHeartbeat != nil {
		return t.onHeartbeat(ctx)
	}
	return nil
}
