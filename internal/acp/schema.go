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

package acp

import (
	"encoding/json"
	"slices"
)

// ProtocolVersion is the ACP major version the client speaks.
const ProtocolVersion = 1

// Method names of ACP requests and notifications.
const (
	MethodInitialize        = "initialize"
	MethodSessionNew        = "session/new"
	MethodSessionLoad       = "session/load"
	MethodSessionPrompt     = "session/prompt"
	MethodSessionCancel     = "session/cancel"
	MethodSessionUpdate     = "session/update"
	MethodSetConfigOption   = "session/set_config_option"
	MethodSetMode           = "session/set_mode"
	MethodSetModel          = "session/set_model"
	MethodRequestPermission = "session/request_permission"
	MethodElicitationCreate = "elicitation/create"
)

// InitializeRequest negotiates the protocol version and capabilities.
type InitializeRequest struct {
	ProtocolVersion    int                `json:"protocolVersion"`
	ClientCapabilities ClientCapabilities `json:"clientCapabilities"`
	ClientInfo         *Implementation    `json:"clientInfo,omitempty"`
}

// Implementation identifies a client or an agent.
type Implementation struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ClientCapabilities tells the agent which client methods it may call.
type ClientCapabilities struct {
	FS          FSCapabilities          `json:"fs"`
	Terminal    bool                    `json:"terminal"`
	Elicitation ElicitationCapabilities `json:"elicitation"`
}

// FSCapabilities tells the agent whether it may read and write files through the client.
type FSCapabilities struct {
	ReadTextFile  bool `json:"readTextFile"`
	WriteTextFile bool `json:"writeTextFile"`
}

// ElicitationCapabilities advertises support for form elicitation.
type ElicitationCapabilities struct {
	Form struct{} `json:"form"`
}

// InitializeResponse is the agent's answer to [InitializeRequest].
type InitializeResponse struct {
	ProtocolVersion   int               `json:"protocolVersion"`
	AgentCapabilities AgentCapabilities `json:"agentCapabilities"`
}

// AgentCapabilities are the optional features of an agent.
type AgentCapabilities struct {
	LoadSession        bool               `json:"loadSession"`
	PromptCapabilities PromptCapabilities `json:"promptCapabilities"`
}

// PromptCapabilities are the content types an agent accepts in prompts besides text and resource links.
type PromptCapabilities struct {
	Image           bool `json:"image"`
	Audio           bool `json:"audio"`
	EmbeddedContext bool `json:"embeddedContext"`
}

// NewSessionRequest creates a session. MCPServers are in the JSON form of the ACP McpServer schema.
type NewSessionRequest struct {
	Cwd        string            `json:"cwd"`
	MCPServers []json.RawMessage `json:"mcpServers"`
}

// SessionState is what session/new and session/load report about the
// settings of a session. Only the presence of modes and models is decoded.
type SessionState struct {
	ConfigOptions []SessionConfigOption `json:"configOptions,omitempty"`
	Modes         *struct{}             `json:"modes,omitempty"`
	Models        *struct{}             `json:"models,omitempty"`
}

// NewSessionResponse identifies the created session.
type NewSessionResponse struct {
	SessionID string `json:"sessionId"`
	SessionState
}

// LoadSessionRequest restores a session created by an earlier agent process.
type LoadSessionRequest struct {
	SessionID  string            `json:"sessionId"`
	Cwd        string            `json:"cwd"`
	MCPServers []json.RawMessage `json:"mcpServers"`
}

// SessionConfigOption is a session setting the client can change.
type SessionConfigOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Type is "select" or "boolean".
	Type string `json:"type"`
}

// SetConfigOptionRequest changes a session setting. Type is "boolean" for boolean options.
type SetConfigOptionRequest struct {
	SessionID string `json:"sessionId"`
	ConfigID  string `json:"configId"`
	Type      string `json:"type,omitempty"`
	Value     any    `json:"value"`
}

// SetConfigOptionResponse lists the session settings after a change.
type SetConfigOptionResponse struct {
	ConfigOptions []SessionConfigOption `json:"configOptions"`
}

// SetModeRequest changes the mode of a session.
type SetModeRequest struct {
	SessionID string `json:"sessionId"`
	ModeID    string `json:"modeId"`
}

// SetModelRequest changes the model of a session.
type SetModelRequest struct {
	SessionID string `json:"sessionId"`
	ModelID   string `json:"modelId"`
}

// PromptRequest starts a turn.
type PromptRequest struct {
	SessionID string         `json:"sessionId"`
	Prompt    []ContentBlock `json:"prompt"`
}

// StopReason tells why a turn ended.
type StopReason string

// Stop reasons of a turn.
const (
	StopEndTurn         StopReason = "end_turn"
	StopMaxTokens       StopReason = "max_tokens"
	StopMaxTurnRequests StopReason = "max_turn_requests"
	StopRefusal         StopReason = "refusal"
	StopCancelled       StopReason = "cancelled"
)

// PromptResponse ends a turn.
type PromptResponse struct {
	StopReason StopReason `json:"stopReason"`
}

