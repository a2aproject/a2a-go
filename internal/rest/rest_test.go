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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

func makeStatusBody(code int, status, message, reason string) string {
	return fmt.Sprintf(`{
		"error": {
			"code": %d,
			"status": %q,
			"message": %s,
			"details": [
				{
					"@type": "type.googleapis.com/google.rpc.ErrorInfo",
					"reason": %q,
					"domain": "a2a-protocol.org"
				}
			]
		}
	}`, code, status, mustJSON(message), reason)
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func TestError_ToA2AError(t *testing.T) {
	tests := []struct {
		name         string
		contentType  string
		responseBody string
		wantError    error
		wantMessage  string
	}{
		{
			name:         "Task Not Found",
			contentType:  "application/json",
			responseBody: makeStatusBody(404, "NOT_FOUND", "The specified task ID does not exist", "TASK_NOT_FOUND"),
			wantError:    a2a.ErrTaskNotFound,
			wantMessage:  "The specified task ID does not exist",
		},
		{
			name:         "Task Not Cancelable",
			contentType:  "application/json",
			responseBody: makeStatusBody(400, "FAILED_PRECONDITION", "The specified task is not cancelable", "TASK_NOT_CANCELABLE"),
			wantError:    a2a.ErrTaskNotCancelable,
			wantMessage:  "The specified task is not cancelable",
		},
		{
			name:         "Push Notification Not Supported",
			contentType:  "application/json",
			responseBody: makeStatusBody(501, "UNIMPLEMENTED", "This agent does not support push notifications", "PUSH_NOTIFICATION_NOT_SUPPORTED"),
			wantError:    a2a.ErrPushNotificationNotSupported,
			wantMessage:  "This agent does not support push notifications",
		},
		{
			name:         "Unsupported Operation",
			contentType:  "application/json",
			responseBody: makeStatusBody(501, "UNIMPLEMENTED", "Operation not allowed", "UNSUPPORTED_OPERATION"),
			wantError:    a2a.ErrUnsupportedOperation,
			wantMessage:  "Operation not allowed",
		},
		{
			name:         "Unsupported Content Type",
			contentType:  "application/json",
			responseBody: makeStatusBody(400, "INVALID_ARGUMENT", "Content type not allowed", "UNSUPPORTED_CONTENT_TYPE"),
			wantError:    a2a.ErrUnsupportedContentType,
			wantMessage:  "Content type not allowed",
		},
		{
			name:         "Invalid Agent Response",
			contentType:  "application/json",
			responseBody: makeStatusBody(500, "INTERNAL", "The agent response is not valid", "INVALID_AGENT_RESPONSE"),
			wantError:    a2a.ErrInvalidAgentResponse,
			wantMessage:  "The agent response is not valid",
		},
		{
			name:         "Extended Agent Card not configured",
			contentType:  "application/json",
			responseBody: makeStatusBody(400, "FAILED_PRECONDITION", "The Extended Agent Card for this agent is not configured", "EXTENDED_AGENT_CARD_NOT_CONFIGURED"),
			wantError:    a2a.ErrExtendedCardNotConfigured,
			wantMessage:  "The Extended Agent Card for this agent is not configured",
		},
		{
			name:         "Extension support required",
			contentType:  "application/json",
			responseBody: makeStatusBody(400, "FAILED_PRECONDITION", "Extension support is required for this agent", "EXTENSION_SUPPORT_REQUIRED"),
			wantError:    a2a.ErrExtensionSupportRequired,
			wantMessage:  "Extension support is required for this agent",
		},
		{
			name:         "Version not supported",
			contentType:  "application/json",
			responseBody: makeStatusBody(501, "UNIMPLEMENTED", "This version is not supported", "VERSION_NOT_SUPPORTED"),
			wantError:    a2a.ErrVersionNotSupported,
			wantMessage:  "This version is not supported",
		},
		{
			name:        "Unknown reason defaults to InternalError",
			contentType: "application/json",
			responseBody: `{
				"error": {
					"code": 500,
					"status": "INTERNAL",
					"message": "Something unexpected happened",
					"details": [
						{
							"@type": "type.googleapis.com/google.rpc.ErrorInfo",
							"reason": "UNKNOWN_REASON",
							"domain": "a2a-protocol.org"
						}
					]
				}
			}`,
			wantError:   a2a.ErrInternalError,
			wantMessage: "Something unexpected happened",
		},
		{
			name:         "Invalid Content-Type",
			contentType:  "text/plain",
			responseBody: `not json`,
			wantError:    a2a.ErrServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &http.Response{
				Header: http.Header{"Content-Type": []string{tt.contentType}},
				Body:   io.NopCloser(bytes.NewBufferString(tt.responseBody)),
			}

			gotErr := ToA2AError(resp)

			if !errors.Is(gotErr, tt.wantError) {
				t.Fatalf("ToA2AError() error = %v, want %v", gotErr, tt.wantError)
			}

			if tt.wantMessage != "" {
				if !strings.Contains(gotErr.Error(), tt.wantMessage) {
					t.Fatalf("ToA2AError() error message %q does not contain %q", gotErr.Error(), tt.wantMessage)
				}
			}
		})
	}
}

