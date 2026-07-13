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

// This file implements conversions between the v1 Go SDK types (a2a.*) and
// the A2A v0.3 REST wire format.
// The wire format is proto-JSON of the "a2a.v1" proto.
//
//   * SendMessageRequest.request       -> JSON key "message"
//   * SendMessageResponse.msg          -> JSON key "message"
//   * StreamResponse.msg               -> JSON key "message"
//   * TaskStatus.update                -> JSON key "message"
//   * Message.content (not Message.parts)
//   * Part is a oneof { text, file, data }
//     - file wraps FilePart{ fileWithUri|fileWithBytes, mimeType, name }
//   * MessageSendConfiguration.blocking is the inverse of returnImmediately
//   * MessageSendConfiguration.push_notification (PushNotificationConfig,
//     not wrapped in TaskPushNotificationConfig)
//   * TaskPushNotificationConfig has {name, pushNotificationConfig}
//   * Enums are stringified: Role "ROLE_USER"/"ROLE_AGENT",
//     TaskState "TASK_STATE_XXX"
//   * Bytes are base64-encoded
//   * Timestamps are RFC 3339 strings
//   * Push config lookups use resource names:
//       "tasks/{taskId}/pushNotificationConfigs/{configId}"

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

// ---- request marshalling ------------------------------------------------

func marshalRESTSendMessageRequest(req *a2a.SendMessageRequest) ([]byte, error) {
	if req == nil {
		return json.Marshal(map[string]any{})
	}
	out := map[string]any{}
	if req.Message != nil {
		out["message"] = messageToWire(req.Message)
	}
	if req.Config != nil {
		out["configuration"] = sendMessageConfigToWire(req.Config)
	}
	if len(req.Metadata) > 0 {
		out["metadata"] = req.Metadata
	}
	return json.Marshal(out)
}

// marshalRESTCreatePushConfigRequest encodes a v1 PushConfig as the request
// body posted to POST /v1/tasks/{id}/pushNotificationConfigs. The Python
// v0.3.24 server parses this body as a CreateTaskPushNotificationConfigRequest
// proto with fields {parent, configId, config}.
func marshalRESTCreatePushConfigRequest(pc *a2a.PushConfig) ([]byte, error) {
	if pc == nil {
		return json.Marshal(map[string]any{})
	}
	return json.Marshal(map[string]any{
		"parent":   "tasks/" + string(pc.TaskID),
		"configId": pc.ID,
		"config":   taskPushNotificationConfigToWire(pc),
	})
}

// ---- response / event unmarshalling -------------------------------------

// unmarshalRESTSendMessageResponse parses the on-message-send response body,
// which is proto-JSON of google.a2a.v1.SendMessageResponse (a oneof of
// {"message": Message} or {"task": Task}).
func unmarshalRESTSendMessageResponse(body []byte) (a2a.SendMessageResult, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to decode SendMessageResponse: %w", err)
	}
	if msgRaw, ok := raw["message"]; ok {
		msg, err := unmarshalMessage(msgRaw)
		if err != nil {
			return nil, err
		}
		return msg, nil
	}
	if taskRaw, ok := raw["task"]; ok {
		task, err := unmarshalTask(taskRaw)
		if err != nil {
			return nil, err
		}
		return task, nil
	}
	return nil, fmt.Errorf("SendMessageResponse has neither 'message' nor 'task' payload")
}

// unmarshalRESTStreamEvent parses one SSE event body of a v0.3 REST stream.
// It handles StreamResponse oneof of {message, task, statusUpdate, artifactUpdate}.
func unmarshalRESTStreamEvent(body []byte) (a2a.Event, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to decode StreamResponse: %w", err)
	}
	if msgRaw, ok := raw["message"]; ok {
		return unmarshalMessage(msgRaw)
	}
	if taskRaw, ok := raw["task"]; ok {
		return unmarshalTask(taskRaw)
	}
	if suRaw, ok := raw["statusUpdate"]; ok {
		return unmarshalStatusUpdate(suRaw)
	}
	if auRaw, ok := raw["artifactUpdate"]; ok {
		return unmarshalArtifactUpdate(auRaw)
	}
	return nil, fmt.Errorf("StreamResponse has no known payload key")
}

