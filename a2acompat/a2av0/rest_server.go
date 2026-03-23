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

	a2alegacy "github.com/a2aproject/a2a-go/a2a"
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
// It exposes the same URL routes as the v1.0 REST handler but accepts and returns
// v0.3-compatible JSON payloads. This allows v0.3 clients (e.g. Python ADK Agent Engine
// deployments) to communicate with a v1.0 Go server.
func NewRESTHandler(handler a2asrv.RequestHandler, opts ...a2asrv.TransportOption) http.Handler {
	h := &restCompatHandler{handler: handler, cfg: &a2asrv.TransportConfig{}}
	for _, opt := range opts {
		opt(h.cfg)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST "+rest.MakeSendMessagePath(), h.handleSendMessage)
	mux.HandleFunc("POST "+rest.MakeStreamMessagePath(), h.handleStreamMessage)
	mux.HandleFunc("GET "+rest.MakeGetTaskPath("{id}"), h.handleGetTask)
	mux.HandleFunc("GET "+rest.MakeListTasksPath(), h.handleListTasks)
	mux.HandleFunc("POST /tasks/{idAndAction}", h.handlePOSTTasks)
	mux.HandleFunc("POST "+rest.MakeCreatePushConfigPath("{id}"), h.handleCreateTaskPushConfig)
	mux.HandleFunc("GET "+rest.MakeGetPushConfigPath("{id}", "{configId}"), h.handleGetTaskPushConfig)
	mux.HandleFunc("GET "+rest.MakeListPushConfigsPath("{id}"), h.handleListTaskPushConfigs)
	mux.HandleFunc("DELETE "+rest.MakeDeletePushConfigPath("{id}", "{configId}"), h.handleDeleteTaskPushConfig)
	mux.HandleFunc("GET "+rest.MakeGetExtendedAgentCardPath(), h.handleGetExtendedAgentCard)

	return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		ctx, _ := a2asrv.NewCallContext(req.Context(), ToServiceParams(req.Header))
		mux.ServeHTTP(rw, req.WithContext(ctx))
	})
}

func (h *restCompatHandler) handleSendMessage(rw http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	var params a2alegacy.MessageSendParams
	if err := readSnakeCaseBody(req.Body, &params); err != nil {
		writeRESTCompatError(ctx, rw, a2a.ErrParseError, a2a.TaskID(""))
		return
	}
	v1req, err := ToV1SendMessageRequest(&params)
	if err != nil {
		writeRESTCompatError(ctx, rw, a2a.ErrParseError, a2a.TaskID(""))
		return
	}
	result, err := h.handler.SendMessage(ctx, v1req)
	if err != nil {
		writeRESTCompatError(ctx, rw, err, a2a.TaskID(""))
		return
	}
	compatEvent, err := FromV1Event(result)
	if err != nil {
		writeRESTCompatError(ctx, rw, err, a2a.TaskID(""))
		return
	}
	writeSnakeCaseJSON(ctx, rw, compatEvent)
}

func (h *restCompatHandler) handleStreamMessage(rw http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	var params a2alegacy.MessageSendParams
	if err := readSnakeCaseBody(req.Body, &params); err != nil {
		writeRESTCompatError(ctx, rw, a2a.ErrParseError, a2a.TaskID(""))
		return
	}
	v1req, err := ToV1SendMessageRequest(&params)
	if err != nil {
		writeRESTCompatError(ctx, rw, a2a.ErrParseError, a2a.TaskID(""))
		return
	}
	h.handleStreamingRequest(h.handler.SendStreamingMessage(ctx, v1req), rw, req)
}

func (h *restCompatHandler) handleGetTask(rw http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	taskID := req.PathValue("id")
	if taskID == "" {
		writeRESTCompatError(ctx, rw, a2a.ErrInvalidRequest, a2a.TaskID(""))
		return
	}
	query := &a2alegacy.TaskQueryParams{ID: a2alegacy.TaskID(taskID)}
	if raw := req.URL.Query().Get("historyLength"); raw != "" {
		val, err := strconv.Atoi(raw)
		if err != nil {
			writeRESTCompatError(ctx, rw, a2a.ErrInvalidRequest, a2a.TaskID(taskID))
			return
		}
		query.HistoryLength = &val
	}
	result, err := h.handler.GetTask(ctx, ToV1GetTaskRequest(query))
	if err != nil {
		writeRESTCompatError(ctx, rw, err, a2a.TaskID(taskID))
		return
	}
	writeSnakeCaseJSON(ctx, rw, FromV1Task(result))
}

