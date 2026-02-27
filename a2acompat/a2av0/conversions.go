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

package a2av0

import (
	"context"
	"encoding/base64"
	"fmt"

	a2alegacy "github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/v1/a2a"
	"github.com/a2aproject/a2a-go/v1/log"
)

var v1ToLegacyTaskState = map[a2a.TaskState]a2alegacy.TaskState{
	a2a.TaskStateAuthRequired:  a2alegacy.TaskStateAuthRequired,
	a2a.TaskStateCanceled:      a2alegacy.TaskStateCanceled,
	a2a.TaskStateCompleted:     a2alegacy.TaskStateCompleted,
	a2a.TaskStateFailed:        a2alegacy.TaskStateFailed,
	a2a.TaskStateInputRequired: a2alegacy.TaskStateInputRequired,
	a2a.TaskStateRejected:      a2alegacy.TaskStateRejected,
	a2a.TaskStateSubmitted:     a2alegacy.TaskStateSubmitted,
	a2a.TaskStateUnknown:       a2alegacy.TaskStateUnknown,
	a2a.TaskStateWorking:       a2alegacy.TaskStateWorking,
}

var legacyToV1TaskState map[a2alegacy.TaskState]a2a.TaskState

func init() {
	legacyToV1TaskState = make(map[a2alegacy.TaskState]a2a.TaskState, len(v1ToLegacyTaskState))
	for k, v := range v1ToLegacyTaskState {
		legacyToV1TaskState[v] = k
	}
}

// ToV1TaskState converts a legacy task state to a v1 task state.
func ToV1TaskState(s a2alegacy.TaskState) a2a.TaskState {
	if mapped, ok := legacyToV1TaskState[s]; ok {
		return mapped
	}
	return a2a.TaskStateUnspecified
}

// FromV1TaskState converts a v1 task state to a legacy task state.
func FromV1TaskState(s a2a.TaskState) a2alegacy.TaskState {
	if mapped, ok := v1ToLegacyTaskState[s]; ok {
		return mapped
	}
	return a2alegacy.TaskStateUnspecified
}

// ToV1Event converts a legacy event to a v1 event.
func ToV1Event(comp a2alegacy.Event) (a2a.Event, error) {
	if comp == nil {
		return nil, nil
	}
	switch v := comp.(type) {
	case *a2alegacy.Message:
		return ToV1Message(v)
	case *a2alegacy.Task:
		return ToV1Task(v)
	case *a2alegacy.TaskStatusUpdateEvent:
		return ToV1TaskStatusUpdateEvent(v)
	case *a2alegacy.TaskArtifactUpdateEvent:
		return ToV1TaskArtifactUpdateEvent(v)
	default:
		return nil, fmt.Errorf("unknown legacy event kind %T", comp)
	}
}

// FromV1Event converts a v1 event to a legacy event.
func FromV1Event(e a2a.Event) (a2alegacy.Event, error) {
	if e == nil {
		return nil, nil
	}
	switch v := e.(type) {
	case *a2a.Message:
		return FromV1Message(v), nil
	case *a2a.Task:
		return FromV1Task(v), nil
	case *a2a.TaskStatusUpdateEvent:
		return FromV1TaskStatusUpdateEvent(v), nil
	case *a2a.TaskArtifactUpdateEvent:
		return FromV1TaskArtifactUpdateEvent(v), nil
	default:
		return nil, fmt.Errorf("unknown event type %T", e)
	}
}

// ToV1Task converts a legacy task to a v1 task.
func ToV1Task(t *a2alegacy.Task) (*a2a.Task, error) {
	if t == nil {
		return nil, nil
	}
	var msg *a2a.Message
	if t.Status.Message != nil {
		var err error
		msg, err = ToV1Message(t.Status.Message)
		if err != nil {
			return nil, err
		}
	}
	res := &a2a.Task{
		ID:        a2a.TaskID(t.ID),
		ContextID: t.ContextID,
		Metadata:  t.Metadata,
		Status: a2a.TaskStatus{
			Message:   msg,
			State:     ToV1TaskState(t.Status.State),
			Timestamp: t.Status.Timestamp,
		},
	}
	if len(t.Artifacts) > 0 {
		res.Artifacts = make([]*a2a.Artifact, len(t.Artifacts))
		for i, a := range t.Artifacts {
			var err error
			res.Artifacts[i], err = ToV1Artifact(a)
			if err != nil {
				return nil, err
			}
		}
	}
	if len(t.History) > 0 {
		res.History = make([]*a2a.Message, len(t.History))
		for i, m := range t.History {
			var err error
			res.History[i], err = ToV1Message(m)
			if err != nil {
				return nil, err
			}
		}
	}
	return res, nil
}

