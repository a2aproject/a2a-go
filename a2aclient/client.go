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

	"github.com/a2aproject/a2a-go/a2a"
)

// Config exposes options for customizing Client behavior.
type Config struct {
	// PushConfigs specifies the default push notification configurations to apply for every Task.
	PushConfigs []a2a.PushConfig
	// AcceptedOutputModes to pass as message send configuration to A2A server.
	AcceptedOutputModes []string
	// PreferredTransports
	PreferredTransports []string
}

// Event is one of Message | Task | TaskStatusUpdateEvent | TaskArtifactUpdateEvent
type EventCallback func(ctx context.Context, card a2a.AgentCard, event a2a.Event)

// Client represents a transport-agnostic implementation of A2A client.
// The actual call is delegated to a specific Transport implementation.
// CallInterceptors are applied before and after every protocol call.
// EventCallback are notified about new Tasks, Messages and Task update events.
type Client struct {
	Config       Config
	transport    Transport
	interceptors []CallInterceptor
	callbacks    []EventCallback
}

// AddEventCallback allows to attach an EventCallback to the client after creation.
func (c *Client) AddEventCallback(ec EventCallback) {
	c.callbacks = append(c.callbacks, ec)
}

// AddCallInterceptor allows to attach a CallInterceptor to the client after creation.
func (c *Client) AddCallInterceptor(ci CallInterceptor) {
	c.interceptors = append(c.interceptors, ci)
}

// A2A protocol methods

func (c *Client) GetTask(ctx context.Context, query a2a.TaskQueryParams) (a2a.Task, error) {
	return a2a.Task{}, ErrNotImplemented
}

func (c *Client) CancelTask(ctx context.Context, id a2a.TaskIDParams) (a2a.Task, error) {
	return a2a.Task{}, ErrNotImplemented
}

func (c *Client) SendMessage(ctx context.Context, message a2a.MessageSendParams) (a2a.SendMessageResult, error) {
	return a2a.Task{}, ErrNotImplemented
}

func (c *Client) ResubscribeToTask(ctx context.Context, id a2a.TaskIDParams) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		yield(a2a.Message{}, ErrNotImplemented)
	}
}

func (c *Client) SendStreamingMessage(ctx context.Context, message a2a.MessageSendParams) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		yield(a2a.Message{}, ErrNotImplemented)
	}
}

func (c *Client) GetTaskPushConfig(ctx context.Context, params a2a.GetTaskPushConfigParams) (a2a.TaskPushConfig, error) {
	return a2a.TaskPushConfig{}, ErrNotImplemented
}

func (c *Client) ListTaskPushConfig(ctx context.Context, params a2a.ListTaskPushConfigParams) ([]a2a.TaskPushConfig, error) {
	return []a2a.TaskPushConfig{}, ErrNotImplemented
}

func (c *Client) SetTaskPushConfig(ctx context.Context, params a2a.TaskPushConfig) (a2a.TaskPushConfig, error) {
	return a2a.TaskPushConfig{}, ErrNotImplemented
}

func (c *Client) DeleteTaskPushConfig(ctx context.Context, params a2a.DeleteTaskPushConfigParams) error {
	return ErrNotImplemented
}

func (c *Client) GetAgentCard(ctx context.Context) (a2a.AgentCard, error) {
	return a2a.AgentCard{}, ErrNotImplemented
}

func (c *Client) Destroy() error {
	return c.transport.Destroy()
}
