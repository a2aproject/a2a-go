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
	"fmt"
	"io"
	"iter"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/internal/rest"
	"github.com/a2aproject/a2a-go/v2/internal/sse"
	"github.com/a2aproject/a2a-go/v2/log"
)

type restCompatHandler struct {
	handler a2asrv.RequestHandler
	cfg     *a2asrv.TransportConfig
}

// NewRESTHandler creates an [http.Handler] that serves A2A-protocol over HTTP+JSON v0.3.
//
// It exposes the A2A v0.3 REST routes (mounted under the "/v1" path prefix
// per the v0.3 spec) and accepts and returns proto-JSON payloads of the
// google.a2a.v1 proto. This allows v0.3 clients (e.g. Python ADK Agent Engine
// deployments) to communicate with a v1.0 Go server.
func NewRESTHandler(handler a2asrv.RequestHandler, opts ...a2asrv.TransportOption) http.Handler {
	h := &restCompatHandler{handler: handler, cfg: &a2asrv.TransportConfig{}}
	for _, opt := range opts {
		opt(h.cfg)
	}

	paths := rest.NewPathBuilder(RESTPathPrefix)
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+paths.SendMessage(), h.handleSendMessage)
	mux.HandleFunc("POST "+paths.StreamMessage(), h.handleStreamMessage)
	mux.HandleFunc("GET "+paths.ListTasks(), h.handleListTasks)
	mux.HandleFunc("GET "+paths.PostTasksActionRoute(), h.handleGETTasks)
	mux.HandleFunc("POST "+paths.PostTasksActionRoute(), h.handlePOSTTasks)
	mux.HandleFunc("POST "+paths.CreatePushConfig("{id}"), h.handleCreateTaskPushConfig)
	mux.HandleFunc("GET "+paths.ListPushConfigs("{id}"), h.handleListTaskPushConfigs)
	mux.HandleFunc("GET "+paths.GetPushConfig("{id}", "{configId}"), h.handleGetTaskPushConfig)
	mux.HandleFunc("DELETE "+paths.DeletePushConfig("{id}", "{configId}"), h.handleDeleteTaskPushConfig)
	mux.HandleFunc("GET "+RESTPathPrefix+"/card", h.handleGetExtendedAgentCard)

	return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		ctx, _ := a2asrv.NewCallContext(req.Context(), ToServiceParams(req.Header))
		mux.ServeHTTP(rw, req.WithContext(ctx))
	})
}

func (h *restCompatHandler) handleSendMessage(rw http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	body, err := io.ReadAll(req.Body)
	if err != nil {
		writeRESTCompatError(ctx, rw, a2a.ErrParseError, a2a.TaskID(""))
		return
	}
	v1req, err := unmarshalRESTSendMessageRequest(body)
	if err != nil {
		writeRESTCompatError(ctx, rw, a2a.ErrParseError, a2a.TaskID(""))
		return
	}
	result, err := h.handler.SendMessage(ctx, v1req)
	if err != nil {
		writeRESTCompatError(ctx, rw, err, a2a.TaskID(""))
		return
	}
	data, err := marshalRESTSendMessageResult(result)
	if err != nil {
		writeRESTCompatError(ctx, rw, err, a2a.TaskID(""))
		return
	}
	writeJSON(ctx, rw, data)
}

func (h *restCompatHandler) handleStreamMessage(rw http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	body, err := io.ReadAll(req.Body)
	if err != nil {
		writeRESTCompatError(ctx, rw, a2a.ErrParseError, a2a.TaskID(""))
		return
	}
	v1req, err := unmarshalRESTSendMessageRequest(body)
	if err != nil {
		writeRESTCompatError(ctx, rw, a2a.ErrParseError, a2a.TaskID(""))
		return
	}
	h.handleStreamingRequest(h.handler.SendStreamingMessage(ctx, v1req), rw, req)
}

func (h *restCompatHandler) handleGetTask(taskID string, rw http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	if taskID == "" {
		writeRESTCompatError(ctx, rw, a2a.ErrInvalidRequest, a2a.TaskID(""))
		return
	}
	getTaskReq := &a2a.GetTaskRequest{ID: a2a.TaskID(taskID)}
	if raw := req.URL.Query().Get("historyLength"); raw != "" {
		val, err := strconv.Atoi(raw)
		if err != nil {
			writeRESTCompatError(ctx, rw, a2a.ErrInvalidRequest, a2a.TaskID(taskID))
			return
		}
		getTaskReq.HistoryLength = &val
	}
	result, err := h.handler.GetTask(ctx, getTaskReq)
	if err != nil {
		writeRESTCompatError(ctx, rw, err, a2a.TaskID(taskID))
		return
	}
	data, err := marshalRESTTask(result)
	if err != nil {
		writeRESTCompatError(ctx, rw, err, a2a.TaskID(taskID))
		return
	}
	writeJSON(ctx, rw, data)
}

