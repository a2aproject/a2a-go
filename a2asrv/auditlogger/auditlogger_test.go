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

package auditlogger

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

func TestAuditLogger_Success(t *testing.T) {
	w := NewInMemoryWriter()
	al := NewAuditLogger(w)

	ctx := context.Background()
	ctx, callCtx := a2asrv.NewCallContext(ctx, nil)
	callCtx.User = a2asrv.NewAuthenticatedUser("agent-strategy", nil)

	ctx, _, err := al.Before(ctx, callCtx, &a2asrv.Request{})
	if err != nil {
		t.Fatal(err)
	}
	if err := al.After(ctx, callCtx, &a2asrv.Response{}); err != nil {
		t.Fatal(err)
	}

	if w.Count() != 1 {
		t.Fatalf("expected 1 event, got %d", w.Count())
	}
	ev := w.Events()[0]
	if ev.AgentID != "agent-strategy" {
		t.Errorf("expected agent-strategy, got %s", ev.AgentID)
	}
	if ev.Result != "success" {
		t.Errorf("expected success, got %s", ev.Result)
	}
	if ev.Duration <= 0 {
		t.Errorf("expected positive duration, got %v", ev.Duration)
	}
}

func TestAuditLogger_Error(t *testing.T) {
	w := NewInMemoryWriter()
	al := NewAuditLogger(w)

	ctx := context.Background()
	ctx, callCtx := a2asrv.NewCallContext(ctx, nil)

	ctx, _, _ = al.Before(ctx, callCtx, &a2asrv.Request{})
	_ = al.After(ctx, callCtx, &a2asrv.Response{Err: errors.New("something went wrong")})

	ev := w.Events()[0]
	if ev.Result != "error" {
		t.Errorf("expected error, got %s", ev.Result)
	}
	if ev.ErrorMessage != "something went wrong" {
		t.Errorf("expected error message, got %s", ev.ErrorMessage)
	}
}

func TestAuditLogger_Cancelled(t *testing.T) {
	w := NewInMemoryWriter()
	al := NewAuditLogger(w)

	ctx := context.Background()
	ctx, callCtx := a2asrv.NewCallContext(ctx, nil)

	ctx, _, _ = al.Before(ctx, callCtx, &a2asrv.Request{})
	_ = al.After(ctx, callCtx, &a2asrv.Response{Err: context.Canceled})

	ev := w.Events()[0]
	if ev.Result != "cancelled" {
		t.Errorf("expected cancelled, got %s", ev.Result)
	}
}

func TestAuditLogger_MultipleEvents(t *testing.T) {
	w := NewInMemoryWriter()
	al := NewAuditLogger(w)

	for i := 0; i < 5; i++ {
		ctx := context.Background()
		ctx, callCtx := a2asrv.NewCallContext(ctx, nil)
		callCtx.User = a2asrv.NewAuthenticatedUser("agent-ops", nil)
		ctx, _, _ = al.Before(ctx, callCtx, &a2asrv.Request{})
		_ = al.After(ctx, callCtx, &a2asrv.Response{})
	}

	if w.Count() != 5 {
		t.Fatalf("expected 5 events, got %d", w.Count())
	}
}

func TestAuditLogger_NoUser(t *testing.T) {
	w := NewInMemoryWriter()
	al := NewAuditLogger(w)

	ctx := context.Background()
	ctx, callCtx := a2asrv.NewCallContext(ctx, nil)
	ctx, _, _ = al.Before(ctx, callCtx, &a2asrv.Request{})
	_ = al.After(ctx, callCtx, &a2asrv.Response{})

	ev := w.Events()[0]
	if ev.AgentID != "" {
		t.Errorf("expected empty agent_id, got %s", ev.AgentID)
	}
}

func TestJSONLWriter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	w, err := NewJSONLWriter(path)
	if err != nil {
		t.Fatal(err)
	}

	ev := &AuditEvent{
		Timestamp: time.Unix(1000, 0),
		AgentID:   "agent-test",
		Method:    "SendMessage",
		TaskID:    "task-123",
		Duration:  time.Second,
		Result:    "success",
	}

	if err := w.Write(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty file")
	}
}

func TestInMemoryWriter_Concurrent(t *testing.T) {
	w := NewInMemoryWriter()
	al := NewAuditLogger(w)

	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 20; j++ {
				ctx := context.Background()
				ctx, callCtx := a2asrv.NewCallContext(ctx, nil)
				ctx, _, _ = al.Before(ctx, callCtx, &a2asrv.Request{})
				_ = al.After(ctx, callCtx, &a2asrv.Response{})
			}
			done <- true
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}

	if w.Count() != 200 {
		t.Errorf("expected 200 events, got %d", w.Count())
	}
}
