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
	"iter"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/internal/rest"
	"github.com/a2aproject/a2a-go/internal/sse"
	"github.com/a2aproject/a2a-go/log"
)

// RESTTransport implemetns Transport using RESTful HTTP API.
type RESTTransport struct {
	url        string
	httpClient *http.Client
}

// NewRESTTransport creates a new REST Transport for A2A protocol and communication
// By default, an HTTP client with 5-second timeout is used.
// For production deployments, provide a client with appropriate timeout, retry policy,
// and connection pooling configured for your requirements.
func NewRESTTransport(url string, client *http.Client) Transport {
	t := &RESTTransport{
		url:        url,
		httpClient: client,
	}

	if t.httpClient == nil {
		t.httpClient = &http.Client{
			Timeout: 5 * time.Second,
		}
	}
	return t
}

// WithRESTTransport returns a Client factory option that enables REST transport support.
func WithRESTTransport(client *http.Client) FactoryOption {
	return WithTransport(
		a2a.TransportProtocolHTTPJSON,
		TransportFactoryFn(func(ctx context.Context, url string, card *a2a.AgentCard) (Transport, error) {
			return NewRESTTransport(url, client), nil
		}),
	)
}

// sendRequest prepares the HTTP request and sends it to the server.
// It returns the HTTP response with the Body OPEN.
// The caller is responsible for closing the response body.
func (t *RESTTransport) sendRequest(ctx context.Context, method string, params ServiceParams, path string, payload any, acceptHeader string) (*http.Response, error) {
	reqBody, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w: %w", err, a2a.ErrInvalidRequest)
	}

	fullURL := t.url + path
	httpReq, err := http.NewRequestWithContext(ctx, method, fullURL, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", acceptHeader)

	for k, vals := range params {
		for _, v := range vals {
			httpReq.Header.Add(k, v)
		}
	}

	httpResp, err := t.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send HTTP request: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		defer func() {
			if err := httpResp.Body.Close(); err != nil {
				log.Error(ctx, "failed to close http response body", err)
			}
		}()
		return nil, rest.ToA2AError(httpResp)
	}

	return httpResp, nil
}

// doRequest is an adapter for Single Response calls
func (t *RESTTransport) doRequest(ctx context.Context, method string, params ServiceParams, path string, payload any, result any) error {
	resp, err := t.sendRequest(ctx, method, params, path, payload, "application/json")
	if err != nil {
		return err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Error(ctx, "failed to close http response body", err)
		}
	}()

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	}
	return nil
}