func unmarshalRESTTask(body []byte) (*a2a.Task, error) {
	return unmarshalTask(body)
}

func unmarshalRESTPushConfigResponse(body []byte) (*a2a.PushConfig, error) {
	return unmarshalTaskPushNotificationConfig(body)
}

func unmarshalTaskPushNotificationConfig(raw []byte) (*a2a.PushConfig, error) {
	var wire struct {
		Name                   string          `json:"name"`
		PushNotificationConfig json.RawMessage `json:"pushNotificationConfig"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("failed to decode TaskPushNotificationConfig: %w", err)
	}
	inner, err := unmarshalPushNotificationConfig(wire.PushNotificationConfig)
	if err != nil {
		return nil, err
	}
	taskID, _ := parsePushConfigResourceName(wire.Name)
	inner.TaskID = taskID
	return inner, nil
}

// unmarshalRESTListTasksResponse parses the response of GET /v1/tasks. The
// spec is silent on the exact shape; we emit and accept the AIP-158 style
// wrapper {"tasks": [...], "nextPageToken": ...}, and also accept a bare
// array for peers that follow the spec's implied shape from
// pushNotificationConfig/list.
func unmarshalRESTListTasksResponse(body []byte) (*a2a.ListTasksResponse, error) {
	trimmed := bytes.TrimLeft(body, " \t\r\n")
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var arr []json.RawMessage
		if err := json.Unmarshal(body, &arr); err != nil {
			return nil, fmt.Errorf("failed to decode ListTasks response array: %w", err)
		}
		return listTasksFromRawArray(arr, "")
	}
	var wire struct {
		Tasks         []json.RawMessage `json:"tasks"`
		NextPageToken string            `json:"nextPageToken"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("failed to decode ListTasks response: %w", err)
	}
	return listTasksFromRawArray(wire.Tasks, wire.NextPageToken)
}

