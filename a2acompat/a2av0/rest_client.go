// Copyright 2026 The A2A Authors
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
	"net/url"
	"strconv"
	"time"

	a2alegacy "github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/internal/rest"
	"github.com/a2aproject/a2a-go/v2/internal/sse"
	"github.com/a2aproject/a2a-go/v2/log"
)

// WithRESTTransport creates a factory option for the v0.3 HTTP+JSON transport.
func WithRESTTransport(cfg RESTTransportConfig) a2aclient.FactoryOption {
	return a2aclient.WithCompatTransport(
		Version,
		a2a.TransportProtocolHTTPJSON,
		NewRESTTransportFactory(cfg),
	)
}

// RESTTransportConfig holds configuration for the v0.3 HTTP+JSON transport.
type RESTTransportConfig struct {
	// URL is the base URL of the v0.3 REST server. If empty, the URL from the
	// agent card interface will be used.
	URL    string
	Client *http.Client
}

// NewRESTTransportFactory creates a new [a2aclient.TransportFactory] for the v0.3 HTTP+JSON protocol binding.
func NewRESTTransportFactory(cfg RESTTransportConfig) a2aclient.TransportFactory {
	return a2aclient.TransportFactoryFn(func(ctx context.Context, card *a2a.AgentCard, iface *a2a.AgentInterface) (a2aclient.Transport, error) {
		cfgCopy := cfg
		cfgCopy.URL = iface.URL
		return NewRESTTransport(cfgCopy)
	})
}

// NewRESTTransport creates a new [a2aclient.Transport] that speaks the v0.3 HTTP+JSON protocol.
//
// It translates v1.0 client calls to v0.3 HTTP requests and converts the v0.3
// responses back to v1.0 types. Use this to connect a v1.0 Go client to a v0.3
// server (e.g. Python ADK Agent Engine deployments).
func NewRESTTransport(cfg RESTTransportConfig) (a2aclient.Transport, error) {
	u, err := url.Parse(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}
	client := cfg.Client
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Minute}
	}
	return &restCompatTransport{base: u, httpClient: client}, nil
}

type restCompatTransport struct {
	base       *url.URL
	httpClient *http.Client
}

type compatRestReq struct {
	method    string
	path      string
	query     url.Values
	params    a2aclient.ServiceParams
	payload   any
	streaming bool
}

func (t *restCompatTransport) sendRequest(ctx context.Context, req *compatRestReq) (*http.Response, error) {
	var bodyReader io.Reader
	if req.payload != nil {
		body, err := marshalSnakeCase(req.payload)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request: %w", err)
		}
		bodyReader = bytes.NewBuffer(body)
	}

	rel, err := url.Parse(req.path)
	if err != nil {
		return nil, fmt.Errorf("failed to parse path: %w", err)
	}
	u := t.base.JoinPath(rel.Path)
	if len(req.query) > 0 {
		u.RawQuery = req.query.Encode()
	}

	httpReq, err := http.NewRequestWithContext(ctx, req.method, u.String(), bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}
	if req.payload != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	if req.streaming {
		httpReq.Header.Set("Accept", sse.ContentEventStream)
	} else {
		httpReq.Header.Set("Accept", "application/json")
	}
	for k, vals := range FromServiceParams(req.params) {
		for _, v := range vals {
			httpReq.Header.Add(k, v)
		}
	}

	resp, err := t.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send HTTP request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer func() {
			if err := resp.Body.Close(); err != nil {
				log.Error(ctx, "failed to close http response body", err)
			}
		}()
		return nil, rest.ToA2AError(resp)
	}
	return resp, nil
}

func (t *restCompatTransport) doRequest(ctx context.Context, req *compatRestReq, result any) error {
	resp, err := t.sendRequest(ctx, req)
	if err != nil {
		return err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Error(ctx, "failed to close http response body", err)
		}
	}()
	if result != nil {
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("failed to read response: %w", err)
		}
		if err := unmarshalSnakeCase(data, result); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	}
	return nil
}

func (t *restCompatTransport) doStreamingRequest(ctx context.Context, req *compatRestReq) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		req.streaming = true
		resp, err := t.sendRequest(ctx, req)
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
			// v0.3 SSE events use snake_case keys; transform to camelCase
			// before legacy unmarshal.
			camelData, err := transformJSONKeys(data, snakeToCamel)
			if err != nil {
				yield(nil, fmt.Errorf("failed to transform SSE event keys: %w", err))
				return
			}
			compatEvent, err := a2alegacy.UnmarshalEventJSON(camelData)
			if err != nil {
				yield(nil, fmt.Errorf("failed to unmarshal SSE event: %w", err))
				return
			}
			event, err := ToV1Event(compatEvent)
			if err != nil {
				yield(nil, fmt.Errorf("failed to convert SSE event: %w", err))
				return
			}
			if !yield(event, nil) {
				return
			}
		}
	}
}