// FromV1Task converts a v1 task to a legacy task.
func FromV1Task(t *a2a.Task) *a2alegacy.Task {
	if t == nil {
		return nil
	}
	res := &a2alegacy.Task{
		ID:        a2alegacy.TaskID(t.ID),
		ContextID: t.ContextID,
		Metadata:  t.Metadata,
		Status: a2alegacy.TaskStatus{
			Message:   FromV1Message(t.Status.Message),
			State:     FromV1TaskState(t.Status.State),
			Timestamp: t.Status.Timestamp,
		},
	}
	if len(t.Artifacts) > 0 {
		res.Artifacts = make([]*a2alegacy.Artifact, len(t.Artifacts))
		for i, a := range t.Artifacts {
			res.Artifacts[i] = FromV1Artifact(a)
		}
	}
	if len(t.History) > 0 {
		res.History = make([]*a2alegacy.Message, len(t.History))
		for i, m := range t.History {
			res.History[i] = FromV1Message(m)
		}
	}
	return res
}

// ToV1Message converts a legacy message to a v1 message.
func ToV1Message(m *a2alegacy.Message) (*a2a.Message, error) {
	if m == nil {
		return nil, nil
	}
	parts, err := ToV1Parts(m.Parts)
	if err != nil {
		return nil, err
	}
	return &a2a.Message{
		ID:             m.ID,
		ContextID:      m.ContextID,
		Extensions:     m.Extensions,
		Metadata:       m.Metadata,
		Parts:          parts,
		ReferenceTasks: toV1TaskIDs(m.ReferenceTasks),
		Role:           a2a.MessageRole(m.Role),
		TaskID:         a2a.TaskID(m.TaskID),
	}, nil
}

func toV1TaskIDs(ids []a2alegacy.TaskID) []a2a.TaskID {
	if ids == nil {
		return nil
	}
	res := make([]a2a.TaskID, len(ids))
	for i, id := range ids {
		res[i] = a2a.TaskID(id)
	}
	return res
}

func fromV1TaskIDs(ids []a2a.TaskID) []a2alegacy.TaskID {
	if ids == nil {
		return nil
	}
	res := make([]a2alegacy.TaskID, len(ids))
	for i, id := range ids {
		res[i] = a2alegacy.TaskID(id)
	}
	return res
}

// FromV1Message converts a v1 message to a legacy message.
func FromV1Message(m *a2a.Message) *a2alegacy.Message {
	if m == nil {
		return nil
	}
	return &a2alegacy.Message{
		ID:             m.ID,
		ContextID:      m.ContextID,
		Extensions:     m.Extensions,
		Metadata:       m.Metadata,
		Parts:          FromV1Parts(m.Parts),
		ReferenceTasks: fromV1TaskIDs(m.ReferenceTasks),
		Role:           a2alegacy.MessageRole(m.Role),
		TaskID:         a2alegacy.TaskID(m.TaskID),
	}
}

