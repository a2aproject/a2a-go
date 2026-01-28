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
	"context"
	"errors"

	"github.com/a2aproject/a2a-go/a2a"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

// toGRPCError translates a2a errors into gRPC status errors.
func toGRPCError(err error) error {
	if err == nil {
		return nil
	}

	// If it's already a gRPC status error, return it.
	if _, ok := status.FromError(err); ok {
		return err
	}

	var code codes.Code
	switch {
	case errors.Is(err, a2a.ErrTaskNotFound),
		errors.Is(err, a2a.ErrAuthenticatedExtendedCardNotConfigured):
		code = codes.NotFound
	case errors.Is(err, a2a.ErrTaskNotCancelable):
		code = codes.FailedPrecondition
	case errors.Is(err, a2a.ErrPushNotificationNotSupported),
		errors.Is(err, a2a.ErrUnsupportedOperation),
		errors.Is(err, a2a.ErrMethodNotFound):
		code = codes.Unimplemented
	case errors.Is(err, a2a.ErrUnsupportedContentType),
		errors.Is(err, a2a.ErrInvalidRequest),
		errors.Is(err, a2a.ErrInvalidParams):
		code = codes.InvalidArgument
	case errors.Is(err, a2a.ErrInvalidAgentResponse):
		code = codes.Internal
	case errors.Is(err, a2a.ErrUnauthenticated):
		code = codes.Unauthenticated
	case errors.Is(err, a2a.ErrUnauthorized):
		code = codes.PermissionDenied
	case errors.Is(err, context.Canceled):
		code = codes.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		code = codes.DeadlineExceeded
	default:
		code = codes.Internal
	}

	st := status.New(code, err.Error())

	var a2aErr *a2a.Error
	if errors.As(err, &a2aErr) && len(a2aErr.Details) > 0 {
		s, err := structpb.NewStruct(a2aErr.Details)
		if err != nil {
			return st.Err()
		}
		withDetails, err := st.WithDetails(s)
		if err != nil {
			return st.Err()
		}
		st = withDetails
	}

	return st.Err()
}
