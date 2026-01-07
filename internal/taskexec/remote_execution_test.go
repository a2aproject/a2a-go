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
	"github.com/a2aproject/a2a-go/internal/testutil"
	"github.com/google/go-cmp/cmp"
)

func TestRemoteExecution_Events(t *testing.T) {
	tid := a2a.NewTaskID()

	tests := []struct {
		name           string
		events         []a2a.Event
		wantEvents     []a2a.Event
		getTaskResults []*a2a.Task
		wantResult     a2a.SendMessageResult
		getQueueErr    error
		getTaskErr     error
		wantErr        error
	}{
		{
			name: "terminal state task event is returned",
			events: []a2a.Event{
				&a2a.Task{ID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateWorking}},
				&a2a.Task{ID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}},
			},
			wantEvents: []a2a.Event{
				&a2a.Task{ID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateWorking}},
				&a2a.Task{ID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}},
			},
			wantResult: &a2a.Task{ID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}},
		},
		{
			name: "stored task is returned if final event was status update",
			events: []a2a.Event{
				&a2a.TaskStatusUpdateEvent{TaskID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateWorking}},
				&a2a.TaskStatusUpdateEvent{TaskID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}, Final: true},
			},
			wantEvents: []a2a.Event{
				&a2a.Task{ID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}},
				&a2a.TaskStatusUpdateEvent{TaskID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateWorking}},
				&a2a.TaskStatusUpdateEvent{TaskID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}, Final: true},
			},
			getTaskResults: []*a2a.Task{
				{ID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}},
				{ID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}, ContextID: "123"},
			},
			wantResult: &a2a.Task{ID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}, ContextID: "123"},
		},
		{
			name: "error if stored task in non-terminal state after final event",
			events: []a2a.Event{
				&a2a.TaskStatusUpdateEvent{TaskID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateWorking}},
				&a2a.TaskStatusUpdateEvent{TaskID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}, Final: true},
			},
			getTaskResults: []*a2a.Task{
				{ID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}},
				{ID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}},
			},
			wantErr: fmt.Errorf("execution finished in unexpected task state: submitted"),
		},
		{
			name:        "queue creation error",
			getQueueErr: fmt.Errorf("queue creation failed"),
			wantErr:     fmt.Errorf("queue creation failed"),
		},
		{
			name:       "task snapshot loading error",
			getTaskErr: fmt.Errorf("snapshot loading failed"),
			wantErr:    fmt.Errorf("snapshot loading failed"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := testutil.NewTestTaskStore()
			getResults := tc.getTaskResults
			store.GetFunc = func(context.Context, a2a.TaskID) (*a2a.Task, a2a.TaskVersion, error) {
				if len(getResults) == 0 && tc.getTaskErr != nil {
					return nil, nil, tc.getTaskErr
				}
				if len(getResults) == 0 {
					return nil, nil, a2a.ErrTaskNotFound
				}
				task := getResults[0]
				getResults = getResults[1:]
				return task, a2a.TaskVersionMissing, nil
			}

			queue := testutil.NewTestEventQueue()
			events := tc.events
			queue.ReadFunc = func(context.Context) (a2a.Event, a2a.TaskVersion, error) {
				event := events[0]
				events = events[1:]
				return event, a2a.TaskVersionMissing, nil
			}

			qm := testutil.NewTestQueueManager()
			qm.GetOrCreateFunc = func(ctx context.Context, taskID a2a.TaskID) (eventqueue.Queue, error) {
				return queue, tc.getQueueErr
			}

			exec := newRemoteExecution(qm, store, tid)
			var gotEvents []a2a.Event
			var gotErr error
			for event, err := range exec.Events(t.Context()) {
				if err != nil {
					gotErr = err
					break
				}
				gotEvents = append(gotEvents, event)
			}

			if gotErr != nil && tc.wantErr == nil {
				t.Fatalf("Events() error = %v, want nil", gotErr)
			}
			if gotErr == nil && tc.wantErr != nil {
				t.Fatalf("Events() error = nil, want %v", tc.wantErr)
			}
			if gotErr != nil && !strings.Contains(gotErr.Error(), tc.wantErr.Error()) {
				t.Fatalf("Events() error = %v, want %v", gotErr, tc.wantErr)
			}
			if gotErr == nil {
				if diff := cmp.Diff(tc.wantEvents, gotEvents); diff != "" {
					t.Errorf("Events() mismatch (-want +got):\n%s", diff)
				}
			}

			gotResult, gotResultErr := exec.Result(t.Context())
			if gotResultErr != nil && tc.wantErr == nil {
				t.Fatalf("Result() error = %v, want nil", gotResultErr)
			}
			if gotResultErr == nil && tc.wantErr != nil {
				t.Fatalf("Result() error = nil, want %v", tc.wantErr)
			}
			if gotResultErr != nil && !strings.Contains(gotResultErr.Error(), tc.wantErr.Error()) {
				t.Fatalf("Result() error = %v, want %v", gotResultErr, tc.wantErr)
			}
			if gotResultErr == nil {
				if diff := cmp.Diff(tc.wantResult, gotResult); diff != "" {
					t.Errorf("Result() mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}
