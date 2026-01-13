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

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
	"github.com/a2aproject/a2a-go/a2asrv/workqueue"
	"github.com/a2aproject/a2a-go/internal/testutil"
	"github.com/google/go-cmp/cmp"
)

func TestClusterFrontend_GetExecution(t *testing.T) {
	taskSeed := &a2a.Task{ID: a2a.NewTaskID()}

	tests := []struct {
		name        string
		taskID      a2a.TaskID
		getTaskErr  error
		wantSuccess bool
		wantErr     bool
	}{
		{
			name:        "task exists",
			taskID:      taskSeed.ID,
			wantSuccess: true,
		},
		{
			name:        "task does not exist",
			taskID:      a2a.NewTaskID(),
			wantSuccess: false,
		},
		{
			name:        "store error",
			getTaskErr:  fmt.Errorf("store failed"),
			wantSuccess: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := testutil.NewTestTaskStore().WithTasks(t, taskSeed)
			store.GetFunc = func(ctx context.Context, tid a2a.TaskID) (*a2a.Task, a2a.TaskVersion, error) {
				if tc.getTaskErr != nil {
					return nil, a2a.TaskVersionMissing, tc.getTaskErr
				}
				return store.Mem.Get(ctx, tid)
			}

			frontend := NewClusterFrontend(&ClusterConfig{
				TaskStore:    store,
				QueueManager: testutil.NewTestQueueManager(),
				WorkQueue:    testutil.NewTestWorkQueue(),
			})

			_, err := frontend.Resubscribe(t.Context(), tc.taskID)
			if err != nil && tc.wantSuccess {
				t.Errorf("GetExecution() error = %v, want %v", err, tc.wantSuccess)
			}
		})
	}
}