func (h *restCompatHandler) handleListTasks(rw http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	qv := req.URL.Query()
	legacyReq := &a2alegacy.ListTasksRequest{}
	var parseErrors []error
	parse := func(key string, target any) {
		val := qv.Get(key)
		if val == "" {
			return
		}
		switch t := target.(type) {
		case *string:
			*t = val
		case *a2alegacy.TaskState:
			*t = a2alegacy.TaskState(val)
		case *int:
			v, err := strconv.Atoi(val)
			if err != nil {
				parseErrors = append(parseErrors, fmt.Errorf("invalid %s: %w", key, err))
				return
			}
			*t = v
		case *bool:
			v, err := strconv.ParseBool(val)
			if err != nil {
				parseErrors = append(parseErrors, fmt.Errorf("invalid %s: %w", key, err))
				return
			}
			*t = v
		case **time.Time:
			parsed, err := time.Parse(time.RFC3339, val)
			if err != nil {
				parseErrors = append(parseErrors, fmt.Errorf("invalid %s: %w", key, err))
				return
			}
			*t = &parsed
		}
	}
	parse("contextId", &legacyReq.ContextID)
	parse("status", &legacyReq.Status)
	parse("pageSize", &legacyReq.PageSize)
	parse("pageToken", &legacyReq.PageToken)
	parse("historyLength", &legacyReq.HistoryLength)
	parse("lastUpdatedAfter", &legacyReq.LastUpdatedAfter)
	parse("includeArtifacts", &legacyReq.IncludeArtifacts)

	if len(parseErrors) > 0 {
		writeRESTCompatError(ctx, rw, a2a.ErrInvalidRequest, a2a.TaskID(""))
		return
	}
	result, err := h.handler.ListTasks(ctx, ToV1ListTasksRequest(legacyReq))
	if err != nil {
		writeRESTCompatError(ctx, rw, err, a2a.TaskID(""))
		return
	}
	writeSnakeCaseJSON(ctx, rw, FromV1ListTasksResponse(result))
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
	} else if before, ok := strings.CutSuffix(idAndAction, ":subscribe"); ok {
		subscribeReq := ToV1SubscribeToTaskRequest(&a2alegacy.TaskIDParams{ID: a2alegacy.TaskID(before)})
		h.handleStreamingRequest(h.handler.SubscribeToTask(ctx, subscribeReq), rw, req)
	} else {
		writeRESTCompatError(ctx, rw, a2a.ErrInvalidRequest, a2a.TaskID(""))
	}
}

func (h *restCompatHandler) handleCancelTask(taskID string, rw http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	result, err := h.handler.CancelTask(ctx, ToV1CancelTaskRequest(&a2alegacy.TaskIDParams{ID: a2alegacy.TaskID(taskID)}))
	if err != nil {
		writeRESTCompatError(ctx, rw, err, a2a.TaskID(taskID))
		return
	}
	writeSnakeCaseJSON(ctx, rw, FromV1Task(result))
}

func (h *restCompatHandler) handleStreamingRequest(eventSequence iter.Seq2[a2a.Event, error], rw http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	sseWriter, err := sse.NewWriter(rw)
	if err != nil {
		writeRESTCompatError(ctx, rw, err, a2a.TaskID(""))
		return
	}
	sseWriter.WriteHeaders()

	sseChan, panicChan := make(chan []byte), make(chan error)
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
			compatEvent, err := FromV1Event(event)
			if err != nil {
				handleError(err)
				return
			}
			data, jErr := marshalSnakeCase(compatEvent)
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
	var config a2alegacy.PushConfig
	if err := readSnakeCaseBody(req.Body, &config); err != nil {
		writeRESTCompatError(ctx, rw, a2a.ErrParseError, a2a.TaskID(taskID))
		return
	}
	v1req := ToV1CreateTaskPushConfigRequest(&a2alegacy.TaskPushConfig{
		TaskID: a2alegacy.TaskID(taskID),
		Config: config,
	})
	result, err := h.handler.CreateTaskPushConfig(ctx, v1req)
	if err != nil {
		writeRESTCompatError(ctx, rw, err, a2a.TaskID(taskID))
		return
	}
	compatConfig, err := FromV1TaskPushConfig(result)
	if err != nil {
		writeRESTCompatError(ctx, rw, err, a2a.TaskID(taskID))
		return
	}
	writeSnakeCaseJSON(ctx, rw, compatConfig)
}

