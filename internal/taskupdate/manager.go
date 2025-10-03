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
	"slices"

	"github.com/a2aproject/a2a-go/a2a"
)

// Saver is used for saving the Task after updating its state.
type Saver interface {
	Save(ctx context.Context, task *a2a.Task) error
}

// Manager is used for processing a2a.Event related to a Task. It updates
// the Task accordingly and uses Saver to store the new state.
type Manager struct {
	Task  *a2a.Task
	saver Saver
}

// NewManager creates an initialized update Manager for the provided task.
func NewManager(saver Saver, task *a2a.Task) *Manager {
	return &Manager{Task: task, saver: saver}
}

// Process validates that the event is associated with the managed Task and updates the Task accordingly.
func (mgr *Manager) Process(ctx context.Context, event a2a.Event) error {
	if mgr.Task == nil {
		return fmt.Errorf("event processor Task not set")
	}

	switch v := event.(type) {
	case *a2a.Message:
		return nil

	case *a2a.Task:
		if err := mgr.validate(v.ID, v.ContextID); err != nil {
			return err
		}
		if err := mgr.saver.Save(ctx, v); err != nil {
			return err
		}
		mgr.Task = v
		return nil

	case *a2a.TaskArtifactUpdateEvent:
		if err := mgr.validate(v.TaskID, v.ContextID); err != nil {
			return err
		}
		return mgr.updateArtifact(ctx, v)

	case *a2a.TaskStatusUpdateEvent:
		if err := mgr.validate(v.TaskID, v.ContextID); err != nil {
			return err
		}
		return mgr.updateStatus(ctx, v)

	default:
		return fmt.Errorf("unexpected event type %T", v)
	}
}

func (mgr *Manager) updateArtifact(ctx context.Context, event *a2a.TaskArtifactUpdateEvent) error {
	task := mgr.Task

	updateIdx := slices.IndexFunc(task.Artifacts, func(a *a2a.Artifact) bool {
		return a.ID == event.Artifact.ID
	})

	if updateIdx < 0 {
		if event.Append {
			// TODO(yarolegovich): log "artifact for update not found" as Python does
			return nil
		}
		task.Artifacts = append(task.Artifacts, event.Artifact)
		return mgr.saver.Save(ctx, task)
	}

	if !event.Append {
		task.Artifacts[updateIdx] = event.Artifact
		return mgr.saver.Save(ctx, task)
	}

	toUpdate := task.Artifacts[updateIdx]
	toUpdate.Parts = append(toUpdate.Parts, event.Artifact.Parts...)
	if toUpdate.Metadata == nil && event.Artifact.Metadata != nil {
		toUpdate.Metadata = event.Artifact.Metadata
	} else {
		for k, v := range event.Artifact.Metadata {
			toUpdate.Metadata[k] = v
		}
	}
	return mgr.saver.Save(ctx, task)
}

func (mgr *Manager) updateStatus(ctx context.Context, event *a2a.TaskStatusUpdateEvent) error {
	task := mgr.Task

	if task.Status.Message != nil {
		task.History = append(task.History, task.Status.Message)
	}

	if event.Metadata != nil {
		if task.Metadata == nil {
			task.Metadata = make(map[string]any)
		}
		for k, v := range event.Metadata {
			task.Metadata[k] = v
		}
	}

	task.Status = event.Status

	return mgr.saver.Save(ctx, task)
}

func (mgr *Manager) validate(taskID a2a.TaskID, contextID string) error {
	if mgr.Task.ID != taskID {
		return fmt.Errorf("task IDs don't match: %s != %s", mgr.Task.ID, taskID)
	}

	if mgr.Task.ContextID != contextID {
		return fmt.Errorf("context IDs don't match: %s != %s", mgr.Task.ContextID, contextID)
	}

	return nil
}
