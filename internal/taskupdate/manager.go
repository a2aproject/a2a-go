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
	"fmt"
	"maps"
	"slices"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv/taskstore"
	"github.com/a2aproject/a2a-go/internal/utils"
	"github.com/a2aproject/a2a-go/log"
)

// Manager is used for processing [a2a.Event] related to an [a2a.Task]. It updates
// the Task accordingly and uses [taskstore.Store] to store the new state.
type Manager struct {
	taskInfo  a2a.TaskInfo
	lastSaved *taskstore.StoredTask
	store     taskstore.Store
}

// NewManager is a [Manager] constructor function.
func NewManager(store taskstore.Store, info a2a.TaskInfo, task *taskstore.StoredTask) *Manager {
	return &Manager{
		taskInfo:  info,
		lastSaved: task,
		store:     store,
	}
}

// SetTaskFailed attempts to move the Task to failed state and returns it in case of a success.
func (mgr *Manager) SetTaskFailed(ctx context.Context, event a2a.Event, cause error) (*taskstore.StoredTask, error) {
	if mgr.lastSaved == nil {
		return nil, fmt.Errorf("execution failed before a task was created: %w", cause)
	}

	task := *mgr.lastSaved.Task // copy to update task status

	// do not store cause.Error() as part of status to not disclose the cause to clients
	task.Status = a2a.TaskStatus{State: a2a.TaskStateFailed}

	if _, err := mgr.saveTask(ctx, &task, event); err != nil {
		return nil, fmt.Errorf("failed to store failed task state: %w: %w", err, cause)
	}

	log.Info(ctx, "task moved to failed state", "cause", cause.Error())
	return mgr.lastSaved, nil
}

// Process validates the event associated with the managed [a2a.Task] and integrates the new state into it.
func (mgr *Manager) Process(ctx context.Context, event a2a.Event) (*taskstore.StoredTask, error) {
	if _, ok := event.(*a2a.Message); ok {
		if mgr.lastSaved != nil {
			return nil, fmt.Errorf("message not allowed after task was stored: %w", a2a.ErrInvalidAgentResponse)
		}
		return nil, nil
	}

	if v, ok := event.(*a2a.Task); ok {
		if err := mgr.validate(v); err != nil {
			return nil, err
		}
		return mgr.saveTask(ctx, v, event)
	}

	if mgr.lastSaved == nil {
		return nil, fmt.Errorf("first event must be a Task or a message: %w", a2a.ErrInvalidAgentResponse)
	}

	switch v := event.(type) {
	case *a2a.TaskArtifactUpdateEvent:
		if err := mgr.validate(v); err != nil {
			return nil, err
		}
		if len(v.Artifact.Parts) == 0 {
			return nil, fmt.Errorf("artifact cannot be empty: %w", a2a.ErrInvalidAgentResponse)
		}
		return mgr.updateArtifact(ctx, v)

	case *a2a.TaskStatusUpdateEvent:
		if err := mgr.validate(v); err != nil {
			return nil, err
		}
		return mgr.updateStatus(ctx, v)

	default:
		return nil, fmt.Errorf("unexpected event type %T", v)
	}
}

func (mgr *Manager) updateArtifact(ctx context.Context, event *a2a.TaskArtifactUpdateEvent) (*taskstore.StoredTask, error) {
	task := mgr.lastSaved.Task

	// The copy is required because the event will be passed to subscriber goroutines, while
	// the artifact might be modified in our goroutine by other TaskArtifactUpdateEvent-s.
	artifact, err := utils.DeepCopy(event.Artifact)
	if err != nil {
		return nil, fmt.Errorf("failed to copy artifact: %w", err)
	}

	updateIdx := slices.IndexFunc(task.Artifacts, func(a *a2a.Artifact) bool {
		return a.ID == artifact.ID
	})

	if updateIdx < 0 {
		if event.Append {
			return nil, fmt.Errorf("no artifact found for update")
		}
		task.Artifacts = append(task.Artifacts, artifact)
		return mgr.saveTask(ctx, task, event)
	}

	if !event.Append {
		task.Artifacts[updateIdx] = artifact
		return mgr.saveTask(ctx, task, event)
	}

	toUpdate := task.Artifacts[updateIdx]
	toUpdate.Parts = append(toUpdate.Parts, artifact.Parts...)
	if toUpdate.Metadata == nil && artifact.Metadata != nil {
		toUpdate.Metadata = make(map[string]any, len(artifact.Description))
	}
	for k, v := range artifact.Metadata {
		toUpdate.Metadata[k] = v
	}
	return mgr.saveTask(ctx, task, event)
}

func (mgr *Manager) updateStatus(ctx context.Context, event *a2a.TaskStatusUpdateEvent) (*taskstore.StoredTask, error) {
	task, err := utils.DeepCopy(mgr.lastSaved.Task)
	if err != nil {
		return nil, err
	}

	if task.Status.Message != nil {
		task.History = append(task.History, task.Status.Message)
	}

	if event.Metadata != nil {
		if task.Metadata == nil {
			task.Metadata = make(map[string]any)
		}
		maps.Copy(task.Metadata, event.Metadata)
	}

	task.Status = event.Status

	return mgr.saveTask(ctx, task, event)
}

func (mgr *Manager) saveTask(ctx context.Context, task *a2a.Task, event a2a.Event) (*taskstore.StoredTask, error) {
	var version taskstore.TaskVersion
	var err error
	if mgr.lastSaved == nil {
		version, err = mgr.store.Create(ctx, task)
	} else {
		version, err = mgr.store.Update(ctx, &taskstore.UpdateRequest{
			Task:        task,
			Event:       event,
			PrevVersion: mgr.lastSaved.Version,
		})
	}
	if err != nil {
		return nil, fmt.Errorf("failed to save task state: %w", err)
	}
	mgr.lastSaved = &taskstore.StoredTask{Task: task, Version: version}
	return mgr.lastSaved, nil
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
