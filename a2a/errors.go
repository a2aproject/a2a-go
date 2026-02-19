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

package a2a

import "errors"

// https://a2a-protocol.org/latest/specification/#8-error-handling
var (
	// ErrParseError indicates that server received payload that was not well-formed.
	ErrParseError = errors.New("parse error")

	// ErrInvalidRequest indicates that server received a well-formed payload which was not a valid request.
	ErrInvalidRequest = errors.New("invalid request")

	// ErrMethodNotFound indicates that a method does not exist or is not supported.
	ErrMethodNotFound = errors.New("method not found")

	// ErrInvalidParams indicates that params provided for the method were invalid (e.g., wrong type, missing required field).
	ErrInvalidParams = errors.New("invalid params")

	// ErrInternalError indicates an unexpected error occurred on the server during processing.
	ErrInternalError = errors.New("internal error")

	// ErrServerError reserved for implementation-defined server-errors.
	ErrServerError = errors.New("server error")

	// ErrTaskNotFound indicates that a task with the provided ID was not found.
	ErrTaskNotFound = errors.New("task not found")

	// ErrTaskNotCancelable indicates that the task was in a state where it could not be canceled.
	ErrTaskNotCancelable = errors.New("task cannot be canceled")

	// ErrPushNotificationNotSupported indicates that the agent does not support push notifications.
	ErrPushNotificationNotSupported = errors.New("push notification not supported")

	// ErrUnsupportedOperation indicates that the requested operation is not supported by the agent.
	ErrUnsupportedOperation = errors.New("this operation is not supported")

	// ErrUnsupportedContentType indicates an incompatibility between the requested
	// content types and the agent's capabilities.
	ErrUnsupportedContentType = errors.New("incompatible content types")

	// ErrInvalidAgentResponse indicates that the agent returned a response that
	// does not conform to the specification for the current method.
	ErrInvalidAgentResponse = errors.New("invalid agent response")

	// ErrAuthenticatedExtendedCardNotConfigured indicates that the agent does not have an Authenticated
	// Extended Card configured.
	ErrAuthenticatedExtendedCardNotConfigured = errors.New("extended card not configured")

	// ErrExtensionSupportRequired indicates that the Client requested use of an extension marked as
	// required: true in the Agent Card but the client did not declare support for it in the request.
	ErrExtensionSupportRequired = errors.New("extension support required")

	// ErrVersionNotSupported indicates that the The A2A protocol version specified in the request
	// (via A2A-Version service parameter) is not supported by the agent.
	ErrVersionNotSupported = errors.New("this version is not supported")

	// ErrUnauthenticated indicates that the request does not have valid authentication credentials.
	ErrUnauthenticated = errors.New("unauthenticated")

	// ErrUnauthorized indicates that the caller does not have permission to execute the specified operation.
	ErrUnauthorized = errors.New("permission denied")
)

// Error provides control over the message and details returned to clients.
type Error struct {
	// Err is the underlying error. It will be used for transport-specific code selection.
	Err error
	// Message is a human-readable description of the error returned to clients.
	Message string
	// Details can contain additional structured information about the error.
	Details map[string]any
}

// Error returns the error message.
func (e *Error) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return "internal error"
}

// Unwrap provides access to the error cause.
func (e *Error) Unwrap() error {
	return e.Err
}

// NewError creates a new A2A Error wrapping the provided error with a custom message.
func NewError(err error, message string) *Error {
	return &Error{Err: err, Message: message}
}

// WithDetails attaches structured data to the error.
func (e *Error) WithDetails(details map[string]any) *Error {
	e.Details = details
	return e
}
