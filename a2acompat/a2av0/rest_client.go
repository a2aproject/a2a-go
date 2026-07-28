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

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2apb/v0/pbjson"
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
// It translates v1.0 client calls into A2A v0.3 REST requests (proto-JSON of the
// google.a2a.v1 proto) and converts the
// responses back into v1.0 SDK types. Use it to connect a v1.0 Go client to a
// v0.3 server (e.g. Python ADK Agent Engine deployments).
func NewRESTTransport(cfg RESTTransportConfig) (a2aclient.Transport, error) {
	u, err := url.Parse(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}
	client := cfg.Client
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Minute}
	}
	return &restCompatTransport{base: u, httpClient: client, paths: rest.NewPathBuilder(RESTPathPrefix)}, nil
}

type restCompatTransport struct {
	base       *url.URL
	httpClient *http.Client
	paths      rest.PathBuilder
}

type compatRestReq struct {
	method    string
	path      string
	query     url.Values
	params    a2aclient.ServiceParams
	body      []byte
	streaming bool
}

func (t *restCompatTransport) sendRequest(ctx context.Context, req *compatRestReq) (*http.Response, error) {
	var bodyReader io.Reader
	if len(req.body) > 0 {
		bodyReader = bytes.NewReader(req.body)
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
	if len(req.body) > 0 {
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
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		defer func() {
			if err := resp.Body.Close(); err != nil {
				log.Error(ctx, "failed to close http response body", err)
			}
		}()
		return nil, rest.FromRESTError(resp)
	}
	return resp, nil
}

// doRequest sends a request and returns the raw response body.
func (t *restCompatTransport) doRequest(ctx context.Context, req *compatRestReq) ([]byte, error) {
	resp, err := t.sendRequest(ctx, req)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Error(ctx, "failed to close http response body", err)
		}
	}()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	return data, nil
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
			if restErr := parseErrorBytes(data); restErr != nil {
				yield(nil, restErr)
				return
			}
			event, err := pbjson.FromProtoStreamEvent(data)
			if err != nil {
				yield(nil, fmt.Errorf("failed to unmarshal SSE event: %w", err))
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
	body, err := pbjson.ToProtoSendMessageRequest(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal SendMessageRequest: %w", err)
	}
	data, err := t.doRequest(ctx, &compatRestReq{
		method: "POST",
		path:   t.paths.SendMessage(),
		params: params,
		body:   body,
	})
	if err != nil {
		return nil, err
	}
	return pbjson.FromProtoSendMessageResponse(data)
}

// SendStreamingMessage implements [a2aclient.Transport].
func (t *restCompatTransport) SendStreamingMessage(ctx context.Context, params a2aclient.ServiceParams, req *a2a.SendMessageRequest) iter.Seq2[a2a.Event, error] {
	body, err := pbjson.ToProtoSendMessageRequest(req)
	if err != nil {
		return errorStream(fmt.Errorf("failed to marshal SendMessageRequest: %w", err))
	}
	return t.doStreamingRequest(ctx, &compatRestReq{
		method: "POST",
		path:   t.paths.StreamMessage(),
		params: params,
		body:   body,
	})
}

// GetTask implements [a2aclient.Transport].
func (t *restCompatTransport) GetTask(ctx context.Context, params a2aclient.ServiceParams, req *a2a.GetTaskRequest) (*a2a.Task, error) {
	q := url.Values{}
	if req.HistoryLength != nil {
		q.Set("historyLength", strconv.Itoa(*req.HistoryLength))
	}
	data, err := t.doRequest(ctx, &compatRestReq{
		method: "GET",
		path:   t.paths.GetTask(string(req.ID)),
		query:  q,
		params: params,
	})
	if err != nil {
		return nil, err
	}
	return pbjson.FromProtoTask(data)
}

// ListTasks implements [a2aclient.Transport].
func (t *restCompatTransport) ListTasks(ctx context.Context, params a2aclient.ServiceParams, req *a2a.ListTasksRequest) (*a2a.ListTasksResponse, error) {
	q := url.Values{}
	if req.ContextID != "" {
		q.Set("contextId", req.ContextID)
	}
	if req.Status != "" {
		q.Set("status", string(pbjson.EncodeTaskState(req.Status)))
	}
	if req.PageSize != 0 {
		q.Set("pageSize", strconv.Itoa(req.PageSize))
	}
	if req.PageToken != "" {
		q.Set("pageToken", req.PageToken)
	}
	if req.HistoryLength != nil {
		q.Set("historyLength", strconv.Itoa(*req.HistoryLength))
	}
	if req.StatusTimestampAfter != nil {
		q.Set("lastUpdatedAfter", req.StatusTimestampAfter.Format(time.RFC3339))
	}
	if req.IncludeArtifacts {
		q.Set("includeArtifacts", "true")
	}
	data, err := t.doRequest(ctx, &compatRestReq{
		method: "GET",
		path:   t.paths.ListTasks(),
		query:  q,
		params: params,
	})
	if err != nil {
		return nil, err
	}
	return pbjson.FromProtoListTasksResponse(data)
}