// ToV1Part converts a legacy part to a v1 part.
func ToV1Part(p a2alegacy.Part) *a2a.Part {
	switch c := p.(type) {
	case a2alegacy.TextPart:
		return &a2a.Part{
			Content:  a2a.Text(c.Text),
			Metadata: c.Metadata,
		}
	case a2alegacy.DataPart:
		var val any = c.Data
		if compat, ok := c.Metadata["data_part_compat"].(bool); ok && compat {
			val = c.Data["value"]
			delete(c.Metadata, "data_part_compat")
		}
		return &a2a.Part{
			Content:  a2a.Data{Value: val},
			Metadata: c.Metadata,
		}
	case a2alegacy.FilePart:
		switch f := c.File.(type) {
		case a2alegacy.FileBytes:
			bytes, err := base64.StdEncoding.DecodeString(f.Bytes)
			if err != nil {
				bytes = []byte(f.Bytes)
				log.Warn(context.Background(), "failed to decode base64 content", "error", err)
			}
			return &a2a.Part{
				Content:   a2a.Raw(bytes),
				Metadata:  c.Metadata,
				MediaType: f.MimeType,
				Filename:  f.Name,
			}
		case a2alegacy.FileURI:
			return &a2a.Part{
				Content:   a2a.URL(f.URI),
				Metadata:  c.Metadata,
				MediaType: f.MimeType,
				Filename:  f.Name,
			}
		default:
			log.Warn(context.Background(), fmt.Sprintf("unknown file type %T", f))
			return &a2a.Part{Content: a2a.Data{Value: f}, Metadata: c.Metadata}
		}
	default:
		log.Warn(context.Background(), fmt.Sprintf("unknown part type %T", c))
		return &a2a.Part{Content: a2a.Data{Value: c}, Metadata: p.Meta()}
	}
}

// ToV1Parts converts legacy content parts to v1 content parts.
func ToV1Parts(parts a2alegacy.ContentParts) (a2a.ContentParts, error) {
	if len(parts) == 0 {
		return nil, nil
	}
	res := make(a2a.ContentParts, len(parts))
	for i, p := range parts {
		res[i] = ToV1Part(p)
	}
	return res, nil
}

// FromV1Part converts a v1 part to a legacy part.
func FromV1Part(p *a2a.Part) a2alegacy.Part {
	switch c := p.Content.(type) {
	case a2a.Text:
		return a2alegacy.TextPart{
			Text:     string(c),
			Metadata: p.Metadata,
		}
	case a2a.Data:
		val := c.Value
		// Compatibility mode: wrap non-map values to avoid crashing old clients that expect a map.
		if _, ok := val.(map[string]any); !ok {
			val = map[string]any{"value": val}
			if p.Metadata == nil {
				p.Metadata = make(map[string]any)
			}
			p.Metadata["data_part_compat"] = true
		}
		return a2alegacy.DataPart{
			Data:     val.(map[string]any),
			Metadata: p.Metadata,
		}
	case a2a.Raw:
		return a2alegacy.FilePart{
			File: a2alegacy.FileBytes{
				FileMeta: a2alegacy.FileMeta{
					MimeType: p.MediaType,
					Name:     p.Filename,
				},
				Bytes: base64.StdEncoding.EncodeToString(c),
			},
			Metadata: p.Metadata,
		}
	case a2a.URL:
		return a2alegacy.FilePart{
			File: a2alegacy.FileURI{
				FileMeta: a2alegacy.FileMeta{
					MimeType: p.MediaType,
					Name:     p.Filename,
				},
				URI: string(c),
			},
			Metadata: p.Metadata,
		}
	default:
		return a2alegacy.DataPart{
			Data:     map[string]any{"value": p},
			Metadata: p.Metadata,
		}
	}
}

// FromV1Parts converts v1 content parts to legacy content parts.
func FromV1Parts(parts a2a.ContentParts) a2alegacy.ContentParts {
	if len(parts) == 0 {
		return nil
	}
	res := make(a2alegacy.ContentParts, len(parts))
	for i, p := range parts {
		res[i] = FromV1Part(p)
	}
	return res
}

// ToV1Artifact converts a legacy artifact to a v1 artifact.
func ToV1Artifact(a *a2alegacy.Artifact) (*a2a.Artifact, error) {
	if a == nil {
		return nil, nil
	}
	parts, err := ToV1Parts(a.Parts)
	if err != nil {
		return nil, err
	}
	return &a2a.Artifact{
		ID:          a2a.ArtifactID(a.ID),
		Description: a.Description,
		Extensions:  a.Extensions,
		Metadata:    a.Metadata,
		Name:        a.Name,
		Parts:       parts,
	}, nil
}

