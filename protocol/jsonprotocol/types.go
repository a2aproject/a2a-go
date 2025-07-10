package jsonprotocol

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// TaskState represents the lifecycle state of a task.
// See A2A Spec section on Task Lifecycle.
type TaskState string

// TaskState constants define the possible states of a task.
const (
	// TaskStateSubmitted is the state when the task is received but not yet processed.
	TaskStateSubmitted TaskState = "submitted"
	// TaskStateWorking is the state when the task is actively being processed.
	TaskStateWorking TaskState = "working"
	// TaskStateInputRequired is the state when the task requires additional input (semantics evolving in spec).
	TaskStateInputRequired TaskState = "input-required"
	// TaskStateCompleted is the state when the task finished successfully.
	TaskStateCompleted TaskState = "completed"
	// TaskStateCanceled is the state when the task was canceled before completion.
	TaskStateCanceled TaskState = "canceled"
	// TaskStateFailed is the state when the task failed during processing.
	TaskStateFailed TaskState = "failed"
	// TaskStateRejected is the state when the task was rejected by the agent.
	TaskStateRejected TaskState = "rejected"
	// TaskStateAuthRequired is the state when the task requires authentication before processing.
	TaskStateAuthRequired TaskState = "auth-required"
	// TaskStateUnknown is the state when the task is in an unknown or indeterminate state.
	TaskStateUnknown TaskState = "unknown"
)

// Event is an interface that represents the kind of the struct.
type Event interface {
	GetKind() string
}

// GetKind returns the kind of the result.
func (m *Message) GetKind() string { return KindMessage }

// GetKind returns the kind of the task.
func (r *Task) GetKind() string { return KindTask }

// GetKind returns the kind of the task status update event.
func (r *TaskStatusUpdateEvent) GetKind() string { return KindTaskStatusUpdate }

// GetKind returns the kind of the task artifact update event.
func (r *TaskArtifactUpdateEvent) GetKind() string { return KindTaskArtifactUpdate }

// GenerateMessageID generates a new unique message ID.
func GenerateMessageID() string {
	id := uuid.New()
	return "msg-" + id.String()
}

// GenerateContextID generates a new unique context ID for a task.
func GenerateContextID() string {
	id := uuid.New()
	return "ctx-" + id.String()
}

// GenerateTaskID generates a new unique task ID.
func GenerateTaskID() string {
	id := uuid.New()
	return "task-" + id.String()
}

// GenerateArtifactID generates a new unique artifact ID.
func GenerateArtifactID() string {
	id := uuid.New()
	return "artifact-" + id.String()
}

// GenerateRPCID generates a new unique RPC ID.
func GenerateRPCID() string {
	id := uuid.New()
	return id.String()
}

// UnaryMessageResult is an interface representing a result of SendMessage.
// It only supports Message or Task.
type UnaryMessageResult interface {
	unaryMessageResultMarker()
	GetKind() string
}

func (Message) unaryMessageResultMarker() {}
func (Task) unaryMessageResultMarker()    {}

// Part is an interface representing a segment of a message (text, file, or data).
// It uses an unexported method to ensure only defined part types implement it.
// See A2A Spec section on Message Parts.
// Exported interface.
type Part interface {
	partMarker() // Internal marker method.
	GetKind() string
}

func (TextPart) partMarker() {}
func (FilePart) partMarker() {}
func (DataPart) partMarker() {}

// StreamingMessageResult is an interface representing a result of SendMessageStream.
type StreamingMessageResult interface {
	streamingMessageResultMarker()
	GetKind() string
}

func (Message) streamingMessageResultMarker()                 {}
func (Task) streamingMessageResultMarker()                    {}
func (TaskStatusUpdateEvent) streamingMessageResultMarker()   {}
func (TaskArtifactUpdateEvent) streamingMessageResultMarker() {}

// Kind constants define the possible kinds of the struct.
const (
	// KindMessage is the kind of the message.
	KindMessage = "message"
	// KindTask is the kind of the task.
	KindTask = "task"
	// KindTaskStatusUpdate is the kind of the task status update event.
	KindTaskStatusUpdate = "status-update"
	// KindTaskArtifactUpdate is the kind of the task artifact update event.
	KindTaskArtifactUpdate = "artifact-update"
	// KindData is the kind of the data.
	KindData = "data"
	// KindFile is the kind of the file.
	KindFile = "file"
	// KindText is the kind of the text.
	KindText = "text"
)

