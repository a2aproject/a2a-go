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

package a2av0

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
)

type sendMessageResult interface {
	event

	isSendMessageResult()
}

func (*task) isSendMessageResult()    {}
func (*message) isSendMessageResult() {}

type event interface {
	isEvent()
}

func (*message) isEvent()                 {}
func (*task) isEvent()                    {}
func (*taskStatusUpdateEvent) isEvent()   {}
func (*taskArtifactUpdateEvent) isEvent() {}

func unmarshalEventJSON(data []byte) (event, error) {
	type typedEvent struct {
		Kind string `json:"kind"`
	}

	var te typedEvent
	if err := json.Unmarshal(data, &te); err != nil {
		return nil, fmt.Errorf("failed to unmarshal event: %w", err)
	}

	switch te.Kind {
	case "message":
		var msg message
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, fmt.Errorf("failed to unmarshal Message event: %w", err)
		}
		return &msg, nil
	case "task":
		var task task
		if err := json.Unmarshal(data, &task); err != nil {
			return nil, fmt.Errorf("failed to unmarshal Task event: %w", err)
		}
		return &task, nil
	case "status-update":
		var statusUpdate taskStatusUpdateEvent
		if err := json.Unmarshal(data, &statusUpdate); err != nil {
			return nil, fmt.Errorf("failed to unmarshal TaskStatusUpdateEvent: %w", err)
		}
		return &statusUpdate, nil
	case "artifact-update":
		var artifactUpdate taskArtifactUpdateEvent
		if err := json.Unmarshal(data, &artifactUpdate); err != nil {
			return nil, fmt.Errorf("failed to unmarshal TaskArtifactUpdateEvent: %w", err)
		}
		return &artifactUpdate, nil
	default:
		return nil, fmt.Errorf("unknown event kind: %s", te.Kind)
	}
}

type message struct {
	ID             string          `json:"messageId"`
	ContextID      string          `json:"contextId,omitempty"`
	Extensions     []string        `json:"extensions,omitempty"`
	Metadata       map[string]any  `json:"metadata,omitempty"`
	Parts          ContentParts    `json:"parts"`
	ReferenceTasks []a2a.TaskID    `json:"referenceTaskIds,omitempty"`
	Role           a2a.MessageRole `json:"role"`
	TaskID         a2a.TaskID      `json:"taskId,omitempty"`
}

func (m message) MarshalJSON() ([]byte, error) {
	type wrapped message
	type withKind struct {
		Kind string `json:"kind"`
		wrapped
	}
	return json.Marshal(withKind{Kind: "message", wrapped: wrapped(m)})
}

type taskState string

const (
	taskStateUnspecified   taskState = ""
	taskStateAuthRequired  taskState = "auth-required"
	taskStateCanceled      taskState = "canceled"
	taskStateCompleted     taskState = "completed"
	taskStateFailed        taskState = "failed"
	taskStateInputRequired taskState = "input-required"
	taskStateRejected      taskState = "rejected"
	taskStateSubmitted     taskState = "submitted"
	taskStateUnknown       taskState = "unknown"
	taskStateWorking       taskState = "working"
)

type task struct {
	ID        a2a.TaskID     `json:"id"`
	Artifacts []*artifact    `json:"artifacts,omitempty"`
	ContextID string         `json:"contextId"`
	History   []*message     `json:"history,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	Status    taskStatus     `json:"status"`
}

func (t task) MarshalJSON() ([]byte, error) {
	type wrapped task
	type withKind struct {
		Kind string `json:"kind"`
		wrapped
	}
	return json.Marshal(withKind{Kind: "task", wrapped: wrapped(t)})
}

type taskStatus struct {
	Message   *message   `json:"message,omitempty"`
	State     taskState  `json:"state"`
	Timestamp *time.Time `json:"timestamp,omitempty"`
}

type artifact struct {
	ID          a2a.ArtifactID `json:"artifactId"`
	Description string         `json:"description,omitempty"`
	Extensions  []string       `json:"extensions,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	Name        string         `json:"name,omitempty"`
	Parts       ContentParts   `json:"parts"`
}

