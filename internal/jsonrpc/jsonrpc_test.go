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

package jsonrpc

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/errordetails"
	"github.com/google/go-cmp/cmp"
)

func TestJSONRPCError_RoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		checkNil     bool
		name         string
		inputError   *a2a.Error
		typedDetails []*errordetails.Typed
		// midpoint (REST response) assertions
		wantCode   int
		wantReason string
		wantMeta   map[string]string
		// final (A2A error) assertions
		wantBaseErr error
		wantMessage string
		wantDetails map[string]any
	}{
		{
			name:     "Nil error",
			checkNil: true,
		},
		{
			name: "TaskNotFound with details and metadata",
			inputError: a2a.NewError(a2a.ErrTaskNotFound, "task not found").WithErrorInfoMeta(map[string]string{
				"foo": "bar",
			}).WithDetails(map[string]any{
				"num": float64(123),
			}),
			wantCode:   -32001,
			wantReason: "TASK_NOT_FOUND",
			wantMeta: map[string]string{
				"foo": "bar",
			},
			wantBaseErr: a2a.ErrTaskNotFound,
			wantMessage: "task not found",
			wantDetails: map[string]any{
				"num": float64(123),
			},
		},
		{
			name: "Wrapped ParseError with details and metadata",
			inputError: a2a.NewError(fmt.Errorf("wrapped error: %w", a2a.ErrParseError), "wrapped parse error").WithErrorInfoMeta(map[string]string{
				"foo": "bar",
			}).WithDetails(map[string]any{
				"num": float64(123),
			}),
			wantCode:   -32700,
			wantReason: "PARSE_ERROR",
			wantMeta: map[string]string{
				"foo": "bar",
			},
			wantBaseErr: a2a.ErrParseError,
			wantMessage: "wrapped parse error",
			wantDetails: map[string]any{
				"num": float64(123),
			},
		},
		{
			name: "InvalidParams with details and metadata",
			inputError: a2a.NewError(a2a.ErrInvalidParams, "invalid params").WithErrorInfoMeta(map[string]string{
				"foo": "bar",
			}).WithDetails(map[string]any{
				"num": float64(123),
			}),
			wantCode:   -32602,
			wantReason: "INVALID_PARAMS",
			wantMeta: map[string]string{
				"foo": "bar",
			},
			wantBaseErr: a2a.ErrInvalidParams,
			wantMessage: "invalid params",
			wantDetails: map[string]any{
				"num": float64(123),
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
			wantCode:   -32008,
			wantReason: "EXTENSION_SUPPORT_REQUIRED",
			wantMeta: map[string]string{
				"foo": "bar",
			},
			wantBaseErr: a2a.ErrExtensionSupportRequired,
			wantMessage: "extension support required",
			wantDetails: map[string]any{
				"num": float64(123),
			},
		},
		{
			name:        "InvalidRequest without extra details",
			inputError:  a2a.NewError(a2a.ErrInvalidRequest, "invalid request"),
			wantCode:    -32600,
			wantReason:  "INVALID_REQUEST",
			wantBaseErr: a2a.ErrInvalidRequest,
			wantMessage: "invalid request",
		},
		{
			name: "MethodNotFound with details only",
			inputError: a2a.NewError(a2a.ErrMethodNotFound, "method not found").WithDetails(map[string]any{
				"num": float64(123),
			}),
			wantCode:    -32601,
			wantReason:  "METHOD_NOT_FOUND",
			wantBaseErr: a2a.ErrMethodNotFound,
			wantMessage: "method not found",
			wantDetails: map[string]any{
				"num": float64(123),
			},
		},
		{
			name: "ServerError with ErrorInfo only",
			inputError: a2a.NewError(a2a.ErrServerError, "server error").WithErrorInfoMeta(map[string]string{
				"foo": "bar",
			}),
			wantCode:   -32000,
			wantReason: "SERVER_ERROR",
			wantMeta: map[string]string{
				"foo": "bar",
			},
			wantBaseErr: a2a.ErrServerError,
			wantMessage: "server error",
		},
		{
			name: "TaskNotCancelable ErrorInfo override",
			inputError: a2a.NewError(a2a.ErrTaskNotCancelable, "task not cancelable").WithErrorInfoMeta(map[string]string{
				"foo": "bar",
			}).WithErrorInfoMeta(map[string]string{
				"new_key": "new_value",
			}),
			wantCode:   -32002,
			wantReason: "TASK_NOT_CANCELABLE",
			wantMeta: map[string]string{
				"new_key": "new_value",
			},
			wantBaseErr: a2a.ErrTaskNotCancelable,
			wantMessage: "task not cancelable",
		},
		{
			name:        "PushNotificationNotSupported",
			inputError:  a2a.NewError(a2a.ErrPushNotificationNotSupported, "push notification not supported"),
			wantCode:    -32003,
			wantReason:  "PUSH_NOTIFICATION_NOT_SUPPORTED",
			wantBaseErr: a2a.ErrPushNotificationNotSupported,
			wantMessage: "push notification not supported",
		},
		{
			name:        "UnsupportedOperation",
			inputError:  a2a.NewError(a2a.ErrUnsupportedOperation, "unsupported operation"),
			wantCode:    -32004,
			wantReason:  "UNSUPPORTED_OPERATION",
			wantBaseErr: a2a.ErrUnsupportedOperation,
			wantMessage: "unsupported operation",
		},
		{
			name:        "UnsupportedContentType",
			inputError:  a2a.NewError(a2a.ErrUnsupportedContentType, "unsupported content type"),
			wantCode:    -32005,
			wantReason:  "CONTENT_TYPE_NOT_SUPPORTED",
			wantBaseErr: a2a.ErrUnsupportedContentType,
			wantMessage: "unsupported content type",
		},
		{
			name:        "InvalidAgentResponse",
			inputError:  a2a.NewError(a2a.ErrInvalidAgentResponse, "invalid agent response"),
			wantCode:    -32006,
			wantReason:  "INVALID_AGENT_RESPONSE",
			wantBaseErr: a2a.ErrInvalidAgentResponse,
			wantMessage: "invalid agent response",
		},
		{
			name:        "ExtendedCardNotConfigured",
			inputError:  a2a.NewError(a2a.ErrExtendedCardNotConfigured, "extended card not configured"),
			wantCode:    -32007,
			wantReason:  "EXTENDED_AGENT_CARD_NOT_CONFIGURED",
			wantBaseErr: a2a.ErrExtendedCardNotConfigured,
			wantMessage: "extended card not configured",
		},
		{
			name:        "VersionNotSupported",
			inputError:  a2a.NewError(a2a.ErrVersionNotSupported, "version not supported"),
			wantCode:    -32009,
			wantReason:  "VERSION_NOT_SUPPORTED",
			wantBaseErr: a2a.ErrVersionNotSupported,
			wantMessage: "version not supported",
		},
		{
			name:        "Unauthenticated",
			inputError:  a2a.NewError(a2a.ErrUnauthenticated, "unauthenticated"),
			wantCode:    -31401,
			wantReason:  "UNAUTHENTICATED",
			wantBaseErr: a2a.ErrUnauthenticated,
			wantMessage: "unauthenticated",
		},
		{
			name:        "Unauthorized",
			inputError:  a2a.NewError(a2a.ErrUnauthorized, "unauthorized"),
			wantCode:    -31403,
			wantReason:  "UNAUTHORIZED",
			wantBaseErr: a2a.ErrUnauthorized,
			wantMessage: "unauthorized",
		},
		{
			name:        "InternalError",
			inputError:  a2a.NewError(a2a.ErrInternalError, "internal error"),
			wantCode:    -32603,
			wantReason:  "INTERNAL_ERROR",
			wantBaseErr: a2a.ErrInternalError,
			wantMessage: "internal error",
		},
		{
			name:        "UnknownError roundtrips to Internal",
			inputError:  a2a.NewError(errors.New("unknown error"), "unknown error"),
			wantCode:    -32603,
			wantReason:  "INTERNAL_ERROR",
			wantBaseErr: a2a.ErrInternalError,
			wantMessage: "unknown error",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if tc.typedDetails != nil {
				tc.inputError.TypedDetails = append(tc.inputError.TypedDetails, tc.typedDetails...)
			}

			if tc.checkNil {
				if err := ToJSONRPCError(nil); err != nil {
					t.Errorf("ToJSONRPCError(nil) = %v, want nil", err)
				}
				return
			}

			got := ToJSONRPCError(tc.inputError)
			if got.Code != tc.wantCode {
				t.Fatalf("ToJSONRPCError() Code = %v, want %v", got.Code, tc.wantCode)
			}
			if got.Message != tc.wantMessage {
				t.Errorf("ToJSONRPCError() Message = %v, want %v", got.Message, tc.wantMessage)
			}

			var foundErrorInfo bool
			for _, typed := range got.Data {
				if typed.TypeURL == errordetails.ErrorInfoType {
					foundErrorInfo = true
					if typed.Value["reason"] != tc.wantReason {
						t.Errorf("ErrorInfo.Reason = %v, want %v", typed.Value["reason"], tc.wantReason)
					}
					if typed.Value["domain"] != a2a.ProtocolDomain {
						t.Errorf("ErrorInfo.Domain = %v, want %v", typed.Value["domain"], a2a.ProtocolDomain)
					}
					metadata, ok := typed.Value["metadata"].(map[string]string)
					if !ok {
						t.Fatalf("ErrorInfo.Metadata = %v, want %v", typed.Value["metadata"], tc.wantMeta)
					}
					if timestamp, ok := metadata["timestamp"]; !ok || timestamp == "" {
						t.Fatalf("ErrorInfo.Metadata = %v, want %v", metadata, tc.wantMeta)
					}
					for key, value := range tc.wantMeta {
						if metadata[key] != value {
							t.Errorf("ErrorInfo.Metadata = %v, want %v", metadata, tc.wantMeta)
						}
					}
				}
			}
			if !foundErrorInfo {
				t.Fatalf("ErrorInfo not found in details")
			}

			jsonBytes, err := json.Marshal(got)
			if err != nil {
				t.Fatalf("json.Marshal() = %v, want nil", err)
			}
			var unmarshalled Error
			if err := json.Unmarshal(jsonBytes, &unmarshalled); err != nil {
				t.Fatalf("json.Unmarshal() = %v, want nil", err)
			}

			back := FromJSONRPCError(&unmarshalled)
			var a2aBack *a2a.Error
			if !errors.As(back, &a2aBack) {
				t.Fatalf("Expected *a2a.Error")
			}
			if !errors.Is(a2aBack.Err, tc.wantBaseErr) {
				t.Errorf("FromJSONRPCError() = %v, want %v", a2aBack.Err, tc.wantBaseErr)
			}
			if diff := cmp.Diff(tc.wantDetails, a2aBack.Details); diff != "" {
				t.Fatalf("Round-trip details mismatch (+got,-want):\n%s", diff)
			}
			if diff := cmp.Diff(tc.wantMessage, a2aBack.Message); diff != "" {
				t.Fatalf("Round-trip message mismatch (+got,-want):\n%s", diff)
			}
			errInfo := a2aBack.ErrorInfo()
			if domain, ok := errInfo.Value["domain"].(string); !ok || domain != a2a.ProtocolDomain {
				t.Fatalf("Round-trip ErrorInfo domain = %q, want %q", domain, a2a.ProtocolDomain)
			}
			if reason, ok := errInfo.Value["reason"].(string); !ok || reason != tc.wantReason {
				t.Fatalf("Round-trip ErrorInfo reason = %q, want %q", reason, tc.wantReason)
			}
			metadata, ok := errInfo.Value["metadata"].(map[string]string)
			if !ok {
				t.Fatalf("Round-trip ErrorInfo metadata not found or wrong type: %v", errInfo.Value["metadata"])
			}
			if timestamp, ok := metadata["timestamp"]; !ok || timestamp == "" {
				t.Fatalf("Round-trip ErrorInfo metadata timestamp not found or empty: %v", metadata)
			}
			for key, value := range tc.wantMeta {
				if metadata[key] != value {
					t.Errorf("Round-trip ErrorInfo metadata = %v, want %v", metadata, tc.wantMeta)
				}
			}

			if tc.wantDetails != nil {
				foundDetailsInTypedDetails := false
				structCount := 0
				for _, td := range a2aBack.TypedDetails {
					if td.TypeURL != errordetails.StructType {
						continue
					}
					structCount++
					if cmp.Equal(tc.wantDetails, td.Value) {
						foundDetailsInTypedDetails = true
					}
				}
				if !foundDetailsInTypedDetails {
					t.Fatalf("Round-trip TypedDetails missing struct with expected details")
				}
				wantStructCount := 1
				if tc.typedDetails != nil {
					for _, td := range tc.typedDetails {
						if td.TypeURL == errordetails.StructType {
							wantStructCount++
						}
					}
				}
				if structCount != wantStructCount {
					t.Fatalf("Round-trip TypedDetails struct count = %d, want %d", structCount, wantStructCount)
				}
			}
		})
	}
}
