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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/http"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/internal/jsonrpc"
	"github.com/a2aproject/a2a-go/v2/internal/sse"
	"github.com/a2aproject/a2a-go/v2/log"
	"github.com/google/uuid"
)

// JSONRPCOption configures optional parameters for the JSONRPC transport.
// Options are applied during NewJSONRPCTransport initialization.
type JSONRPCOption func(*jsonrpcTransport)

// WithJSONRPCTransport returns a Client factory option that enables JSON-RPC transport support.
// When applied, the client will use JSON-RPC 2.0 over HTTP for all A2A protocol communication
// as defined in the A2A specification §7.
func WithJSONRPCTransport(client *http.Client) FactoryOption {
	return WithTransport(
		a2a.TransportProtocolJSONRPC,
		TransportFactoryFn(func(ctx context.Context, card *a2a.AgentCard, iface *a2a.AgentInterface) (Transport, error) {
			return NewJSONRPCTransport(iface.URL, client), nil
		}),
	)
}

// NewJSONRPCTransport creates a new JSON-RPC transport for A2A protocol communication.
// For production deployments, provide a client with appropriate timeout, retry policy,
// and connection pooling configured for your requirements.
//
// To create an A2A client with custom HTTP client use WithJSONRPCTransport option:
//
//	httpClient := &http.Client{Timeout: 5 * time.Minute}
//	client := NewFromCard(ctx, card, WithJSONRPCTransport(httpClient))
func NewJSONRPCTransport(url string, client *http.Client) Transport {
	t := &jsonrpcTransport{
		url:        url,
		httpClient: client,
	}

	if t.httpClient == nil {
		t.httpClient = &http.Client{Timeout: defaultRequestTimeout}
	}

	return t
}

// jsonrpcTransport implements Transport using JSON-RPC 2.0 over HTTP.
type jsonrpcTransport struct {
	url        string
	httpClient *http.Client
}

var _ Transport = (*jsonrpcTransport)(nil)

func (t *jsonrpcTransport) newHTTPRequest(ctx context.Context, method string, params ServiceParams, payload any) (*http.Request, error) {
	req := jsonrpc.ClientRequest{
		JSONRPC: jsonrpc.Version,
		Method:  method,
		Params:  payload,
		ID:      uuid.NewString(),
	}

	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", t.url, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	httpReq.Header.Set("Content-Type", jsonrpc.ContentJSON)

	for k, vals := range params {
		for _, v := range vals {
			httpReq.Header.Add(k, v)
		}
	}

	return httpReq, nil
}

// sendRequest sends a non-streaming JSON-RPC request and returns the response.
func (t *jsonrpcTransport) sendRequest(ctx context.Context, method string, params ServiceParams, req any) (json.RawMessage, error) {
	httpReq, err := t.newHTTPRequest(ctx, method, params, req)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	httpResp, err := t.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send HTTP request: %w", err)
	}
	defer func() {
		if err := httpResp.Body.Close(); err != nil {
			log.Error(ctx, "failed to close http response body", err)
		}
	}()

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected HTTP status: %s", httpResp.Status)
	}

	var resp jsonrpc.ClientResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if resp.Error != nil {
		return nil, jsonrpc.FromJSONRPCError(resp.Error)
	}

	return resp.Result, nil
}

func sendRequestAs[T any](t *jsonrpcTransport, ctx context.Context, method string, params ServiceParams, req any, resultName string) (T, error) {
	result, err := t.sendRequest(ctx, method, params, req)
	if err != nil {
		var zero T
		return zero, err
	}
	return jsonrpc.UnmarshalResult[T](result, resultName)
}

// sendStreamingRequest sends a streaming JSON-RPC request and returns an SSE stream.
func (t *jsonrpcTransport) sendStreamingRequest(ctx context.Context, method string, params ServiceParams, req any) (io.ReadCloser, error) {
	httpReq, err := t.newHTTPRequest(ctx, method, params, req)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}
	httpReq.Header.Set("Accept", sse.ContentEventStream)

	httpResp, err := t.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send HTTP request: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		if err := httpResp.Body.Close(); err != nil {
			log.Error(ctx, "failed to close http response body", err)
		}
		return nil, fmt.Errorf("unexpected HTTP status: %s", httpResp.Status)
	}

	return httpResp.Body, nil
}

// SendMessage implements [a2a.Transport].
func (t *jsonrpcTransport) SendMessage(ctx context.Context, params ServiceParams, req *a2a.SendMessageRequest) (a2a.SendMessageResult, error) {
	result, err := t.sendRequest(ctx, jsonrpc.MethodMessageSend, params, req)
	if err != nil {
		return nil, err
	}

	var sr a2a.StreamResponse
	if err := json.Unmarshal(result, &sr); err != nil {
		return nil, fmt.Errorf("result violates A2A spec - could not determine type: %w; data: %s", err, string(result))
	}
	event := sr.Event

	// SendMessage can return either a Task or a Message
	switch e := event.(type) {
	case *a2a.Task:
		return e, nil
	case *a2a.Message:
		return e, nil
	default:
		return nil, fmt.Errorf("result violates A2A spec - expected Task or Message, got %T: %s", event, string(result))
	}
}

