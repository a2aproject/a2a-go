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
	"io"
	"iter"
	"strings"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2aclient"
	"github.com/a2aproject/a2a-go/a2apb/v1"
	"github.com/a2aproject/a2a-go/a2apb/v1/pbconv"
	"github.com/a2aproject/a2a-go/internal/grpcutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// WithGRPCTransport create a gRPC transport implementation which will use the provided [grpc.DialOption]s during connection establishment.
func WithGRPCTransport(opts ...grpc.DialOption) a2aclient.FactoryOption {
	return a2aclient.WithTransport(
		a2a.TransportProtocolGRPC,
		a2aclient.TransportFactoryFn(func(ctx context.Context, card *a2a.AgentCard, iface *a2a.AgentInterface) (a2aclient.Transport, error) {
			conn, err := grpc.NewClient(iface.URL, opts...)
			if err != nil {
				return nil, err
			}
			return NewGRPCTransport(conn), nil
		}),
	)
}

// NewGRPCTransport exposes a method for direct A2A gRPC protocol handler.
func NewGRPCTransport(conn *grpc.ClientConn) a2aclient.Transport {
	return &grpcTransport{
		client:      a2apb.NewA2AServiceClient(conn),
		closeConnFn: func() error { return conn.Close() },
	}
}

// NewGRPCTransportFromClient creates a gRPC transport where the connection is managed
// externally and encapsulated in the service client. The transport's Destroy method is a no-op.
func NewGRPCTransportFromClient(client a2apb.A2AServiceClient) a2aclient.Transport {
	return &grpcTransport{
		client:      client,
		closeConnFn: func() error { return nil },
	}
}

// grpcTransport implements Transport by delegating to a2apb.A2AServiceClient.
type grpcTransport struct {
	client      a2apb.A2AServiceClient
	closeConnFn func() error
}

var _ a2aclient.Transport = (*grpcTransport)(nil)

// A2A protocol methods

func (c *grpcTransport) GetTask(ctx context.Context, params a2aclient.ServiceParams, req *a2a.GetTaskRequest) (*a2a.Task, error) {
	pbReq, err := pbconv.ToProtoGetTaskRequest(req)
	if err != nil {
		return nil, err
	}

	pbTask, err := c.client.GetTask(withGRPCMetadata(ctx, params), pbReq)
	if err != nil {
		return nil, grpcutil.FromGRPCError(err)
	}

	return pbconv.FromProtoTask(pbTask)
}

func (c *grpcTransport) ListTasks(ctx context.Context, params a2aclient.ServiceParams, req *a2a.ListTasksRequest) (*a2a.ListTasksResponse, error) {
	pbReq, err := pbconv.ToProtoListTasksRequest(req)
	if err != nil {
		return nil, err
	}

	pbResp, err := c.client.ListTasks(withGRPCMetadata(ctx, params), pbReq)
	if err != nil {
		return nil, err
	}

	return pbconv.FromProtoListTasksResponse(pbResp)
}

func (c *grpcTransport) CancelTask(ctx context.Context, params a2aclient.ServiceParams, req *a2a.CancelTaskRequest) (*a2a.Task, error) {
	pbReq, err := pbconv.ToProtoCancelTaskRequest(req)
	if err != nil {
		return nil, err
	}

	pbTask, err := c.client.CancelTask(withGRPCMetadata(ctx, params), pbReq)
	if err != nil {
		return nil, grpcutil.FromGRPCError(err)
	}

	return pbconv.FromProtoTask(pbTask)
}

func (c *grpcTransport) SendMessage(ctx context.Context, params a2aclient.ServiceParams, req *a2a.SendMessageRequest) (a2a.SendMessageResult, error) {
	pbReq, err := pbconv.ToProtoSendMessageRequest(req)
	if err != nil {
		return nil, err
	}

	pbResp, err := c.client.SendMessage(withGRPCMetadata(ctx, params), pbReq)
	if err != nil {
		return nil, grpcutil.FromGRPCError(err)
	}

	return pbconv.FromProtoSendMessageResponse(pbResp)
}

func (c *grpcTransport) SubscribeToTask(ctx context.Context, params a2aclient.ServiceParams, req *a2a.SubscribeToTaskRequest) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		pbReq, err := pbconv.ToProtoSubscribeToTaskRequest(req)
		if err != nil {
			yield(nil, err)
			return
		}

		stream, err := c.client.SubscribeToTask(withGRPCMetadata(ctx, params), pbReq)
		if err != nil {
			yield(nil, grpcutil.FromGRPCError(err))
			return
		}

		drainEventStream(stream, yield)
	}
}

