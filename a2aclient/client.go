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
	"slices"
	"strings"
	"sync/atomic"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/internal/utils"
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
	// DisableTenantPropagation disables tenant propagation.
	// If true, the client will not attach tenant from context to the request.
	DisableTenantPropagation bool
}

// Client represents a transport-agnostic implementation of A2A client.
// The actual call is delegated to a specific [Transport] implementation.
// [CallInterceptor]-s are applied before and after every protocol call.
type Client struct {
	config          Config
	transport       Transport
	protocolVersion a2a.ProtocolVersion
	endpoint        a2a.AgentInterface
	interceptors    []CallInterceptor

	card atomic.Pointer[a2a.AgentCard]
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
	req = c.withDefaultSendConfig(req)
	return doCall(ctx, c, "SendMessage", req, c.transport.SendMessage)
}

// SendStreamingMessage implements the 'SendStreamingMessage' protocol method (streaming).
func (c *Client) SendStreamingMessage(ctx context.Context, req *a2a.SendMessageRequest) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		req = c.withDefaultSendConfig(req)
		streamCall(ctx, c, "SendStreamingMessage", req, sendStreamingMessage(c), yield)
	}
}

// SubscribeToTask implements the `SubscribeToTask` protocol method.
func (c *Client) SubscribeToTask(ctx context.Context, req *a2a.SubscribeToTaskRequest) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		streamCall(ctx, c, "SubscribeToTask", req, streamHandler(c.transport.SubscribeToTask), yield)
	}
}

// GetTaskPushConfig implements the `GetTaskPushNotificationConfig` protocol method.
func (c *Client) GetTaskPushConfig(ctx context.Context, req *a2a.GetTaskPushConfigRequest) (*a2a.PushConfig, error) {
	return doCall(ctx, c, "GetTaskPushConfig", req, c.transport.GetTaskPushConfig)
}

// ListTaskPushConfigs implements the `ListTaskPushNotificationConfig` protocol method.
func (c *Client) ListTaskPushConfigs(ctx context.Context, req *a2a.ListTaskPushConfigRequest) ([]*a2a.PushConfig, error) {
	return doCall(ctx, c, "ListTaskPushConfigs", req, c.transport.ListTaskPushConfigs)
}

// CreateTaskPushConfig implements the `CreateTaskPushNotificationConfig` protocol method.
func (c *Client) CreateTaskPushConfig(ctx context.Context, req *a2a.PushConfig) (*a2a.PushConfig, error) {
	return doCall(ctx, c, "CreateTaskPushConfig", req, c.transport.CreateTaskPushConfig)
}

// DeleteTaskPushConfig implements the `DeleteTaskPushNotificationConfig` protocol method.
func (c *Client) DeleteTaskPushConfig(ctx context.Context, req *a2a.DeleteTaskPushConfigRequest) error {
	_, err := doCall(ctx, c, "DeleteTaskPushConfig", req, func(ctx context.Context, params ServiceParams, req *a2a.DeleteTaskPushConfigRequest) (struct{}, error) {
		return struct{}{}, c.transport.DeleteTaskPushConfig(ctx, params, req)
	})
	return err
}

// GetExtendedAgentCard implements the `GetExtendedAgentCard` protocol method.
func (c *Client) GetExtendedAgentCard(ctx context.Context, req *a2a.GetExtendedAgentCardRequest) (*a2a.AgentCard, error) {
	card := c.card.Load()
	if card != nil && !card.Capabilities.ExtendedAgentCard {
		return nil, a2a.ErrExtendedCardNotConfigured
	}
	return doCall(ctx, c, "GetExtendedAgentCard", req, c.transport.GetExtendedAgentCard)
}