// streamRequestToEvents handles SSE streaming for JSON-RPC methods.
// It converts the SSE stream into a sequence of A2A events.
func (t *jsonrpcTransport) streamRequestToEvents(ctx context.Context, method string, params ServiceParams, req any) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		body, err := t.sendStreamingRequest(ctx, method, params, req)
		if err != nil {
			yield(nil, err)
			return
		}
		defer func() {
			if err := body.Close(); err != nil {
				log.Error(ctx, "failed to close http response body", err)
			}
		}()

		for result, err := range jsonrpc.ParseSSEStream(body) {
			if err != nil {
				yield(nil, err)
				return
			}

			var sr a2a.StreamResponse
			if err := json.Unmarshal(result, &sr); err != nil {
				yield(nil, err)
				return
			}
			event := sr.Event

			if !yield(event, nil) {
				return
			}
		}
	}
}

// SendStreamingMessage implements [a2a.Transport].
func (t *jsonrpcTransport) SendStreamingMessage(ctx context.Context, params ServiceParams, req *a2a.SendMessageRequest) iter.Seq2[a2a.Event, error] {
	return t.streamRequestToEvents(ctx, jsonrpc.MethodMessageStream, params, req)
}

// GetTask implements [a2a.Transport].
func (t *jsonrpcTransport) GetTask(ctx context.Context, params ServiceParams, req *a2a.GetTaskRequest) (*a2a.Task, error) {
	task, err := sendRequestAs[a2a.Task](t, ctx, jsonrpc.MethodTasksGet, params, req, "task")
	if err != nil {
		return nil, err
	}

	return &task, nil
}

// ListTasks implements [a2a.Transport].
func (t *jsonrpcTransport) ListTasks(ctx context.Context, params ServiceParams, req *a2a.ListTasksRequest) (*a2a.ListTasksResponse, error) {
	response, err := sendRequestAs[a2a.ListTasksResponse](t, ctx, jsonrpc.MethodTasksList, params, req, "response")
	if err != nil {
		return nil, err
	}

	return &response, nil
}

// CancelTask implements [a2a.Transport].
func (t *jsonrpcTransport) CancelTask(ctx context.Context, params ServiceParams, req *a2a.CancelTaskRequest) (*a2a.Task, error) {
	task, err := sendRequestAs[a2a.Task](t, ctx, jsonrpc.MethodTasksCancel, params, req, "task")
	if err != nil {
		return nil, err
	}

	return &task, nil
}

// SubscribeToTask implements [a2a.Transport].
func (t *jsonrpcTransport) SubscribeToTask(ctx context.Context, params ServiceParams, req *a2a.SubscribeToTaskRequest) iter.Seq2[a2a.Event, error] {
	return t.streamRequestToEvents(ctx, jsonrpc.MethodTasksResubscribe, params, req)
}

// GetTaskPushConfig implements [a2a.Transport].
func (t *jsonrpcTransport) GetTaskPushConfig(ctx context.Context, params ServiceParams, req *a2a.GetTaskPushConfigRequest) (*a2a.PushConfig, error) {
	config, err := sendRequestAs[a2a.PushConfig](t, ctx, jsonrpc.MethodPushConfigGet, params, req, "config")
	if err != nil {
		return nil, err
	}

	return &config, nil
}

// ListTaskPushConfig implements [a2a.Transport].
func (t *jsonrpcTransport) ListTaskPushConfigs(ctx context.Context, params ServiceParams, req *a2a.ListTaskPushConfigRequest) ([]*a2a.PushConfig, error) {
	return sendRequestAs[[]*a2a.PushConfig](t, ctx, jsonrpc.MethodPushConfigList, params, req, "configs")
}

// CreateTaskPushConfig implements [a2a.Transport].
func (t *jsonrpcTransport) CreateTaskPushConfig(ctx context.Context, params ServiceParams, req *a2a.PushConfig) (*a2a.PushConfig, error) {
	config, err := sendRequestAs[a2a.PushConfig](t, ctx, jsonrpc.MethodPushConfigSet, params, req, "config")
	if err != nil {
		return nil, err
	}

	return &config, nil
}

// DeleteTaskPushConfig implements [a2a.Transport].
func (t *jsonrpcTransport) DeleteTaskPushConfig(ctx context.Context, params ServiceParams, req *a2a.DeleteTaskPushConfigRequest) error {
	_, err := t.sendRequest(ctx, jsonrpc.MethodPushConfigDelete, params, req)
	return err
}

// GetExtendedAgentCard implements [a2a.Transport].
func (t *jsonrpcTransport) GetExtendedAgentCard(ctx context.Context, params ServiceParams, req *a2a.GetExtendedAgentCardRequest) (*a2a.AgentCard, error) {
	card, err := sendRequestAs[a2a.AgentCard](t, ctx, jsonrpc.MethodGetExtendedAgentCard, params, req, "agent card")
	if err != nil {
		return nil, err
	}
	return &card, nil
}

// Destroy implements [a2a.Transport].
func (t *jsonrpcTransport) Destroy() error {
	// HTTP client doesn't need explicit cleanup in most cases
	// If a custom client with cleanup is needed, implement via options
	return nil
}
