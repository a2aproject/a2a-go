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

package pbjson

// This file unmarshals JSON bodies into the wire structs declared in
// a2av0.pb.go and translates them back to v1 Go SDK types (a2a.*).  See
// the package doc for wire format details, and to_proto.go for the inverse
// direction (including notes on the enum-spelling and double-base64
// deviations that also apply here).

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

// ---- response / event unmarshalling -------------------------------------

// FromProtoSendMessageResponse parses the on-message-send response body,
// which is proto-JSON of google.a2a.v1.SendMessageResponse (a oneof of
// {"message": Message} or {"task": Task}).
func FromProtoSendMessageResponse(body []byte) (a2a.SendMessageResult, error) {
	var wire SendMessageResponse
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("failed to decode SendMessageResponse: %w", err)
	}
	if wire.Message != nil {
		return messageFromWire(wire.Message), nil
	}
	if wire.Task != nil {
		return taskFromWire(wire.Task)
	}
	return nil, fmt.Errorf("SendMessageResponse has neither 'message' nor 'task' payload")
}

// FromProtoStreamEvent parses one SSE event body of a v0.3 REST stream.
// It handles StreamResponse oneof of {message, task, statusUpdate, artifactUpdate}.
func FromProtoStreamEvent(body []byte) (a2a.Event, error) {
	var wire StreamResponse
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("failed to decode StreamResponse: %w", err)
	}
	if wire.Message != nil {
		return messageFromWire(wire.Message), nil
	}
	if wire.Task != nil {
		return taskFromWire(wire.Task)
	}
	if wire.StatusUpdate != nil {
		return statusUpdateFromWire(wire.StatusUpdate), nil
	}
	if wire.ArtifactUpdate != nil {
		return artifactUpdateFromWire(wire.ArtifactUpdate)
	}
	return nil, fmt.Errorf("StreamResponse has no known payload key")
}

// FromProtoTask parses an unwrapped Task JSON body.
func FromProtoTask(body []byte) (*a2a.Task, error) {
	var wire Task
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("failed to decode Task: %w", err)
	}
	return taskFromWire(&wire)
}

// FromProtoPushConfigResponse parses an unwrapped
// TaskPushNotificationConfig JSON body.
func FromProtoPushConfigResponse(body []byte) (*a2a.PushConfig, error) {
	var wire TaskPushNotificationConfig
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("failed to decode TaskPushNotificationConfig: %w", err)
	}
	return taskPushNotificationConfigFromWire(&wire), nil
}

// FromProtoListTasksResponse parses the response of GET /v1/tasks. The
// spec is silent on the exact shape; we emit and accept the AIP-158 style
// wrapper {"tasks": [...], "nextPageToken": ...}, and also accept a bare
// array for peers that follow the spec's implied shape from
// pushNotificationConfig/list.
func FromProtoListTasksResponse(body []byte) (*a2a.ListTasksResponse, error) {
	trimmed := bytes.TrimLeft(body, " \t\r\n")
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var arr []*Task
		if err := json.Unmarshal(body, &arr); err != nil {
			return nil, fmt.Errorf("failed to decode ListTasks response array: %w", err)
		}
		return listTasksFromWire(arr, "")
	}
	var wire ListTasksResponse
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("failed to decode ListTasks response: %w", err)
	}
	return listTasksFromWire(wire.Tasks, wire.NextPageToken)
}

// FromProtoListPushConfigsResponse parses the response of
// GET /v1/tasks/{id}/pushNotificationConfigs.
func FromProtoListPushConfigsResponse(body []byte, taskID a2a.TaskID) ([]*a2a.PushConfig, error) {
	trimmed := bytes.TrimLeft(body, " \t\r\n")
	var wires []*TaskPushNotificationConfig
	if len(trimmed) > 0 && trimmed[0] == '[' {
		if err := json.Unmarshal(body, &wires); err != nil {
			return nil, fmt.Errorf("failed to decode list push configs array: %w", err)
		}
	} else {
		var wire ListPushNotificationConfigsResponse
		if err := json.Unmarshal(body, &wire); err != nil {
			return nil, fmt.Errorf("failed to decode list push configs response: %w", err)
		}
		wires = wire.Configs
	}
	out := make([]*a2a.PushConfig, 0, len(wires))
	for _, w := range wires {
		pc := taskPushNotificationConfigFromWire(w)
		if pc.TaskID == "" {
			pc.TaskID = taskID
		}
		out = append(out, pc)
	}
	return out, nil
}

// ---- server-side request unmarshalling ----------------------------------

