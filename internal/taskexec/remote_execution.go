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
	"fmt"
	"iter"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
	"github.com/a2aproject/a2a-go/internal/taskupdate"
)

type remoteExecution struct {
	tid    a2a.TaskID
	store  TaskStore
	qm     eventqueue.Manager
	result *promise
}

var _ Execution = (*remoteExecution)(nil)

func newRemoteExecution(qm eventqueue.Manager, store TaskStore, tid a2a.TaskID) *remoteExecution {
	return &remoteExecution{tid: tid, qm: qm, store: store, result: newPromise()}
}

func (e *remoteExecution) TaskID() a2a.TaskID {
	return e.tid
}

func (e *remoteExecution) Events(ctx context.Context) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		if e.finished() {
			return
		}

		yieldErr := func(err error) {
			e.result.setError(err)
			yield(nil, err)
		}

		subscription, err := e.newSubscription(ctx)
		if err != nil {
			yieldErr(err)
			return
		}

		for event, err := range subscription.Events(ctx) {
			if err != nil {
				yieldErr(err)
				return
			}

			if taskupdate.IsFinal(event) {
				defer e.result.signalDone()

				if result, ok := event.(a2a.SendMessageResult); ok {
					e.result.setValue(result)
					yield(event, nil)
					return
				}

				task, _, err := e.store.Get(ctx, e.tid)
				if err != nil {
					yieldErr(fmt.Errorf("failed to load execution result: %w", err))
					return
				}

				// TODO: for resubscription case when task is in input required we should stay subscribed to events
				if !task.Status.State.Terminal() && task.Status.State != a2a.TaskStateInputRequired {
					e.result.setError(fmt.Errorf("execution finished in unexpected task state: %s", task.Status.State))
				} else {
					e.result.setValue(task)
				}
				yield(event, nil)
				return
			}

			if !yield(event, nil) {
				return
			}
		}
	}
}

func (e *remoteExecution) Result(ctx context.Context) (a2a.SendMessageResult, error) {
	if e.finished() {
		return e.result.value, e.result.err
	}
	for _, err := range e.Events(ctx) {
		if err != nil {
			return nil, fmt.Errorf("failed waiting for terminal event: %w", err)
		}
	}
	return e.result.value, e.result.err
}

func (e *remoteExecution) newSubscription(ctx context.Context) (Subscription, error) {
	queue, err := e.qm.GetOrCreate(ctx, e.tid)
	if err != nil {
		return nil, err
	}
	return newRemoteSubscription(queue, e.store, e.tid), nil
}

func (e *remoteExecution) finished() bool {
	return e.result.value != nil || e.result.err != nil
}
