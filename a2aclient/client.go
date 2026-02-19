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
	"fmt"
	"iter"
	"sync/atomic"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/internal/utils"
)

// Config exposes options for customizing [Client] behavior.
type Config struct {
	// PushConfig specifies the default push notification configuration to apply for every Task.
	PushConfig *a2a.PushConfig
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
	// Whether client prefers to poll for task updates instead of blocking until a terminal state is reached.
	// If set to true, non-streaming send message result might be a Message or a Task in any (including non-terminal) state.
	// Callers are responsible for running the polling loop. This configuration does not apply to streaming requests.
	Polling bool
}

// Client represents a transport-agnostic implementation of A2A client.
// The actual call is delegated to a specific [Transport] implementation.
// [CallInterceptor]-s are applied before and after every protocol call.
type Client struct {
	config       Config
	transport    Transport
	interceptors []CallInterceptor
	baseURL      string

	card atomic.Pointer[a2a.AgentCard]
}

type interceptBeforeResult[Req any, Resp any] struct {
	reqOverride   Req
	params        ServiceParams
	earlyResponse *Resp
	earlyErr      error
}

// AddCallInterceptor allows to attach a [CallInterceptor] to the client after creation.
func (c *Client) AddCallInterceptor(ci CallInterceptor) {
	c.interceptors = append(c.interceptors, ci)
}

// A2A protocol methods

// GetTask implements the 'GetTask' protocol method.
func (c *Client) GetTask(ctx context.Context, req *a2a.GetTaskRequest) (*a2a.Task, error) {
	return doCall(ctx, c, "GetTask", req, c.transport.GetTask)
}

// ListTasks implements the 'ListTasks' protocol method.
func (c *Client) ListTasks(ctx context.Context, req *a2a.ListTasksRequest) (*a2a.ListTasksResponse, error) {
	return doCall(ctx, c, "ListTasks", req, c.transport.ListTasks)
}

// CancelTask implements the 'CancelTask' protocol method.
func (c *Client) CancelTask(ctx context.Context, req *a2a.CancelTaskRequest) (*a2a.Task, error) {
	return doCall(ctx, c, "CancelTask", req, c.transport.CancelTask)
}

// SendMessage implements the 'SendMessage' protocol method (non-streaming).
func (c *Client) SendMessage(ctx context.Context, req *a2a.SendMessageRequest) (a2a.SendMessageResult, error) {
	req = c.withDefaultSendConfig(req, blocking(!c.config.Polling))
	return doCall(ctx, c, "SendMessage", req, c.transport.SendMessage)
}

// SendStreamingMessage implements the 'SendStreamingMessage' protocol method (streaming).
func (c *Client) SendStreamingMessage(ctx context.Context, req *a2a.SendMessageRequest) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		method := "SendStreamingMessage"

		req = c.withDefaultSendConfig(req, blocking(true))

		ctx, res := interceptBefore[*a2a.SendMessageRequest, a2a.SendMessageResult](ctx, c, method, req)
		if res.earlyErr != nil {
			yield(nil, res.earlyErr)
			return
		}

		if res.earlyResponse != nil {
			yield(*res.earlyResponse, nil)
			return
		}

		if card := c.card.Load(); card != nil && !card.Capabilities.Streaming {
			resp, err := c.transport.SendMessage(ctx, res.params, res.reqOverride)
			interceptedResponse, errOverride := interceptAfter(ctx, c, c.interceptors, method, res.params, resp, err)
			if errOverride != nil {
				yield(nil, errOverride)
				return
			}
			yield(interceptedResponse, nil)
			return
		}

		for resp, err := range c.transport.SendStreamingMessage(ctx, res.params, res.reqOverride) {
			interceptedEvent, errOverride := interceptAfter(ctx, c, c.interceptors, method, res.params, resp, err)
			if errOverride != nil {
				yield(nil, errOverride)
				return
			}

			if err != nil {
				yield(nil, err)
				return
			}

			if !yield(interceptedEvent, nil) {
				return
			}
		}
	}
}

