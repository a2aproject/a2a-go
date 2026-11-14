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

package taskexec

import (
	"context"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/internal/eventpipe"
	"github.com/a2aproject/a2a-go/v2/internal/testutil"
)

type handoffProbeExecutor struct {
	*testProcessor
	start, interrupt, cancelDone, abort chan struct{}
	id                                  a2a.TaskID
}

func (e *handoffProbeExecutor) Execute(ctx context.Context, w eventpipe.Writer) error {
	close(e.start)
	<-e.interrupt
	if err := w.Write(ctx, &a2a.Task{ID: e.id, Status: a2a.TaskStatus{State: a2a.TaskStateInputRequired}}); err != nil {
		return err
	}
	select {
	case <-e.cancelDone:
	case <-e.abort:
	}
	return nil
}

type handoffProbeCanceler struct {
	*testProcessor
	start, proceed, done chan struct{}
	id                   a2a.TaskID
}

func (c *handoffProbeCanceler) Cancel(ctx context.Context, w eventpipe.Writer) error {
	close(c.start)
	<-c.proceed
	defer close(c.done)
	return w.Write(ctx, &a2a.Task{ID: c.id, Status: a2a.TaskStatus{State: a2a.TaskStateCanceled}})
}
func TestManager_CancelAndInterruptedExecutionDoNotDeadlock(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	id := a2a.NewTaskID()
	done, abort := make(chan struct{}), make(chan struct{})
	executor := &handoffProbeExecutor{testProcessor: &testProcessor{nextEventTerminal: true}, start: make(chan struct{}), interrupt: make(chan struct{}), cancelDone: done, abort: abort, id: id}
	canceler := &handoffProbeCanceler{testProcessor: &testProcessor{nextEventTerminal: true}, start: make(chan struct{}), proceed: make(chan struct{}), done: done, id: id}
	f := &testFactory{
		CreateExecutorFn: func(context.Context, a2a.TaskID, *a2a.SendMessageRequest) (Executor, Processor, Cleaner, error) {
			return executor, executor, executor, nil
		},
		CreateCancelerFn: func(context.Context, *a2a.CancelTaskRequest) (Canceler, Processor, Cleaner, error) {
			return canceler, canceler, canceler, nil
		},
	}
	m := NewLocalManager(LocalManagerConfig{Factory: f, TaskStore: testutil.NewTestTaskStore()})
	msg := a2a.NewMessage(a2a.MessageRoleUser)
	msg.TaskID = id
	sub, err := m.Execute(ctx, &a2a.SendMessageRequest{Message: msg})
	if err != nil {
		t.Fatal(err)
	}
	events, errs := consumeEvents(t, sub)
	<-executor.start
	result := make(chan error, 1)
	go func() { _, err := m.Cancel(ctx, &a2a.CancelTaskRequest{ID: id}); result <- err }()
	<-canceler.start
	close(executor.interrupt)
	<-events
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	close(canceler.proceed)
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("Cancel() error = %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		close(abort)
		<-result
		t.Fatal("Cancel() deadlocked while the executor waited for Cancel to return")
	}
}
