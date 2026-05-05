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

// Package rest provides REST protocol constants and error handling for A2A.
package rest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"strings"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/errordetails"
	"github.com/a2aproject/a2a-go/v2/internal/utils"
)

// MakeListTasksPath returns the REST path for listing tasks.
func MakeListTasksPath() string {
	return "/tasks"
}

// MakeSendMessagePath returns the REST path for sending a message.
func MakeSendMessagePath() string {
	return "/message:send"
}

// MakeStreamMessagePath returns the REST path for streaming messages.
func MakeStreamMessagePath() string {
	return "/message:stream"
}

// MakeGetExtendedAgentCardPath returns the REST path for getting an extended agent card.
func MakeGetExtendedAgentCardPath() string {
	return "/extendedAgentCard"
}

// MakeGetTaskPath returns the REST path for getting a specific task.
func MakeGetTaskPath(taskID string) string {
	return "/tasks/" + taskID
}

// MakeCancelTaskPath returns the REST path for cancelling a task.
func MakeCancelTaskPath(taskID string) string {
	return "/tasks/" + taskID + ":cancel"
}

// MakeSubscribeTaskPath returns the REST path for subscribing to task updates.
func MakeSubscribeTaskPath(taskID string) string {
	return "/tasks/" + taskID + ":subscribe"
}

// MakeCreatePushConfigPath returns the REST path for creating a push notification config for a task.
func MakeCreatePushConfigPath(taskID string) string {
	return "/tasks/" + taskID + "/pushNotificationConfigs"
}

// MakeGetPushConfigPath returns the REST path for getting a specific push notification config for a task.
func MakeGetPushConfigPath(taskID, configID string) string {
	return "/tasks/" + taskID + "/pushNotificationConfigs/" + configID
}

// MakeListPushConfigsPath returns the REST path for listing push notification configs for a task.
func MakeListPushConfigsPath(taskID string) string {
	return "/tasks/" + taskID + "/pushNotificationConfigs"
}

// MakeDeletePushConfigPath returns the REST path for deleting a push notification config for a task.
func MakeDeletePushConfigPath(taskID, configID string) string {
	return "/tasks/" + taskID + "/pushNotificationConfigs/" + configID
}

// ErrorInfo represents a google.rpc.ErrorInfo message in the details array.
type ErrorInfo struct {
	Type     string            `json:"@type"`
	Reason   string            `json:"reason"`
	Domain   string            `json:"domain"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// StatusError represents the inner error object in a google.rpc.Status response.
type StatusError struct {
	Code    int                   `json:"code"`
	Status  string                `json:"status"`
	Message string                `json:"message"`
	Details []*errordetails.Typed `json:"details,omitempty"`
}

// Error represents a google.rpc.Status error response per AIP-193.
type Error struct {
	httpStatus int
	Err        StatusError `json:"error"`
}

// HTTPStatus returns the HTTP status code for the error.
func (e *Error) HTTPStatus() int {
	return e.httpStatus
}

var errorMappings = []struct {
	err        error
	httpStatus int
	grpcStatus string
}{
	{a2a.ErrParseError, http.StatusBadRequest, "INVALID_ARGUMENT"},
	{a2a.ErrInvalidRequest, http.StatusBadRequest, "INVALID_ARGUMENT"},
	{a2a.ErrMethodNotFound, http.StatusNotImplemented, "UNIMPLEMENTED"},
	{a2a.ErrInvalidParams, http.StatusBadRequest, "INVALID_ARGUMENT"},
	{a2a.ErrInternalError, http.StatusInternalServerError, "INTERNAL"},
	{a2a.ErrServerError, http.StatusInternalServerError, "INTERNAL"},
	{a2a.ErrTaskNotFound, http.StatusNotFound, "NOT_FOUND"},
	{a2a.ErrTaskNotCancelable, http.StatusBadRequest, "FAILED_PRECONDITION"},
	{a2a.ErrPushNotificationNotSupported, http.StatusBadRequest, "FAILED_PRECONDITION"},
	{a2a.ErrUnsupportedOperation, http.StatusBadRequest, "FAILED_PRECONDITION"},
	{a2a.ErrUnsupportedContentType, http.StatusBadRequest, "INVALID_ARGUMENT"},
	{a2a.ErrInvalidAgentResponse, http.StatusInternalServerError, "INTERNAL"},
	{a2a.ErrExtendedCardNotConfigured, http.StatusBadRequest, "FAILED_PRECONDITION"},
	{a2a.ErrExtensionSupportRequired, http.StatusBadRequest, "FAILED_PRECONDITION"},
	{a2a.ErrVersionNotSupported, http.StatusBadRequest, "FAILED_PRECONDITION"},
	{a2a.ErrUnauthenticated, http.StatusUnauthorized, "UNAUTHENTICATED"},
	{a2a.ErrUnauthorized, http.StatusForbidden, "PERMISSION_DENIED"},

	{context.Canceled, 499, "CANCELLED"},
	{context.DeadlineExceeded, http.StatusGatewayTimeout, "DEADLINE_EXCEEDED"},
}

// ErrorBodyJSON is the JSON shape of the inner error object in a google.rpc.Status response.
type ErrorBodyJSON struct {
	Code    int               `json:"code"`
	Status  string            `json:"status"`
	Message string            `json:"message"`
	Details []json.RawMessage `json:"details"`
}

// ConvertErrorBody converts a parsed google.rpc.Status error body into an a2a error.
func ConvertErrorBody(body *ErrorBodyJSON) error {
	var reason string
	var typedDetails []*errordetails.Typed
	errInfoMeta := make(map[string]string)
	details := make(map[string]any)
	firstStruct := true

	for _, raw := range body.Details {
		var hint struct {
			Type string `json:"@type"`
		}
		if json.Unmarshal(raw, &hint) == nil && hint.Type == errordetails.ErrorInfoType {
			var info ErrorInfo
			if json.Unmarshal(raw, &info) != nil || info.Domain != a2a.ProtocolDomain {
				continue
			}
			reason = info.Reason
			maps.Copy(errInfoMeta, info.Metadata)
			continue
		}
		var extra errordetails.Typed
		if json.Unmarshal(raw, &extra) == nil {
			// Add the first struct to details
			if firstStruct {
				maps.Copy(details, extra.Value)
				firstStruct = false
			}
			// Add all structs to typedDetails
			typedDetails = append(typedDetails, &extra)
		}
	}

	baseErr := a2a.ErrInternalError
	for _, mapping := range errorMappings {
		if a2a.ErrorReason(mapping.err) == reason {
			baseErr = mapping.err
			break
		}
	}

	out := a2a.NewError(baseErr, body.Message)
	if len(errInfoMeta) > 0 {
		out = out.WithErrorInfoMeta(errInfoMeta)
	}
	if len(details) > 0 {
		out = out.WithDetails(details)
	}
	out.TypedDetails = append(out.TypedDetails, typedDetails...)
	return out
}

// FromRESTError converts an HTTP error response in google.rpc.Status format to an a2a error.
func FromRESTError(resp *http.Response) error {
	contentType := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "application/json") {
		return a2a.ErrServerError
	}

	var body struct {
		Error ErrorBodyJSON `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return fmt.Errorf("failed to decode error response: %w: %w", err, a2a.ErrParseError)
	}

	return ConvertErrorBody(&body.Error)
}

