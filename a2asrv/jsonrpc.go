package a2asrv

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/internal/jsonrpc"
	"iter"
	"net/http"
)

// jsonrpcRequest represents a JSON-RPC 2.0 request.
type jsonrpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      string          `json:"id"`
}

// jsonrpcResponse represents a JSON-RPC 2.0 response.
type jsonrpcResponse struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      *string        `json:"id"`
	Result  any            `json:"result,omitempty"`
	Error   *jsonrpc.Error `json:"error,omitempty"`
}

type JSONRPCHandler struct {
	handler RequestHandler
}

func NewJSONRPCHandler(handler RequestHandler) *JSONRPCHandler {
	return &JSONRPCHandler{handler: handler}
}

func (h *JSONRPCHandler) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	defer req.Body.Close()

	var payload jsonrpcRequest
	if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
		jsonrpcErr := toJSONRPCError(a2a.ErrParseError)
		resp := jsonrpcResponse{JSONRPC: jsonrpc.Version, Error: jsonrpcErr}
		if err := json.NewEncoder(rw).Encode(resp); err != nil {
			// TODO(yarolegovich): log error
		}
		return
	}

	ctx := context.Background()
	if payload.Method == jsonrpc.MethodTasksResubscribe || payload.Method == jsonrpc.MethodMessageStream {
		h.handleStreamingRequest(ctx, rw, &payload)
	} else {
		h.handleRequest(ctx, rw, &payload)
	}
}

func (h *JSONRPCHandler) handleRequest(ctx context.Context, rw http.ResponseWriter, req *jsonrpcRequest) {
	var result any
	var err error
	switch req.Method {
	case jsonrpc.MethodTasksGet:
		result, err = h.onGetTask(ctx, req.Params)
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
	case jsonrpc.MethodGetAuthenticatedExtended:
		result, err = h.onGetAgentCard(ctx)
	default:
		err = a2a.ErrMethodNotFound
	}

	if err != nil {
		jsonrpcErr := toJSONRPCError(err)
		resp := jsonrpcResponse{JSONRPC: jsonrpc.Version, ID: &req.ID, Error: jsonrpcErr}
		if err := json.NewEncoder(rw).Encode(resp); err != nil {
			// TODO(yarolegovich): log error
		}
		return
	}

	if result != nil {
		resp := jsonrpcResponse{JSONRPC: jsonrpc.Version, ID: &req.ID, Result: result}
		if err := json.NewEncoder(rw).Encode(resp); err != nil {
			// TODO(yarolegovich): log error
		}
	}
}

func (h *JSONRPCHandler) handleStreamingRequest(ctx context.Context, rw http.ResponseWriter, req *jsonrpcRequest) {
	var events iter.Seq2[a2a.Event, error]
	switch req.Method {
	case jsonrpc.MethodTasksResubscribe:
		events = h.onResubscribeToTask(ctx, req.Params)
	case jsonrpc.MethodMessageStream:
		events = h.onSendMessageStream(ctx, req.Params)
	default:
		events = func(yield func(a2a.Event, error) bool) { yield(nil, a2a.ErrMethodNotFound) }
	}

	for event, err := range events {
		if err != nil {
			jsonrpcErr := toJSONRPCError(err)
			resp := jsonrpcResponse{JSONRPC: jsonrpc.Version, ID: &req.ID, Error: jsonrpcErr}
			if err := json.NewEncoder(rw).Encode(resp); err != nil {
				// TODO(yarolegovich): log error
			}
			return
		}

		resp := jsonrpcResponse{JSONRPC: jsonrpc.Version, ID: &req.ID, Result: result}
		if err := json.NewEncoder(rw).Encode(resp); err != nil {
			// TODO(yarolegovich): log error
		}
	}
}

func (h *JSONRPCHandler) onGetTask(ctx context.Context, raw json.RawMessage) (*a2a.Task, error) {
	var query a2a.TaskQueryParams
	if err := json.Unmarshal(raw, &query); err != nil {
		return nil, a2a.ErrParseError
	}
	return h.handler.OnGetTask(ctx, &query)
}

func (h *JSONRPCHandler) onCancelTask(ctx context.Context, raw json.RawMessage) (*a2a.Task, error) {
	var id a2a.TaskIDParams
	if err := json.Unmarshal(raw, &id); err != nil {
		return nil, a2a.ErrParseError
	}
	return h.handler.OnCancelTask(ctx, &id)
}

func (h *JSONRPCHandler) onSendMessage(ctx context.Context, raw json.RawMessage) (a2a.SendMessageResult, error) {
	var message a2a.MessageSendParams
	if err := json.Unmarshal(raw, &message); err != nil {
		return nil, a2a.ErrParseError
	}
	return h.handler.OnSendMessage(ctx, &message)
}

