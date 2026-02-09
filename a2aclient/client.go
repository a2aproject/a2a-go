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
	earlyResponse *Resp
	earlyErr      error
}

// AddCallInterceptor allows to attach a [CallInterceptor] to the client after creation.
func (c *Client) AddCallInterceptor(ci CallInterceptor) {
	c.interceptors = append(c.interceptors, ci)
}

// A2A protocol methods

func (c *Client) GetTask(ctx context.Context, query *a2a.TaskQueryParams) (*a2a.Task, error) {
	return doCall(ctx, c, "GetTask", query, c.transport.GetTask)
}

func (c *Client) ListTasks(ctx context.Context, request *a2a.ListTasksRequest) (*a2a.ListTasksResponse, error) {
	return doCall(ctx, c, "ListTasks", request, c.transport.ListTasks)
}

func (c *Client) CancelTask(ctx context.Context, id *a2a.TaskIDParams) (*a2a.Task, error) {
	return doCall(ctx, c, "CancelTask", id, c.transport.CancelTask)
}

func (c *Client) SendMessage(ctx context.Context, message *a2a.MessageSendParams) (a2a.SendMessageResult, error) {
	message = c.withDefaultSendConfig(message, blocking(!c.config.Polling))
	return doCall(ctx, c, "SendMessage", message, c.transport.SendMessage)
}

func (c *Client) SendStreamingMessage(ctx context.Context, message *a2a.MessageSendParams) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		method := "SendStreamingMessage"

		message = c.withDefaultSendConfig(message, blocking(true))

		ctx, res := interceptBefore[*a2a.MessageSendParams, a2a.SendMessageResult](ctx, c, method, message)
		if res.earlyErr != nil {
			yield(nil, res.earlyErr)
			return
		}

		if res.earlyResponse != nil {
			yield(*res.earlyResponse, nil)
			return
		}

		if card := c.card.Load(); card != nil && !card.Capabilities.Streaming {
			resp, err := c.transport.SendMessage(ctx, res.reqOverride)
			interceptedResponse, errOverride := interceptAfter(ctx, c, c.interceptors, method, resp, err)
			if errOverride != nil {
				yield(nil, errOverride)
				return
			}
			yield(interceptedResponse, nil)
			return
		}

		for resp, err := range c.transport.SendStreamingMessage(ctx, res.reqOverride) {
			interceptedEvent, errOverride := interceptAfter(ctx, c, c.interceptors, method, resp, err)
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

func (c *Client) ResubscribeToTask(ctx context.Context, id *a2a.TaskIDParams) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		method := "ResubscribeToTask"

		ctx, res := interceptBefore[*a2a.TaskIDParams, a2a.SendMessageResult](ctx, c, method, id)
		if res.earlyErr != nil {
			yield(nil, res.earlyErr)
			return
		}

		if res.earlyResponse != nil {
			yield(*res.earlyResponse, nil)
			return
		}

		for resp, err := range c.transport.ResubscribeToTask(ctx, res.reqOverride) {
			interceptedEvent, errOverride := interceptAfter(ctx, c, c.interceptors, method, resp, err)
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

func (c *Client) GetTaskPushConfig(ctx context.Context, params *a2a.GetTaskPushConfigParams) (*a2a.TaskPushConfig, error) {
	return doCall(ctx, c, "GetTaskPushConfig", params, c.transport.GetTaskPushConfig)
}

func (c *Client) ListTaskPushConfig(ctx context.Context, params *a2a.ListTaskPushConfigParams) ([]*a2a.TaskPushConfig, error) {
	return doCall(ctx, c, "ListTaskPushConfig", params, c.transport.ListTaskPushConfig)
}

func (c *Client) SetTaskPushConfig(ctx context.Context, params *a2a.TaskPushConfig) (*a2a.TaskPushConfig, error) {
	return doCall(ctx, c, "SetTaskPushConfig", params, c.transport.SetTaskPushConfig)
}

func (c *Client) DeleteTaskPushConfig(ctx context.Context, params *a2a.DeleteTaskPushConfigParams) error {
	method := "DeleteTaskPushConfig"

	ctx, res := interceptBefore[*a2a.DeleteTaskPushConfigParams, struct{}](ctx, c, method, params)
	if res.earlyErr != nil {
		return res.earlyErr
	}
	if res.earlyResponse != nil {
		return nil
	}

	err := c.transport.DeleteTaskPushConfig(ctx, res.reqOverride)
	var emptyResp struct{}
	_, errOverride := interceptAfter(ctx, c, c.interceptors, method, emptyResp, err)
	return errOverride
}

func (c *Client) GetAgentCard(ctx context.Context) (*a2a.AgentCard, error) {
	if card := c.card.Load(); card != nil && !card.SupportsAuthenticatedExtendedCard {
		return card, nil
	}

	method := "GetAgentCard"
	var req *struct{}
	ctx, res := interceptBefore[*struct{}, *a2a.AgentCard](ctx, c, method, req)
	if res.earlyErr != nil {
		return nil, res.earlyErr
	}
	if res.earlyResponse != nil {
		return *res.earlyResponse, nil
	}

	resp, err := c.transport.GetAgentCard(ctx)
	interceptedResponse, errOverride := interceptAfter(ctx, c, c.interceptors, method, resp, err)
	if errOverride != nil {
		return nil, errOverride
	}

	if err == nil {
		c.card.Store(interceptedResponse)
	}

	return interceptedResponse, nil
}

func (c *Client) Destroy() error {
	return c.transport.Destroy()
}

type blocking bool

func (c *Client) withDefaultSendConfig(message *a2a.MessageSendParams, blocking blocking) *a2a.MessageSendParams {
	if c.config.PushConfig == nil && c.config.AcceptedOutputModes == nil && blocking {
		return message
	}
	result := *message
	if result.Config == nil {
		result.Config = &a2a.MessageSendConfig{}
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
		Method:  method,
		BaseURL: c.baseURL,
		Meta:    CallMeta{},
		Card:    c.card.Load(),
		Payload: payload,
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
			resp, err := interceptAfter(ctx, c, interceptors, method, typedResult, err)
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
	return withCallMeta(ctx, req.Meta), outcome
}

func interceptAfter[T any](ctx context.Context, c *Client, interceptors []CallInterceptor, method string, payload T, err error) (T, error) {
	meta, ok := CallMetaFrom(ctx)
	if !ok {
		meta = CallMeta{}
	}

	resp := Response{
		BaseURL: c.baseURL,
		Method:  method,
		Meta:    meta,
		Payload: payload,
		Card:    c.card.Load(),
		Err:     err,
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
	transportCall func(context.Context, Req) (Resp, error),
) (Resp, error) {
	ctx, res := interceptBefore[Req, Resp](ctx, c, method, req)
	if res.earlyErr != nil {
		var zero Resp
		return zero, res.earlyErr
	}
	if res.earlyResponse != nil {
		return *res.earlyResponse, nil
	}
	response, err := transportCall(ctx, res.reqOverride)
	return interceptAfter(ctx, c, c.interceptors, method, response, err)
}
