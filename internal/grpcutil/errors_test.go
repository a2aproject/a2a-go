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

package grpcutil

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/errordetails"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestGRPCErrorRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		inputError        *a2a.Error
		inputTypedDetails []*errordetails.Typed
		// midpoint (gRPC status) assertions
		wantCode   codes.Code
		wantReason string
		wantMeta   map[string]string
		// end point (a2a error) assertions
		wantBaseErr error
		wantMessage string
		wantDetails map[string]any
	}{
		{
			name: "TaskNotFound with details and metadata",
			inputError: a2a.NewError(a2a.ErrTaskNotFound, "task not found").WithErrorInfoMeta(map[string]string{
				"foo": "bar",
			}).WithDetails(map[string]any{
				"num": float64(123),
			}),
			wantCode:   codes.NotFound,
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
			wantCode:   codes.InvalidArgument,
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
				"str": "hello",
			}),
			wantCode:   codes.InvalidArgument,
			wantReason: "INVALID_PARAMS",
			wantMeta: map[string]string{
				"foo": "bar",
			},
			wantBaseErr: a2a.ErrInvalidParams,
			wantMessage: "invalid params",
			wantDetails: map[string]any{
				"str": "hello",
			},
		},
		{
			name: "ExtensionSupportRequired with details, typed details and metadata",
			inputError: a2a.NewError(a2a.ErrExtensionSupportRequired, "extension support required").WithErrorInfoMeta(map[string]string{
				"foo": "bar",
			}).WithDetails(map[string]any{
				"str": "hello",
			}),
			inputTypedDetails: []*errordetails.Typed{
				errordetails.NewTyped("google.protobuf.Struct", map[string]any{
					"extra": "should not leak into details",
				}),
			},
			wantCode:   codes.FailedPrecondition,
			wantReason: "EXTENSION_SUPPORT_REQUIRED",
			wantMeta: map[string]string{
				"foo": "bar",
			},
			wantBaseErr: a2a.ErrExtensionSupportRequired,
			wantMessage: "extension support required",
			wantDetails: map[string]any{
				"str": "hello",
			},
		},
		{
			name:        "InvalidRequest without extra details",
			inputError:  a2a.NewError(a2a.ErrInvalidRequest, "invalid request"),
			wantCode:    codes.InvalidArgument,
			wantReason:  "INVALID_REQUEST",
			wantBaseErr: a2a.ErrInvalidRequest,
			wantMessage: "invalid request",
		},
		{
			name: "MethodNotFound with details only",
			inputError: a2a.NewError(a2a.ErrMethodNotFound, "method not found").WithDetails(map[string]any{
				"str": "hello",
			}),
			wantCode:    codes.Unimplemented,
			wantReason:  "METHOD_NOT_FOUND",
			wantBaseErr: a2a.ErrMethodNotFound,
			wantMessage: "method not found",
			wantDetails: map[string]any{
				"str": "hello",
			},
		},
		{
			name: "ServerError with ErrorInfo only",
			inputError: a2a.NewError(a2a.ErrServerError, "server error").WithErrorInfoMeta(map[string]string{
				"foo": "bar",
			}),
			wantCode:   codes.Internal,
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
			wantCode:    codes.FailedPrecondition,
			wantReason:  "TASK_NOT_CANCELABLE",
			wantBaseErr: a2a.ErrTaskNotCancelable,
			wantMessage: "task not cancelable",
			wantMeta: map[string]string{
				"new_key": "new_value",
			},
		},
		{
			name:        "PushNotificationNotSupported",
			inputError:  a2a.NewError(a2a.ErrPushNotificationNotSupported, "push notification not supported"),
			wantCode:    codes.FailedPrecondition,
			wantReason:  "PUSH_NOTIFICATION_NOT_SUPPORTED",
			wantBaseErr: a2a.ErrPushNotificationNotSupported,
			wantMessage: "push notification not supported",
		},
		{
			name:        "UnsupportedOperation",
			inputError:  a2a.NewError(a2a.ErrUnsupportedOperation, "unsupported operation"),
			wantCode:    codes.FailedPrecondition,
			wantReason:  "UNSUPPORTED_OPERATION",
			wantBaseErr: a2a.ErrUnsupportedOperation,
			wantMessage: "unsupported operation",
		},
		{
			name:        "UnsupportedContentType",
			inputError:  a2a.NewError(a2a.ErrUnsupportedContentType, "unsupported content type"),
			wantCode:    codes.InvalidArgument,
			wantReason:  "CONTENT_TYPE_NOT_SUPPORTED",
			wantBaseErr: a2a.ErrUnsupportedContentType,
			wantMessage: "unsupported content type",
		},
		{
			name:        "InvalidAgentResponse",
			inputError:  a2a.NewError(a2a.ErrInvalidAgentResponse, "invalid agent response"),
			wantCode:    codes.Internal,
			wantReason:  "INVALID_AGENT_RESPONSE",
			wantBaseErr: a2a.ErrInvalidAgentResponse,
			wantMessage: "invalid agent response",
		},
		{
			name:        "ExtendedCardNotConfigured",
			inputError:  a2a.NewError(a2a.ErrExtendedCardNotConfigured, "extended card not configured"),
			wantCode:    codes.FailedPrecondition,
			wantReason:  "EXTENDED_AGENT_CARD_NOT_CONFIGURED",
			wantBaseErr: a2a.ErrExtendedCardNotConfigured,
			wantMessage: "extended card not configured",
		},
		{
			name:        "VersionNotSupported",
			inputError:  a2a.NewError(a2a.ErrVersionNotSupported, "version not supported"),
			wantCode:    codes.FailedPrecondition,
			wantReason:  "VERSION_NOT_SUPPORTED",
			wantBaseErr: a2a.ErrVersionNotSupported,
			wantMessage: "version not supported",
		},
		{
			name:        "Unauthenticated",
			inputError:  a2a.NewError(a2a.ErrUnauthenticated, "unauthenticated"),
			wantCode:    codes.Unauthenticated,
			wantReason:  "UNAUTHENTICATED",
			wantBaseErr: a2a.ErrUnauthenticated,
			wantMessage: "unauthenticated",
		},
		{
			name:        "Unauthorized",
			inputError:  a2a.NewError(a2a.ErrUnauthorized, "unauthorized"),
			wantCode:    codes.PermissionDenied,
			wantReason:  "UNAUTHORIZED",
			wantBaseErr: a2a.ErrUnauthorized,
			wantMessage: "unauthorized",
		},
		{
			name:        "InternalError",
			inputError:  a2a.NewError(a2a.ErrInternalError, "internal error"),
			wantCode:    codes.Internal,
			wantReason:  "INTERNAL_ERROR",
			wantBaseErr: a2a.ErrInternalError,
			wantMessage: "internal error",
		},
		{
			name:        "Unknown error roundtrips to Internal",
			inputError:  a2a.NewError(errors.New("unknown error"), "unknown error"),
			wantCode:    codes.Internal,
			wantReason:  "INTERNAL_ERROR",
			wantBaseErr: a2a.ErrInternalError,
			wantMessage: "unknown error",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if tc.inputTypedDetails != nil {
				tc.inputError.TypedDetails = append(tc.inputError.TypedDetails, tc.inputTypedDetails...)
			}
			got := ToGRPCError(tc.inputError)
			st, ok := status.FromError(got)
			if !ok {
				t.Fatalf("Expected gRPC status error")
			}
			if st.Code() != tc.wantCode {
				t.Fatalf("ToGRPCError() code = %v, want %v", st.Code(), tc.wantCode)
			}
			if st.Message() != tc.wantMessage {
				t.Fatalf("ToGRPCError() message = %q, want %q", st.Message(), tc.wantMessage)
			}

			var foundErrorInfo bool
			for _, d := range st.Details() {
				if v, ok := d.(*errdetails.ErrorInfo); ok {
					foundErrorInfo = true
					if v.Reason != tc.wantReason {
						t.Errorf("ErrorInfo.Reason = %q, want %q", v.Reason, tc.wantReason)
					}
					if v.Domain != a2a.PROTOCOL_DOMAIN {
						t.Errorf("ErrorInfo.Domain = %q, want %q", v.Domain, a2a.PROTOCOL_DOMAIN)
					}
					if diff := cmp.Diff(tc.wantMeta, v.Metadata); diff != "" {
						t.Errorf("ErrorInfo.Metadata mismatch (+got,-want):\n%s", diff)
					}
				}
			}
			if !foundErrorInfo {
				t.Errorf("ErrorInfo not found in details")
			}

			back := FromGRPCError(got)
			var a2aBack *a2a.Error
			if !errors.As(back, &a2aBack) {
				t.Fatalf("Expected *a2a.Error")
			}
			if !errors.Is(a2aBack.Err, tc.wantBaseErr) {
				t.Fatalf("Round-trip error = %v, want %v", a2aBack.Err, tc.wantBaseErr)
			}
			if diff := cmp.Diff(tc.wantDetails, a2aBack.Details); diff != "" {
				t.Fatalf("Round-trip details mismatch (+got,-want):\n%s", diff)
			}
			errInfo := a2aBack.ErrorInfo()
			if domain, ok := errInfo.Value["domain"].(string); !ok || domain != a2a.PROTOCOL_DOMAIN {
				t.Fatalf("Round-trip ErrorInfo domain = %q, want %q", domain, a2a.PROTOCOL_DOMAIN)
			}
			if reason, ok := errInfo.Value["reason"].(string); !ok || reason != tc.wantReason {
				t.Fatalf("Round-trip ErrorInfo reason = %q, want %q", reason, tc.wantReason)
			}
			if tc.wantMeta != nil {
				metadata, ok := errInfo.Value["metadata"].(map[string]string)
				if !ok {
					t.Fatalf("Round-trip ErrorInfo metadata not found or wrong type: %v", errInfo.Value["metadata"])
				}
				if diff := cmp.Diff(tc.wantMeta, metadata); diff != "" {
					t.Fatalf("Round-trip ErrorInfo metadata mismatch (+got,-want):\n%s", diff)
				}
			}

			if tc.wantDetails != nil {
				foundDetailsInTypedDetails := false
				structCount := 0
				for _, td := range a2aBack.TypedDetails {
					if td.TypeURL != "google.protobuf.Struct" {
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
				if tc.inputTypedDetails != nil {
					for _, td := range tc.inputTypedDetails {
						if td.TypeURL == "google.protobuf.Struct" {
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

func TestToGRPCErrorEdgeCases(t *testing.T) {
	t.Parallel()

	wrappedTaskNotFound := fmt.Errorf("wrapping: %w", a2a.ErrTaskNotFound)
	unknownError := errors.New("some unknown error")
	grpcError := status.Error(codes.AlreadyExists, "already there")

	tests := []struct {
		name    string
		err     error
		want    error
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
			want: status.Error(codes.NotFound, a2a.ErrTaskNotFound.Error()),
		},
		{
			name: "wrapped ErrTaskNotFound",
			err:  wrappedTaskNotFound,
			want: status.Error(codes.NotFound, wrappedTaskNotFound.Error()),
		},
		{
			name: "context canceled",
			err:  context.Canceled,
			want: status.Error(codes.Canceled, context.Canceled.Error()),
		},
		{
			name: "context deadline exceeded",
			err:  context.DeadlineExceeded,
			want: status.Error(codes.DeadlineExceeded, context.DeadlineExceeded.Error()),
		},
		{
			name: "unknown error",
			err:  unknownError,
			want: status.Error(codes.Internal, unknownError.Error()),
		},
		{
			name: "structpb conversion failure",
			err:  a2a.NewError(errors.New("bad details"), "oops").WithDetails(map[string]any{"func": func() {}}),
			want: status.Error(codes.Internal, "oops"),
		},
		{
			name: "already a grpc error",
			err:  grpcError,
			want: grpcError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ToGRPCError(tt.err)
			if tt.wantNil {
				if got != nil {
					t.Fatalf("ToGRPCError() = %v, want nil", got)
				}
				return
			}

			if got.Error() != tt.want.Error() {
				t.Fatalf("ToGRPCError() = %v, want %v", got, tt.want)
			}
			gotSt, _ := status.FromError(got)
			wantSt, _ := status.FromError(tt.want)

			if gotSt.Code() != wantSt.Code() {
				t.Fatalf("ToGRPCError() code = %v, want %v", gotSt.Code(), wantSt.Code())
			}
			if gotSt.Message() != wantSt.Message() {
				t.Fatalf("ToGRPCError() message = %q, want %q", gotSt.Message(), wantSt.Message())
			}
		})
	}
}

func TestFromGRPCErrorEdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want error
	}{
		{
			name: "nil error",
			err:  nil,
			want: nil,
		},
		{
			name: "non-grpc error",
			err:  errors.New("simple error"),
			want: errors.New("simple error"), // Should return as is
		},
		{
			name: "Unknown code -> ErrInternalError",
			err:  status.Error(codes.Unknown, "unknown"),
			want: a2a.NewError(a2a.ErrInternalError, "unknown"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := FromGRPCError(tc.err)
			if tc.want == nil {
				if got != nil {
					t.Errorf("FromGRPCError() = %v, want nil", got)
				}
				return
			}
			// For non-grpc error check identity or equality
			if _, ok := status.FromError(tc.err); !ok {
				if got.Error() != tc.want.Error() {
					t.Errorf("FromGRPCError() = %v, want %v", got, tc.want)
				}
				return
			}

			// Check primary error mapping
			var wantErr error
			if a2aErr, ok := tc.want.(*a2a.Error); ok {
				wantErr = a2aErr.Err
			} else {
				wantErr = tc.want
			}

			// Extract inner error if got is a2a.Error
			gotBaseErr := got
			if a2aErr, ok := got.(*a2a.Error); ok {
				gotBaseErr = a2aErr.Err
			}

			if !errors.Is(gotBaseErr, wantErr) {
				t.Errorf("FromGRPCError() base error = %v, want %v", gotBaseErr, wantErr)
			}
		})
	}
}