func (h *JSONRPCHandler) onResubscribeToTask(ctx context.Context, raw json.RawMessage) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		var id a2a.TaskIDParams
		if err := json.Unmarshal(raw, &id); err != nil {
			yield(nil, a2a.ErrParseError)
			return
		}
		for event, err := range h.handler.OnResubscribeToTask(ctx, &id) {
			if !yield(event, err) {
				return
			}
		}
	}
}

func (h *JSONRPCHandler) onSendMessageStream(ctx context.Context, raw json.RawMessage) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		var message a2a.MessageSendParams
		if err := json.Unmarshal(raw, &message); err != nil {
			yield(nil, a2a.ErrParseError)
			return
		}
		for event, err := range h.handler.OnSendMessageStream(ctx, &message) {
			if !yield(event, err) {
				return
			}
		}
	}

}

func (h *JSONRPCHandler) onGetTaskPushConfig(ctx context.Context, raw json.RawMessage) (*a2a.TaskPushConfig, error) {
	var params a2a.GetTaskPushConfigParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, a2a.ErrParseError
	}
	return h.handler.OnGetTaskPushConfig(ctx, &params)
}

func (h *JSONRPCHandler) onListTaskPushConfig(ctx context.Context, raw json.RawMessage) ([]*a2a.TaskPushConfig, error) {
	var params a2a.ListTaskPushConfigParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, a2a.ErrParseError
	}
	return h.handler.OnListTaskPushConfig(ctx, &params)
}

func (h *JSONRPCHandler) onSetTaskPushConfig(ctx context.Context, raw json.RawMessage) (*a2a.TaskPushConfig, error) {
	var params a2a.TaskPushConfig
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, a2a.ErrParseError
	}
	return h.handler.OnSetTaskPushConfig(ctx, &params)
}

func (h *JSONRPCHandler) onDeleteTaskPushConfig(ctx context.Context, raw json.RawMessage) error {
	var params a2a.DeleteTaskPushConfigParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return a2a.ErrParseError
	}
	return h.handler.OnDeleteTaskPushConfig(ctx, &params)
}

func (h *JSONRPCHandler) onGetAgentCard(_ context.Context) (*a2a.AgentCard, error) {
	return &a2a.AgentCard{}, nil
}

func toJSONRPCError(err error) *jsonrpc.Error {
	jsonrpcErr := &jsonrpc.Error{}
	if errors.As(err, &jsonrpcErr) {
		return jsonrpcErr
	}

	switch {
	case errors.Is(err, a2a.ErrParseError):
		return &jsonrpc.Error{Code: -32700, Message: err.Error()}
	case errors.Is(err, a2a.ErrInvalidRequest):
		return &jsonrpc.Error{Code: -32600, Message: err.Error()}
	case errors.Is(err, a2a.ErrMethodNotFound):
		return &jsonrpc.Error{Code: -32601, Message: err.Error()}
	case errors.Is(err, a2a.ErrInvalidParams):
		return &jsonrpc.Error{Code: -32602, Message: err.Error()}
	case errors.Is(err, a2a.ErrInternalError):
		return &jsonrpc.Error{Code: -32603, Message: err.Error()}
	case errors.Is(err, a2a.ErrServerError):
		return &jsonrpc.Error{Code: -32000, Message: err.Error()}
	case errors.Is(err, a2a.ErrTaskNotFound):
		return &jsonrpc.Error{Code: -32001, Message: err.Error()}
	case errors.Is(err, a2a.ErrTaskNotCancelable):
		return &jsonrpc.Error{Code: -32002, Message: err.Error()}
	case errors.Is(err, a2a.ErrPushNotificationNotSupported):
		return &jsonrpc.Error{Code: -32003, Message: err.Error()}
	case errors.Is(err, a2a.ErrUnsupportedOperation):
		return &jsonrpc.Error{Code: -32004, Message: err.Error()}
	case errors.Is(err, a2a.ErrUnsupportedContentType):
		return &jsonrpc.Error{Code: -32005, Message: err.Error()}
	case errors.Is(err, a2a.ErrInvalidAgentResponse):
		return &jsonrpc.Error{Code: -32006, Message: err.Error()}
	case errors.Is(err, a2a.ErrAuthenticatedExtendedCardNotConfigured):
		return &jsonrpc.Error{Code: -32007, Message: err.Error()}
	default:
		return &jsonrpc.Error{Code: -32000, Message: a2a.ErrInternalError.Error()}
	}
}