// doStreamingRequest is an adapter for Streaming Response calls
func (t *RESTTransport) doStreamingRequest(ctx context.Context, method string, params ServiceParams, path string, payload any) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		resp, err := t.sendRequest(ctx, method, params, path, payload, sse.ContentEventStream)
		if err != nil {
			yield(nil, err)
			return
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				log.Error(ctx, "failed to close http response body", err)
			}
		}()

		for data, err := range sse.ParseDataStream(resp.Body) {
			if err != nil {
				yield(nil, err)
				return
			}

			event, err := a2a.UnmarshalEventJSON(data)
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

// GetTask retrieves the current state of a task.
func (t *RESTTransport) GetTask(ctx context.Context, params ServiceParams, req *a2a.GetTaskRequest) (*a2a.Task, error) {
	path := rest.MakeGetTaskPath(string(req.ID))
	q := url.Values{}
	if req.HistoryLength != nil {
		q.Add("historyLength", strconv.Itoa(*req.HistoryLength))
	}
	if encoded := q.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var task a2a.Task

	if err := t.doRequest(ctx, "GET", params, path, nil, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

// ListTasks retrieves a list of tasks.
func (t *RESTTransport) ListTasks(ctx context.Context, params ServiceParams, req *a2a.ListTasksRequest) (*a2a.ListTasksResponse, error) {
	path := rest.MakeTasksListPath()

	query := url.Values{}
	if req.ContextID != "" {
		query.Add("contextId", string(req.ContextID))
	}
	if req.Status != "" {
		query.Add("status", string(req.Status))
	}
	if req.PageSize != 0 {
		query.Add("pageSize", strconv.Itoa(req.PageSize))
	}
	if req.PageToken != "" {
		query.Add("pageToken", string(req.PageToken))
	}
	if req.HistoryLength != 0 {
		query.Add("historyLength", strconv.Itoa(req.HistoryLength))
	}
	if req.StatusTimestampAfter != nil {
		query.Add("lastUpdatedAfter", req.StatusTimestampAfter.Format(time.RFC3339))
	}
	if req.IncludeArtifacts {
		query.Add("includeArtifacts", "true")
	}

	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}

	var result a2a.ListTasksResponse

	if err := t.doRequest(ctx, "GET", params, path, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// CancelTask requests cancellation of a task.
func (t *RESTTransport) CancelTask(ctx context.Context, params ServiceParams, req *a2a.CancelTaskRequest) (*a2a.Task, error) {
	path := rest.MakeCancelTaskPath(string(req.ID))
	var result a2a.Task

	if err := t.doRequest(ctx, "POST", params, path, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// SendMessage sends a non-streaming message to the agent.
func (t *RESTTransport) SendMessage(ctx context.Context, params ServiceParams, req *a2a.SendMessageRequest) (a2a.SendMessageResult, error) {
	path := rest.MakeSendMessagePath()

	var result json.RawMessage
	if err := t.doRequest(ctx, "POST", params, path, req, &result); err != nil {
		return nil, err
	}

	event, err := a2a.UnmarshalEventJSON(result)
	if err != nil {
		return nil, fmt.Errorf("result violates A2A spec - could not determine type: %w; data: %s", err, string(result))
	}

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

// SubscribeToTask reconnects to an SSE stream for an ongoing task.
func (t *RESTTransport) SubscribeToTask(ctx context.Context, params ServiceParams, req *a2a.SubscribeToTaskRequest) iter.Seq2[a2a.Event, error] {
	path := rest.MakeSubscribeTaskPath(string(req.ID))
	return t.doStreamingRequest(ctx, "POST", params, path, nil)
}

// SendStreamingMessage sends a streaming message to the agent and returns an SSE stream.
func (t *RESTTransport) SendStreamingMessage(ctx context.Context, params ServiceParams, req *a2a.SendMessageRequest) iter.Seq2[a2a.Event, error] {
	path := rest.MakeStreamMessagePath()
	return t.doStreamingRequest(ctx, "POST", params, path, req)
}

// GetTaskPushConfig retrieves the push notification configuration for a task.
func (t *RESTTransport) GetTaskPushConfig(ctx context.Context, params ServiceParams, req *a2a.GetTaskPushConfigRequest) (*a2a.TaskPushConfig, error) {
	path := rest.MakeGetPushConfigPath(string(req.TaskID), string(req.ID))
	var config a2a.TaskPushConfig

	if err := t.doRequest(ctx, "GET", params, path, nil, &config); err != nil {
		return nil, err
	}
	return &config, nil
}

// ListTaskPushConfig lists all push notification configurations for a task.
func (t *RESTTransport) ListTaskPushConfig(ctx context.Context, params ServiceParams, req *a2a.ListTaskPushConfigRequest) ([]*a2a.TaskPushConfig, error) {
	path := rest.MakeListPushConfigsPath(string(req.TaskID))
	var configs []*a2a.TaskPushConfig

	if err := t.doRequest(ctx, "GET", params, path, nil, &configs); err != nil {
		return nil, err
	}
	return configs, nil
}

// CreateTaskPushConfig sets or updates the push notification configuration for a task.
func (t *RESTTransport) CreateTaskPushConfig(ctx context.Context, params ServiceParams, req *a2a.CreateTaskPushConfigRequest) (*a2a.TaskPushConfig, error) {
	path := rest.MakeCreatePushConfigPath(string(req.TaskID))
	var config a2a.TaskPushConfig

	if err := t.doRequest(ctx, "POST", params, path, req, &config); err != nil {
		return nil, err
	}
	return &config, nil
}

// DeleteTaskPushConfig deletes a specific push notification configuration for a task.
func (t *RESTTransport) DeleteTaskPushConfig(ctx context.Context, params ServiceParams, req *a2a.DeleteTaskPushConfigRequest) error {
	path := rest.MakeDeletePushConfigPath(string(req.TaskID), string(req.ID))
	return t.doRequest(ctx, "DELETE", params, path, nil, nil)
}

// GetExtendedAgentCard retrieves the agent's A2A Agent Card.
func (t *RESTTransport) GetExtendedAgentCard(ctx context.Context, params ServiceParams) (*a2a.AgentCard, error) {
	path := rest.MakeGetExtendedAgentCardPath()
	var card a2a.AgentCard

	if err := t.doRequest(ctx, "GET", params, path, nil, &card); err != nil {
		return nil, err
	}
	return &card, nil
}

// Destroy closes the transport and releases resources.
func (t *RESTTransport) Destroy() error {
	// HTTP client doesn't need explicit cleanup in most cases
	// If a custom client with cleanup is needed, implement via options
	return nil
}
