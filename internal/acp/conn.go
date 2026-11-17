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

// Package acp implements the client side of the Agent Client Protocol
// (https://agentclientprotocol.com) over stdio: the protocol types the a2aacp
// executor uses, a JSON-RPC 2.0 connection and a typed client.
package acp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/a2aproject/a2a-go/v2/log"
)

// JSON-RPC error codes.
const (
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
)

// ErrAgentExited is reported for calls in flight when the peer closes its output or the connection is closed.
var ErrAgentExited = errors.New("acp agent exited")

// Error is a JSON-RPC error response.
type Error struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *Error) Error() string {
	if len(e.Data) > 0 {
		return fmt.Sprintf("%s (code %d): %s", e.Message, e.Code, e.Data)
	}
	return fmt.Sprintf("%s (code %d)", e.Message, e.Code)
}

// Message is a JSON-RPC request, notification or response. Requests and
// notifications have a Method; notifications have no ID.
type Message struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *Error          `json:"error,omitempty"`
}

// Conn is a JSON-RPC 2.0 connection over newline-delimited JSON, the ACP stdio transport.
// Lines which are not JSON-RPC messages are logged and skipped.
// Requests and notifications from the peer are passed to handle on the read goroutine,
// in order, so handle must not block.
type Conn struct {
	rwc    io.ReadWriteCloser
	handle func(*Message)

	wmu sync.Mutex
	enc *json.Encoder

	nextID atomic.Int64

	mu      sync.Mutex
	pending map[int64]chan *wireMessage
	err     error
	done    chan struct{}
}

// NewConn creates a connection over rwc. Call [Conn.Start] to begin reading.
func NewConn(rwc io.ReadWriteCloser, handle func(*Message)) *Conn {
	c := &Conn{
		rwc:     rwc,
		handle:  handle,
		enc:     json.NewEncoder(rwc),
		pending: map[int64]chan *wireMessage{},
		done:    make(chan struct{}),
	}
	return c
}

// Start begins reading messages from the peer.
func (c *Conn) Start() { go c.read() }

func (c *Conn) read() {
	r := bufio.NewReader(c.rwc)
	for {
		line, err := r.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			c.receive(line)
		}
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) || errors.Is(err, os.ErrClosed) {
				err = ErrAgentExited
			}
			c.fail(err)
			return
		}
	}
}

