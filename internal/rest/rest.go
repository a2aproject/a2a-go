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

package rest

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
)

type Error struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	Status    int    `json:"status"`
	Detail    string `json:"detail"`
	TaskID    string `json:"taskId,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
}

// ToA2AError returns A2A error  based on HTTP status codes and messages
func ToA2AError(resp *http.Response) error {
	contentType := resp.Header.Get("Content-Type")
	if contentType != "application/problem+json" {
		return a2a.ErrServerError
	}

	var e Error
	if err := json.NewDecoder(resp.Body).Decode(&e); err != nil {
		return fmt.Errorf("failed to decode error details: %w", err)
	}

	var A2AError error

	switch e.Type {
	case "https://a2a-protocol.org/errors/task-not-found":
		A2AError = a2a.ErrTaskNotFound
	case "https://a2a-protocol.org/errors/task-not-cancelable":
		A2AError = a2a.ErrTaskNotCancelable
	case "https://a2a-protocol.org/errors/push-notification-not-supported":
		A2AError = a2a.ErrPushNotificationNotSupported
	case "https://a2a-protocol.org/errors/unsupported-operation":
		A2AError = a2a.ErrUnsupportedOperation
	case "https://a2a-protocol.org/errors/content-type-not-supported":
		A2AError = a2a.ErrUnsupportedContentType
	case "https://a2a-protocol.org/errors/invalid-agent-response":
		A2AError = a2a.ErrInvalidAgentResponse
	case "https://a2a-protocol.org/errors/extended-agent-card-not-configured":
		A2AError = a2a.ErrAuthenticatedExtendedCardNotConfigured
	case "https://a2a-protocol.org/errors/extension-support-required":
		A2AError = a2a.ErrExtensionSupportRequired
	case "https://a2a-protocol.org/errors/version-not-supported":
		A2AError = a2a.ErrVersionNotSupported
	default:
		return a2a.ErrServerError
	}

	return fmt.Errorf("%s: %w", e.Detail, A2AError)
}

func ToRESTError(err error, taskID string) *Error {
	// Default to Internal Server Error
	e := &Error{
		Type:      "https://a2a-protocol.org/errors/internal-error",
		Title:     "Internal Server Error",
		Status:    http.StatusInternalServerError,
		Detail:    err.Error(),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		TaskID:    taskID,
	}

	switch {
	case errors.Is(err, a2a.ErrTaskNotFound):
		e.Status = http.StatusNotFound
		e.Type = "https://a2a-protocol.org/errors/task-not-found"
		e.Title = "Task Not Found"

	case errors.Is(err, a2a.ErrTaskNotCancelable):
		e.Status = http.StatusConflict
		e.Type = "https://a2a-protocol.org/errors/task-not-cancelable"
		e.Title = "Task Not Cancelable"

	case errors.Is(err, a2a.ErrPushNotificationNotSupported):
		e.Status = http.StatusBadRequest
		e.Type = "https://a2a-protocol.org/errors/push-notification-not-supported"
		e.Title = "Push Notification Not Supported"

	case errors.Is(err, a2a.ErrUnsupportedOperation):
		e.Status = http.StatusBadRequest
		e.Type = "https://a2a-protocol.org/errors/unsupported-operation"
		e.Title = "Unsupported Operation"

	case errors.Is(err, a2a.ErrUnsupportedContentType):
		e.Status = http.StatusUnsupportedMediaType
		e.Type = "https://a2a-protocol.org/errors/content-type-not-supported"
		e.Title = "Content Type Not Supported"

	case errors.Is(err, a2a.ErrInvalidAgentResponse):
		e.Status = http.StatusBadGateway
		e.Type = "https://a2a-protocol.org/errors/invalid-agent-response"
		e.Title = "Invalid Agent Response"

	case errors.Is(err, a2a.ErrAuthenticatedExtendedCardNotConfigured):
		e.Status = http.StatusBadRequest
		e.Type = "https://a2a-protocol.org/errors/extended-agent-card-not-configured"
		e.Title = "Extended Agent Card Not Configured"

	case errors.Is(err, a2a.ErrExtensionSupportRequired):
		e.Status = http.StatusBadRequest
		e.Type = "https://a2a-protocol.org/errors/extension-support-required"
		e.Title = "Extension Support Required"

	case errors.Is(err, a2a.ErrVersionNotSupported):
		e.Status = http.StatusBadRequest
		e.Type = "https://a2a-protocol.org/errors/version-not-supported"
		e.Title = "Version Not Supported"

	case errors.Is(err, a2a.ErrParseError):
		e.Status = http.StatusBadRequest
		e.Type = "https://a2a-protocol.org/errors/parse-error"
		e.Title = "Parse Error"

	case errors.Is(err, a2a.ErrInvalidRequest):
		e.Status = http.StatusBadRequest
		e.Type = "https://a2a-protocol.org/errors/invalid-request"
		e.Title = "Invalid Request"

	}

	return e
}