func TestClusterFrontend_Execute(t *testing.T) {
	taskSeed := &a2a.Task{ID: a2a.NewTaskID()}
	terminalStateTaskSeed := &a2a.Task{ID: a2a.NewTaskID(), Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}}

	tests := []struct {
		name           string
		params         *a2a.MessageSendParams
		storedTask     *a2a.Task
		getTaskErr     error
		getQueueErr    error
		writeQueueErr  error
		queueClosed    bool
		wantTaskID     a2a.TaskID
		wantQueueWrite *workqueue.Payload
		wantErr        error
	}{
		{
			name:   "execute new task",
			params: &a2a.MessageSendParams{Message: &a2a.Message{Role: a2a.MessageRoleUser}},
			wantQueueWrite: &workqueue.Payload{
				Type: workqueue.PayloadTypeExecute,
				ExecuteParams: &a2a.MessageSendParams{
					Message: &a2a.Message{Role: a2a.MessageRoleUser},
				},
			},
		},
		{
			name:   "execute existing task",
			params: &a2a.MessageSendParams{Message: &a2a.Message{TaskID: taskSeed.ID, Role: a2a.MessageRoleUser}},
			wantQueueWrite: &workqueue.Payload{
				TaskID: taskSeed.ID,
				Type:   workqueue.PayloadTypeExecute,
				ExecuteParams: &a2a.MessageSendParams{
					Message: &a2a.Message{TaskID: taskSeed.ID, Role: a2a.MessageRoleUser},
				},
			},
		},
		{
			name:    "nil request",
			wantErr: a2a.ErrInvalidParams,
		},
		{
			name:    "nil message field",
			params:  &a2a.MessageSendParams{},
			wantErr: a2a.ErrInvalidParams,
		},
		{
			name:       "task not found",
			params:     &a2a.MessageSendParams{Message: &a2a.Message{TaskID: a2a.NewTaskID()}},
			getTaskErr: a2a.ErrTaskNotFound,
			wantErr:    a2a.ErrTaskNotFound,
		},
		{
			name:       "store error",
			params:     &a2a.MessageSendParams{Message: &a2a.Message{TaskID: taskSeed.ID}},
			getTaskErr: fmt.Errorf("store failed"),
			wantErr:    fmt.Errorf("store failed"),
		},
		{
			name: "context ID mismatch",
			params: &a2a.MessageSendParams{
				Message: &a2a.Message{TaskID: taskSeed.ID, ContextID: "not-" + taskSeed.ContextID},
			},
			wantErr: a2a.ErrInvalidParams,
		},
		{
			name:    "task in terminal state",
			params:  &a2a.MessageSendParams{Message: &a2a.Message{TaskID: terminalStateTaskSeed.ID}},
			wantErr: a2a.ErrInvalidParams,
		},
		{
			name:        "queue creation error",
			params:      &a2a.MessageSendParams{Message: &a2a.Message{Role: a2a.MessageRoleUser}},
			getQueueErr: fmt.Errorf("queue failed"),
			wantErr:     fmt.Errorf("queue failed"),
		},
		{
			name:          "work queue write error - closes queue",
			params:        &a2a.MessageSendParams{Message: &a2a.Message{Role: a2a.MessageRoleUser}},
			writeQueueErr: fmt.Errorf("write failed"),
			queueClosed:   true,
			wantErr:       fmt.Errorf("write failed"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			store := testutil.NewTestTaskStore().WithTasks(t, taskSeed, terminalStateTaskSeed)
			store.GetFunc = func(ctx context.Context, tid a2a.TaskID) (*a2a.Task, a2a.TaskVersion, error) {
				if tc.getTaskErr != nil {
					return nil, a2a.TaskVersionMissing, tc.getTaskErr
				}
				return store.Mem.Get(ctx, tid)
			}

			queue := testutil.NewTestEventQueue()
			queueClosed := false
			queue.CloseFunc = func() error {
				queueClosed = true
				return nil
			}
			qm := testutil.NewTestQueueManager()
			qm.GetOrCreateFunc = func(ctx context.Context, taskID a2a.TaskID) (eventqueue.Queue, error) {
				return queue, tc.getQueueErr
			}

			wq := testutil.NewTestWorkQueue()
			wq.WriteErr = tc.writeQueueErr
			frontend := NewClusterFrontend(&ClusterConfig{
				TaskStore:    store,
				QueueManager: qm,
				WorkQueue:    wq,
			})

			sub, err := frontend.Execute(ctx, tc.params)
			if err != nil {
				if tc.wantErr == nil {
					t.Fatalf("Execute() error = %v, want nil", err)
				}
				if !strings.Contains(err.Error(), tc.wantErr.Error()) {
					t.Fatalf("Execute() error = %v, want %v", err, tc.wantErr)
				}
				if tc.queueClosed && !queueClosed {
					t.Error("queue was not closed on write error")
				}
				return
			}

			if tc.wantErr != nil {
				t.Fatalf("Execute() error = nil, want %v", tc.wantErr)
			}
			if sub == nil {
				t.Error("Execute() returned nil execution or subscription")
			}
			if tc.wantQueueWrite != nil && len(wq.Payloads) != 1 {
				t.Errorf("Execute() wrote %d payloads, want %v", len(wq.Payloads), *tc.wantQueueWrite)
			}
			if queueClosed {
				t.Error("Execute() success, but queue was closed")
			}
			if tc.wantQueueWrite != nil {
				if tc.wantQueueWrite.TaskID == "" { // check task ID was generated
					if wq.Payloads[0].TaskID == "" {
						t.Fatalf("Execute() wrote payload without taskID")
					}
					tc.wantQueueWrite.TaskID = wq.Payloads[0].TaskID
				}
				if diff := cmp.Diff(tc.wantQueueWrite, wq.Payloads[0]); diff != "" {
					t.Errorf("Execute() payload mismatch (+got,-want):\n%s", diff)
				}
			}
		})
	}
}

