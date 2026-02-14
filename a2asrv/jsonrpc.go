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

package a2asrv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/internal/jsonrpc"
	"github.com/a2aproject/a2a-go/internal/sse"
	"github.com/a2aproject/a2a-go/log"
)

// jsonrpcRequest represents a JSON-RPC 2.0 request.
type jsonrpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	// ID can be a number, a string or nil.
	ID any `json:"id"`
}

// jsonrpcResponse represents a JSON-RPC 2.0 response.
type jsonrpcResponse struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      any            `json:"id"`
	Result  any            `json:"result,omitempty"`
	Error   *jsonrpc.Error `json:"error,omitempty"`
}

type jsonrpcHandler struct {
	handler           RequestHandler
	keepAliveInterval time.Duration
	panicHandler      func(r any) error
}

// JSONRPCHandlerOption is a functional option for configuring the JSONRPC handler.
type JSONRPCHandlerOption func(*jsonrpcHandler)

// WithKeepAlive enables SSE keep-alive messages at the specified interval.
// Keep-alive messages prevent API gateways from dropping idle connections.
// If interval is 0 or negative, keep-alive is disabled (default behavior).
func WithKeepAlive(interval time.Duration) JSONRPCHandlerOption {
	return func(h *jsonrpcHandler) {
		h.keepAliveInterval = interval
	}
}

// WithPanicHandler sets a custom panic handler for the JSONRPC handler.
// This gives the ability to recovery from panic by returning an error to the client.
func WithPanicHandler(handler func(r any) error) JSONRPCHandlerOption {
	return func(h *jsonrpcHandler) {
		h.panicHandler = handler
	}
}

// NewJSONRPCHandler creates an [http.Handler] implementation for serving A2A-protocol over JSONRPC.
func NewJSONRPCHandler(handler RequestHandler, options ...JSONRPCHandlerOption) http.Handler {
	h := &jsonrpcHandler{handler: handler}
	for _, option := range options {
		option(h)
	}
	return h
}

func (h *jsonrpcHandler) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	ctx, _ = WithCallContext(ctx, NewRequestMeta(req.Header))

	if req.Method != "POST" {
		h.writeJSONRPCError(ctx, rw, a2a.ErrInvalidRequest, nil)
		return
	}

	defer func() {
		if err := req.Body.Close(); err != nil {
			log.Error(ctx, "failed to close request body", err)
		}
	}()

	var payload jsonrpcRequest
	if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
		h.writeJSONRPCError(ctx, rw, handleUnmarshalError(err), nil)
		return
	}

	if !isValidID(payload.ID) {
		h.writeJSONRPCError(ctx, rw, a2a.ErrInvalidRequest, nil)
		return
	}

	if payload.JSONRPC != jsonrpc.Version {
		h.writeJSONRPCError(ctx, rw, a2a.ErrInvalidRequest, payload.ID)
		return
	}

	if payload.Method == jsonrpc.MethodTasksResubscribe || payload.Method == jsonrpc.MethodMessageStream {
		h.handleStreamingRequest(ctx, rw, &payload)
	} else {
		h.handleRequest(ctx, rw, &payload)
	}
}

func isValidID(id any) bool {
	if id == nil {
		return true
	}
	switch id.(type) {
	case string, float64:
		return true
	default:
		return false
	}
}

func (h *jsonrpcHandler) handleRequest(ctx context.Context, rw http.ResponseWriter, req *jsonrpcRequest) {
	defer func() {
		if r := recover(); r != nil {
			if h.panicHandler == nil {
				panic(r)
			}
			err := h.panicHandler(r)
			if err != nil {
				h.writeJSONRPCError(ctx, rw, err, req.ID)
				return
			}
		}
	}()

	var result any
	var err error
	switch req.Method {
	case jsonrpc.MethodTasksGet:
		result, err = h.onGetTask(ctx, req.Params)
	case jsonrpc.MethodTasksList:
		result, err = h.onListTasks(ctx, req.Params)
	case jsonrpc.MethodMessageSend:
		result, err = h.onSendMessage(ctx, req.Params)
	case jsonrpc.MethodTasksCancel:
		result, err = h.onCancelTask(ctx, req.Params)
	case jsonrpc.MethodPushConfigGet:
		result, err = h.onGetTaskPushConfig(ctx, req.Params)
	case jsonrpc.MethodPushConfigList:
		result, err = h.onListTaskPushConfig(ctx, req.Params)
	case jsonrpc.MethodPushConfigSet:
		result, err = h.onSetTaskPushConfig(ctx, req.Params)
	case jsonrpc.MethodPushConfigDelete:
		err = h.onDeleteTaskPushConfig(ctx, req.Params)
	case jsonrpc.MethodGetExtendedAgentCard:
		result, err = h.onGetAgentCard(ctx)
	case "":
		err = a2a.ErrInvalidRequest
	default:
		err = a2a.ErrMethodNotFound
	}

	if err != nil {
		h.writeJSONRPCError(ctx, rw, err, req.ID)
		return
	}

	if result != nil {
		resp := jsonrpcResponse{JSONRPC: jsonrpc.Version, ID: req.ID, Result: result}
		if err := json.NewEncoder(rw).Encode(resp); err != nil {
			log.Error(ctx, "failed to encode response", err)
		}
	}
}

