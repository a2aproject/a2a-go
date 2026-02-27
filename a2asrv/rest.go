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
	"net/url"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/a2aproject/a2a-go/v1/a2a"
	"github.com/a2aproject/a2a-go/v1/internal/pathtemplate"
	"github.com/a2aproject/a2a-go/v1/internal/rest"
	"github.com/a2aproject/a2a-go/v1/internal/sse"
	"github.com/a2aproject/a2a-go/v1/log"
)

type restHandler struct {
	handler RequestHandler
	cfg     *TransportConfig
}

// NewRESTHandler creates an [http.Handler] which implements the HTTP+JSON A2A protocol binding.
func NewRESTHandler(handler RequestHandler, opts ...TransportOption) http.Handler {
	h := &restHandler{handler: handler, cfg: &TransportConfig{}}
	for _, option := range opts {
		option(h.cfg)
	}

	mux := http.NewServeMux()

	// TODO: handle tenant
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

	return mux
}

// NewTenantRESTHandler creates an [http.Handler] which implements the HTTP+JSON A2A protocol binding.
// It extracts tenant information from the URL path based on the provided template, strips the prefix,
// and attaches the tenant ID (part inside {}) to the request context.
// Examples of templates:
// - "/{*}"
// - "/locations/*/projects/{*}"
// - "/{locations/*/projects/*}"
func NewTenantRESTHandler(tenantTemplate string, handler RequestHandler, opts ...TransportOption) http.Handler {
	compiledTemplate, err := pathtemplate.New(tenantTemplate)
	if err != nil {
		panic(fmt.Errorf("invalid template: %w", err))
	}
	restHandler := NewRESTHandler(handler, opts...)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		matchResult, ok := compiledTemplate.Match(r.URL.Path)
		if !ok {
			http.NotFound(w, r)
			return
		}

		r2 := new(http.Request)
		*r2 = *r
		r2 = r2.WithContext(attachTenant(r.Context(), matchResult.Captured))
		r2.URL = new(url.URL)
		*r2.URL = *r.URL
		r2.URL.Path = matchResult.Rest
		r2.URL.RawPath = ""
		restHandler.ServeHTTP(w, r2)
	})
}

func (h *restHandler) handleSendMessage(rw http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	var message a2a.SendMessageRequest
	if err := json.NewDecoder(req.Body).Decode(&message); err != nil {
		writeRESTError(ctx, rw, a2a.ErrParseError, a2a.TaskID(""))
		return
	}
	fillTenant(ctx, &message.Tenant)

	result, err := h.handler.SendMessage(ctx, &message)

	if err != nil {
		writeRESTError(ctx, rw, err, a2a.TaskID(""))
		return
	}

	if err := json.NewEncoder(rw).Encode(a2a.StreamResponse{Event: result}); err != nil {
		log.Error(ctx, "failed to encode response", err)
	}
}

func (h *restHandler) handleStreamMessage(rw http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	var message a2a.SendMessageRequest
	if err := json.NewDecoder(req.Body).Decode(&message); err != nil {
		writeRESTError(ctx, rw, a2a.ErrParseError, a2a.TaskID(""))
		return
	}
	fillTenant(ctx, &message.Tenant)
	h.handleStreamingRequest(h.handler.SendStreamingMessage(ctx, &message), rw, req)
}

func (h *restHandler) handleGetTask(rw http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	taskID := req.PathValue("id")
	historyLengthRaw := req.URL.Query().Get("historyLength")
	var historyLength *int
	if historyLengthRaw != "" {
		val, err := strconv.Atoi(historyLengthRaw)
		if err != nil {
			writeRESTError(ctx, rw, a2a.ErrInvalidRequest, a2a.TaskID(taskID))
			return
		}
		historyLength = &val
	}
	if taskID == "" {
		writeRESTError(ctx, rw, a2a.ErrInvalidRequest, a2a.TaskID(""))
		return
	}
	params := &a2a.GetTaskRequest{
		ID:            a2a.TaskID(taskID),
		HistoryLength: historyLength,
	}
	fillTenant(ctx, &params.Tenant)

	result, err := h.handler.GetTask(ctx, params)
	if err != nil {
		writeRESTError(ctx, rw, err, a2a.TaskID(taskID))
		return
	}

	if err := json.NewEncoder(rw).Encode(result); err != nil {
		log.Error(ctx, "failed to encode response", err)
	}
}

