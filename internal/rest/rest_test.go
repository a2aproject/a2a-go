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
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/a2aproject/a2a-go/a2a"
)

func TestError_ToA2AError(t *testing.T) {
	tests := []struct {
		name         string
		contentType  string
		responseBody string
		wantError    error
		wantDetail   string
	}{
		{
			name:        "Task Not Found",
			contentType: "application/problem+json",
			responseBody: `{
				"type": "https://a2a-protocol.org/errors/task-not-found",
				"title": "Task Not Found",
				"status": 404,
				"detail": "The specified task ID does not exist"
			}`,
			wantError:  a2a.ErrTaskNotFound,
			wantDetail: "The specified task ID does not exist",
		},
		{
			name:        "Task Not Cancelable",
			contentType: "application/problem+json",
			responseBody: `{
				"type": "https://a2a-protocol.org/errors/task-not-cancelable",
				"title": "Task Not Cancelable",
				"status": 409,
				"detail": "The specified task is not cancelable"
			}`,
			wantError:  a2a.ErrTaskNotCancelable,
			wantDetail: "The specified task is not cancelable",
		},
		{
			name:        "Push Notification Not Supported",
			contentType: "application/problem+json",
			responseBody: `{
				"type": "https://a2a-protocol.org/errors/push-notification-not-supported",
				"title": "Push Not Supported",
				"status": 400,
				"detail": "This agent does not support push notifications"
			}`,
			wantError:  a2a.ErrPushNotificationNotSupported,
			wantDetail: "This agent does not support push notifications",
		},
		{
			name:        "Unsupported Operation",
			contentType: "application/problem+json",
			responseBody: `{
				"type": "https://a2a-protocol.org/errors/unsupported-operation",
				"title": "Unsupported",
				"status": 400,
				"detail": "Operation not allowed"
			}`,
			wantError:  a2a.ErrUnsupportedOperation,
			wantDetail: "Operation not allowed",
		},
		{
			name:        "Unsupported Content Typy",
			contentType: "application/problem+json",
			responseBody: `{
				"type": "https://a2a-protocol.org/errors/content-type-not-supported",
				"title": "Unsupported",
				"status": 415,
				"detail": "Content type not allowed"
			}`,
			wantError:  a2a.ErrUnsupportedContentType,
			wantDetail: "Content type not allowed",
		},
		{
			name:        "Invalid Agent Response",
			contentType: "application/problem+json",
			responseBody: `{
				"type": "https://a2a-protocol.org/errors/invalid-agent-response",
				"title": "Invalid Agent Response",
				"status": 502,
				"detail": "The agent response is not valid"
			}`,
			wantError:  a2a.ErrInvalidAgentResponse,
			wantDetail: "The agent response is not valid",
		},
		{
			name:        "Extended Agent Card not configured",
			contentType: "application/problem+json",
			responseBody: `{
				"type": "https://a2a-protocol.org/errors/extended-agent-card-not-configured",
				"title": "Extended Agent Card not configured",
				"status": 400,
				"detail": "The Extended Agent Card for this agent is not configured"
			}`,
			wantError:  a2a.ErrExtendedCardNotConfigured,
			wantDetail: "The Extended Agent Card for this agent is not configured",
		},
		{
			name:        "Extension support required",
			contentType: "application/problem+json",
			responseBody: `{
				"type": "https://a2a-protocol.org/errors/extension-support-required",
				"title": "Extension support required",
				"status": 400,
				"detail": "Extension support is required for this agent"
			}`,
			wantError:  a2a.ErrExtensionSupportRequired,
			wantDetail: "Extension support is required for this agent",
		},
		{
			name:        "Version not supported",
			contentType: "application/problem+json",
			responseBody: `{
				"type": "https://a2a-protocol.org/errors/version-not-supported",
				"title": "Version not supported",
				"status": 400,
				"detail": "This version is not supported"
			}`,
			wantError:  a2a.ErrVersionNotSupported,
			wantDetail: "This version is not supported",
		},
		{
			name:        "Unknown Type defaults to InternalError",
			contentType: "application/problem+json",
			responseBody: `{
				"type": "https://a2a-protocol.org/errors/unknown-error",
				"title": "Weird Error",
				"status": 500,
				"detail": "Something unexpected happened"
			}`,
			wantError:  a2a.ErrInternalError,
			wantDetail: "Something unexpected happened",
		},
		{
			name:         "Invalid Content-Type (Standard JSON)",
			contentType:  "application/json",
			responseBody: `{"error": "generic error"}`,
			wantError:    a2a.ErrServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &http.Response{
				Header: http.Header{"Content-Type": []string{tt.contentType}},
				Body:   io.NopCloser(bytes.NewBufferString(tt.responseBody)),
			}

			// 3. Call the method
			gotErr := ToA2AError(resp)

			if !errors.Is(gotErr, tt.wantError) {
				t.Errorf("ToA2AError() error = %v, want %v", gotErr, tt.wantError)
			}

			if tt.wantDetail != "" {
				if !strings.Contains(gotErr.Error(), tt.wantDetail) {
					t.Errorf("ToA2AError() error message %q does not contain detail %q", gotErr.Error(), tt.wantDetail)
				}
			}
		})
	}
}

