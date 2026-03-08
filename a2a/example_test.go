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

package a2a_test

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/a2aproject/a2a-go/v1/a2a"
)

func ExampleNewMessage() {
	msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("Hello, agent!"))

	fmt.Println("Role:", msg.Role)
	fmt.Println("Parts count:", len(msg.Parts))
	fmt.Println("Has ID:", msg.ID != "")
	// Output:
	// Role: user
	// Parts count: 1
	// Has ID: true
}

func ExampleNewMessageForTask() {
	taskInfo := a2a.TaskInfo{
		TaskID:    "task-abc",
		ContextID: "ctx-123",
	}

	msg := a2a.NewMessageForTask(a2a.MessageRoleAgent, taskInfo, a2a.NewTextPart("Working on it..."))

	fmt.Println("Role:", msg.Role)
	fmt.Println("TaskID:", msg.TaskID)
	fmt.Println("ContextID:", msg.ContextID)
	// Output:
	// Role: agent
	// TaskID: task-abc
	// ContextID: ctx-123
}

func ExampleNewSubmittedTask() {
	initialMsg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("Translate this document"))

	task := a2a.NewSubmittedTask(initialMsg, initialMsg)

	fmt.Println("State:", task.Status.State)
	fmt.Println("Has TaskID:", task.ID != "")
	fmt.Println("Has ContextID:", task.ContextID != "")
	fmt.Println("History length:", len(task.History))
	// Output:
	// State: SUBMITTED
	// Has TaskID: true
	// Has ContextID: true
	// History length: 1
}

func ExampleTaskState_Terminal() {
	states := []a2a.TaskState{
		a2a.TaskStateSubmitted,
		a2a.TaskStateWorking,
		a2a.TaskStateCompleted,
		a2a.TaskStateCanceled,
		a2a.TaskStateFailed,
		a2a.TaskStateInputRequired,
		a2a.TaskStateRejected,
	}

	for _, s := range states {
		fmt.Printf("%-16s terminal=%v\n", s, s.Terminal())
	}
	// Output:
	// SUBMITTED        terminal=false
	// WORKING          terminal=false
	// COMPLETED        terminal=true
	// CANCELED         terminal=true
	// FAILED           terminal=true
	// INPUT_REQUIRED   terminal=false
	// REJECTED         terminal=true
}

func ExampleStreamResponse_UnmarshalJSON() {
	jsonData := []byte(`{"statusUpdate":{"taskId":"task-1","contextId":"ctx-1","status":{"state":"WORKING"}}}`)

	var sr a2a.StreamResponse
	if err := json.Unmarshal(jsonData, &sr); err != nil {
		fmt.Println("Error:", err)
		return
	}

	switch ev := sr.Event.(type) {
	case *a2a.TaskStatusUpdateEvent:
		fmt.Println("Event type: TaskStatusUpdateEvent")
		fmt.Println("Task ID:", ev.TaskID)
		fmt.Println("State:", ev.Status.State)
	default:
		fmt.Printf("Unexpected type: %T\n", ev)
	}
	// Output:
	// Event type: TaskStatusUpdateEvent
	// Task ID: task-1
	// State: WORKING
}

func ExampleStreamResponse_UnmarshalJSON_message() {
	jsonData := []byte(`{"message":{"messageId":"msg-42","role":"user","parts":[{"kind":"text","text":"hello"}]}}`)

	var sr a2a.StreamResponse
	if err := json.Unmarshal(jsonData, &sr); err != nil {
		fmt.Println("Error:", err)
		return
	}

	msg := sr.Event.(*a2a.Message)
	fmt.Println("ID:", msg.ID)
	fmt.Println("Role:", msg.Role)
	fmt.Println("Text:", msg.Parts[0].Text())
	// Output:
	// ID: msg-42
	// Role: user
	// Text: hello
}

func ExampleNewError() {
	err := a2a.NewError(a2a.ErrTaskNotFound, "task xyz was not found")

	fmt.Println("Message:", err.Error())
	fmt.Println("Is ErrTaskNotFound:", errors.Is(err, a2a.ErrTaskNotFound))
	// Output:
	// Message: task xyz was not found
	// Is ErrTaskNotFound: true
}

func ExampleError_WithDetails() {
	err := a2a.NewError(a2a.ErrInvalidParams, "missing required field").
		WithDetails(map[string]any{
			"field":  "taskId",
			"reason": "must not be empty",
		})

	fmt.Println("Message:", err.Error())
	fmt.Println("Field:", err.Details["field"])
	fmt.Println("Reason:", err.Details["reason"])
	// Output:
	// Message: missing required field
	// Field: taskId
	// Reason: must not be empty
}

func ExampleNewStatusUpdateEvent() {
	taskInfo := a2a.TaskInfo{TaskID: "task-1", ContextID: "ctx-1"}

	event := a2a.NewStatusUpdateEvent(taskInfo, a2a.TaskStateWorking, nil)

	fmt.Println("Task ID:", event.TaskID)
	fmt.Println("State:", event.Status.State)
	fmt.Println("Has timestamp:", event.Status.Timestamp != nil)
	// Output:
	// Task ID: task-1
	// State: WORKING
	// Has timestamp: true
}

func ExampleNewArtifactEvent() {
	taskInfo := a2a.TaskInfo{TaskID: "task-1", ContextID: "ctx-1"}

	event := a2a.NewArtifactEvent(taskInfo, a2a.NewTextPart("Generated content"))

	fmt.Println("Task ID:", event.TaskID)
	fmt.Println("Has artifact ID:", event.Artifact.ID != "")
	fmt.Println("Text:", event.Artifact.Parts[0].Text())
	// Output:
	// Task ID: task-1
	// Has artifact ID: true
	// Text: Generated content
}

func ExampleStreamResponse_MarshalJSON_message() {
	msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("Hello"))
	msg.ID = "msg-1"

	data, err := json.Marshal(a2a.StreamResponse{Event: msg})
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	var raw map[string]any
	if err = json.Unmarshal(data, &raw); err != nil {
		fmt.Println("Error:", err)
		return
	}
	fmt.Println("Has message key:", raw["message"] != nil)
	inner := raw["message"].(map[string]any)
	fmt.Println("role:", inner["role"])
	// Output:
	// Has message key: true
	// role: user
}

func ExampleStreamResponse_MarshalJSON_task() {
	task := &a2a.Task{
		ID:        "task-1",
		ContextID: "ctx-1",
		Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
	}

	data, err := json.Marshal(a2a.StreamResponse{Event: task})
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	var raw map[string]any
	if err = json.Unmarshal(data, &raw); err != nil {
		fmt.Println("Error:", err)
		return
	}
	fmt.Println("Has task key:", raw["task"] != nil)
	inner := raw["task"].(map[string]any)
	fmt.Println("id:", inner["id"])
	// Output:
	// Has task key: true
	// id: task-1
}
