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

// Subscription encapsulates the logic of subscribing a channel to [Execution] events and canceling the localSubscription.
// A default localSubscription is created when an Execution is started.
type Subscription interface {
	Events(ctx context.Context) iter.Seq2[a2a.Event, error]
}

type localSubscription struct {
	execution *localExecution
	queue     eventqueue.Queue
	consumed  bool
}

func newLocalSubscription(e *localExecution, q eventqueue.Queue) *localSubscription {
	return &localSubscription{execution: e, queue: q}
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
			event, err := s.queue.Read(ctx)
			if errors.Is(err, eventqueue.ErrQueueClosed) {
				break
			}
			if err != nil {
				yield(nil, err)
				return
			}
			terminalReported = taskupdate.IsFinal(event)
			if !yield(event, nil) {
				return
			}
		}

		// execution might not report the terminal event in case context was canceled which
		// might happen if event producer panics.
		if result, err := s.execution.Result(ctx); !terminalReported || err != nil {
			yield(result, err)
		}
	}
}
