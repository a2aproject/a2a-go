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
	"errors"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/internal/eventpipe"
	"github.com/google/go-cmp/cmp"
)

type handoffResultProcessor struct {
	result a2a.SendMessageResult
	cause  error
}

func (p *handoffResultProcessor) Process(context.Context, a2a.Event) (*ProcessorResult, error) {
	return &ProcessorResult{ExecutionResult: p.result, ExecutionFailureCause: p.cause}, nil
}

func (p *handoffResultProcessor) ProcessError(_ context.Context, err error) (a2a.SendMessageResult, error) {
	return nil, err
}

func TestManager_PendingCancelResultOverridesFailureCause(t *testing.T) {
	t.Parallel()
	task := &a2a.Task{ID: a2a.NewTaskID(), Status: a2a.TaskStatus{State: a2a.TaskStateFailed}}
	processor := &handoffResultProcessor{result: task, cause: errors.New("processor failure")}
	factory := &testFactory{CreateCancelerFn: func(context.Context, *a2a.CancelTaskRequest) (Canceler, Processor, Cleaner, error) {
		return nil, processor, nil, nil
	}}
	manager := NewLocalManager(LocalManagerConfig{Factory: factory}).(*localManager)
	got, err := manager.processPendingCancelEvents(t.Context(), &a2a.CancelTaskRequest{ID: task.ID}, []a2a.Event{task})
	if err != nil {
		t.Fatalf("processPendingCancelEvents() error = %v, want nil", err)
	}
	if diff := cmp.Diff(task, got); diff != "" {
		t.Fatalf("processPendingCancelEvents() wrong result (-want +got) diff = %s", diff)
	}
}

func TestCancelHandoffWriter_OnlyReplaysUnread(t *testing.T) {
	t.Parallel()
	run := newLocalExecution(nil, nil, a2a.NewTaskID(), nil)
	writer := &cancelHandoffWriter{run: run}
	first := &a2a.Task{ID: run.tid, Status: a2a.TaskStatus{State: a2a.TaskStateWorking}}
	second := &a2a.Task{ID: run.tid, Status: a2a.TaskStatus{State: a2a.TaskStateCanceled}}
	if err := writer.Write(t.Context(), first); err != nil {
		t.Fatal(err)
	}
	if _, err := run.pipe.Reader.Read(t.Context()); err != nil {
		t.Fatal(err)
	}
	close(run.consumerDone)
	if err := writer.Write(t.Context(), second); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff([]a2a.Event{second}, writer.pending()); diff != "" {
		t.Fatalf("pending diff (-want +got): %s", diff)
	}
}

func TestCancelHandoffWriter_ConsumerStopsWithFullQueue(t *testing.T) {
	t.Parallel()
	run := newLocalExecution(nil, nil, a2a.NewTaskID(), nil)
	run.pipe = eventpipe.NewLocal(eventpipe.WithBufferSize(1))
	event := &a2a.Task{ID: run.tid, Status: a2a.TaskStatus{State: a2a.TaskStateWorking}}
	if err := run.pipe.Writer.Write(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	close(run.consumerDone)
	writer := &cancelHandoffWriter{run: run}
	if err := writer.Write(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	if got := len(writer.pending()); got != 1 {
		t.Fatalf("pending count = %d, want 1", got)
	}
}
