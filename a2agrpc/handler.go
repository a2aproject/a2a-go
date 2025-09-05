package a2agrpc

import (
	"context"

	"github.com/a2aproject/a2a-go/a2apb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/a2aproject/a2a-go/a2asrv"
)

type grpcHandler struct {
	a2apb.UnimplementedA2AServiceServer
	handler a2asrv.RequestHandler
}

func (h *grpcHandler) RegisterWith(s *grpc.Server) {
	a2apb.RegisterA2AServiceServer(s, h)
}

func NewHandler(handler a2asrv.RequestHandler) *grpcHandler {
	return &grpcHandler{handler: handler}
}

func (h *grpcHandler) SendMessage(ctx context.Context, req *a2apb.SendMessageRequest) (*a2apb.SendMessageResponse, error) {
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
