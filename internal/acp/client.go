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

package acp

import (
	"context"
	"encoding/json"
	"io"

	"github.com/a2aproject/a2a-go/v2/log"
)

// Handler receives the requests and notifications an agent sends to the
// client. Methods are called on the connection's read goroutine, in order, and
// must not block. A request must eventually be answered with the matching
// Client.Respond* method, using the passed request ID.
type Handler interface {
	// SessionUpdate reports progress of a session.
	SessionUpdate(n *SessionNotification)
	// RequestPermission asks to approve a tool call. Answer with [Client.RespondPermission].
	RequestPermission(id json.RawMessage, req *RequestPermissionRequest)
	// CreateElicitation asks the user for input. Answer with [Client.RespondElicitation].
	CreateElicitation(id json.RawMessage, req *CreateElicitationRequest)
}

// Client is the client side of an ACP connection. Requests the client does not
// support are answered with "method not found".
type Client struct {
	conn *Conn
	h    Handler
}

// NewClient creates a client over rwc. Call [Client.Start] to begin
// receiving messages from the agent.
func NewClient(rwc io.ReadWriteCloser, h Handler) *Client {
	c := &Client{h: h}
	c.conn = NewConn(rwc, c.dispatch)
	return c
}

// Start begins reading messages from the agent.
func (c *Client) Start() { c.conn.Start() }

// Done is closed when the agent's output ends.
func (c *Client) Done() <-chan struct{} { return c.conn.Done() }

// Err returns the reason the agent's output ended.
func (c *Client) Err() error { return c.conn.Err() }

// Close closes the connection to the agent.
func (c *Client) Close() error { return c.conn.Close() }

// Initialize negotiates the protocol version and capabilities.
func (c *Client) Initialize(ctx context.Context, req InitializeRequest) (*InitializeResponse, error) {
	var resp InitializeResponse
	if err := c.conn.Call(ctx, MethodInitialize, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// NewSession creates a session.
func (c *Client) NewSession(ctx context.Context, req NewSessionRequest) (*NewSessionResponse, error) {
	var resp NewSessionResponse
	if err := c.conn.Call(ctx, MethodSessionNew, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// LoadSession restores a session. The agent replays its history as session
// updates before it responds.
func (c *Client) LoadSession(ctx context.Context, req LoadSessionRequest) (*SessionState, error) {
	var resp SessionState
	if err := c.conn.Call(ctx, MethodSessionLoad, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Prompt runs a turn and returns when it ends. Progress arrives through [Handler.SessionUpdate].
func (c *Client) Prompt(ctx context.Context, req PromptRequest) (*PromptResponse, error) {
	var resp PromptResponse
	if err := c.conn.Call(ctx, MethodSessionPrompt, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Cancel asks the agent to stop the current turn of a session.
func (c *Client) Cancel(sessionID string) error {
	return c.conn.Notify(MethodSessionCancel, CancelNotification{SessionID: sessionID})
}

// SetConfigOption changes a session setting.
func (c *Client) SetConfigOption(ctx context.Context, req SetConfigOptionRequest) (*SetConfigOptionResponse, error) {
	var resp SetConfigOptionResponse
	if err := c.conn.Call(ctx, MethodSetConfigOption, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SetMode changes the mode of a session.
func (c *Client) SetMode(ctx context.Context, req SetModeRequest) error {
	return c.conn.Call(ctx, MethodSetMode, req, nil)
}

// SetModel changes the model of a session.
func (c *Client) SetModel(ctx context.Context, req SetModelRequest) error {
	return c.conn.Call(ctx, MethodSetModel, req, nil)
}

// RespondPermission answers a permission request.
func (c *Client) RespondPermission(id json.RawMessage, outcome PermissionOutcome) error {
	return c.conn.Respond(id, RequestPermissionResponse{Outcome: outcome})
}

// RespondElicitation answers an elicitation.
func (c *Client) RespondElicitation(id json.RawMessage, resp CreateElicitationResponse) error {
	return c.conn.Respond(id, resp)
}

// RespondError rejects a request from the agent.
func (c *Client) RespondError(id json.RawMessage, rpcErr *Error) error {
	return c.conn.RespondError(id, rpcErr)
}

func (c *Client) dispatch(m *Message) {
	ctx := context.Background()
	switch m.Method {
	case MethodSessionUpdate:
		var n SessionNotification
		if err := json.Unmarshal(m.Params, &n); err != nil {
			log.Warn(ctx, "malformed acp session update", "cause", err)
			return
		}
		c.h.SessionUpdate(&n)

	case MethodRequestPermission:
		var req RequestPermissionRequest
		if c.decode(m, &req) {
			c.h.RequestPermission(m.ID, &req)
		}

	case MethodElicitationCreate:
		var req CreateElicitationRequest
		if c.decode(m, &req) {
			c.h.CreateElicitation(m.ID, &req)
		}

	default:
		if len(m.ID) == 0 {
			log.Debug(ctx, "dropped unsupported acp notification", "method", m.Method)
			return
		}
		log.Debug(ctx, "rejected unsupported acp request", "method", m.Method)
		c.respondError(m.ID, &Error{Code: CodeMethodNotFound, Message: "method not supported by client: " + m.Method})
	}
}

// decode decodes the params of a request, answering malformed ones. Notifications
// with a request method are ignored, since there is nobody to answer.
func (c *Client) decode(m *Message, v any) bool {
	if len(m.ID) == 0 {
		log.Warn(context.Background(), "dropped acp request without id", "method", m.Method)
		return false
	}
	if err := json.Unmarshal(m.Params, v); err != nil {
		log.Warn(context.Background(), "rejected malformed acp request", "method", m.Method, "cause", err)
		c.respondError(m.ID, &Error{Code: CodeInvalidParams, Message: err.Error()})
		return false
	}
	return true
}

func (c *Client) respondError(id json.RawMessage, rpcErr *Error) {
	if err := c.conn.RespondError(id, rpcErr); err != nil {
		log.Warn(context.Background(), "failed to respond to acp agent", "cause", err)
	}
}
