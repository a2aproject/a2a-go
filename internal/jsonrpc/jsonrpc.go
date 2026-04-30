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

// Package jsonrpc provides JSON-RPC 2.0 protocol implementation for A2A.
package jsonrpc

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"maps"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/errordetails"
	"github.com/a2aproject/a2a-go/v2/internal/sse"
	"github.com/a2aproject/a2a-go/v2/internal/utils"
)

// JSON-RPC 2.0 protocol constants
const (
	Version = "2.0"

	// HTTP headers
	ContentJSON = "application/json"

	// JSON-RPC method names per A2A spec §7
	MethodMessageSend          = "SendMessage"
	MethodMessageStream        = "SendStreamingMessage"
	MethodTasksGet             = "GetTask"
	MethodTasksList            = "ListTasks"
	MethodTasksCancel          = "CancelTask"
	MethodTasksResubscribe     = "SubscribeToTask"
	MethodPushConfigGet        = "GetTaskPushNotificationConfig"
	MethodPushConfigSet        = "CreateTaskPushNotificationConfig"
	MethodPushConfigList       = "ListTaskPushNotificationConfigs"
	MethodPushConfigDelete     = "DeleteTaskPushNotificationConfig"
	MethodGetExtendedAgentCard = "GetExtendedAgentCard"
)

// Error represents a JSON-RPC 2.0 error object.
// TODO(yarolegovich): Convert to transport-agnostic error format so Client can use errors.Is(err, a2a.ErrMethodNotFound).
// This needs to be implemented across all transports (currently not in grpc either).
type Error struct {
	Code    int                   `json:"code"`
	Message string                `json:"message"`
	Data    []*errordetails.Typed `json:"data,omitempty"`
}

// Error implements the error interface for jsonrpcError.
func (e *Error) Error() string {
	if len(e.Data) > 0 {
		return fmt.Sprintf("jsonrpc error %d: %s (data: %v)", e.Code, e.Message, e.Data)
	}
	return fmt.Sprintf("jsonrpc error %d: %s", e.Code, e.Message)
}

var codeToError = map[int]error{
	-32700: a2a.ErrParseError,
	-32600: a2a.ErrInvalidRequest,
	-32601: a2a.ErrMethodNotFound,
	-32602: a2a.ErrInvalidParams,
	-32603: a2a.ErrInternalError,
	-32000: a2a.ErrServerError,
	-32001: a2a.ErrTaskNotFound,
	-32002: a2a.ErrTaskNotCancelable,
	-32003: a2a.ErrPushNotificationNotSupported,
	-32004: a2a.ErrUnsupportedOperation,
	-32005: a2a.ErrUnsupportedContentType,
	-32006: a2a.ErrInvalidAgentResponse,
	-32007: a2a.ErrExtendedCardNotConfigured,
	-32008: a2a.ErrExtensionSupportRequired,
	-32009: a2a.ErrVersionNotSupported,
	-31401: a2a.ErrUnauthenticated,
	-31403: a2a.ErrUnauthorized,
}

// FromJSONRPCError converts a JSON-RPC error to an [a2a.Error].
func FromJSONRPCError(e *Error) error {
	if e == nil {
		return nil
	}
	err, ok := codeToError[e.Code]
	if !ok {
		err = a2a.ErrInternalError
	}

	msg := e.Message
	if len(msg) == 0 {
		msg = err.Error()
	}

	var typedDetails []*errordetails.Typed
	firstStruct := true

	result := a2a.NewError(err, msg)
	for _, d := range e.Data {
		if d.TypeURL == errordetails.ErrorInfoType {
			if rawMeta, ok := d.Value["metadata"]; ok {
				m, ok := utils.ToStringMap(rawMeta)
				if ok {
					result = result.WithErrorInfoMeta(m)
				}
			}
		} else {
			if firstStruct {
				result = result.WithDetails(d.Value)
				firstStruct = false
			}
			typedDetails = append(typedDetails, d)
		}
	}
	result.TypedDetails = append(result.TypedDetails, typedDetails...)
	return result
}

