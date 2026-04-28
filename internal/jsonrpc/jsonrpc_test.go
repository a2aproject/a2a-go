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
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestJSONRPCError_RoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		checkNil     bool
		name         string
		inputError   *a2a.Error
		typedDetails []*errordetails.Typed
		wantCode     int
		want         *a2a.Error
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
			wantCode: -32001,
			want: &a2a.Error{
				Err:     a2a.ErrTaskNotFound,
				Message: "task not found",
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
			wantCode: -32700,
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
			wantCode: -32602,
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
			wantCode: -32008,
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
			name:       "InvalidRequest without extra details",
			inputError: a2a.NewError(a2a.ErrInvalidRequest, "invalid request"),
			wantCode:   -32600,
			want:       errorWithErrorInfo(t, "invalid request", a2a.ErrInvalidRequest, "INVALID_REQUEST"),
		},
		{
			name: "MethodNotFound with details only",
			inputError: a2a.NewError(a2a.ErrMethodNotFound, "method not found").WithDetails(map[string]any{
				"num": float64(123),
			}),
			wantCode: -32601,
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
			wantCode: -32000,
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
			wantCode: -32002,
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
			name:       "PushNotificationNotSupported",
			inputError: a2a.NewError(a2a.ErrPushNotificationNotSupported, "push notification not supported"),
			wantCode:   -32003,
			want:       errorWithErrorInfo(t, "push notification not supported", a2a.ErrPushNotificationNotSupported, "PUSH_NOTIFICATION_NOT_SUPPORTED"),
		},
		{
			name:       "UnsupportedOperation",
			inputError: a2a.NewError(a2a.ErrUnsupportedOperation, "unsupported operation"),
			wantCode:   -32004,
			want:       errorWithErrorInfo(t, "unsupported operation", a2a.ErrUnsupportedOperation, "UNSUPPORTED_OPERATION"),
		},
		{
			name:       "UnsupportedContentType",
			inputError: a2a.NewError(a2a.ErrUnsupportedContentType, "unsupported content type"),
			wantCode:   -32005,
			want:       errorWithErrorInfo(t, "unsupported content type", a2a.ErrUnsupportedContentType, "CONTENT_TYPE_NOT_SUPPORTED"),
		},
		{
			name:       "InvalidAgentResponse",
			inputError: a2a.NewError(a2a.ErrInvalidAgentResponse, "invalid agent response"),
			wantCode:   -32006,
			want:       errorWithErrorInfo(t, "invalid agent response", a2a.ErrInvalidAgentResponse, "INVALID_AGENT_RESPONSE"),
		},
		{
			name:       "ExtendedCardNotConfigured",
			inputError: a2a.NewError(a2a.ErrExtendedCardNotConfigured, "extended card not configured"),
			wantCode:   -32007,
			want:       errorWithErrorInfo(t, "extended card not configured", a2a.ErrExtendedCardNotConfigured, "EXTENDED_AGENT_CARD_NOT_CONFIGURED"),
		},
		{
			name:       "VersionNotSupported",
			inputError: a2a.NewError(a2a.ErrVersionNotSupported, "version not supported"),
			wantCode:   -32009,
			want:       errorWithErrorInfo(t, "version not supported", a2a.ErrVersionNotSupported, "VERSION_NOT_SUPPORTED"),
		},
		{
			name:       "Unauthenticated",
			inputError: a2a.NewError(a2a.ErrUnauthenticated, "unauthenticated"),
			wantCode:   -31401,
			want:       errorWithErrorInfo(t, "unauthenticated", a2a.ErrUnauthenticated, "UNAUTHENTICATED"),
		},
		{
			name:       "Unauthorized",
			inputError: a2a.NewError(a2a.ErrUnauthorized, "unauthorized"),
			wantCode:   -31403,
			want:       errorWithErrorInfo(t, "unauthorized", a2a.ErrUnauthorized, "UNAUTHORIZED"),
		},
		{
			name:       "InternalError",
			inputError: a2a.NewError(a2a.ErrInternalError, "internal error"),
			wantCode:   -32603,
			want:       errorWithErrorInfo(t, "internal error", a2a.ErrInternalError, "INTERNAL_ERROR"),
		},
		{
			name:       "UnknownError roundtrips to Internal",
			inputError: a2a.NewError(errors.New("unknown error"), "unknown error"),
			wantCode:   -32603,
			want:       errorWithErrorInfo(t, "unknown error", a2a.ErrInternalError, "INTERNAL_ERROR"),
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