func (h *jsonrpcHandler) handleStreamingRequest(ctx context.Context, rw http.ResponseWriter, req *jsonrpcRequest) {
	sseWriter, err := sse.NewWriter(rw)
	if err != nil {
		h.writeJSONRPCError(ctx, rw, err, req.ID)
		return
	}

	sseWriter.WriteHeaders()

	sseChan, panicChan := make(chan []byte), make(chan error)
	requestCtx, cancelReqCtx := context.WithCancel(ctx)
	defer cancelReqCtx()
	go func() {
		defer func() {
			if r := recover(); r != nil {
				panicChan <- fmt.Errorf("%v\n%s", r, debug.Stack())
			} else {
				close(sseChan)
			}
		}()

		var events iter.Seq2[a2a.Event, error]
		switch req.Method {
		case jsonrpc.MethodTasksResubscribe:
			events = h.onResubscribeToTask(requestCtx, req.Params)
		case jsonrpc.MethodMessageStream:
			events = h.onSendMessageStream(requestCtx, req.Params)
		default:
			events = func(yield func(a2a.Event, error) bool) { yield(nil, a2a.ErrMethodNotFound) }
		}
		eventSeqToSSEDataStream(requestCtx, req, sseChan, events)
	}()

	// Set up keep-alive ticker if enabled (interval > 0)
	var keepAliveTicker *time.Ticker
	var keepAliveChan <-chan time.Time
	if h.keepAliveInterval > 0 {
		keepAliveTicker = time.NewTicker(h.keepAliveInterval)
		defer keepAliveTicker.Stop()
		keepAliveChan = keepAliveTicker.C
	}

	for {
		select {
		case <-ctx.Done():
			return
		case err := <-panicChan:
			if h.panicHandler == nil {
				panic(err)
			}
			data, ok := marshalJSONRPCError(req, h.panicHandler(err))
			if !ok {
				log.Error(ctx, "failed to marshal error response", err)
				return
			}
			if err := sseWriter.WriteData(ctx, data); err != nil {
				log.Error(ctx, "failed to write an event", err)
				return
			}
		case <-keepAliveChan:
			if err := sseWriter.WriteKeepAlive(ctx); err != nil {
				log.Error(ctx, "failed to write keep-alive", err)
				return
			}
		case data, ok := <-sseChan:
			if !ok {
				return
			}
			if err := sseWriter.WriteData(ctx, data); err != nil {
				log.Error(ctx, "failed to write an event", err)
				return
			}
		}
	}
}

func eventSeqToSSEDataStream(ctx context.Context, req *jsonrpcRequest, sseChan chan []byte, events iter.Seq2[a2a.Event, error]) {
	handleError := func(err error) {
		bytes, ok := marshalJSONRPCError(req, err)
		if !ok {
			log.Error(ctx, "failed to marshal error response", err)
			return
		}
		select {
		case <-ctx.Done():
			return
		case sseChan <- bytes:
		}
	}

	for event, err := range events {
		if err != nil {
			handleError(err)
			return
		}

		resp := jsonrpcResponse{JSONRPC: jsonrpc.Version, ID: req.ID, Result: event}
		bytes, err := json.Marshal(resp)
		if err != nil {
			handleError(err)
			return
		}

		select {
		case <-ctx.Done():
			return
		case sseChan <- bytes:
		}
	}
}

func (h *jsonrpcHandler) onGetTask(ctx context.Context, raw json.RawMessage) (*a2a.Task, error) {
	var query a2a.TaskQueryParams
	if err := json.Unmarshal(raw, &query); err != nil {
		return nil, handleUnmarshalError(err)
	}
	return h.handler.OnGetTask(ctx, &query)
}

