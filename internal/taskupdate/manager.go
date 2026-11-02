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

package taskupdate

import (
	"context"
	"errors"
	"fmt"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"
	"github.com/a2aproject/a2a-go/v2/internal/utils"
)

const maxCancelationAttempts = 10

// Manager is used for processing [a2a.Event] related to an [a2a.Task]. It updates
// the Task accordingly and uses [taskstore.Store] to store the new state.
type Manager struct {
	taskInfo     a2a.TaskInfo
	store        taskstore.Store
	materializer taskstore.UpdateMaterializer
	initTask     *taskstore.StoredTask

	// tracked is the state produced by the materializer. It can be shared with the store and event subscribers,
	// so it must never be modified in place.
	tracked *a2a.Task
	// state is tracked separately, because custom materializers are not required to track it.
	state a2a.TaskState
	// version is the last known stored version.
	version taskstore.TaskVersion
}

// NewManager is a [Manager] constructor function.
func NewManager(store taskstore.Store, info a2a.TaskInfo, task *taskstore.StoredTask, m taskstore.UpdateMaterializer) (*Manager, error) {
	mgr := &Manager{taskInfo: info, store: store, materializer: m, initTask: task}
	if mgr.materializer == nil {
		return nil, fmt.Errorf("materializer must be set")
	}
	if task != nil {
		taskCopy, err := utils.DeepCopy(task.Task)
		if err != nil {
			return nil, err
		}
		mgr.track(taskCopy, task.Version, taskCopy.Status.State)
	}
	return mgr, nil
}

// InMemorySnapshot returns the latest materialized task state if update manager has it.
func (mgr *Manager) InMemorySnapshot() (*taskstore.StoredTask, bool) {
	if mgr.tracked != nil && mgr.materializer.ReturnsFullSnapshot() {
		return &taskstore.StoredTask{Task: mgr.tracked, Version: mgr.version}, true
	}
	return nil, false
}

// SetTaskFailed attempts to move the Task to failed state. It fails if the task is already in a terminal state.
// The store receives a synthesized failed [a2a.TaskStatusUpdateEvent], so that stores which derive
// the task state from events record the failure.
func (mgr *Manager) SetTaskFailed(ctx context.Context) error {
	if mgr.tracked == nil {
		return fmt.Errorf("execution failed before a task was created")
	}
	if mgr.state.Terminal() {
		return fmt.Errorf("%q task can not be moved to failed state", mgr.state)
	}
	_, err := mgr.apply(ctx, a2a.NewStatusUpdateEvent(mgr.taskInfo, a2a.TaskStateFailed, nil))
	return err
}

// Process validates the event associated with the managed [a2a.Task] and integrates the new state into it.
func (mgr *Manager) Process(ctx context.Context, event a2a.Event) (taskstore.TaskVersion, error) {
	if _, ok := event.(*a2a.Message); ok {
		if mgr.tracked != nil {
			return taskstore.TaskVersionMissing, fmt.Errorf("message not allowed after task was stored: %w", a2a.ErrInvalidAgentResponse)
		}
		return taskstore.TaskVersionMissing, nil
	}

	if mgr.state.Terminal() {
		if v, ok := event.(*a2a.Task); ok {
			if mgr.initTask != nil && v == mgr.initTask.Task {
				return mgr.version, nil
			}
		}
		return taskstore.TaskVersionMissing, fmt.Errorf("%q task state updates are not allowed: %w", mgr.state, a2a.ErrInvalidAgentResponse)
	}

	if v, ok := event.(*a2a.Task); ok {
		if err := mgr.validate(v); err != nil {
			return taskstore.TaskVersionMissing, err
		}
		if mgr.tracked == nil {
			return mgr.create(ctx, v)
		}
		return mgr.apply(ctx, event)
	}

	switch v := event.(type) {
	case *a2a.TaskArtifactUpdateEvent:
		if err := mgr.validate(v); err != nil {
			return taskstore.TaskVersionMissing, err
		}
		if len(v.Artifact.Parts) == 0 {
			return taskstore.TaskVersionMissing, fmt.Errorf("artifact cannot be empty: %w", a2a.ErrInvalidAgentResponse)
		}
		return mgr.apply(ctx, v)

	case *a2a.TaskStatusUpdateEvent:
		if err := mgr.validate(v); err != nil {
			return taskstore.TaskVersionMissing, err
		}
		return mgr.updateStatus(ctx, v)

	default:
		return taskstore.TaskVersionMissing, fmt.Errorf("unexpected event type %T", v)
	}
}

