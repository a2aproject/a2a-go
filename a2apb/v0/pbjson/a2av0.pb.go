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

// Package pbjson contains plain Go structs mirroring the A2A v0.3 proto
// types (as generated for release/spec-v0 in the a2a-go v0.3 line), tailored
// for the proto-JSON wire format that other sdks emits and
// expects for its HTTP+JSON REST binding.
package pbjson

import "time"

// Role is the role of a Message sender.
type Role string

// Role enum values as they appear on the wire.
const (
	RoleUnspecified Role = "ROLE_UNSPECIFIED"
	RoleUser        Role = "ROLE_USER"
	RoleAgent       Role = "ROLE_AGENT"
)

// TaskState is the state of a Task.
//
// Note the British spelling of TaskStateCancelled: the v0.3 proto uses
// TASK_STATE_CANCELLED while the v2 Go SDK core uses TASK_STATE_CANCELED
// (American). The compat layer translates between the two.
type TaskState string

// TaskState enum values as they appear on the wire.
const (
	TaskStateUnspecified   TaskState = "TASK_STATE_UNSPECIFIED"
	TaskStateSubmitted     TaskState = "TASK_STATE_SUBMITTED"
	TaskStateWorking       TaskState = "TASK_STATE_WORKING"
	TaskStateCompleted     TaskState = "TASK_STATE_COMPLETED"
	TaskStateFailed        TaskState = "TASK_STATE_FAILED"
	TaskStateCancelled     TaskState = "TASK_STATE_CANCELLED"
	TaskStateInputRequired TaskState = "TASK_STATE_INPUT_REQUIRED"
	TaskStateRejected      TaskState = "TASK_STATE_REJECTED"
	TaskStateAuthRequired  TaskState = "TASK_STATE_AUTH_REQUIRED"
)

// SendMessageConfiguration mirrors a2a.v1.SendMessageConfiguration.
//
// Blocking is the inverse of a2a.SendMessageConfig.ReturnImmediately: a
// blocking send waits for task completion, a returnImmediately send does not.
// The field is intentionally NOT omitempty: the wire semantics distinguish
// "blocking=false" (return immediately) from "blocking absent" (server
// default), and v0.3 peers commonly treat absence as blocking=true.
type SendMessageConfiguration struct {
	AcceptedOutputModes []string                `json:"acceptedOutputModes,omitempty"`
	PushNotification    *PushNotificationConfig `json:"pushNotification,omitempty"`
	HistoryLength       int32                   `json:"historyLength,omitempty"`
	Blocking            bool                    `json:"blocking"`
}

// SendMessageRequest is the body of POST /v1/message:send and /v1/message:stream.
//
// The proto field is named "request" but its json_name override is "message",
// so the wire key is "message".
type SendMessageRequest struct {
	Message       *Message                  `json:"message,omitempty"`
	Configuration *SendMessageConfiguration `json:"configuration,omitempty"`
	Metadata      map[string]any            `json:"metadata,omitempty"`
}

// SendMessageResponse is the response body of POST /v1/message:send.
//
// This is a proto oneof {msg, task} where the "msg" field carries a
// json_name of "message". Exactly one of Message or Task should be set.
type SendMessageResponse struct {
	Message *Message `json:"message,omitempty"`
	Task    *Task    `json:"task,omitempty"`
}

// StreamResponse is one SSE event body of POST /v1/message:stream or
// GET /v1/tasks/{id}:subscribe.
//
// Proto oneof {msg, task, status_update, artifact_update}; "msg" has json_name
// "message". Exactly one field should be set.
type StreamResponse struct {
	Message        *Message                 `json:"message,omitempty"`
	Task           *Task                    `json:"task,omitempty"`
	StatusUpdate   *TaskStatusUpdateEvent   `json:"statusUpdate,omitempty"`
	ArtifactUpdate *TaskArtifactUpdateEvent `json:"artifactUpdate,omitempty"`
}

