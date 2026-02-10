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
	"fmt"
	"iter"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
	"github.com/a2aproject/a2a-go/internal/taskupdate"
	"github.com/a2aproject/a2a-go/log"
)

type localSubscription struct {
	execution *localExecution
	queue     eventqueue.Queue
	consumed  bool
}

var _ Subscription = (*localSubscription)(nil)

func newLocalSubscription(e *localExecution, q eventqueue.Queue) *localSubscription {
	return &localSubscription{execution: e, queue: q}
}

func (s *localSubscription) TaskID() a2a.TaskID {
	return s.execution.tid
}

func (s *localSubscription) Events(ctx context.Context) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		if s.consumed {
			yield(nil, fmt.Errorf("subscription already consumed"))
			return
		}
		s.consumed = true

		defer func() {
			if err := s.queue.Close(); err != nil {
				log.Warn(ctx, "subscription cancel failed", "error", err)
			}
		}()

		terminalReported := false
		for {
			msg, err := s.queue.Read(ctx)
			if errors.Is(err, eventqueue.ErrQueueClosed) {
				break
			}
			if err != nil {
				yield(nil, err)
				return
			}
			event := msg.Event
			terminalReported = taskupdate.IsFinal(event)
			if !yield(event, nil) {
				return
			}
		}

		// execution might not report the terminal event in case execution context.Context was canceled which
		// might happen if event producer panics.
		if result, err := s.execution.result.wait(ctx); !terminalReported || err != nil {
			yield(result, err)
		}
	}
}

type remoteSubscription struct {
	tid      a2a.TaskID
	store    TaskStore
	queue    eventqueue.Queue
	consumed bool
}

var _ Subscription = (*remoteSubscription)(nil)

func newRemoteSubscription(queue eventqueue.Queue, store TaskStore, tid a2a.TaskID) *remoteSubscription {
	return &remoteSubscription{tid: tid, queue: queue, store: store}
}

func (s *remoteSubscription) TaskID() a2a.TaskID {
	return s.tid
}

func (s *remoteSubscription) Events(ctx context.Context) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		if s.consumed {
			yield(nil, fmt.Errorf("subscription already consumed"))
			return
		}
		s.consumed = true

		defer func() {
			if err := s.queue.Close(); err != nil {
				log.Warn(ctx, "queue close failed", "error", err)
			}
		}()

		task, snapshotVersion, err := s.store.Get(ctx, s.tid)
		if err != nil && !errors.Is(err, a2a.ErrTaskNotFound) {
			yield(nil, fmt.Errorf("task snapshot loading failed: %w", err))
			return
		}

		if task != nil {
			if !yield(task, nil) {
				return
			}
			if task.Status.State.Terminal() {
				return
			}
		}

		for {
			msg, err := s.queue.Read(ctx)
			if err != nil {
				yield(nil, err)
				return
			}
			if msg.TaskVersion != a2a.TaskVersionMissing && !msg.TaskVersion.After(snapshotVersion) {
				log.Info(ctx, "skipping old event", "event", msg.Event, "version", msg.TaskVersion)
				continue
			}
			if !yield(msg.Event, nil) {
				return
			}
			if taskupdate.IsFinal(msg.Event) {
				return
			}
		}
	}
}
