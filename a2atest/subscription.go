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

package a2atest

import (
	"iter"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

// CollectEvents drains an [iter.Seq2[a2a.Event, error]] iterator and returns
// all events in order. If the iterator yields an error, the events collected
// so far are returned alongside the error.
//
// This is the primary helper for testing [a2asrv.RequestHandler.SubscribeToTask]:
//
//	events, err := a2atest.CollectEvents(server.SubscribeToTask(ctx, req))
//	if err != nil {
//	    t.Fatal(err)
//	}
func CollectEvents(seq iter.Seq2[a2a.Event, error]) ([]a2a.Event, error) {
	var events []a2a.Event
	for event, err := range seq {
		if err != nil {
			return events, err
		}
		events = append(events, event)
	}
	return events, nil
}

// FindEvent returns the first event in the iterator for which pred returns true.
// Returns nil, nil if no matching event is found before the iterator ends.
// The iterator is consumed until a match is found or it finishes.
func FindEvent(seq iter.Seq2[a2a.Event, error], pred func(a2a.Event) bool) (a2a.Event, error) {
	for event, err := range seq {
		if err != nil {
			return nil, err
		}
		if pred(event) {
			return event, nil
		}
	}
	return nil, nil
}

// IsTerminal returns true if the given task state is terminal
// (completed, failed, canceled, or rejected).
func IsTerminal(state a2a.TaskState) bool {
	switch state {
	case a2a.TaskStateCompleted, a2a.TaskStateFailed,
		a2a.TaskStateCanceled, a2a.TaskStateRejected:
		return true
	}
	return false
}

// FinalTaskState returns the final task state from a SubscribeToTask
// event stream. It returns the state and the last task snapshot seen.
// Returns an error if the iterator yields an error or terminates
// without reaching a terminal state.
//
// Usage:
//
//	state, task, err := a2atest.FinalTaskState(server.SubscribeToTask(ctx, req))
//	if err != nil {
//	    t.Fatal(err)
//	}
//	if state != a2a.TaskStateCompleted {
//	    t.Fatalf("expected completed, got %v", state)
//	}
func FinalTaskState(seq iter.Seq2[a2a.Event, error]) (a2a.TaskState, *a2a.Task, error) {
	var lastTask *a2a.Task
	for event, err := range seq {
		if err != nil {
			return a2a.TaskStateUnspecified, lastTask, err
		}
		if task, ok := event.(*a2a.Task); ok {
			lastTask = task
		}
		if update, ok := event.(*a2a.TaskStatusUpdateEvent); ok {
			if IsTerminal(update.Status.State) {
				return update.Status.State, lastTask, nil
			}
		}
	}
	return a2a.TaskStateUnspecified, lastTask, nil
}