func TestToRESTError(t *testing.T) {
	tests := []struct {
		name           string
		err            error
		taskID         string
		wantHTTPStatus int
		wantGRPCStatus string
		wantReason     string
	}{
		{
			name:           "Task Not Found",
			err:            a2a.ErrTaskNotFound,
			taskID:         "task-123",
			wantHTTPStatus: http.StatusNotFound,
			wantGRPCStatus: "NOT_FOUND",
			wantReason:     "TASK_NOT_FOUND",
		},
		{
			name:           "Task Not Cancelable",
			err:            a2a.ErrTaskNotCancelable,
			taskID:         "task-123",
			wantHTTPStatus: http.StatusBadRequest,
			wantGRPCStatus: "FAILED_PRECONDITION",
			wantReason:     "TASK_NOT_CANCELABLE",
		},
		{
			name:           "Push Notification Not Supported",
			err:            a2a.ErrPushNotificationNotSupported,
			taskID:         "task-123",
			wantHTTPStatus: http.StatusNotImplemented,
			wantGRPCStatus: "UNIMPLEMENTED",
			wantReason:     "PUSH_NOTIFICATION_NOT_SUPPORTED",
		},
		{
			name:           "Unsupported Operation",
			err:            a2a.ErrUnsupportedOperation,
			taskID:         "task-123",
			wantHTTPStatus: http.StatusNotImplemented,
			wantGRPCStatus: "UNIMPLEMENTED",
			wantReason:     "UNSUPPORTED_OPERATION",
		},
		{
			name:           "Content Type Not Supported",
			err:            a2a.ErrUnsupportedContentType,
			taskID:         "task-123",
			wantHTTPStatus: http.StatusBadRequest,
			wantGRPCStatus: "INVALID_ARGUMENT",
			wantReason:     "UNSUPPORTED_CONTENT_TYPE",
		},
		{
			name:           "Invalid Agent Response",
			err:            a2a.ErrInvalidAgentResponse,
			wantHTTPStatus: http.StatusInternalServerError,
			wantGRPCStatus: "INTERNAL",
			wantReason:     "INVALID_AGENT_RESPONSE",
		},
		{
			name:           "Extended Agent Card Not Configured",
			err:            a2a.ErrExtendedCardNotConfigured,
			wantHTTPStatus: http.StatusBadRequest,
			wantGRPCStatus: "FAILED_PRECONDITION",
			wantReason:     "EXTENDED_AGENT_CARD_NOT_CONFIGURED",
		},
		{
			name:           "Extension Support Required",
			err:            a2a.ErrExtensionSupportRequired,
			wantHTTPStatus: http.StatusBadRequest,
			wantGRPCStatus: "FAILED_PRECONDITION",
			wantReason:     "EXTENSION_SUPPORT_REQUIRED",
		},
		{
			name:           "Version Not Supported",
			err:            a2a.ErrVersionNotSupported,
			wantHTTPStatus: http.StatusNotImplemented,
			wantGRPCStatus: "UNIMPLEMENTED",
			wantReason:     "VERSION_NOT_SUPPORTED",
		},
		{
			name:           "Parse Error",
			err:            a2a.ErrParseError,
			wantHTTPStatus: http.StatusBadRequest,
			wantGRPCStatus: "INVALID_ARGUMENT",
			wantReason:     "PARSE_ERROR",
		},
		{
			name:           "Invalid Request",
			err:            a2a.ErrInvalidRequest,
			wantHTTPStatus: http.StatusBadRequest,
			wantGRPCStatus: "INVALID_ARGUMENT",
			wantReason:     "INVALID_REQUEST",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ToRESTError(tt.err, a2a.TaskID(tt.taskID))

			if got.HTTPStatus() != tt.wantHTTPStatus {
				t.Fatalf("ToRESTError() HTTPStatus() = %v, want %v", got.HTTPStatus(), tt.wantHTTPStatus)
			}
			if got.Err.Code != tt.wantHTTPStatus {
				t.Fatalf("ToRESTError() Err.Code = %v, want %v", got.Err.Code, tt.wantHTTPStatus)
			}
			if got.Err.Status != tt.wantGRPCStatus {
				t.Fatalf("ToRESTError() Err.Status = %v, want %v", got.Err.Status, tt.wantGRPCStatus)
			}
			if got.Err.Message == "" {
				t.Fatal("ToRESTError() Err.Message is empty")
			}
			if len(got.Err.Details) == 0 {
				t.Fatal("ToRESTError() Err.Details is empty")
			}
			info, ok := got.Err.Details[0].(ErrorInfo)
			if !ok {
				t.Fatalf("ToRESTError() first detail is %T, want ErrorInfo", got.Err.Details[0])
			}
			if info.Reason != tt.wantReason {
				t.Fatalf("ToRESTError() ErrorInfo.Reason = %v, want %v", info.Reason, tt.wantReason)
			}
			if info.Domain != "a2a-protocol.org" {
				t.Fatalf("ToRESTError() ErrorInfo.Domain = %v, want a2a-protocol.org", info.Domain)
			}
			if tt.taskID != "" {
				if info.Metadata["taskId"] != tt.taskID {
					t.Fatalf("ToRESTError() ErrorInfo.Metadata[taskId] = %v, want %v", info.Metadata["taskId"], tt.taskID)
				}
			}
		})
	}
}

