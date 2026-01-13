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

package taskexec

import (
	"context"
	"fmt"
	"iter"
	"strings"
	"testing"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/internal/testutil"
	"github.com/google/go-cmp/cmp"
)

func TestRemoteSubscription_Events(t *testing.T) {
	tid := a2a.NewTaskID()

	tests := []struct {
		name            string
		snapshot        *a2a.Task
		snapshotVersion a2a.TaskVersion
		events          []a2a.Event
		eventVersions   []a2a.TaskVersion
		wantEvents      []a2a.Event
		getTaskErr      error
		queueReadErr    error
		wantErrContain  string
	}{
		{
			name:       "terminal task state event subscription",
			events:     []a2a.Event{&a2a.Task{ID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}}},
			wantEvents: []a2a.Event{&a2a.Task{ID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}}},
		},
		{
			name:       "input-required task event ends subscription",
			events:     []a2a.Event{&a2a.Task{ID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateInputRequired}}},
			wantEvents: []a2a.Event{&a2a.Task{ID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateInputRequired}}},
		},
		{
			name:     "task snapshot emitted before events subscription",
			snapshot: &a2a.Task{ID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}},
			events:   []a2a.Event{&a2a.Task{ID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}}},
			wantEvents: []a2a.Event{
				&a2a.Task{ID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}},
				&a2a.Task{ID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}},
			},
		},
		{
			name:       "terminal task state snapshot ends subscription",
			snapshot:   &a2a.Task{ID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}},
			events:     []a2a.Event{&a2a.Task{ID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateFailed}}}, // not received
			wantEvents: []a2a.Event{&a2a.Task{ID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}}},
		},
		{
			name:     "final task status update event ends subscription",
			snapshot: &a2a.Task{ID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}},
			events: []a2a.Event{
				&a2a.TaskStatusUpdateEvent{TaskID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateWorking}},
				&a2a.TaskStatusUpdateEvent{TaskID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}, Final: true},
				&a2a.TaskStatusUpdateEvent{TaskID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateFailed}}, // not received
			},
			wantEvents: []a2a.Event{
				&a2a.Task{ID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}},
				&a2a.TaskStatusUpdateEvent{TaskID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateWorking}},
				&a2a.TaskStatusUpdateEvent{TaskID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}, Final: true},
			},
		},
		{
			name:            "events older than snapshot are skipped",
			snapshot:        &a2a.Task{ID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateInputRequired}},
			snapshotVersion: a2a.TaskVersion(2),
			events: []a2a.Event{
				&a2a.TaskStatusUpdateEvent{TaskID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}},
				&a2a.TaskStatusUpdateEvent{TaskID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateInputRequired}},
				&a2a.TaskStatusUpdateEvent{TaskID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateWorking}},
				&a2a.TaskStatusUpdateEvent{TaskID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}, Final: true},
			},
			eventVersions: []a2a.TaskVersion{
				// older than snapshot
				a2a.TaskVersion(1), a2a.TaskVersion(2),
				// newer than snapshot
				a2a.TaskVersion(3), a2a.TaskVersion(4),
			},
			wantEvents: []a2a.Event{
				&a2a.Task{ID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateInputRequired}},
				&a2a.TaskStatusUpdateEvent{TaskID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateWorking}},
				&a2a.TaskStatusUpdateEvent{TaskID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}, Final: true},
			},
		},
		{
			name:       "queue read error ends subscription",
			snapshot:   &a2a.Task{ID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}},
			events:     []a2a.Event{&a2a.Task{ID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateFailed}}}, // not received
			wantEvents: []a2a.Event{&a2a.Task{ID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}}},
		},
		{
			name:           "error if task loading fails",
			getTaskErr:     fmt.Errorf("db unavailable"),
			wantErrContain: "db unavailable",
		},
		{
			name:           "error if queue read fails",
			events:         []a2a.Event{&a2a.Task{ID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateWorking}}},
			queueReadErr:   fmt.Errorf("queue failed"),
			wantEvents:     []a2a.Event{&a2a.Task{ID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateWorking}}},
			wantErrContain: "queue failed",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := testutil.NewTestTaskStore()
			if tc.getTaskErr != nil {
				store.GetFunc = func(context.Context, a2a.TaskID) (*a2a.Task, a2a.TaskVersion, error) {
					return nil, a2a.TaskVersionMissing, tc.getTaskErr
				}
			} else if tc.snapshot != nil {
				version := a2a.TaskVersionMissing
				if tc.snapshotVersion != a2a.TaskVersionMissing {
					version = tc.snapshotVersion
				}
				store.GetFunc = func(context.Context, a2a.TaskID) (*a2a.Task, a2a.TaskVersion, error) {
					return tc.snapshot, version, nil
				}
			}

			queue := testutil.NewTestEventQueue()
			events, eventVersions := tc.events, tc.eventVersions
			queue.ReadFunc = func(context.Context) (a2a.Event, a2a.TaskVersion, error) {
				if len(events) == 0 {
					return nil, a2a.TaskVersionMissing, tc.queueReadErr
				}
				version := a2a.TaskVersionMissing
				if len(eventVersions) > 0 {
					version = eventVersions[0]
					eventVersions = eventVersions[1:]
				}
				event := events[0]
				events = events[1:]
				return event, version, nil
			}
			queueClosed := false
			queue.CloseFunc = func() error {
				queueClosed = true
				return nil
			}

			sub := newRemoteSubscription(queue, store, tid)
			var gotEvents []a2a.Event
			var gotErr error
			for event, err := range sub.Events(t.Context()) {
				if err != nil {
					gotErr = err
					break
				}
				gotEvents = append(gotEvents, event)
			}

			if !queueClosed {
				t.Fatalf("queue was not closed by consumed subscription")
			}
			if gotErr != nil && tc.wantErrContain == "" {
				t.Fatalf("Events() error = %v, want nil", gotErr)
			}
			if gotErr == nil && tc.wantErrContain != "" {
				t.Fatalf("Events() error = nil, want %v", tc.wantErrContain)
			}
			if gotErr != nil && !strings.Contains(gotErr.Error(), tc.wantErrContain) {
				t.Fatalf("Events() error = nil, want %v", tc.wantErrContain)
			}
			if diff := cmp.Diff(tc.wantEvents, gotEvents); diff != "" {
				t.Fatalf("Events() result mismatch (+got,-want):\n%s", diff)
			}
		})
	}
}

func TestRemoteSubscription_ErrorOnDoubleConsumption(t *testing.T) {
	ctx := t.Context()
	task := &a2a.Task{ID: a2a.NewTaskID()}
	queue := testutil.NewTestEventQueue()
	store := testutil.NewTestTaskStore().WithTasks(t, task)
	sub := newRemoteSubscription(queue, store, task.ID)

	if _, err := consumeOne(sub.Events(ctx)); err != nil {
		t.Fatalf("sub.Events(ctx) failed with %v, want task snapshot", err)
	}
	if _, err := consumeOne(sub.Events(ctx)); err == nil {
		t.Fatalf("sub.Events(ctx) returned an element after double consumption, want err")
	}
}

func consumeOne(seq iter.Seq2[a2a.Event, error]) (a2a.Event, error) {
	next, stop := iter.Pull2(seq)
	event, err, _ := next()
	stop()
	return event, err
}
