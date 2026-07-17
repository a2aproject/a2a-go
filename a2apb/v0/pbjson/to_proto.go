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

// This file translates v1 Go SDK types (a2a.*) into the wire structs declared
// in a2av0.pb.go and marshals them as JSON.  See the package doc for wire
// format details.
//
// Deviations from the v2 SDK that are handled here:
//   * MessageSendConfiguration.blocking is the inverse of ReturnImmediately.
//   * TaskState enum: v2 uses TASK_STATE_CANCELED, v0.3 uses TASK_STATE_CANCELLED.
//   * FilePart.fileWithBytes is double base64-encoded (see partToWire) to
//     match v0.3 peers' FilePart.bytes string field.
//   * PushConfig lookups use resource names of the form
//     "tasks/{taskId}/pushNotificationConfigs/{configId}".
//   * v2 SDK PushAuthInfo carries a single scheme; the wire type carries a
//     list. Encode as a one-element list; decode by taking the first entry.

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

// ---- request marshalling ------------------------------------------------

// ToProtoSendMessageRequest marshals a v1 SendMessageRequest as the JSON
// body posted to /v1/message:send or /v1/message:stream.
func ToProtoSendMessageRequest(req *a2a.SendMessageRequest) ([]byte, error) {
	if req == nil {
		return json.Marshal(SendMessageRequest{})
	}
	wire := SendMessageRequest{
		Message:  messageToWire(req.Message),
		Metadata: req.Metadata,
	}
	if req.Config != nil {
		wire.Configuration = sendMessageConfigToWire(req.Config)
	}
	return json.Marshal(wire)
}

// ToProtoCreatePushConfigRequest encodes a v1 PushConfig as the request body
// posted to POST /v1/tasks/{id}/pushNotificationConfigs. v0.3 servers parse
// this body as a CreateTaskPushNotificationConfigRequest proto with fields
// {parent, configId, config}.
func ToProtoCreatePushConfigRequest(pc *a2a.PushConfig) ([]byte, error) {
	if pc == nil {
		return json.Marshal(CreateTaskPushNotificationConfigRequest{})
	}
	return json.Marshal(CreateTaskPushNotificationConfigRequest{
		Parent:   "tasks/" + string(pc.TaskID),
		ConfigID: pc.ID,
		Config:   taskPushNotificationConfigToWire(pc),
	})
}

// ---- server-side response marshalling -----------------------------------

// ToProtoTask marshals a v1 Task as an unwrapped JSON body. Used as the
// response body of GET /v1/tasks/{id} and POST /v1/tasks/{id}:cancel.
func ToProtoTask(task *a2a.Task) ([]byte, error) {
	if task == nil {
		return json.Marshal(Task{})
	}
	return json.Marshal(taskToWire(task))
}

// ToProtoSendMessageResult encodes either a Task or a Message as the
// on-message-send response body, wrapped in a SendMessageResponse envelope
// (either {"message": Message} or {"task": Task}).
func ToProtoSendMessageResult(result a2a.SendMessageResult) ([]byte, error) {
	switch v := result.(type) {
	case *a2a.Task:
		return json.Marshal(SendMessageResponse{Task: taskToWire(v)})
	case *a2a.Message:
		return json.Marshal(SendMessageResponse{Message: messageToWire(v)})
	default:
		return nil, fmt.Errorf("unsupported SendMessageResult type: %T", result)
	}
}

// ToProtoStreamEvent encodes an Event as a StreamResponse envelope with
// exactly one of the oneof keys set.
func ToProtoStreamEvent(event a2a.Event) ([]byte, error) {
	switch v := event.(type) {
	case *a2a.Task:
		return json.Marshal(StreamResponse{Task: taskToWire(v)})
	case *a2a.Message:
		return json.Marshal(StreamResponse{Message: messageToWire(v)})
	case *a2a.TaskStatusUpdateEvent:
		return json.Marshal(StreamResponse{StatusUpdate: taskStatusUpdateEventToWire(v)})
	case *a2a.TaskArtifactUpdateEvent:
		return json.Marshal(StreamResponse{ArtifactUpdate: taskArtifactUpdateEventToWire(v)})
	default:
		return nil, fmt.Errorf("unsupported Event type: %T", event)
	}
}

