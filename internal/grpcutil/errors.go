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

// Package grpcutil provides gRPC utility functions for A2A.
package grpcutil

import (
	"context"
	"errors"
	"maps"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/errordetails"
	"github.com/a2aproject/a2a-go/v2/internal/utils"
	"github.com/a2aproject/a2a-go/v2/log"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/protoadapt"
	"google.golang.org/protobuf/types/known/structpb"
)

var errorMappings = []struct {
	code codes.Code
	err  error
}{
	// Primary mappings (used for FromGRPCError as the first match is chosen)
	{codes.NotFound, a2a.ErrTaskNotFound},
	{codes.FailedPrecondition, a2a.ErrTaskNotCancelable},
	{codes.FailedPrecondition, a2a.ErrUnsupportedOperation},
	{codes.InvalidArgument, a2a.ErrInvalidParams},
	{codes.Internal, a2a.ErrInternalError},
	{codes.Unauthenticated, a2a.ErrUnauthenticated},
	{codes.PermissionDenied, a2a.ErrUnauthorized},
	{codes.Canceled, context.Canceled},
	{codes.DeadlineExceeded, context.DeadlineExceeded},

	// Secondary mappings (only used for ToGRPCError)
	{codes.FailedPrecondition, a2a.ErrExtendedCardNotConfigured},
	{codes.FailedPrecondition, a2a.ErrPushNotificationNotSupported},
	{codes.Unimplemented, a2a.ErrMethodNotFound},
	{codes.InvalidArgument, a2a.ErrUnsupportedContentType},
	{codes.InvalidArgument, a2a.ErrInvalidRequest},
	{codes.Internal, a2a.ErrInvalidAgentResponse},

	// Additional mappings from a2a/errors.go
	{codes.FailedPrecondition, a2a.ErrExtensionSupportRequired},
	{codes.FailedPrecondition, a2a.ErrVersionNotSupported},
	{codes.InvalidArgument, a2a.ErrParseError},
	{codes.Internal, a2a.ErrServerError},
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
	reason := "INTERNAL_ERROR"
	for _, mapping := range errorMappings {
		if errors.Is(err, mapping.err) {
			code = mapping.code
			reason = a2a.ErrorReason(mapping.err)
			break
		}
	}

	st := status.New(code, err.Error())
	var errInfoMeta map[string]string
	var a2aErr *a2a.Error
	var messages []protoadapt.MessageV1

	if errors.As(err, &a2aErr) {
		if len(a2aErr.Details) > 0 {
			s, err := structpb.NewStruct(a2aErr.Details)
			if err == nil {
				messages = append(messages, s)
			} else {
				log.Warn(context.Background(), "failed to convert error meta to proto", "error", err, "meta", a2aErr.Details)
			}
		}
		for _, d := range a2aErr.TypedDetails {
			if d.TypeURL == errordetails.ErrorInfoType {
				if rawMeta, ok := d.Value["metadata"]; ok {
					if m, ok := utils.ToStringMap(rawMeta); ok {
						errInfoMeta = m
					}
				}
			} else {
				s, err := structpb.NewStruct(d.Value)
				if err != nil {
					log.Warn(context.Background(), "failed to convert error meta to proto", "error", err, "meta", d.Value)
					continue
				}
				messages = append(messages, s)
			}
		}
	}

	errInfo := &errdetails.ErrorInfo{Reason: reason, Domain: a2a.ProtocolDomain}
	if len(errInfoMeta) > 0 {
		errInfo.Metadata = errInfoMeta
	}
	messages = append(messages, errInfo)

	withDetails, err := st.WithDetails(messages...)
	if err != nil {
		log.Warn(context.Background(), "failed to attach details to gRPC error", "error", err)
		return st.Err()
	}
	return withDetails.Err()
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

	var reason string
	var typedDetails []*errordetails.Typed
	errInfoMeta := make(map[string]string)
	details := make(map[string]any)
	firstStruct := true

	for _, d := range s.Details() {
		switch v := d.(type) {
		case *errdetails.ErrorInfo:
			reason = v.Reason
			maps.Copy(errInfoMeta, v.Metadata)
		case *structpb.Struct:
			// Add the first struct to Err.Details.
			if firstStruct {
				maps.Copy(details, v.AsMap())
				firstStruct = false
			}
			// Add every struct including the first one to Err.TypedDetails.
			typedDetails = append(typedDetails, errordetails.NewFromStruct(v.AsMap()))
		}
	}

	baseErr := a2a.ErrInternalError
	if reason != "" {
		for _, mapping := range errorMappings {
			if a2a.ErrorReason(mapping.err) == reason && s.Code() == mapping.code {
				baseErr = mapping.err
				break
			}
		}
	} else {
		for _, mapping := range errorMappings {
			if s.Code() == mapping.code {
				baseErr = mapping.err
				break
			}
		}
	}

	errOut := a2a.NewError(baseErr, s.Message())
	if len(errInfoMeta) > 0 {
		errOut = errOut.WithErrorInfoMeta(errInfoMeta)
	}
	if len(details) > 0 {
		errOut = errOut.WithDetails(details)
	}
	errOut.TypedDetails = append(errOut.TypedDetails, typedDetails...)

	return errOut
}
