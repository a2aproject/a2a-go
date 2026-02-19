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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/a2aproject/a2a-go/internal/jsonrpc"
	"github.com/a2aproject/a2a-go/internal/sse"
	"github.com/a2aproject/a2a-go/log"
)

type JSONRPCHandlerConfig struct {
	PanicHandler      func(r any) error
	KeepAliveInterval time.Duration
}

type jsonrpcHandler struct {
	handler           a2asrv.RequestHandler
	keepAliveInterval time.Duration
	panicHandler      func(r any) error
}

// NewJSONRPCHandler creates an [http.Handler] implementation for serving A2A-protocol over JSONRPC v0.3.
func NewJSONRPCHandler(handler a2asrv.RequestHandler, config JSONRPCHandlerConfig) http.Handler {
	h := &jsonrpcHandler{
		handler:           handler,
		keepAliveInterval: config.KeepAliveInterval,
		panicHandler:      config.PanicHandler,
	}
	return h
}

func (h *jsonrpcHandler) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	ctx, _ = a2asrv.WithCallContext(ctx, a2asrv.NewServiceParams(req.Header))

	if req.Method != "POST" {
		h.writeJSONRPCError(ctx, rw, a2a.ErrInvalidRequest, nil)
		return
	}

	defer func() {
		if err := req.Body.Close(); err != nil {
			log.Error(ctx, "failed to close request body", err)
		}
	}()

	var payload jsonrpc.ServerRequest
	if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
		h.writeJSONRPCError(ctx, rw, handleUnmarshalError(err), nil)
		return
	}

	if !jsonrpc.IsValidID(payload.ID) {
		h.writeJSONRPCError(ctx, rw, a2a.ErrInvalidRequest, nil)
		return
	}

	if payload.JSONRPC != jsonrpc.Version {
		h.writeJSONRPCError(ctx, rw, a2a.ErrInvalidRequest, payload.ID)
		return
	}

	if payload.Method == methodTasksResubscribe || payload.Method == methodMessageStream {
		h.handleStreamingRequest(ctx, rw, &payload)
	} else {
		h.handleRequest(ctx, rw, &payload)
	}
}

func (h *jsonrpcHandler) handleRequest(ctx context.Context, rw http.ResponseWriter, req *jsonrpc.ServerRequest) {
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
	case methodTasksGet:
		result, err = h.onGetTask(ctx, req.Params)
	case methodMessageSend:
		result, err = h.onSendMessage(ctx, req.Params)
	case methodTasksCancel:
		result, err = h.onCancelTask(ctx, req.Params)
	case methodPushConfigGet:
		result, err = h.onGetTaskPushConfig(ctx, req.Params)
	case methodPushConfigList:
		result, err = h.onListTaskPushConfig(ctx, req.Params)
	case methodPushConfigSet:
		result, err = h.onSetTaskPushConfig(ctx, req.Params)
	case methodPushConfigDelete:
		err = h.onDeleteTaskPushConfig(ctx, req.Params)
	case methodGetExtendedAgentCard:
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
		resp := jsonrpc.ServerResponse{JSONRPC: jsonrpc.Version, ID: req.ID, Result: result}
		if err := json.NewEncoder(rw).Encode(resp); err != nil {
			log.Error(ctx, "failed to encode response", err)
		}
	}
}

