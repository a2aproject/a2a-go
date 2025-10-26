package jsonrpc

import (
	"encoding/json"
	"fmt"
)

// JSON-RPC 2.0 protocol constants
const (
	Version = "2.0"

	// HTTP headers
	HeaderContentType = "application/json"
	HeaderEventStream = "text/event-stream"

	// JSON-RPC method names per A2A spec §7
	MethodMessageSend              = "message/send"
	MethodMessageStream            = "message/stream"
	MethodTasksGet                 = "tasks/get"
	MethodTasksCancel              = "tasks/cancel"
	MethodTasksResubscribe         = "tasks/resubscribe"
	MethodPushConfigGet            = "tasks/pushNotificationConfig/get"
	MethodPushConfigSet            = "tasks/pushNotificationConfig/set"
	MethodPushConfigList           = "tasks/pushNotificationConfig/list"
	MethodPushConfigDelete         = "tasks/pushNotificationConfig/delete"
	MethodGetAuthenticatedExtended = "agent/getAuthenticatedExtendedCard"
)

// jsonrpcError represents a JSON-RPC 2.0 error object.
// TODO(yarolegovich): Convert to transport-agnostic error format so Client can use errors.Is(err, a2a.ErrMethodNotFound).
// This needs to be implemented across all transports (currently not in grpc either).
type Error struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Error implements the error interface for jsonrpcError.
func (e *Error) Error() string {
	if len(e.Data) > 0 {
		return fmt.Sprintf("jsonrpc error %d: %s (data: %s)", e.Code, e.Message, string(e.Data))
	}
	return fmt.Sprintf("jsonrpc error %d: %s", e.Code, e.Message)
}
