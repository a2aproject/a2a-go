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

package a2aacp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

const (
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
)

// errAgentExited is reported for calls that were in flight when the agent closed its output.
var errAgentExited = errors.New("acp agent exited")

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *rpcError) Error() string {
	if len(e.Data) > 0 {
		return fmt.Sprintf("%s (code %d): %s", e.Message, e.Code, e.Data)
	}
	return fmt.Sprintf("%s (code %d)", e.Message, e.Code)
}

type rpcMessage struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *rpcError       `json:"error,omitempty"`
}

// conn is a JSON-RPC 2.0 connection over newline-delimited JSON, the ACP stdio transport.
// Requests and notifications from the peer are passed to handle on the read goroutine,
// in order, so handle must not block.
type conn struct {
	rwc    io.ReadWriteCloser
	handle func(*rpcMessage)

	wmu sync.Mutex
	enc *json.Encoder

	nextID atomic.Int64

	mu      sync.Mutex
	pending map[int64]chan *rpcMessage
	err     error
	done    chan struct{}
}

func newConn(rwc io.ReadWriteCloser, handle func(*rpcMessage)) *conn {
	c := &conn{
		rwc:     rwc,
		handle:  handle,
		enc:     json.NewEncoder(rwc),
		pending: map[int64]chan *rpcMessage{},
		done:    make(chan struct{}),
	}
	return c
}

// start begins reading messages from the peer.
func (c *conn) start() { go c.read() }

func (c *conn) read() {
	dec := json.NewDecoder(c.rwc)
	for {
		var m rpcMessage
		if err := dec.Decode(&m); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) || errors.Is(err, os.ErrClosed) {
				err = errAgentExited
			}
			c.fail(err)
			return
		}
		if m.Method != "" {
			c.handle(&m)
			continue
		}
		var id int64
		if err := json.Unmarshal(m.ID, &id); err != nil {
			continue
		}
		c.mu.Lock()
		ch := c.pending[id]
		delete(c.pending, id)
		c.mu.Unlock()
		if ch != nil {
			ch <- &m
		}
	}
}

func (c *conn) fail(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return
	}
	c.err = err
	close(c.done)
}

// Done is closed when the connection stops reading.
func (c *conn) Done() <-chan struct{} { return c.done }

// Err returns the reason the connection stopped reading.
func (c *conn) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

func (c *conn) Close() error {
	c.fail(errAgentExited)
	return c.rwc.Close()
}

func (c *conn) call(ctx context.Context, method string, params, result any) error {
	id := c.nextID.Add(1)
	ch := make(chan *rpcMessage, 1)
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
		if m.Error != nil {
			return m.Error
		}
		if result == nil || len(m.Result) == 0 {
			return nil
		}
		if err := json.Unmarshal(m.Result, result); err != nil {
			return fmt.Errorf("decoding %s result: %w", method, err)
		}
		return nil
	case <-c.done:
		return c.Err()
	case <-ctx.Done():
		unregister()
		return ctx.Err()
	}
}

// writeErr explains a failed write. It usually means the agent exited, and the
// reader soon learns why, for example from the agent's last output.
func (c *conn) writeErr(ctx context.Context, err error) error {
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

func (c *conn) notify(method string, params any) error {
	return c.write(struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
		Params  any    `json:"params"`
	}{"2.0", method, params})
}

func (c *conn) respond(id json.RawMessage, result any) error {
	return c.write(struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  any             `json:"result"`
	}{"2.0", id, result})
}

func (c *conn) respondError(id json.RawMessage, rpcErr *rpcError) error {
	return c.write(struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Error   *rpcError       `json:"error"`
	}{"2.0", id, rpcErr})
}

func (c *conn) write(v any) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	return c.enc.Encode(v)
}
