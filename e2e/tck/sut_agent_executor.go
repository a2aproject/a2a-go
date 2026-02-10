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

package main

import (
	"context"
	"iter"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"
)

type SUTAgentExecutor struct{}

func (c *SUTAgentExecutor) Execute(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		task := execCtx.StoredTask

		if task == nil {
			if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateSubmitted, nil), nil) {
				return
			}
		}
		// Short delay to allow tests to see current state
		time.Sleep(1 * time.Second)
		if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateWorking, nil), nil) {
			return
		}
		time.Sleep(1 * time.Second)
		event := a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCompleted, nil)
		event.Final = true
		yield(event, nil)
	}
}

func (c *SUTAgentExecutor) Cancel(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		task := execCtx.StoredTask
		if task == nil {
			yield(nil, a2a.ErrTaskNotFound)
			return
		}

		event := a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCanceled, nil)
		event.Final = true
		yield(event, nil)
	}
}

func newCustomAgentExecutor() a2asrv.AgentExecutor {
	return &SUTAgentExecutor{}
}
