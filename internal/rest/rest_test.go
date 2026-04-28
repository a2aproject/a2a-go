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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/errordetails"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestRESTError_RoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		inputError     *a2a.Error
		taskID         a2a.TaskID
		typedDetails   []*errordetails.Typed
		wantHTTPStatus int
		wantGRPCStatus string
		want           *a2a.Error
	}{
		{
			name: "TaskNotFound with details and metadata",
			inputError: a2a.NewError(a2a.ErrTaskNotFound, "not found").WithErrorInfoMeta(map[string]string{
				"foo": "bar",
			}).WithDetails(map[string]any{
				"num": float64(123),
			}),
			wantHTTPStatus: http.StatusNotFound,
			wantGRPCStatus: "NOT_FOUND",
			want: &a2a.Error{
				Err:     a2a.ErrTaskNotFound,
				Message: "not found",
				Details: map[string]any{
					"num": float64(123),
				},
				TypedDetails: []*errordetails.Typed{
					errordetails.NewErrorInfo("TASK_NOT_FOUND", a2a.ProtocolDomain, map[string]string{
						"foo": "bar",
					}),
					errordetails.NewFromStruct(map[string]any{
						"num": float64(123),
					}),
				},
			},
		},
		{
			name: "Wrapped ParseError with details and metadata",
			inputError: a2a.NewError(fmt.Errorf("wrapped error: %w", a2a.ErrParseError), "wrapped parse error").WithErrorInfoMeta(map[string]string{
				"foo": "bar",
			}).WithDetails(map[string]any{
				"num": float64(123),
			}),
			wantHTTPStatus: http.StatusBadRequest,
			wantGRPCStatus: "INVALID_ARGUMENT",
			want: &a2a.Error{
				Err:     a2a.ErrParseError,
				Message: "wrapped parse error",
				Details: map[string]any{
					"num": float64(123),
				},
				TypedDetails: []*errordetails.Typed{
					errordetails.NewErrorInfo("PARSE_ERROR", a2a.ProtocolDomain, map[string]string{
						"foo": "bar",
					}),
					errordetails.NewFromStruct(map[string]any{
						"num": float64(123),
					}),
				},
			},
		},
		{
			name: "InvalidParams with details and metadata",
			inputError: a2a.NewError(a2a.ErrInvalidParams, "invalid params").WithErrorInfoMeta(map[string]string{
				"foo": "bar",
			}).WithDetails(map[string]any{
				"num": float64(123),
			}),
			wantHTTPStatus: http.StatusBadRequest,
			wantGRPCStatus: "INVALID_ARGUMENT",
			want: &a2a.Error{
				Err:     a2a.ErrInvalidParams,
				Message: "invalid params",
				Details: map[string]any{
					"num": float64(123),
				},
				TypedDetails: []*errordetails.Typed{
					errordetails.NewErrorInfo("INVALID_PARAMS", a2a.ProtocolDomain, map[string]string{
						"foo": "bar",
					}),
					errordetails.NewFromStruct(map[string]any{
						"num": float64(123),
					}),
				},
			},
		},
		{
			name: "ExtensionSupportRequired with details, typed details and metadata",
			inputError: a2a.NewError(a2a.ErrExtensionSupportRequired, "extension support required").WithErrorInfoMeta(map[string]string{
				"foo": "bar",
			}).WithDetails(map[string]any{
				"num": float64(123),
			}),
			typedDetails: []*errordetails.Typed{
				errordetails.NewFromStruct(map[string]any{
					"extra": "should not leak into details",
				}),
			},
			wantHTTPStatus: http.StatusBadRequest,
			wantGRPCStatus: "FAILED_PRECONDITION",
			want: &a2a.Error{
				Err:     a2a.ErrExtensionSupportRequired,
				Message: "extension support required",
				Details: map[string]any{
					"num": float64(123),
				},
				TypedDetails: []*errordetails.Typed{
					errordetails.NewErrorInfo("EXTENSION_SUPPORT_REQUIRED", a2a.ProtocolDomain, map[string]string{
						"foo": "bar",
					}),
					errordetails.NewFromStruct(map[string]any{
						"num": float64(123),
					}),
					errordetails.NewFromStruct(map[string]any{
						"extra": "should not leak into details",
					}),
				},
			},
		},
		{
			name:           "InvalidRequest without extra details",
			inputError:     a2a.NewError(a2a.ErrInvalidRequest, "invalid request"),
			wantHTTPStatus: http.StatusBadRequest,
			wantGRPCStatus: "INVALID_ARGUMENT",
			want:           errorWithErrorInfo(t, "invalid request", a2a.ErrInvalidRequest, "INVALID_REQUEST"),
		},
		{
			name: "MethodNotFound with details only",
			inputError: a2a.NewError(a2a.ErrMethodNotFound, "method not found").WithDetails(map[string]any{
				"num": float64(123),
			}),
			wantHTTPStatus: http.StatusNotImplemented,
			wantGRPCStatus: "UNIMPLEMENTED",
			want: &a2a.Error{
				Err:     a2a.ErrMethodNotFound,
				Message: "method not found",
				Details: map[string]any{
					"num": float64(123),
				},
				TypedDetails: []*errordetails.Typed{
					errordetails.NewTyped(errordetails.ErrorInfoType, map[string]any{
						"reason":   "METHOD_NOT_FOUND",
						"domain":   a2a.ProtocolDomain,
						"metadata": map[string]string{},
					}),
					errordetails.NewFromStruct(map[string]any{
						"num": float64(123),
					}),
				},
			},
		},
		{
			name: "ServerError with ErrorInfo only",
			inputError: a2a.NewError(a2a.ErrServerError, "server error").WithErrorInfoMeta(map[string]string{
				"foo": "bar",
			}),
			wantHTTPStatus: http.StatusInternalServerError,
			wantGRPCStatus: "INTERNAL",
			want: &a2a.Error{
				Err:     a2a.ErrServerError,
				Message: "server error",
				TypedDetails: []*errordetails.Typed{
					errordetails.NewErrorInfo("SERVER_ERROR", a2a.ProtocolDomain, map[string]string{
						"foo": "bar",
					}),
				},
			},
		},
		{
			name: "TaskNotCancelable ErrorInfo override",
			inputError: a2a.NewError(a2a.ErrTaskNotCancelable, "task not cancelable").WithErrorInfoMeta(map[string]string{
				"foo": "bar",
			}).WithErrorInfoMeta(map[string]string{
				"new_key": "new_value",
			}),
			wantHTTPStatus: http.StatusBadRequest,
			wantGRPCStatus: "FAILED_PRECONDITION",
			want: &a2a.Error{
				Err:     a2a.ErrTaskNotCancelable,
				Message: "task not cancelable",
				TypedDetails: []*errordetails.Typed{
					errordetails.NewErrorInfo("TASK_NOT_CANCELABLE", a2a.ProtocolDomain, map[string]string{
						"new_key": "new_value",
					}),
				},
			},
		},
		{
			name:           "PushNotificationNotSupported with task id",
			inputError:     a2a.NewError(a2a.ErrPushNotificationNotSupported, "push notification not supported"),
			taskID:         a2a.TaskID("task-id"),
			wantHTTPStatus: http.StatusBadRequest,
			wantGRPCStatus: "FAILED_PRECONDITION",
			want: &a2a.Error{
				Err:     a2a.ErrPushNotificationNotSupported,
				Message: "push notification not supported",
				TypedDetails: []*errordetails.Typed{
					errordetails.NewErrorInfo("PUSH_NOTIFICATION_NOT_SUPPORTED", a2a.ProtocolDomain, map[string]string{
						"taskId": "task-id",
					}),
				},
			},
		},
		{
			name:           "UnsupportedOperation",
			inputError:     a2a.NewError(a2a.ErrUnsupportedOperation, "unsupported operation"),
			wantHTTPStatus: http.StatusBadRequest,
			wantGRPCStatus: "FAILED_PRECONDITION",
			want:           errorWithErrorInfo(t, "unsupported operation", a2a.ErrUnsupportedOperation, "UNSUPPORTED_OPERATION"),
		},
		{
			name:           "UnsupportedContentType",
			inputError:     a2a.NewError(a2a.ErrUnsupportedContentType, "unsupported content type"),
			wantHTTPStatus: http.StatusBadRequest,
			wantGRPCStatus: "INVALID_ARGUMENT",
			want:           errorWithErrorInfo(t, "unsupported content type", a2a.ErrUnsupportedContentType, "CONTENT_TYPE_NOT_SUPPORTED"),
		},
		{
			name:           "InvalidAgentResponse",
			inputError:     a2a.NewError(a2a.ErrInvalidAgentResponse, "invalid agent response"),
			wantHTTPStatus: http.StatusInternalServerError,
			wantGRPCStatus: "INTERNAL",
			want:           errorWithErrorInfo(t, "invalid agent response", a2a.ErrInvalidAgentResponse, "INVALID_AGENT_RESPONSE"),
		},
		{
			name:           "ExtendedCardNotConfigured",
			inputError:     a2a.NewError(a2a.ErrExtendedCardNotConfigured, "extended card not configured"),
			wantHTTPStatus: http.StatusBadRequest,
			wantGRPCStatus: "FAILED_PRECONDITION",
			want:           errorWithErrorInfo(t, "extended card not configured", a2a.ErrExtendedCardNotConfigured, "EXTENDED_AGENT_CARD_NOT_CONFIGURED"),
		},
		{
			name:           "VersionNotSupported",
			inputError:     a2a.NewError(a2a.ErrVersionNotSupported, "version not supported"),
			wantHTTPStatus: http.StatusBadRequest,
			wantGRPCStatus: "FAILED_PRECONDITION",
			want:           errorWithErrorInfo(t, "version not supported", a2a.ErrVersionNotSupported, "VERSION_NOT_SUPPORTED"),
		},
		{
			name:           "Unauthenticated",
			inputError:     a2a.NewError(a2a.ErrUnauthenticated, "unauthenticated"),
			wantHTTPStatus: http.StatusUnauthorized,
			wantGRPCStatus: "UNAUTHENTICATED",
			want:           errorWithErrorInfo(t, "unauthenticated", a2a.ErrUnauthenticated, "UNAUTHENTICATED"),
		},
		{
			name:           "Unauthorized",
			inputError:     a2a.NewError(a2a.ErrUnauthorized, "unauthorized"),
			wantHTTPStatus: http.StatusForbidden,
			wantGRPCStatus: "PERMISSION_DENIED",
			want:           errorWithErrorInfo(t, "unauthorized", a2a.ErrUnauthorized, "UNAUTHORIZED"),
		},
		{
			name:           "InternalError",
			inputError:     a2a.NewError(a2a.ErrInternalError, "internal error"),
			wantHTTPStatus: http.StatusInternalServerError,
			wantGRPCStatus: "INTERNAL",
			want:           errorWithErrorInfo(t, "internal error", a2a.ErrInternalError, "INTERNAL_ERROR"),
		},
		{
			name:           "UnknownError roundtrips to Internal",
			inputError:     a2a.NewError(errors.New("unknown error"), "unknown error"),
			wantHTTPStatus: http.StatusInternalServerError,
			wantGRPCStatus: "INTERNAL",
			want:           errorWithErrorInfo(t, "unknown error", a2a.ErrInternalError, "INTERNAL_ERROR"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if tc.typedDetails != nil {
				tc.inputError.TypedDetails = append(tc.inputError.TypedDetails, tc.typedDetails...)
			}

			restErr := ToRESTError(tc.inputError, tc.taskID)
			if restErr.HTTPStatus() != tc.wantHTTPStatus {
				t.Fatalf("ToRESTError() HTTPStatus = %v, want %v", restErr.HTTPStatus(), tc.wantHTTPStatus)
			}
			if restErr.Err.Code != tc.wantHTTPStatus {
				t.Fatalf("ToRESTError() Err.Code = %v, want %v", restErr.Err.Code, tc.wantHTTPStatus)
			}
			if restErr.Err.Status != tc.wantGRPCStatus {
				t.Fatalf("ToRESTError() Err.Status = %v, want %v", restErr.Err.Status, tc.wantGRPCStatus)
			}

			jsonBytes, err := json.Marshal(restErr)
			if err != nil {
				t.Fatalf("json.Marshal() = %v, want nil", err)
			}
			var unmarshalled Error
			if err := json.Unmarshal(jsonBytes, &unmarshalled); err != nil {
				t.Fatalf("json.Unmarshal() = %v, want nil", err)
			}

			restResp := toHTTPResponse(t, &unmarshalled)
			back := FromRESTError(restResp)

			var a2aBack *a2a.Error
			if !errors.As(back, &a2aBack) {
				t.Fatalf("Expected *a2a.Error")
			}
			if diff := cmp.Diff(*tc.want, *a2aBack,
				cmpopts.EquateErrors(),
				cmpopts.IgnoreMapEntries(func(k string, _ string) bool {
					return k == "timestamp"
				}),
			); diff != "" {
				t.Fatalf("Round-trip error mismatch (+got, -want):\n%s", diff)
			}
		})
	}
}

func TestFromRESTErrorEdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		contentType  string
		responseBody string
		wantError    error
		wantMessage  string
	}{
		{
			name:         "Invalid Content-Type",
			contentType:  "text/plain",
			responseBody: `not json`,
			wantError:    a2a.ErrServerError,
		},
		{
			name:         "Invalid JSON",
			contentType:  "application/json",
			responseBody: `not json`,
			wantError:    a2a.ErrParseError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			resp := &http.Response{
				Header: http.Header{"Content-Type": []string{tt.contentType}},
				Body:   io.NopCloser(bytes.NewBufferString(tt.responseBody)),
			}

			gotErr := FromRESTError(resp)

			if !errors.Is(gotErr, tt.wantError) {
				t.Fatalf("FromRESTError() error = %v, want %v", gotErr, tt.wantError)
			}

			if tt.wantMessage != "" {
				if !strings.Contains(gotErr.Error(), tt.wantMessage) {
					t.Fatalf("FromRESTError() error message %q does not contain %q", gotErr.Error(), tt.wantMessage)
				}
			}
		})
	}
}

func TestToRESTErrorEdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		err     error
		want    *Error
		wantNil bool
	}{
		{
			name:    "nil error",
			err:     nil,
			wantNil: true,
		},
		{
			name: "plain sentinel error",
			err:  a2a.ErrTaskNotFound,
			want: &Error{httpStatus: http.StatusNotFound, Err: StatusError{Code: http.StatusNotFound, Status: "NOT_FOUND", Message: "task not found"}},
		},
		{
			name: "wrapped sentinel error",
			err:  fmt.Errorf("wrapping: %w", a2a.ErrParseError),
			want: &Error{httpStatus: http.StatusBadRequest, Err: StatusError{Code: http.StatusBadRequest, Status: "INVALID_ARGUMENT", Message: "wrapping: parse error"}},
		},
		{
			name: "context canceled",
			err:  context.Canceled,
			want: &Error{httpStatus: 499, Err: StatusError{Code: 499, Status: "CANCELLED", Message: "context canceled"}},
		},
		{
			name: "context deadline exceeded",
			err:  context.DeadlineExceeded,
			want: &Error{httpStatus: http.StatusGatewayTimeout, Err: StatusError{Code: http.StatusGatewayTimeout, Status: "DEADLINE_EXCEEDED", Message: "context deadline exceeded"}},
		},
		{
			name: "unknown error",
			err:  errors.New("some unknown error"),
			want: &Error{httpStatus: http.StatusInternalServerError, Err: StatusError{Code: http.StatusInternalServerError, Status: "INTERNAL", Message: "some unknown error"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ToRESTError(tt.err, "")
			if tt.wantNil {
				if got != nil {
					t.Fatalf("ToRESTError() = %v, want nil", got)
				}
				return
			}

			if got.HTTPStatus() != tt.want.HTTPStatus() {
				t.Fatalf("ToRESTError() HTTPStatus() = %v, want %v", got.HTTPStatus(), tt.want.HTTPStatus())
			}
			if got.Err.Code != tt.want.Err.Code {
				t.Fatalf("ToRESTError() Err.Code = %v, want %v", got.Err.Code, tt.want.Err.Code)
			}
			if got.Err.Status != tt.want.Err.Status {
				t.Fatalf("ToRESTError() Err.Status = %v, want %v", got.Err.Status, tt.want.Err.Status)
			}
			if got.Err.Message != tt.want.Err.Message {
				t.Fatalf("ToRESTError() Err.Message = %v, want %v", got.Err.Message, tt.want.Err.Message)
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
		data := []byte(makeStatusBody(t, 400, "INVALID_ARGUMENT", "bad request", "INVALID_REQUEST"))
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

func toHTTPResponse(t *testing.T, restErr *Error) *http.Response {
	t.Helper()
	body, err := json.Marshal(restErr)
	if err != nil {
		t.Fatalf("failed to marshal RESTError: %v", err)
	}
	return &http.Response{
		StatusCode: restErr.HTTPStatus(),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewBuffer(body)),
	}
}

func makeStatusBody(t *testing.T, code int, status, message, reason string) string {
	t.Helper()
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
	}`, code, status, mustJSON(t, message), reason)
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}
	return string(b)
}

func errorWithErrorInfo(t *testing.T, message string, err error, reason string) *a2a.Error {
	t.Helper()
	return &a2a.Error{
		Err:     err,
		Message: message,
		TypedDetails: []*errordetails.Typed{
			errordetails.NewTyped(errordetails.ErrorInfoType, map[string]any{
				"reason":   reason,
				"domain":   a2a.ProtocolDomain,
				"metadata": map[string]string{},
			}),
		},
	}
}