// streamResponse is the JSON shape of a REST SSE stream response.
type streamResponse struct {
	Message        *a2a.Message                 `json:"message,omitempty"`
	Task           *a2a.Task                    `json:"task,omitempty"`
	StatusUpdate   *a2a.TaskStatusUpdateEvent   `json:"statusUpdate,omitempty"`
	ArtifactUpdate *a2a.TaskArtifactUpdateEvent `json:"artifactUpdate,omitempty"`
	Error          *ErrorBodyJSON               `json:"error,omitempty"`
}

// ParseStreamResponse parses raw SSE stream data in a single JSON unmarshal.
// Returns (event, nil) for event payloads or (nil, err) for server-side errors.
func ParseStreamResponse(data []byte) (a2a.Event, error) {
	var wrapper streamResponse
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return nil, fmt.Errorf("failed to unmarshal stream response: %w", err)
	}
	var n int
	var event a2a.Event
	var errResp error
	if wrapper.Message != nil {
		event = wrapper.Message
		n++
	}
	if wrapper.Task != nil {
		event = wrapper.Task
		n++
	}
	if wrapper.StatusUpdate != nil {
		event = wrapper.StatusUpdate
		n++
	}
	if wrapper.ArtifactUpdate != nil {
		event = wrapper.ArtifactUpdate
		n++
	}
	if wrapper.Error != nil {
		errResp = ConvertErrorBody(wrapper.Error)
		n++
	}
	if n == 0 {
		return nil, fmt.Errorf("unknown stream response type")
	}
	if n != 1 {
		return nil, fmt.Errorf("expected exactly one stream response type, got %d", n)
	}
	if errResp != nil {
		return nil, errResp
	}
	return event, nil
}

// ToRESTError converts an error and a [a2a.TaskID] to a REST [Error] in google.rpc.Status format.
func ToRESTError(err error, taskID a2a.TaskID) *Error {
	if err == nil {
		return nil
	}
	httpStatus := http.StatusInternalServerError
	grpcStatus := "INTERNAL"
	reason := "INTERNAL_ERROR"

	for _, mapping := range errorMappings {
		if errors.Is(err, mapping.err) {
			httpStatus = mapping.httpStatus
			grpcStatus = mapping.grpcStatus
			reason = a2a.ErrorReason(mapping.err)
			break
		}
	}

	metadata := map[string]string{
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}
	if taskID != "" {
		metadata["taskId"] = string(taskID)
	}

	var a2aErr *a2a.Error
	var details []*errordetails.Typed

	if errors.As(err, &a2aErr) {
		if len(a2aErr.Details) > 0 {
			details = append(details, errordetails.NewFromStruct(a2aErr.Details))
		}
		for _, d := range a2aErr.TypedDetails {
			if d.TypeURL == errordetails.ErrorInfoType {
				if rawMeta, ok := d.Value["metadata"]; ok {
					if m, ok := utils.ToStringMap(rawMeta); ok {
						maps.Copy(metadata, m)
					}
				}
			} else {
				details = append(details, d)
			}
		}
	}

	errorInfo := errordetails.NewErrorInfo(reason, a2a.ProtocolDomain, metadata)
	details = append(details, errorInfo)

	return &Error{
		httpStatus: httpStatus,
		Err: StatusError{
			Code:    httpStatus,
			Status:  grpcStatus,
			Message: err.Error(),
			Details: details,
		},
	}
}