// CancelNotification asks the agent to stop the current turn of a session.
type CancelNotification struct {
	SessionID string `json:"sessionId"`
}

// ContentBlock is a piece of prompt or message content. Type is "text",
// "image", "audio", "resource_link" or "resource".
type ContentBlock struct {
	// Raw, if set, is sent instead of the other fields.
	Raw      json.RawMessage   `json:"-"`
	Type     string            `json:"type"`
	Text     string            `json:"text,omitempty"`
	Data     string            `json:"data,omitempty"`
	MimeType string            `json:"mimeType,omitempty"`
	URI      string            `json:"uri,omitempty"`
	Name     string            `json:"name,omitempty"`
	Resource *EmbeddedResource `json:"resource,omitempty"`
}

// MarshalJSON implements [json.Marshaler].
func (b ContentBlock) MarshalJSON() ([]byte, error) {
	if b.Raw != nil {
		return b.Raw, nil
	}
	type plain ContentBlock
	return json.Marshal(plain(b))
}

// EmbeddedResource is the content of a resource block, as Text or base64 Blob.
type EmbeddedResource struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
	Blob     string `json:"blob,omitempty"`
}

// SessionNotification reports progress of a session.
type SessionNotification struct {
	SessionID string        `json:"sessionId"`
	Update    SessionUpdate `json:"update"`
}

// Kinds of [SessionUpdate] the client handles.
const (
	UpdateAgentMessageChunk = "agent_message_chunk"
	UpdateToolCall          = "tool_call"
	UpdateToolCallUpdate    = "tool_call_update"
	UpdateConfigOption      = "config_option_update"
)

// SessionUpdate is a union of all session/update variants. Content holds a
// single content block for message chunks and a list for tool calls.
type SessionUpdate struct {
	// Raw is the update as the agent sent it, including fields this type
	// doesn't describe.
	Raw           json.RawMessage       `json:"-"`
	SessionUpdate string                `json:"sessionUpdate"`
	Content       json.RawMessage       `json:"content,omitempty"`
	MessageID     string                `json:"messageId,omitempty"`
	ConfigOptions []SessionConfigOption `json:"configOptions,omitempty"`
	ToolCall
}

// UnmarshalJSON implements [json.Unmarshaler].
func (u *SessionUpdate) UnmarshalJSON(data []byte) error {
	type plain SessionUpdate
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	*u = SessionUpdate(p)
	u.Raw = slices.Clone(data)
	return nil
}

// ToolCall describes a tool call. In updates and permission requests only the
// fields that changed may be set.
type ToolCall struct {
	ToolCallID string `json:"toolCallId,omitempty"`
	Title      string `json:"title,omitempty"`
	// Kind is read, edit, delete, move, search, execute, think, fetch or other.
	Kind     string `json:"kind,omitempty"`
	Status   string `json:"status,omitempty"`
	RawInput any    `json:"rawInput,omitempty"`
}

// Statuses of a tool call reported to the caller. Calls start as "pending".
const (
	ToolStatusInProgress = "in_progress"
	ToolStatusCompleted  = "completed"
	ToolStatusFailed     = "failed"
)

// RequestPermissionRequest asks the client to approve a tool call.
type RequestPermissionRequest struct {
	SessionID string             `json:"sessionId"`
	ToolCall  ToolCall           `json:"toolCall"`
	Options   []PermissionOption `json:"options"`
}

// PermissionOption is one of the answers to a permission request.
type PermissionOption struct {
	OptionID string `json:"optionId"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
}

// Kinds of [PermissionOption].
const (
	PermissionAllowOnce    = "allow_once"
	PermissionAllowAlways  = "allow_always"
	PermissionRejectOnce   = "reject_once"
	PermissionRejectAlways = "reject_always"
)

// RequestPermissionResponse answers a permission request.
type RequestPermissionResponse struct {
	Outcome PermissionOutcome `json:"outcome"`
}

// PermissionOutcome is a selected option, or the cancellation of the request.
type PermissionOutcome struct {
	Outcome  string `json:"outcome"`
	OptionID string `json:"optionId,omitempty"`
}

// Values of [PermissionOutcome.Outcome].
const (
	OutcomeSelected  = "selected"
	OutcomeCancelled = "cancelled"
)

// CreateElicitationRequest asks the user for input. Session-scoped requests
// carry SessionID; request-scoped ones carry RequestID instead.
type CreateElicitationRequest struct {
	// Mode is "form" or "url".
	Mode            string          `json:"mode"`
	Message         string          `json:"message"`
	SessionID       string          `json:"sessionId,omitempty"`
	ToolCallID      string          `json:"toolCallId,omitempty"`
	RequestID       json.RawMessage `json:"requestId,omitempty"`
	RequestedSchema map[string]any  `json:"requestedSchema,omitempty"`
}

// Actions of [CreateElicitationResponse].
const (
	ElicitationAccept  = "accept"
	ElicitationDecline = "decline"
	ElicitationCancel  = "cancel"
)

// CreateElicitationResponse answers an elicitation. Content holds the fields of an accepted form.
type CreateElicitationResponse struct {
	Action  string         `json:"action"`
	Content map[string]any `json:"content,omitempty"`
}