func (h *restHandler) handleListTasks(rw http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	query := req.URL.Query()
	request := &a2a.ListTasksRequest{}
	var err error
	parse := func(key string, target any) {
		val := query.Get(key)
		if val == "" {
			return
		}
		switch t := target.(type) {
		case *string:
			*t = val
		case *a2a.TaskState:
			*t = a2a.TaskState(val)
		case *int:
			*t, err = strconv.Atoi(val)
		case *bool:
			*t, err = strconv.ParseBool(val)
		case *time.Time:
			var parsedTime time.Time
			parsedTime, err = time.Parse(time.RFC3339, val)
			*t = parsedTime
		}
	}
	parse("contextId", &request.ContextID)
	parse("status", &request.Status)
	parse("pageSize", &request.PageSize)
	parse("pageToken", &request.PageToken)
	parse("historyLength", &request.HistoryLength)
	parse("statusTimestampAfter", &request.StatusTimestampAfter)
	parse("includeArtifacts", &request.IncludeArtifacts)
	fillTenant(ctx, &request.Tenant)
	if err != nil {
		writeRESTError(ctx, rw, a2a.ErrInvalidRequest, a2a.TaskID(""))
		return
	}
	result, err := h.handler.ListTasks(ctx, request)
	if err != nil {
		writeRESTError(ctx, rw, err, a2a.TaskID(""))
		return
	}
	if err := json.NewEncoder(rw).Encode(result); err != nil {
		log.Error(ctx, "failed to encode response", err)
	}
}

func (h *restHandler) handlePOSTTasks(rw http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	idAndAction := req.PathValue("idAndAction")
	if idAndAction == "" {
		writeRESTError(ctx, rw, a2a.ErrInvalidRequest, a2a.TaskID(""))
		return
	}

	if before, ok := strings.CutSuffix(idAndAction, ":cancel"); ok {
		taskID := before
		h.handleCancelTask(taskID, rw, req)
	} else if before, ok := strings.CutSuffix(idAndAction, ":subscribe"); ok {
		taskID := before
		req2 := &a2a.SubscribeToTaskRequest{ID: a2a.TaskID(taskID)}
		fillTenant(ctx, &req2.Tenant)
		h.handleStreamingRequest(h.handler.SubscribeToTask(ctx, req2), rw, req)
	} else {
		writeRESTError(ctx, rw, a2a.ErrInvalidRequest, a2a.TaskID(""))
		return
	}
}

func (h *restHandler) handleCancelTask(taskID string, rw http.ResponseWriter, req *http.Request) {
	ctx := req.Context()

	id := &a2a.CancelTaskRequest{
		ID: a2a.TaskID(taskID),
	}
	fillTenant(ctx, &id.Tenant)

	result, err := h.handler.CancelTask(ctx, id)

	if err != nil {
		writeRESTError(ctx, rw, err, a2a.TaskID(taskID))
		return
	}

	if err := json.NewEncoder(rw).Encode(result); err != nil {
		log.Error(ctx, "failed to encode response", err)
	}
}