// ToJSONRPCError converts an error to a JSON-RPC [Error].
func ToJSONRPCError(err error) *Error {
	if err == nil {
		return nil
	}
	var jsonrpcErr *Error
	if errors.As(err, &jsonrpcErr) {
		return jsonrpcErr
	}

	code := -32603
	reason := "INTERNAL_ERROR"
	var data []*errordetails.Typed

	var a2aErr *a2a.Error
	metadata := map[string]string{
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}

	for c, target := range codeToError {
		if errors.Is(err, target) {
			code = c
			reason = a2a.ErrorReason(target)
			break
		}
	}

	if errors.As(err, &a2aErr) {
		if len(a2aErr.Details) > 0 {
			data = append(data, errordetails.NewFromStruct(a2aErr.Details))
		}
		for _, d := range a2aErr.TypedDetails {
			if d.TypeURL == errordetails.ErrorInfoType {
				if rawMeta, ok := d.Value["metadata"]; ok {
					m, ok := utils.ToStringMap(rawMeta)
					if ok {
						maps.Copy(metadata, m)
					}
				}
			} else {
				data = append(data, d)
			}
		}
	}

	errorInfo := errordetails.NewErrorInfo(reason, a2a.ProtocolDomain, metadata)
	data = append(data, errorInfo)

	return &Error{
		Code:    code,
		Message: err.Error(),
		Data:    data,
	}
}

// NewResultResponse returns a JSON-RPC response with result payload.
func NewResultResponse(reqID any, result any) ServerResponse {
	return ServerResponse{JSONRPC: Version, ID: reqID, Result: result}
}

// NewErrorResponse returns a JSON-RPC response with error payload.
func NewErrorResponse(reqID any, err error) ServerResponse {
	return ServerResponse{JSONRPC: Version, Error: ToJSONRPCError(err), ID: reqID}
}

// MarshalErrorResponse marshals an error as a JSON-RPC response.
func MarshalErrorResponse(reqID any, respErr error) ([]byte, bool) {
	bytes, err := json.Marshal(NewErrorResponse(reqID, respErr))
	if err != nil {
		return nil, false
	}
	return bytes, true
}

// UnmarshalError maps JSON decoding errors to A2A protocol errors.
func UnmarshalError(err error) error {
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		return fmt.Errorf("%w: %w", a2a.ErrInvalidParams, err)
	}
	return fmt.Errorf("%w: %w", a2a.ErrParseError, err)
}

// UnmarshalParams unmarshals JSON-RPC params into typed request params.
func UnmarshalParams[T any](raw json.RawMessage) (*T, error) {
	var params T
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, UnmarshalError(err)
	}
	return &params, nil
}

// UnmarshalResult unmarshals a JSON-RPC result into the requested type.
func UnmarshalResult[T any](raw json.RawMessage, resultName string) (T, error) {
	var result T
	if err := json.Unmarshal(raw, &result); err != nil {
		return result, fmt.Errorf("failed to unmarshal %s: %w", resultName, err)
	}
	return result, nil
}

// ParseSSEStream parses SSE data as JSON-RPC client responses.
func ParseSSEStream(body io.Reader) iter.Seq2[json.RawMessage, error] {
	return func(yield func(json.RawMessage, error) bool) {
		for data, err := range sse.ParseDataStream(body) {
			if err != nil {
				yield(nil, err)
				return
			}
			var resp ClientResponse
			if err := json.Unmarshal(data, &resp); err != nil {
				yield(nil, fmt.Errorf("failed to parse SSE data: %w", err))
				return
			}
			if resp.Error != nil {
				yield(nil, FromJSONRPCError(resp.Error))
				return
			}
			if !yield(resp.Result, nil) {
				return
			}
		}
	}
}

// IsValidID checks if the given ID is valid for a JSON-RPC request.
func IsValidID(id any) bool {
	if id == nil {
		return true
	}
	switch id.(type) {
	case string, float64:
		return true
	default:
		return false
	}
}

// ServerRequest represents a JSON-RPC 2.0 server request.
type ServerRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      any             `json:"id"`
}

// ServerResponse represents a JSON-RPC 2.0 server response.
type ServerResponse struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id"`
	Result  any    `json:"result,omitempty"`
	Error   *Error `json:"error,omitempty"`
}

// ClientRequest represents a JSON-RPC 2.0 client request.
type ClientRequest struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
	ID      string `json:"id"`
}

// ClientResponse represents a JSON-RPC 2.0 client response.
type ClientResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}
