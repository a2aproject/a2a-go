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
	"iter"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/internal/rest"
	"github.com/a2aproject/a2a-go/internal/sse"
	"github.com/a2aproject/a2a-go/log"
)

func NewRESTHandler(handler RequestHandler) http.Handler {
	mux := http.NewServeMux()

	// TODO: handle tenant
	mux.HandleFunc("POST "+rest.MakeSendMessagePath(), handleSendMessage(handler))
	mux.HandleFunc("POST "+rest.MakeStreamMessagePath(), handleStreamMessage(handler))
	mux.HandleFunc("GET "+rest.MakeGetTaskPath("{id}"), handleGetTask(handler))
	mux.HandleFunc("GET "+rest.MakeListTasksPath(), handleListTasks(handler))
	mux.HandleFunc("POST /tasks/{idAndAction}", handlePOSTTasks(handler))
	mux.HandleFunc("POST "+rest.MakeCreatePushConfigPath("{id}"), handleCreateTaskPushConfig(handler))
	mux.HandleFunc("GET "+rest.MakeGetPushConfigPath("{id}", "{configId}"), handleGetTaskPushConfig(handler))
	mux.HandleFunc("GET "+rest.MakeListPushConfigsPath("{id}"), handleListTaskPushConfig(handler))
	mux.HandleFunc("DELETE "+rest.MakeDeletePushConfigPath("{id}", "{configId}"), handleDeleteTaskPushConfig(handler))
	mux.HandleFunc("GET "+rest.MakeGetExtendedAgentCardPath(), handleGetExtendedAgentCard(handler))

	return mux
}

func handleSendMessage(handler RequestHandler) http.HandlerFunc {
	return func(rw http.ResponseWriter, req *http.Request) {
		ctx := req.Context()
		var message a2a.SendMessageRequest
		if err := json.NewDecoder(req.Body).Decode(&message); err != nil {
			writeRESTError(ctx, rw, a2a.ErrParseError, a2a.TaskID(""))
			return
		}

		result, err := handler.SendMessage(ctx, &message)

		if err != nil {
			writeRESTError(ctx, rw, err, a2a.TaskID(""))
			return
		}

		if err := json.NewEncoder(rw).Encode(a2a.StreamResponse{Event: result}); err != nil {
			log.Error(ctx, "failed to encode response", err)
		}
	}
}

func handleStreamMessage(handler RequestHandler) http.HandlerFunc {
	return func(rw http.ResponseWriter, req *http.Request) {
		ctx := req.Context()
		var message a2a.SendMessageRequest
		if err := json.NewDecoder(req.Body).Decode(&message); err != nil {
			writeRESTError(ctx, rw, a2a.ErrParseError, a2a.TaskID(""))
			return
		}
		handleStreamingRequest(handler.SendStreamingMessage(ctx, &message), rw, req)
	}
}

func handleGetTask(handler RequestHandler) http.HandlerFunc {
	return func(rw http.ResponseWriter, req *http.Request) {
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

		result, err := handler.GetTask(ctx, params)
		if err != nil {
			writeRESTError(ctx, rw, err, a2a.TaskID(taskID))
			return
		}

		if err := json.NewEncoder(rw).Encode(result); err != nil {
			log.Error(ctx, "failed to encode response", err)
		}
	}
}

