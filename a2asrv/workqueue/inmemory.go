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

package workqueue

import (
	"context"
	"fmt"
	"sync"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

type inMemoryQueue struct {
	config    HandlerConfig
	handlerFn HandlerFn

	mu           sync.Mutex
	executions   map[a2a.TaskID]struct{}
	cancelations map[a2a.TaskID]struct{}

	concurrencyQuota *semaphore
}

// NewInMemory creates a [Queue] implementation which uses in-memory storage for tasks.
func NewInMemory() Queue {
	return &inMemoryQueue{
		executions:   make(map[a2a.TaskID]struct{}),
		cancelations: make(map[a2a.TaskID]struct{}),
	}
}

// Write implements [Queue].
func (q *inMemoryQueue) Write(ctx context.Context, p *Payload) (a2a.TaskID, error) {
	tid := p.TaskID
	if started, err := q.startTracking(p); !started || err != nil {
		return tid, err
	}

	if !q.concurrencyQuota.tryAcquire() {
		q.stopTracking(p)
		return "", ErrConcurrencyLimitExceeded
	}

	detachedCtx := context.WithoutCancel(ctx)
	go func(ctx context.Context) {
		defer q.concurrencyQuota.release()
		defer q.stopTracking(p)
		_, _ = q.handlerFn(ctx, p) // result is logged by the handler
	}(detachedCtx)

	return p.TaskID, nil
}

// RegisterHandler implements [Queue].
func (q *inMemoryQueue) RegisterHandler(cfg HandlerConfig, handlerFn HandlerFn) {
	q.config = cfg
	q.concurrencyQuota = newSemaphore(cfg.Limiter.MaxExecutions)
	q.handlerFn = handlerFn
}

func (q *inMemoryQueue) startTracking(p *Payload) (bool, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	tid := p.TaskID
	if p.Type == PayloadTypeExecute {
		// execution is already in progress for this taskID
		if _, ok := q.executions[tid]; ok {
			return false, nil
		}
		// do not allow to start executions if a task is being canceled
		if _, ok := q.cancelations[tid]; ok {
			return false, fmt.Errorf("task is being canceled")
		}
	} else {
		// cancelation is already in progress for this taskID
		if _, ok := q.cancelations[tid]; ok {
			return false, nil
		}
	}
	if p.Type == PayloadTypeExecute {
		q.executions[tid] = struct{}{}
	} else {
		q.cancelations[tid] = struct{}{}
	}
	return true, nil
}

func (q *inMemoryQueue) stopTracking(p *Payload) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if p.Type == PayloadTypeExecute {
		delete(q.executions, p.TaskID)
	} else {
		delete(q.cancelations, p.TaskID)
	}
}
