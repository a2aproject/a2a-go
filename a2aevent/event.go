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
// The input task is never modified, the output task does not share any mutable structs with inputs.
func ApplyUpdate(task *a2a.Task, event a2a.Event) (*a2a.Task, error) {
	if err := validateUpdate(task, event); err != nil {
		return nil, err
	}
	if v, ok := event.(*a2a.Task); ok {
		copy, err := utils.DeepCopy(v)
		if err != nil {
			return nil, err
		}
		return copy, nil
	}

	taskCopy, err := utils.DeepCopy(task)
	if err != nil {
		return nil, err
	}

	switch v := event.(type) {
	case *a2a.TaskArtifactUpdateEvent:
		eventCopy, err := utils.DeepCopy(v)
		if err != nil {
			return nil, err
		}
		return applyShallowArtifactUpdate(taskCopy, eventCopy)

	case *a2a.TaskStatusUpdateEvent:
		eventCopy, err := utils.DeepCopy(v)
		if err != nil {
			return nil, err
		}
		return applyShallowStatusUpdate(taskCopy, eventCopy), nil

	default:
		return nil, fmt.Errorf("unexpected event type %T", v)
	}
}

// ApplyShallowUpdate returns a new [a2a.Task] produced by applying the event to the provided task.
// The input task is never modified. The output task shares mutable structs with inputs.
func ApplyShallowUpdate(task *a2a.Task, event a2a.Event) (*a2a.Task, error) {
	if err := validateUpdate(task, event); err != nil {
		return nil, err
	}
	if v, ok := event.(*a2a.Task); ok {
		shallow := *v
		return &shallow, nil
	}
	switch v := event.(type) {
	case *a2a.TaskArtifactUpdateEvent:
		return applyShallowArtifactUpdate(task, v)
	case *a2a.TaskStatusUpdateEvent:
		return applyShallowStatusUpdate(task, v), nil
	default:
		return nil, fmt.Errorf("unexpected event type %T", v)
	}
}

// ApplyArtifactUpdate returns a new [a2a.Task] with the event's artifact applied to the provided task.
func ApplyArtifactUpdate(task *a2a.Task, event *a2a.TaskArtifactUpdateEvent) (*a2a.Task, error) {
	if err := validateUpdate(task, event); err != nil {
		return nil, err
	}
	taskCopy, err := utils.DeepCopy(task)
	if err != nil {
		return nil, err
	}
	eventCopy, err := utils.DeepCopy(event)
	if err != nil {
		return nil, err
	}
	return applyShallowArtifactUpdate(taskCopy, eventCopy)
}

// ApplyStatusUpdate returns a new [a2a.Task] with the event's status applied to a copy of base.
func ApplyStatusUpdate(task *a2a.Task, event *a2a.TaskStatusUpdateEvent) (*a2a.Task, error) {
	if err := validateUpdate(task, event); err != nil {
		return nil, err
	}
	taskCopy, err := utils.DeepCopy(task)
	if err != nil {
		return nil, err
	}
	eventCopy, err := utils.DeepCopy(event)
	if err != nil {
		return nil, err
	}
	return applyShallowStatusUpdate(taskCopy, eventCopy), nil
}

func applyShallowArtifactUpdate(src *a2a.Task, event *a2a.TaskArtifactUpdateEvent) (*a2a.Task, error) {
	if len(event.Artifact.Parts) == 0 {
		return nil, fmt.Errorf("artifact cannot be empty")
	}

	task := *src

	artifact := event.Artifact
	updateIdx := slices.IndexFunc(task.Artifacts, func(a *a2a.Artifact) bool {
		return a.ID == artifact.ID
	})
	if updateIdx < 0 && event.Append {
		return nil, fmt.Errorf("no artifact found for update")
	}
	task.Artifacts = slices.Clone(src.Artifacts)

	if updateIdx < 0 {
		task.Artifacts = append(task.Artifacts, artifact)
		return &task, nil
	}

	if !event.Append {
		task.Artifacts[updateIdx] = artifact
		return &task, nil
	}

	toUpdate := *task.Artifacts[updateIdx]
	toUpdate.Parts = slices.Clone(toUpdate.Parts)
	toUpdate.Parts = append(toUpdate.Parts, artifact.Parts...)
	task.Artifacts[updateIdx] = &toUpdate

	if artifact.Metadata != nil {
		if toUpdate.Metadata == nil {
			toUpdate.Metadata = make(map[string]any, len(artifact.Metadata))
		} else {
			toUpdate.Metadata = maps.Clone(toUpdate.Metadata)
		}
		maps.Copy(toUpdate.Metadata, artifact.Metadata)
	}
	return &task, nil
}

func applyShallowStatusUpdate(src *a2a.Task, event *a2a.TaskStatusUpdateEvent) *a2a.Task {
	task := *src
	if task.Status.Message != nil {
		task.History = slices.Clone(src.History)
		task.History = append(task.History, task.Status.Message)
	}
	if event.Metadata != nil {
		if task.Metadata == nil {
			task.Metadata = make(map[string]any, len(event.Metadata))
		} else {
			task.Metadata = maps.Clone(src.Metadata)
		}
		maps.Copy(task.Metadata, event.Metadata)
	}
	task.Status = event.Status
	return &task
}

func validateUpdate(task *a2a.Task, event a2a.Event) error {
	if _, ok := event.(*a2a.Message); ok {
		return fmt.Errorf("message cannot be applied to the task state")
	}
	if task.Status.State.Terminal() {
		return fmt.Errorf("%q task state updates are not allowed", task.Status.State)
	}
	ti1, ti2 := task.TaskInfo(), event.TaskInfo()
	if ti1.TaskID != ti2.TaskID {
		return fmt.Errorf("task IDs don't match: %s != %s", ti1.TaskID, ti2.TaskID)
	}
	if ti1.ContextID != ti2.ContextID {
		return fmt.Errorf("context IDs don't match: %s != %s", ti1.ContextID, ti2.ContextID)
	}
	return nil
}

// IsFinal returns true for events which end the agent execution.
func IsFinal(event a2a.Event) bool {
	if _, ok := event.(*a2a.Message); ok {
		return true
	}

	var state a2a.TaskState
	switch v := event.(type) {
	case *a2a.TaskStatusUpdateEvent:
		state = v.Status.State
	case *a2a.Task:
		state = v.Status.State
	default:
		return false
	}

	return state.Terminal() || state == a2a.TaskStateInputRequired
}
