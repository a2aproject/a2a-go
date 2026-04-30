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
	"fmt"
	"iter"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/internal/jsonrpc"
	"github.com/a2aproject/a2a-go/v2/internal/sse"
	"github.com/a2aproject/a2a-go/v2/log"
)

type jsonrpcHandler struct {
	handler RequestHandler
	cfg     *TransportConfig
}

// NewJSONRPCHandler creates an [http.Handler] which implements JSONRPC A2A protocol binding.
func NewJSONRPCHandler(handler RequestHandler, options ...TransportOption) http.Handler {
	h := &jsonrpcHandler{handler: handler, cfg: &TransportConfig{}}
	for _, option := range options {
		option(h.cfg)
	}
	return h
}

// ServeHTTP implements http.Handler.
func (h *jsonrpcHandler) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	ctx, _ = NewCallContext(ctx, NewServiceParams(req.Header))

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
		h.writeJSONRPCError(ctx, rw, jsonrpc.UnmarshalError(err), nil)
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

	if payload.Method == jsonrpc.MethodTasksResubscribe || payload.Method == jsonrpc.MethodMessageStream {
		h.handleStreamingRequest(ctx, rw, &payload)
	} else {
		h.handleRequest(ctx, rw, &payload)
	}
}

func (h *jsonrpcHandler) handleRequest(ctx context.Context, rw http.ResponseWriter, req *jsonrpc.ServerRequest) {
	defer func() {
		if r := recover(); r != nil {
			if h.cfg.PanicHandler == nil {
				panic(r)
			}
			err := h.cfg.PanicHandler(r)
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
		var res a2a.SendMessageResult
		res, err = h.onSendMessage(ctx, req.Params)
		if err == nil {
			result = a2a.StreamResponse{Event: res}
		}
	case jsonrpc.MethodTasksCancel:
		result, err = h.onCancelTask(ctx, req.Params)
	case jsonrpc.MethodPushConfigGet:
		result, err = h.onGetTaskPushConfig(ctx, req.Params)
	case jsonrpc.MethodPushConfigList:
		result, err = h.onListTaskPushConfigs(ctx, req.Params)
	case jsonrpc.MethodPushConfigSet:
		result, err = h.onSetTaskPushConfig(ctx, req.Params)
	case jsonrpc.MethodPushConfigDelete:
		err = h.onDeleteTaskPushConfig(ctx, req.Params)
	case jsonrpc.MethodGetExtendedAgentCard:
		result, err = h.onGetExtendedAgentCard(ctx, req.Params)
	case "":
		err = a2a.ErrInvalidRequest
	default:
		err = a2a.ErrMethodNotFound
	}

	if err != nil {
		h.writeJSONRPCError(ctx, rw, err, req.ID)
		return
	}

	rw.Header().Set("Content-Type", jsonrpc.ContentJSON)
	resp := jsonrpc.NewResultResponse(req.ID, result)
	if err := json.NewEncoder(rw).Encode(resp); err != nil {
		log.Error(ctx, "failed to encode response", err)
	}
}

func (h *jsonrpcHandler) handleStreamingRequest(ctx context.Context, rw http.ResponseWriter, req *jsonrpc.ServerRequest) {
	sseWriter, err := sse.NewWriter(rw)
	if err != nil {
		h.writeJSONRPCError(ctx, rw, err, req.ID)
		return
	}

	sseWriter.WriteHeaders()

	sseChan, panicChan := make(chan []byte), make(chan error, 1)
	requestCtx, cancelExecCtx := context.WithCancel(ctx)
	defer cancelExecCtx()
	go func() {
		defer func() {
			if r := recover(); r != nil {
				panicChan <- fmt.Errorf("%v\n%s", r, debug.Stack())
			} else {
				// Only close if not panice, otherwise <-sseChan would compete with <-panicChan in select
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
	if h.cfg.KeepAliveInterval > 0 {
		keepAliveTicker = time.NewTicker(h.cfg.KeepAliveInterval)
		defer keepAliveTicker.Stop()
		keepAliveChan = keepAliveTicker.C
	}

	for {
		select {
		case <-ctx.Done():
			return
		case err := <-panicChan:
			if h.cfg.PanicHandler == nil {
				panic(err)
			}
			data, ok := jsonrpc.MarshalErrorResponse(req.ID, h.cfg.PanicHandler(err))
			if !ok {
				log.Error(ctx, "failed to marshal error response", err)
				return
			}
			if err := sseWriter.WriteData(ctx, data); err != nil {
				log.Error(ctx, "failed to write an event", err)
			}
			// Prevent the handler from hanging
			return
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
		bytes, ok := jsonrpc.MarshalErrorResponse(req.ID, err)
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

		resp := jsonrpc.NewResultResponse(req.ID, a2a.StreamResponse{Event: event})
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

func callJSONRPC[Req any, Resp any](ctx context.Context, raw json.RawMessage, call func(context.Context, *Req) (Resp, error)) (Resp, error) {
	var zero Resp
	params, err := jsonrpc.UnmarshalParams[Req](raw)
	if err != nil {
		return zero, err
	}
	return call(ctx, params)
}

func callJSONRPCNoResult[Req any](ctx context.Context, raw json.RawMessage, call func(context.Context, *Req) error) error {
	params, err := jsonrpc.UnmarshalParams[Req](raw)
	if err != nil {
		return err
	}
	return call(ctx, params)
}

func streamJSONRPC[Req any](ctx context.Context, raw json.RawMessage, call func(context.Context, *Req) iter.Seq2[a2a.Event, error]) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		params, err := jsonrpc.UnmarshalParams[Req](raw)
		if err != nil {
			yield(nil, err)
			return
		}
		for event, err := range call(ctx, params) {
			if !yield(event, err) {
				return
			}
		}
	}
}

func (h *jsonrpcHandler) onGetTask(ctx context.Context, raw json.RawMessage) (*a2a.Task, error) {
	return callJSONRPC(ctx, raw, h.handler.GetTask)
}

func (h *jsonrpcHandler) onListTasks(ctx context.Context, raw json.RawMessage) (*a2a.ListTasksResponse, error) {
	return callJSONRPC(ctx, raw, h.handler.ListTasks)
}

func (h *jsonrpcHandler) onCancelTask(ctx context.Context, raw json.RawMessage) (*a2a.Task, error) {
	return callJSONRPC(ctx, raw, h.handler.CancelTask)
}

func (h *jsonrpcHandler) onSendMessage(ctx context.Context, raw json.RawMessage) (a2a.SendMessageResult, error) {
	return callJSONRPC(ctx, raw, h.handler.SendMessage)
}

func (h *jsonrpcHandler) onResubscribeToTask(ctx context.Context, raw json.RawMessage) iter.Seq2[a2a.Event, error] {
	return streamJSONRPC(ctx, raw, h.handler.SubscribeToTask)
}

func (h *jsonrpcHandler) onSendMessageStream(ctx context.Context, raw json.RawMessage) iter.Seq2[a2a.Event, error] {
	return streamJSONRPC(ctx, raw, h.handler.SendStreamingMessage)
}

func (h *jsonrpcHandler) onGetTaskPushConfig(ctx context.Context, raw json.RawMessage) (*a2a.PushConfig, error) {
	return callJSONRPC(ctx, raw, h.handler.GetTaskPushConfig)
}

func (h *jsonrpcHandler) onListTaskPushConfigs(ctx context.Context, raw json.RawMessage) ([]*a2a.PushConfig, error) {
	return callJSONRPC(ctx, raw, h.handler.ListTaskPushConfigs)
}

func (h *jsonrpcHandler) onSetTaskPushConfig(ctx context.Context, raw json.RawMessage) (*a2a.PushConfig, error) {
	return callJSONRPC(ctx, raw, h.handler.CreateTaskPushConfig)
}

func (h *jsonrpcHandler) onDeleteTaskPushConfig(ctx context.Context, raw json.RawMessage) error {
	return callJSONRPCNoResult(ctx, raw, h.handler.DeleteTaskPushConfig)
}

func (h *jsonrpcHandler) onGetExtendedAgentCard(ctx context.Context, raw json.RawMessage) (*a2a.AgentCard, error) {
	return callJSONRPC(ctx, raw, h.handler.GetExtendedAgentCard)
}

func (h *jsonrpcHandler) writeJSONRPCError(ctx context.Context, rw http.ResponseWriter, err error, reqID any) {
	resp := jsonrpc.NewErrorResponse(reqID, err)
	rw.Header().Set("Content-Type", jsonrpc.ContentJSON)
	if err := json.NewEncoder(rw).Encode(resp); err != nil {
		log.Error(ctx, "failed to send error response", err)
	}
}