func (h *restHandler) handleStreamingRequest(eventSequence iter.Seq2[a2a.Event, error], rw http.ResponseWriter, req *http.Request) {
	ctx := req.Context()

	sseWriter, err := sse.NewWriter(rw)
	if err != nil {
		writeRESTError(ctx, rw, err, a2a.TaskID(""))
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
				// Only close if not panice, otherwise <-sseChan would compete with <-panicChan in select
				close(sseChan)
			}
		}()

		handleError := func(err error) {
			errResp := rest.ToRESTError(err, a2a.TaskID(""))
			bytes, jErr := json.Marshal(errResp)
			if jErr != nil {
				log.Error(ctx, "failed to marshal error response", jErr)
				return
			}
			select {
			case <-requestCtx.Done():
			case sseChan <- bytes:
			}
		}

		for event, err := range eventSequence {
			if err != nil {
				handleError(err)
				return
			}

			bytes, jErr := json.Marshal(a2a.StreamResponse{Event: event})
			if jErr != nil {
				handleError(jErr)
				return
			}

			select {
			case <-requestCtx.Done():
				return
			case sseChan <- bytes:
			}
		}
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
			errResp := rest.ToRESTError(h.cfg.PanicHandler(err), a2a.TaskID(""))
			data, jErr := json.Marshal(errResp)
			if jErr != nil {
				log.Error(ctx, "failed to marshal panic error response", jErr)
				return
			}
			if err := sseWriter.WriteData(ctx, data); err != nil {
				log.Error(ctx, "failed to write panic event", err)
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

func (h *restHandler) handleCreateTaskPushConfig(rw http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	taskID := req.PathValue("id")
	if taskID == "" {
		writeRESTError(ctx, rw, a2a.ErrInvalidRequest, a2a.TaskID(taskID))
		return
	}

	config := &a2a.PushConfig{}
	if err := json.NewDecoder(req.Body).Decode(config); err != nil {
		writeRESTError(ctx, rw, a2a.ErrParseError, a2a.TaskID(taskID))
		return
	}

	params := &a2a.CreateTaskPushConfigRequest{
		TaskID: a2a.TaskID(taskID),
		Config: *config,
	}
	fillTenant(ctx, &params.Tenant)

	result, err := h.handler.CreateTaskPushConfig(ctx, params)

	if err != nil {
		writeRESTError(ctx, rw, err, a2a.TaskID(taskID))
		return
	}

	if err := json.NewEncoder(rw).Encode(result); err != nil {
		log.Error(ctx, "failed to encode response", err)
	}
}

func (h *restHandler) handleGetTaskPushConfig(rw http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	taskID := req.PathValue("id")
	configID := req.PathValue("configId")
	if taskID == "" || configID == "" {
		writeRESTError(ctx, rw, a2a.ErrInvalidRequest, a2a.TaskID(taskID))
		return
	}

	params := &a2a.GetTaskPushConfigRequest{
		TaskID: a2a.TaskID(taskID),
		ID:     configID,
	}
	fillTenant(ctx, &params.Tenant)

	result, err := h.handler.GetTaskPushConfig(ctx, params)

	if err != nil {
		writeRESTError(ctx, rw, err, a2a.TaskID(taskID))
		return
	}

	if err := json.NewEncoder(rw).Encode(result); err != nil {
		log.Error(ctx, "failed to encode response", err)
	}

}

func (h *restHandler) handleListTaskPushConfigs(rw http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	taskID := req.PathValue("id")
	if taskID == "" {
		writeRESTError(ctx, rw, a2a.ErrInvalidRequest, a2a.TaskID(taskID))
		return
	}

	params := &a2a.ListTaskPushConfigRequest{
		TaskID: a2a.TaskID(taskID),
	}
	fillTenant(ctx, &params.Tenant)

	result, err := h.handler.ListTaskPushConfigs(ctx, params)

	if err != nil {
		writeRESTError(ctx, rw, err, a2a.TaskID(taskID))
		return
	}

	if err := json.NewEncoder(rw).Encode(result); err != nil {
		log.Error(ctx, "failed to encode response", err)
	}
}

func (h *restHandler) handleDeleteTaskPushConfig(rw http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	taskID := req.PathValue("id")
	configID := req.PathValue("configId")
	if taskID == "" || configID == "" {
		writeRESTError(ctx, rw, a2a.ErrInvalidRequest, a2a.TaskID(taskID))
		return
	}

	params := &a2a.DeleteTaskPushConfigRequest{
		TaskID: a2a.TaskID(taskID),
		ID:     configID,
	}
	fillTenant(ctx, &params.Tenant)

	err := h.handler.DeleteTaskPushConfig(ctx, params)

	if err != nil {
		writeRESTError(ctx, rw, err, a2a.TaskID(taskID))
		return
	}
}

func (h *restHandler) handleGetExtendedAgentCard(rw http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	req2 := &a2a.GetExtendedAgentCardRequest{}
	fillTenant(ctx, &req2.Tenant)
	result, err := h.handler.GetExtendedAgentCard(ctx, req2)

	if err != nil {
		writeRESTError(ctx, rw, err, a2a.TaskID(""))
		return
	}

	if err := json.NewEncoder(rw).Encode(result); err != nil {
		log.Error(ctx, "failed to encode response", err)
	}
}

func writeRESTError(ctx context.Context, rw http.ResponseWriter, err error, taskID a2a.TaskID) {
	errResp := rest.ToRESTError(err, taskID)
	rw.Header().Set("Content-Type", "application/problem+json")
	rw.WriteHeader(errResp.Status)

	if err := json.NewEncoder(rw).Encode(errResp); err != nil {
		log.Error(ctx, "failed to write error response", err)
	}
}

type tenantKeyType struct{}

func fillTenant(ctx context.Context, tenant *string) {
	if t := tenantFromContext(ctx); t != "" {
		*tenant = t
	}
}

func attachTenant(parent context.Context, tenant string) context.Context {
	return context.WithValue(parent, tenantKeyType{}, tenant)
}

func tenantFromContext(ctx context.Context) string {
	if tenant, ok := ctx.Value(tenantKeyType{}).(string); ok {
		return tenant
	}
	return ""
}