// FromV1Artifact converts a v1 artifact to a legacy artifact.
func FromV1Artifact(a *a2a.Artifact) *a2alegacy.Artifact {
	if a == nil {
		return nil
	}
	return &a2alegacy.Artifact{
		ID:          a2alegacy.ArtifactID(a.ID),
		Description: a.Description,
		Extensions:  a.Extensions,
		Metadata:    a.Metadata,
		Name:        a.Name,
		Parts:       FromV1Parts(a.Parts),
	}
}

// ToV1TaskStatusUpdateEvent converts a legacy task status update event to a v1 event.
func ToV1TaskStatusUpdateEvent(e *a2alegacy.TaskStatusUpdateEvent) (*a2a.TaskStatusUpdateEvent, error) {
	if e == nil {
		return nil, nil
	}
	msg, err := ToV1Message(e.Status.Message)
	if err != nil {
		return nil, err
	}
	return &a2a.TaskStatusUpdateEvent{
		ContextID: e.ContextID,
		Status: a2a.TaskStatus{
			Message:   msg,
			State:     ToV1TaskState(e.Status.State),
			Timestamp: e.Status.Timestamp,
		},
		TaskID:   a2a.TaskID(e.TaskID),
		Metadata: e.Metadata,
	}, nil
}

// FromV1TaskStatusUpdateEvent converts a v1 task status update event to a legacy event.
func FromV1TaskStatusUpdateEvent(e *a2a.TaskStatusUpdateEvent) *a2alegacy.TaskStatusUpdateEvent {
	if e == nil {
		return nil
	}
	return &a2alegacy.TaskStatusUpdateEvent{
		ContextID: e.ContextID,
		Final:     e.Status.State.Terminal(),
		Status: a2alegacy.TaskStatus{
			Message:   FromV1Message(e.Status.Message),
			State:     FromV1TaskState(e.Status.State),
			Timestamp: e.Status.Timestamp,
		},
		TaskID:   a2alegacy.TaskID(e.TaskID),
		Metadata: e.Metadata,
	}
}

// ToV1TaskArtifactUpdateEvent converts a legacy task artifact update event to a v1 event.
func ToV1TaskArtifactUpdateEvent(e *a2alegacy.TaskArtifactUpdateEvent) (*a2a.TaskArtifactUpdateEvent, error) {
	if e == nil {
		return nil, nil
	}
	artifact, err := ToV1Artifact(e.Artifact)
	if err != nil {
		return nil, err
	}
	return &a2a.TaskArtifactUpdateEvent{
		Append:    e.Append,
		Artifact:  artifact,
		ContextID: e.ContextID,
		LastChunk: e.LastChunk,
		TaskID:    a2a.TaskID(e.TaskID),
		Metadata:  e.Metadata,
	}, nil
}

// FromV1TaskArtifactUpdateEvent converts a v1 task artifact update event to a legacy event.
func FromV1TaskArtifactUpdateEvent(e *a2a.TaskArtifactUpdateEvent) *a2alegacy.TaskArtifactUpdateEvent {
	if e == nil {
		return nil
	}
	return &a2alegacy.TaskArtifactUpdateEvent{
		Append:    e.Append,
		Artifact:  FromV1Artifact(e.Artifact),
		ContextID: e.ContextID,
		LastChunk: e.LastChunk,
		TaskID:    a2alegacy.TaskID(e.TaskID),
		Metadata:  e.Metadata,
	}
}

// ToV1PushConfig converts a legacy push config to a v1 push config.
func ToV1PushConfig(c a2alegacy.PushConfig) *a2a.PushConfig {
	res := &a2a.PushConfig{
		ID:    c.ID,
		Token: c.Token,
		URL:   c.URL,
	}
	if c.Auth != nil {
		res.Auth = &a2a.PushAuthInfo{
			Credentials: c.Auth.Credentials,
		}
		if len(c.Auth.Schemes) > 0 {
			res.Auth.Scheme = c.Auth.Schemes[0]
		}
	}
	return res
}