// CancelTask implements [a2aclient.Transport].
func (t *restCompatTransport) CancelTask(ctx context.Context, params a2aclient.ServiceParams, req *a2a.CancelTaskRequest) (*a2a.Task, error) {
	data, err := t.doRequest(ctx, &compatRestReq{
		method: "POST",
		path:   t.paths.CancelTask(string(req.ID)),
		params: params,
	})
	if err != nil {
		return nil, err
	}
	return pbjson.FromProtoTask(data)
}

// SubscribeToTask implements [a2aclient.Transport].
func (t *restCompatTransport) SubscribeToTask(ctx context.Context, params a2aclient.ServiceParams, req *a2a.SubscribeToTaskRequest) iter.Seq2[a2a.Event, error] {
	return t.doStreamingRequest(ctx, &compatRestReq{
		method: "GET",
		path:   t.paths.SubscribeTask(string(req.ID)),
		params: params,
	})
}

// GetTaskPushConfig implements [a2aclient.Transport].
func (t *restCompatTransport) GetTaskPushConfig(ctx context.Context, params a2aclient.ServiceParams, req *a2a.GetTaskPushConfigRequest) (*a2a.PushConfig, error) {
	data, err := t.doRequest(ctx, &compatRestReq{
		method: "GET",
		path:   t.paths.GetPushConfig(string(req.TaskID), req.ID),
		params: params,
	})
	if err != nil {
		return nil, err
	}
	return pbjson.FromProtoPushConfigResponse(data)
}

// ListTaskPushConfigs implements [a2aclient.Transport].
func (t *restCompatTransport) ListTaskPushConfigs(ctx context.Context, params a2aclient.ServiceParams, req *a2a.ListTaskPushConfigRequest) ([]*a2a.PushConfig, error) {
	q := url.Values{}
	if req.PageSize != 0 {
		q.Set("pageSize", strconv.Itoa(req.PageSize))
	}
	if req.PageToken != "" {
		q.Set("pageToken", req.PageToken)
	}
	data, err := t.doRequest(ctx, &compatRestReq{
		method: "GET",
		path:   t.paths.ListPushConfigs(string(req.TaskID)),
		query:  q,
		params: params,
	})
	if err != nil {
		return nil, err
	}
	return pbjson.FromProtoListPushConfigsResponse(data, req.TaskID)
}

// CreateTaskPushConfig implements [a2aclient.Transport].
func (t *restCompatTransport) CreateTaskPushConfig(ctx context.Context, params a2aclient.ServiceParams, req *a2a.PushConfig) (*a2a.PushConfig, error) {
	body, err := pbjson.ToProtoCreatePushConfigRequest(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal CreateTaskPushNotificationConfigRequest: %w", err)
	}
	data, err := t.doRequest(ctx, &compatRestReq{
		method: "POST",
		path:   t.paths.CreatePushConfig(string(req.TaskID)),
		params: params,
		body:   body,
	})
	if err != nil {
		return nil, err
	}
	return pbjson.FromProtoPushConfigResponse(data)
}

// DeleteTaskPushConfig implements [a2aclient.Transport].
func (t *restCompatTransport) DeleteTaskPushConfig(ctx context.Context, params a2aclient.ServiceParams, req *a2a.DeleteTaskPushConfigRequest) error {
	_, err := t.doRequest(ctx, &compatRestReq{
		method: "DELETE",
		path:   t.paths.DeletePushConfig(string(req.TaskID), req.ID),
		params: params,
	})
	return err
}

// GetExtendedAgentCard implements [a2aclient.Transport].
func (t *restCompatTransport) GetExtendedAgentCard(ctx context.Context, params a2aclient.ServiceParams, req *a2a.GetExtendedAgentCardRequest) (*a2a.AgentCard, error) {
	data, err := t.doRequest(ctx, &compatRestReq{
		method: "GET",
		path:   RESTPathPrefix + "/card",
		params: params,
	})
	if err != nil {
		return nil, err
	}
	parser := NewAgentCardParser()
	card, err := parser(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse agent card: %w", err)
	}
	return card, nil
}

// Destroy implements [a2aclient.Transport].
func (t *restCompatTransport) Destroy() error {
	return nil
}

func errorStream(err error) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		yield(nil, err)
	}
}

// parseErrorBytes attempts to parse raw JSON bytes as a google.rpc.Status error.
// Returns the corresponding A2A error if the bytes contain an "error" key, nil otherwise.
// If the "error" key is present but malformed, it returns [a2a.ErrInternalError].
func parseErrorBytes(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	rawErr, hasError := raw["error"]
	if !hasError || bytes.Equal(rawErr, []byte("null")) {
		return nil
	}
	var body rest.ErrorBodyJSON
	if err := json.Unmarshal(rawErr, &body); err != nil {
		return a2a.ErrInternalError
	}
	return rest.ConvertErrorBody(&body)
}