func (mgr *Manager) updateStatus(ctx context.Context, event *a2a.TaskStatusUpdateEvent) (taskstore.TaskVersion, error) {
	for range maxCancelationAttempts {
		version, err := mgr.apply(ctx, event)
		if err == nil {
			return version, nil
		}

		if !errors.Is(err, taskstore.ErrConcurrentModification) || event.Status.State != a2a.TaskStateCanceled {
			return taskstore.TaskVersionMissing, err
		}

		storedTask, getErr := mgr.store.Get(ctx, event.TaskID)
		if getErr != nil {
			return taskstore.TaskVersionMissing, fmt.Errorf("failed to get task: %w", getErr)
		}

		if storedTask.Task.Status.State.Terminal() && storedTask.Task.Status.State != a2a.TaskStateCanceled {
			return taskstore.TaskVersionMissing, fmt.Errorf("task moved to %q before it could be cancelled: %w", storedTask.Task.Status.State, taskstore.ErrConcurrentModification)
		}

		mgr.track(storedTask.Task, storedTask.Version, storedTask.Task.Status.State)

		if storedTask.Task.Status.State == a2a.TaskStateCanceled {
			return mgr.version, nil
		}
	}

	return taskstore.TaskVersionMissing, fmt.Errorf("max task cancelation attempts reached")
}

func (mgr *Manager) create(ctx context.Context, task *a2a.Task) (taskstore.TaskVersion, error) {
	created, err := utils.DeepCopy(task)
	if err != nil {
		return taskstore.TaskVersionMissing, fmt.Errorf("failed to copy task: %w", err)
	}
	version, err := mgr.store.Create(ctx, created)
	if err != nil {
		return taskstore.TaskVersionMissing, fmt.Errorf("failed to create task: %w", err)
	}
	mgr.track(created, version, created.Status.State)
	return version, nil
}

func (mgr *Manager) apply(ctx context.Context, event a2a.Event) (taskstore.TaskVersion, error) {
	if mgr.tracked == nil {
		return taskstore.TaskVersionMissing, fmt.Errorf("first event must be a Task or a message: %w", a2a.ErrInvalidAgentResponse)
	}

	prevTask := mgr.tracked
	task, err := mgr.materializer.ApplyUpdate(ctx, prevTask, event)
	if err != nil {
		return taskstore.TaskVersionMissing, fmt.Errorf("apply event: %w", err)
	}
	if task == nil {
		return taskstore.TaskVersionMissing, fmt.Errorf("bug: task materializer returned nil task for %T", event)
	}

	version, err := mgr.store.Update(ctx, &taskstore.UpdateRequest{
		Task:        task,
		Event:       event,
		PrevTask:    prevTask,
		PrevVersion: mgr.version,
	})
	if err != nil {
		return taskstore.TaskVersionMissing, fmt.Errorf("failed to save task state: %w", err)
	}

	state := mgr.state
	switch v := event.(type) {
	case *a2a.Task:
		state = v.Status.State
	case *a2a.TaskStatusUpdateEvent:
		state = v.Status.State
	}
	mgr.track(task, version, state)

	return version, nil
}

func (mgr *Manager) track(t *a2a.Task, v taskstore.TaskVersion, state a2a.TaskState) {
	mgr.tracked = t
	mgr.version = v
	mgr.state = state
}

func (mgr *Manager) validate(provider a2a.TaskInfoProvider) error {
	info := provider.TaskInfo()
	if mgr.taskInfo.TaskID != info.TaskID {
		return fmt.Errorf("task IDs don't match: %s != %s", info.TaskID, mgr.taskInfo.TaskID)
	}
	if mgr.taskInfo.ContextID != info.ContextID {
		return fmt.Errorf("context IDs don't match: %s != %s", info.ContextID, mgr.taskInfo.ContextID)
	}
	return nil
}