func (h *restCompatHandler) handlePOSTTasks(rw http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	idAndAction := req.PathValue("idAndAction")
	if idAndAction == "" {
		writeRESTCompatError(ctx, rw, a2a.ErrInvalidRequest, a2a.TaskID(""))
		return
	}
	if before, ok := strings.CutSuffix(idAndAction, ":cancel"); ok {
		h.handleCancelTask(before, rw, req)
		return
	}
	if before, ok := strings.CutSuffix(idAndAction, ":subscribe"); ok {
		h.handleStreamingRequest(h.handler.SubscribeToTask(ctx, &a2a.SubscribeToTaskRequest{ID: a2a.TaskID(before)}), rw, req)
		return
	}
	writeRESTCompatError(ctx, rw, a2a.ErrInvalidRequest, a2a.TaskID(""))
}

func (h *restCompatHandler) handleGETTasks(rw http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	idAndAction := req.PathValue("idAndAction")
	if before, ok := strings.CutSuffix(idAndAction, ":subscribe"); ok {
		h.handleStreamingRequest(h.handler.SubscribeToTask(ctx, &a2a.SubscribeToTaskRequest{ID: a2a.TaskID(before)}), rw, req)
		return
	}
	h.handleGetTask(idAndAction, rw, req)
}

func (h *restCompatHandler) handleCancelTask(taskID string, rw http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	result, err := h.handler.CancelTask(ctx, &a2a.CancelTaskRequest{ID: a2a.TaskID(taskID)})
	if err != nil {
		writeRESTCompatError(ctx, rw, err, a2a.TaskID(taskID))
		return
	}
	data, err := marshalRESTTask(result)
	if err != nil {
		writeRESTCompatError(ctx, rw, err, a2a.TaskID(taskID))
		return
	}
	writeJSON(ctx, rw, data)
}

func (h *restCompatHandler) handleStreamingRequest(eventSequence iter.Seq2[a2a.Event, error], rw http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	sseWriter, err := sse.NewWriter(rw)
	if err != nil {
		writeRESTCompatError(ctx, rw, err, a2a.TaskID(""))
		return
	}
	sseWriter.WriteHeaders()

	sseChan, panicChan := make(chan []byte), make(chan error, 1)
	requestCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				panicChan <- fmt.Errorf("%v\n%s", r, debug.Stack())
			} else {
				close(sseChan)
			}
		}()

		handleError := func(err error) {
			errData, jErr := json.Marshal(rest.ToRESTError(err, a2a.TaskID("")))
			if jErr != nil {
				log.Error(ctx, "failed to marshal error response", jErr)
				return
			}
			select {
			case <-requestCtx.Done():
			case sseChan <- errData:
			}
		}

		for event, err := range eventSequence {
			if err != nil {
				handleError(err)
				return
			}
			data, jErr := marshalRESTStreamEvent(event)
			if jErr != nil {
				handleError(jErr)
				return
			}
			select {
			case <-requestCtx.Done():
				return
			case sseChan <- data:
			}
		}
	}()

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
			data, jErr := json.Marshal(rest.ToRESTError(h.cfg.PanicHandler(err), a2a.TaskID("")))
			if jErr != nil {
				log.Error(ctx, "failed to marshal panic error response", jErr)
				return
			}
			if err := sseWriter.WriteData(ctx, data); err != nil {
				log.Error(ctx, "failed to write panic event", err)
			}
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

func (h *restCompatHandler) handleCreateTaskPushConfig(rw http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	taskID := req.PathValue("id")
	if taskID == "" {
		writeRESTCompatError(ctx, rw, a2a.ErrInvalidRequest, a2a.TaskID(""))
		return
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		writeRESTCompatError(ctx, rw, a2a.ErrParseError, a2a.TaskID(taskID))
		return
	}
	v1req, err := unmarshalRESTCreatePushConfigRequest(body, a2a.TaskID(taskID))
	if err != nil {
		writeRESTCompatError(ctx, rw, a2a.ErrParseError, a2a.TaskID(taskID))
		return
	}
	result, err := h.handler.CreateTaskPushConfig(ctx, v1req)
	if err != nil {
		writeRESTCompatError(ctx, rw, err, a2a.TaskID(taskID))
		return
	}
	data, err := marshalRESTPushConfigResponse(result)
	if err != nil {
		writeRESTCompatError(ctx, rw, err, a2a.TaskID(taskID))
		return
	}
	writeJSON(ctx, rw, data)
}