// FromV1PushConfig converts a v1 push config to a legacy push config.
func FromV1PushConfig(c *a2a.PushConfig) *a2alegacy.PushConfig {
	if c == nil {
		return nil
	}
	res := &a2alegacy.PushConfig{
		ID:    c.ID,
		Token: c.Token,
		URL:   c.URL,
	}
	if c.Auth != nil {
		res.Auth = &a2alegacy.PushAuthInfo{
			Credentials: c.Auth.Credentials,
			Schemes:     []string{c.Auth.Scheme},
		}
	}
	return res
}

// ToV1TaskPushConfig converts a legacy task push config to a v1 task push config.
func ToV1TaskPushConfig(comp *a2alegacy.TaskPushConfig) (*a2a.TaskPushConfig, error) {
	if comp == nil {
		return nil, nil
	}
	return &a2a.TaskPushConfig{TaskID: a2a.TaskID(comp.TaskID), Config: *ToV1PushConfig(comp.Config)}, nil
}

// FromV1TaskPushConfig converts a v1 task push config to a legacy task push config.
func FromV1TaskPushConfig(c *a2a.TaskPushConfig) (*a2alegacy.TaskPushConfig, error) {
	if c == nil {
		return nil, nil
	}
	res := &a2alegacy.TaskPushConfig{
		Config: *FromV1PushConfig(&c.Config),
		TaskID: a2alegacy.TaskID(c.TaskID),
	}
	return res, nil
}

// FromV1TaskPushConfigs converts multiple v1 task push configs to legacy task push configs.
func FromV1TaskPushConfigs(cs []*a2a.TaskPushConfig) ([]*a2alegacy.TaskPushConfig, error) {
	var res []*a2alegacy.TaskPushConfig
	for _, c := range cs {
		comp, err := FromV1TaskPushConfig(c)
		if err != nil {
			return nil, err
		}
		if comp != nil {
			res = append(res, comp)
		}
	}
	return res, nil
}

// ToV1GetTaskRequest converts a legacy get task request to a v1 request.
func ToV1GetTaskRequest(q *a2alegacy.TaskQueryParams) *a2a.GetTaskRequest {
	return &a2a.GetTaskRequest{
		ID:            a2a.TaskID(q.ID),
		HistoryLength: q.HistoryLength,
	}
}

// FromV1GetTaskRequest converts a v1 get task request to a legacy request.
func FromV1GetTaskRequest(req *a2a.GetTaskRequest) *a2alegacy.TaskQueryParams {
	if req == nil {
		return nil
	}
	return &a2alegacy.TaskQueryParams{
		ID:            a2alegacy.TaskID(req.ID),
		HistoryLength: req.HistoryLength,
	}
}

// ToV1CancelTaskRequest converts a legacy cancel task request to a v1 request.
func ToV1CancelTaskRequest(q *a2alegacy.TaskIDParams) *a2a.CancelTaskRequest {
	return &a2a.CancelTaskRequest{ID: a2a.TaskID(q.ID), Metadata: q.Metadata}
}

// FromV1CancelTaskRequest converts a v1 cancel task request to a legacy request.
func FromV1CancelTaskRequest(req *a2a.CancelTaskRequest) *a2alegacy.TaskIDParams {
	if req == nil {
		return nil
	}
	return &a2alegacy.TaskIDParams{ID: a2alegacy.TaskID(req.ID), Metadata: req.Metadata}
}

// ToV1SendMessageRequest converts a legacy send message request to a v1 request.
func ToV1SendMessageRequest(p *a2alegacy.MessageSendParams) (*a2a.SendMessageRequest, error) {
	msg, err := ToV1Message(p.Message)
	if err != nil {
		return nil, err
	}
	req := &a2a.SendMessageRequest{
		Message:  msg,
		Metadata: p.Metadata,
	}
	if p.Config != nil {
		req.Config = &a2a.SendMessageConfig{
			AcceptedOutputModes: p.Config.AcceptedOutputModes,
			Blocking:            p.Config.Blocking,
			HistoryLength:       p.Config.HistoryLength,
		}
		if p.Config.PushConfig != nil {
			req.Config.PushConfig = ToV1PushConfig(*p.Config.PushConfig)
		}
	}
	return req, nil
}

