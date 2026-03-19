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

package testexecutor

import (
	"context"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
)

type TestAgentExecutor struct {
	Emitted   []a2a.Event
	ExecuteFn func(context.Context, *a2asrv.RequestContext, eventqueue.Queue) error
	CleanupFn func(context.Context, *a2asrv.RequestContext, a2a.SendMessageResult, error)
	CancelFn  func(context.Context, *a2asrv.RequestContext, eventqueue.Queue) error
}

var _ a2asrv.AgentExecutor = (*TestAgentExecutor)(nil)

func (e *TestAgentExecutor) Execute(ctx context.Context, reqCtx *a2asrv.RequestContext, q eventqueue.Queue) error {
	if e.ExecuteFn != nil {
		return e.ExecuteFn(ctx, reqCtx, q)
	}
	return nil
}

func (e *TestAgentExecutor) Cleanup(ctx context.Context, reqCtx *a2asrv.RequestContext, result a2a.SendMessageResult, err error) {
	if e.CleanupFn != nil {
		e.CleanupFn(ctx, reqCtx, result, err)
	}
}

func (e *TestAgentExecutor) Cancel(ctx context.Context, reqCtx *a2asrv.RequestContext, q eventqueue.Queue) error {
	if e.CancelFn != nil {
		return e.CancelFn(ctx, reqCtx, q)
	}
	return nil
}

func FromFunction(fn func(context.Context, *a2asrv.RequestContext, eventqueue.Queue) error) *TestAgentExecutor {
	return &TestAgentExecutor{ExecuteFn: fn}
}

func FromEventGenerator(generator func(reqCtx *a2asrv.RequestContext) []a2a.Event) *TestAgentExecutor {
	var exec *TestAgentExecutor
	exec = &TestAgentExecutor{
		Emitted: []a2a.Event{},
		ExecuteFn: func(ctx context.Context, reqCtx *a2asrv.RequestContext, q eventqueue.Queue) error {
			for _, ev := range generator(reqCtx) {
				if err := q.Write(ctx, ev); err != nil {
					return err
				}
				exec.Emitted = append(exec.Emitted, ev)
			}
			return nil
		},
	}
	return exec
}

type ControlChannels struct {
	ReqCtx         <-chan *a2asrv.RequestContext
	ExecEvent      chan<- a2a.Event
	CancelCalled   <-chan struct{}
	ContinueCancel chan<- struct{}
}

func NewWithControlChannels() (*TestAgentExecutor, *ControlChannels) {
	reqCtxChan, eventsChan := make(chan *a2asrv.RequestContext, 1), make(chan a2a.Event, 1)
	cancelCalledChan, continueCancelChan := make(chan struct{}, 1), make(chan struct{}, 1)
	var executor *TestAgentExecutor
	executor = &TestAgentExecutor{
		Emitted: []a2a.Event{},
		ExecuteFn: func(ctx context.Context, reqCtx *a2asrv.RequestContext, q eventqueue.Queue) error {
			reqCtxChan <- reqCtx
			for ev := range eventsChan {
				if err := q.Write(ctx, ev); err != nil {
					return err
				}
				executor.Emitted = append(executor.Emitted, ev)
			}
			return nil
		},
		CancelFn: func(ctx context.Context, reqCtx *a2asrv.RequestContext, q eventqueue.Queue) error {
			cancelCalledChan <- struct{}{}
			<-continueCancelChan
			event := a2a.NewStatusUpdateEvent(reqCtx, a2a.TaskStateCanceled, nil)
			event.Final = true
			return q.Write(ctx, event)
		},
	}
	return executor, &ControlChannels{
		ReqCtx:         reqCtxChan,
		ExecEvent:      eventsChan,
		CancelCalled:   cancelCalledChan,
		ContinueCancel: continueCancelChan,
	}
}

func NewCanceler() *TestAgentExecutor {
	return &TestAgentExecutor{
		CancelFn: func(ctx context.Context, reqCtx *a2asrv.RequestContext, q eventqueue.Queue) error {
			event := a2a.NewStatusUpdateEvent(reqCtx, a2a.TaskStateCanceled, nil)
			event.Final = true
			return q.Write(ctx, event)
		},
	}
}
