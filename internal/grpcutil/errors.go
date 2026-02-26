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

	"github.com/a2aproject/a2a-go/v1/a2a"
	"github.com/a2aproject/a2a-go/v1/log"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/protoadapt"
	"google.golang.org/protobuf/types/known/structpb"
)

var errorMappings = []struct {
	code   codes.Code
	err    error
	reason string
}{
	// Primary mappings (used for FromGRPCError as the first match is chosen)
	{codes.NotFound, a2a.ErrTaskNotFound, "TASK_NOT_FOUND"},
	{codes.FailedPrecondition, a2a.ErrTaskNotCancelable, "TASK_NOT_CANCELABLE"},
	{codes.Unimplemented, a2a.ErrUnsupportedOperation, "UNSUPPORTED_OPERATION"},
	{codes.InvalidArgument, a2a.ErrInvalidParams, "INVALID_PARAMS"},
	{codes.Internal, a2a.ErrInternalError, "INTERNAL_ERROR"},
	{codes.Unauthenticated, a2a.ErrUnauthenticated, "UNAUTHENTICATED"},
	{codes.PermissionDenied, a2a.ErrUnauthorized, "UNAUTHORIZED"},
	{codes.Canceled, context.Canceled, "INTERNAL_ERROR"},
	{codes.DeadlineExceeded, context.DeadlineExceeded, "INTERNAL_ERROR"},

	// Secondary mappings (only used for ToGRPCError)
	{codes.FailedPrecondition, a2a.ErrExtendedCardNotConfigured, "EXTENDED_AGENT_CARD_NOT_CONFIGURED"},
	{codes.Unimplemented, a2a.ErrPushNotificationNotSupported, "PUSH_NOTIFICATION_NOT_SUPPORTED"},
	{codes.Unimplemented, a2a.ErrMethodNotFound, "METHOD_NOT_FOUND"},
	{codes.InvalidArgument, a2a.ErrUnsupportedContentType, "UNSUPPORTED_CONTENT_TYPE"},
	{codes.InvalidArgument, a2a.ErrInvalidRequest, "INVALID_REQUEST"},
	{codes.Internal, a2a.ErrInvalidAgentResponse, "INVALID_AGENT_RESPONSE"},

	// Additional mappings from a2a/errors.go
	{codes.FailedPrecondition, a2a.ErrExtensionSupportRequired, "EXTENSION_SUPPORT_REQUIRED"},
	{codes.Unimplemented, a2a.ErrVersionNotSupported, "VERSION_NOT_SUPPORTED"},
	{codes.InvalidArgument, a2a.ErrParseError, "PARSE_ERROR"},
	{codes.Internal, a2a.ErrServerError, "SERVER_ERROR"},
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
			reason = mapping.reason
			break
		}
	}

	st := status.New(code, err.Error())

	additionalMeta := map[string]any{}
	errInfoMeta := map[string]string{}
	var a2aErr *a2a.Error
	if errors.As(err, &a2aErr) && len(a2aErr.Details) > 0 {
		for k, v := range a2aErr.Details {
			if s, ok := v.(string); ok {
				errInfoMeta[k] = s
			} else {
				additionalMeta[k] = v
			}
		}
	}

	errInfo := &errdetails.ErrorInfo{Reason: reason, Domain: "a2a-protocol.org"}
	if len(errInfoMeta) > 0 {
		errInfo.Metadata = errInfoMeta
	}

	var messages = []protoadapt.MessageV1{errInfo}
	if len(additionalMeta) > 0 {
		s, err := structpb.NewStruct(additionalMeta)
		if err == nil {
			messages = append(messages, s)
		} else {
			log.Warn(context.Background(), "failed to convert error meta to proto", "error", err, "meta", additionalMeta)
		}
	}

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

	baseErr := a2a.ErrInternalError
	for _, mapping := range errorMappings {
		if s.Code() == mapping.code {
			baseErr = mapping.err
			break
		}
	}

	details := make(map[string]any)
	for _, d := range s.Details() {
		switch v := d.(type) {
		case *errdetails.ErrorInfo:
			for k, val := range v.Metadata {
				details[k] = val
			}
		case *structpb.Struct:
			maps.Copy(details, v.AsMap())
		}
	}

	errOut := a2a.NewError(baseErr, s.Message())
	if len(details) > 0 {
		errOut = errOut.WithDetails(details)
	}
	return errOut
}