// FromV1SendMessageRequest converts a v1 send message request to a legacy request.
func FromV1SendMessageRequest(req *a2a.SendMessageRequest) *a2alegacy.MessageSendParams {
	if req == nil {
		return nil
	}
	res := &a2alegacy.MessageSendParams{
		Message:  FromV1Message(req.Message),
		Metadata: req.Metadata,
	}
	if req.Config != nil {
		res.Config = &a2alegacy.MessageSendConfig{
			AcceptedOutputModes: req.Config.AcceptedOutputModes,
			Blocking:            req.Config.Blocking,
			HistoryLength:       req.Config.HistoryLength,
		}
		if req.Config.PushConfig != nil {
			res.Config.PushConfig = FromV1PushConfig(req.Config.PushConfig)
		}
	}
	return res
}

// ToV1SubscribeToTaskRequest converts a legacy subscribe to task request to a v1 request.
func ToV1SubscribeToTaskRequest(q *a2alegacy.TaskIDParams) *a2a.SubscribeToTaskRequest {
	return &a2a.SubscribeToTaskRequest{
		ID: a2a.TaskID(q.ID),
	}
}

// FromV1SubscribeToTaskRequest converts a v1 subscribe to task request to a legacy request.
func FromV1SubscribeToTaskRequest(req *a2a.SubscribeToTaskRequest) *a2alegacy.TaskIDParams {
	if req == nil {
		return nil
	}
	return &a2alegacy.TaskIDParams{
		ID: a2alegacy.TaskID(req.ID),
	}
}

// ToV1GetTaskPushConfigRequest converts a legacy get task push config request to a v1 request.
func ToV1GetTaskPushConfigRequest(p *a2alegacy.GetTaskPushConfigParams) *a2a.GetTaskPushConfigRequest {
	return &a2a.GetTaskPushConfigRequest{
		TaskID: a2a.TaskID(p.TaskID),
		ID:     p.ConfigID,
	}
}

// FromV1GetTaskPushConfigRequest converts a v1 get task push config request to a legacy request.
func FromV1GetTaskPushConfigRequest(req *a2a.GetTaskPushConfigRequest) *a2alegacy.GetTaskPushConfigParams {
	if req == nil {
		return nil
	}
	return &a2alegacy.GetTaskPushConfigParams{
		TaskID:   a2alegacy.TaskID(req.TaskID),
		ConfigID: req.ID,
	}
}

// ToV1ListTaskPushConfigRequest converts a legacy list task push config request to a v1 request.
func ToV1ListTaskPushConfigRequest(p *a2alegacy.ListTaskPushConfigParams) *a2a.ListTaskPushConfigRequest {
	return &a2a.ListTaskPushConfigRequest{
		TaskID: a2a.TaskID(p.TaskID),
	}
}

// FromV1ListTaskPushConfigRequest converts a v1 list task push config request to a legacy request.
func FromV1ListTaskPushConfigRequest(req *a2a.ListTaskPushConfigRequest) *a2alegacy.ListTaskPushConfigParams {
	if req == nil {
		return nil
	}
	return &a2alegacy.ListTaskPushConfigParams{
		TaskID: a2alegacy.TaskID(req.TaskID),
	}
}

// ToV1CreateTaskPushConfigRequest converts a legacy create task push config request to a v1 request.
func ToV1CreateTaskPushConfigRequest(p *a2alegacy.TaskPushConfig) *a2a.CreateTaskPushConfigRequest {
	return &a2a.CreateTaskPushConfigRequest{
		TaskID: a2a.TaskID(p.TaskID),
		Config: *ToV1PushConfig(p.Config),
	}
}

// FromV1CreateTaskPushConfigRequest converts a v1 create task push config request to a legacy request.
func FromV1CreateTaskPushConfigRequest(req *a2a.CreateTaskPushConfigRequest) *a2alegacy.TaskPushConfig {
	if req == nil {
		return nil
	}
	cfg := FromV1PushConfig(&req.Config)
	if cfg == nil {
		cfg = &a2alegacy.PushConfig{}
	}
	return &a2alegacy.TaskPushConfig{TaskID: a2alegacy.TaskID(req.TaskID), Config: *cfg}
}