// SubscribeToTask implements the `SubscribeToTask` protocol method.
func (c *Client) SubscribeToTask(ctx context.Context, req *a2a.SubscribeToTaskRequest) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		method := "SubscribeToTask"

		ctx, res := interceptBefore[*a2a.SubscribeToTaskRequest, a2a.SendMessageResult](ctx, c, method, req)
		if res.earlyErr != nil {
			yield(nil, res.earlyErr)
			return
		}

		if res.earlyResponse != nil {
			yield(*res.earlyResponse, nil)
			return
		}

		for resp, err := range c.transport.SubscribeToTask(ctx, res.params, res.reqOverride) {
			interceptedEvent, errOverride := interceptAfter(ctx, c, c.interceptors, method, res.params, resp, err)
			if errOverride != nil {
				yield(nil, errOverride)
				return
			}

			if err != nil {
				yield(nil, err)
				return
			}

			if !yield(interceptedEvent, nil) {
				return
			}
		}
	}
}

// GetTaskPushConfig implements the `GetTaskPushNotificationConfig` protocol method.
func (c *Client) GetTaskPushConfig(ctx context.Context, req *a2a.GetTaskPushConfigRequest) (*a2a.TaskPushConfig, error) {
	return doCall(ctx, c, "GetTaskPushConfig", req, c.transport.GetTaskPushConfig)
}

// ListTaskPushConfig implements the `ListTaskPushNotificationConfig` protocol method.
func (c *Client) ListTaskPushConfig(ctx context.Context, req *a2a.ListTaskPushConfigRequest) ([]*a2a.TaskPushConfig, error) {
	return doCall(ctx, c, "ListTaskPushConfig", req, c.transport.ListTaskPushConfig)
}

// CreateTaskPushConfig implements the `CreateTaskPushNotificationConfig` protocol method.
func (c *Client) CreateTaskPushConfig(ctx context.Context, req *a2a.CreateTaskPushConfigRequest) (*a2a.TaskPushConfig, error) {
	return doCall(ctx, c, "CreateTaskPushConfig", req, c.transport.CreateTaskPushConfig)
}

// DeleteTaskPushConfig implements the `DeleteTaskPushNotificationConfig` protocol method.
func (c *Client) DeleteTaskPushConfig(ctx context.Context, req *a2a.DeleteTaskPushConfigRequest) error {
	method := "DeleteTaskPushConfig"

	ctx, res := interceptBefore[*a2a.DeleteTaskPushConfigRequest, struct{}](ctx, c, method, req)
	if res.earlyErr != nil {
		return res.earlyErr
	}
	if res.earlyResponse != nil {
		return nil
	}

	err := c.transport.DeleteTaskPushConfig(ctx, res.params, res.reqOverride)
	var emptyResp struct{}
	_, errOverride := interceptAfter(ctx, c, c.interceptors, method, res.params, emptyResp, err)
	return errOverride
}

// GetExtendedAgentCard implements the `GetExtendedAgentCard` protocol method.
func (c *Client) GetExtendedAgentCard(ctx context.Context) (*a2a.AgentCard, error) {
	if card := c.card.Load(); card != nil && !card.Capabilities.ExtendedAgentCard {
		return card, nil
	}

	method := "GetExtendedAgentCard"
	var req *struct{}
	ctx, res := interceptBefore[*struct{}, *a2a.AgentCard](ctx, c, method, req)
	if res.earlyErr != nil {
		return nil, res.earlyErr
	}
	if res.earlyResponse != nil {
		return *res.earlyResponse, nil
	}

	resp, err := c.transport.GetExtendedAgentCard(ctx, res.params)
	interceptedResponse, errOverride := interceptAfter(ctx, c, c.interceptors, method, res.params, resp, err)
	if errOverride != nil {
		return nil, errOverride
	}

	if err == nil {
		c.card.Store(interceptedResponse)
	}

	return interceptedResponse, nil
}

