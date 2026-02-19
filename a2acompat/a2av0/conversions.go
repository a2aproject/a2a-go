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
	"encoding/base64"
	"fmt"

	"github.com/a2aproject/a2a-go/a2a"
)

var coreToCompatTaskState = map[a2a.TaskState]taskState{
	a2a.TaskStateAuthRequired:  taskStateAuthRequired,
	a2a.TaskStateCanceled:      taskStateCanceled,
	a2a.TaskStateCompleted:     taskStateCompleted,
	a2a.TaskStateFailed:        taskStateFailed,
	a2a.TaskStateInputRequired: taskStateInputRequired,
	a2a.TaskStateRejected:      taskStateRejected,
	a2a.TaskStateSubmitted:     taskStateSubmitted,
	a2a.TaskStateUnknown:       taskStateUnknown,
	a2a.TaskStateWorking:       taskStateWorking,
}

var compatToCoreTaskState map[taskState]a2a.TaskState

func init() {
	compatToCoreTaskState = make(map[taskState]a2a.TaskState, len(coreToCompatTaskState))
	for k, v := range coreToCompatTaskState {
		compatToCoreTaskState[v] = k
	}
}

func toCompatTaskState(s a2a.TaskState) taskState {
	if mapped, ok := coreToCompatTaskState[s]; ok {
		return mapped
	}
	return taskStateUnspecified
}

func fromCompatTaskState(s taskState) a2a.TaskState {
	if mapped, ok := compatToCoreTaskState[s]; ok {
		return mapped
	}
	return a2a.TaskStateUnspecified
}

func toCompatEvent(e a2a.Event) (event, error) {
	if e == nil {
		return nil, nil
	}
	switch v := e.(type) {
	case *a2a.Message:
		return toCompatMessage(v), nil
	case *a2a.Task:
		return toCompatTask(v), nil
	case *a2a.TaskStatusUpdateEvent:
		return toCompatTaskStatusUpdateEvent(v), nil
	case *a2a.TaskArtifactUpdateEvent:
		return toCompatTaskArtifactUpdateEvent(v), nil
	default:
		return nil, fmt.Errorf("unknown event type %T", e)
	}
}

func toCompatTask(t *a2a.Task) *task {
	if t == nil {
		return nil
	}
	res := &task{
		ID:        t.ID,
		ContextID: t.ContextID,
		Metadata:  t.Metadata,
		Status: taskStatus{
			Message:   toCompatMessage(t.Status.Message),
			State:     toCompatTaskState(t.Status.State),
			Timestamp: t.Status.Timestamp,
		},
	}
	if len(t.Artifacts) > 0 {
		res.Artifacts = make([]*artifact, len(t.Artifacts))
		for i, a := range t.Artifacts {
			res.Artifacts[i] = toCompatArtifact(a)
		}
	}
	if len(t.History) > 0 {
		res.History = make([]*message, len(t.History))
		for i, m := range t.History {
			res.History[i] = toCompatMessage(m)
		}
	}
	return res
}

func toCompatMessage(m *a2a.Message) *message {
	if m == nil {
		return nil
	}
	return &message{
		ID:             m.ID,
		ContextID:      m.ContextID,
		Extensions:     m.Extensions,
		Metadata:       m.Metadata,
		Parts:          toCompatParts(m.Parts),
		ReferenceTasks: m.ReferenceTasks,
		Role:           m.Role,
		TaskID:         m.TaskID,
	}
}

func toCompatParts(parts a2a.ContentParts) ContentParts {
	if len(parts) == 0 {
		return nil
	}
	res := make(ContentParts, len(parts))
	for i, p := range parts {
		switch c := p.Content.(type) {
		case a2a.Text:
			res[i] = textPart{
				Text:     string(c),
				Metadata: p.Metadata,
			}
		case a2a.Data:
			res[i] = dataPart{
				Data:     map[string]any(c),
				Metadata: p.Metadata,
			}
		case a2a.Raw:
			res[i] = filePart{
				File: fileBytes{
					FileMeta: FileMeta{
						MimeType: p.MediaType,
						Name:     p.Filename,
					},
					Bytes: base64.StdEncoding.EncodeToString(c),
				},
				Metadata: p.Metadata,
			}
		case a2a.URL:
			res[i] = filePart{
				File: fileURI{
					FileMeta: FileMeta{
						MimeType: p.MediaType,
						Name:     p.Filename,
					},
					URI: string(c),
				},
				Metadata: p.Metadata,
			}
		}
	}
	return res
}