// SendMessage implements [a2aclient.Transport].
func (t *restCompatTransport) SendMessage(ctx context.Context, params a2aclient.ServiceParams, req *a2a.SendMessageRequest) (a2a.SendMessageResult, error) {
	compatReq := FromV1SendMessageRequest(req)
	resp, err := t.sendRequest(ctx, &compatRestReq{
		method:  "POST",
		path:    rest.MakeSendMessagePath(),
		params:  params,
		payload: compatReq,
	})
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Error(ctx, "failed to close http response body", err)
		}
	}()
	rawData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	// Transform snake_case response to camelCase for legacy unmarshal.
	camelData, err := transformJSONKeys(rawData, snakeToCamel)
	if err != nil {
		return nil, fmt.Errorf("failed to transform send message result keys: %w", err)
	}
	compatEvent, err := a2alegacy.UnmarshalEventJSON(camelData)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal send message result: %w", err)
	}
	event, err := ToV1Event(compatEvent)
	if err != nil {
		return nil, fmt.Errorf("failed to convert send message result: %w", err)
	}
	switch e := event.(type) {
	case *a2a.Task:
		return e, nil
	case *a2a.Message:
		return e, nil
	default:
		return nil, fmt.Errorf("result violates A2A spec - expected Task or Message, got %T", event)
	}
}

// SendStreamingMessage implements [a2aclient.Transport].
func (t *restCompatTransport) SendStreamingMessage(ctx context.Context, params a2aclient.ServiceParams, req *a2a.SendMessageRequest) iter.Seq2[a2a.Event, error] {
	compatReq := FromV1SendMessageRequest(req)
	return t.doStreamingRequest(ctx, &compatRestReq{
		method:  "POST",
		path:    rest.MakeStreamMessagePath(),
		params:  params,
		payload: compatReq,
	})
}

// GetTask implements [a2aclient.Transport].
func (t *restCompatTransport) GetTask(ctx context.Context, params a2aclient.ServiceParams, req *a2a.GetTaskRequest) (*a2a.Task, error) {
	q := url.Values{}
	if req.HistoryLength != nil {
		q.Set("historyLength", strconv.Itoa(*req.HistoryLength))
	}
	var compatTask a2alegacy.Task
	if err := t.doRequest(ctx, &compatRestReq{
		method: "GET",
		path:   rest.MakeGetTaskPath(string(req.ID)),
		query:  q,
		params: params,
	}, &compatTask); err != nil {
		return nil, err
	}
	task, err := ToV1Task(&compatTask)
	if err != nil {
		return nil, fmt.Errorf("failed to convert task: %w", err)
	}
	return task, nil
}

// ListTasks implements [a2aclient.Transport].
func (t *restCompatTransport) ListTasks(ctx context.Context, params a2aclient.ServiceParams, req *a2a.ListTasksRequest) (*a2a.ListTasksResponse, error) {
	legacyReq := FromV1ListTasksRequest(req)
	q := url.Values{}
	if legacyReq.ContextID != "" {
		q.Set("contextId", legacyReq.ContextID)
	}
	if legacyReq.Status != "" {
		q.Set("status", string(legacyReq.Status))
	}
	if legacyReq.PageSize != 0 {
		q.Set("pageSize", strconv.Itoa(legacyReq.PageSize))
	}
	if legacyReq.PageToken != "" {
		q.Set("pageToken", legacyReq.PageToken)
	}
	if legacyReq.HistoryLength != 0 {
		q.Set("historyLength", strconv.Itoa(legacyReq.HistoryLength))
	}
	if legacyReq.LastUpdatedAfter != nil {
		q.Set("lastUpdatedAfter", legacyReq.LastUpdatedAfter.Format(time.RFC3339))
	}
	if legacyReq.IncludeArtifacts {
		q.Set("includeArtifacts", "true")
	}

	var compatResp a2alegacy.ListTasksResponse
	if err := t.doRequest(ctx, &compatRestReq{
		method: "GET",
		path:   rest.MakeListTasksPath(),
		query:  q,
		params: params,
	}, &compatResp); err != nil {
		return nil, err
	}
	resp, err := ToV1ListTasksResponse(&compatResp)
	if err != nil {
		return nil, fmt.Errorf("failed to convert list tasks response: %w", err)
	}
	return resp, nil
}

