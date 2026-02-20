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

package a2agrpc

import (
	"context"
	"strings"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2apb/v1"
	"github.com/a2aproject/a2a-go/internal/grpcutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/a2aproject/a2a-go/a2apb/v1/pbconv"
	"github.com/a2aproject/a2a-go/a2asrv"
)

// Handler implements protobuf translation layer and delegates the actual method handling to [a2asrv.RequestHandler].
type Handler struct {
	a2apb.UnimplementedA2AServiceServer
	handler a2asrv.RequestHandler
}

// RegisterWith registers as an A2AService implementation with the provided [grpc.Server].
func (h *Handler) RegisterWith(s *grpc.Server) {
	a2apb.RegisterA2AServiceServer(s, h)
}

// NewHandler is a [Handler] constructor function.
func NewHandler(handler a2asrv.RequestHandler) *Handler {
	return &Handler{handler: handler}
}

func (h *Handler) SendMessage(ctx context.Context, pbReq *a2apb.SendMessageRequest) (*a2apb.SendMessageResponse, error) {
	req, err := pbconv.FromProtoSendMessageRequest(pbReq)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to convert request: %v", err)
	}

	ctx, callCtx := withCallContext(ctx)
	res, err := h.handler.SendMessage(ctx, req)
	if err != nil {
		return nil, grpcutil.ToGRPCError(err)
	}
	if err := grpc.SetTrailer(ctx, toTrailer(callCtx)); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to send active extensions: %v", err)
	}

	pbRes, err := pbconv.ToProtoSendMessageResponse(res)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to convert response: %v", err)
	}

	return pbRes, nil
}

func (h *Handler) SendStreamingMessage(pbReq *a2apb.SendMessageRequest, stream grpc.ServerStreamingServer[a2apb.StreamResponse]) error {
	req, err := pbconv.FromProtoSendMessageRequest(pbReq)
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "failed to convert request: %v", err)
	}

	ctx, callCtx := withCallContext(stream.Context())
	for event, err := range h.handler.SendStreamingMessage(ctx, req) {
		if err != nil {
			return grpcutil.ToGRPCError(err)
		}
		pbResp, err := pbconv.ToProtoStreamResponse(event)
		if err != nil {
			return status.Errorf(codes.Internal, "failed to convert response: %v", err)
		}
		err = stream.Send(pbResp)
		if err != nil {
			return status.Errorf(codes.Aborted, "failed to send response: %v", err)
		}
	}
	stream.SetTrailer(toTrailer(callCtx))

	return nil
}

func (h *Handler) GetTask(ctx context.Context, pbReq *a2apb.GetTaskRequest) (*a2apb.Task, error) {
	req, err := pbconv.FromProtoGetTaskRequest(pbReq)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to convert request: %v", err)
	}

	ctx, callCtx := withCallContext(ctx)
	task, err := h.handler.GetTask(ctx, req)
	if err != nil {
		return nil, grpcutil.ToGRPCError(err)
	}
	if err := grpc.SetTrailer(ctx, toTrailer(callCtx)); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to send active extensions: %v", err)
	}

	pbTask, err := pbconv.ToProtoTask(task)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to convert task: %v", err)
	}
	return pbTask, nil
}

func (h *Handler) ListTasks(ctx context.Context, pbReq *a2apb.ListTasksRequest) (*a2apb.ListTasksResponse, error) {
	req, err := pbconv.FromProtoListTasksRequest(pbReq)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to convert request: %v", err)
	}

	ctx, callCtx := withCallContext(ctx)
	resp, err := h.handler.ListTasks(ctx, req)
	if err != nil {
		return nil, grpcutil.ToGRPCError(err)
	}
	if err := grpc.SetTrailer(ctx, toTrailer(callCtx)); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to send active extensions: %v", err)
	}

	pbResp, err := pbconv.ToProtoListTasksResponse(resp)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to convert response: %v", err)
	}
	return pbResp, nil
}

func (h *Handler) CancelTask(ctx context.Context, pbReq *a2apb.CancelTaskRequest) (*a2apb.Task, error) {
	taskID := a2a.TaskID(pbReq.GetId())

	ctx, callCtx := withCallContext(ctx)
	task, err := h.handler.CancelTask(ctx, &a2a.CancelTaskRequest{ID: taskID})
	if err != nil {
		return nil, grpcutil.ToGRPCError(err)
	}
	if err := grpc.SetTrailer(ctx, toTrailer(callCtx)); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to send active extensions: %v", err)
	}

	pbTask, err := pbconv.ToProtoTask(task)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to convert task: %v", err)
	}
	return pbTask, nil
}