func toCompatArtifact(a *a2a.Artifact) *artifact {
	if a == nil {
		return nil
	}
	return &artifact{
		ID:          a.ID,
		Description: a.Description,
		Extensions:  a.Extensions,
		Metadata:    a.Metadata,
		Name:        a.Name,
		Parts:       toCompatParts(a.Parts),
	}
}

func toCompatTaskStatusUpdateEvent(e *a2a.TaskStatusUpdateEvent) *taskStatusUpdateEvent {
	if e == nil {
		return nil
	}
	return &taskStatusUpdateEvent{
		ContextID: e.ContextID,
		Final:     e.Status.State.Terminal(),
		Status: taskStatus{
			Message:   toCompatMessage(e.Status.Message),
			State:     toCompatTaskState(e.Status.State),
			Timestamp: e.Status.Timestamp,
		},
		TaskID:   e.TaskID,
		Metadata: e.Metadata,
	}
}

func toCompatTaskArtifactUpdateEvent(e *a2a.TaskArtifactUpdateEvent) *taskArtifactUpdateEvent {
	if e == nil {
		return nil
	}
	return &taskArtifactUpdateEvent{
		Append:    e.Append,
		Artifact:  toCompatArtifact(e.Artifact),
		ContextID: e.ContextID,
		LastChunk: e.LastChunk,
		TaskID:    e.TaskID,
		Metadata:  e.Metadata,
	}
}

func toCompatPushConfig(c *a2a.PushConfig) *pushConfig {
	if c == nil {
		return nil
	}
	res := &pushConfig{
		ID:    c.ID,
		Token: c.Token,
		URL:   c.URL,
	}
	if c.Auth != nil {
		res.Auth = &pushAuthInfo{
			Credentials: c.Auth.Credentials,
			Schemes:     []string{c.Auth.Scheme},
		}
	}
	return res
}

func toCompatTaskPushConfig(c *a2a.TaskPushConfig) (*taskPushConfig, error) {
	if c == nil {
		return nil, nil
	}
	res := &taskPushConfig{
		Config: pushConfig{
			ID:    c.Config.ID,
			Token: c.Config.Token,
			URL:   c.Config.URL,
		},
		TaskID: c.TaskID,
	}
	if c.Config.Auth != nil {
		res.Config.Auth = &pushAuthInfo{
			Credentials: c.Config.Auth.Credentials,
			Schemes:     []string{c.Config.Auth.Scheme},
		}
	}
	return res, nil
}

func toCompatTaskPushConfigs(cs []*a2a.TaskPushConfig) ([]*taskPushConfig, error) {
	var res []*taskPushConfig
	for _, c := range cs {
		comp, err := toCompatTaskPushConfig(c)
		if err != nil {
			return nil, err
		}
		if comp != nil {
			res = append(res, comp)
		}
	}
	return res, nil
}

func toCompatGetTaskRequest(req *a2a.GetTaskRequest) *taskQueryParams {
	if req == nil {
		return nil
	}
	return &taskQueryParams{
		ID:            req.ID,
		HistoryLength: req.HistoryLength,
	}
}

func toCompatCancelTaskRequest(req *a2a.CancelTaskRequest) *taskIDParams {
	if req == nil {
		return nil
	}
	return &taskIDParams{
		ID: req.ID,
	}
}

func toCompatSendMessageRequest(req *a2a.SendMessageRequest) *messageSendParams {
	if req == nil {
		return nil
	}
	res := &messageSendParams{
		Message:  toCompatMessage(req.Message),
		Metadata: req.Metadata,
	}
	if req.Config != nil {
		res.Config = &messageSendConfig{
			AcceptedOutputModes: req.Config.AcceptedOutputModes,
			Blocking:            req.Config.Blocking,
			HistoryLength:       req.Config.HistoryLength,
		}
		if req.Config.PushConfig != nil {
			res.Config.PushConfig = toCompatPushConfig(req.Config.PushConfig)
		}
	}
	return res
}

func toCompatSubscribeToTaskRequest(req *a2a.SubscribeToTaskRequest) *taskIDParams {
	if req == nil {
		return nil
	}
	return &taskIDParams{
		ID: req.ID,
	}
}

func toCompatGetTaskPushConfigRequest(req *a2a.GetTaskPushConfigRequest) *getTaskPushConfigParams {
	if req == nil {
		return nil
	}
	return &getTaskPushConfigParams{
		TaskID:   req.TaskID,
		ConfigID: req.ID,
	}
}

