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
	ExecuteFn func(context.Context, *a2asrv.ExecutorContext, eventqueue.Queue) error
	CancelFn  func(context.Context, *a2asrv.ExecutorContext, eventqueue.Queue) error
}

var _ a2asrv.AgentExecutor = (*TestAgentExecutor)(nil)

func FromFunction(fn func(context.Context, *a2asrv.ExecutorContext, eventqueue.Queue) error) *TestAgentExecutor {
	return &TestAgentExecutor{ExecuteFn: fn}
}

func FromEventGenerator(generator func(execCtx *a2asrv.ExecutorContext) []a2a.Event) *TestAgentExecutor {
	var exec *TestAgentExecutor
	exec = &TestAgentExecutor{
		Emitted: []a2a.Event{},
		ExecuteFn: func(ctx context.Context, execCtx *a2asrv.ExecutorContext, q eventqueue.Queue) error {
			for _, ev := range generator(execCtx) {
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

func (e *TestAgentExecutor) Execute(ctx context.Context, execCtx *a2asrv.ExecutorContext, q eventqueue.Queue) error {
	if e.ExecuteFn != nil {
		return e.ExecuteFn(ctx, execCtx, q)
	}
	return nil
}

func (e *TestAgentExecutor) Cancel(ctx context.Context, execCtx *a2asrv.ExecutorContext, q eventqueue.Queue) error {
	if e.CancelFn != nil {
		return e.CancelFn(ctx, execCtx, q)
	}
	return nil
}
