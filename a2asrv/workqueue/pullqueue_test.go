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
	"fmt"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv/limiter"
)

func TestPullQueue_SuccessfulExecution(t *testing.T) {
	t.Parallel()

	tid := a2a.NewTaskID()
	wantResult := &a2a.Task{ID: tid, Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}}

	msg := newTestMsg(&Payload{Type: PayloadTypeExecute, TaskID: tid})
	queue := NewPullQueue(newFixedReader([]Message{msg}), nil)

	queue.RegisterHandler(HandlerConfig{}, newStaticResultHandler(wantResult, nil))

	if res := msg.resolution(); res != msgResultCompleted {
		t.Fatalf("msg.result = %q, want %q", res, msgResultCompleted)
	}
}

func TestPullQueue_HandlerErrorReturnsMessage(t *testing.T) {
	t.Parallel()

	msg := newTestMsg(&Payload{Type: PayloadTypeExecute, TaskID: a2a.NewTaskID()})
	queue := NewPullQueue(newFixedReader([]Message{msg}), nil)

	queue.RegisterHandler(HandlerConfig{}, newStaticResultHandler(nil, fmt.Errorf("handler failed")))

	if res := msg.resolution(); res != msgResultReturned {
		t.Fatalf("msg.result = %q, want %q", res, msgResultReturned)
	}
}

func TestPullQueue_ErrQueueClosedStopsLoop(t *testing.T) {
	t.Parallel()
	queueShutdown := make(chan struct{})
	queue := pullQueue{
		ReadWriter: newFixedReader([]Message{}),
		onShutdown: func() { close(queueShutdown) },
	}
	queue.RegisterHandler(HandlerConfig{}, newStaticResultHandler(&a2a.Task{}, nil))
	<-queueShutdown
}

func TestPullQueue_MalformedPayloadCompletesAndContinues(t *testing.T) {
	t.Parallel()

	malformedMsg := newTestMsg(&Payload{Type: "bad"})
	goodMsg := newTestMsg(&Payload{Type: PayloadTypeExecute, TaskID: a2a.NewTaskID()})

	queue := NewPullQueue(newFixedReader([]Message{malformedMsg, goodMsg}), nil)
	queue.RegisterHandler(HandlerConfig{}, newStaticResultHandler(&a2a.Task{}, nil))

	if res := malformedMsg.resolution(); res != msgResultCompleted {
		t.Errorf("malformedMsg.resolution = %q, want %q", res, msgResultCompleted)
	}
	if res := goodMsg.resolution(); res != msgResultCompleted {
		t.Errorf("goodMsg.resolution = %q, want %q", res, msgResultCompleted)
	}
}

func TestPullQueue_HandlerMalformedPayloadCompletesMessage(t *testing.T) {
	t.Parallel()

	msg := newTestMsg(&Payload{Type: PayloadTypeExecute, TaskID: a2a.NewTaskID()})
	queue := NewPullQueue(newFixedReader([]Message{msg}), nil)

	queue.RegisterHandler(HandlerConfig{}, newStaticResultHandler(nil, fmt.Errorf("bad input: %w", ErrMalformedPayload)))

	if res := msg.resolution(); res != msgResultCompleted {
		t.Fatalf("msg.resolution = %q, want %q (ErrMalformedPayload should complete, not return)", res, msgResultCompleted)
	}
}

func TestPullQueue_ReadErrorTriggersBackoff(t *testing.T) {
	t.Parallel()

	hadCalls, wantCalls := 0, 3
	rw := &testReadWriter{
		readFunc: func(ctx context.Context) (Message, error) {
			if hadCalls >= wantCalls {
				return nil, ErrQueueClosed
			}
			hadCalls++
			return nil, fmt.Errorf("transient error")
		},
	}

	retryPolicy := newTestRetryPolicy(wantCalls)
	queue := NewPullQueue(rw, &PullQueueConfig{ReadRetry: retryPolicy})
	queue.RegisterHandler(HandlerConfig{}, newStaticResultHandler(nil, nil))

	for i := range wantCalls {
		if got := <-retryPolicy.attemptValueChan; got != i {
			t.Fatalf("retryAttemptsChan[%d] = %d, want %d", i, got, i)
		}
	}
}

