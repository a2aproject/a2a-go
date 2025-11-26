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

package eventpipe

import (
	"errors"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
	"github.com/google/go-cmp/cmp"
)

func mustWrite(t *testing.T, pipe *Local, event a2a.Event) {
	t.Helper()
	if err := pipe.Writer.Write(t.Context(), event); err != nil {
		t.Fatalf("pipe.Writer.Writer() error = %v", err)
	}
}

func mustClose(t *testing.T, pipe *Local) {
	t.Helper()
	if err := pipe.Close(); err != nil {
		t.Fatalf("pipe.Close() error = %v", err)
	}
}

func mustRead(t *testing.T, pipe *Local) a2a.Event {
	t.Helper()
	res, err := pipe.Reader.Read(t.Context())
	if err != nil {
		t.Fatalf("pipe.Reader.Read() error = %v", err)
	}
	return res
}

func TestLocalPipe_WriteRead(t *testing.T) {
	t.Parallel()
	pipe := NewLocal()
	want := &a2a.Message{ID: a2a.NewMessageID()}
	mustWrite(t, pipe, want)
	got := mustRead(t, pipe)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("Read() wrong result (+got,-want) diff = %s", diff)
	}
}

func TestLocalPipe_WriteCloseRead(t *testing.T) {
	t.Parallel()
	pipe := NewLocal()
	want := &a2a.Message{ID: a2a.NewMessageID()}
	mustWrite(t, pipe, want)
	mustClose(t, pipe)
	got := mustRead(t, pipe)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("Read() wrong result (+got,-want) diff = %s", diff)
	}
}

func TestLocalPipe_CloseWrite(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	pipe := NewLocal()
	event := &a2a.Message{ID: a2a.NewMessageID()}
	mustClose(t, pipe)
	if err := pipe.Writer.Write(ctx, event); !errors.Is(err, eventqueue.ErrQueueClosed) {
		t.Fatalf("pipe.Writer.Write() error = %v, want %v", err, eventqueue.ErrQueueClosed)
	}
}

func TestLocalPipe_ReadBlocksUntilWritten(t *testing.T) {
	t.Parallel()
	pipe := NewLocal()

	completed := make(chan struct{})
	go func() {
		mustRead(t, pipe)
		close(completed)
	}()

	select {
	case <-completed:
		t.Fatal("method should be blocking")
	case <-time.After(15 * time.Millisecond):
		mustWrite(t, pipe, &a2a.Message{ID: "test"})
	}
	<-completed
}

func TestLocalPipe_WriteBlocksWhenQueueFull(t *testing.T) {
	t.Parallel()
	pipe := NewLocal(WithBufferSize(1))
	mustWrite(t, pipe, &a2a.Message{ID: a2a.NewMessageID()})

	completed := make(chan struct{})
	go func() {
		mustWrite(t, pipe, &a2a.Message{ID: a2a.NewMessageID()})
		close(completed)
	}()

	select {
	case <-completed:
		t.Fatal("method should be blocking")
	case <-time.After(15 * time.Millisecond):
		_ = mustRead(t, pipe)
	}
	<-completed
}

func TestLocalPipeWriter_ReadAndCloseFail(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	pipe := NewLocal()
	if err := pipe.Writer.Close(); err == nil {
		t.Fatal("pipe.Writer.Close() error = nil, want to fail")
	}
	if _, err := pipe.Writer.Read(ctx); err == nil {
		t.Fatal("pipe.Writer.Read() error = nil, want to fail")
	}
}