func TestToRESTError_RoundTrip(t *testing.T) {
	errs := []error{
		a2a.ErrTaskNotFound,
		a2a.ErrTaskNotCancelable,
		a2a.ErrPushNotificationNotSupported,
		a2a.ErrUnsupportedOperation,
		a2a.ErrUnsupportedContentType,
		a2a.ErrInvalidAgentResponse,
		a2a.ErrExtendedCardNotConfigured,
		a2a.ErrExtensionSupportRequired,
		a2a.ErrVersionNotSupported,
		a2a.ErrParseError,
		a2a.ErrInvalidRequest,
	}

	for _, sentinel := range errs {
		t.Run(sentinel.Error(), func(t *testing.T) {
			t.Parallel()

			restErr := ToRESTError(sentinel, "task-rt")
			body, err := json.Marshal(restErr)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}

			resp := &http.Response{
				Header: http.Header{"Content-Type": []string{"application/json"}},
				Body:   io.NopCloser(bytes.NewBuffer(body)),
			}
			got := ToA2AError(resp)
			if !errors.Is(got, sentinel) {
				t.Fatalf("ToA2AError(ToRESTError(%v)) = %v, want errors.Is to match", sentinel, got)
			}

			var a2aErr *a2a.Error
			if !errors.As(got, &a2aErr) {
				t.Fatalf("ToA2AError() returned %T, want *a2a.Error", got)
			}
			if a2aErr.Details["taskId"] != "task-rt" {
				t.Fatalf("ToA2AError() Details[taskId] = %v, want task-rt", a2aErr.Details["taskId"])
			}
			if _, ok := a2aErr.Details["timestamp"]; !ok {
				t.Fatal("ToA2AError() Details[timestamp] is missing")
			}
		})
	}
}

