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

package a2av0

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/http"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2aclient"
	"github.com/a2aproject/a2a-go/internal/jsonrpc"
	"github.com/a2aproject/a2a-go/internal/sse"
	"github.com/a2aproject/a2a-go/log"
	"github.com/google/uuid"
)

type JSONRPCTransportConfig struct {
	URL    string
	Client *http.Client
}

func NewJSONRPCTransportFactory(cfg JSONRPCTransportConfig) a2aclient.TransportFactory {
	return a2aclient.TransportFactoryFn(func(ctx context.Context, card *a2a.AgentCard, iface *a2a.AgentInterface) (a2aclient.Transport, error) {
		if cfg.URL == "" {
			cfg.URL = iface.URL
		}
		return NewJSONRPCTransport(cfg), nil
	})
}

func NewJSONRPCTransport(cfg JSONRPCTransportConfig) a2aclient.Transport {
	t := &jsonrpcTransport{
		url:        cfg.URL,
		httpClient: cfg.Client,
	}
	if t.httpClient == nil {
		t.httpClient = &http.Client{Timeout: 3 * time.Minute}
	}
	return t
}

type jsonrpcTransport struct {
	url        string
	httpClient *http.Client
}

func (t *jsonrpcTransport) newHTTPRequest(ctx context.Context, method string, params a2aclient.ServiceParams, payload any) (*http.Request, error) {
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
func (t *jsonrpcTransport) sendRequest(ctx context.Context, method string, params a2aclient.ServiceParams, req any) (json.RawMessage, error) {
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
		return nil, resp.Error.ToA2AError()
	}

	return resp.Result, nil
}

func (t *jsonrpcTransport) sendStreamingRequest(ctx context.Context, method string, params a2aclient.ServiceParams, req any) (io.ReadCloser, error) {
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

func parseSSEStream(body io.Reader) iter.Seq2[json.RawMessage, error] {
	return func(yield func(json.RawMessage, error) bool) {
		for data, err := range sse.ParseDataStream(body) {
			if err != nil {
				yield(nil, err)
				return
			}
			var resp jsonrpc.ClientResponse
			if err := json.Unmarshal(data, &resp); err != nil {
				yield(nil, fmt.Errorf("failed to parse SSE data: %w", err))
				return
			}
			if resp.Error != nil {
				yield(nil, resp.Error.ToA2AError())
				return
			}
			if !yield(resp.Result, nil) {
				return
			}
		}
	}
}

// SendMessage sends a non-streaming message to the agent.
func (t *jsonrpcTransport) SendMessage(ctx context.Context, params a2aclient.ServiceParams, req *a2a.SendMessageRequest) (a2a.SendMessageResult, error) {
	compatReq := toCompatSendMessageRequest(req)
	result, err := t.sendRequest(ctx, methodMessageSend, params, compatReq)
	if err != nil {
		return nil, err
	}

	compatEvent, err := unmarshalEventJSON(result)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal event: %w", err)
	}

	event, err := fromCompatEvent(compatEvent)
	if err != nil {
		return nil, fmt.Errorf("failed to convert event: %w", err)
	}

	switch e := event.(type) {
	case *a2a.Task:
		return e, nil
	case *a2a.Message:
		return e, nil
	default:
		return nil, fmt.Errorf("result violates A2A spec - expected Task or Message, got %T: %s", event, string(result))
	}
}

func (t *jsonrpcTransport) streamRequestToEvents(ctx context.Context, method string, params a2aclient.ServiceParams, req any) iter.Seq2[a2a.Event, error] {
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

		for result, err := range parseSSEStream(body) {
			if err != nil {
				yield(nil, err)
				return
			}

			compatEvent, err := unmarshalEventJSON(result)
			if err != nil {
				yield(nil, err)
				return
			}

			event, err := fromCompatEvent(compatEvent)
			if err != nil {
				yield(nil, err)
				return
			}

			if !yield(event, nil) {
				return
			}
		}
	}
}

// SendStreamingMessage sends a streaming message to the agent.
func (t *jsonrpcTransport) SendStreamingMessage(ctx context.Context, params a2aclient.ServiceParams, req *a2a.SendMessageRequest) iter.Seq2[a2a.Event, error] {
	compatReq := toCompatSendMessageRequest(req)
	return t.streamRequestToEvents(ctx, methodMessageStream, params, compatReq)
}

// GetTask retrieves the current state of a task.
func (t *jsonrpcTransport) GetTask(ctx context.Context, params a2aclient.ServiceParams, req *a2a.GetTaskRequest) (*a2a.Task, error) {
	compatReq := toCompatGetTaskRequest(req)
	result, err := t.sendRequest(ctx, methodTasksGet, params, compatReq)
	if err != nil {
		return nil, err
	}

	var compatTask task
	if err := json.Unmarshal(result, &compatTask); err != nil {
		return nil, fmt.Errorf("failed to unmarshal task: %w", err)
	}

	task, err := fromCompatTask(&compatTask)
	if err != nil {
		return nil, fmt.Errorf("failed to convert task: %w", err)
	}

	return task, nil
}

