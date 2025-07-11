// Package jsonprotocol defines constants and potentially shared types for the A2A protocol itself.
package jsonprotocol

// Method names for A2A 0.2.2 specification.
const (
	// MethodMessageSend corresponds to the 'message/send' RPC method.
	MethodMessageSend = "message/send"
	// MethodMessageStream corresponds to the 'message/stream' RPC method.
	MethodMessageStream = "message/stream"
	// MethodTasksGet corresponds to the 'tasks/get' RPC method.
	MethodTasksGet = "tasks/get"
	// MethodTasksCancel corresponds to the 'tasks/cancel' RPC method.
	MethodTasksCancel = "tasks/cancel"
	// MethodTasksPushNotificationConfigSet corresponds to the 'tasks/pushNotificationConfig/set' RPC method.
	MethodTasksPushNotificationConfigSet = "tasks/pushNotificationConfig/set"
	// MethodTasksPushNotificationConfigGet corresponds to the 'tasks/pushNotificationConfig/get' RPC method.
	MethodTasksPushNotificationConfigGet = "tasks/pushNotificationConfig/get"
	// MethodTasksResubscribe corresponds to the 'tasks/resubscribe' RPC method.
	MethodTasksResubscribe = "tasks/resubscribe"
	// MethodAgentAuthenticatedExtendedCard corresponds to the 'agent/authenticatedExtendedCard' HTTP GET endpoint.
	MethodAgentAuthenticatedExtendedCard = "agent/authenticatedExtendedCard"
)

// A2A SSE Event Types define the standard event type strings used in A2A SSE streams.
const (
	EventStatusUpdate   = "task_status_update"
	EventArtifactUpdate = "task_artifact_update"
	EventTask           = "task"
	EventMessage        = "message"

	// EventClose is used internally by this implementation's server to signal stream closure.
	// Note: This might not be part of the formal A2A spec but is used in server logic.
	EventClose = "close"
)

// A2A HTTP Endpoint Paths define the standard paths used in the A2A protocol.
const (
	// AgentCardPath is the path for the agent metadata JSON endpoint.
	AgentCardPath = "/.well-known/agent.json"
	// JWKSPath is the path for the JWKS endpoint.
	JWKSPath = "/.well-known/jwks.json"
	// DefaultJSONRPCPath is the default path for the JSON-RPC endpoint.
	DefaultJSONRPCPath = "/"
)
