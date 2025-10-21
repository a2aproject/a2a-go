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
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/google/uuid"
)

// JSON-RPC 2.0 protocol constants
const (
	jsonrpcVersion = "2.0"

	// HTTP headers
	contentTypeJSON   = "application/json"
	acceptEventStream = "text/event-stream"

	// JSON-RPC method names per A2A spec §7
	methodMessageSend              = "message/send"
	methodMessageStream            = "message/stream"
	methodTasksGet                 = "tasks/get"
	methodTasksCancel              = "tasks/cancel"
	methodTasksResubscribe         = "tasks/resubscribe"
	methodPushConfigGet            = "tasks/pushNotificationConfig/get"
	methodPushConfigSet            = "tasks/pushNotificationConfig/set"
	methodPushConfigList           = "tasks/pushNotificationConfig/list"
	methodPushConfigDelete         = "tasks/pushNotificationConfig/delete"
	methodGetAuthenticatedExtended = "agent/getAuthenticatedExtendedCard"

	// SSE data prefix
	sseDataPrefix = "data: "
)

// JSONRPCOption configures optional parameters for the JSONRPC transport.
// Options are applied during NewJSONRPCTransport initialization.
type JSONRPCOption func(*jsonrpcTransport)

// WithHTTPClient sets a custom HTTP client for the JSONRPC transport.
// By default, a client with 5-second timeout is used (matching the Python SDK default).
// For production deployments, provide a client with appropriate timeout, retry policy,
// and connection pooling configured for your requirements.
//
// Example:
//
//	client := &http.Client{
//	    Timeout: 60 * time.Second,
//	    Transport: &http.Transport{
//	        MaxIdleConns:        100,
//	        MaxIdleConnsPerHost: 10,
//	        IdleConnTimeout:     90 * time.Second,
//	    },
//	}
//	transport := NewJSONRPCTransport(url, card, WithHTTPClient(client))
func WithHTTPClient(client *http.Client) JSONRPCOption {
	return func(t *jsonrpcTransport) {
		t.httpClient = client
	}
}

// WithJSONRPCTransport returns a Client factory option that enables JSON-RPC transport support.
// When applied, the client will use JSON-RPC 2.0 over HTTP for all A2A protocol communication
// as defined in the A2A specification §7.
func WithJSONRPCTransport(opts ...JSONRPCOption) FactoryOption {
	return WithTransport(
		a2a.TransportProtocolJSONRPC,
		TransportFactoryFn(func(ctx context.Context, url string, card *a2a.AgentCard) (Transport, error) {
			return NewJSONRPCTransport(url, card, opts...), nil
		}),
	)
}

// NewJSONRPCTransport creates a new JSON-RPC transport for A2A protocol communication.
// By default, an HTTP client with 5-second timeout is used (matching Python SDK behavior).
// For custom timeout, retry logic, or connection pooling, provide a configured client via WithHTTPClient.
func NewJSONRPCTransport(url string, card *a2a.AgentCard, opts ...JSONRPCOption) Transport {
	t := &jsonrpcTransport{
		url:       url,
		agentCard: card,
		httpClient: &http.Client{
			Timeout: 5 * time.Second, // Match Python SDK httpx.AsyncClient default
		},
	}

	for _, opt := range opts {
		opt(t)
	}

	return t
}

// jsonrpcTransport implements Transport using JSON-RPC 2.0 over HTTP.
type jsonrpcTransport struct {
	url        string
	httpClient *http.Client
	agentCard  *a2a.AgentCard
	cardMu     sync.RWMutex // protects agentCard
}

// jsonrpcRequest represents a JSON-RPC 2.0 request.
type jsonrpcRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
	ID      string      `json:"id"`
}

// jsonrpcResponse represents a JSON-RPC 2.0 response.
type jsonrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonrpcError   `json:"error,omitempty"`
}

// jsonrpcError represents a JSON-RPC 2.0 error object.
type jsonrpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Error implements the error interface for jsonrpcError.
func (e *jsonrpcError) Error() string {
	if len(e.Data) > 0 {
		return fmt.Sprintf("jsonrpc error %d: %s (data: %s)", e.Code, e.Message, string(e.Data))
	}
	return fmt.Sprintf("jsonrpc error %d: %s", e.Code, e.Message)
}

// sendRequest sends a non-streaming JSON-RPC request and returns the response.
func (t *jsonrpcTransport) sendRequest(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	req := jsonrpcRequest{
		JSONRPC: jsonrpcVersion,
		Method:  method,
		Params:  params,
		ID:      uuid.New().String(),
	}

	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", t.url, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	httpReq.Header.Set("Content-Type", contentTypeJSON)

	httpResp, err := t.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send HTTP request: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected HTTP status code: %d", httpResp.StatusCode)
	}

	var resp jsonrpcResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if resp.Error != nil {
		return nil, resp.Error
	}

	return resp.Result, nil
}

