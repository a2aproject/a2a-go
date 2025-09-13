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

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2apb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/a2aproject/a2a-go/a2asrv"
)

type GrpcHandler struct {
	a2apb.UnimplementedA2AServiceServer
	handler a2asrv.RequestHandler
}

func (h *GrpcHandler) RegisterWith(s *grpc.Server) {
	a2apb.RegisterA2AServiceServer(s, h)
}

func NewHandler(handler a2asrv.RequestHandler) *GrpcHandler {
	return &GrpcHandler{handler: handler}
}

func (h *GrpcHandler) SendMessage(ctx context.Context, req *a2apb.SendMessageRequest) (*a2apb.SendMessageResponse, error) {
	if req.GetRequest() == nil {
		return nil, status.Error(codes.InvalidArgument, "request message is missing")
	}
	params, err := fromProtoSendMessageRequest(req)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to convert request: %v", err)
	}

	result, err := h.handler.OnSendMessage(ctx, params)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "handler failed: %v", err)
	}

	resp, err := toProtoSendMessageResponse(result)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to convert response: %v", err)
	}

	return resp, nil
}

func (h *GrpcHandler) SendStreamingMessage(req *a2apb.SendMessageRequest, stream grpc.ServerStreamingServer[a2apb.StreamResponse]) error {
	if req.GetRequest() == nil {
		return status.Error(codes.InvalidArgument, "request message is missing")
	}
	params, err := fromProtoSendMessageRequest(req)
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "failed to convert request: %v", err)
	}

	for event, err := range h.handler.OnSendMessageStream(stream.Context(), params) {
		if err != nil {
			return status.Errorf(codes.Internal, "handler failed: %v", err)
		}
		resp, err := toProtoStreamResponse(event)
		if err != nil {
			return status.Errorf(codes.Internal, "failed to convert response: %v", err)
		}
		err = stream.Send(resp)
		if err != nil {
			return status.Errorf(codes.Aborted, "failed to send response: %v", err)
		}
	}

	return nil
}

func (h *GrpcHandler) GetTask(ctx context.Context, req *a2apb.GetTaskRequest) (*a2apb.Task, error) {
	params, err := fromProtoGetTaskRequest(req)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to convert request: %v", err)
	}
	task, err := h.handler.OnGetTask(ctx, params)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "handler failed: %v", err)
	}
	return toProtoTask(&task)
}

func (h *GrpcHandler) CancelTask(ctx context.Context, req *a2apb.CancelTaskRequest) (*a2apb.Task, error) {
	taskID, err := extractTaskID(req.GetName())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to extract task id: %v", err)
	}
	task, err := h.handler.OnCancelTask(ctx, a2a.TaskIDParams{ID: taskID})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "handler failed: %v", err)
	}
	return toProtoTask(&task)
}

func (h *GrpcHandler) TaskSubscription(req *a2apb.TaskSubscriptionRequest, stream grpc.ServerStreamingServer[a2apb.StreamResponse]) error {
	taskID, err := extractTaskID(req.GetName())
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "failed to extract task id: %v", err)
	}
	for event, err := range h.handler.OnResubscribeToTask(stream.Context(), a2a.TaskIDParams{ID: taskID}) {
		if err != nil {
			return status.Errorf(codes.Internal, "handler failed: %v", err)
		}
		resp, err := toProtoStreamResponse(event)
		if err != nil {
			return status.Errorf(codes.Internal, "failed to convert response: %v", err)
		}
		err = stream.Send(resp)
		if err != nil {
			return status.Errorf(codes.Aborted, "failed to send response: %v", err)
		}
	}
	return nil
}

func (h *GrpcHandler) CreateTaskPushNotificationConfig(ctx context.Context, req *a2apb.CreateTaskPushNotificationConfigRequest) (*a2apb.TaskPushNotificationConfig, error) {
	params, err := fromProtoCreateTaskPushConfigRequest(req)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to convert request: %v", err)
	}
	config, err := h.handler.OnSetTaskPushConfig(ctx, params)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "handler failed: %v", err)
	}
	return toProtoTaskPushConfig(&config)
}

func (h *GrpcHandler) GetTaskPushNotificationConfig(ctx context.Context, req *a2apb.GetTaskPushNotificationConfigRequest) (*a2apb.TaskPushNotificationConfig, error) {
	params, err := fromProtoGetTaskPushConfigRequest(req)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to convert request: %v", err)
	}
	config, err := h.handler.OnGetTaskPushConfig(ctx, params)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "handler failed: %v", err)
	}
	return toProtoTaskPushConfig(&config)
}

func (h *GrpcHandler) ListTaskPushNotificationConfig(ctx context.Context, req *a2apb.ListTaskPushNotificationConfigRequest) (*a2apb.ListTaskPushNotificationConfigResponse, error) {
	taskID, err := extractTaskID(req.GetParent())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to extract task id: %v", err)
	}
	// todo: handling pagination
	configs, err := h.handler.OnListTaskPushConfig(ctx, a2a.ListTaskPushConfigParams{TaskID: taskID})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "handler failed: %v", err)
	}
	return toProtoListTaskPushConfig(configs)
}

func (h *GrpcHandler) GetAgentCard(ctx context.Context, req *a2apb.GetAgentCardRequest) (*a2apb.AgentCard, error) {
	// todo: add a way to get agent card
	// h.cardProvider.GetAgentCard(ctx)
	return nil, nil
}

func (h *GrpcHandler) DeleteTaskPushNotificationConfig(ctx context.Context, req *a2apb.DeleteTaskPushNotificationConfigRequest) (*emptypb.Empty, error) {
	params, err := fromProtoDeleteTaskPushConfigRequest(req)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to convert request: %v", err)
	}
	if err := h.handler.OnDeleteTaskPushConfig(ctx, params); err != nil {
		return nil, status.Errorf(codes.Internal, "handler failed: %v", err)
	}
	return &emptypb.Empty{}, nil
}