func (h *jsonrpcHandler) handleStreamingRequest(ctx context.Context, rw http.ResponseWriter, req *jsonrpc.ServerRequest) {
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

func eventSeqToSSEDataStream(ctx context.Context, req *jsonrpc.ServerRequest, sseChan chan []byte, events iter.Seq2[a2a.Event, error]) {
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

		compatEvent, err := toCompatEvent(event)
		if err != nil {
			handleError(err)
			return
		}

		resp := jsonrpc.ServerResponse{JSONRPC: jsonrpc.Version, ID: req.ID, Result: compatEvent}
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

func (h *jsonrpcHandler) onGetTask(ctx context.Context, raw json.RawMessage) (*task, error) {
	var query taskQueryParams
	if err := json.Unmarshal(raw, &query); err != nil {
		return nil, handleUnmarshalError(err)
	}
	compatTask, err := h.handler.GetTask(ctx, toCoreGetTaskRequest(&query))
	if err != nil {
		return nil, err
	}
	return toCompatTask(compatTask), nil
}

func (h *jsonrpcHandler) onCancelTask(ctx context.Context, raw json.RawMessage) (*task, error) {
	var id taskIDParams
	if err := json.Unmarshal(raw, &id); err != nil {
		return nil, handleUnmarshalError(err)
	}
	compatTask, err := h.handler.CancelTask(ctx, toCoreCancelTaskRequest(&id))
	if err != nil {
		return nil, err
	}
	return toCompatTask(compatTask), nil
}

func (h *jsonrpcHandler) onSendMessage(ctx context.Context, raw json.RawMessage) (sendMessageResult, error) {
	var message messageSendParams
	if err := json.Unmarshal(raw, &message); err != nil {
		return nil, handleUnmarshalError(err)
	}
	compatResult, err := h.handler.SendMessage(ctx, toCoreSendMessageRequest(&message))
	if err != nil {
		return nil, err
	}
	compEvent, err := toCompatEvent(compatResult)
	if err != nil || compEvent == nil {
		return nil, err
	}
	return compEvent.(sendMessageResult), nil
}

func (h *jsonrpcHandler) onResubscribeToTask(ctx context.Context, raw json.RawMessage) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		var id taskIDParams
		if err := json.Unmarshal(raw, &id); err != nil {
			yield(nil, handleUnmarshalError(err))
			return
		}
		for event, err := range h.handler.SubscribeToTask(ctx, toCoreSubscribeToTaskRequest(&id)) {
			if !yield(event, err) {
				return
			}
		}
	}
}

func (h *jsonrpcHandler) onSendMessageStream(ctx context.Context, raw json.RawMessage) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		var message messageSendParams
		if err := json.Unmarshal(raw, &message); err != nil {
			yield(nil, handleUnmarshalError(err))
			return
		}
		for event, err := range h.handler.SendStreamingMessage(ctx, toCoreSendMessageRequest(&message)) {
			if !yield(event, err) {
				return
			}
		}
	}

}

func (h *jsonrpcHandler) onGetTaskPushConfig(ctx context.Context, raw json.RawMessage) (*taskPushConfig, error) {
	var params getTaskPushConfigParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, handleUnmarshalError(err)
	}
	config, err := h.handler.GetTaskPushConfig(ctx, toCoreGetTaskPushConfigRequest(&params))
	if err != nil {
		return nil, err
	}
	return toCompatTaskPushConfig(config)
}

func (h *jsonrpcHandler) onListTaskPushConfig(ctx context.Context, raw json.RawMessage) ([]*taskPushConfig, error) {
	var params listTaskPushConfigParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, handleUnmarshalError(err)
	}
	configs, err := h.handler.ListTaskPushConfig(ctx, toCoreListTaskPushConfigRequest(&params))
	if err != nil {
		return nil, err
	}
	return toCompatTaskPushConfigs(configs)
}

func (h *jsonrpcHandler) onSetTaskPushConfig(ctx context.Context, raw json.RawMessage) (*taskPushConfig, error) {
	var params taskPushConfig
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, handleUnmarshalError(err)
	}
	config, err := h.handler.CreateTaskPushConfig(ctx, toCoreCreateTaskPushConfigRequest(&params))
	if err != nil {
		return nil, err
	}
	return toCompatTaskPushConfig(config)
}

func (h *jsonrpcHandler) onDeleteTaskPushConfig(ctx context.Context, raw json.RawMessage) error {
	var params deleteTaskPushConfigParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return handleUnmarshalError(err)
	}
	return h.handler.DeleteTaskPushConfig(ctx, toCoreDeleteTaskPushConfigRequest(&params))
}

func (h *jsonrpcHandler) onGetAgentCard(ctx context.Context) (*agentCard, error) {
	card, err := h.handler.GetExtendedAgentCard(ctx)
	if err != nil {
		return nil, err
	}
	compatCard, err := toCompatCard(card)
	if err != nil {
		return nil, err
	}
	return compatCard, nil
}

func marshalJSONRPCError(req *jsonrpc.ServerRequest, err error) ([]byte, bool) {
	jsonrpcErr := jsonrpc.ToJSONRPCError(err)
	resp := jsonrpc.ServerResponse{JSONRPC: jsonrpc.Version, ID: req.ID, Error: jsonrpcErr}
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
	resp := jsonrpc.ServerResponse{JSONRPC: jsonrpc.Version, Error: jsonrpcErr, ID: reqID}
	if err := json.NewEncoder(rw).Encode(resp); err != nil {
		log.Error(ctx, "failed to send error response", err)
	}
}