func TestToRESTError(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		taskID     string
		wantStatus int
		wantType   string
		wantTitle  string
	}{
		{
			name:       "Task Not Found",
			err:        a2a.ErrTaskNotFound,
			taskID:     "task-123",
			wantStatus: http.StatusNotFound,
			wantType:   "https://a2a-protocol.org/errors/task-not-found",
			wantTitle:  "Task Not Found",
		},
		{
			name:       "Task Not Cancelable",
			err:        a2a.ErrTaskNotCancelable,
			taskID:     "task-123",
			wantStatus: http.StatusConflict,
			wantType:   "https://a2a-protocol.org/errors/task-not-cancelable",
			wantTitle:  "Task Not Cancelable",
		},
		{
			name:       "Push Notification Not Supported",
			err:        a2a.ErrPushNotificationNotSupported,
			taskID:     "task-123",
			wantStatus: http.StatusBadRequest,
			wantType:   "https://a2a-protocol.org/errors/push-notification-not-supported",
			wantTitle:  "Push Notification Not Supported",
		},
		{
			name:       "Unsupported Operation",
			err:        a2a.ErrUnsupportedOperation,
			taskID:     "task-123",
			wantStatus: http.StatusBadRequest,
			wantType:   "https://a2a-protocol.org/errors/unsupported-operation",
			wantTitle:  "Unsupported Operation",
		},
		{
			name:       "Content Type Not Supported",
			err:        a2a.ErrUnsupportedContentType,
			taskID:     "task-123",
			wantStatus: http.StatusUnsupportedMediaType,
			wantType:   "https://a2a-protocol.org/errors/content-type-not-supported",
			wantTitle:  "Content Type Not Supported",
		},
		{
			name:       "Invalid Agent Response",
			err:        a2a.ErrInvalidAgentResponse,
			wantStatus: http.StatusBadGateway,
			wantType:   "https://a2a-protocol.org/errors/invalid-agent-response",
			wantTitle:  "Invalid Agent Response",
		},
		{
			name:       "Extended Agent Card Not Configured",
			err:        a2a.ErrExtendedCardNotConfigured,
			wantStatus: http.StatusBadRequest,
			wantType:   "https://a2a-protocol.org/errors/extended-agent-card-not-configured",
			wantTitle:  "Extended Agent Card Not Configured",
		},
		{
			name:       "Extension Support Required",
			err:        a2a.ErrExtensionSupportRequired,
			wantStatus: http.StatusBadRequest,
			wantType:   "https://a2a-protocol.org/errors/extension-support-required",
			wantTitle:  "Extension Support Required",
		},
		{
			name:       "Version Not Supported",
			err:        a2a.ErrVersionNotSupported,
			wantStatus: http.StatusBadRequest,
			wantType:   "https://a2a-protocol.org/errors/version-not-supported",
			wantTitle:  "Version Not Supported",
		},
		{
			name:       "Parse Error",
			err:        a2a.ErrParseError,
			wantStatus: http.StatusBadRequest,
			wantType:   "https://a2a-protocol.org/errors/parse-error",
			wantTitle:  "Parse Error",
		},
		{
			name:       "Invalid Request",
			err:        a2a.ErrInvalidRequest,
			wantStatus: http.StatusBadRequest,
			wantType:   "https://a2a-protocol.org/errors/invalid-request",
			wantTitle:  "Invalid Request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ToRESTError(tt.err, a2a.TaskID(tt.taskID))

			if got.Status != tt.wantStatus {
				t.Errorf("ToRESTError() status = %v, want %v", got.Status, tt.wantStatus)
			}
			if got.Type != tt.wantType {
				t.Errorf("ToRESTError type = %v, want %v", got.Type, tt.wantType)
			}
			if got.Title != tt.wantTitle {
				t.Errorf("ToRESTError title = %v, want %v", got.Title, tt.wantTitle)
			}
			if got.TaskID != "" && got.TaskID != tt.taskID {
				t.Errorf("ToRESTError taskID = %v, want %v", got.TaskID, tt.taskID)
			}
			if got.Detail == "" {
				t.Error("ToRESTError() detail is empty")
			}
		})
	}
}