// Destroy cleans up resources associated with the client.
func (c *Client) Destroy() error {
	return c.transport.Destroy()
}

type blocking bool

func (c *Client) withDefaultSendConfig(message *a2a.SendMessageRequest, blocking blocking) *a2a.SendMessageRequest {
	if c.config.PushConfig == nil && c.config.AcceptedOutputModes == nil && blocking {
		return message
	}
	result := *message
	if result.Config == nil {
		result.Config = &a2a.SendMessageConfig{}
	} else {
		configCopy := *result.Config
		result.Config = &configCopy
	}
	if result.Config.PushConfig == nil {
		result.Config.PushConfig = c.config.PushConfig
	}
	if result.Config.AcceptedOutputModes == nil {
		result.Config.AcceptedOutputModes = c.config.AcceptedOutputModes
	}
	result.Config.Blocking = utils.Ptr(bool(blocking))
	return &result
}

func interceptBefore[Req any, Resp any](ctx context.Context, c *Client, method string, payload Req) (context.Context, interceptBeforeResult[Req, Resp]) {
	req := Request{
		Method:        method,
		BaseURL:       c.baseURL,
		ServiceParams: serviceParamsFrom(ctx),
		Card:          c.card.Load(),
		Payload:       payload,
	}

	var zeroReq Req
	outcome := interceptBeforeResult[Req, Resp]{
		reqOverride:   zeroReq,
		earlyResponse: nil,
		earlyErr:      nil,
	}

	for i, interceptor := range c.interceptors {
		localCtx, result, err := interceptor.Before(ctx, &req)
		if err != nil || result != nil {
			var typedResult Resp
			if result != nil {
				r, ok := result.(Resp)
				if !ok {
					outcome.earlyErr = fmt.Errorf("result type changed from %T to %T", result, r)
					return ctx, outcome
				}
				typedResult = r
			}
			interceptors := c.interceptors[:i+1]
			resp, err := interceptAfter(ctx, c, interceptors, method, req.ServiceParams, typedResult, err)
			outcome.earlyResponse = &resp
			outcome.earlyErr = err
			return ctx, outcome
		}
		ctx = localCtx
	}

	if req.Payload == nil {
		return ctx, outcome
	}

	typed, ok := req.Payload.(Req)
	if !ok {
		outcome.earlyErr = fmt.Errorf("payload type changed from %T to %T", payload, req.Payload)
		return ctx, outcome
	}
	outcome.reqOverride = typed
	outcome.params = req.ServiceParams
	return ctx, outcome
}

func interceptAfter[T any](ctx context.Context, c *Client, interceptors []CallInterceptor, method string, params ServiceParams, payload T, err error) (T, error) {
	resp := Response{
		BaseURL:       c.baseURL,
		Method:        method,
		ServiceParams: params,
		Payload:       payload,
		Card:          c.card.Load(),
		Err:           err,
	}

	var zero T
	for i := len(interceptors) - 1; i >= 0; i-- {
		if err := interceptors[i].After(ctx, &resp); err != nil {
			return zero, err
		}
	}

	if resp.Payload == nil {
		return zero, resp.Err
	}

	typed, ok := resp.Payload.(T)
	if !ok {
		return zero, fmt.Errorf("payload type changed from %T to %T", payload, resp.Payload)
	}

	return typed, resp.Err
}

func doCall[Req any, Resp any](
	ctx context.Context, c *Client, method string, req Req,
	transportCall func(context.Context, ServiceParams, Req) (Resp, error),
) (Resp, error) {
	ctx, res := interceptBefore[Req, Resp](ctx, c, method, req)
	if res.earlyErr != nil {
		var zero Resp
		return zero, res.earlyErr
	}
	if res.earlyResponse != nil {
		return *res.earlyResponse, nil
	}
	response, err := transportCall(ctx, res.params, res.reqOverride)
	return interceptAfter(ctx, c, c.interceptors, method, res.params, response, err)
}
