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

// Package testexecutor provides mock implementations for agent executor for testing.
package testexecutor

import (
	"context"
	"iter"
	"sync"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

// TestAgentExecutor is a mock of [a2asrv.AgentExecutor].
type TestAgentExecutor struct {
	mu      sync.Mutex
	emitted []a2a.Event

	ExecuteFn func(context.Context, *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error]
	CancelFn  func(context.Context, *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error]
}

var _ a2asrv.AgentExecutor = (*TestAgentExecutor)(nil)

// Emitted provides access to events emitted by [TestAgentExecutor] guarded with a mutex.
func (e *TestAgentExecutor) Emitted() []a2a.Event {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.emitted
}

func (e *TestAgentExecutor) record(event a2a.Event) {
	e.mu.Lock()
	e.emitted = append(e.emitted, event)
	e.mu.Unlock()
}

// Execute implements [a2asrv.AgentExecutor] interface.
func (e *TestAgentExecutor) Execute(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	if e.ExecuteFn != nil {
		return e.ExecuteFn(ctx, execCtx)
	}
	return func(yield func(a2a.Event, error) bool) {}
}

// Cancel implements [a2asrv.AgentExecutor] interface.
func (e *TestAgentExecutor) Cancel(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	if e.CancelFn != nil {
		return e.CancelFn(ctx, execCtx)
	}
	return func(yield func(a2a.Event, error) bool) {}
}

// FromFunction creates a [TestAgentExecutor] from a function.
func FromFunction(fn func(ctx context.Context, ec *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error]) *TestAgentExecutor {
	return &TestAgentExecutor{ExecuteFn: fn}
}

// FromEventGenerator creates a [TestAgentExecutor] that emits events from a generator.
func FromEventGenerator(generator func(execCtx *a2asrv.ExecutorContext) []a2a.Event) *TestAgentExecutor {
	var exec *TestAgentExecutor
	exec = &TestAgentExecutor{
		emitted: []a2a.Event{},
		ExecuteFn: func(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
			return func(yield func(a2a.Event, error) bool) {
				for _, ev := range generator(execCtx) {
					exec.record(ev)

					if !yield(ev, nil) {
						return
					}
				}
			}
		},
	}
	return exec
}

// ControlChannels is a group of channels for controlling [TestAgentExecutor] behavior.
type ControlChannels struct {
	ReqCtx         <-chan *a2asrv.ExecutorContext
	ExecEvent      chan<- a2a.Event
	CancelCalled   <-chan struct{}
	ContinueCancel chan<- struct{}
}

// NewWithControlChannels creates a [TestAgentExecutor] controllable through the returned channels.
func NewWithControlChannels() (*TestAgentExecutor, *ControlChannels) {
	reqCtxChan, eventsChan := make(chan *a2asrv.ExecutorContext, 1), make(chan a2a.Event, 1)
	cancelCalledChan, continueCancelChan := make(chan struct{}, 1), make(chan struct{}, 1)
	var executor *TestAgentExecutor
	executor = &TestAgentExecutor{
		emitted: []a2a.Event{},
		ExecuteFn: func(ctx context.Context, reqCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
			return func(yield func(a2a.Event, error) bool) {
				reqCtxChan <- reqCtx
				for ev := range eventsChan {
					executor.record(ev)
					if !yield(ev, nil) {
						return
					}
				}
			}
		},
		CancelFn: func(ctx context.Context, reqCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
			return func(yield func(a2a.Event, error) bool) {
				cancelCalledChan <- struct{}{}
				<-continueCancelChan
				yield(a2a.NewStatusUpdateEvent(reqCtx, a2a.TaskStateCanceled, nil), nil)
			}
		},
	}
	return executor, &ControlChannels{
		ReqCtx:         reqCtxChan,
		ExecEvent:      eventsChan,
		CancelCalled:   cancelCalledChan,
		ContinueCancel: continueCancelChan,
	}
}

// NewCanceler creates a [TestAgentExecutor] which emits a single [a2a.TaskStateCanceled] even on cancel.
func NewCanceler() *TestAgentExecutor {
	return &TestAgentExecutor{
		CancelFn: func(ctx context.Context, reqCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
			return func(yield func(a2a.Event, error) bool) {
				yield(a2a.NewStatusUpdateEvent(reqCtx, a2a.TaskStateCanceled, nil), nil)
			}
		},
	}
}
