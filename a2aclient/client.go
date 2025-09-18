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
	// AcceptedOutputModes are MIME types passed with every Client message and might be used by an agent
	// to decide on the result format.
	// For example, an Agent might declare a skill with OutputModes: ["application/json", "image/png"]
	// and a Client that doesn't support images will pass AcceptedOutputModes: ["application/json"]
	// to get a result in the desired format.
	AcceptedOutputModes []string
	// PreferredTransports is used for selecting the most appropriate communication protocol.
	// The first transport from the list which is also supported by the server is going to be used
	// to establish a connection. If no preference is provided the server ordering will be used.
	// If there's no overlap in supported Transport Factory will return an error on Client
	// creation attempt.
	PreferredTransports []a2a.TransportProtocol
}

// Client represents a transport-agnostic implementation of A2A client.
// The actual call is delegated to a specific Transport implementation.
// CallInterceptors are applied before and after every protocol call.
type Client struct {
	Config       Config
	transport    Transport
	interceptors []CallInterceptor
}

// AddCallInterceptor allows to attach a CallInterceptor to the client after creation.
func (c *Client) AddCallInterceptor(ci CallInterceptor) {
	c.interceptors = append(c.interceptors, ci)
}

// A2A protocol methods

func (c *Client) GetTask(ctx context.Context, query *a2a.TaskQueryParams) (*a2a.Task, error) {
	ctx = withMethod(ctx, "GetTask")

	ctx, err := interceptBefore(ctx, c.interceptors, query)
	if err != nil {
		return nil, err
	}

	resp, err := c.transport.GetTask(ctx, query)

	if err := interceptAfter(ctx, c.interceptors, resp, err); err != nil {
		return nil, err
	}

	return resp, nil
}

func (c *Client) CancelTask(ctx context.Context, id *a2a.TaskIDParams) (*a2a.Task, error) {
	ctx = withMethod(ctx, "CancelTask")

	ctx, err := interceptBefore(ctx, c.interceptors, id)
	if err != nil {
		return nil, err
	}

	resp, err := c.transport.CancelTask(ctx, id)

	if err := interceptAfter(ctx, c.interceptors, resp, err); err != nil {
		return nil, err
	}

	return resp, nil
}

func (c *Client) SendMessage(ctx context.Context, message *a2a.MessageSendParams) (a2a.SendMessageResult, error) {
	ctx = withMethod(ctx, "SendMessage")

	ctx, err := interceptBefore(ctx, c.interceptors, message)
	if err != nil {
		return nil, err
	}

	resp, err := c.transport.SendMessage(ctx, message)

	if err := interceptAfter(ctx, c.interceptors, resp, err); err != nil {
		return nil, err
	}

	return resp, nil
}

func (c *Client) ResubscribeToTask(ctx context.Context, id *a2a.TaskIDParams) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		ctx = withMethod(ctx, "ResubscribeToTask")

		ctx, err := interceptBefore(ctx, c.interceptors, id)
		if err != nil {
			yield(nil, err)
			return
		}

		for resp, err := range c.transport.ResubscribeToTask(ctx, id) {
			if err := interceptAfter(ctx, c.interceptors, resp, err); err != nil {
				yield(nil, err)
				return
			}

			if !yield(resp, nil) {
				return
			}
		}
	}
}

func (c *Client) SendStreamingMessage(ctx context.Context, message *a2a.MessageSendParams) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		ctx = withMethod(ctx, "SendStreamingMessage")

		ctx, err := interceptBefore(ctx, c.interceptors, message)
		if err != nil {
			yield(nil, err)
			return
		}

		for resp, err := range c.transport.SendStreamingMessage(ctx, message) {
			if err := interceptAfter(ctx, c.interceptors, resp, err); err != nil {
				yield(nil, err)
				return
			}

			if !yield(resp, nil) {
				return
			}
		}
	}
}

func (c *Client) GetTaskPushConfig(ctx context.Context, params *a2a.GetTaskPushConfigParams) (*a2a.TaskPushConfig, error) {
	ctx = withMethod(ctx, "GetTaskPushConfig")

	ctx, err := interceptBefore(ctx, c.interceptors, params)
	if err != nil {
		return nil, err
	}

	resp, err := c.transport.GetTaskPushConfig(ctx, params)

	if err := interceptAfter(ctx, c.interceptors, resp, err); err != nil {
		return nil, err
	}

	return resp, nil
}

func (c *Client) ListTaskPushConfig(ctx context.Context, params *a2a.ListTaskPushConfigParams) ([]*a2a.TaskPushConfig, error) {
	ctx = withMethod(ctx, "ListTaskPushConfig")

	ctx, err := interceptBefore(ctx, c.interceptors, params)
	if err != nil {
		return nil, err
	}

	resp, err := c.transport.ListTaskPushConfig(ctx, params)

	if err := interceptAfter(ctx, c.interceptors, resp, err); err != nil {
		return nil, err
	}

	return resp, nil
}

func (c *Client) SetTaskPushConfig(ctx context.Context, params *a2a.TaskPushConfig) (*a2a.TaskPushConfig, error) {
	ctx = withMethod(ctx, "SetTaskPushConfig")

	ctx, err := interceptBefore(ctx, c.interceptors, params)
	if err != nil {
		return nil, err
	}

	resp, err := c.transport.SetTaskPushConfig(ctx, params)

	if err := interceptAfter(ctx, c.interceptors, resp, err); err != nil {
		return nil, err
	}

	return resp, nil
}

func (c *Client) DeleteTaskPushConfig(ctx context.Context, params *a2a.DeleteTaskPushConfigParams) error {
	ctx = withMethod(ctx, "DeleteTaskPushConfig")

	ctx, err := interceptBefore(ctx, c.interceptors, params)
	if err != nil {
		return err
	}

	err = c.transport.DeleteTaskPushConfig(ctx, params)

	if err := interceptAfter(ctx, c.interceptors, nil, err); err != nil {
		return err
	}

	return nil
}

func (c *Client) GetAgentCard(ctx context.Context) (*a2a.AgentCard, error) {
	ctx = withMethod(ctx, "GetAgentCard")

	ctx, err := interceptBefore(ctx, c.interceptors, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.transport.GetAgentCard(ctx)
	if err := interceptAfter(ctx, c.interceptors, resp, err); err != nil {
		return nil, err
	}

	return resp, nil
}

func (c *Client) Destroy() error {
	return c.transport.Destroy()
}

func interceptBefore(ctx context.Context, interceptors []CallInterceptor, payload any) (context.Context, error) {
	req := Request{Meta: CallMeta{}, Payload: payload}
	if payload == nil { // set interface to nil if method does not take any parameters
		req.Payload = nil
	}

	for _, interceptor := range interceptors {
		localCtx, err := interceptor.Before(ctx, &req)
		if err != nil {
			return ctx, err
		}
		ctx = localCtx
	}

	return withCallMeta(ctx, req.Meta), nil
}

func interceptAfter(ctx context.Context, interceptors []CallInterceptor, payload any, err error) error {
	meta, ok := CallMetaFrom(ctx)
	if !ok {
		meta = CallMeta{}
	}

	resp := Response{Meta: meta, Payload: payload, Err: err}
	if payload == nil { // set interface to nil if method does not return any value
		resp.Payload = nil
	}

	for _, interceptor := range interceptors {
		if err := interceptor.After(ctx, &resp); err != nil {
			return err
		}
	}

	return resp.Err
}
