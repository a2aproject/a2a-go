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

package a2agrpc

import (
	"errors"
	"fmt"
	"testing"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestToGRPCError(t *testing.T) {
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
			name: "ErrTaskNotFound",
			err:  a2a.ErrTaskNotFound,
			want: status.Error(codes.NotFound, a2a.ErrTaskNotFound.Error()),
		},
		{
			name: "wrapped ErrTaskNotFound",
			err:  wrappedTaskNotFound,
			want: status.Error(codes.NotFound, wrappedTaskNotFound.Error()),
		},
		{
			name: "ErrTaskNotCancelable",
			err:  a2a.ErrTaskNotCancelable,
			want: status.Error(codes.FailedPrecondition, a2a.ErrTaskNotCancelable.Error()),
		},
		{
			name: "ErrPushNotificationNotSupported",
			err:  a2a.ErrPushNotificationNotSupported,
			want: status.Error(codes.Unimplemented, a2a.ErrPushNotificationNotSupported.Error()),
		},
		{
			name: "ErrUnsupportedOperation",
			err:  a2a.ErrUnsupportedOperation,
			want: status.Error(codes.Unimplemented, a2a.ErrUnsupportedOperation.Error()),
		},
		{
			name: "ErrUnsupportedContentType",
			err:  a2a.ErrUnsupportedContentType,
			want: status.Error(codes.InvalidArgument, a2a.ErrUnsupportedContentType.Error()),
		},
		{
			name: "ErrInvalidRequest",
			err:  a2a.ErrInvalidRequest,
			want: status.Error(codes.InvalidArgument, a2a.ErrInvalidRequest.Error()),
		},
		{
			name: "ErrAuthenticatedExtendedCardNotConfigured",
			err:  a2a.ErrAuthenticatedExtendedCardNotConfigured,
			want: status.Error(codes.NotFound, a2a.ErrAuthenticatedExtendedCardNotConfigured.Error()),
		},
		{
			name: "ErrInvalidAgentResponse",
			err:  a2a.ErrInvalidAgentResponse,
			want: status.Error(codes.Internal, a2a.ErrInvalidAgentResponse.Error()),
		},
		{
			name: "ErrUnimplemented",
			err:  a2asrv.ErrUnimplemented,
			want: status.Error(codes.Unimplemented, a2asrv.ErrUnimplemented.Error()),
		},
		{
			name: "unknown error",
			err:  unknownError,
			want: status.Error(codes.Internal, unknownError.Error()),
		},
		{
			name: "already a grpc error",
			err:  grpcError,
			want: grpcError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toGRPCError(tt.err)
			if tt.wantNil {
				if got != nil {
					t.Errorf("toGRPCError() = %v, want nil", got)
				}
				return
			}
			if got.Error() != tt.want.Error() {
				t.Errorf("toGRPCError() = %v, want %v", got, tt.want)
			}
			gotSt, _ := status.FromError(got)
			wantSt, _ := status.FromError(tt.want)
			if gotSt.Code() != wantSt.Code() {
				t.Errorf("toGRPCError() code = %v, want %v", gotSt.Code(), wantSt.Code())
			}
		})
	}
}