// Task mirrors a2a.v1.Task.
type Task struct {
	ID        string         `json:"id,omitempty"`
	ContextID string         `json:"contextId,omitempty"`
	Status    *TaskStatus    `json:"status,omitempty"`
	Artifacts []*Artifact    `json:"artifacts,omitempty"`
	History   []*Message     `json:"history,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// TaskStatus mirrors a2a.v1.TaskStatus.
//
// The proto field is named "update" but its json_name override is "message",
// so the wire key is "message".
type TaskStatus struct {
	State     TaskState  `json:"state,omitempty"`
	Message   *Message   `json:"message,omitempty"`
	Timestamp *time.Time `json:"timestamp,omitempty"`
}

// Part mirrors a2a.v1.Part.
//
// The proto uses a oneof of {text, file, data}. In JSON the oneof fields sit
// side-by-side at the same nesting level as Metadata. Exactly one of Text,
// File, or Data should be non-nil.
type Part struct {
	// Text is a plain string. A pointer distinguishes an absent field from
	// the empty string (a valid, if odd, text part).
	Text     *string        `json:"text,omitempty"`
	File     *FilePart      `json:"file,omitempty"`
	Data     *DataPart      `json:"data,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// FilePart mirrors a2a.v1.FilePart with an important quirk: FileWithBytes is
// modeled as a string (not []byte), because v0.3 peers double-encode bytes
// on the wire. The outer base64 is proto-JSON's byte encoding; the inner
// base64 is what the peer's FileWithBytes.bytes string field ultimately
// carries after proto-JSON parsing on that side.
//
// Callers pass an already-base64-encoded string; the JSON marshaler emits it
// verbatim, which proto-JSON on the peer's side then base64-decodes back to
// the original inner string.
type FilePart struct {
	FileWithURI   string `json:"fileWithUri,omitempty"`
	FileWithBytes string `json:"fileWithBytes,omitempty"`
	MimeType      string `json:"mimeType,omitempty"`
	Name          string `json:"name,omitempty"`
}

// DataPart mirrors a2a.v1.DataPart. Data is any JSON-serializable value; the
// proto schema uses google.protobuf.Struct which encodes as arbitrary JSON.
type DataPart struct {
	Data any `json:"data,omitempty"`
}

// Message mirrors a2a.v1.Message.
//
// The proto field for the message body is named "parts" (json_name "parts")
// in the a2a-go v0.3.15 stub; the wire shape used by v0.3 peers renames
// it to "content", which is what this struct follows.
type Message struct {
	MessageID        string         `json:"messageId,omitempty"`
	ContextID        string         `json:"contextId,omitempty"`
	TaskID           string         `json:"taskId,omitempty"`
	Role             Role           `json:"role,omitempty"`
	Content          []*Part        `json:"content,omitempty"`
	Metadata         map[string]any `json:"metadata,omitempty"`
	Extensions       []string       `json:"extensions,omitempty"`
	ReferenceTaskIDs []string       `json:"referenceTaskIds,omitempty"`
}

// Artifact mirrors a2a.v1.Artifact.
type Artifact struct {
	ArtifactID  string         `json:"artifactId,omitempty"`
	Name        string         `json:"name,omitempty"`
	Description string         `json:"description,omitempty"`
	Parts       []*Part        `json:"parts,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	Extensions  []string       `json:"extensions,omitempty"`
}

// TaskStatusUpdateEvent mirrors a2a.v1.TaskStatusUpdateEvent.
type TaskStatusUpdateEvent struct {
	TaskID    string         `json:"taskId,omitempty"`
	ContextID string         `json:"contextId,omitempty"`
	Status    *TaskStatus    `json:"status,omitempty"`
	Final     bool           `json:"final,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// TaskArtifactUpdateEvent mirrors a2a.v1.TaskArtifactUpdateEvent.
type TaskArtifactUpdateEvent struct {
	TaskID    string         `json:"taskId,omitempty"`
	ContextID string         `json:"contextId,omitempty"`
	Artifact  *Artifact      `json:"artifact,omitempty"`
	Append    bool           `json:"append,omitempty"`
	LastChunk bool           `json:"lastChunk,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// PushNotificationConfig mirrors a2a.v1.PushNotificationConfig.
type PushNotificationConfig struct {
	ID             string              `json:"id,omitempty"`
	URL            string              `json:"url,omitempty"`
	Token          string              `json:"token,omitempty"`
	Authentication *AuthenticationInfo `json:"authentication,omitempty"`
}

// TaskPushNotificationConfig is the wire shape returned by the push-config
// REST endpoints and posted (wrapped in CreateTaskPushNotificationConfigRequest)
// to POST /v1/tasks/{id}/pushNotificationConfigs.
type TaskPushNotificationConfig struct {
	Name                   string                  `json:"name,omitempty"`
	PushNotificationConfig *PushNotificationConfig `json:"pushNotificationConfig,omitempty"`
}

// CreateTaskPushNotificationConfigRequest is the body of
// POST /v1/tasks/{id}/pushNotificationConfigs.
type CreateTaskPushNotificationConfigRequest struct {
	Parent   string                      `json:"parent,omitempty"`
	ConfigID string                      `json:"configId,omitempty"`
	Config   *TaskPushNotificationConfig `json:"config,omitempty"`
}

// ListTasksResponse is the wire shape emitted by GET /v1/tasks in the
// AIP-158 style. Not implemented by all v0.3 peers (some return 501), but
// exposed for spec-compliant peers.
type ListTasksResponse struct {
	Tasks         []*Task `json:"tasks,omitempty"`
	NextPageToken string  `json:"nextPageToken,omitempty"`
}

// ListPushNotificationConfigsResponse is the wire shape emitted by
// GET /v1/tasks/{id}/pushNotificationConfigs.
type ListPushNotificationConfigsResponse struct {
	Configs []*TaskPushNotificationConfig `json:"configs,omitempty"`
}

// AuthenticationInfo mirrors a2a.v1.AuthenticationInfo used by
// PushNotificationConfig.
type AuthenticationInfo struct {
	Schemes     []string `json:"schemes,omitempty"`
	Credentials string   `json:"credentials,omitempty"`
}