func toCompatListTaskPushConfigRequest(req *a2a.ListTaskPushConfigRequest) *listTaskPushConfigParams {
	if req == nil {
		return nil
	}
	return &listTaskPushConfigParams{
		TaskID: req.TaskID,
	}
}

func toCompatCreateTaskPushConfigRequest(req *a2a.CreateTaskPushConfigRequest) *taskPushConfig {
	if req == nil {
		return nil
	}
	cfg := toCompatPushConfig(&req.Config)
	if cfg == nil {
		cfg = &pushConfig{}
	}
	return &taskPushConfig{TaskID: req.TaskID, Config: *cfg}
}

func toCompatDeleteTaskPushConfigRequest(req *a2a.DeleteTaskPushConfigRequest) *deleteTaskPushConfigParams {
	if req == nil {
		return nil
	}
	return &deleteTaskPushConfigParams{
		TaskID:   req.TaskID,
		ConfigID: req.ID,
	}
}

func toCoreGetTaskRequest(q *taskQueryParams) *a2a.GetTaskRequest {
	return &a2a.GetTaskRequest{
		ID:            q.ID,
		HistoryLength: q.HistoryLength,
	}
}

func toCoreCancelTaskRequest(q *taskIDParams) *a2a.CancelTaskRequest {
	return &a2a.CancelTaskRequest{
		ID: q.ID,
	}
}

func toCoreSendMessageRequest(p *messageSendParams) (*a2a.SendMessageRequest, error) {
	msg, err := toCoreMessage(p.Message)
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
			req.Config.PushConfig = toCorePushConfig(*p.Config.PushConfig)
		}
	}
	return req, nil
}

func toCoreSubscribeToTaskRequest(q *taskIDParams) *a2a.SubscribeToTaskRequest {
	return &a2a.SubscribeToTaskRequest{
		ID: q.ID,
	}
}

func toCoreGetTaskPushConfigRequest(p *getTaskPushConfigParams) *a2a.GetTaskPushConfigRequest {
	return &a2a.GetTaskPushConfigRequest{
		TaskID: p.TaskID,
		ID:     p.ConfigID,
	}
}

func toCoreListTaskPushConfigRequest(p *listTaskPushConfigParams) *a2a.ListTaskPushConfigRequest {
	return &a2a.ListTaskPushConfigRequest{
		TaskID: p.TaskID,
	}
}

func toCoreCreateTaskPushConfigRequest(p *taskPushConfig) *a2a.CreateTaskPushConfigRequest {
	return &a2a.CreateTaskPushConfigRequest{
		TaskID: p.TaskID,
		Config: *toCorePushConfig(p.Config),
	}
}

func toCoreDeleteTaskPushConfigRequest(p *deleteTaskPushConfigParams) *a2a.DeleteTaskPushConfigRequest {
	return &a2a.DeleteTaskPushConfigRequest{
		TaskID: p.TaskID,
		ID:     p.ConfigID,
	}
}

func toCorePushConfig(c pushConfig) *a2a.PushConfig {
	res := &a2a.PushConfig{
		ID:    c.ID,
		Token: c.Token,
		URL:   c.URL,
	}
	if c.Auth != nil {
		scheme := ""
		if len(c.Auth.Schemes) > 0 {
			scheme = c.Auth.Schemes[0]
		}
		res.Auth = &a2a.PushAuthInfo{
			Credentials: c.Auth.Credentials,
			Scheme:      scheme,
		}
	}
	return res
}

func toCoreMessage(m *message) (*a2a.Message, error) {
	if m == nil {
		return nil, nil
	}
	parts, err := toCoreParts(m.Parts)
	if err != nil {
		return nil, err
	}
	return &a2a.Message{
		ID:             m.ID,
		ContextID:      m.ContextID,
		Extensions:     m.Extensions,
		Metadata:       m.Metadata,
		Parts:          parts,
		ReferenceTasks: m.ReferenceTasks,
		Role:           m.Role,
		TaskID:         m.TaskID,
	}, nil
}