func handleListTasks(handler RequestHandler) http.HandlerFunc {
	return func(rw http.ResponseWriter, req *http.Request) {
		ctx := req.Context()
		query := req.URL.Query()
		request := &a2a.ListTasksRequest{}
		var err error
		parse := func(key string, target interface{}) {
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
		if err != nil {
			writeRESTError(ctx, rw, a2a.ErrInvalidRequest, a2a.TaskID(""))
			return
		}
		result, err := handler.ListTasks(ctx, request)
		if err != nil {
			writeRESTError(ctx, rw, err, a2a.TaskID(""))
			return
		}
		if err := json.NewEncoder(rw).Encode(result); err != nil {
			log.Error(ctx, "failed to encode response", err)
		}
	}
}

func handlePOSTTasks(handler RequestHandler) http.HandlerFunc {
	return func(rw http.ResponseWriter, req *http.Request) {
		ctx := req.Context()
		idAndAction := req.PathValue("idAndAction")
		if idAndAction == "" {
			writeRESTError(ctx, rw, a2a.ErrInvalidRequest, a2a.TaskID(""))
			return
		}

		if before, ok := strings.CutSuffix(idAndAction, ":cancel"); ok {
			taskID := before
			handleCancelTask(handler, taskID, rw, req)
		} else if before, ok := strings.CutSuffix(idAndAction, ":subscribe"); ok {
			taskID := before
			handleStreamingRequest(handler.SubscribeToTask(ctx, &a2a.SubscribeToTaskRequest{ID: a2a.TaskID(taskID)}), rw, req)
		} else {
			writeRESTError(ctx, rw, a2a.ErrInvalidRequest, a2a.TaskID(""))
			return
		}
	}
}

func handleCancelTask(handler RequestHandler, taskID string, rw http.ResponseWriter, req *http.Request) {
	ctx := req.Context()

	id := &a2a.CancelTaskRequest{
		ID: a2a.TaskID(taskID),
	}

	result, err := handler.CancelTask(ctx, id)

	if err != nil {
		writeRESTError(ctx, rw, err, a2a.TaskID(taskID))
		return
	}

	if err := json.NewEncoder(rw).Encode(result); err != nil {
		log.Error(ctx, "failed to encode response", err)
	}
}

func handleStreamingRequest(eventSequence iter.Seq2[a2a.Event, error], rw http.ResponseWriter, req *http.Request) {
	ctx := req.Context()

	sseWriter, err := sse.NewWriter(rw)
	if err != nil {
		writeRESTError(ctx, rw, err, a2a.TaskID(""))
		return
	}
	sseWriter.WriteHeaders()

	sseChan := make(chan []byte)
	requestCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// TODO: handle panic and sse keep-alives similar to jsonrpc 
	go func() {
		defer close(sseChan)
		events := eventSequence
		for event, err := range events {
			if err != nil {
				// TODO(yarolegovich): clarify how rest bindings sends SSE errors
				log.Warn(ctx, "unhandled sse error", "error", err)
				return
			}

			b, jbErr := json.Marshal(a2a.StreamResponse{Event: event})
			if jbErr != nil {
				errObj := map[string]string{"error": jbErr.Error()}
				if eb, err := json.Marshal(errObj); err == nil {
					if eb != nil {
						select {
						case <-requestCtx.Done():
							return
						case sseChan <- eb:
						}
					}
				}
				return
			}

			select {
			case <-requestCtx.Done():
				return
			case sseChan <- b:
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		// TODO(yarolegovich): add keep-alive
		case data, ok := <-sseChan:
			if !ok {
				return
			}
			if err := sseWriter.WriteData(ctx, data); err != nil {
				log.Error(ctx, "failed to write SSE data", err)
				return
			}
		}
	}
}

func handleCreateTaskPushConfig(handler RequestHandler) http.HandlerFunc {
	return func(rw http.ResponseWriter, req *http.Request) {
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

		result, err := handler.CreateTaskPushConfig(ctx, params)

		if err != nil {
			writeRESTError(ctx, rw, err, a2a.TaskID(taskID))
			return
		}

		if err := json.NewEncoder(rw).Encode(result); err != nil {
			log.Error(ctx, "failed to encode response", err)
		}

	}
}

func handleGetTaskPushConfig(handler RequestHandler) http.HandlerFunc {
	return func(rw http.ResponseWriter, req *http.Request) {
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

		result, err := handler.GetTaskPushConfig(ctx, params)

		if err != nil {
			writeRESTError(ctx, rw, err, a2a.TaskID(taskID))
			return
		}

		if err := json.NewEncoder(rw).Encode(result); err != nil {
			log.Error(ctx, "failed to encode response", err)
		}

	}
}

func handleListTaskPushConfig(handler RequestHandler) http.HandlerFunc {
	return func(rw http.ResponseWriter, req *http.Request) {
		ctx := req.Context()
		taskID := req.PathValue("id")
		if taskID == "" {
			writeRESTError(ctx, rw, a2a.ErrInvalidRequest, a2a.TaskID(taskID))
			return
		}

		params := &a2a.ListTaskPushConfigRequest{
			TaskID: a2a.TaskID(taskID),
		}

		result, err := handler.ListTaskPushConfig(ctx, params)

		if err != nil {
			writeRESTError(ctx, rw, err, a2a.TaskID(taskID))
			return
		}

		if err := json.NewEncoder(rw).Encode(result); err != nil {
			log.Error(ctx, "failed to encode response", err)
		}
	}
}

func handleDeleteTaskPushConfig(handler RequestHandler) http.HandlerFunc {
	return func(rw http.ResponseWriter, req *http.Request) {
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

		err := handler.DeleteTaskPushConfig(ctx, params)

		if err != nil {
			writeRESTError(ctx, rw, err, a2a.TaskID(taskID))
			return
		}
	}
}

func handleGetExtendedAgentCard(handler RequestHandler) http.HandlerFunc {
	return func(rw http.ResponseWriter, req *http.Request) {
		ctx := req.Context()
		result, err := handler.GetExtendedAgentCard(ctx)

		if err != nil {
			writeRESTError(ctx, rw, err, a2a.TaskID(""))
			return
		}

		if err := json.NewEncoder(rw).Encode(result); err != nil {
			log.Error(ctx, "failed to encode response", err)
		}
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
