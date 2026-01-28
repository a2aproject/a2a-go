// Copyright 2026 The A2A Authors
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

	"github.com/a2aproject/a2a-go/a2a"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

var errorMappings = []struct {
	code codes.Code
	err  error
}{
	// Primary mappings (used for FromGRPCError as the first match is chosen)
	{codes.NotFound, a2a.ErrTaskNotFound},
	{codes.FailedPrecondition, a2a.ErrTaskNotCancelable},
	{codes.Unimplemented, a2a.ErrUnsupportedOperation},
	{codes.InvalidArgument, a2a.ErrInvalidParams},
	{codes.Internal, a2a.ErrInternalError},
	{codes.Unauthenticated, a2a.ErrUnauthenticated},
	{codes.PermissionDenied, a2a.ErrUnauthorized},
	{codes.Canceled, context.Canceled},
	{codes.DeadlineExceeded, context.DeadlineExceeded},

	// Secondary mappings (only used for ToGRPCError)
	{codes.NotFound, a2a.ErrAuthenticatedExtendedCardNotConfigured},
	{codes.Unimplemented, a2a.ErrPushNotificationNotSupported},
	{codes.Unimplemented, a2a.ErrMethodNotFound},
	{codes.InvalidArgument, a2a.ErrUnsupportedContentType},
	{codes.InvalidArgument, a2a.ErrInvalidRequest},
	{codes.Internal, a2a.ErrInvalidAgentResponse},
}

// ToGRPCError translates a2a errors into gRPC status errors.
func ToGRPCError(err error) error {
	if err == nil {
		return nil
	}

	// If it's already a gRPC status error, return it.
	if _, ok := status.FromError(err); ok {
		return err
	}

	code := codes.Internal
	for _, mapping := range errorMappings {
		if errors.Is(err, mapping.err) {
			code = mapping.code
			break
		}
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

// FromGRPCError translates gRPC errors into a2a errors.
func FromGRPCError(err error) error {
	if err == nil {
		return nil
	}
	s, ok := status.FromError(err)
	if !ok {
		return err
	}

	baseErr := a2a.ErrInternalError
	for _, mapping := range errorMappings {
		if s.Code() == mapping.code {
			baseErr = mapping.err
			break
		}
	}

	details := make(map[string]any)
	for _, d := range s.Details() {
		if pbStruct, ok := d.(*structpb.Struct); ok {
			for k, v := range pbStruct.AsMap() {
				details[k] = v
			}
		}
	}

	errOut := a2a.NewError(baseErr, s.Message())
	if len(details) > 0 {
		errOut.WithDetails(details)
	}
	return errOut
}