func (h *restCompatHandler) handleGetTaskPushConfig(rw http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	taskID := req.PathValue("id")
	configID := req.PathValue("configId")
	if taskID == "" || configID == "" {
		writeRESTCompatError(ctx, rw, a2a.ErrInvalidRequest, a2a.TaskID(taskID))
		return
	}
	result, err := h.handler.GetTaskPushConfig(ctx, ToV1GetTaskPushConfigRequest(&a2alegacy.GetTaskPushConfigParams{
		TaskID:   a2alegacy.TaskID(taskID),
		ConfigID: configID,
	}))
	if err != nil {
		writeRESTCompatError(ctx, rw, err, a2a.TaskID(taskID))
		return
	}
	compatConfig, err := FromV1TaskPushConfig(result)
	if err != nil {
		writeRESTCompatError(ctx, rw, err, a2a.TaskID(taskID))
		return
	}
	writeSnakeCaseJSON(ctx, rw, compatConfig)
}

func (h *restCompatHandler) handleListTaskPushConfigs(rw http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	taskID := req.PathValue("id")
	if taskID == "" {
		writeRESTCompatError(ctx, rw, a2a.ErrInvalidRequest, a2a.TaskID(""))
		return
	}
	result, err := h.handler.ListTaskPushConfigs(ctx, ToV1ListTaskPushConfigRequest(&a2alegacy.ListTaskPushConfigParams{
		TaskID: a2alegacy.TaskID(taskID),
	}))
	if err != nil {
		writeRESTCompatError(ctx, rw, err, a2a.TaskID(taskID))
		return
	}
	compatConfigs, err := FromV1TaskPushConfigs(result)
	if err != nil {
		writeRESTCompatError(ctx, rw, err, a2a.TaskID(taskID))
		return
	}
	if compatConfigs == nil {
		compatConfigs = []*a2alegacy.TaskPushConfig{}
	}
	writeSnakeCaseJSON(ctx, rw, compatConfigs)
}

func (h *restCompatHandler) handleDeleteTaskPushConfig(rw http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	taskID := req.PathValue("id")
	configID := req.PathValue("configId")
	if taskID == "" || configID == "" {
		writeRESTCompatError(ctx, rw, a2a.ErrInvalidRequest, a2a.TaskID(taskID))
		return
	}
	if err := h.handler.DeleteTaskPushConfig(ctx, ToV1DeleteTaskPushConfigRequest(&a2alegacy.DeleteTaskPushConfigParams{
		TaskID:   a2alegacy.TaskID(taskID),
		ConfigID: configID,
	})); err != nil {
		writeRESTCompatError(ctx, rw, err, a2a.TaskID(taskID))
	}
}

func (h *restCompatHandler) handleGetExtendedAgentCard(rw http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	result, err := h.handler.GetExtendedAgentCard(ctx, &a2a.GetExtendedAgentCardRequest{})
	if err != nil {
		writeRESTCompatError(ctx, rw, err, a2a.TaskID(""))
		return
	}
	writeSnakeCaseJSON(ctx, rw, FromV1AgentCard(result))
}

func writeRESTCompatError(ctx context.Context, rw http.ResponseWriter, err error, taskID a2a.TaskID) {
	errResp := rest.ToRESTError(err, taskID)
	rw.Header().Set("Content-Type", "application/problem+json")
	rw.WriteHeader(errResp.HTTPStatus())
	if jErr := json.NewEncoder(rw).Encode(errResp); jErr != nil {
		log.Error(ctx, "failed to write error response", jErr)
	}
}

// writeSnakeCaseJSON marshals v with snake_case keys and writes it to the response.
func writeSnakeCaseJSON(ctx context.Context, rw http.ResponseWriter, v any) {
	data, err := marshalSnakeCase(v)
	if err != nil {
		writeRESTCompatError(ctx, rw, fmt.Errorf("failed to marshal: %w", err), a2a.TaskID(""))
		return
	}
	if _, err := rw.Write(data); err != nil {
		log.Error(ctx, "failed to write response", err)
	}
}

// readSnakeCaseBody reads a JSON body with snake_case keys and unmarshals it
// into v (which has camelCase JSON tags).
func readSnakeCaseBody(body io.Reader, v any) error {
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	return unmarshalSnakeCase(data, v)
}