func (t *jsonrpcTransport) ListTasks(ctx context.Context, params a2aclient.ServiceParams, req *a2a.ListTasksRequest) (*a2a.ListTasksResponse, error) {
	return nil, a2a.ErrUnsupportedOperation
}

// CancelTask requests cancellation of a task.
func (t *jsonrpcTransport) CancelTask(ctx context.Context, params a2aclient.ServiceParams, req *a2a.CancelTaskRequest) (*a2a.Task, error) {
	compatReq := toCompatCancelTaskRequest(req)
	result, err := t.sendRequest(ctx, methodTasksCancel, params, compatReq)
	if err != nil {
		return nil, err
	}

	var compatTask task
	if err := json.Unmarshal(result, &compatTask); err != nil {
		return nil, fmt.Errorf("failed to unmarshal task: %w", err)
	}

	task, err := fromCompatTask(&compatTask)
	if err != nil {
		return nil, fmt.Errorf("failed to convert task: %w", err)
	}

	return task, nil
}

// SubscribeToTask reconnects to an SSE stream for an ongoing task.
func (t *jsonrpcTransport) SubscribeToTask(ctx context.Context, params a2aclient.ServiceParams, req *a2a.SubscribeToTaskRequest) iter.Seq2[a2a.Event, error] {
	compatReq := toCompatSubscribeToTaskRequest(req)
	return t.streamRequestToEvents(ctx, methodTasksResubscribe, params, compatReq)
}

// GetTaskPushConfig retrieves the push notification configuration for a task.
func (t *jsonrpcTransport) GetTaskPushConfig(ctx context.Context, params a2aclient.ServiceParams, req *a2a.GetTaskPushConfigRequest) (*a2a.TaskPushConfig, error) {
	compatReq := toCompatGetTaskPushConfigRequest(req)
	result, err := t.sendRequest(ctx, methodPushConfigGet, params, compatReq)
	if err != nil {
		return nil, err
	}

	var compatConfig taskPushConfig
	if err := json.Unmarshal(result, &compatConfig); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	config, err := fromCompatTaskPushConfig(&compatConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to convert config: %w", err)
	}
	return config, nil
}

// ListTaskPushConfig lists push notification configurations.
func (t *jsonrpcTransport) ListTaskPushConfig(ctx context.Context, params a2aclient.ServiceParams, req *a2a.ListTaskPushConfigRequest) ([]*a2a.TaskPushConfig, error) {
	compatReq := toCompatListTaskPushConfigRequest(req)
	result, err := t.sendRequest(ctx, methodPushConfigList, params, compatReq)
	if err != nil {
		return nil, err
	}

	var compatConfigs []*taskPushConfig
	if err := json.Unmarshal(result, &compatConfigs); err != nil {
		return nil, fmt.Errorf("failed to unmarshal configs: %w", err)
	}

	configs := make([]*a2a.TaskPushConfig, 0, len(compatConfigs))
	for _, compatConfig := range compatConfigs {
		config, err := fromCompatTaskPushConfig(compatConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to convert config: %w", err)
		}
		configs = append(configs, config)
	}

	return configs, nil
}

// CreateTaskPushConfig sets or updates the push notification configuration for a task.
func (t *jsonrpcTransport) CreateTaskPushConfig(ctx context.Context, params a2aclient.ServiceParams, req *a2a.CreateTaskPushConfigRequest) (*a2a.TaskPushConfig, error) {
	compatReq := toCompatCreateTaskPushConfigRequest(req)
	result, err := t.sendRequest(ctx, methodPushConfigSet, params, compatReq)
	if err != nil {
		return nil, err
	}

	var compatConfig taskPushConfig
	if err := json.Unmarshal(result, &compatConfig); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	config, err := fromCompatTaskPushConfig(&compatConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to convert config: %w", err)
	}
	return config, nil
}

// DeleteTaskPushConfig deletes a push notification configuration.
func (t *jsonrpcTransport) DeleteTaskPushConfig(ctx context.Context, params a2aclient.ServiceParams, req *a2a.DeleteTaskPushConfigRequest) error {
	compatReq := toCompatDeleteTaskPushConfigRequest(req)
	_, err := t.sendRequest(ctx, methodPushConfigDelete, params, compatReq)
	return err
}

// GetExtendedAgentCard retrieves the agent's card.
func (t *jsonrpcTransport) GetExtendedAgentCard(ctx context.Context, params a2aclient.ServiceParams) (*a2a.AgentCard, error) {
	result, err := t.sendRequest(ctx, methodGetExtendedAgentCard, params, nil)
	if err != nil {
		return nil, err
	}

	parser := NewAgentCardParser()
	card, err := parser(result)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal agent card: %w", err)
	}
	return card, nil
}

func (t *jsonrpcTransport) Destroy() error {
	return nil
}
