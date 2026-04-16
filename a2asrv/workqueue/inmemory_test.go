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
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv/limiter"
)

func TestInMemoryQueue_Write(t *testing.T) {
	t.Parallel()

	queue := NewInMemory(nil)
	tid := a2a.NewTaskID()
	payload := &Payload{Type: PayloadTypeExecute, TaskID: tid}

	gotPayload := make(chan *Payload, 1)
	queue.RegisterHandler(HandlerConfig{}, func(ctx context.Context, p *Payload) (a2a.SendMessageResult, error) {
		gotPayload <- p
		return &a2a.Task{ID: p.TaskID}, nil
	})

	gotID, err := queue.Write(t.Context(), payload)
	if err != nil {
		t.Fatalf("Write() error = %v, want nil", err)
	}
	if gotID != tid {
		t.Fatalf("Write() = %v, want %v", gotID, tid)
	}

	got := <-gotPayload
	if got != payload {
		t.Fatalf("handler received payload = %v, want %v", got, payload)
	}
}

func TestInMemoryQueue_LeaseTaken(t *testing.T) {
	testCases := []struct {
		firstCall  PayloadType
		secondCall PayloadType
		wantErr    bool
	}{
		{
			firstCall:  PayloadTypeExecute,
			secondCall: PayloadTypeExecute,
			wantErr:    true,
		},
		{
			firstCall:  PayloadTypeCancel,
			secondCall: PayloadTypeExecute,
			wantErr:    true,
		},
		{
			firstCall:  PayloadTypeCancel,
			secondCall: PayloadTypeCancel,
			wantErr:    true,
		},
		{
			firstCall:  PayloadTypeExecute,
			secondCall: PayloadTypeCancel,
			wantErr:    false,
		},
	}
	for _, tc := range testCases {
		name := string(tc.secondCall) + " after " + string(tc.firstCall)
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			queue := NewInMemory(nil)
			handlerBlock := make(chan struct{})
			queue.RegisterHandler(HandlerConfig{}, func(ctx context.Context, p *Payload) (a2a.SendMessageResult, error) {
				<-handlerBlock
				return &a2a.Task{ID: p.TaskID}, nil
			})

			tid := a2a.NewTaskID()
			if _, err := queue.Write(t.Context(), &Payload{Type: tc.firstCall, TaskID: tid}); err != nil {
				t.Fatalf("Write() error = %v, want nil", err)
			}
			_, err := queue.Write(t.Context(), &Payload{Type: tc.secondCall, TaskID: tid})
			if tc.wantErr && !errors.Is(err, ErrLeaseAlreadyTaken) {
				t.Fatalf("Write() error = %v, want %v", err, ErrLeaseAlreadyTaken)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("Write() error = %v, want nil", err)
			}
			close(handlerBlock)
		})
	}
}

func TestInMemoryQueue_ConcurrencyLimit(t *testing.T) {
	t.Parallel()

	queue := NewInMemory(nil)

	handlerStarted, handlerBlock := make(chan struct{}), make(chan struct{})
	queue.RegisterHandler(
		HandlerConfig{Limiter: limiter.ConcurrencyConfig{MaxExecutions: 1}},
		func(ctx context.Context, p *Payload) (a2a.SendMessageResult, error) {
			close(handlerStarted)
			<-handlerBlock
			return &a2a.Task{ID: p.TaskID}, nil
		},
	)

	if _, err := queue.Write(t.Context(), &Payload{Type: PayloadTypeExecute, TaskID: a2a.NewTaskID()}); err != nil {
		t.Fatalf("Write() error = %v, want nil", err)
	}
	<-handlerStarted

	_, err := queue.Write(t.Context(), &Payload{Type: PayloadTypeExecute, TaskID: a2a.NewTaskID()})
	if !errors.Is(err, ErrConcurrencyLimitExceeded) {
		t.Fatalf("Write() error = %v, want %v", err, ErrConcurrencyLimitExceeded)
	}
	close(handlerBlock)
}

func TestInMemoryQueue_ContextDetached(t *testing.T) {
	t.Parallel()

	queue := NewInMemory(nil)

	parentCtx, parentCancel := context.WithCancel(t.Context())
	defer parentCancel()

	parentCanceled, handlerCtx := make(chan struct{}), make(chan context.Context, 1)
	queue.RegisterHandler(HandlerConfig{}, func(ctx context.Context, p *Payload) (a2a.SendMessageResult, error) {
		<-parentCanceled
		handlerCtx <- ctx
		return &a2a.Task{ID: p.TaskID}, nil
	})

	if _, err := queue.Write(parentCtx, &Payload{Type: PayloadTypeExecute, TaskID: a2a.NewTaskID()}); err != nil {
		t.Fatalf("Write() error = %v, want nil", err)
	}

	parentCancel()
	close(parentCanceled)
	gotCtx := <-handlerCtx
	if err := gotCtx.Err(); err != nil {
		t.Fatalf("handler context error = %v, want nil (context should be detached from caller)", err)
	}
}

func TestInMemoryQueue_LeaseHeartbeaterAttached(t *testing.T) {
	t.Parallel()

	lm := &heartbeaterLeaseManager{inner: NewInMemoryLeaseManager()}
	queue := NewInMemory(&InMemoryQueueConfig{LeaseManager: lm})

	handlerCtx := make(chan context.Context, 1)
	queue.RegisterHandler(HandlerConfig{}, func(ctx context.Context, p *Payload) (a2a.SendMessageResult, error) {
		handlerCtx <- ctx
		return &a2a.Task{ID: p.TaskID}, nil
	})

	if _, err := queue.Write(t.Context(), &Payload{Type: PayloadTypeExecute, TaskID: a2a.NewTaskID()}); err != nil {
		t.Fatalf("Write() error = %v, want nil", err)
	}

	gotCtx := <-handlerCtx
	if _, ok := HeartbeaterFrom(gotCtx); !ok {
		t.Fatalf("HeartbeaterFrom() = _, false, want true")
	}
}

type heartbeaterLeaseManager struct{ inner LeaseManager }

func (m *heartbeaterLeaseManager) Acquire(ctx context.Context, p *Payload) (Lease, error) {
	lease, err := m.inner.Acquire(ctx, p)
	if err != nil {
		return nil, err
	}
	return &heartbeaterLease{Lease: lease}, nil
}

type heartbeaterLease struct {
	Lease
}

func (l *heartbeaterLease) HeartbeatInterval() time.Duration { return time.Second }
func (l *heartbeaterLease) Heartbeat(context.Context) error  { return nil }