// FromProtoSendMessageRequest parses a request body posted by a v0.3
// REST client to /v1/message:send or /v1/message:stream and returns a v1
// SendMessageRequest.
func FromProtoSendMessageRequest(body []byte) (*a2a.SendMessageRequest, error) {
	var wire SendMessageRequest
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("failed to decode SendMessageRequest: %w", err)
	}
	req := &a2a.SendMessageRequest{Metadata: wire.Metadata}
	if wire.Message != nil {
		req.Message = messageFromWire(wire.Message)
	}
	if wire.Configuration != nil {
		req.Config = sendMessageConfigFromWire(wire.Configuration)
	}
	return req, nil
}

// FromProtoCreatePushConfigRequest parses a request body posted by a
// v0.3 REST client to /v1/tasks/{id}/pushNotificationConfigs. The body is
// a CreateTaskPushNotificationConfigRequest with {parent, configId, config}.
// The returned PushConfig has taskID set from the path parameter.
func FromProtoCreatePushConfigRequest(body []byte, taskID a2a.TaskID) (*a2a.PushConfig, error) {
	var wire CreateTaskPushNotificationConfigRequest
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("failed to decode CreateTaskPushNotificationConfigRequest: %w", err)
	}
	if wire.Config == nil {
		return nil, fmt.Errorf("CreateTaskPushNotificationConfigRequest missing 'config'")
	}
	pc := taskPushNotificationConfigFromWire(wire.Config)
	pc.TaskID = taskID
	if pc.ID == "" && wire.ConfigID != "" {
		pc.ID = wire.ConfigID
	}
	return pc, nil
}

// ---- core decoders (wire types -> a2a.*) --------------------------------

func messageFromWire(wire *Message) *a2a.Message {
	if wire == nil {
		return nil
	}
	msg := &a2a.Message{
		ID:         wire.MessageID,
		ContextID:  wire.ContextID,
		TaskID:     a2a.TaskID(wire.TaskID),
		Role:       roleFromWire(wire.Role),
		Metadata:   wire.Metadata,
		Extensions: wire.Extensions,
	}
	if len(wire.Content) > 0 {
		msg.Parts = make(a2a.ContentParts, 0, len(wire.Content))
		for _, p := range wire.Content {
			part, err := partFromWire(p)
			if err != nil {
				// Should not happen: we constructed these types locally. If it
				// does, fall back to an empty part rather than losing the whole
				// message.
				part = &a2a.Part{}
			}
			msg.Parts = append(msg.Parts, part)
		}
	}
	return msg
}

func partFromWire(wire *Part) (*a2a.Part, error) {
	if wire == nil {
		return nil, fmt.Errorf("part is nil")
	}
	part := &a2a.Part{Metadata: wire.Metadata}
	switch {
	case wire.Text != nil:
		part.Content = a2a.Text(*wire.Text)
	case wire.File != nil:
		part.MediaType = wire.File.MimeType
		part.Filename = wire.File.Name
		switch {
		case wire.File.FileWithURI != "":
			part.Content = a2a.URL(wire.File.FileWithURI)
		case wire.File.FileWithBytes != "":
			// Reverse the double base64 encoding (see partToWire for a2a.Raw).
			outer, err := base64.StdEncoding.DecodeString(wire.File.FileWithBytes)
			if err != nil {
				return nil, fmt.Errorf("failed to decode fileWithBytes (outer): %w", err)
			}
			inner, err := base64.StdEncoding.DecodeString(string(outer))
			if err != nil {
				return nil, fmt.Errorf("failed to decode fileWithBytes (inner): %w", err)
			}
			part.Content = a2a.Raw(inner)
		}
	case wire.Data != nil:
		part.Content = a2a.Data{Value: wire.Data.Data}
	default:
		return nil, fmt.Errorf("part has no known oneof key")
	}
	return part, nil
}

func taskFromWire(wire *Task) (*a2a.Task, error) {
	if wire == nil {
		return nil, nil
	}
	task := &a2a.Task{
		ID:        a2a.TaskID(wire.ID),
		ContextID: wire.ContextID,
		Metadata:  wire.Metadata,
	}
	if wire.Status != nil {
		task.Status = taskStatusFromWire(wire.Status)
	}
	for _, a := range wire.Artifacts {
		task.Artifacts = append(task.Artifacts, artifactFromWire(a))
	}
	for _, m := range wire.History {
		task.History = append(task.History, messageFromWire(m))
	}
	return task, nil
}

func taskStatusFromWire(wire *TaskStatus) a2a.TaskStatus {
	if wire == nil {
		return a2a.TaskStatus{}
	}
	status := a2a.TaskStatus{State: DecodeTaskState(wire.State)}
	if wire.Message != nil {
		status.Message = messageFromWire(wire.Message)
	}
	if wire.Timestamp != nil {
		ts := *wire.Timestamp
		status.Timestamp = &ts
	}
	return status
}