// CancelTask implements [a2aclient.Transport].
func (t *restCompatTransport) CancelTask(ctx context.Context, params a2aclient.ServiceParams, req *a2a.CancelTaskRequest) (*a2a.Task, error) {
	var compatTask a2alegacy.Task
	if err := t.doRequest(ctx, &compatRestReq{
		method: "POST",
		path:   rest.MakeCancelTaskPath(string(req.ID)),
		params: params,
	}, &compatTask); err != nil {
		return nil, err
	}
	task, err := ToV1Task(&compatTask)
	if err != nil {
		return nil, fmt.Errorf("failed to convert task: %w", err)
	}
	return task, nil
}

// SubscribeToTask implements [a2aclient.Transport].
func (t *restCompatTransport) SubscribeToTask(ctx context.Context, params a2aclient.ServiceParams, req *a2a.SubscribeToTaskRequest) iter.Seq2[a2a.Event, error] {
	return t.doStreamingRequest(ctx, &compatRestReq{
		method: "POST",
		path:   rest.MakeSubscribeTaskPath(string(req.ID)),
		params: params,
	})
}

// GetTaskPushConfig implements [a2aclient.Transport].
func (t *restCompatTransport) GetTaskPushConfig(ctx context.Context, params a2aclient.ServiceParams, req *a2a.GetTaskPushConfigRequest) (*a2a.TaskPushConfig, error) {
	var compatConfig a2alegacy.TaskPushConfig
	if err := t.doRequest(ctx, &compatRestReq{
		method: "GET",
		path:   rest.MakeGetPushConfigPath(string(req.TaskID), req.ID),
		params: params,
	}, &compatConfig); err != nil {
		return nil, err
	}
	config, err := ToV1TaskPushConfig(&compatConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to convert push config: %w", err)
	}
	return config, nil
}

// ListTaskPushConfigs implements [a2aclient.Transport].
func (t *restCompatTransport) ListTaskPushConfigs(ctx context.Context, params a2aclient.ServiceParams, req *a2a.ListTaskPushConfigRequest) ([]*a2a.TaskPushConfig, error) {
	var compatConfigs []*a2alegacy.TaskPushConfig
	if err := t.doRequest(ctx, &compatRestReq{
		method: "GET",
		path:   rest.MakeListPushConfigsPath(string(req.TaskID)),
		params: params,
	}, &compatConfigs); err != nil {
		return nil, err
	}
	configs := make([]*a2a.TaskPushConfig, 0, len(compatConfigs))
	for _, c := range compatConfigs {
		config, err := ToV1TaskPushConfig(c)
		if err != nil {
			return nil, fmt.Errorf("failed to convert push config: %w", err)
		}
		configs = append(configs, config)
	}
	return configs, nil
}

// CreateTaskPushConfig implements [a2aclient.Transport].
func (t *restCompatTransport) CreateTaskPushConfig(ctx context.Context, params a2aclient.ServiceParams, req *a2a.CreateTaskPushConfigRequest) (*a2a.TaskPushConfig, error) {
	compatPushConfig := FromV1PushConfig(&req.Config)
	var compatConfig a2alegacy.TaskPushConfig
	if err := t.doRequest(ctx, &compatRestReq{
		method:  "POST",
		path:    rest.MakeCreatePushConfigPath(string(req.TaskID)),
		params:  params,
		payload: compatPushConfig,
	}, &compatConfig); err != nil {
		return nil, err
	}
	config, err := ToV1TaskPushConfig(&compatConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to convert push config: %w", err)
	}
	return config, nil
}

// DeleteTaskPushConfig implements [a2aclient.Transport].
func (t *restCompatTransport) DeleteTaskPushConfig(ctx context.Context, params a2aclient.ServiceParams, req *a2a.DeleteTaskPushConfigRequest) error {
	return t.doRequest(ctx, &compatRestReq{
		method: "DELETE",
		path:   rest.MakeDeletePushConfigPath(string(req.TaskID), req.ID),
		params: params,
	}, nil)
}

// GetExtendedAgentCard implements [a2aclient.Transport].
func (t *restCompatTransport) GetExtendedAgentCard(ctx context.Context, params a2aclient.ServiceParams, req *a2a.GetExtendedAgentCardRequest) (*a2a.AgentCard, error) {
	var rawCard json.RawMessage
	if err := t.doRequest(ctx, &compatRestReq{
		method: "GET",
		path:   rest.MakeGetExtendedAgentCardPath(),
		params: params,
	}, &rawCard); err != nil {
		return nil, err
	}
	parser := NewAgentCardParser()
	card, err := parser(rawCard)
	if err != nil {
		return nil, fmt.Errorf("failed to parse agent card: %w", err)
	}
	return card, nil
}

// Destroy implements [a2aclient.Transport].
func (t *restCompatTransport) Destroy() error {
	return nil
}