func (h *restCompatHandler) handleListTasks(rw http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	q := req.URL.Query()
	listReq := &a2a.ListTasksRequest{
		ContextID: q.Get("contextId"),
		Status:    decodeTaskState(q.Get("status")),
		PageToken: q.Get("pageToken"),
	}
	if v := q.Get("pageSize"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			writeRESTCompatError(ctx, rw, a2a.ErrInvalidRequest, a2a.TaskID(""))
			return
		}
		listReq.PageSize = n
	}
	if v := q.Get("historyLength"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			writeRESTCompatError(ctx, rw, a2a.ErrInvalidRequest, a2a.TaskID(""))
			return
		}
		listReq.HistoryLength = &n
	}
	if v := q.Get("lastUpdatedAfter"); v != "" {
		ts, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeRESTCompatError(ctx, rw, a2a.ErrInvalidRequest, a2a.TaskID(""))
			return
		}
		listReq.StatusTimestampAfter = &ts
	}
	if v := q.Get("includeArtifacts"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			writeRESTCompatError(ctx, rw, a2a.ErrInvalidRequest, a2a.TaskID(""))
			return
		}
		listReq.IncludeArtifacts = b
	}

	result, err := h.handler.ListTasks(ctx, listReq)
	if err != nil {
		writeRESTCompatError(ctx, rw, err, a2a.TaskID(""))
		return
	}
	data, err := marshalRESTListTasksResponse(result)
	if err != nil {
		writeRESTCompatError(ctx, rw, err, a2a.TaskID(""))
		return
	}
	writeJSON(ctx, rw, data)
}

func (h *restCompatHandler) handleListTaskPushConfigs(rw http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	taskID := req.PathValue("id")
	if taskID == "" {
		writeRESTCompatError(ctx, rw, a2a.ErrInvalidRequest, a2a.TaskID(""))
		return
	}
	result, err := h.handler.ListTaskPushConfigs(ctx, &a2a.ListTaskPushConfigRequest{TaskID: a2a.TaskID(taskID)})
	if err != nil {
		writeRESTCompatError(ctx, rw, err, a2a.TaskID(taskID))
		return
	}
	var configs []*a2a.PushConfig
	if result != nil {
		configs = result.Configs
	}
	data, err := marshalRESTListPushConfigsResponse(configs)
	if err != nil {
		writeRESTCompatError(ctx, rw, err, a2a.TaskID(taskID))
		return
	}
	writeJSON(ctx, rw, data)
}

func (h *restCompatHandler) handleDeleteTaskPushConfig(rw http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	taskID := req.PathValue("id")
	configID := req.PathValue("configId")
	if taskID == "" || configID == "" {
		writeRESTCompatError(ctx, rw, a2a.ErrInvalidRequest, a2a.TaskID(taskID))
		return
	}
	if err := h.handler.DeleteTaskPushConfig(ctx, &a2a.DeleteTaskPushConfigRequest{
		TaskID: a2a.TaskID(taskID),
		ID:     configID,
	}); err != nil {
		writeRESTCompatError(ctx, rw, err, a2a.TaskID(taskID))
		return
	}
	rw.WriteHeader(http.StatusNoContent)
}

func (h *restCompatHandler) handleGetTaskPushConfig(rw http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	taskID := req.PathValue("id")
	configID := req.PathValue("configId")
	if taskID == "" || configID == "" {
		writeRESTCompatError(ctx, rw, a2a.ErrInvalidRequest, a2a.TaskID(taskID))
		return
	}
	result, err := h.handler.GetTaskPushConfig(ctx, &a2a.GetTaskPushConfigRequest{
		TaskID: a2a.TaskID(taskID),
		ID:     configID,
	})
	if err != nil {
		writeRESTCompatError(ctx, rw, err, a2a.TaskID(taskID))
		return
	}
	data, err := marshalRESTPushConfigResponse(result)
	if err != nil {
		writeRESTCompatError(ctx, rw, err, a2a.TaskID(taskID))
		return
	}
	writeJSON(ctx, rw, data)
}

func (h *restCompatHandler) handleGetExtendedAgentCard(rw http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	result, err := h.handler.GetExtendedAgentCard(ctx, &a2a.GetExtendedAgentCardRequest{})
	if err != nil {
		writeRESTCompatError(ctx, rw, err, a2a.TaskID(""))
		return
	}
	data, err := json.Marshal(FromV1AgentCard(result))
	if err != nil {
		writeRESTCompatError(ctx, rw, fmt.Errorf("failed to marshal agent card: %w", err), a2a.TaskID(""))
		return
	}
	writeJSON(ctx, rw, data)
}

func writeRESTCompatError(ctx context.Context, rw http.ResponseWriter, err error, taskID a2a.TaskID) {
	errResp := rest.ToRESTError(err, taskID)
	rw.Header().Set("Content-Type", "application/problem+json")
	rw.WriteHeader(errResp.HTTPStatus())
	if jErr := json.NewEncoder(rw).Encode(errResp); jErr != nil {
		log.Error(ctx, "failed to write error response", jErr)
	}
}

// writeJSON writes pre-marshaled JSON bytes as a 200 OK response.
func writeJSON(ctx context.Context, rw http.ResponseWriter, data []byte) {
	if _, err := rw.Write(data); err != nil {
		log.Error(ctx, "failed to write response", err)
	}
}