func artifactFromWire(wire *Artifact) *a2a.Artifact {
	if wire == nil {
		return nil
	}
	art := &a2a.Artifact{
		ID:          a2a.ArtifactID(wire.ArtifactID),
		Name:        wire.Name,
		Description: wire.Description,
		Metadata:    wire.Metadata,
		Extensions:  wire.Extensions,
	}
	for _, p := range wire.Parts {
		part, err := partFromWire(p)
		if err != nil {
			part = &a2a.Part{}
		}
		art.Parts = append(art.Parts, part)
	}
	return art
}

func statusUpdateFromWire(wire *TaskStatusUpdateEvent) *a2a.TaskStatusUpdateEvent {
	// The wire "final" flag is discarded because the v2 SDK infers finality
	// from the terminal-state predicate on TaskState.
	if wire == nil {
		return nil
	}
	evt := &a2a.TaskStatusUpdateEvent{
		TaskID:    a2a.TaskID(wire.TaskID),
		ContextID: wire.ContextID,
		Metadata:  wire.Metadata,
	}
	if wire.Status != nil {
		evt.Status = taskStatusFromWire(wire.Status)
	}
	return evt
}

func artifactUpdateFromWire(wire *TaskArtifactUpdateEvent) (*a2a.TaskArtifactUpdateEvent, error) {
	if wire == nil {
		return nil, nil
	}
	evt := &a2a.TaskArtifactUpdateEvent{
		TaskID:    a2a.TaskID(wire.TaskID),
		ContextID: wire.ContextID,
		Append:    wire.Append,
		LastChunk: wire.LastChunk,
		Metadata:  wire.Metadata,
	}
	if wire.Artifact != nil {
		evt.Artifact = artifactFromWire(wire.Artifact)
	}
	return evt, nil
}

func sendMessageConfigFromWire(wire *SendMessageConfiguration) *a2a.SendMessageConfig {
	if wire == nil {
		return nil
	}
	cfg := &a2a.SendMessageConfig{
		AcceptedOutputModes: wire.AcceptedOutputModes,
		ReturnImmediately:   !wire.Blocking,
	}
	if wire.HistoryLength != 0 {
		hl := int(wire.HistoryLength)
		cfg.HistoryLength = &hl
	}
	if wire.PushNotification != nil {
		cfg.PushConfig = pushNotificationConfigFromWire(wire.PushNotification)
	}
	return cfg
}

func pushNotificationConfigFromWire(wire *PushNotificationConfig) *a2a.PushConfig {
	if wire == nil {
		return &a2a.PushConfig{}
	}
	pc := &a2a.PushConfig{ID: wire.ID, URL: wire.URL, Token: wire.Token}
	if wire.Authentication != nil {
		auth := &a2a.PushAuthInfo{Credentials: wire.Authentication.Credentials}
		if len(wire.Authentication.Schemes) > 0 {
			// v2 SDK PushAuthInfo carries a single scheme; use the first.
			auth.Scheme = wire.Authentication.Schemes[0]
		}
		pc.Auth = auth
	}
	return pc
}

func taskPushNotificationConfigFromWire(wire *TaskPushNotificationConfig) *a2a.PushConfig {
	if wire == nil {
		return &a2a.PushConfig{}
	}
	pc := pushNotificationConfigFromWire(wire.PushNotificationConfig)
	taskID, _ := parsePushConfigResourceName(wire.Name)
	pc.TaskID = taskID
	return pc
}

func listTasksFromWire(wires []*Task, nextPageToken string) (*a2a.ListTasksResponse, error) {
	tasks := make([]*a2a.Task, 0, len(wires))
	for _, w := range wires {
		t, err := taskFromWire(w)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return &a2a.ListTasksResponse{
		Tasks:         tasks,
		NextPageToken: nextPageToken,
	}, nil
}

// ---- misc decoding helpers ----------------------------------------------

func roleFromWire(r Role) a2a.MessageRole {
	switch r {
	case RoleUser:
		return a2a.MessageRoleUser
	case RoleAgent:
		return a2a.MessageRoleAgent
	default:
		return a2a.MessageRoleUnspecified
	}
}

// DecodeTaskState translates the wire enum name into the v2 SDK enum.
// The single spelling difference is TASK_STATE_CANCELED (v2) vs
// TASK_STATE_CANCELLED (wire); see EncodeTaskState for the inverse.
func DecodeTaskState(s TaskState) a2a.TaskState {
	switch s {
	case "", TaskStateUnspecified:
		return a2a.TaskStateUnspecified
	case TaskStateCancelled:
		return a2a.TaskStateCanceled
	default:
		return a2a.TaskState(s)
	}
}

// parsePushConfigResourceName extracts (taskID, configID) from a resource
// name of the form "tasks/{taskId}/pushNotificationConfigs/{configId}".
func parsePushConfigResourceName(name string) (a2a.TaskID, string) {
	segments := strings.Split(name, "/")
	if len(segments) != 4 || segments[0] != "tasks" || segments[2] != "pushNotificationConfigs" {
		return "", ""
	}
	return a2a.TaskID(segments[1]), segments[3]
}
