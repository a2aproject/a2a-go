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
	"fmt"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

// GetTextParts extracts text from all text parts in an event.
//
// Supported events:
//   - *a2a.Message — extracts text from message parts
//   - *a2a.TaskStatusUpdateEvent — extracts text from status message parts
//
// Returns nil if the event type is not supported.
func GetTextParts(event a2a.Event) []string {
	switch e := event.(type) {
	case *a2a.Message:
		return textPartsFromContentParts(e.Parts)
	case *a2a.TaskStatusUpdateEvent:
		if e.Status.Message != nil {
			return textPartsFromContentParts(e.Status.Message.Parts)
		}
	}
	return nil
}

// MustToTask asserts that the given event is a [*a2a.Task] and returns it.
// It calls t.Fatal if the type assertion fails.
func MustToTask(t *testing.T, event a2a.Event) *a2a.Task {
	t.Helper()
	task, ok := event.(*a2a.Task)
	if !ok {
		t.Fatalf("expected *a2a.Task, got %T", event)
	}
	return task
}

// MustToMessage asserts that the given event is a [*a2a.Message] and returns it.
// It calls t.Fatal if the type assertion fails.
func MustToMessage(t *testing.T, event a2a.Event) *a2a.Message {
	t.Helper()
	msg, ok := event.(*a2a.Message)
	if !ok {
		t.Fatalf("expected *a2a.Message, got %T", event)
	}
	return msg
}

// MustToStatusUpdate asserts that the given event is a [*a2a.TaskStatusUpdateEvent]
// and returns it. It calls t.Fatal if the type assertion fails.
func MustToStatusUpdate(t *testing.T, event a2a.Event) *a2a.TaskStatusUpdateEvent {
	t.Helper()
	update, ok := event.(*a2a.TaskStatusUpdateEvent)
	if !ok {
		t.Fatalf("expected *a2a.TaskStatusUpdateEvent, got %T", event)
	}
	return update
}

// MustToArtifactUpdate asserts that the given event is a [*a2a.TaskArtifactUpdateEvent]
// and returns it. It calls t.Fatal if the type assertion fails.
func MustToArtifactUpdate(t *testing.T, event a2a.Event) *a2a.TaskArtifactUpdateEvent {
	t.Helper()
	update, ok := event.(*a2a.TaskArtifactUpdateEvent)
	if !ok {
		t.Fatalf("expected *a2a.TaskArtifactUpdateEvent, got %T", event)
	}
	return update
}

func textPartsFromContentParts(parts a2a.ContentParts) []string {
	if len(parts) == 0 {
		return nil
	}
	var texts []string
	for _, part := range parts {
		if text, ok := part.Content.(a2a.Text); ok {
			texts = append(texts, string(text))
		}
	}
	return texts
}

// FormatEvent returns a human-readable string representation of an event for
// use in error messages and test output.
func FormatEvent(event a2a.Event) string {
	if event == nil {
		return "<nil>"
	}
	switch e := event.(type) {
	case *a2a.Message:
		return fmt.Sprintf("Message(role=%s, parts=%d)", e.Role, len(e.Parts))
	case *a2a.Task:
		return fmt.Sprintf("Task(id=%s, state=%s)", e.ID, e.Status.State)
	case *a2a.TaskStatusUpdateEvent:
		return fmt.Sprintf("TaskStatusUpdate(task=%s, state=%s)", e.TaskID, e.Status.State)
	case *a2a.TaskArtifactUpdateEvent:
		return fmt.Sprintf("TaskArtifactUpdate(task=%s, artifact=%s)", e.TaskID, e.Artifact.ID)
	default:
		return fmt.Sprintf("%T", event)
	}
}