func listTasksFromRawArray(raws []json.RawMessage, nextPageToken string) (*a2a.ListTasksResponse, error) {
	tasks := make([]*a2a.Task, 0, len(raws))
	for _, raw := range raws {
		t, err := unmarshalTask(raw)
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

// unmarshalRESTListPushConfigsResponse parses the response of
// GET /v1/tasks/{id}/pushNotificationConfigs.
func unmarshalRESTListPushConfigsResponse(body []byte, taskID a2a.TaskID) ([]*a2a.PushConfig, error) {
	trimmed := bytes.TrimLeft(body, " \t\r\n")
	var raws []json.RawMessage
	if len(trimmed) > 0 && trimmed[0] == '[' {
		if err := json.Unmarshal(body, &raws); err != nil {
			return nil, fmt.Errorf("failed to decode list push configs array: %w", err)
		}
	} else {
		var wire struct {
			Configs []json.RawMessage `json:"configs"`
		}
		if err := json.Unmarshal(body, &wire); err != nil {
			return nil, fmt.Errorf("failed to decode list push configs response: %w", err)
		}
		raws = wire.Configs
	}
	out := make([]*a2a.PushConfig, 0, len(raws))
	for _, raw := range raws {
		pc, err := unmarshalTaskPushNotificationConfig(raw)
		if err != nil {
			return nil, err
		}
		if pc.TaskID == "" {
			pc.TaskID = taskID
		}
		out = append(out, pc)
	}
	return out, nil
}

// ---- server-side response marshalling -----------------------------------

func marshalRESTTask(task *a2a.Task) ([]byte, error) {
	if task == nil {
		return json.Marshal(map[string]any{})
	}
	return json.Marshal(taskToWire(task))
}

func marshalRESTMessage(msg *a2a.Message) ([]byte, error) {
	if msg == nil {
		return json.Marshal(map[string]any{})
	}
	return json.Marshal(messageToWire(msg))
}

// marshalRESTSendMessageResult encodes either a Task or a Message as the
// on-message-send response body, wrapped in a SendMessageResponse envelope
// (either {"message": Message} or {"task": Task}).
func marshalRESTSendMessageResult(result a2a.SendMessageResult) ([]byte, error) {
	envelope := map[string]any{}
	switch v := result.(type) {
	case *a2a.Task:
		envelope["task"] = taskToWire(v)
	case *a2a.Message:
		envelope["message"] = messageToWire(v)
	default:
		return nil, fmt.Errorf("unsupported SendMessageResult type: %T", result)
	}
	return json.Marshal(envelope)
}

// marshalRESTStreamEvent encodes an Event as a StreamResponse envelope with
// exactly one of the oneof keys set.
func marshalRESTStreamEvent(event a2a.Event) ([]byte, error) {
	envelope := map[string]any{}
	switch v := event.(type) {
	case *a2a.Task:
		envelope["task"] = taskToWire(v)
	case *a2a.Message:
		envelope["message"] = messageToWire(v)
	case *a2a.TaskStatusUpdateEvent:
		envelope["statusUpdate"] = taskStatusUpdateEventToWire(v)
	case *a2a.TaskArtifactUpdateEvent:
		envelope["artifactUpdate"] = taskArtifactUpdateEventToWire(v)
	default:
		return nil, fmt.Errorf("unsupported Event type: %T", event)
	}
	return json.Marshal(envelope)
}

func marshalRESTPushConfigResponse(pc *a2a.PushConfig) ([]byte, error) {
	if pc == nil {
		return json.Marshal(map[string]any{})
	}
	return json.Marshal(taskPushNotificationConfigToWire(pc))
}

// marshalRESTListTasksResponse serializes a ListTasksResponse as the AIP-158
// wrapper {"tasks": [...], "nextPageToken": "..."}.
// Clients that expect a bare array (per the spec's implied shape for
// pushNotificationConfig/list) are handled by the client-side decoder.
func marshalRESTListTasksResponse(resp *a2a.ListTasksResponse) ([]byte, error) {
	if resp == nil {
		return json.Marshal(map[string]any{"tasks": []any{}})
	}
	tasks := make([]any, 0, len(resp.Tasks))
	for _, t := range resp.Tasks {
		tasks = append(tasks, taskToWire(t))
	}
	out := map[string]any{"tasks": tasks}
	if resp.NextPageToken != "" {
		out["nextPageToken"] = resp.NextPageToken
	}
	return json.Marshal(out)
}

// marshalRESTListPushConfigsResponse serializes a slice of PushConfig as the
// wrapped {"configs": [...]} shape.
func marshalRESTListPushConfigsResponse(configs []*a2a.PushConfig) ([]byte, error) {
	out := make([]any, 0, len(configs))
	for _, pc := range configs {
		out = append(out, taskPushNotificationConfigToWire(pc))
	}
	return json.Marshal(map[string]any{"configs": out})
}

// ---- server-side request unmarshalling ----------------------------------

// unmarshalRESTSendMessageRequest parses a request body posted by a v0.3
// REST client to /v1/message:send or /v1/message:stream and returns a v1
// SendMessageRequest.
func unmarshalRESTSendMessageRequest(body []byte) (*a2a.SendMessageRequest, error) {
	var raw struct {
		Message       json.RawMessage `json:"message"`
		Configuration json.RawMessage `json:"configuration"`
		Metadata      map[string]any  `json:"metadata"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to decode SendMessageRequest: %w", err)
	}
	req := &a2a.SendMessageRequest{Metadata: raw.Metadata}
	if len(raw.Message) > 0 {
		msg, err := unmarshalMessage(raw.Message)
		if err != nil {
			return nil, err
		}
		req.Message = msg
	}
	if len(raw.Configuration) > 0 {
		cfg, err := unmarshalSendMessageConfig(raw.Configuration)
		if err != nil {
			return nil, err
		}
		req.Config = cfg
	}
	return req, nil
}

// unmarshalRESTCreatePushConfigRequest parses a request body posted by a
// v0.3 REST client to /v1/tasks/{id}/pushNotificationConfigs. The body is
// a CreateTaskPushNotificationConfigRequest with {parent, configId, config}.
// The returned PushConfig has taskID set from the path parameter.
func unmarshalRESTCreatePushConfigRequest(body []byte, taskID a2a.TaskID) (*a2a.PushConfig, error) {
	var raw struct {
		Parent   string          `json:"parent"`
		ConfigID string          `json:"configId"`
		Config   json.RawMessage `json:"config"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to decode CreateTaskPushNotificationConfigRequest: %w", err)
	}
	if len(raw.Config) == 0 {
		return nil, fmt.Errorf("CreateTaskPushNotificationConfigRequest missing 'config'")
	}
	var cfg struct {
		Name                   string          `json:"name"`
		PushNotificationConfig json.RawMessage `json:"pushNotificationConfig"`
	}
	if err := json.Unmarshal(raw.Config, &cfg); err != nil {
		return nil, fmt.Errorf("failed to decode TaskPushNotificationConfig: %w", err)
	}
	inner, err := unmarshalPushNotificationConfig(cfg.PushNotificationConfig)
	if err != nil {
		return nil, err
	}
	inner.TaskID = taskID
	if inner.ID == "" && raw.ConfigID != "" {
		inner.ID = raw.ConfigID
	}
	return inner, nil
}

// ---- core encoders ------------------------------------------------------

func messageToWire(msg *a2a.Message) map[string]any {
	out := map[string]any{"messageId": msg.ID}
	if msg.ContextID != "" {
		out["contextId"] = msg.ContextID
	}
	if msg.TaskID != "" {
		out["taskId"] = string(msg.TaskID)
	}
	if msg.Role != a2a.MessageRoleUnspecified {
		out["role"] = string(msg.Role)
	} else {
		out["role"] = "ROLE_UNSPECIFIED"
	}
	if len(msg.Parts) > 0 {
		parts := make([]any, 0, len(msg.Parts))
		for _, p := range msg.Parts {
			parts = append(parts, partToWire(p))
		}
		out["content"] = parts
	}
	if len(msg.Metadata) > 0 {
		out["metadata"] = msg.Metadata
	}
	if len(msg.Extensions) > 0 {
		out["extensions"] = stringSliceToAny(msg.Extensions)
	}
	return out
}

func partToWire(p *a2a.Part) map[string]any {
	if p == nil {
		return map[string]any{}
	}
	out := map[string]any{}
	switch c := p.Content.(type) {
	case a2a.Text:
		out["text"] = string(c)
	case a2a.Raw:
		inner := base64.StdEncoding.EncodeToString(c)
		out["file"] = map[string]any{
			"fileWithBytes": base64.StdEncoding.EncodeToString([]byte(inner)),
			"mimeType":      p.MediaType,
			"name":          p.Filename,
		}
	case a2a.URL:
		out["file"] = map[string]any{
			"fileWithUri": string(c),
			"mimeType":    p.MediaType,
			"name":        p.Filename,
		}
	case a2a.Data:
		out["data"] = map[string]any{"data": c.Value}
	}
	if len(p.Metadata) > 0 {
		out["metadata"] = p.Metadata
	}
	return out
}

func taskToWire(task *a2a.Task) map[string]any {
	out := map[string]any{}
	if task.ID != "" {
		out["id"] = string(task.ID)
	}
	if task.ContextID != "" {
		out["contextId"] = task.ContextID
	}
	out["status"] = taskStatusToWire(task.Status)
	if len(task.Artifacts) > 0 {
		arts := make([]any, 0, len(task.Artifacts))
		for _, a := range task.Artifacts {
			arts = append(arts, artifactToWire(a))
		}
		out["artifacts"] = arts
	}
	if len(task.History) > 0 {
		hist := make([]any, 0, len(task.History))
		for _, m := range task.History {
			hist = append(hist, messageToWire(m))
		}
		out["history"] = hist
	}
	if len(task.Metadata) > 0 {
		out["metadata"] = task.Metadata
	}
	return out
}

func taskStatusToWire(s a2a.TaskStatus) map[string]any {
	out := map[string]any{"state": encodeTaskState(s.State)}
	if s.Message != nil {
		out["message"] = messageToWire(s.Message)
	}
	if s.Timestamp != nil {
		out["timestamp"] = s.Timestamp.UTC().Format(time.RFC3339Nano)
	}
	return out
}

// encodeTaskState renders a v2 SDK TaskState using the enum name.
// The single spelling difference is TASK_STATE_CANCELED vs TASK_STATE_CANCELLED
func encodeTaskState(s a2a.TaskState) string {
	if s == a2a.TaskStateCanceled {
		return "TASK_STATE_CANCELLED"
	}
	return s.String()
}

func artifactToWire(a *a2a.Artifact) map[string]any {
	out := map[string]any{}
	if a.ID != "" {
		out["artifactId"] = string(a.ID)
	}
	if a.Name != "" {
		out["name"] = a.Name
	}
	if a.Description != "" {
		out["description"] = a.Description
	}
	if len(a.Parts) > 0 {
		parts := make([]any, 0, len(a.Parts))
		for _, p := range a.Parts {
			parts = append(parts, partToWire(p))
		}
		out["parts"] = parts
	}
	if len(a.Metadata) > 0 {
		out["metadata"] = a.Metadata
	}
	if len(a.Extensions) > 0 {
		out["extensions"] = stringSliceToAny(a.Extensions)
	}
	return out
}

func taskStatusUpdateEventToWire(e *a2a.TaskStatusUpdateEvent) map[string]any {
	// The v2 Go SDK does not carry the "final" flag on TaskStatusUpdateEvent;
	// derive it from the state (terminal states are always final).
	out := map[string]any{
		"taskId":    string(e.TaskID),
		"contextId": e.ContextID,
		"status":    taskStatusToWire(e.Status),
		"final":     e.Status.State.Terminal(),
	}
	if len(e.Metadata) > 0 {
		out["metadata"] = e.Metadata
	}
	return out
}

func taskArtifactUpdateEventToWire(e *a2a.TaskArtifactUpdateEvent) map[string]any {
	out := map[string]any{
		"taskId":    string(e.TaskID),
		"contextId": e.ContextID,
	}
	if e.Artifact != nil {
		out["artifact"] = artifactToWire(e.Artifact)
	}
	if e.Append {
		out["append"] = true
	}
	if e.LastChunk {
		out["lastChunk"] = true
	}
	if len(e.Metadata) > 0 {
		out["metadata"] = e.Metadata
	}
	return out
}

func sendMessageConfigToWire(cfg *a2a.SendMessageConfig) map[string]any {
	out := map[string]any{}
	if len(cfg.AcceptedOutputModes) > 0 {
		out["acceptedOutputModes"] = stringSliceToAny(cfg.AcceptedOutputModes)
	}
	// The v0.3 proto uses "blocking" (positive: wait for completion), while
	// v1 SDK uses ReturnImmediately (negative). Emit both meanings faithfully:
	// blocking == !ReturnImmediately.
	out["blocking"] = !cfg.ReturnImmediately
	if cfg.HistoryLength != nil {
		out["historyLength"] = *cfg.HistoryLength
	}
	if cfg.PushConfig != nil {
		out["pushNotification"] = pushNotificationConfigToWire(cfg.PushConfig)
	}
	return out
}

func pushNotificationConfigToWire(pc *a2a.PushConfig) map[string]any {
	out := map[string]any{}
	if pc.ID != "" {
		out["id"] = pc.ID
	}
	if pc.URL != "" {
		out["url"] = pc.URL
	}
	if pc.Token != "" {
		out["token"] = pc.Token
	}
	if pc.Auth != nil {
		auth := map[string]any{}
		if pc.Auth.Scheme != "" {
			auth["schemes"] = []any{pc.Auth.Scheme}
		}
		if pc.Auth.Credentials != "" {
			auth["credentials"] = pc.Auth.Credentials
		}
		out["authentication"] = auth
	}
	return out
}

// taskPushNotificationConfigToWire produces the proto-JSON representation
// of TaskPushNotificationConfig{name, pushNotificationConfig}.
func taskPushNotificationConfigToWire(pc *a2a.PushConfig) map[string]any {
	return map[string]any{
		"name":                   formatPushConfigResourceName(pc.TaskID, pc.ID),
		"pushNotificationConfig": pushNotificationConfigToWire(pc),
	}
}

// ---- core decoders ------------------------------------------------------

func unmarshalMessage(raw json.RawMessage) (*a2a.Message, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var wire struct {
		MessageID  string            `json:"messageId"`
		ContextID  string            `json:"contextId"`
		TaskID     string            `json:"taskId"`
		Role       string            `json:"role"`
		Content    []json.RawMessage `json:"content"`
		Metadata   map[string]any    `json:"metadata"`
		Extensions []string          `json:"extensions"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("failed to decode Message: %w", err)
	}
	msg := &a2a.Message{
		ID:         wire.MessageID,
		ContextID:  wire.ContextID,
		TaskID:     a2a.TaskID(wire.TaskID),
		Role:       decodeRole(wire.Role),
		Metadata:   wire.Metadata,
		Extensions: wire.Extensions,
	}
	if len(wire.Content) > 0 {
		parts := make(a2a.ContentParts, 0, len(wire.Content))
		for _, raw := range wire.Content {
			p, err := unmarshalPart(raw)
			if err != nil {
				return nil, err
			}
			parts = append(parts, p)
		}
		msg.Parts = parts
	}
	return msg, nil
}

func unmarshalPart(raw json.RawMessage) (*a2a.Part, error) {
	var wire struct {
		Text     *string         `json:"text"`
		File     json.RawMessage `json:"file"`
		Data     json.RawMessage `json:"data"`
		Metadata map[string]any  `json:"metadata"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("failed to decode Part: %w", err)
	}
	part := &a2a.Part{Metadata: wire.Metadata}
	switch {
	case wire.Text != nil:
		part.Content = a2a.Text(*wire.Text)
	case len(wire.File) > 0 && string(wire.File) != "null":
		var f struct {
			FileWithURI   string `json:"fileWithUri"`
			FileWithBytes string `json:"fileWithBytes"`
			MimeType      string `json:"mimeType"`
			Name          string `json:"name"`
		}
		if err := json.Unmarshal(wire.File, &f); err != nil {
			return nil, fmt.Errorf("failed to decode FilePart: %w", err)
		}
		part.MediaType = f.MimeType
		part.Filename = f.Name
		if f.FileWithURI != "" {
			part.Content = a2a.URL(f.FileWithURI)
		} else if f.FileWithBytes != "" {
			// Reverse the double base64 encoding (see partToWire for
			// a2a.Raw).
			outer, err := base64.StdEncoding.DecodeString(f.FileWithBytes)
			if err != nil {
				return nil, fmt.Errorf("failed to decode fileWithBytes (outer): %w", err)
			}
			inner, err := base64.StdEncoding.DecodeString(string(outer))
			if err != nil {
				return nil, fmt.Errorf("failed to decode fileWithBytes (inner): %w", err)
			}
			part.Content = a2a.Raw(inner)
		}
	case len(wire.Data) > 0 && string(wire.Data) != "null":
		var d struct {
			Data any `json:"data"`
		}
		if err := json.Unmarshal(wire.Data, &d); err != nil {
			return nil, fmt.Errorf("failed to decode DataPart: %w", err)
		}
		part.Content = a2a.Data{Value: d.Data}
	default:
		return nil, fmt.Errorf("Part has no known oneof key")
	}
	return part, nil
}

func unmarshalTask(raw json.RawMessage) (*a2a.Task, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var wire struct {
		ID        string            `json:"id"`
		ContextID string            `json:"contextId"`
		Status    json.RawMessage   `json:"status"`
		Artifacts []json.RawMessage `json:"artifacts"`
		History   []json.RawMessage `json:"history"`
		Metadata  map[string]any    `json:"metadata"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("failed to decode Task: %w", err)
	}
	task := &a2a.Task{
		ID:        a2a.TaskID(wire.ID),
		ContextID: wire.ContextID,
		Metadata:  wire.Metadata,
	}
	if len(wire.Status) > 0 {
		status, err := unmarshalTaskStatus(wire.Status)
		if err != nil {
			return nil, err
		}
		task.Status = status
	}
	for _, rawA := range wire.Artifacts {
		a, err := unmarshalArtifact(rawA)
		if err != nil {
			return nil, err
		}
		task.Artifacts = append(task.Artifacts, a)
	}
	for _, rawM := range wire.History {
		m, err := unmarshalMessage(rawM)
		if err != nil {
			return nil, err
		}
		task.History = append(task.History, m)
	}
	return task, nil
}

func unmarshalTaskStatus(raw json.RawMessage) (a2a.TaskStatus, error) {
	var wire struct {
		State     string          `json:"state"`
		Message   json.RawMessage `json:"message"`
		Timestamp string          `json:"timestamp"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return a2a.TaskStatus{}, fmt.Errorf("failed to decode TaskStatus: %w", err)
	}
	status := a2a.TaskStatus{State: decodeTaskState(wire.State)}
	if len(wire.Message) > 0 && string(wire.Message) != "null" {
		m, err := unmarshalMessage(wire.Message)
		if err != nil {
			return a2a.TaskStatus{}, err
		}
		status.Message = m
	}
	if wire.Timestamp != "" {
		ts, err := time.Parse(time.RFC3339Nano, wire.Timestamp)
		if err != nil {
			return a2a.TaskStatus{}, fmt.Errorf("failed to parse timestamp %q: %w", wire.Timestamp, err)
		}
		status.Timestamp = &ts
	}
	return status, nil
}

func unmarshalArtifact(raw json.RawMessage) (*a2a.Artifact, error) {
	var wire struct {
		ArtifactID  string            `json:"artifactId"`
		Name        string            `json:"name"`
		Description string            `json:"description"`
		Parts       []json.RawMessage `json:"parts"`
		Metadata    map[string]any    `json:"metadata"`
		Extensions  []string          `json:"extensions"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("failed to decode Artifact: %w", err)
	}
	art := &a2a.Artifact{
		ID:          a2a.ArtifactID(wire.ArtifactID),
		Name:        wire.Name,
		Description: wire.Description,
		Metadata:    wire.Metadata,
		Extensions:  wire.Extensions,
	}
	for _, rawP := range wire.Parts {
		p, err := unmarshalPart(rawP)
		if err != nil {
			return nil, err
		}
		art.Parts = append(art.Parts, p)
	}
	return art, nil
}

func unmarshalStatusUpdate(raw json.RawMessage) (*a2a.TaskStatusUpdateEvent, error) {
	// The wire "final" flag is discarded because v2 SDK infers finality from
	// the terminal-state predicate on TaskState.
	var wire struct {
		TaskID    string          `json:"taskId"`
		ContextID string          `json:"contextId"`
		Status    json.RawMessage `json:"status"`
		Metadata  map[string]any  `json:"metadata"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("failed to decode TaskStatusUpdateEvent: %w", err)
	}
	evt := &a2a.TaskStatusUpdateEvent{
		TaskID:    a2a.TaskID(wire.TaskID),
		ContextID: wire.ContextID,
		Metadata:  wire.Metadata,
	}
	if len(wire.Status) > 0 {
		s, err := unmarshalTaskStatus(wire.Status)
		if err != nil {
			return nil, err
		}
		evt.Status = s
	}
	return evt, nil
}

func unmarshalArtifactUpdate(raw json.RawMessage) (*a2a.TaskArtifactUpdateEvent, error) {
	var wire struct {
		TaskID    string          `json:"taskId"`
		ContextID string          `json:"contextId"`
		Artifact  json.RawMessage `json:"artifact"`
		Append    bool            `json:"append"`
		LastChunk bool            `json:"lastChunk"`
		Metadata  map[string]any  `json:"metadata"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("failed to decode TaskArtifactUpdateEvent: %w", err)
	}
	evt := &a2a.TaskArtifactUpdateEvent{
		TaskID:    a2a.TaskID(wire.TaskID),
		ContextID: wire.ContextID,
		Append:    wire.Append,
		LastChunk: wire.LastChunk,
		Metadata:  wire.Metadata,
	}
	if len(wire.Artifact) > 0 && string(wire.Artifact) != "null" {
		a, err := unmarshalArtifact(wire.Artifact)
		if err != nil {
			return nil, err
		}
		evt.Artifact = a
	}
	return evt, nil
}

func unmarshalSendMessageConfig(raw json.RawMessage) (*a2a.SendMessageConfig, error) {
	var wire struct {
		AcceptedOutputModes []string        `json:"acceptedOutputModes"`
		Blocking            *bool           `json:"blocking"`
		HistoryLength       *int            `json:"historyLength"`
		PushNotification    json.RawMessage `json:"pushNotification"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("failed to decode SendMessageConfiguration: %w", err)
	}
	cfg := &a2a.SendMessageConfig{
		AcceptedOutputModes: wire.AcceptedOutputModes,
		HistoryLength:       wire.HistoryLength,
	}
	if wire.Blocking != nil {
		cfg.ReturnImmediately = !*wire.Blocking
	}
	if len(wire.PushNotification) > 0 && string(wire.PushNotification) != "null" {
		pc, err := unmarshalPushNotificationConfig(wire.PushNotification)
		if err != nil {
			return nil, err
		}
		cfg.PushConfig = pc
	}
	return cfg, nil
}

func unmarshalPushNotificationConfig(raw json.RawMessage) (*a2a.PushConfig, error) {
	if len(raw) == 0 {
		return &a2a.PushConfig{}, nil
	}
	var wire struct {
		ID             string `json:"id"`
		URL            string `json:"url"`
		Token          string `json:"token"`
		Authentication *struct {
			Schemes     []string `json:"schemes"`
			Credentials string   `json:"credentials"`
		} `json:"authentication"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("failed to decode PushNotificationConfig: %w", err)
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
	return pc, nil
}

// ---- misc helpers -------------------------------------------------------

func decodeRole(s string) a2a.MessageRole {
	switch s {
	case "ROLE_USER":
		return a2a.MessageRoleUser
	case "ROLE_AGENT":
		return a2a.MessageRoleAgent
	default:
		return a2a.MessageRoleUnspecified
	}
}

func decodeTaskState(s string) a2a.TaskState {
	switch s {
	case "", "TASK_STATE_UNSPECIFIED":
		return a2a.TaskStateUnspecified
	case "TASK_STATE_CANCELLED":
		return a2a.TaskStateCanceled
	default:
		return a2a.TaskState(s)
	}
}

func stringSliceToAny(in []string) []any {
	out := make([]any, len(in))
	for i, s := range in {
		out[i] = s
	}
	return out
}

func formatPushConfigResourceName(taskID a2a.TaskID, configID string) string {
	return "tasks/" + string(taskID) + "/pushNotificationConfigs/" + configID
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
