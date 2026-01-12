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
	"fmt"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
)

type SUTAgentExecutor struct{}

func (c *SUTAgentExecutor) Execute(ctx context.Context, reqCtx *a2asrv.RequestContext, q eventqueue.Queue) error {
	task := reqCtx.StoredTask

	if task == nil {
		event := a2a.NewStatusUpdateEvent(reqCtx, a2a.TaskStateSubmitted, nil)
		if err := q.Write(ctx, event); err != nil {
			return fmt.Errorf("failed to write state submitted: %w", err)
		}
	}

	event := a2a.NewStatusUpdateEvent(reqCtx, a2a.TaskStateWorking, nil)
	if err := q.Write(ctx, event); err != nil {
		return fmt.Errorf("failed to write state working: %w", err)
	}

	event = a2a.NewStatusUpdateEvent(reqCtx, a2a.TaskStateCompleted, nil)
	event.Final = true
	if err := q.Write(ctx, event); err != nil {
		return fmt.Errorf("failed to write state completed: %w", err)
	}

	return nil
}

func (c *SUTAgentExecutor) Cancel(ctx context.Context, reqCtx *a2asrv.RequestContext, q eventqueue.Queue) error {
	task := reqCtx.StoredTask
	if task == nil {
		return a2a.ErrTaskNotFound
	}
	if task.Status.State == a2a.TaskStateCanceled || task.Status.State == a2a.TaskStateCompleted || task.Status.State == a2a.TaskStateFailed {
		return a2a.ErrTaskNotCancelable
	}
	event := a2a.NewStatusUpdateEvent(reqCtx, a2a.TaskStateCanceled, nil)
	event.Final = true
	if err := q.Write(ctx, event); err != nil {
		return fmt.Errorf("failed to write state canceled: %w", err)
	}
	return nil
}

func newCustomAgentExecutor() a2asrv.AgentExecutor {
	return &SUTAgentExecutor{}
}