// MessageRole indicates the originator of a message (user or agent).
// See A2A Spec section on Messages.
type MessageRole string

// MessageRole constants define the possible roles for a message sender.
const (
	// MessageRoleUser is the role of a message originated from the user/client.
	MessageRoleUser MessageRole = "user"
	// MessageRoleAgent is the role of a message originated from the agent/server.
	MessageRoleAgent MessageRole = "agent"
)

// FileUnion represents the union type for file content.
// It contains either FileWithBytes or FileWithURI.
type FileUnion interface {
	fileUnionMarker()
}

func (f *FileWithBytes) fileUnionMarker() {}
func (f *FileWithURI) fileUnionMarker()   {}

// TextPart represents a text segment within a message.
// Corresponds to the 'text' part type in A2A Message Parts.
type TextPart struct {
	// Kind is the type of the part.
	Kind string `json:"kind"`
	// Text is the text content.
	Text string `json:"text"`
	// Metadata is the optional metadata.
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// GetKind returns the kind of the part.
func (t TextPart) GetKind() string {
	return KindText
}

// FilePart represents a file included in a message.
// Corresponds to the 'file' part type in A2A Message Parts.
type FilePart struct {
	// Kind is the type of the part.
	Kind string `json:"kind"`
	// File is the file content.
	File FileUnion `json:"file"`
	// Metadata is the optional metadata.
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// GetKind returns the kind of the part.
func (f FilePart) GetKind() string {
	return KindFile
}

// UnmarshalJSON implements custom unmarshalling logic for FilePart
// to handle the polymorphic FileUnion interface.
func (f *FilePart) UnmarshalJSON(data []byte) error {
	type Alias FilePart // Alias to avoid recursion.
	temp := &struct {
		File json.RawMessage `json:"file"` // Unmarshal file into RawMessage first.
		*Alias
	}{
		Alias: (*Alias)(f),
	}
	if err := json.Unmarshal(data, &temp); err != nil {
		return fmt.Errorf("failed to unmarshal file part base: %w", err)
	}

	// Now determine the concrete type of FileUnion based on fields present
	var fileContent map[string]interface{}
	if err := json.Unmarshal(temp.File, &fileContent); err != nil {
		return fmt.Errorf("failed to unmarshal file content: %w", err)
	}

	// Check if it has "bytes" field (FileWithBytes) or "uri" field (FileWithURI)
	if _, hasBytes := fileContent["bytes"]; hasBytes {
		var fileWithBytes FileWithBytes
		if err := json.Unmarshal(temp.File, &fileWithBytes); err != nil {
			return fmt.Errorf("failed to unmarshal FileWithBytes: %w", err)
		}
		f.File = &fileWithBytes
	} else if _, hasURI := fileContent["uri"]; hasURI {
		var fileWithURI FileWithURI
		if err := json.Unmarshal(temp.File, &fileWithURI); err != nil {
			return fmt.Errorf("failed to unmarshal FileWithURI: %w", err)
		}
		f.File = &fileWithURI
	} else {
		return fmt.Errorf("unknown file type: must have either 'bytes' or 'uri' field")
	}

	return nil
}

// DataPart represents arbitrary structured data (JSON) within a message.
// Corresponds to the 'data' part type in A2A Message Parts.
type DataPart struct {
	// Kind is the type of the part.
	Kind string `json:"kind"`
	// Data is the actual data payload.
	Data interface{} `json:"data"`
	// Metadata is the optional metadata.
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// GetKind returns the kind of the part.
func (d DataPart) GetKind() string {
	return KindData
}

// FileWithBytes represents file data with embedded content.
// This is one variant of the File union type in A2A 0.2.2.
type FileWithBytes struct {
	// Name is the optional filename.
	Name *string `json:"name,omitempty"`
	// MimeType is the optional MIME type.
	MimeType *string `json:"mimeType,omitempty"`
	// Bytes is the required base64-encoded content.
	Bytes string `json:"bytes"`
}

// FileWithURI represents file data with URI reference.
// This is one variant of the File union type in A2A 0.2.2.
type FileWithURI struct {
	// Name is the optional filename.
	Name *string `json:"name,omitempty"`
	// MimeType is the optional MIME type.
	MimeType *string `json:"mimeType,omitempty"`
	// URI is the required URI pointing to the content.
	URI string `json:"uri"`
}

// PushNotificationAuthenticationInfo represents authentication details for external services.
// Renamed from AuthenticationInfo for A2A 0.2.2 specification compliance.
type PushNotificationAuthenticationInfo struct {
	// Credentials are the actual authentication credentials.
	Credentials *string `json:"credentials,omitempty"`
	// Schemes is a list of authentication schemes supported.
	Schemes []string `json:"schemes"`
}

// OAuth2AuthInfo contains OAuth2-specific authentication details.
type OAuth2AuthInfo struct {
	// ClientID is the OAuth2 client ID.
	ClientID string `json:"clientId,omitempty"`
	// ClientSecret is the OAuth2 client secret.
	ClientSecret string `json:"clientSecret,omitempty"`
	// TokenURL is the OAuth2 token endpoint.
	TokenURL string `json:"tokenUrl,omitempty"`
	// AuthURL is the OAuth2 authorization endpoint (for authorization code flow).
	AuthURL string `json:"authUrl,omitempty"`
	// Scopes is a list of OAuth2 scopes to request.
	Scopes []string `json:"scopes,omitempty"`
	// RefreshToken is the OAuth2 refresh token.
	RefreshToken string `json:"refreshToken,omitempty"`
	// AccessToken is the OAuth2 access token.
	AccessToken string `json:"accessToken,omitempty"`
}

// JWTAuthInfo contains JWT-specific authentication details.
type JWTAuthInfo struct {
	// Token is a pre-generated JWT token.
	Token string `json:"token,omitempty"`
	// KeyID is the ID of the key used to sign the JWT.
	KeyID string `json:"keyId,omitempty"`
	// JWKSURL is the URL of the JWKS endpoint for key discovery.
	JWKSURL string `json:"jwksUrl,omitempty"`
	// Audience is the expected audience claim.
	Audience string `json:"audience,omitempty"`
	// Issuer is the expected issuer claim.
	Issuer string `json:"issuer,omitempty"`
}

// APIKeyAuthInfo contains API key-specific authentication details.
type APIKeyAuthInfo struct {
	// Key is the API key value.
	Key string `json:"key,omitempty"`
	// HeaderName is the name of the header to include the API key in.
	HeaderName string `json:"headerName,omitempty"`
	// ParamName is the name of the query parameter to include the API key in.
	ParamName string `json:"paramName,omitempty"`
	// Location specifies where to place the API key (header, query, or cookie).
	Location string `json:"location,omitempty"`
}

// PushNotificationConfig represents the configuration for task push notifications.
type PushNotificationConfig struct {
	// Authentication contains optional authentication details.
	Authentication *PushNotificationAuthenticationInfo `json:"authentication,omitempty"`
	// Push Notification ID, created by server to support multiple push notification.
	ID string `json:"id,omitempty"`
	// Token is an optional authentication token.
	Token string `json:"token,omitempty"`
	// URL is the endpoint where notifications should be sent.
	URL string `json:"url"`
	// Metadata is the optional metadata.
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// TaskPushNotificationConfig associates a task ID with push notification settings.
type TaskPushNotificationConfig struct {
	// RPCID is the ID of json-rpc.
	RPCID string `json:"-"`
	// PushNotificationConfig contains the notification settings.
	PushNotificationConfig PushNotificationConfig `json:"pushNotificationConfig"`
	// TaskID is the unique task identifier.
	TaskID string `json:"taskId"`
	// Metadata is the optional metadata.
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// Message represents a single exchange between a user and an agent.
// See A2A Spec section on Messages.
type Message struct {
	// ContextID is the optional context identifier for the message.
	ContextID *string `json:"contextId,omitempty"`
	// Extensions is the optional list of extension URIs.
	Extensions []string `json:"extensions,omitempty"`
	// Kind is the type discriminator for this message (always "message").
	Kind string `json:"kind"`
	// MessageID is the unique identifier for this message.
	MessageID string `json:"messageId"`
	// Metadata is the optional metadata.
	Metadata map[string]interface{} `json:"metadata,omitempty"`
	// Parts is the content parts (must implement Part).
	Parts []Part `json:"parts"`
	// ReferenceTaskIDs is the optional list of referenced task IDs.
	ReferenceTaskIDs []string `json:"referenceTaskIds,omitempty"`
	// Role is the sender of the message.
	Role MessageRole `json:"role"`
	// TaskID is the optional task identifier this message belongs to.
	TaskID *string `json:"taskId,omitempty"`
}

// UnmarshalJSON implements custom unmarshalling logic for Message
// to handle the polymorphic Part interface slice.
func (m *Message) UnmarshalJSON(data []byte) error {
	type Alias Message // Alias to avoid recursion.
	temp := &struct {
		Parts []json.RawMessage `json:"parts"` // Unmarshal parts into RawMessage first.
		*Alias
	}{
		Alias: (*Alias)(m),
	}
	if err := json.Unmarshal(data, &temp); err != nil {
		return fmt.Errorf("failed to unmarshal message base: %w", err)
	}
	// Now, unmarshal each part based on its type.
	m.Parts = make([]Part, 0, len(temp.Parts))
	for i, rawPart := range temp.Parts {
		part, err := unmarshalPart(rawPart)
		if err != nil {
			return fmt.Errorf("failed to unmarshal part %d: %w", i, err)
		}
		m.Parts = append(m.Parts, part)
	}
	return nil
}

// Artifact represents an output generated by a task.
// See A2A Spec 0.2.2 section on Artifacts.
type Artifact struct {
	// ArtifactID is the unique identifier for the artifact.
	ArtifactID string `json:"artifactId"`
	// Name is the name of the artifact.
	Name *string `json:"name,omitempty"`
	// Description is the description of the artifact.
	Description *string `json:"description,omitempty"`
	// Parts is the content parts of the artifact.
	Parts []Part `json:"parts"`
	// Metadata is optional metadata for the artifact.
	Metadata map[string]interface{} `json:"metadata,omitempty"`
	// Extensions is the optional list of extension URIs.
	Extensions []string `json:"extensions,omitempty"`
}

// UnmarshalJSON implements custom unmarshalling logic for Artifact
// to handle the polymorphic Part interface slice.
func (a *Artifact) UnmarshalJSON(data []byte) error {
	type Alias Artifact // Alias to avoid recursion.
	temp := &struct {
		Parts []json.RawMessage `json:"parts"` // Unmarshal parts into RawMessage first.
		*Alias
	}{
		Alias: (*Alias)(a),
	}
	if err := json.Unmarshal(data, &temp); err != nil {
		return fmt.Errorf("failed to unmarshal artifact base: %w", err)
	}
	// Now, unmarshal each part based on its type.
	a.Parts = make([]Part, 0, len(temp.Parts))
	for i, rawPart := range temp.Parts {
		part, err := unmarshalPart(rawPart)
		if err != nil {
			return fmt.Errorf("failed to unmarshal artifact part %d: %w", i, err)
		}
		a.Parts = append(a.Parts, part)
	}
	return nil
}

// unmarshalPart determines the concrete type of a Part from raw JSON
// based on the "kind" field and unmarshals into that concrete type.
// Internal helper function.
func unmarshalPart(rawPart json.RawMessage) (Part, error) {
	// First, determine the type by unmarshalling just the "kind" field.
	var typeDetect struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(rawPart, &typeDetect); err != nil {
		return nil, fmt.Errorf("failed to detect part type: %w", err)
	}
	// Unmarshal into the correct concrete type.
	switch typeDetect.Kind {
	case KindText:
		var p TextPart
		if err := json.Unmarshal(rawPart, &p); err != nil {
			return nil, fmt.Errorf("failed to unmarshal TextPart: %w", err)
		}
		return &p, nil
	case KindFile:
		var p FilePart
		if err := json.Unmarshal(rawPart, &p); err != nil {
			return nil, fmt.Errorf("failed to unmarshal FilePart: %w", err)
		}
		return &p, nil
	case KindData:
		var p DataPart
		if err := json.Unmarshal(rawPart, &p); err != nil {
			return nil, fmt.Errorf("failed to unmarshal DataPart: %w", err)
		}
		return &p, nil
	default:
		// If we need to handle unknown part types gracefully (e.g., store raw JSON),
		// we would add that logic here. For now, treat as an error.
		return nil, fmt.Errorf("unsupported part kind: %s", typeDetect.Kind)
	}
}

// TaskStatus represents the current status of a task.
// See A2A Spec section on Task Lifecycle.
type TaskStatus struct {
	// State is the current lifecycle state.
	State TaskState `json:"state"`
	// Message is the optional message associated with the status (e.g., final response).
	Message *Message `json:"message,omitempty"`
	// Timestamp is the ISO 8601 timestamp of the status change.
	Timestamp string `json:"timestamp"`
}

// Task represents a unit of work being processed by the agent.
// See A2A Spec section on Tasks.
type Task struct {
	// Artifacts is the accumulated artifacts generated by the task.
	Artifacts []Artifact `json:"artifacts,omitempty"`
	// ContextID is the unique context identifier for the task.
	ContextID string `json:"contextId"`
	// History is the history of messages exchanged for this task (if tracked).
	History []Message `json:"history,omitempty"`
	// ID is the unique task identifier.
	ID string `json:"id"`
	// Kind is the event type discriminator (always "task").
	Kind string `json:"kind"`
	// Metadata is the optional metadata associated with the task.
	Metadata map[string]interface{} `json:"metadata,omitempty"`
	// Status is the current task status.
	Status TaskStatus `json:"status"`
}

// TaskStatusUpdateEvent indicates a change in the task's lifecycle state.
// Corresponds to the 'task_status_update' event in A2A Spec 0.2.2.
type TaskStatusUpdateEvent struct {
	// ContextID is the context ID of the task.
	ContextID string `json:"contextId"`
	// Final is a flag indicating if this is the final event for the task.
	Final bool `json:"final"`
	// Kind is the event type discriminator.
	Kind string `json:"kind"`
	// Metadata is the optional metadata.
	Metadata map[string]interface{} `json:"metadata,omitempty"`
	// Status is the new status.
	Status TaskStatus `json:"status"`
	// TaskID is the ID of the task.
	TaskID string `json:"taskId"`
}

// IsFinal returns true if this is a final event.
func (r *TaskStatusUpdateEvent) IsFinal() bool {
	return r.Final
}

// TaskArtifactUpdateEvent indicates a new or updated artifact chunk.
// Corresponds to the 'task_artifact_update' event in A2A Spec 0.2.2.
type TaskArtifactUpdateEvent struct {
	// Append is a hint for the client to append data (streaming).
	Append *bool `json:"append,omitempty"`
	// Artifact is the artifact data.
	Artifact Artifact `json:"artifact"`
	// ContextID is the context ID of the task.
	ContextID string `json:"contextId"`
	// Kind is the event type discriminator.
	Kind string `json:"kind"`
	// LastChunk is a flag indicating if this is the final chunk of an artifact stream.
	LastChunk *bool `json:"lastChunk,omitempty"`
	// Metadata is optional metadata.
	Metadata map[string]interface{} `json:"metadata,omitempty"`
	// TaskID is the ID of the task.
	TaskID string `json:"taskId"`
}

// IsFinal returns true if this is the final artifact event.
func (r TaskArtifactUpdateEvent) IsFinal() bool {
	return r.LastChunk != nil && *r.LastChunk
}

// NewTask creates a new Task with initial state (Submitted).
func NewTask(id string, contextID string) *Task {
	return &Task{
		ID:        id,
		ContextID: contextID,
		Kind:      KindTask,
		Status: TaskStatus{
			State:     TaskStateSubmitted,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		},
		Metadata: make(map[string]interface{}),
	}
}

// NewMessage creates a new Message with the specified role and parts.
func NewMessage(role MessageRole, parts []Part) Message {
	messageID := GenerateMessageID()
	return Message{
		Role:      role,
		Parts:     parts,
		MessageID: messageID,
		Kind:      KindMessage,
	}
}

// NewMessageWithContext creates a new Message with context information.
func NewMessageWithContext(role MessageRole, parts []Part, taskID, contextID *string) Message {
	messageID := GenerateMessageID()
	return Message{
		Role:      role,
		Parts:     parts,
		MessageID: messageID,
		TaskID:    taskID,
		ContextID: contextID,
		Kind:      "message",
	}
}

// NewTextPart creates a new TextPart containing the given text.
func NewTextPart(text string) TextPart {
	return TextPart{
		Kind: KindText,
		Text: text,
	}
}

// NewFilePartWithBytes creates a new FilePart with embedded bytes content.
func NewFilePartWithBytes(name, mimeType string, bytes string) FilePart {
	return FilePart{
		Kind: KindFile,
		File: &FileWithBytes{
			Name:     &name,
			MimeType: &mimeType,
			Bytes:    bytes,
		},
	}
}

// NewFilePartWithURI creates a new FilePart with URI reference.
func NewFilePartWithURI(name, mimeType string, uri string) FilePart {
	return FilePart{
		Kind: KindFile,
		File: &FileWithURI{
			Name:     &name,
			MimeType: &mimeType,
			URI:      uri,
		},
	}
}

// NewDataPart creates a new DataPart with the given data.
func NewDataPart(data interface{}) DataPart {
	return DataPart{
		Kind: KindData,
		Data: data,
	}
}

// NewArtifactWithID creates a new Artifact with a generated ID.
func NewArtifactWithID(name, description *string, parts []Part) *Artifact {
	artifactID := GenerateArtifactID()
	return &Artifact{
		ArtifactID:  artifactID,
		Name:        name,
		Description: description,
		Parts:       parts,
		Metadata:    make(map[string]interface{}),
	}
}

// NewTaskStatusUpdateEvent creates a new TaskStatusUpdateEvent.
func NewTaskStatusUpdateEvent(taskID, contextID string, status TaskStatus, final bool) TaskStatusUpdateEvent {
	return TaskStatusUpdateEvent{
		TaskID:    taskID,
		ContextID: contextID,
		Kind:      KindTaskStatusUpdate,
		Status:    status,
		Final:     final,
	}
}

// NewTaskArtifactUpdateEvent creates a new TaskArtifactUpdateEvent.
func NewTaskArtifactUpdateEvent(taskID, contextID string, artifact Artifact, lastChunk bool) TaskArtifactUpdateEvent {
	return TaskArtifactUpdateEvent{
		TaskID:    taskID,
		ContextID: contextID,
		Kind:      KindTaskArtifactUpdate,
		Artifact:  artifact,
		LastChunk: &lastChunk,
	}
}

// SendTaskParams defines the parameters for the tasks_send and tasks_sendSubscribe RPC methods.
// See A2A Spec section on RPC Methods.
type SendTaskParams struct {
	// RPCID is the ID of json-rpc.
	RPCID string `json:"-"`
	// ID is the ID of the task.
	ID string `json:"id"`
	// SessionID is the optional session ID.
	SessionID *string `json:"sessionId,omitempty"`
	// Message is the user's message initiating the task.
	Message Message `json:"message"`
	// PushNotification contains optional push notification settings.
	PushNotification *PushNotificationConfig `json:"pushNotification,omitempty"`
	// HistoryLength is the requested history length in response.
	HistoryLength *int `json:"historyLength,omitempty"`
	// Metadata is the optional metadata.
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// TaskQueryParams defines the parameters for the tasks_get RPC method.
// See A2A Spec section on RPC Methods.
type TaskQueryParams struct {
	// RPCID is the ID of json-rpc.
	RPCID string `json:"-"`
	// ID is the ID of the task.
	ID string `json:"id"`
	// HistoryLength is the requested message history length.
	HistoryLength *int `json:"historyLength,omitempty"`
	// Metadata is the optional metadata.
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// TaskIDParams defines parameters for methods needing only a task ID (e.g., tasks_cancel).
// See A2A Spec section on RPC Methods.
type TaskIDParams struct {
	// RPCID is the ID of json-rpc.
	RPCID string `json:"-"`
	// ID is task ID.
	ID string `json:"id"`
	// Metadata is the optional metadata.
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// SendMessageParams defines the parameters for the message/send and message/stream RPC methods.
// See A2A Spec 0.2.2 section on RPC Methods.
type SendMessageParams struct {
	// RPCID is the ID of json-rpc.
	RPCID string `json:"-"`
	// Configuration contains optional sending configuration.
	Configuration *SendMessageConfiguration `json:"configuration,omitempty"`
	// Message is the message to send.
	Message Message `json:"message"`
	// Metadata is optional metadata.
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// SendMessageConfiguration defines optional configuration for message sending.
type SendMessageConfiguration struct {
	// PushNotificationConfig contains optional push notification settings.
	PushNotificationConfig *PushNotificationConfig `json:"pushNotificationConfig,omitempty"`
	// HistoryLength is the requested history length in response.
	HistoryLength *int `json:"historyLength,omitempty"`
	// Blocking indicates whether to wait for task completion (message/send only).
	Blocking *bool `json:"blocking,omitempty"`
}

// MessageResult represents the union type response for Message/Task.
type MessageResult struct {
	Result UnaryMessageResult
}

// UnmarshalJSON implements custom unmarshalling logic for MessageResult
func (r *MessageResult) UnmarshalJSON(data []byte) error {
	// First, detect the kind of the result
	var kindOnly struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(data, &kindOnly); err != nil {
		return fmt.Errorf("failed to unmarshal result kind: %w", err)
	}

	// Now unmarshal to the correct concrete type based on kind
	switch kindOnly.Kind {
	case KindMessage:
		r.Result = &Message{}
		if err := json.Unmarshal(data, r.Result); err != nil {
			return fmt.Errorf("failed to unmarshal message: %w", err)
		}
		return nil
	case KindTask:
		r.Result = &Task{}
		if err := json.Unmarshal(data, r.Result); err != nil {
			return fmt.Errorf("failed to unmarshal task: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unsupported result kind: %s", kindOnly.Kind)
	}
}

// MarshalJSON implements custom marshalling logic for MessageResult
func (r *MessageResult) MarshalJSON() ([]byte, error) {
	switch r.Result.GetKind() {
	case KindMessage:
		return json.Marshal(r.Result)
	case KindTask:
		return json.Marshal(r.Result)
	default:
		return nil, fmt.Errorf("unsupported result kind: %s", r.Result.GetKind())
	}
}

// SendStreamingMessageParams defines the parameters for the message/stream RPC method.
type SendStreamingMessageParams struct {
	// ID is the ID of the message.
	ID string `json:"-"`
	// Configuration contains optional sending configuration.
	Configuration *SendMessageConfiguration `json:"configuration,omitempty"`
	// Message is the message to send.
	Message Message `json:"message"`
	// Metadata is optional metadata.
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// StreamingMessageEvent represents the result of a streaming message operation.
// StreamingMessageEvent is the union type of Message/Task/TaskStatusUpdate/TaskArtifactUpdate.
type StreamingMessageEvent struct {
	// Result is the final result of the streaming operation.
	Result StreamingMessageResult
}

// UnmarshalJSON implements custom unmarshalling logic for StreamingMessageEvent
func (r *StreamingMessageEvent) UnmarshalJSON(data []byte) error {
	// First, try to detect if this is wrapped in a Result field
	type StreamingMessageEventRaw struct {
		Result json.RawMessage `json:"Result"`
	}

	var raw StreamingMessageEventRaw
	var actualData []byte

	// Try to unmarshal as wrapped structure first
	if err := json.Unmarshal(data, &raw); err == nil && len(raw.Result) > 0 {
		// It's wrapped, use the Result field
		actualData = raw.Result
	} else {
		// It's not wrapped, use the data directly
		actualData = data
	}

	// Parse the actual data to get the kind
	var kindOnly struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(actualData, &kindOnly); err != nil {
		return fmt.Errorf("failed to unmarshal result kind: %w", err)
	}

	// Now unmarshal to the correct concrete type based on kind
	switch kindOnly.Kind {
	case KindMessage:
		r.Result = &Message{}
		if err := json.Unmarshal(actualData, r.Result); err != nil {
			return fmt.Errorf("failed to unmarshal message: %w", err)
		}
		return nil
	case KindTask:
		r.Result = &Task{}
		if err := json.Unmarshal(actualData, r.Result); err != nil {
			return fmt.Errorf("failed to unmarshal task: %w", err)
		}
		return nil
	case KindTaskStatusUpdate:
		r.Result = &TaskStatusUpdateEvent{}
		if err := json.Unmarshal(actualData, r.Result); err != nil {
			return fmt.Errorf("failed to unmarshal task status update event: %w", err)
		}
		return nil
	case KindTaskArtifactUpdate:
		r.Result = &TaskArtifactUpdateEvent{}
		if err := json.Unmarshal(actualData, r.Result); err != nil {
			return fmt.Errorf("failed to unmarshal task artifact update event: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unsupported result kind: %s", kindOnly.Kind)
	}
}

// MarshalJSON implements custom marshalling logic for StreamingMessageResult
func (r *StreamingMessageEvent) MarshalJSON() ([]byte, error) {
	switch r.Result.GetKind() {
	case KindMessage:
		return json.Marshal(r.Result)
	case KindTask:
		return json.Marshal(r.Result)
	case KindTaskStatusUpdate:
		return json.Marshal(r.Result)
	case KindTaskArtifactUpdate:
		return json.Marshal(r.Result)
	default:
		return nil, fmt.Errorf("unsupported result kind: %s", r.Result.GetKind())
	}
}