// ToV1DeleteTaskPushConfigRequest converts a legacy delete task push config request to a v1 request.
func ToV1DeleteTaskPushConfigRequest(p *a2alegacy.DeleteTaskPushConfigParams) *a2a.DeleteTaskPushConfigRequest {
	return &a2a.DeleteTaskPushConfigRequest{
		TaskID: a2a.TaskID(p.TaskID),
		ID:     p.ConfigID,
	}
}

// FromV1DeleteTaskPushConfigRequest converts a v1 delete task push config request to a legacy request.
func FromV1DeleteTaskPushConfigRequest(req *a2a.DeleteTaskPushConfigRequest) *a2alegacy.DeleteTaskPushConfigParams {
	if req == nil {
		return nil
	}
	return &a2alegacy.DeleteTaskPushConfigParams{
		TaskID:   a2alegacy.TaskID(req.TaskID),
		ConfigID: req.ID,
	}
}

// FromV1ListTasksRequest converts a v1 list tasks request to a legacy request.
func FromV1ListTasksRequest(req *a2a.ListTasksRequest) *a2alegacy.ListTasksRequest {
	if req == nil {
		return nil
	}
	var historyLength int
	if req.HistoryLength != nil {
		historyLength = *req.HistoryLength
	}
	return &a2alegacy.ListTasksRequest{
		ContextID:        req.ContextID,
		Status:           FromV1TaskState(req.Status),
		PageSize:         req.PageSize,
		PageToken:        req.PageToken,
		HistoryLength:    historyLength,
		LastUpdatedAfter: req.StatusTimestampAfter,
		IncludeArtifacts: req.IncludeArtifacts,
	}
}

// ToV1ListTasksRequest converts a legacy list tasks request to a v1 request.
func ToV1ListTasksRequest(req *a2alegacy.ListTasksRequest) *a2a.ListTasksRequest {
	if req == nil {
		return nil
	}
	return &a2a.ListTasksRequest{
		ContextID:            req.ContextID,
		Status:               ToV1TaskState(req.Status),
		PageSize:             req.PageSize,
		PageToken:            req.PageToken,
		HistoryLength:        &req.HistoryLength,
		StatusTimestampAfter: req.LastUpdatedAfter,
		IncludeArtifacts:     req.IncludeArtifacts,
	}
}

// ToV1ListTasksResponse converts a legacy list tasks response to a v1 response.
func ToV1ListTasksResponse(resp *a2alegacy.ListTasksResponse) (*a2a.ListTasksResponse, error) {
	if resp == nil {
		return nil, nil
	}
	tasks := make([]*a2a.Task, len(resp.Tasks))
	for i, t := range resp.Tasks {
		var err error
		tasks[i], err = ToV1Task(t)
		if err != nil {
			return nil, err
		}
	}
	return &a2a.ListTasksResponse{
		Tasks:         tasks,
		TotalSize:     resp.TotalSize,
		PageSize:      resp.PageSize,
		NextPageToken: resp.NextPageToken,
	}, nil
}

// FromV1ListTasksResponse converts a v1 list tasks response to a legacy response.
func FromV1ListTasksResponse(resp *a2a.ListTasksResponse) *a2alegacy.ListTasksResponse {
	if resp == nil {
		return nil
	}
	tasks := make([]*a2alegacy.Task, len(resp.Tasks))
	for i, t := range resp.Tasks {
		tasks[i] = FromV1Task(t)
	}
	return &a2alegacy.ListTasksResponse{
		Tasks:         tasks,
		TotalSize:     resp.TotalSize,
		PageSize:      resp.PageSize,
		NextPageToken: resp.NextPageToken,
	}
}