func TestPullQueue_ReadAttemptResetsOnSuccess(t *testing.T) {
	t.Parallel()

	msg := newTestMsg(&Payload{Type: PayloadTypeExecute, TaskID: a2a.NewTaskID()})
	readResults := []readResult{
		{err: fmt.Errorf("transient error")},
		{err: fmt.Errorf("transient error")},
		{err: fmt.Errorf("transient error")},
		{msg: msg, err: nil},
		{err: fmt.Errorf("transient error")},
		{err: ErrQueueClosed},
	}

	wantAttempts := []int{0, 1, 2, 0}
	retryPolicy := newTestRetryPolicy(4)
	queue := NewPullQueue(newFixedResultReader(readResults), &PullQueueConfig{
		ReadRetry: retryPolicy,
	})

	queue.RegisterHandler(HandlerConfig{}, newStaticResultHandler(&a2a.Task{}, nil))
	if res := msg.resolution(); res != msgResultCompleted {
		t.Errorf("msg.resolution = %q, want %q", res, msgResultCompleted)
	}

	for i, want := range wantAttempts {
		if got := <-retryPolicy.attemptValueChan; got != want {
			t.Fatalf("retryAttemptsChan[%d] = %d, want %d", i, got, want)
		}
	}
}

func TestPullQueue_ConcurrencyLimit(t *testing.T) {
	t.Parallel()

	executions := make(chan chan struct{}, 2)
	msg1, msg2 := newTestMsg(&Payload{Type: PayloadTypeExecute}), newTestMsg(&Payload{Type: PayloadTypeExecute})
	queue := NewPullQueue(newFixedReader([]Message{msg1, msg2}), nil)
	queue.RegisterHandler(HandlerConfig{
		Limiter: limiter.ConcurrencyConfig{MaxExecutions: 1},
	}, func(ctx context.Context, p *Payload) (a2a.SendMessageResult, error) {
		finishChan := make(chan struct{})
		executions <- finishChan
		<-finishChan
		return &a2a.Task{ID: p.TaskID}, nil
	})

	// Verify the second execution is not started until the first one is finished
	firstExec := <-executions
	go func() {
		secondExec := <-executions
		close(secondExec)
	}()
	time.Sleep(10 * time.Millisecond)

	select {
	case <-msg2.resChan:
		t.Fatalf("msg2 was processed before msg1")
	default:
	}
	close(firstExec)

	// Verify both executions finished
	for _, msg := range []*testMessage{msg1, msg2} {
		if res := msg.resolution(); res != msgResultCompleted {
			t.Errorf("msg.resolution = %q, want %q", res, msgResultCompleted)
		}
	}
}

func TestPullQueue_HeartbeaterAttached(t *testing.T) {
	t.Parallel()

	var gotHeartbeater bool
	msg := newTestMsg(&Payload{Type: PayloadTypeExecute, TaskID: a2a.NewTaskID()})
	queue := NewPullQueue(newFixedReader([]Message{msg}), nil)
	queue.RegisterHandler(HandlerConfig{}, func(ctx context.Context, p *Payload) (a2a.SendMessageResult, error) {
		_, gotHeartbeater = HeartbeaterFrom(ctx)
		return &a2a.Task{ID: p.TaskID}, nil
	})

	if _ = msg.resolution(); !gotHeartbeater {
		t.Fatalf("heartbeater was not attached to context")
	}
}

func TestPullQueue_BeforeExecCallbackContexModification(t *testing.T) {
	t.Parallel()

	type contextKeyType struct{}

	wantVal := "test-value"
	msg := newTestMsg(&Payload{Type: PayloadTypeExecute, TaskID: a2a.NewTaskID()})

	var gotVal string
	queue := NewPullQueue(newFixedReader([]Message{msg}), &PullQueueConfig{
		BeforeExecutionCallback: func(ctx context.Context, m Message) (context.Context, error) {
			return context.WithValue(ctx, contextKeyType{}, wantVal), nil
		},
	})
	queue.RegisterHandler(HandlerConfig{}, func(ctx context.Context, p *Payload) (a2a.SendMessageResult, error) {
		gotVal, _ = ctx.Value(contextKeyType{}).(string)
		return &a2a.Task{ID: p.TaskID}, nil
	})

	if res := msg.resolution(); res != msgResultCompleted {
		t.Fatalf("msg.resolution = %q, want %q", res, msgResultCompleted)
	}
	if gotVal != wantVal {
		t.Errorf("context value = %q, want %q", gotVal, wantVal)
	}
}

func TestPullQueue_BeforeExecCallbackAbortsExecution(t *testing.T) {
	t.Parallel()

	msg := newTestMsg(&Payload{Type: PayloadTypeExecute, TaskID: a2a.NewTaskID()})

	handlerCalled := false
	queue, queueShutdown := newTestQueue([]Message{msg})
	queue.config.BeforeExecutionCallback = func(ctx context.Context, m Message) (context.Context, error) {
		return ctx, fmt.Errorf("abort execution")
	}

	queue.RegisterHandler(HandlerConfig{}, func(ctx context.Context, p *Payload) (a2a.SendMessageResult, error) {
		handlerCalled = true
		return &a2a.Task{ID: p.TaskID}, nil
	})

	<-queueShutdown

	if handlerCalled {
		t.Errorf("handler was called despite before callback error")
	}

	select {
	case res := <-msg.resChan:
		t.Errorf("msg received resolution %q, want none", res)
	default:
	}
}

