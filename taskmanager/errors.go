// Package taskmanager defines task management interfaces, types, and implementations.
package taskmanager

import (
	"fmt"

	"github.com/a2aproject/a2a-go/internal/jsonrpc"
	"github.com/a2aproject/a2a-go/protocol/jsonprotocol"
)

// JSON-RPC standard error codes
const (
	ErrCodeJSONParse      int = -32700 // Invalid JSON was received by the server
	ErrCodeInvalidRequest int = -32600 // The JSON sent is not a valid Request object
	ErrCodeMethodNotFound int = -32601 // The method does not exist or is not available
	ErrCodeInvalidParams  int = -32602 // Invalid method parameter(s)
	ErrCodeInternalError  int = -32603 // Internal JSON-RPC error
)

// Custom JSON-RPC error codes specific to the A2A specification
const (
	ErrCodeTaskNotFound                 int = -32001 // Task not found
	ErrCodeTaskNotCancelable            int = -32002 // Task cannot be canceled
	ErrCodePushNotificationNotSupported int = -32003 // Push Notification is not supported
	ErrCodeUnsupportedOperation         int = -32004 // This operation is not supported
	ErrCodeContentTypeNotSupported      int = -32005 // Incompatible content types
	ErrCodeInvalidAgentResponse         int = -32006 // Invalid agent response
)

// ErrCodeTaskFinal is deprecated: Use ErrCodeTaskNotCancelable instead
const ErrCodeTaskFinal int = -32002

// ErrCodePushNotificationNotConfigured is deprecated: Use ErrCodePushNotificationNotSupported instead
const ErrCodePushNotificationNotConfigured int = -32003

// Standard JSON-RPC error functions

// ErrJSONParse creates a JSON-RPC error for invalid JSON payload.
func ErrJSONParse(details string) *jsonrpc.Error {
	return &jsonrpc.Error{
		Code:    ErrCodeJSONParse,
		Message: "Invalid JSON payload",
		Data:    details,
	}
}

// ErrInvalidRequest creates a JSON-RPC error for invalid request object.
func ErrInvalidRequest(details string) *jsonrpc.Error {
	return &jsonrpc.Error{
		Code:    ErrCodeInvalidRequest,
		Message: "Request payload validation error",
		Data:    details,
	}
}

// ErrMethodNotFound creates a JSON-RPC error for method not found.
func ErrMethodNotFound(method string) *jsonrpc.Error {
	return &jsonrpc.Error{
		Code:    ErrCodeMethodNotFound,
		Message: "Method not found",
		Data:    fmt.Sprintf("Method '%s' not found", method),
	}
}

// ErrInvalidParams creates a JSON-RPC error for invalid parameters.
func ErrInvalidParams(details string) *jsonrpc.Error {
	return &jsonrpc.Error{
		Code:    ErrCodeInvalidParams,
		Message: "Invalid parameters",
		Data:    details,
	}
}

// ErrInternalError creates a JSON-RPC error for internal server error.
func ErrInternalError(details string) *jsonrpc.Error {
	return &jsonrpc.Error{
		Code:    ErrCodeInternalError,
		Message: "Internal error",
		Data:    details,
	}
}

// A2A specific error functions

// ErrTaskNotFound creates a JSON-RPC error for task not found.
func ErrTaskNotFound(taskID string) *jsonrpc.Error {
	return &jsonrpc.Error{
		Code:    ErrCodeTaskNotFound,
		Message: "Task not found",
		Data:    fmt.Sprintf("Task with ID '%s' was not found.", taskID),
	}
}

// ErrTaskNotCancelable creates a JSON-RPC error for task that cannot be canceled.
func ErrTaskNotCancelable(taskID string, state jsonprotocol.TaskState) *jsonrpc.Error {
	return &jsonrpc.Error{
		Code:    ErrCodeTaskNotCancelable,
		Message: "Task cannot be canceled",
		Data:    fmt.Sprintf("Task '%s' is in state '%s' and cannot be canceled", taskID, state),
	}
}

// ErrPushNotificationNotSupported creates a JSON-RPC error for unsupported push notifications.
func ErrPushNotificationNotSupported() *jsonrpc.Error {
	return &jsonrpc.Error{
		Code:    ErrCodePushNotificationNotSupported,
		Message: "Push Notification is not supported",
		Data:    "This agent does not support push notifications",
	}
}

// ErrUnsupportedOperation creates a JSON-RPC error for unsupported operations.
func ErrUnsupportedOperation(operation string) *jsonrpc.Error {
	return &jsonrpc.Error{
		Code:    ErrCodeUnsupportedOperation,
		Message: "This operation is not supported",
		Data:    fmt.Sprintf("Operation '%s' is not supported by this agent", operation),
	}
}

// ErrContentTypeNotSupported creates a JSON-RPC error for incompatible content types.
func ErrContentTypeNotSupported(contentType string) *jsonrpc.Error {
	return &jsonrpc.Error{
		Code:    ErrCodeContentTypeNotSupported,
		Message: "Incompatible content types",
		Data:    fmt.Sprintf("Content type '%s' is not supported", contentType),
	}
}

// ErrInvalidAgentResponse creates a JSON-RPC error for invalid agent response.
func ErrInvalidAgentResponse(details string) *jsonrpc.Error {
	return &jsonrpc.Error{
		Code:    ErrCodeInvalidAgentResponse,
		Message: "Invalid agent response",
		Data:    details,
	}
}