// ToProtoPushConfigResponse marshals a v1 PushConfig as an unwrapped
// TaskPushNotificationConfig JSON body. Used for GET and POST push-config
// endpoints.
func ToProtoPushConfigResponse(pc *a2a.PushConfig) ([]byte, error) {
	if pc == nil {
		return json.Marshal(TaskPushNotificationConfig{})
	}
	return json.Marshal(taskPushNotificationConfigToWire(pc))
}

// ToProtoListTasksResponse serializes a ListTasksResponse as the AIP-158
// wrapper {"tasks": [...], "nextPageToken": "..."}.
// Clients that expect a bare array (per the spec's implied shape for
// pushNotificationConfig/list) are handled by the client-side decoder.
func ToProtoListTasksResponse(resp *a2a.ListTasksResponse) ([]byte, error) {
	if resp == nil {
		return json.Marshal(ListTasksResponse{Tasks: []*Task{}})
	}
	wire := ListTasksResponse{
		Tasks:         make([]*Task, 0, len(resp.Tasks)),
		NextPageToken: resp.NextPageToken,
	}
	for _, t := range resp.Tasks {
		wire.Tasks = append(wire.Tasks, taskToWire(t))
	}
	return json.Marshal(wire)
}

// ToProtoListPushConfigsResponse serializes a slice of PushConfig as the
// wrapped {"configs": [...]} shape.
func ToProtoListPushConfigsResponse(configs []*a2a.PushConfig) ([]byte, error) {
	wire := ListPushNotificationConfigsResponse{
		Configs: make([]*TaskPushNotificationConfig, 0, len(configs)),
	}
	for _, pc := range configs {
		wire.Configs = append(wire.Configs, taskPushNotificationConfigToWire(pc))
	}
	return json.Marshal(wire)
}

// ---- core encoders (a2a.* -> wire types) --------------------------------

func messageToWire(msg *a2a.Message) *Message {
	if msg == nil {
		return nil
	}
	wire := &Message{
		MessageID:  msg.ID,
		ContextID:  msg.ContextID,
		TaskID:     string(msg.TaskID),
		Role:       roleToWire(msg.Role),
		Metadata:   msg.Metadata,
		Extensions: msg.Extensions,
	}
	if len(msg.Parts) > 0 {
		wire.Content = make([]*Part, 0, len(msg.Parts))
		for _, p := range msg.Parts {
			wire.Content = append(wire.Content, partToWire(p))
		}
	}
	return wire
}

func partToWire(p *a2a.Part) *Part {
	if p == nil {
		return &Part{}
	}
	wire := &Part{Metadata: p.Metadata}
	switch c := p.Content.(type) {
	case a2a.Text:
		text := string(c)
		wire.Text = &text
	case a2a.Raw:
		inner := base64.StdEncoding.EncodeToString(c)
		wire.File = &FilePart{
			FileWithBytes: base64.StdEncoding.EncodeToString([]byte(inner)),
			MimeType:      p.MediaType,
			Name:          p.Filename,
		}
	case a2a.URL:
		wire.File = &FilePart{
			FileWithURI: string(c),
			MimeType:    p.MediaType,
			Name:        p.Filename,
		}
	case a2a.Data:
		wire.Data = &DataPart{Data: c.Value}
	}
	return wire
}

func taskToWire(task *a2a.Task) *Task {
	if task == nil {
		return nil
	}
	wire := &Task{
		ID:        string(task.ID),
		ContextID: task.ContextID,
		Status:    taskStatusToWire(task.Status),
		Metadata:  task.Metadata,
	}
	if len(task.Artifacts) > 0 {
		wire.Artifacts = make([]*Artifact, 0, len(task.Artifacts))
		for _, a := range task.Artifacts {
			wire.Artifacts = append(wire.Artifacts, artifactToWire(a))
		}
	}
	if len(task.History) > 0 {
		wire.History = make([]*Message, 0, len(task.History))
		for _, m := range task.History {
			wire.History = append(wire.History, messageToWire(m))
		}
	}
	return wire
}

func taskStatusToWire(s a2a.TaskStatus) *TaskStatus {
	wire := &TaskStatus{State: EncodeTaskState(s.State)}
	if s.Message != nil {
		wire.Message = messageToWire(s.Message)
	}
	if s.Timestamp != nil {
		ts := s.Timestamp.UTC()
		wire.Timestamp = &ts
	}
	return wire
}