type taskArtifactUpdateEvent struct {
	Append    bool           `json:"append,omitempty"`
	Artifact  *artifact      `json:"artifact"`
	ContextID string         `json:"contextId"`
	LastChunk bool           `json:"lastChunk,omitempty"`
	TaskID    a2a.TaskID     `json:"taskId"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type taskStatusUpdateEvent struct {
	ContextID string         `json:"contextId"`
	Final     bool           `json:"final"`
	Status    taskStatus     `json:"status"`
	TaskID    a2a.TaskID     `json:"taskId"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

func (e taskStatusUpdateEvent) MarshalJSON() ([]byte, error) {
	type wrapped taskStatusUpdateEvent
	type withKind struct {
		Kind string `json:"kind"`
		wrapped
	}
	return json.Marshal(withKind{Kind: "status-update", wrapped: wrapped(e)})
}

type ContentParts []part

func (j ContentParts) MarshalJSON() ([]byte, error) {
	if j == nil {
		return []byte("[]"), nil
	}
	return json.Marshal([]part(j))
}

func (j *ContentParts) UnmarshalJSON(b []byte) error {
	type typedPart struct {
		Kind string `json:"kind"`
	}

	var arr []json.RawMessage
	if err := json.Unmarshal(b, &arr); err != nil {
		return err
	}

	result := make([]part, len(arr))
	for i, rawMsg := range arr {
		var tp typedPart
		if err := json.Unmarshal(rawMsg, &tp); err != nil {
			return err
		}
		switch tp.Kind {
		case "text":
			var part textPart
			if err := json.Unmarshal(rawMsg, &part); err != nil {
				return err
			}
			result[i] = part
		case "data":
			var part dataPart
			if err := json.Unmarshal(rawMsg, &part); err != nil {
				return err
			}
			result[i] = part
		case "file":
			var part filePart
			if err := json.Unmarshal(rawMsg, &part); err != nil {
				return err
			}
			result[i] = part
		default:
			return fmt.Errorf("unknown part kind %s", tp.Kind)
		}
	}

	*j = result
	return nil
}

type part interface {
	isPart()
}

func (textPart) isPart() {}
func (filePart) isPart() {}
func (dataPart) isPart() {}

type textPart struct {
	Text     string         `json:"text"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

func (p textPart) MarshalJSON() ([]byte, error) {
	type wrapped textPart
	type withKind struct {
		Kind string `json:"kind"`
		wrapped
	}
	return json.Marshal(withKind{Kind: "text", wrapped: wrapped(p)})
}

type dataPart struct {
	Data     map[string]any `json:"data"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

func (p dataPart) MarshalJSON() ([]byte, error) {
	type wrapped dataPart
	type withKind struct {
		Kind string `json:"kind"`
		wrapped
	}
	return json.Marshal(withKind{Kind: "data", wrapped: wrapped(p)})
}

type filePart struct {
	File     filePartContent `json:"file"`
	Metadata map[string]any  `json:"metadata,omitempty"`
}

func (p filePart) MarshalJSON() ([]byte, error) {
	type wrapped filePart
	type withKind struct {
		Kind string `json:"kind"`
		wrapped
	}
	return json.Marshal(withKind{Kind: "file", wrapped: wrapped(p)})
}

func (p *filePart) UnmarshalJSON(b []byte) error {
	type filePartContentUnion struct {
		FileMeta
		URI   string `json:"uri"`
		Bytes string `json:"bytes"`
	}
	type partJSON struct {
		File     filePartContentUnion `json:"file"`
		Metadata map[string]any       `json:"metadata"`
	}
	var decoded partJSON
	if err := json.Unmarshal(b, &decoded); err != nil {
		return err
	}

	if len(decoded.File.Bytes) == 0 && len(decoded.File.URI) == 0 {
		return fmt.Errorf("invalid file part: either Bytes or URI must be set")
	}
	if len(decoded.File.Bytes) > 0 && len(decoded.File.URI) > 0 {
		return fmt.Errorf("invalid file part: Bytes and URI cannot be set at the same time")
	}

	res := filePart{Metadata: decoded.Metadata}
	if len(decoded.File.Bytes) > 0 {
		res.File = fileBytes{Bytes: decoded.File.Bytes, FileMeta: decoded.File.FileMeta}
	} else {
		res.File = fileURI{URI: decoded.File.URI, FileMeta: decoded.File.FileMeta}
	}

	*p = res
	return nil
}

type filePartContent interface{ isFilePartContent() }

func (fileBytes) isFilePartContent() {}
func (fileURI) isFilePartContent()   {}

type FileMeta struct {
	MimeType string `json:"mimeType,omitempty"`
	Name     string `json:"name,omitempty"`
}

type fileBytes struct {
	FileMeta
	Bytes string `json:"bytes"`
}

type fileURI struct {
	FileMeta
	URI string `json:"uri"`
}

type taskIDParams struct {
	ID       a2a.TaskID     `json:"id"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type taskQueryParams struct {
	HistoryLength *int       `json:"historyLength,omitempty"`
	ID            a2a.TaskID `json:"id"`
}

type messageSendConfig struct {
	AcceptedOutputModes []string    `json:"acceptedOutputModes,omitempty"`
	Blocking            *bool       `json:"blocking,omitempty"`
	HistoryLength       *int        `json:"historyLength,omitempty"`
	PushConfig          *pushConfig `json:"pushNotificationConfig,omitempty"`
}

type messageSendParams struct {
	Config   *messageSendConfig `json:"configuration,omitempty"`
	Message  *message           `json:"message"`
	Metadata map[string]any     `json:"metadata,omitempty"`
}

type getTaskPushConfigParams struct {
	TaskID   a2a.TaskID `json:"id"`
	ConfigID string     `json:"pushNotificationConfigId,omitempty"`
}

type listTaskPushConfigParams struct {
	TaskID a2a.TaskID `json:"id"`
}

type deleteTaskPushConfigParams struct {
	TaskID   a2a.TaskID `json:"id"`
	ConfigID string     `json:"pushNotificationConfigId"`
}

type taskPushConfig struct {
	Config pushConfig `json:"pushNotificationConfig"`
	TaskID a2a.TaskID `json:"taskId"`
}

type pushConfig struct {
	ID    string        `json:"id,omitempty"`
	Auth  *pushAuthInfo `json:"authentication,omitempty"`
	Token string        `json:"token,omitempty"`
	URL   string        `json:"url"`
}

type pushAuthInfo struct {
	Credentials string   `json:"credentials,omitempty"`
	Schemes     []string `json:"schemes"`
}