func (h *Handler) SubscribeToTask(pbReq *a2apb.SubscribeToTaskRequest, stream grpc.ServerStreamingServer[a2apb.StreamResponse]) error {
	taskID := a2a.TaskID(pbReq.GetId())

	ctx, callCtx := withCallContext(stream.Context())
	for event, err := range h.handler.SubscribeToTask(ctx, &a2a.SubscribeToTaskRequest{ID: taskID}) {
		if err != nil {
			return grpcutil.ToGRPCError(err)
		}
		pbResp, err := pbconv.ToProtoStreamResponse(event)
		if err != nil {
			return status.Errorf(codes.Internal, "failed to convert response: %v", err)
		}
		err = stream.Send(pbResp)
		if err != nil {
			return status.Errorf(codes.Aborted, "failed to send response: %v", err)
		}
	}
	stream.SetTrailer(toTrailer(callCtx))

	return nil
}

func (h *Handler) CreateTaskPushNotificationConfig(ctx context.Context, pbReq *a2apb.CreateTaskPushNotificationConfigRequest) (*a2apb.TaskPushNotificationConfig, error) {
	req, err := pbconv.FromProtoCreateTaskPushConfigRequest(pbReq)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to convert request: %v", err)
	}

	ctx, callCtx := withCallContext(ctx)
	config, err := h.handler.CreateTaskPushConfig(ctx, req)
	if err != nil {
		return nil, grpcutil.ToGRPCError(err)
	}
	if err := grpc.SetTrailer(ctx, toTrailer(callCtx)); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to send active extensions: %v", err)
	}

	pbConfig, err := pbconv.ToProtoTaskPushConfig(config)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to convert push config: %v", err)
	}
	return pbConfig, nil
}

func (h *Handler) GetTaskPushNotificationConfig(ctx context.Context, pbReq *a2apb.GetTaskPushNotificationConfigRequest) (*a2apb.TaskPushNotificationConfig, error) {
	req, err := pbconv.FromProtoGetTaskPushConfigRequest(pbReq)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to convert request: %v", err)
	}

	ctx, callCtx := withCallContext(ctx)
	config, err := h.handler.GetTaskPushConfig(ctx, req)
	if err != nil {
		return nil, grpcutil.ToGRPCError(err)
	}
	if err := grpc.SetTrailer(ctx, toTrailer(callCtx)); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to send active extensions: %v", err)
	}

	pbConfig, err := pbconv.ToProtoTaskPushConfig(config)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to convert push config: %v", err)
	}
	return pbConfig, nil
}

func (h *Handler) ListTaskPushNotificationConfig(ctx context.Context, pbReq *a2apb.ListTaskPushNotificationConfigRequest) (*a2apb.ListTaskPushNotificationConfigResponse, error) {
	req, err := pbconv.FromProtoListTaskPushConfigRequest(pbReq)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to convert request: %v", err)
	}

	ctx, callCtx := withCallContext(ctx)
	configs, err := h.handler.ListTaskPushConfig(ctx, req)
	if err != nil {
		return nil, grpcutil.ToGRPCError(err)
	}
	if err := grpc.SetTrailer(ctx, toTrailer(callCtx)); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to send active extensions: %v", err)
	}

	resp := &a2a.ListTaskPushConfigResponse{
		Configs:       configs,
		NextPageToken: "",
	}

	pbResp, err := pbconv.ToProtoListTaskPushConfigResponse(resp)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to convert list of push configs: %v", err)
	}
	return pbResp, nil
}

func (h *Handler) GetExtendedAgentCard(ctx context.Context, pbReq *a2apb.GetExtendedAgentCardRequest) (*a2apb.AgentCard, error) {
	req, err := pbconv.FromProtoGetExtendedAgentCardRequest(pbReq)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to convert request: %v", err)
	}

	card, err := h.handler.GetExtendedAgentCard(ctx, req)
	if err != nil {
		return nil, grpcutil.ToGRPCError(err)
	}
	pbCard, err := pbconv.ToProtoAgentCard(card)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to convert agent card: %v", err)
	}
	return pbCard, nil
}

func (h *Handler) DeleteTaskPushNotificationConfig(ctx context.Context, pbReq *a2apb.DeleteTaskPushNotificationConfigRequest) (*emptypb.Empty, error) {
	req, err := pbconv.FromProtoDeleteTaskPushConfigRequest(pbReq)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to convert request: %v", err)
	}

	ctx, callCtx := withCallContext(ctx)
	if err := h.handler.DeleteTaskPushConfig(ctx, req); err != nil {
		return nil, grpcutil.ToGRPCError(err)
	}
	if err := grpc.SetTrailer(ctx, toTrailer(callCtx)); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to send active extensions: %v", err)
	}

	return &emptypb.Empty{}, nil
}

func withCallContext(ctx context.Context) (context.Context, *a2asrv.CallContext) {
	var svcParams *a2asrv.ServiceParams
	if meta, ok := metadata.FromIncomingContext(ctx); ok {
		svcParams = a2asrv.NewServiceParams(meta)
	}
	return a2asrv.NewCallContext(ctx, svcParams)
}

func toTrailer(callCtx *a2asrv.CallContext) metadata.MD {
	activated := callCtx.Extensions().ActivatedURIs()
	if len(activated) == 0 {
		return metadata.MD{}
	}
	return metadata.MD{strings.ToLower(a2asrv.ExtensionsMetaKey): activated}
}