// EncodeTaskState renders a v2 SDK TaskState using the wire enum name.
// The single spelling difference is TASK_STATE_CANCELED vs TASK_STATE_CANCELLED.
func EncodeTaskState(s a2a.TaskState) TaskState {
	if s == a2a.TaskStateCanceled {
		return TaskStateCancelled
	}
	return TaskState(s.String())
}

func roleToWire(r a2a.MessageRole) Role {
	if r == a2a.MessageRoleUnspecified {
		return RoleUnspecified
	}
	return Role(r)
}

func artifactToWire(a *a2a.Artifact) *Artifact {
	if a == nil {
		return nil
	}
	wire := &Artifact{
		ArtifactID:  string(a.ID),
		Name:        a.Name,
		Description: a.Description,
		Metadata:    a.Metadata,
		Extensions:  a.Extensions,
	}
	if len(a.Parts) > 0 {
		wire.Parts = make([]*Part, 0, len(a.Parts))
		for _, p := range a.Parts {
			wire.Parts = append(wire.Parts, partToWire(p))
		}
	}
	return wire
}

func taskStatusUpdateEventToWire(e *a2a.TaskStatusUpdateEvent) *TaskStatusUpdateEvent {
	// The v2 Go SDK does not carry the "final" flag on TaskStatusUpdateEvent;
	// derive it from the state (terminal states are always final).
	return &TaskStatusUpdateEvent{
		TaskID:    string(e.TaskID),
		ContextID: e.ContextID,
		Status:    taskStatusToWire(e.Status),
		Final:     e.Status.State.Terminal(),
		Metadata:  e.Metadata,
	}
}

func taskArtifactUpdateEventToWire(e *a2a.TaskArtifactUpdateEvent) *TaskArtifactUpdateEvent {
	wire := &TaskArtifactUpdateEvent{
		TaskID:    string(e.TaskID),
		ContextID: e.ContextID,
		Append:    e.Append,
		LastChunk: e.LastChunk,
		Metadata:  e.Metadata,
	}
	if e.Artifact != nil {
		wire.Artifact = artifactToWire(e.Artifact)
	}
	return wire
}

func sendMessageConfigToWire(cfg *a2a.SendMessageConfig) *SendMessageConfiguration {
	// The v0.3 proto uses "blocking" (positive: wait for completion), while
	// v1 SDK uses ReturnImmediately (negative). Emit both meanings faithfully:
	// blocking == !ReturnImmediately.
	wire := &SendMessageConfiguration{
		AcceptedOutputModes: cfg.AcceptedOutputModes,
		Blocking:            !cfg.ReturnImmediately,
	}
	if cfg.HistoryLength != nil {
		wire.HistoryLength = int32(*cfg.HistoryLength)
	}
	if cfg.PushConfig != nil {
		wire.PushNotification = pushNotificationConfigToWire(cfg.PushConfig)
	}
	return wire
}

func pushNotificationConfigToWire(pc *a2a.PushConfig) *PushNotificationConfig {
	wire := &PushNotificationConfig{
		ID:    pc.ID,
		URL:   pc.URL,
		Token: pc.Token,
	}
	if pc.Auth != nil {
		auth := &AuthenticationInfo{Credentials: pc.Auth.Credentials}
		if pc.Auth.Scheme != "" {
			auth.Schemes = []string{pc.Auth.Scheme}
		}
		wire.Authentication = auth
	}
	return wire
}

// taskPushNotificationConfigToWire produces the proto-JSON representation
// of TaskPushNotificationConfig{name, pushNotificationConfig}.
func taskPushNotificationConfigToWire(pc *a2a.PushConfig) *TaskPushNotificationConfig {
	return &TaskPushNotificationConfig{
		Name:                   formatPushConfigResourceName(pc.TaskID, pc.ID),
		PushNotificationConfig: pushNotificationConfigToWire(pc),
	}
}

// formatPushConfigResourceName builds the "tasks/{taskId}/pushNotificationConfigs/{configId}"
// resource name used by the push-config endpoints.
func formatPushConfigResourceName(taskID a2a.TaskID, configID string) string {
	return "tasks/" + string(taskID) + "/pushNotificationConfigs/" + configID
}