func (c *grpcTransport) SendStreamingMessage(ctx context.Context, params a2aclient.ServiceParams, req *a2a.SendMessageRequest) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		pbReq, err := pbconv.ToProtoSendMessageRequest(req)
		if err != nil {
			yield(nil, err)
			return
		}

		stream, err := c.client.SendStreamingMessage(withGRPCMetadata(ctx, params), pbReq)
		if err != nil {
			yield(nil, grpcutil.FromGRPCError(err))
			return
		}

		drainEventStream(stream, yield)
	}
}

func drainEventStream(stream grpc.ServerStreamingClient[a2apb.StreamResponse], yield func(a2a.Event, error) bool) {
	for {
		pResp, err := stream.Recv()
		if err == io.EOF {
			return
		}
		if err != nil {
			yield(nil, grpcutil.FromGRPCError(err))
			return
		}

		resp, err := pbconv.FromProtoStreamResponse(pResp)
		if err != nil {
			yield(nil, err)
			return
		}

		if !yield(resp, nil) {
			return
		}
	}
}

func (c *grpcTransport) GetTaskPushConfig(ctx context.Context, params a2aclient.ServiceParams, req *a2a.GetTaskPushConfigRequest) (*a2a.TaskPushConfig, error) {
	pbReq, err := pbconv.ToProtoGetTaskPushConfigRequest(req)
	if err != nil {
		return nil, err
	}

	pbConfig, err := c.client.GetTaskPushNotificationConfig(withGRPCMetadata(ctx, params), pbReq)
	if err != nil {
		return nil, grpcutil.FromGRPCError(err)
	}

	return pbconv.FromProtoTaskPushConfig(pbConfig)
}

func (c *grpcTransport) ListTaskPushConfig(ctx context.Context, params a2aclient.ServiceParams, req *a2a.ListTaskPushConfigRequest) ([]*a2a.TaskPushConfig, error) {
	pbReq, err := pbconv.ToProtoListTaskPushConfigRequest(req)
	if err != nil {
		return nil, err
	}

	pbResp, err := c.client.ListTaskPushNotificationConfig(withGRPCMetadata(ctx, params), pbReq)
	if err != nil {
		return nil, grpcutil.FromGRPCError(err)
	}
	resp, err := pbconv.FromProtoListTaskPushConfigResponse(pbResp)
	if err != nil {
		return nil, err
	}

	return resp.Configs, nil
}

func (c *grpcTransport) CreateTaskPushConfig(ctx context.Context, params a2aclient.ServiceParams, req *a2a.CreateTaskPushConfigRequest) (*a2a.TaskPushConfig, error) {
	pbReq, err := pbconv.ToProtoCreateTaskPushConfigRequest(req)
	if err != nil {
		return nil, err
	}

	pbConfig, err := c.client.CreateTaskPushNotificationConfig(withGRPCMetadata(ctx, params), pbReq)
	if err != nil {
		return nil, grpcutil.FromGRPCError(err)
	}

	return pbconv.FromProtoTaskPushConfig(pbConfig)
}

func (c *grpcTransport) DeleteTaskPushConfig(ctx context.Context, params a2aclient.ServiceParams, req *a2a.DeleteTaskPushConfigRequest) error {
	pbReq, err := pbconv.ToProtoDeleteTaskPushConfigRequest(req)
	if err != nil {
		return err
	}

	_, err = c.client.DeleteTaskPushNotificationConfig(withGRPCMetadata(ctx, params), pbReq)

	return grpcutil.FromGRPCError(err)
}

func (c *grpcTransport) GetExtendedAgentCard(ctx context.Context, params a2aclient.ServiceParams) (*a2a.AgentCard, error) {
	pCard, err := c.client.GetExtendedAgentCard(withGRPCMetadata(ctx, params), &a2apb.GetExtendedAgentCardRequest{})
	if err != nil {
		return nil, grpcutil.FromGRPCError(err)
	}

	return pbconv.FromProtoAgentCard(pCard)
}

func (c *grpcTransport) Destroy() error {
	return c.closeConnFn()
}

func withGRPCMetadata(ctx context.Context, params a2aclient.ServiceParams) context.Context {
	if len(params) == 0 {
		return ctx
	}
	meta := metadata.MD{}
	for k, vals := range params {
		meta[strings.ToLower(k)] = vals
	}
	return metadata.NewOutgoingContext(ctx, meta)
}
