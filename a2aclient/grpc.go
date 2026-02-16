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

package a2aclient

import (
	"context"
	"io"
	"iter"
	"strings"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2apb/v0"
	"github.com/a2aproject/a2a-go/a2apb/v0/pbconv"
	"github.com/a2aproject/a2a-go/internal/grpcutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// WithGRPCTransport create a gRPC transport implementation which will use the provided [grpc.DialOption]s during connection establishment.
func WithGRPCTransport(opts ...grpc.DialOption) FactoryOption {
	return WithTransport(
		a2a.TransportProtocolGRPC,
		TransportFactoryFn(func(ctx context.Context, url string, card *a2a.AgentCard) (Transport, error) {
			conn, err := grpc.NewClient(url, opts...)
			if err != nil {
				return nil, err
			}
			return NewGRPCTransport(conn), nil
		}),
	)
}

// NewGRPCTransport exposes a method for direct A2A gRPC protocol handler.
func NewGRPCTransport(conn *grpc.ClientConn) Transport {
	return &grpcTransport{
		client:      a2apb.NewA2AServiceClient(conn),
		closeConnFn: func() error { return conn.Close() },
	}
}

// NewGRPCTransportFromClient creates a gRPC transport where the connection is managed
// externally and encapsulated in the service client. The transport's Destroy method is a no-op.
func NewGRPCTransportFromClient(client a2apb.A2AServiceClient) Transport {
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

var _ Transport = (*grpcTransport)(nil)

// A2A protocol methods

func (c *grpcTransport) GetTask(ctx context.Context, params ServiceParams, query *a2a.TaskQueryParams) (*a2a.Task, error) {
	req, err := pbconv.ToProtoGetTaskRequest(query)
	if err != nil {
		return nil, err
	}

	pResp, err := c.client.GetTask(withGRPCMetadata(ctx, params), req)
	if err != nil {
		return nil, grpcutil.FromGRPCError(err)
	}

	return pbconv.FromProtoTask(pResp)
}

func (c *grpcTransport) ListTasks(ctx context.Context, params ServiceParams, req *a2a.ListTasksRequest) (*a2a.ListTasksResponse, error) {
	pReq, err := pbconv.ToProtoListTasksRequest(req)
	if err != nil {
		return nil, err
	}

	pResp, err := c.client.ListTasks(withGRPCMetadata(ctx, params), pReq)
	if err != nil {
		return nil, err
	}

	return pbconv.FromProtoListTasksResponse(pResp)
}

func (c *grpcTransport) CancelTask(ctx context.Context, params ServiceParams, id *a2a.TaskIDParams) (*a2a.Task, error) {
	req, err := pbconv.ToProtoCancelTaskRequest(id)
	if err != nil {
		return nil, err
	}

	pResp, err := c.client.CancelTask(withGRPCMetadata(ctx, params), req)
	if err != nil {
		return nil, grpcutil.FromGRPCError(err)
	}

	return pbconv.FromProtoTask(pResp)
}

func (c *grpcTransport) SendMessage(ctx context.Context, params ServiceParams, message *a2a.MessageSendParams) (a2a.SendMessageResult, error) {
	req, err := pbconv.ToProtoSendMessageRequest(message)
	if err != nil {
		return nil, err
	}

	pResp, err := c.client.SendMessage(withGRPCMetadata(ctx, params), req)
	if err != nil {
		return nil, grpcutil.FromGRPCError(err)
	}

	return pbconv.FromProtoSendMessageResponse(pResp)
}

func (c *grpcTransport) ResubscribeToTask(ctx context.Context, params ServiceParams, id *a2a.TaskIDParams) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		req, err := pbconv.ToProtoTaskSubscriptionRequest(id)
		if err != nil {
			yield(nil, err)
			return
		}

		stream, err := c.client.TaskSubscription(withGRPCMetadata(ctx, params), req)
		if err != nil {
			yield(nil, grpcutil.FromGRPCError(err))
			return
		}

		drainEventStream(stream, yield)
	}
}

func (c *grpcTransport) SendStreamingMessage(ctx context.Context, params ServiceParams, message *a2a.MessageSendParams) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		req, err := pbconv.ToProtoSendMessageRequest(message)
		if err != nil {
			yield(nil, err)
			return
		}

		stream, err := c.client.SendStreamingMessage(withGRPCMetadata(ctx, params), req)
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

func (c *grpcTransport) GetTaskPushConfig(ctx context.Context, params ServiceParams, req *a2a.GetTaskPushConfigParams) (*a2a.TaskPushConfig, error) {
	pReq, err := pbconv.ToProtoGetTaskPushConfigRequest(req)
	if err != nil {
		return nil, err
	}

	pResp, err := c.client.GetTaskPushNotificationConfig(withGRPCMetadata(ctx, params), pReq)
	if err != nil {
		return nil, grpcutil.FromGRPCError(err)
	}

	return pbconv.FromProtoTaskPushConfig(pResp)
}

func (c *grpcTransport) ListTaskPushConfig(ctx context.Context, params ServiceParams, req *a2a.ListTaskPushConfigParams) ([]*a2a.TaskPushConfig, error) {
	pReq, err := pbconv.ToProtoListTaskPushConfigRequest(req)
	if err != nil {
		return nil, err
	}

	pResp, err := c.client.ListTaskPushNotificationConfig(withGRPCMetadata(ctx, params), pReq)
	if err != nil {
		return nil, grpcutil.FromGRPCError(err)
	}

	return pbconv.FromProtoListTaskPushConfig(pResp)
}

func (c *grpcTransport) SetTaskPushConfig(ctx context.Context, params ServiceParams, req *a2a.TaskPushConfig) (*a2a.TaskPushConfig, error) {
	pReq, err := pbconv.ToProtoCreateTaskPushConfigRequest(req)
	if err != nil {
		return nil, err
	}

	pResp, err := c.client.CreateTaskPushNotificationConfig(withGRPCMetadata(ctx, params), pReq)
	if err != nil {
		return nil, grpcutil.FromGRPCError(err)
	}

	return pbconv.FromProtoTaskPushConfig(pResp)
}

func (c *grpcTransport) DeleteTaskPushConfig(ctx context.Context, params ServiceParams, req *a2a.DeleteTaskPushConfigParams) error {
	pReq, err := pbconv.ToProtoDeleteTaskPushConfigRequest(req)
	if err != nil {
		return err
	}

	_, err = c.client.DeleteTaskPushNotificationConfig(withGRPCMetadata(ctx, params), pReq)

	return grpcutil.FromGRPCError(err)
}

func (c *grpcTransport) GetAgentCard(ctx context.Context, params ServiceParams) (*a2a.AgentCard, error) {
	pCard, err := c.client.GetAgentCard(withGRPCMetadata(ctx, params), &a2apb.GetAgentCardRequest{})
	if err != nil {
		return nil, grpcutil.FromGRPCError(err)
	}

	return pbconv.FromProtoAgentCard(pCard)
}

func (c *grpcTransport) Destroy() error {
	return c.closeConnFn()
}

func withGRPCMetadata(ctx context.Context, params ServiceParams) context.Context {
	if len(params) == 0 {
		return ctx
	}
	meta := metadata.MD{}
	for k, vals := range params {
		meta[strings.ToLower(k)] = vals
	}
	return metadata.NewOutgoingContext(ctx, meta)
}
