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

	"github.com/a2aproject/a2a-go/a2a"
)

type Saver interface {
	Save(ctx context.Context, task *a2a.Task) error
}

type Manager struct {
	Task  *a2a.Task
	saver Saver
}

func NewManager(saver Saver, task *a2a.Task) *Manager {
	return &Manager{Task: task, saver: saver}
}

func (ep *Manager) Process(ctx context.Context, event a2a.Event) error {
	if _, ok := event.(*a2a.Message); ok {
		return nil
	}

	if ep.Task == nil {
		return fmt.Errorf("event processor Task not set")
	}

	switch v := event.(type) {
	case *a2a.Task:
		if err := ep.validate(v.ID, v.ContextID); err != nil {
			return err
		}
		if err := ep.saver.Save(ctx, v); err != nil {
			return err
		}
		ep.Task = v
		return nil

	case *a2a.TaskArtifactUpdateEvent:
		if err := ep.validate(v.TaskID, v.ContextID); err != nil {
			return err
		}
		return ep.updateArtifact(ctx, v)

	case *a2a.TaskStatusUpdateEvent:
		if err := ep.validate(v.TaskID, v.ContextID); err != nil {
			return err
		}
		return ep.updateStatus(ctx, v)

	default:
		return fmt.Errorf("unexpected event type")
	}
}

func (ep *Manager) updateArtifact(_ context.Context, _ *a2a.TaskArtifactUpdateEvent) error {
	return fmt.Errorf("not implemented")
}

func (ep *Manager) updateStatus(ctx context.Context, event *a2a.TaskStatusUpdateEvent) error {
	task := ep.Task

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

	return ep.saver.Save(ctx, task)
}

func (ep *Manager) validate(taskID a2a.TaskID, contextID string) error {
	if ep.Task.ID != taskID {
		return fmt.Errorf("task IDs don't match: %s != %s", ep.Task.ID, taskID)
	}

	if ep.Task.ContextID != contextID {
		return fmt.Errorf("context IDs don't match: %s != %s", ep.Task.ContextID, contextID)
	}

	return nil
}
