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
	"errors"
	"iter"

	"github.com/a2aproject/a2a-go/a2a"
)

// A2AClient defines a transport-agnostic interface for making A2A requests.
// Transport implementations are a translation layer between a2a core types and wire formats.
type Transport interface {
	// GetTask calls the 'GetTask' protocol method.
	GetTask(context.Context, ServiceParams, *a2a.GetTaskRequest) (*a2a.Task, error)

	// ListTasks calls the 'ListTasks' protocol method.
	ListTasks(context.Context, ServiceParams, *a2a.ListTasksRequest) (*a2a.ListTasksResponse, error)

	// CancelTask calls the 'CancelTask' protocol method.
	CancelTask(context.Context, ServiceParams, *a2a.CancelTaskRequest) (*a2a.Task, error)

	// SendMessage calls the 'SendMessage' protocol method (non-streaming).
	SendMessage(context.Context, ServiceParams, *a2a.SendMessageRequest) (a2a.SendMessageResult, error)

	// SubscribeToTask calls the `SubscribeToTask` protocol method.
	SubscribeToTask(context.Context, ServiceParams, *a2a.SubscribeToTaskRequest) iter.Seq2[a2a.Event, error]

	// SendStreamingMessage calls the 'SendStreamingMessage' protocol method (streaming).
	SendStreamingMessage(context.Context, ServiceParams, *a2a.SendMessageRequest) iter.Seq2[a2a.Event, error]

	// GetTaskPushNotificationConfig calls the `GetTaskPushNotificationConfig` protocol method.
	GetTaskPushConfig(context.Context, ServiceParams, *a2a.GetTaskPushConfigRequest) (*a2a.TaskPushConfig, error)

	// ListTaskPushNotificationConfig calls the `ListTaskPushNotificationConfig` protocol method.
	ListTaskPushConfig(context.Context, ServiceParams, *a2a.ListTaskPushConfigRequest) ([]*a2a.TaskPushConfig, error)

	// CreateTaskPushNotificationConfig calls the `CreateTaskPushNotificationConfig` protocol method.
	CreateTaskPushConfig(context.Context, ServiceParams, *a2a.CreateTaskPushConfigRequest) (*a2a.TaskPushConfig, error)

	// DeleteTaskPushNotificationConfig calls the `DeleteTaskPushNotificationConfig` protocol method.
	DeleteTaskPushConfig(context.Context, ServiceParams, *a2a.DeleteTaskPushConfigRequest) error

	// GetExtendedAgentCard resolves the AgentCard.
	// If extended card is supported calls the 'GetExtendedAgentCard' protocol method.
	GetExtendedAgentCard(context.Context, ServiceParams) (*a2a.AgentCard, error)

	// Clean up resources associated with the transport (eg. close a gRPC channel).
	Destroy() error
}

// TransportFactory creates an A2A protocol connection to the provided URL.
type TransportFactory interface {
	Create(ctx context.Context, url string, card *a2a.AgentCard) (Transport, error)
}

// TransportFactoryFn implements TransportFactory.
type TransportFactoryFn func(ctx context.Context, url string, card *a2a.AgentCard) (Transport, error)

func (fn TransportFactoryFn) Create(ctx context.Context, url string, card *a2a.AgentCard) (Transport, error) {
	return fn(ctx, url, card)
}

var errNotImplemented = errors.New("not implemented")

type unimplementedTransport struct{}

var _ Transport = (*unimplementedTransport)(nil)

func (unimplementedTransport) GetTask(ctx context.Context, params ServiceParams, query *a2a.GetTaskRequest) (*a2a.Task, error) {
	return nil, errNotImplemented
}

func (unimplementedTransport) ListTasks(ctx context.Context, params ServiceParams, request *a2a.ListTasksRequest) (*a2a.ListTasksResponse, error) {
	return nil, errNotImplemented
}

func (unimplementedTransport) CancelTask(ctx context.Context, params ServiceParams, id *a2a.CancelTaskRequest) (*a2a.Task, error) {
	return nil, errNotImplemented
}

func (unimplementedTransport) SendMessage(ctx context.Context, params ServiceParams, message *a2a.SendMessageRequest) (a2a.SendMessageResult, error) {
	return nil, errNotImplemented
}

func (unimplementedTransport) SubscribeToTask(ctx context.Context, params ServiceParams, id *a2a.SubscribeToTaskRequest) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		yield(nil, errNotImplemented)
	}
}

func (unimplementedTransport) SendStreamingMessage(ctx context.Context, params ServiceParams, message *a2a.SendMessageRequest) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		yield(nil, errNotImplemented)
	}
}

func (unimplementedTransport) GetTaskPushConfig(ctx context.Context, params ServiceParams, req *a2a.GetTaskPushConfigRequest) (*a2a.TaskPushConfig, error) {
	return nil, errNotImplemented
}

func (unimplementedTransport) ListTaskPushConfig(ctx context.Context, params ServiceParams, req *a2a.ListTaskPushConfigRequest) ([]*a2a.TaskPushConfig, error) {
	return nil, errNotImplemented
}

func (unimplementedTransport) CreateTaskPushConfig(ctx context.Context, params ServiceParams, req *a2a.CreateTaskPushConfigRequest) (*a2a.TaskPushConfig, error) {
	return nil, errNotImplemented
}

func (unimplementedTransport) DeleteTaskPushConfig(ctx context.Context, params ServiceParams, req *a2a.DeleteTaskPushConfigRequest) error {
	return errNotImplemented
}

func (unimplementedTransport) GetExtendedAgentCard(ctx context.Context, params ServiceParams) (*a2a.AgentCard, error) {
	return nil, errNotImplemented
}

func (unimplementedTransport) Destroy() error {
	return nil
}
