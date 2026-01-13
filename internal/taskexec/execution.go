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
	"iter"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
	"github.com/a2aproject/a2a-go/internal/eventpipe"
)

type Execution interface {
	Events(ctx context.Context) iter.Seq2[a2a.Event, error]

	Result(ctx context.Context) (a2a.SendMessageResult, error)
}

// Execution represents an agent invocation in a context of the referenced task.
// If the invocation was finished Result() will resolve immediately, otherwise it will block.
type localExecution struct {
	tid    a2a.TaskID
	params *a2a.MessageSendParams
	result *promise

	pipe         *eventpipe.Local
	queueManager eventqueue.Manager
}

var _ Execution = (*localExecution)(nil)

// Not exported, because Executions are created by Executor.
func newLocalExecution(qm eventqueue.Manager, tid a2a.TaskID, params *a2a.MessageSendParams) *localExecution {
	return &localExecution{
		tid:          tid,
		params:       params,
		queueManager: qm,
		pipe:         eventpipe.NewLocal(),
		result:       newPromise(),
	}
}

// Events subscribes to the events an agent is producing during an active Execution.
// If the Execution was finished the sequence will be empty.
func (e *localExecution) Events(ctx context.Context) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		queue, ok := e.queueManager.Get(ctx, e.tid)
		if !ok { // execution finished
			return
		}

		subscription := newLocalSubscription(e, queue)
		for event, err := range subscription.Events(ctx) {
			if err != nil {
				yield(nil, err)
				return
			}
			if !yield(event, nil) {
				return
			}
		}
	}
}

// Result resolves immediately for the finished Execution or blocks until it is complete.
func (e *localExecution) Result(ctx context.Context) (a2a.SendMessageResult, error) {
	return e.result.wait(ctx)
}