// wireMessage is a received message with members decoded separately, so that
// a mistyped member fails only the request it belongs to.
type wireMessage struct {
	ID     json.RawMessage `json:"id"`
	Method json.RawMessage `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

// receive handles a line of agent output. Lines which are not JSON-RPC
// messages, such as logs of wrapper scripts, are skipped.
func (c *Conn) receive(line []byte) {
	ctx := context.Background()
	var m wireMessage
	if err := json.Unmarshal(line, &m); err != nil {
		log.Warn(ctx, "skipped acp agent output which is not a JSON-RPC message", "output", excerpt(line), "cause", err)
		return
	}
	if m.Method != nil {
		c.receiveRequest(&m)
		return
	}
	if m.ID == nil {
		log.Warn(ctx, "skipped acp agent output which is not a JSON-RPC message", "output", excerpt(line))
		return
	}
	var id int64
	if err := json.Unmarshal(m.ID, &id); err != nil {
		log.Warn(ctx, "dropped acp response with unknown id", "id", string(m.ID), "cause", err)
		return
	}
	c.mu.Lock()
	ch := c.pending[id]
	delete(c.pending, id)
	c.mu.Unlock()
	if ch == nil {
		log.Debug(ctx, "dropped acp response nobody waits for", "id", id)
		return
	}
	ch <- &m
}

func (c *Conn) receiveRequest(m *wireMessage) {
	ctx := context.Background()
	var method string
	if err := json.Unmarshal(m.Method, &method); err != nil || method == "" {
		if len(m.ID) == 0 {
			log.Warn(ctx, "dropped acp notification with malformed method", "method", string(m.Method))
			return
		}
		log.Warn(ctx, "rejected acp request with malformed method", "method", string(m.Method))
		if err := c.RespondError(m.ID, &Error{Code: CodeInvalidRequest, Message: "method must be a non-empty string"}); err != nil {
			log.Warn(ctx, "failed to respond to acp agent", "cause", err)
		}
		return
	}
	c.handle(&Message{ID: m.ID, Method: method, Params: m.Params})
}

const maxExcerpt = 200

func excerpt(line []byte) string {
	line = bytes.TrimSpace(line)
	if len(line) > maxExcerpt {
		return strings.ToValidUTF8(string(line[:maxExcerpt]), "") + "..."
	}
	return string(line)
}

func (c *Conn) fail(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return
	}
	c.err = err
	close(c.done)
}

// Done is closed when the connection stops reading.
func (c *Conn) Done() <-chan struct{} { return c.done }

// Err returns the reason the connection stopped reading.
func (c *Conn) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// Close fails calls in flight and closes the underlying stream.
func (c *Conn) Close() error { return c.CloseWithError(ErrAgentExited) }

// CloseWithError fails calls in flight with err and closes the underlying stream.
// Calls keep the earlier error if the connection already stopped reading.
func (c *Conn) CloseWithError(err error) error {
	c.fail(err)
	return c.rwc.Close()
}

// Call sends a request and decodes the result of its response into result, if it is not nil.
func (c *Conn) Call(ctx context.Context, method string, params, result any) error {
	id := c.nextID.Add(1)
	ch := make(chan *wireMessage, 1)
	c.mu.Lock()
	if c.err != nil {
		c.mu.Unlock()
		return c.err
	}
	c.pending[id] = ch
	c.mu.Unlock()
	unregister := func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}

	req := struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int64  `json:"id"`
		Method  string `json:"method"`
		Params  any    `json:"params"`
	}{"2.0", id, method, params}
	if err := c.write(req); err != nil {
		unregister()
		return c.writeErr(ctx, err)
	}

	select {
	case m := <-ch:
		return decodeResult(method, m, result)
	case <-c.done:
		// The reader delivers a response before it stops, so a peer that
		// responded and exited may have closed done with ch already filled.
		select {
		case m := <-ch:
			return decodeResult(method, m, result)
		default:
			return c.Err()
		}
	case <-ctx.Done():
		unregister()
		return ctx.Err()
	}
}

func decodeResult(method string, m *wireMessage, result any) error {
	if len(m.Error) > 0 {
		var rpcErr *Error
		if err := json.Unmarshal(m.Error, &rpcErr); err != nil {
			return fmt.Errorf("decoding %s error: %w", method, err)
		}
		if rpcErr != nil {
			return rpcErr
		}
	}
	if result == nil || len(m.Result) == 0 {
		return nil
	}
	if err := json.Unmarshal(m.Result, result); err != nil {
		return fmt.Errorf("decoding %s result: %w", method, err)
	}
	return nil
}

// writeErr explains a failed write. It usually means the agent exited, and the
// reader soon learns why, for example from the agent's last output.
func (c *Conn) writeErr(ctx context.Context, err error) error {
	select {
	case <-c.done:
		return c.Err()
	case <-ctx.Done():
		return err
	case <-time.After(writeErrGrace):
		return err
	}
}

const writeErrGrace = 3 * time.Second

// Notify sends a notification.
func (c *Conn) Notify(method string, params any) error {
	return c.write(struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
		Params  any    `json:"params"`
	}{"2.0", method, params})
}

// Respond sends the result of a request from the peer.
func (c *Conn) Respond(id json.RawMessage, result any) error {
	return c.write(struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  any             `json:"result"`
	}{"2.0", id, result})
}

// RespondError sends an error response to a request from the peer.
func (c *Conn) RespondError(id json.RawMessage, rpcErr *Error) error {
	return c.write(struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Error   *Error          `json:"error"`
	}{"2.0", id, rpcErr})
}

func (c *Conn) write(v any) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	return c.enc.Encode(v)
}