// UpdateCard updates the agent card used by the client.
func (c *Client) UpdateCard(card *a2a.AgentCard) error {
	found := false
	for _, iface := range card.SupportedInterfaces {
		if *iface == c.endpoint {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("client needs to be recreated, %+v not longer listed as supported", c.endpoint)
	}
	c.card.Store(card)
	return nil
}

// Card returns the last AgentCard set on this client or nil if it was created from an [a2a.AgentInterface] handle
// and was not updated using [Client.UpdateCard].
func (c *Client) Card() *a2a.AgentCard {
	return c.card.Load()
}

// Destroy cleans up resources associated with the client.
func (c *Client) Destroy() error {
	return c.transport.Destroy()
}

func (c *Client) withDefaultSendConfig(message *a2a.SendMessageRequest) *a2a.SendMessageRequest {
	if c.config.PushConfig == nil && c.config.AcceptedOutputModes == nil {
		// Attach config to all v0.* protocol requests to preserve legacy blocking=true default
		// which will be applied on conversion.
		if message.Config != nil || !strings.HasPrefix(string(c.endpoint.ProtocolVersion), "0") {
			return message
		}
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
	return &result
}

func (c *Client) newRequest(ctx context.Context, method string, payload any) Request {
	serviceParams := serviceParamsCloneFrom(ctx)
	serviceParams.set(a2a.SvcParamVersion, string(c.protocolVersion))
	return Request{
		Method:          method,
		BaseURL:         c.endpoint.URL,
		ProtocolBinding: c.endpoint.ProtocolBinding,
		ServiceParams:   serviceParams,
		Card:            c.card.Load(),
		Payload:         payload,
	}
}

func interceptCalls(c *Client, next callHandler) callHandler {
	return func(ctx context.Context, req Request) iter.Seq2[any, error] {
		return func(yield func(any, error) bool) {
			ctx, req := ctx, req
			req.ServiceParams = req.ServiceParams.clone()
			for i, interceptor := range c.interceptors {
				localCtx, result, err := interceptor.Before(ctx, &req)
				ctx = localCtx
				if err != nil || result != nil {
					yield(interceptAfter(ctx, c, c.interceptors[:i+1], req, result, err))
					return
				}
			}
			for payload, err := range next(ctx, req) {
				if !yield(interceptAfter(ctx, c, c.interceptors, req, payload, err)) {
					return
				}
			}
		}
	}
}

func interceptAfter(ctx context.Context, c *Client, interceptors []CallInterceptor, req Request, payload any, err error) (any, error) {
	resp := Response{
		BaseURL:       c.endpoint.URL,
		Method:        req.Method,
		ServiceParams: req.ServiceParams,
		Payload:       payload,
		Card:          c.card.Load(),
		Err:           err,
	}
	for _, interceptor := range slices.Backward(interceptors) {
		if err := interceptor.After(ctx, &resp); err != nil {
			return nil, err
		}
	}
	return resp.Payload, resp.Err
}

func unaryHandler[Req, Resp any](transportCall func(context.Context, ServiceParams, Req) (Resp, error)) callHandler {
	return func(ctx context.Context, req Request) iter.Seq2[any, error] {
		return func(yield func(any, error) bool) {
			typed, err := utils.CastPayload[Req](req.Payload)
			if err != nil {
				yield(nil, err)
				return
			}
			yield(transportCall(ctx, req.ServiceParams, typed))
		}
	}
}

func streamHandler[Req any](transportCall func(context.Context, ServiceParams, Req) iter.Seq2[a2a.Event, error]) callHandler {
	return func(ctx context.Context, req Request) iter.Seq2[any, error] {
		return func(yield func(any, error) bool) {
			typed, err := utils.CastPayload[Req](req.Payload)
			if err != nil {
				yield(nil, err)
				return
			}
			for event, err := range transportCall(ctx, req.ServiceParams, typed) {
				if !yield(event, err) {
					return
				}
			}
		}
	}
}

func sendStreamingMessage(c *Client) callHandler {
	return func(ctx context.Context, req Request) iter.Seq2[any, error] {
		if card := c.card.Load(); card != nil && !card.Capabilities.Streaming {
			return unaryHandler(c.transport.SendMessage)(ctx, req)
		}
		return streamHandler(c.transport.SendStreamingMessage)(ctx, req)
	}
}

func doCall[Req any, Resp any](
	ctx context.Context, c *Client, method string, req Req,
	transportCall func(context.Context, ServiceParams, Req) (Resp, error),
) (Resp, error) {
	for payload, err := range interceptCalls(c, unaryHandler(transportCall))(ctx, c.newRequest(ctx, method, req)) {
		return utils.CastResult[Resp](payload, err)
	}
	var zero Resp
	return zero, fmt.Errorf("%s produced no result", method)
}

func streamCall(ctx context.Context, c *Client, method string, req any, handler callHandler, yield func(a2a.Event, error) bool) {
	for payload, err := range interceptCalls(c, handler)(ctx, c.newRequest(ctx, method, req)) {
		event, err := utils.CastResult[a2a.Event](payload, err)
		if err != nil {
			yield(nil, err)
			return
		}
		if !yield(event, nil) {
			return
		}
	}
}