func toCoreParts(parts ContentParts) (a2a.ContentParts, error) {
	if len(parts) == 0 {
		return nil, nil
	}
	res := make(a2a.ContentParts, len(parts))
	for i, p := range parts {
		switch c := p.(type) {
		case textPart:
			res[i] = &a2a.Part{
				Content:  a2a.Text(c.Text),
				Metadata: c.Metadata,
			}
		case dataPart:
			res[i] = &a2a.Part{
				Content:  a2a.Data(c.Data),
				Metadata: c.Metadata,
			}
		case filePart:
			switch f := c.File.(type) {
			case fileBytes:
				bytes, err := base64.StdEncoding.DecodeString(f.Bytes)
				if err != nil {
					return nil, fmt.Errorf("failed to decode base64 content: %w", err)
				}
				res[i] = &a2a.Part{
					Content:   a2a.Raw(bytes),
					Metadata:  c.Metadata,
					MediaType: f.MimeType,
					Filename:  f.Name,
				}
			case fileURI:
				res[i] = &a2a.Part{
					Content:   a2a.URL(f.URI),
					Metadata:  c.Metadata,
					MediaType: f.MimeType,
					Filename:  f.Name,
				}
			}
		}
	}
	return res, nil
}

func fromCompatEvent(comp event) (a2a.Event, error) {
	if comp == nil {
		return nil, nil
	}
	switch v := comp.(type) {
	case *message:
		return toCoreMessage(v)
	case *task:
		return fromCompatTask(v)
	case *taskStatusUpdateEvent:
		return fromCompatTaskStatusUpdateEvent(v)
	case *taskArtifactUpdateEvent:
		return fromCompatTaskArtifactUpdateEvent(v)
	default:
		return nil, fmt.Errorf("unknown task event compat kind %T", comp)
	}
}

func fromCompatTask(t *task) (*a2a.Task, error) {
	if t == nil {
		return nil, nil
	}
	var msg *a2a.Message
	if t.Status.Message != nil {
		var err error
		msg, err = toCoreMessage(t.Status.Message)
		if err != nil {
			return nil, err
		}
	}
	res := &a2a.Task{
		ID:        t.ID,
		ContextID: t.ContextID,
		Metadata:  t.Metadata,
		Status: a2a.TaskStatus{
			Message:   msg,
			State:     fromCompatTaskState(t.Status.State),
			Timestamp: t.Status.Timestamp,
		},
	}
	if len(t.Artifacts) > 0 {
		res.Artifacts = make([]*a2a.Artifact, len(t.Artifacts))
		for i, a := range t.Artifacts {
			var err error
			res.Artifacts[i], err = fromCompatArtifact(a)
			if err != nil {
				return nil, err
			}
		}
	}
	if len(t.History) > 0 {
		res.History = make([]*a2a.Message, len(t.History))
		for i, m := range t.History {
			var err error
			res.History[i], err = toCoreMessage(m)
			if err != nil {
				return nil, err
			}
		}
	}
	return res, nil
}

func fromCompatArtifact(a *artifact) (*a2a.Artifact, error) {
	if a == nil {
		return nil, nil
	}
	parts, err := toCoreParts(a.Parts)
	if err != nil {
		return nil, err
	}
	return &a2a.Artifact{
		ID:          a.ID,
		Description: a.Description,
		Extensions:  a.Extensions,
		Metadata:    a.Metadata,
		Name:        a.Name,
		Parts:       parts,
	}, nil
}

func fromCompatTaskStatusUpdateEvent(e *taskStatusUpdateEvent) (*a2a.TaskStatusUpdateEvent, error) {
	if e == nil {
		return nil, nil
	}
	msg, err := toCoreMessage(e.Status.Message)
	if err != nil {
		return nil, err
	}
	return &a2a.TaskStatusUpdateEvent{
		ContextID: e.ContextID,
		Status: a2a.TaskStatus{
			Message:   msg,
			State:     fromCompatTaskState(e.Status.State),
			Timestamp: e.Status.Timestamp,
		},
		TaskID:   e.TaskID,
		Metadata: e.Metadata,
	}, nil
}

func fromCompatTaskArtifactUpdateEvent(e *taskArtifactUpdateEvent) (*a2a.TaskArtifactUpdateEvent, error) {
	if e == nil {
		return nil, nil
	}
	artifact, err := fromCompatArtifact(e.Artifact)
	if err != nil {
		return nil, err
	}
	return &a2a.TaskArtifactUpdateEvent{
		Append:    e.Append,
		Artifact:  artifact,
		ContextID: e.ContextID,
		LastChunk: e.LastChunk,
		TaskID:    e.TaskID,
		Metadata:  e.Metadata,
	}, nil
}

func fromCompatTaskPushConfig(comp *taskPushConfig) (*a2a.TaskPushConfig, error) {
	if comp == nil {
		return nil, nil
	}
	return &a2a.TaskPushConfig{
		TaskID: comp.TaskID,
		Config: *toCorePushConfig(comp.Config),
		ID:     comp.Config.ID,
	}, nil
}
