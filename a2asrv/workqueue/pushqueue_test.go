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

package workqueue

import (
	"context"
	"errors"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv/limiter"
)

func TestPushQueue_HandlerDelegation(t *testing.T) {
	t.Parallel()

	queue, handler := NewPushQueue(&testWriter{})

	wantResult := &a2a.Task{ID: a2a.NewTaskID(), Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}}
	queue.RegisterHandler(HandlerConfig{}, newStaticResultHandler(wantResult, nil))

	got, err := handler(t.Context(), &Payload{Type: PayloadTypeExecute, TaskID: wantResult.ID})
	if err != nil {
		t.Fatalf("handler() error = %v, want nil", err)
	}
	if got != wantResult {
		t.Fatalf("handler() = %v, want %v", got, wantResult)
	}
}

func TestPushQueue_ConcurrencyLimit(t *testing.T) {
	t.Parallel()

	writer := &testWriter{}
	queue, pushMsg := NewPushQueue(writer)

	called, done := make(chan struct{}), make(chan struct{})
	resChan := make(chan a2a.SendMessageResult, 1)
	queue.RegisterHandler(
		HandlerConfig{Limiter: limiter.ConcurrencyConfig{MaxExecutions: 1}},
		func(ctx context.Context, p *Payload) (a2a.SendMessageResult, error) {
			close(called)
			return <-resChan, nil
		},
	)

	go func() {
		if _, err := pushMsg(t.Context(), &Payload{Type: PayloadTypeExecute, TaskID: "t1"}); err != nil {
			t.Errorf("pushMsg() error = %v", err)
		}
		close(done)
	}()
	<-called

	if _, err := pushMsg(t.Context(), &Payload{Type: PayloadTypeExecute, TaskID: "t2"}); !errors.Is(err, ErrConcurrencyLimitExceeded) {
		t.Errorf("pushMsg() error = %v, want %v", err, ErrConcurrencyLimitExceeded)
	}
	resChan <- &a2a.Task{}
	<-done
}

func TestPushQueue_Write(t *testing.T) {
	t.Parallel()

	wantID := a2a.NewTaskID()
	writer := &testWriter{writeFunc: func(ctx context.Context, p *Payload) (a2a.TaskID, error) {
		return wantID, nil
	}}

	queue, _ := NewPushQueue(writer)
	gotID, err := queue.Write(t.Context(), &Payload{Type: PayloadTypeExecute})
	if err != nil {
		t.Fatalf("Write() error = %v, want nil", err)
	}
	if gotID != wantID {
		t.Fatalf("Write() = %v, want %v", gotID, wantID)
	}
}

type testWriter struct {
	writeFunc func(context.Context, *Payload) (a2a.TaskID, error)
}

func (w *testWriter) Write(ctx context.Context, p *Payload) (a2a.TaskID, error) {
	if w.writeFunc != nil {
		return w.writeFunc(ctx, p)
	}
	return p.TaskID, nil
}

func newStaticResultHandler(res a2a.SendMessageResult, err error) HandlerFn {
	return func(ctx context.Context, p *Payload) (a2a.SendMessageResult, error) {
		return res, err
	}
}
