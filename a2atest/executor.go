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

package a2atest

import (
	"context"
	"iter"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

// Executor is a test agent executor that can be controlled via callbacks.
// It is a convenience alias for creating simple mock executors inline.
type Executor struct {
	ExecuteFn func(context.Context, *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error]
	CancelFn  func(context.Context, *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error]
}

var _ a2asrv.AgentExecutor = (*Executor)(nil)

// Execute implements [a2asrv.AgentExecutor].
func (e *Executor) Execute(ctx context.Context, ec *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	if e.ExecuteFn != nil {
		return e.ExecuteFn(ctx, ec)
	}
	return func(yield func(a2a.Event, error) bool) {}
}

// Cancel implements [a2asrv.AgentExecutor].
func (e *Executor) Cancel(ctx context.Context, ec *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	if e.CancelFn != nil {
		return e.CancelFn(ctx, ec)
	}
	return func(yield func(a2a.Event, error) bool) {}
}

// ExecutorFromFunction creates an [Executor] from a function.
func ExecutorFromFunction(fn func(ctx context.Context, ec *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error]) *Executor {
	return &Executor{ExecuteFn: fn}
}

// ExecutorFromEvents creates an [Executor] that yields the given events in order.
func ExecutorFromEvents(events ...a2a.Event) *Executor {
	return ExecutorFromFunction(func(ctx context.Context, ec *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
		return func(yield func(a2a.Event, error) bool) {
			for _, ev := range events {
				if !yield(ev, nil) {
					return
				}
			}
		}
	})
}