func (h *jsonrpcHandler) onListTasks(ctx context.Context, raw json.RawMessage) (*a2a.ListTasksResponse, error) {
	var req a2a.ListTasksRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, handleUnmarshalError(err)
	}
	return h.handler.OnListTasks(ctx, &req)
}

func (h *jsonrpcHandler) onCancelTask(ctx context.Context, raw json.RawMessage) (*a2a.Task, error) {
	var id a2a.TaskIDParams
	if err := json.Unmarshal(raw, &id); err != nil {
		return nil, handleUnmarshalError(err)
	}
	return h.handler.OnCancelTask(ctx, &id)
}

func (h *jsonrpcHandler) onSendMessage(ctx context.Context, raw json.RawMessage) (a2a.SendMessageResult, error) {
	var message a2a.MessageSendParams
	if err := json.Unmarshal(raw, &message); err != nil {
		return nil, handleUnmarshalError(err)
	}
	return h.handler.OnSendMessage(ctx, &message)
}

func (h *jsonrpcHandler) onResubscribeToTask(ctx context.Context, raw json.RawMessage) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		var id a2a.TaskIDParams
		if err := json.Unmarshal(raw, &id); err != nil {
			yield(nil, handleUnmarshalError(err))
			return
		}
		for event, err := range h.handler.OnResubscribeToTask(ctx, &id) {
			if !yield(event, err) {
				return
			}
		}
	}
}

func (h *jsonrpcHandler) onSendMessageStream(ctx context.Context, raw json.RawMessage) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		var message a2a.MessageSendParams
		if err := json.Unmarshal(raw, &message); err != nil {
			yield(nil, handleUnmarshalError(err))
			return
		}
		for event, err := range h.handler.OnSendMessageStream(ctx, &message) {
			if !yield(event, err) {
				return
			}
		}
	}

}

func (h *jsonrpcHandler) onGetTaskPushConfig(ctx context.Context, raw json.RawMessage) (*a2a.TaskPushConfig, error) {
	var params a2a.GetTaskPushConfigParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, handleUnmarshalError(err)
	}
	return h.handler.OnGetTaskPushConfig(ctx, &params)
}

func (h *jsonrpcHandler) onListTaskPushConfig(ctx context.Context, raw json.RawMessage) ([]*a2a.TaskPushConfig, error) {
	var params a2a.ListTaskPushConfigParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, handleUnmarshalError(err)
	}
	return h.handler.OnListTaskPushConfig(ctx, &params)
}

func (h *jsonrpcHandler) onSetTaskPushConfig(ctx context.Context, raw json.RawMessage) (*a2a.TaskPushConfig, error) {
	var params a2a.TaskPushConfig
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, handleUnmarshalError(err)
	}
	return h.handler.OnSetTaskPushConfig(ctx, &params)
}

func (h *jsonrpcHandler) onDeleteTaskPushConfig(ctx context.Context, raw json.RawMessage) error {
	var params a2a.DeleteTaskPushConfigParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return handleUnmarshalError(err)
	}
	return h.handler.OnDeleteTaskPushConfig(ctx, &params)
}

func (h *jsonrpcHandler) onGetAgentCard(ctx context.Context) (*a2a.AgentCard, error) {
	return h.handler.OnGetExtendedAgentCard(ctx)
}

func marshalJSONRPCError(req *jsonrpcRequest, err error) ([]byte, bool) {
	jsonrpcErr := jsonrpc.ToJSONRPCError(err)
	resp := jsonrpcResponse{JSONRPC: jsonrpc.Version, ID: req.ID, Error: jsonrpcErr}
	bytes, err := json.Marshal(resp)
	if err != nil {
		return nil, false
	}
	return bytes, true
}

func handleUnmarshalError(err error) error {
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		return fmt.Errorf("%w: %w", a2a.ErrInvalidParams, err)
	}
	return fmt.Errorf("%w: %w", a2a.ErrParseError, err)
}

func (h *jsonrpcHandler) writeJSONRPCError(ctx context.Context, rw http.ResponseWriter, err error, reqID any) {
	jsonrpcErr := jsonrpc.ToJSONRPCError(err)
	resp := jsonrpcResponse{JSONRPC: jsonrpc.Version, Error: jsonrpcErr, ID: reqID}
	if err := json.NewEncoder(rw).Encode(resp); err != nil {
		log.Error(ctx, "failed to send error response", err)
	}
}