func TestPullQueue_AfterExecution_PreventMessageResolution(t *testing.T) {
	t.Parallel()

	msg := newTestMsg(&Payload{Type: PayloadTypeExecute, TaskID: a2a.NewTaskID()})

	handlerCalled := make(chan struct{})
	queue, queueShutdown := newTestQueue([]Message{msg})
	queue.config.AfterExecutionCallback = func(ctx context.Context, m Message, res a2a.SendMessageResult, err error) error {
		return fmt.Errorf("handled in callback")
	}

	queue.RegisterHandler(HandlerConfig{}, func(ctx context.Context, p *Payload) (a2a.SendMessageResult, error) {
		close(handlerCalled)
		return &a2a.Task{ID: p.TaskID}, nil
	})

	<-handlerCalled
	<-queueShutdown

	select {
	case res := <-msg.resChan:
		t.Errorf("msg received resolution %q, want none", res)
	default:
	}
}

func TestPullQueue_DrainOnShutdown(t *testing.T) {
	t.Parallel()

	msg1, msg2 := newTestMsg(&Payload{Type: PayloadTypeExecute}), newTestMsg(&Payload{Type: PayloadTypeExecute})
	queue, queueShutdown := newTestQueue([]Message{msg1, msg2})

	executions := make(chan chan struct{}, 2)
	queue.RegisterHandler(HandlerConfig{}, func(ctx context.Context, p *Payload) (a2a.SendMessageResult, error) {
		finishChan := make(chan struct{})
		executions <- finishChan
		<-finishChan
		return &a2a.Task{ID: p.TaskID}, nil
	})

	// Verify shutdown is not called before all executions complete
	firstExec := <-executions
	go func() {
		secondExec := <-executions
		close(secondExec)
	}()

	select {
	case <-msg1.resChan:
	case <-msg2.resChan:
	}

	time.Sleep(10 * time.Millisecond)
	select {
	case <-queueShutdown:
		t.Fatalf("shutdown was called, want all messages processed first")
	default:
	}

	close(firstExec)
	<-queueShutdown
}

type msgResolution string

const (
	msgResultCompleted msgResolution = "completed"
	msgResultReturned  msgResolution = "returned"
)

type testMessage struct {
	payload  *Payload
	resChan  chan msgResolution
	interval time.Duration
}

func newTestMsg(payload *Payload) *testMessage {
	return &testMessage{payload: payload, resChan: make(chan msgResolution, 1)}
}

func (m *testMessage) resolution() msgResolution {
	return <-m.resChan
}

func (m *testMessage) Payload() *Payload {
	return m.payload
}

func (m *testMessage) Complete(context.Context) error {
	m.resChan <- msgResultCompleted
	return nil
}

func (m *testMessage) Return(context.Context, error) error {
	m.resChan <- msgResultReturned
	return nil
}

func (m *testMessage) HeartbeatInterval() time.Duration {
	return m.interval
}

func (m *testMessage) Heartbeat(context.Context) error {
	return nil
}

func newFixedReader(msgs []Message) *testReadWriter {
	return &testReadWriter{
		readFunc: func(ctx context.Context) (Message, error) {
			if len(msgs) == 0 {
				return nil, ErrQueueClosed
			}
			msg := msgs[0]
			msgs = msgs[1:]
			return msg, nil
		},
	}
}

type readResult struct {
	msg Message
	err error
}

func newFixedResultReader(results []readResult) *testReadWriter {
	return &testReadWriter{
		readFunc: func(ctx context.Context) (Message, error) {
			if len(results) == 0 {
				return nil, ErrQueueClosed
			}
			result := results[0]
			results = results[1:]
			return result.msg, result.err
		},
	}
}

type testReadWriter struct {
	testWriter
	readFunc func(context.Context) (Message, error)
}

func (rw *testReadWriter) Read(ctx context.Context) (Message, error) {
	return rw.readFunc(ctx)
}

type testRetryPolicy struct {
	attemptValueChan chan int
}

func newTestRetryPolicy(chanSize int) *testRetryPolicy {
	readAttemptsChan := make(chan int, chanSize)
	return &testRetryPolicy{attemptValueChan: readAttemptsChan}
}

func (p *testRetryPolicy) NextDelay(attempt int) time.Duration {
	p.attemptValueChan <- attempt
	return time.Millisecond
}

func newTestQueue(messages []Message) (*pullQueue, chan struct{}) {
	queueShutdown := make(chan struct{})
	queue := &pullQueue{
		config:     &PullQueueConfig{},
		ReadWriter: newFixedReader(messages),
		onShutdown: func() { close(queueShutdown) },
	}
	return queue, queueShutdown
}