// sendStreamingRequest sends a streaming JSON-RPC request and returns an SSE stream.
func (t *jsonrpcTransport) sendStreamingRequest(ctx context.Context, method string, params interface{}) (io.ReadCloser, error) {
	req := jsonrpcRequest{
		JSONRPC: jsonrpcVersion,
		Method:  method,
		Params:  params,
		ID:      uuid.New().String(),
	}

	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", t.url, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	httpReq.Header.Set("Content-Type", contentTypeJSON)
	httpReq.Header.Set("Accept", acceptEventStream)

	httpResp, err := t.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send HTTP request: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		httpResp.Body.Close()
		return nil, fmt.Errorf("unexpected HTTP status code: %d", httpResp.StatusCode)
	}

	return httpResp.Body, nil
}

// parseSSEStream parses Server-Sent Events and yields JSON-RPC responses.
func parseSSEStream(body io.ReadCloser) iter.Seq2[json.RawMessage, error] {
	return func(yield func(json.RawMessage, error) bool) {
		defer body.Close()

		scanner := bufio.NewScanner(body)

		for scanner.Scan() {
			line := scanner.Text()

			// SSE data lines start with "data: "
			if strings.HasPrefix(line, sseDataPrefix) {
				data := strings.TrimPrefix(line, sseDataPrefix)

				var resp jsonrpcResponse
				if err := json.Unmarshal([]byte(data), &resp); err != nil {
					yield(nil, fmt.Errorf("failed to parse SSE data: %w", err))
					return
				}

				if resp.Error != nil {
					yield(nil, resp.Error)
					return
				}

				if !yield(resp.Result, nil) {
					return
				}
			}
			// Ignore empty lines, comments, and other SSE event types
		}

		if err := scanner.Err(); err != nil {
			yield(nil, fmt.Errorf("SSE stream error: %w", err))
		}
	}
}

