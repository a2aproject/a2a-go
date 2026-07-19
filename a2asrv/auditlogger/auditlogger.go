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

// Package auditlogger provides a server-side CallInterceptor that records
// structured audit events for every A2A protocol method invocation.
//
// The audit event schema aligns with IBM SMF Type 110 (CICS transaction audit)
// and Type 80 (RACF access audit) — recording who called what, when, with what
// result, and at what cost.
package auditlogger

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

// AuditEvent represents a single auditable A2A protocol invocation.
type AuditEvent struct {
	// Timestamp is when the request was received.
	Timestamp time.Time `json:"timestamp"`
	// AgentID is the identifier of the calling agent (from CallContext.User.Name).
	AgentID string `json:"agent_id"`
	// Method is the A2A protocol method (SendMessage, GetTask, CancelTask, etc.).
	Method string `json:"method"`
	// TaskID is the task identifier, if applicable.
	TaskID string `json:"task_id,omitempty"`
	// ContextID is the group identifier linking related tasks.
	ContextID string `json:"context_id,omitempty"`
	// Tenant is the tenant identifier from CallContext.
	Tenant string `json:"tenant,omitempty"`
	// Duration is the wall-clock time from Before to After.
	Duration time.Duration `json:"duration_ns"`
	// Result is "success", "error", or "cancelled".
	Result string `json:"result"`
	// ErrorMessage is set when Result is "error".
	ErrorMessage string `json:"error_message,omitempty"`
	// Extensions lists the extension URIs activated for this call.
	Extensions []string `json:"extensions,omitempty"`
}

// AuditWriter persists audit events. Implementations must be safe for
// concurrent use.
type AuditWriter interface {
	Write(ctx context.Context, event *AuditEvent) error
}

// AuditLogger is a server-side [a2asrv.CallInterceptor] that records a
// structured audit event for every A2A protocol call.
//
// The zero value is not usable; use [NewAuditLogger].
type AuditLogger struct {
	a2asrv.PassthroughCallInterceptor
	writer AuditWriter
}

// NewAuditLogger creates an AuditLogger that writes events to w.
func NewAuditLogger(w AuditWriter) *AuditLogger {
	return &AuditLogger{writer: w}
}

// Before records the start time in the context.
func (a *AuditLogger) Before(ctx context.Context, callCtx *a2asrv.CallContext, _ *a2asrv.Request) (context.Context, any, error) {
	return context.WithValue(ctx, startTimeKey{}, time.Now()), nil, nil
}

// After builds and writes the audit event.
func (a *AuditLogger) After(ctx context.Context, callCtx *a2asrv.CallContext, resp *a2asrv.Response) error {
	start, _ := ctx.Value(startTimeKey{}).(time.Time)
	if start.IsZero() {
		start = time.Now()
	}

	ev := &AuditEvent{
		Timestamp: start,
		Method:    callCtx.Method(),
		Duration:  time.Since(start),
	}

	if callCtx.User != nil {
		ev.AgentID = callCtx.User.Name
	}
	ev.Tenant = callCtx.Tenant()

	exts := callCtx.Extensions()
	if exts != nil {
		ev.Extensions = exts.RequestedURIs()
	}

	if resp != nil {
		if resp.Err != nil {
			if resp.Err == context.Canceled {
				ev.Result = "cancelled"
			} else {
				ev.Result = "error"
				ev.ErrorMessage = resp.Err.Error()
			}
		} else {
			ev.Result = "success"
		}
	} else {
		ev.Result = "success"
	}

	return a.writer.Write(ctx, ev)
}

type startTimeKey struct{}

// ── In-memory writer (for testing) ──────────────────────────────────────

// InMemoryWriter stores audit events in memory. Useful for tests and
// low-volume deployments. Not suitable for production — events are lost on
// restart.
type InMemoryWriter struct {
	mu     sync.Mutex
	events []*AuditEvent
}

// NewInMemoryWriter creates an InMemoryWriter.
func NewInMemoryWriter() *InMemoryWriter {
	return &InMemoryWriter{}
}

// Write implements AuditWriter.
func (w *InMemoryWriter) Write(_ context.Context, ev *AuditEvent) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.events = append(w.events, ev)
	return nil
}

// Events returns a copy of all recorded events.
func (w *InMemoryWriter) Events() []*AuditEvent {
	w.mu.Lock()
	defer w.mu.Unlock()
	cp := make([]*AuditEvent, len(w.events))
	copy(cp, w.events)
	return cp
}

// Count returns the number of recorded events.
func (w *InMemoryWriter) Count() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.events)
}

// ── JSONL file writer (for production) ───────────────────────────────────

// JSONLWriter appends audit events as JSON lines to a file.
// It is safe for concurrent use.
type JSONLWriter struct {
	mu   sync.Mutex
	file *os.File
}

// NewJSONLWriter opens or creates a JSONL file for appending.
func NewJSONLWriter(path string) (*JSONLWriter, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o640)
	if err != nil {
		return nil, fmt.Errorf("auditlogger: open %s: %w", path, err)
	}
	return &JSONLWriter{file: f}, nil
}

// Write implements AuditWriter. Each event is a single JSON line.
func (w *JSONLWriter) Write(_ context.Context, ev *AuditEvent) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	b, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = w.file.Write(b)
	return err
}

// Close flushes and closes the underlying file.
func (w *JSONLWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.file.Close()
}

// Compile-time check.
var _ io.Closer = (*JSONLWriter)(nil)