// FromV1AgentCard converts a v1 agent card to a legacy agent card.
func FromV1AgentCard(card *a2a.AgentCard) *a2alegacy.AgentCard {
	if card == nil {
		return nil
	}
	// Simplified conversion, focusing on common fields.
	// For full conversion, more complex mapping of interfaces/security is needed.
	res := &a2alegacy.AgentCard{
		DefaultInputModes:  card.DefaultInputModes,
		DefaultOutputModes: card.DefaultOutputModes,
		Description:        card.Description,
		DocumentationURL:   card.DocumentationURL,
		IconURL:            card.IconURL,
		Name:               card.Name,
		Provider:           (*a2alegacy.AgentProvider)(card.Provider),
		Signatures:         make([]a2alegacy.AgentCardSignature, len(card.Signatures)),
		Version:            card.Version,
		Capabilities: a2alegacy.AgentCapabilities{
			PushNotifications: card.Capabilities.PushNotifications,
			Streaming:         card.Capabilities.Streaming,
		},
	}
	if len(card.Capabilities.Extensions) > 0 {
		res.Capabilities.Extensions = make([]a2alegacy.AgentExtension, len(card.Capabilities.Extensions))
		for i, e := range card.Capabilities.Extensions {
			res.Capabilities.Extensions[i] = a2alegacy.AgentExtension(e)
		}
	}
	for i, s := range card.Signatures {
		res.Signatures[i] = a2alegacy.AgentCardSignature(s)
	}
	if len(card.SupportedInterfaces) > 0 {
		res.URL = card.SupportedInterfaces[0].URL
		res.PreferredTransport = a2alegacy.TransportProtocol(card.SupportedInterfaces[0].ProtocolBinding)
		res.ProtocolVersion = string(card.SupportedInterfaces[0].ProtocolVersion)

		res.AdditionalInterfaces = make([]a2alegacy.AgentInterface, len(card.SupportedInterfaces))
		for i, iface := range card.SupportedInterfaces {
			res.AdditionalInterfaces[i] = a2alegacy.AgentInterface{
				URL:       iface.URL,
				Transport: a2alegacy.TransportProtocol(iface.ProtocolBinding),
			}
		}
	}
	return res
}

// ToV1AgentCard converts a legacy agent card to a v1 agent card.
func ToV1AgentCard(card *a2alegacy.AgentCard) *a2a.AgentCard {
	if card == nil {
		return nil
	}
	res := &a2a.AgentCard{
		DefaultInputModes:  card.DefaultInputModes,
		DefaultOutputModes: card.DefaultOutputModes,
		Description:        card.Description,
		DocumentationURL:   card.DocumentationURL,
		IconURL:            card.IconURL,
		Name:               card.Name,
		Provider:           (*a2a.AgentProvider)(card.Provider),
		Signatures:         make([]a2a.AgentCardSignature, len(card.Signatures)),
		Version:            card.Version,
		Capabilities: a2a.AgentCapabilities{
			PushNotifications: card.Capabilities.PushNotifications,
			Streaming:         card.Capabilities.Streaming,
			ExtendedAgentCard: card.SupportsAuthenticatedExtendedCard,
		},
	}
	if len(card.Capabilities.Extensions) > 0 {
		res.Capabilities.Extensions = make([]a2a.AgentExtension, len(card.Capabilities.Extensions))
		for i, e := range card.Capabilities.Extensions {
			res.Capabilities.Extensions[i] = a2a.AgentExtension(e)
		}
	}
	for i, s := range card.Signatures {
		res.Signatures[i] = a2a.AgentCardSignature(s)
	}
	var ifaces []*a2a.AgentInterface
	if card.URL != "" {
		iface := &a2a.AgentInterface{
			URL:             card.URL,
			ProtocolBinding: a2a.TransportProtocol(card.PreferredTransport),
			ProtocolVersion: a2a.ProtocolVersion(card.ProtocolVersion),
		}
		if iface.ProtocolVersion == "" {
			iface.ProtocolVersion = a2a.ProtocolVersion(Version)
		}
		ifaces = append(ifaces, iface)
	}
	for _, ai := range card.AdditionalInterfaces {
		// Avoid duplicates if main URL is also in AdditionalInterfaces
		exists := false
		for _, existing := range ifaces {
			if existing.URL == ai.URL && string(existing.ProtocolBinding) == string(ai.Transport) {
				exists = true
				break
			}
		}
		if !exists {
			ifaces = append(ifaces, &a2a.AgentInterface{
				URL:             ai.URL,
				ProtocolBinding: a2a.TransportProtocol(ai.Transport),
				ProtocolVersion: a2a.ProtocolVersion(Version),
			})
		}
	}
	res.SupportedInterfaces = ifaces
	return res
}
