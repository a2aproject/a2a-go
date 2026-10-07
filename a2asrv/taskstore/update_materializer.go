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

package taskstore

import (
	"context"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

// UpdateMaterializer computes [UpdateRequest.Task] from [UpdateRequest.PrevTask] and [UpdateRequest.Event] before
// [Store.Update] is called. Custom implementations can be used to track only the parts of the task state a [Store] needs.
// For example, [NewNoOpUpdateMaterializer] can be used if the store is only persisting events and does not need a
// task snapshot in memory.
type UpdateMaterializer interface {
	// ApplyUpdate is called for every [a2a.Task], [a2a.TaskStatusUpdateEvent] and [a2a.TaskArtifactUpdateEvent] produced by
	// an agent, and for a user [a2a.Message] which gets appended to the history of an existing task. The input task is passed to [Store.Update] as [UpdateRequest.PrevTask] and the output task is passed
	// to [Store.Update] as [UpdateRequest.Task]. The output task becomes the input task argument for the next invocation.
	//
	// The task argument can be:
	//   - the task returned by the previous call;
	//   - the full task state returned by [Store.Get] or passed to [Store.Create]. This happens when an execution starts,
	//     when an execution continues an existing task and when the SDK reloads the task to retry an update.
	//
	// Implementations MUST NOT modify the task or the event, because they can be shared with the store and event subscribers.
	ApplyUpdate(ctx context.Context, task *a2a.Task, event a2a.Event) (*a2a.Task, error)

	// ReturnsFullSnapshot reports whether tasks returned by [ApplyUpdate] are complete task snapshots as [Store.Get]
	// would return. The SDK can use this information to avoid unnecessary [Store.Get] calls when the full state
	// is already available in memory.
	ReturnsFullSnapshot() bool
}

type updateMaterializer struct {
	fn   func(*a2a.Task, a2a.Event) (*a2a.Task, error)
	full bool
}

// ApplyUpdate implements [UpdateMaterializer.ApplyUpdate].
func (m *updateMaterializer) ApplyUpdate(ctx context.Context, task *a2a.Task, event a2a.Event) (*a2a.Task, error) {
	return m.fn(task, event)
}

// ReturnsFullSnapshot implements [UpdateMaterializer.ReturnsFullSnapshot].
func (m *updateMaterializer) ReturnsFullSnapshot() bool {
	return m.full
}

// NewFullUpdateMaterializer is a utility for constructing a full [UpdateMaterializer] implementation from a pure function.
func NewFullUpdateMaterializer(fn func(*a2a.Task, a2a.Event) (*a2a.Task, error)) UpdateMaterializer {
	return &updateMaterializer{full: true, fn: fn}
}

// NewPartialUpdateMaterializer is a utility for constructing a partial [UpdateMaterializer] implementation from a pure function.
func NewPartialUpdateMaterializer(fn func(*a2a.Task, a2a.Event) (*a2a.Task, error)) UpdateMaterializer {
	return &updateMaterializer{full: false, fn: fn}
}

// NewNoOpUpdateMaterializer is a utility for constructing an [UpdateMaterializer] implementation which does not materialize
// the full [a2a.Task] state during the execution. It can be useful for a [Store] which is only persisting events.
func NewNoOpUpdateMaterializer() UpdateMaterializer {
	return &updateMaterializer{full: false, fn: func(t *a2a.Task, e a2a.Event) (*a2a.Task, error) {
		return &a2a.Task{
			ID: t.ID, ContextID: t.ContextID, Status: a2a.TaskStatus{State: t.Status.State},
		}, nil
	}}
}
