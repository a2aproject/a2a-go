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
	"net/http"
	"strings"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/internal/rest"
	"github.com/a2aproject/a2a-go/internal/sse"
	"github.com/a2aproject/a2a-go/log"
)

func NewRESTHandler(handler RequestHandler) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /v1/message:send", handleSendMessage(handler))
	mux.HandleFunc("POST /v1/message:stream", handleStreamMessage(handler))
	mux.HandleFunc("POST /v1/tasks/{idAndAction}", handlePOSTTasks(handler))
	mux.HandleFunc("GET /v1/tasks/{id}", handleGetTask(handler))
	mux.HandleFunc("POST /v1/tasks/{id}/pushNotificationConfigs", handleSetTaskPushConfig(handler))
	mux.HandleFunc("GET /v1/tasks/{id}/pushNotificationConfigs/{configId}", handleGetTaskPushConfig(handler))
	mux.HandleFunc("GET /v1/tasks/{id}/pushNotificationConfigs", handleListTaskPushConfig(handler))
	mux.HandleFunc("DELETE /v1/tasks/{id}/pushNotificationConfigs/{configId}", handleDeleteTaskPushConfig(handler))
	mux.HandleFunc("GET /v1/card", handleGetExtendedAgentCard(handler))

	return mux
}

func handleSendMessage(handler RequestHandler) http.HandlerFunc {
	return func(rw http.ResponseWriter, req *http.Request) {
		ctx := req.Context()
		var message a2a.MessageSendParams
		if err := json.NewDecoder(req.Body).Decode(&message); err != nil {
			writeRESTError(ctx, rw, a2a.ErrParseError, "")
			return
		}

		result, err := handler.OnSendMessage(ctx, &message)

		if err != nil {
			writeRESTError(ctx, rw, err, "")
			return
		}

		if err := json.NewEncoder(rw).Encode(result); err != nil {
			log.Error(ctx, "failed to encode response", err)
		}
	}
}