func TestClusterFrontend_Cancel(t *testing.T) {
	tid := a2a.NewTaskID()

	tests := []struct {
		name           string
		getTaskResults []*a2a.Task
		getTaskErr     error
		writeQueueErr  error
		wantQueueWrite *workqueue.Payload
		wantResult     *a2a.Task
		wantErrContain string
	}{
		{
			name: "cancel running task",
			getTaskResults: []*a2a.Task{
				{ID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateWorking}},
				{ID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateCanceled}},
			},
			wantQueueWrite: &workqueue.Payload{
				Type:         workqueue.PayloadTypeCancel,
				TaskID:       tid,
				CancelParams: &a2a.TaskIDParams{ID: tid},
			},
			wantResult: &a2a.Task{ID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateCanceled}},
		},
		{
			name: "failed to cancel task",
			getTaskResults: []*a2a.Task{
				{ID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateWorking}},
				{ID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}},
			},
			wantQueueWrite: &workqueue.Payload{
				Type:         workqueue.PayloadTypeCancel,
				TaskID:       tid,
				CancelParams: &a2a.TaskIDParams{ID: tid},
			},
			wantErrContain: a2a.ErrTaskNotCancelable.Error(),
		},
		{
			name:           "already canceled",
			getTaskResults: []*a2a.Task{{ID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateCanceled}}},
			wantResult:     &a2a.Task{ID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateCanceled}},
		},
		{
			name:           "non-cancelable state",
			getTaskResults: []*a2a.Task{{ID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}}},
			wantErrContain: a2a.ErrTaskNotCancelable.Error(),
		},
		{
			name:           "task not found",
			getTaskErr:     a2a.ErrTaskNotFound,
			wantErrContain: a2a.ErrTaskNotFound.Error(),
		},
		{
			name:           "store error",
			getTaskErr:     fmt.Errorf("store failed"),
			wantErrContain: "store failed",
		},
		{
			name:           "work queue write error",
			getTaskResults: []*a2a.Task{{ID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateWorking}}},
			writeQueueErr:  fmt.Errorf("write failed"),
			wantErrContain: "write failed",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			store := testutil.NewTestTaskStore()
			getResults := tc.getTaskResults
			store.GetFunc = func(ctx context.Context, taskID a2a.TaskID) (*a2a.Task, a2a.TaskVersion, error) {
				if len(getResults) == 0 && tc.getTaskErr != nil {
					return nil, a2a.TaskVersionMissing, tc.getTaskErr
				}
				if len(getResults) == 0 {
					return nil, a2a.TaskVersionMissing, a2a.ErrTaskNotFound
				}
				result := getResults[0]
				getResults = getResults[1:]
				return result, a2a.TaskVersionMissing, nil
			}

			wq := testutil.NewTestWorkQueue()
			wq.WriteErr = tc.writeQueueErr

			frontend := NewClusterFrontend(&ClusterConfig{
				TaskStore:    store,
				QueueManager: testutil.NewTestQueueManager(),
				WorkQueue:    wq,
			})

			gotTask, err := frontend.Cancel(t.Context(), &a2a.TaskIDParams{ID: tid})
			if err != nil {
				if tc.wantErrContain == "" {
					t.Fatalf("Cancel() error = %v, want nil", err)
				}
				if !strings.Contains(err.Error(), tc.wantErrContain) {
					t.Fatalf("Cancel() error = %v, want %v", err, tc.wantErrContain)
				}
				return
			}
			if tc.wantErrContain != "" {
				t.Fatalf("Cancel() error = nil, want %v", tc.wantErrContain)
			}
			if tc.wantQueueWrite == nil && len(wq.Payloads) != 0 {
				t.Fatalf("Cancel() wrote %d payloads, want 0", len(wq.Payloads))
			}
			if tc.wantQueueWrite != nil && len(wq.Payloads) != 1 {
				t.Fatalf("Cancel() wrote %d payloads, want %v", len(wq.Payloads), tc.wantQueueWrite)
			}
			if tc.wantQueueWrite != nil {
				if diff := cmp.Diff(tc.wantQueueWrite, wq.Payloads[0]); diff != "" {
					t.Errorf("Cancel() payload mismatch (-want +got):\n%s", diff)
				}
			}
			if diff := cmp.Diff(tc.wantResult, gotTask); diff != "" {
				t.Errorf("Cancel() result mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
