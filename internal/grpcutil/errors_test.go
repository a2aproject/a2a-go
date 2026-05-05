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
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestGRPCErrorRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		inputError        *a2a.Error
		inputTypedDetails []*errordetails.Typed
		wantCode          codes.Code
		want              *a2a.Error
	}{
		{
			name: "TaskNotFound with details and metadata",
			inputError: a2a.NewError(a2a.ErrTaskNotFound, "task not found").WithErrorInfoMeta(map[string]string{
				"foo": "bar",
			}).WithDetails(map[string]any{
				"num": float64(123),
			}),
			wantCode: codes.NotFound,
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
			wantCode: codes.InvalidArgument,
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
				"str": "hello",
			}),
			wantCode: codes.InvalidArgument,
			want: &a2a.Error{
				Err:     a2a.ErrInvalidParams,
				Message: "invalid params",
				Details: map[string]any{
					"str": "hello",
				},
				TypedDetails: []*errordetails.Typed{
					errordetails.NewErrorInfo("INVALID_PARAMS", a2a.ProtocolDomain, map[string]string{
						"foo": "bar",
					}),
					errordetails.NewFromStruct(map[string]any{
						"str": "hello",
					}),
				},
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
				errordetails.NewFromStruct(map[string]any{
					"extra": "should not leak into details",
				}),
			},
			wantCode: codes.FailedPrecondition,
			want: &a2a.Error{
				Err:     a2a.ErrExtensionSupportRequired,
				Message: "extension support required",
				Details: map[string]any{
					"str": "hello",
				},
				TypedDetails: []*errordetails.Typed{
					errordetails.NewErrorInfo("EXTENSION_SUPPORT_REQUIRED", a2a.ProtocolDomain, map[string]string{
						"foo": "bar",
					}),
					errordetails.NewFromStruct(map[string]any{
						"str": "hello",
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
			wantCode:   codes.InvalidArgument,
		},
		{
			name: "MethodNotFound with details only",
			inputError: a2a.NewError(a2a.ErrMethodNotFound, "method not found").WithDetails(map[string]any{
				"str": "hello",
			}),
			wantCode: codes.Unimplemented,
			want: &a2a.Error{
				Err:     a2a.ErrMethodNotFound,
				Message: "method not found",
				Details: map[string]any{
					"str": "hello",
				},
				TypedDetails: []*errordetails.Typed{
					errordetails.NewFromStruct(map[string]any{
						"str": "hello",
					}),
				},
			},
		},
		{
			name: "ServerError with ErrorInfo only",
			inputError: a2a.NewError(a2a.ErrServerError, "server error").WithErrorInfoMeta(map[string]string{
				"foo": "bar",
			}),
			wantCode: codes.Internal,
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
			wantCode: codes.FailedPrecondition,
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
			wantCode:   codes.FailedPrecondition,
		},
		{
			name:       "UnsupportedOperation",
			inputError: a2a.NewError(a2a.ErrUnsupportedOperation, "unsupported operation"),
			wantCode:   codes.FailedPrecondition,
		},
		{
			name:       "UnsupportedContentType",
			inputError: a2a.NewError(a2a.ErrUnsupportedContentType, "unsupported content type"),
			wantCode:   codes.InvalidArgument,
		},
		{
			name:       "InvalidAgentResponse",
			inputError: a2a.NewError(a2a.ErrInvalidAgentResponse, "invalid agent response"),
			wantCode:   codes.Internal,
		},
		{
			name:       "ExtendedCardNotConfigured",
			inputError: a2a.NewError(a2a.ErrExtendedCardNotConfigured, "extended card not configured"),
			wantCode:   codes.FailedPrecondition,
		},
		{
			name:       "VersionNotSupported",
			inputError: a2a.NewError(a2a.ErrVersionNotSupported, "version not supported"),
			wantCode:   codes.FailedPrecondition,
		},
		{
			name:       "Unauthenticated",
			inputError: a2a.NewError(a2a.ErrUnauthenticated, "unauthenticated"),
			wantCode:   codes.Unauthenticated,
		},
		{
			name:       "Unauthorized",
			inputError: a2a.NewError(a2a.ErrUnauthorized, "unauthorized"),
			wantCode:   codes.PermissionDenied,
		},
		{
			name:       "InternalError",
			inputError: a2a.NewError(a2a.ErrInternalError, "internal error"),
			wantCode:   codes.Internal,
		},
		{
			name:       "Unknown error roundtrips to Internal",
			inputError: a2a.NewError(errors.New("unknown error"), "unknown error"),
			wantCode:   codes.Internal,
			want:       a2a.NewError(a2a.ErrInternalError, "unknown error"),
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

			back := FromGRPCError(got)
			var a2aBack *a2a.Error
			if !errors.As(back, &a2aBack) {
				t.Fatalf("Expected *a2a.Error")
			}
			want := tc.want
			if want == nil {
				want = tc.inputError
			}
			if diff := cmp.Diff(*want, *a2aBack,
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
