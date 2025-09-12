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
	"iter"

	"google.golang.org/grpc"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2apb"
)

// WithGRPCTransport returns a Client factory configuration option that if applied will
// enable support of gRPC-A2A communication.
func WithGRPCTransport(opts ...grpc.DialOption) FactoryOption {
	return WithTransport(
		string(a2a.TransportProtocolGRPC),
		TransportFactoryFn(func(ctx context.Context, url string, card a2a.AgentCard) (Transport, error) {
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
	return &grpcTransport{a2apb.NewA2AServiceClient(conn), conn}
}

// grpcTransport implements Transport by delegating to a2apb.A2AServiceClient.
type grpcTransport struct {
	client a2apb.A2AServiceClient
	conn   *grpc.ClientConn
}

// A2A protocol methods

func (c *grpcTransport) GetTask(ctx context.Context, query a2a.TaskQueryParams) (*a2a.Task, error) {
	return &a2a.Task{}, ErrNotImplemented
}

func (c *grpcTransport) CancelTask(ctx context.Context, id a2a.TaskIDParams) (*a2a.Task, error) {
	return &a2a.Task{}, ErrNotImplemented
}

func (c *grpcTransport) SendMessage(ctx context.Context, message a2a.MessageSendParams) (a2a.SendMessageResult, error) {
	return &a2a.Task{}, ErrNotImplemented
}

func (c *grpcTransport) ResubscribeToTask(ctx context.Context, id a2a.TaskIDParams) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		yield(&a2a.Message{}, ErrNotImplemented)
	}
}

func (c *grpcTransport) SendStreamingMessage(ctx context.Context, message a2a.MessageSendParams) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		yield(&a2a.Message{}, ErrNotImplemented)
	}
}

func (c *grpcTransport) GetTaskPushConfig(ctx context.Context, params a2a.GetTaskPushConfigParams) (a2a.TaskPushConfig, error) {
	return a2a.TaskPushConfig{}, ErrNotImplemented
}

func (c *grpcTransport) ListTaskPushConfig(ctx context.Context, params a2a.ListTaskPushConfigParams) ([]a2a.TaskPushConfig, error) {
	return []a2a.TaskPushConfig{}, ErrNotImplemented
}

func (c *grpcTransport) SetTaskPushConfig(ctx context.Context, params a2a.TaskPushConfig) (a2a.TaskPushConfig, error) {
	return a2a.TaskPushConfig{}, ErrNotImplemented
}

func (c *grpcTransport) DeleteTaskPushConfig(ctx context.Context, params a2a.DeleteTaskPushConfigParams) error {
	return ErrNotImplemented
}

func (c *grpcTransport) GetAgentCard(ctx context.Context) (a2a.AgentCard, error) {
	return a2a.AgentCard{}, ErrNotImplemented
}

func (c *grpcTransport) Destroy() error {
	return c.conn.Close()
}