func TestParseStreamResponse(t *testing.T) {
	t.Parallel()

	t.Run("task event", func(t *testing.T) {
		data := []byte(`{"task":{"id":"task-123","contextId":"ctx-123","status":{"state":"TASK_STATE_WORKING"}}}`)
		event, err := ParseStreamResponse(data)
		if err != nil {
			t.Fatalf("ParseStreamResponse() error = %v, want nil", err)
		}
		task, ok := event.(*a2a.Task)
		if !ok {
			t.Fatalf("got event type %T, want *a2a.Task", event)
		}
		if task.ID != "task-123" {
			t.Fatalf("got task ID %s, want task-123", task.ID)
		}
	})

	t.Run("message event", func(t *testing.T) {
		data := []byte(`{"message":{"messageId":"msg-1","role":"ROLE_AGENT","parts":[{"text":"hi"}]}}`)
		event, err := ParseStreamResponse(data)
		if err != nil {
			t.Fatalf("ParseStreamResponse() error = %v, want nil", err)
		}
		if _, ok := event.(*a2a.Message); !ok {
			t.Fatalf("got event type %T, want *a2a.Message", event)
		}
	})

	t.Run("status update event", func(t *testing.T) {
		data := []byte(`{"statusUpdate":{"taskId":"task-123","contextId":"ctx-123","final":false,"status":{"state":"TASK_STATE_COMPLETED"}}}`)
		event, err := ParseStreamResponse(data)
		if err != nil {
			t.Fatalf("ParseStreamResponse() error = %v, want nil", err)
		}
		if _, ok := event.(*a2a.TaskStatusUpdateEvent); !ok {
			t.Fatalf("got event type %T, want *a2a.TaskStatusUpdateEvent", event)
		}
	})

	t.Run("artifact update event", func(t *testing.T) {
		data := []byte(`{"artifactUpdate":{"taskId":"task-123","contextId":"ctx-123","artifact":{"artifactId":"art-1","parts":[{"text":"x"}]}}}`)
		event, err := ParseStreamResponse(data)
		if err != nil {
			t.Fatalf("ParseStreamResponse() error = %v, want nil", err)
		}
		if _, ok := event.(*a2a.TaskArtifactUpdateEvent); !ok {
			t.Fatalf("got event type %T, want *a2a.TaskArtifactUpdateEvent", event)
		}
	})

	t.Run("error event", func(t *testing.T) {
		data := []byte(makeStatusBody(400, "INVALID_ARGUMENT", "bad request", "INVALID_REQUEST"))
		event, err := ParseStreamResponse(data)
		if event != nil {
			t.Fatalf("got event %v, want nil", event)
		}
		if !errors.Is(err, a2a.ErrInvalidRequest) {
			t.Fatalf("got error %v, want %v", err, a2a.ErrInvalidRequest)
		}
	})

	t.Run("unknown type", func(t *testing.T) {
		data := []byte(`{"foo":"bar"}`)
		event, err := ParseStreamResponse(data)
		if event != nil {
			t.Fatalf("got event %v, want nil", event)
		}
		if err == nil {
			t.Fatal("ParseStreamResponse() returned nil error, want error")
		}
	})

	t.Run("multiple discriminators", func(t *testing.T) {
		data := []byte(`{"task":{"id":"t1","contextId":"c1","status":{"state":"TASK_STATE_WORKING"}},"message":{"messageId":"m1","role":"ROLE_AGENT","parts":[{"text":"hi"}]}}`)
		event, err := ParseStreamResponse(data)
		if event != nil {
			t.Fatalf("got event %v, want nil", event)
		}
		if err == nil {
			t.Fatal("ParseStreamResponse() returned nil error, want error")
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		event, err := ParseStreamResponse([]byte(`not json`))
		if event != nil {
			t.Fatalf("got event %v, want nil", event)
		}
		if err == nil {
			t.Fatal("ParseStreamResponse() returned nil error, want error")
		}
	})
}

func TestToRESTError_NonStringDetails(t *testing.T) {
	t.Parallel()

	src := a2a.NewError(a2a.ErrTaskNotFound, "not found").WithDetails(map[string]any{
		"foo": "bar",
		"num": 123,
	})

	restErr := ToRESTError(src, "task-x")

	if len(restErr.Err.Details) != 2 {
		t.Fatalf("ToRESTError() len(Details) = %d, want 2", len(restErr.Err.Details))
	}
	info, ok := restErr.Err.Details[0].(ErrorInfo)
	if !ok {
		t.Fatalf("ToRESTError() Details[0] is %T, want ErrorInfo", restErr.Err.Details[0])
	}
	if info.Metadata["foo"] != "bar" {
		t.Fatalf("ErrorInfo.Metadata[foo] = %v, want bar", info.Metadata["foo"])
	}
	extra, ok := restErr.Err.Details[1].(map[string]any)
	if !ok {
		t.Fatalf("ToRESTError() Details[1] is %T, want map[string]any", restErr.Err.Details[1])
	}
	if extra["num"] != 123 {
		t.Fatalf("Details[1][num] = %v, want 123", extra["num"])
	}

	body, err := json.Marshal(restErr)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	resp := &http.Response{
		Header: http.Header{"Content-Type": []string{"application/json"}},
		Body:   io.NopCloser(bytes.NewBuffer(body)),
	}
	got := ToA2AError(resp)

	var a2aErr *a2a.Error
	if !errors.As(got, &a2aErr) {
		t.Fatalf("ToA2AError() returned %T, want *a2a.Error", got)
	}
	if a2aErr.Details["foo"] != "bar" {
		t.Fatalf("ToA2AError() Details[foo] = %v, want bar", a2aErr.Details["foo"])
	}
	if a2aErr.Details["num"].(float64) != 123 {
		t.Fatalf("ToA2AError() Details[num] = %v, want 123", a2aErr.Details["num"])
	}
}
