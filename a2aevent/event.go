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

// Package a2aevent contains utilities for working with [a2a.Event] types.
package a2aevent

import (
	"fmt"
	"maps"
	"slices"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/internal/utils"
)

// ApplyUpdate returns a new [a2a.Task] produced by applying the event to the provided task.
// The input task is never modified.
func ApplyUpdate(task *a2a.Task, event a2a.Event) (*a2a.Task, error) {
	if _, ok := event.(*a2a.Message); ok {
		return nil, fmt.Errorf("message cannot be applied to the task state")
	}

	if task.Status.State.Terminal() {
		return nil, fmt.Errorf("%q task state updates are not allowed", task.Status.State)
	}

	if err := validateSameTask(task, event); err != nil {
		return nil, err
	}

	if v, ok := event.(*a2a.Task); ok {
		return utils.DeepCopy(v)
	}

	switch v := event.(type) {
	case *a2a.TaskArtifactUpdateEvent:
		return applyArtifactUpdate(task, v)
	case *a2a.TaskStatusUpdateEvent:
		return applyStatusUpdate(task, v)
	default:
		return nil, fmt.Errorf("unexpected event type %T", v)
	}
}

// ApplyArtifactUpdate returns a new [a2a.Task] with the event's artifact applied to the provided task.
// The input task is never modified.
func ApplyArtifactUpdate(base *a2a.Task, event *a2a.TaskArtifactUpdateEvent) (*a2a.Task, error) {
	if err := validateSameTask(base, event); err != nil {
		return nil, err
	}
	return applyArtifactUpdate(base, event)
}

// ApplyStatusUpdate returns a new [a2a.Task] with the event's status applied to a copy of base.
// The input task is never modified.
func ApplyStatusUpdate(base *a2a.Task, event *a2a.TaskStatusUpdateEvent) (*a2a.Task, error) {
	if err := validateSameTask(base, event); err != nil {
		return nil, err
	}
	return applyStatusUpdate(base, event)
}

func applyArtifactUpdate(base *a2a.Task, event *a2a.TaskArtifactUpdateEvent) (*a2a.Task, error) {
	if len(event.Artifact.Parts) == 0 {
		return nil, fmt.Errorf("artifact cannot be empty")
	}

	task, err := utils.DeepCopy(base)
	if err != nil {
		return nil, err
	}

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
		return task, nil
	}

	if !event.Append {
		task.Artifacts[updateIdx] = artifact
		return task, nil
	}

	toUpdate := task.Artifacts[updateIdx]
	toUpdate.Parts = append(toUpdate.Parts, artifact.Parts...)
	if toUpdate.Metadata == nil && artifact.Metadata != nil {
		toUpdate.Metadata = make(map[string]any, len(artifact.Metadata))
	}
	maps.Copy(toUpdate.Metadata, artifact.Metadata)
	return task, nil
}

func applyStatusUpdate(base *a2a.Task, event *a2a.TaskStatusUpdateEvent) (*a2a.Task, error) {
	task, err := utils.DeepCopy(base)
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
	return task, nil
}

func validateSameTask(tip1 a2a.TaskInfoProvider, tip2 a2a.TaskInfoProvider) error {
	ti1, ti2 := tip1.TaskInfo(), tip2.TaskInfo()
	if ti1.TaskID != ti2.TaskID {
		return fmt.Errorf("task IDs don't match: %s != %s", ti1.TaskID, ti2.TaskID)
	}
	if ti1.ContextID != ti2.ContextID {
		return fmt.Errorf("context IDs don't match: %s != %s", ti1.ContextID, ti2.ContextID)
	}
	return nil
}