// SendMessage sends a non-streaming message to the agent.
func (t *jsonrpcTransport) SendMessage(ctx context.Context, message *a2a.MessageSendParams) (a2a.SendMessageResult, error) {
	result, err := t.sendRequest(ctx, methodMessageSend, message)
	if err != nil {
		return nil, err
	}

	// unmarshalEvent can distinguish between Task and Message types
	event, err := unmarshalEvent(result)
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

// streamRequestToEvents handles SSE streaming for JSON-RPC methods.
// It converts the SSE stream into a sequence of A2A events.
func (t *jsonrpcTransport) streamRequestToEvents(ctx context.Context, method string, params interface{}) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		body, err := t.sendStreamingRequest(ctx, method, params)
		if err != nil {
			yield(nil, err)
			return
		}
		// parseSSEStream takes ownership of body and will close it

		for result, err := range parseSSEStream(body) {
			if err != nil {
				yield(nil, err)
				return
			}

			event, err := unmarshalEvent(result)
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
func (t *jsonrpcTransport) SendStreamingMessage(ctx context.Context, message *a2a.MessageSendParams) iter.Seq2[a2a.Event, error] {
	return t.streamRequestToEvents(ctx, methodMessageStream, message)
}

// GetTask retrieves the current state of a task.
func (t *jsonrpcTransport) GetTask(ctx context.Context, query *a2a.TaskQueryParams) (*a2a.Task, error) {
	result, err := t.sendRequest(ctx, methodTasksGet, query)
	if err != nil {
		return nil, err
	}

	var task a2a.Task
	if err := json.Unmarshal(result, &task); err != nil {
		return nil, fmt.Errorf("failed to unmarshal task: %w", err)
	}

	return &task, nil
}

// CancelTask requests cancellation of a task.
func (t *jsonrpcTransport) CancelTask(ctx context.Context, id *a2a.TaskIDParams) (*a2a.Task, error) {
	result, err := t.sendRequest(ctx, methodTasksCancel, id)
	if err != nil {
		return nil, err
	}

	var task a2a.Task
	if err := json.Unmarshal(result, &task); err != nil {
		return nil, fmt.Errorf("failed to unmarshal task: %w", err)
	}

	return &task, nil
}

// ResubscribeToTask reconnects to an SSE stream for an ongoing task.
func (t *jsonrpcTransport) ResubscribeToTask(ctx context.Context, id *a2a.TaskIDParams) iter.Seq2[a2a.Event, error] {
	return t.streamRequestToEvents(ctx, methodTasksResubscribe, id)
}

// GetTaskPushConfig retrieves the push notification configuration for a task.
func (t *jsonrpcTransport) GetTaskPushConfig(ctx context.Context, params *a2a.GetTaskPushConfigParams) (*a2a.TaskPushConfig, error) {
	result, err := t.sendRequest(ctx, methodPushConfigGet, params)
	if err != nil {
		return nil, err
	}

	var config a2a.TaskPushConfig
	if err := json.Unmarshal(result, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return &config, nil
}

// ListTaskPushConfig lists push notification configurations.
func (t *jsonrpcTransport) ListTaskPushConfig(ctx context.Context, params *a2a.ListTaskPushConfigParams) ([]*a2a.TaskPushConfig, error) {
	result, err := t.sendRequest(ctx, methodPushConfigList, params)
	if err != nil {
		return nil, err
	}

	var configs []*a2a.TaskPushConfig
	if err := json.Unmarshal(result, &configs); err != nil {
		return nil, fmt.Errorf("failed to unmarshal configs: %w", err)
	}

	return configs, nil
}

// SetTaskPushConfig sets or updates the push notification configuration for a task.
func (t *jsonrpcTransport) SetTaskPushConfig(ctx context.Context, params *a2a.TaskPushConfig) (*a2a.TaskPushConfig, error) {
	result, err := t.sendRequest(ctx, methodPushConfigSet, params)
	if err != nil {
		return nil, err
	}

	var config a2a.TaskPushConfig
	if err := json.Unmarshal(result, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return &config, nil
}

// DeleteTaskPushConfig deletes a push notification configuration.
func (t *jsonrpcTransport) DeleteTaskPushConfig(ctx context.Context, params *a2a.DeleteTaskPushConfigParams) error {
	_, err := t.sendRequest(ctx, methodPushConfigDelete, params)
	return err
}

// GetAgentCard retrieves the agent's card.
// If the card supports authenticated extended cards and we haven't fetched it yet,
// this will call the agent/getAuthenticatedExtendedCard method.
// This method is safe for concurrent access and returns a copy of the cached card
// to prevent data races from concurrent modifications.
func (t *jsonrpcTransport) GetAgentCard(ctx context.Context) (*a2a.AgentCard, error) {
	// Fast path: check if we have a card that doesn't need extension
	t.cardMu.RLock()
	if t.agentCard != nil && !t.agentCard.SupportsAuthenticatedExtendedCard {
		card := *t.agentCard // Return a copy
		t.cardMu.RUnlock()
		return &card, nil
	}
	t.cardMu.RUnlock()

	// Slow path: need to fetch extended card (or no card at all)
	t.cardMu.Lock()
	defer t.cardMu.Unlock()

	// Double-check: another goroutine may have fetched it
	if t.agentCard != nil && !t.agentCard.SupportsAuthenticatedExtendedCard {
		card := *t.agentCard // Return a copy
		return &card, nil
	}

	// No card available
	if t.agentCard == nil {
		return nil, fmt.Errorf("no agent card available")
	}

	// Fetch authenticated extended card
	result, err := t.sendRequest(ctx, methodGetAuthenticatedExtended, nil)
	if err != nil {
		return nil, err
	}

	var extendedCard a2a.AgentCard
	if err := json.Unmarshal(result, &extendedCard); err != nil {
		return nil, fmt.Errorf("failed to unmarshal extended agent card: %w", err)
	}

	// Cache the extended card and return a copy
	t.agentCard = &extendedCard
	card := extendedCard // Return a copy
	return &card, nil
}

// Destroy closes the transport and releases resources.
func (t *jsonrpcTransport) Destroy() error {
	// HTTP client doesn't need explicit cleanup in most cases
	// If a custom client with cleanup is needed, implement via options
	return nil
}

// unmarshalEvent unmarshals a JSON-RPC result into an A2A Event.
func unmarshalEvent(data json.RawMessage) (a2a.Event, error) {
	// Try to determine event type by checking specific field combinations
	var eventMap map[string]interface{}
	if err := json.Unmarshal(data, &eventMap); err != nil {
		return nil, fmt.Errorf("failed to unmarshal event: %w", err)
	}

	// Task: has "id" + "status" with "state" field
	if _, hasID := eventMap["id"]; hasID {
		if statusMap, hasStatus := eventMap["status"].(map[string]interface{}); hasStatus {
			if _, hasState := statusMap["state"]; hasState {
				// This is definitely a Task (has id + status.state)
				var task a2a.Task
				if err := json.Unmarshal(data, &task); err != nil {
					return nil, fmt.Errorf("failed to unmarshal Task event: %w", err)
				}
				return &task, nil
			}
		}
	}

	// Message: has "role" field (messageId or id + role)
	if _, hasRole := eventMap["role"]; hasRole {
		var msg a2a.Message
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, fmt.Errorf("failed to unmarshal Message event: %w", err)
		}
		return &msg, nil
	}

	// TaskStatusUpdateEvent: has "taskId" + "newStatus"
	if _, hasTaskID := eventMap["taskId"]; hasTaskID {
		if newStatusMap, hasNewStatus := eventMap["newStatus"].(map[string]interface{}); hasNewStatus {
			if _, hasState := newStatusMap["state"]; hasState {
				// This is definitely a TaskStatusUpdateEvent
				var statusUpdate a2a.TaskStatusUpdateEvent
				if err := json.Unmarshal(data, &statusUpdate); err != nil {
					return nil, fmt.Errorf("failed to unmarshal TaskStatusUpdateEvent: %w", err)
				}
				return &statusUpdate, nil
			}
		}

		// TaskArtifactUpdateEvent: has "taskId" + "artifact"
		if _, hasArtifact := eventMap["artifact"]; hasArtifact {
			var artifactUpdate a2a.TaskArtifactUpdateEvent
			if err := json.Unmarshal(data, &artifactUpdate); err != nil {
				return nil, fmt.Errorf("failed to unmarshal TaskArtifactUpdateEvent: %w", err)
			}
			return &artifactUpdate, nil
		}
	}

	return nil, fmt.Errorf("unknown event type: %s", string(data))
}
