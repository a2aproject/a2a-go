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
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/a2aproject/a2a-go/v1/a2a"
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

// Error represents a problem detail as defined in RFC 7807.
type Error struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	Status    int    `json:"status"`
	Detail    string `json:"detail"`
	TaskID    string `json:"taskId,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
}

type errorDetails struct {
	status int
	uri    string
	title  string
}

var errToDetails = map[error]errorDetails{
	a2a.ErrTaskNotFound: {
		status: http.StatusNotFound,
		uri:    "https://a2a-protocol.org/errors/task-not-found",
		title:  "Task Not Found",
	},
	a2a.ErrTaskNotCancelable: {
		status: http.StatusConflict,
		uri:    "https://a2a-protocol.org/errors/task-not-cancelable",
		title:  "Task Not Cancelable",
	},
	a2a.ErrPushNotificationNotSupported: {
		status: http.StatusBadRequest,
		uri:    "https://a2a-protocol.org/errors/push-notification-not-supported",
		title:  "Push Notification Not Supported",
	},
	a2a.ErrUnsupportedOperation: {
		status: http.StatusBadRequest,
		uri:    "https://a2a-protocol.org/errors/unsupported-operation",
		title:  "Unsupported Operation",
	},
	a2a.ErrUnsupportedContentType: {
		status: http.StatusUnsupportedMediaType,
		uri:    "https://a2a-protocol.org/errors/content-type-not-supported",
		title:  "Content Type Not Supported",
	},
	a2a.ErrInvalidAgentResponse: {
		status: http.StatusBadGateway,
		uri:    "https://a2a-protocol.org/errors/invalid-agent-response",
		title:  "Invalid Agent Response",
	},
	a2a.ErrExtendedCardNotConfigured: {
		status: http.StatusBadRequest,
		uri:    "https://a2a-protocol.org/errors/extended-agent-card-not-configured",
		title:  "Extended Agent Card Not Configured",
	},
	a2a.ErrExtensionSupportRequired: {
		status: http.StatusBadRequest,
		uri:    "https://a2a-protocol.org/errors/extension-support-required",
		title:  "Extension Support Required",
	},
	a2a.ErrVersionNotSupported: {
		status: http.StatusBadRequest,
		uri:    "https://a2a-protocol.org/errors/version-not-supported",
		title:  "Version Not Supported",
	},
	a2a.ErrParseError: {
		status: http.StatusBadRequest,
		uri:    "https://a2a-protocol.org/errors/parse-error",
		title:  "Parse Error",
	},
	a2a.ErrInvalidRequest: {
		status: http.StatusBadRequest,
		uri:    "https://a2a-protocol.org/errors/invalid-request",
		title:  "Invalid Request",
	},
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

	a2aError := a2a.ErrInternalError
	for err, details := range errToDetails {
		if e.Type == details.uri {
			a2aError = err
			break
		}
	}

	return fmt.Errorf("%s: %w", e.Detail, a2aError)
}

// ToRESTError converts an error and a [a2a.TaskID] to a REST [Error].
func ToRESTError(err error, taskID a2a.TaskID) *Error {
	// Default to Internal Server Error
	e := &Error{
		Type:      "https://a2a-protocol.org/errors/internal-error",
		Title:     "Internal Server Error",
		Status:    http.StatusInternalServerError,
		Detail:    err.Error(),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		TaskID:    string(taskID),
	}

	if details, ok := errToDetails[err]; ok {
		e.Type = details.uri
		e.Title = details.title
		e.Status = details.status
	}

	return e
}
