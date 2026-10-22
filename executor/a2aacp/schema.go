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

package a2aacp

import (
	"encoding/json"
)

// The subset of the Agent Client Protocol v1 schema used by the executor.
// See https://agentclientprotocol.com/protocol/schema.

const protocolVersion = 1

const (
	methodInitialize        = "initialize"
	methodSessionNew        = "session/new"
	methodSessionLoad       = "session/load"
	methodSessionPrompt     = "session/prompt"
	methodSessionCancel     = "session/cancel"
	methodSessionUpdate     = "session/update"
	methodSetConfigOption   = "session/set_config_option"
	methodSetMode           = "session/set_mode"
	methodSetModel          = "session/set_model"
	methodRequestPermission = "session/request_permission"
	methodElicitationCreate = "elicitation/create"
)

type initializeRequest struct {
	ProtocolVersion    int                `json:"protocolVersion"`
	ClientCapabilities clientCapabilities `json:"clientCapabilities"`
	ClientInfo         *implementation    `json:"clientInfo,omitempty"`
}

type implementation struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type clientCapabilities struct {
	FS          fsCapabilities          `json:"fs"`
	Terminal    bool                    `json:"terminal"`
	Elicitation elicitationCapabilities `json:"elicitation"`
}

type fsCapabilities struct {
	ReadTextFile  bool `json:"readTextFile"`
	WriteTextFile bool `json:"writeTextFile"`
}

type elicitationCapabilities struct {
	Form struct{} `json:"form"`
}

type initializeResponse struct {
	ProtocolVersion   int               `json:"protocolVersion"`
	AgentCapabilities agentCapabilities `json:"agentCapabilities"`
}

type agentCapabilities struct {
	LoadSession        bool               `json:"loadSession"`
	PromptCapabilities promptCapabilities `json:"promptCapabilities"`
}

type promptCapabilities struct {
	Image           bool `json:"image"`
	Audio           bool `json:"audio"`
	EmbeddedContext bool `json:"embeddedContext"`
}

type newSessionRequest struct {
	Cwd        string            `json:"cwd"`
	MCPServers []json.RawMessage `json:"mcpServers"`
}

// sessionState is what session/new and session/load report about the
// settings of a session.
type sessionState struct {
	ConfigOptions []sessionConfigOption `json:"configOptions,omitempty"`
	Modes         *struct{}             `json:"modes,omitempty"`
	Models        *struct{}             `json:"models,omitempty"`
}

type newSessionResponse struct {
	SessionID string `json:"sessionId"`
	sessionState
}

type loadSessionRequest struct {
	SessionID  string            `json:"sessionId"`
	Cwd        string            `json:"cwd"`
	MCPServers []json.RawMessage `json:"mcpServers"`
}

type sessionConfigOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type setConfigOptionRequest struct {
	SessionID string `json:"sessionId"`
	ConfigID  string `json:"configId"`
	Type      string `json:"type,omitempty"`
	Value     any    `json:"value"`
}

type setConfigOptionResponse struct {
	ConfigOptions []sessionConfigOption `json:"configOptions"`
}

type setModeRequest struct {
	SessionID string `json:"sessionId"`
	ModeID    string `json:"modeId"`
}

type setModelRequest struct {
	SessionID string `json:"sessionId"`
	ModelID   string `json:"modelId"`
}

type promptRequest struct {
	SessionID string         `json:"sessionId"`
	Prompt    []contentBlock `json:"prompt"`
}

type stopReason string

const (
	stopEndTurn         stopReason = "end_turn"
	stopMaxTokens       stopReason = "max_tokens"
	stopMaxTurnRequests stopReason = "max_turn_requests"
	stopRefusal         stopReason = "refusal"
	stopCancelled       stopReason = "cancelled"
)

type promptResponse struct {
	StopReason stopReason `json:"stopReason"`
}

type cancelNotification struct {
	SessionID string `json:"sessionId"`
}

type contentBlock struct {
	Type     string            `json:"type"`
	Text     string            `json:"text,omitempty"`
	Data     string            `json:"data,omitempty"`
	MimeType string            `json:"mimeType,omitempty"`
	URI      string            `json:"uri,omitempty"`
	Name     string            `json:"name,omitempty"`
	Resource *embeddedResource `json:"resource,omitempty"`
}

type embeddedResource struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
	Blob     string `json:"blob,omitempty"`
}

type sessionNotification struct {
	SessionID string        `json:"sessionId"`
	Update    sessionUpdate `json:"update"`
}

const (
	updateAgentMessageChunk = "agent_message_chunk"
	updateToolCall          = "tool_call"
	updateToolCallUpdate    = "tool_call_update"
	updateConfigOption      = "config_option_update"
)

// sessionUpdate is a union of all session/update variants. Content holds a
// single content block for message chunks and a list for tool calls.
type sessionUpdate struct {
	SessionUpdate string                `json:"sessionUpdate"`
	Content       json.RawMessage       `json:"content,omitempty"`
	MessageID     string                `json:"messageId,omitempty"`
	ConfigOptions []sessionConfigOption `json:"configOptions,omitempty"`
	toolCall
}

type toolCall struct {
	ToolCallID string `json:"toolCallId,omitempty"`
	Title      string `json:"title,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Status     string `json:"status,omitempty"`
	RawInput   any    `json:"rawInput,omitempty"`
}

const (
	toolStatusInProgress = "in_progress"
	toolStatusCompleted  = "completed"
	toolStatusFailed     = "failed"
)

type requestPermissionRequest struct {
	SessionID string             `json:"sessionId"`
	ToolCall  toolCall           `json:"toolCall"`
	Options   []permissionOption `json:"options"`
}

type permissionOption struct {
	OptionID string `json:"optionId"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
}

const (
	permissionAllowOnce    = "allow_once"
	permissionAllowAlways  = "allow_always"
	permissionRejectOnce   = "reject_once"
	permissionRejectAlways = "reject_always"
)

type requestPermissionResponse struct {
	Outcome permissionOutcome `json:"outcome"`
}

type permissionOutcome struct {
	Outcome  string `json:"outcome"`
	OptionID string `json:"optionId,omitempty"`
}

// createElicitationRequest is a form elicitation. Session-scoped requests
// carry SessionID; request-scoped ones carry RequestID instead.
type createElicitationRequest struct {
	Mode            string          `json:"mode"`
	Message         string          `json:"message"`
	SessionID       string          `json:"sessionId,omitempty"`
	ToolCallID      string          `json:"toolCallId,omitempty"`
	RequestID       json.RawMessage `json:"requestId,omitempty"`
	RequestedSchema map[string]any  `json:"requestedSchema,omitempty"`
}

const (
	elicitationAccept  = "accept"
	elicitationDecline = "decline"
	elicitationCancel  = "cancel"
)

type createElicitationResponse struct {
	Action  string         `json:"action"`
	Content map[string]any `json:"content,omitempty"`
}