func handleStreamMessage(handler RequestHandler) http.HandlerFunc {
	return func(rw http.ResponseWriter, req *http.Request) {
		ctx := req.Context()
		// Decode request params
		var message a2a.MessageSendParams
		if err := json.NewDecoder(req.Body).Decode(&message); err != nil {
			writeRESTError(ctx, rw, a2a.ErrParseError, "")
			return
		}

		// Create SSE writer
		sseWriter, err := sse.NewWriter(rw)
		if err != nil {
			writeRESTError(ctx, rw, err, "")
			return
		}
		sseWriter.WriteHeaders()

		// Channel for marshalled JSON bytes to send as SSE data frames
		sseChan := make(chan []byte)
		requestCtx, cancel := context.WithCancel(ctx)
		defer cancel()

		go func() {
			defer close(sseChan)
			events := handler.OnSendMessageStream(requestCtx, &message)
			for event, err := range events {
				if err != nil {
					// send an error object then stop the stream
					errObj := map[string]string{"error": err.Error()}
					if b, jbErr := json.Marshal(errObj); jbErr == nil {
						select {
						case <-requestCtx.Done():
							return
						case sseChan <- b:
						}
					}
					return
				}

				b, jbErr := json.Marshal(event)
				if jbErr != nil {
					// send marshal error and stop
					errObj := map[string]string{"error": jbErr.Error()}
					if eb, _ := json.Marshal(errObj); eb != nil {
						select {
						case <-requestCtx.Done():
							return
						case sseChan <- eb:
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
}

func handleGetTask(handler RequestHandler) http.HandlerFunc {
	return func(rw http.ResponseWriter, req *http.Request) {
		ctx := req.Context()
		taskID := req.URL.Query().Get("id")
		if taskID == "" {
			writeRESTError(ctx, rw, a2a.ErrInvalidRequest, "")
			return
		}

		id := &a2a.TaskQueryParams{
			ID: a2a.TaskID(taskID),
		}

		result, err := handler.OnGetTask(ctx, id)
		if err != nil {
			writeRESTError(ctx, rw, err, taskID)
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
			writeRESTError(ctx, rw, a2a.ErrInvalidRequest, "")
			return
		}

		if strings.HasSuffix(idAndAction, ":cancel") {
			// Handle cancel task
			taskID := strings.TrimSuffix(idAndAction, ":cancel")
			handleCancelTask(handler, taskID, rw, req)
		} else if strings.HasSuffix(idAndAction, ":subscribe") {
			// Handle resubscribe to task
			taskID := strings.TrimSuffix(idAndAction, ":subscribe")
			handleResubscribeTask(handler, taskID, rw, req)
		} else {
			writeRESTError(ctx, rw, a2a.ErrInvalidRequest, "")
			return
		}
	}
}

func handleCancelTask(handler RequestHandler, taskID string, rw http.ResponseWriter, req *http.Request) {
	ctx := req.Context()

	id := &a2a.TaskIDParams{
		ID: a2a.TaskID(taskID),
	}

	result, err := handler.OnCancelTask(ctx, id)

	if err != nil {
		writeRESTError(ctx, rw, err, taskID)
		return
	}

	if err := json.NewEncoder(rw).Encode(result); err != nil {
		log.Error(ctx, "failed to encode response", err)
	}
}

func handleResubscribeTask(handler RequestHandler, taskID string, rw http.ResponseWriter, req *http.Request) {
	ctx := req.Context()

	id := &a2a.TaskIDParams{
		ID: a2a.TaskID(taskID),
	}

	sseWriter, err := sse.NewWriter(rw)
	if err != nil {
		writeRESTError(ctx, rw, err, taskID)
		return
	}
	sseWriter.WriteHeaders()

	sseChan := make(chan []byte)
	requestCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		defer close(sseChan)
		events := handler.OnResubscribeToTask(requestCtx, id)
		for event, err := range events {
			if err != nil {
				// send an error object then stop the stream
				errObj := map[string]string{"error": err.Error()}
				if b, jbErr := json.Marshal(errObj); jbErr == nil {
					select {
					case <-requestCtx.Done():
						return
					case sseChan <- b:
					}
				}
				return
			}

			b, jbErr := json.Marshal(event)
			if jbErr != nil {
				// send marshal error and stop
				errObj := map[string]string{"error": jbErr.Error()}
				if eb, _ := json.Marshal(errObj); eb != nil {
					select {
					case <-requestCtx.Done():
						return
					case sseChan <- eb:
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

func handleSetTaskPushConfig(handler RequestHandler) http.HandlerFunc {
	return func(rw http.ResponseWriter, req *http.Request) {
		ctx := req.Context()
		taskID := req.URL.Query().Get("id")
		if taskID == "" {
			writeRESTError(ctx, rw, a2a.ErrInvalidRequest, taskID)
			return
		}

		config := &a2a.PushConfig{}
		if err := json.NewDecoder(req.Body).Decode(config); err != nil {
			writeRESTError(ctx, rw, a2a.ErrParseError, taskID)
			return
		}

		params := &a2a.TaskPushConfig{
			TaskID: a2a.TaskID(taskID),
			Config: *config,
		}

		result, err := handler.OnSetTaskPushConfig(ctx, params)

		if err != nil {
			writeRESTError(ctx, rw, err, taskID)
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
		taskID := req.URL.Query().Get("id")
		configID := req.URL.Query().Get("configId")
		if taskID == "" || configID == "" {
			writeRESTError(ctx, rw, a2a.ErrInvalidRequest, taskID)
			return
		}

		params := &a2a.GetTaskPushConfigParams{
			TaskID:   a2a.TaskID(taskID),
			ConfigID: configID,
		}

		result, err := handler.OnGetTaskPushConfig(ctx, params)

		if err != nil {
			writeRESTError(ctx, rw, err, taskID)
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
		taskID := req.URL.Query().Get("id")
		if taskID == "" {
			writeRESTError(ctx, rw, a2a.ErrInvalidRequest, taskID)
			return
		}

		params := &a2a.ListTaskPushConfigParams{
			TaskID: a2a.TaskID(taskID),
		}

		result, err := handler.OnListTaskPushConfig(ctx, params)

		if err != nil {
			writeRESTError(ctx, rw, err, taskID)
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
		taskID := req.URL.Query().Get("id")
		configID := req.URL.Query().Get("configId")
		if taskID == "" || configID == "" {
			writeRESTError(ctx, rw, a2a.ErrInvalidRequest, taskID)
			return
		}

		params := &a2a.DeleteTaskPushConfigParams{
			TaskID:   a2a.TaskID(taskID),
			ConfigID: configID,
		}

		err := handler.OnDeleteTaskPushConfig(ctx, params)

		if err != nil {
			writeRESTError(ctx, rw, err, taskID)
			return
		}
	}
}

func handleGetExtendedAgentCard(handler RequestHandler) http.HandlerFunc {
	return func(rw http.ResponseWriter, req *http.Request) {
		ctx := req.Context()
		result, err := handler.OnGetExtendedAgentCard(ctx)

		if err != nil {
			writeRESTError(ctx, rw, err, "")
			return
		}

		if err := json.NewEncoder(rw).Encode(result); err != nil {
			log.Error(ctx, "failed to encode response", err)
		}
	}
}

func writeRESTError(ctx context.Context, rw http.ResponseWriter, err error, taskID string) {
	errResp := rest.ToRESTError(err, taskID)
	rw.Header().Set("Content-Type", "application/problem+json")
	rw.WriteHeader(errResp.Status)

	if err := json.NewEncoder(rw).Encode(errResp); err != nil {
		log.Error(ctx, "failed to write error response", err)
	}
}
