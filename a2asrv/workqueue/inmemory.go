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

package workqueue

import (
	"context"
	"fmt"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

// InMemoryQueueConfig configures the in-memory [Queue] implementation.
type InMemoryQueueConfig struct {
	LeaseManager LeaseManager
}

type inMemoryQueue struct {
	cfg InMemoryQueueConfig

	config    HandlerConfig
	handlerFn HandlerFn

	concurrencyQuota *semaphore
}

// NewInMemory creates a [Queue] implementation which uses in-memory storage for tasks.
func NewInMemory(cfg *InMemoryQueueConfig) Queue {
	if cfg == nil {
		cfg = &InMemoryQueueConfig{}
	}
	if cfg.LeaseManager == nil {
		cfg.LeaseManager = NewInMemoryLeaseManager()
	}
	return &inMemoryQueue{cfg: *cfg}
}

// Write implements [Writer].
func (q *inMemoryQueue) Write(ctx context.Context, p *Payload) (a2a.TaskID, error) {
	lease, err := q.cfg.LeaseManager.Acquire(ctx, p)
	if err != nil {
		return "", fmt.Errorf("failed to acquire a lease: %w", err)
	}

	if !q.concurrencyQuota.tryAcquire() {
		lease.Release(ctx)
		return "", ErrConcurrencyLimitExceeded
	}

	go func(ctx context.Context) {
		defer q.concurrencyQuota.release()
		defer lease.Release(ctx)
		if hb, ok := lease.(Heartbeater); ok {
			ctx = AttachHeartbeater(ctx, hb)
		}
		_, _ = q.handlerFn(ctx, p) // result is logged by the handler
	}(context.Background())

	return lease.TaskID(), nil
}

// RegisterHandler implements [Queue].
func (q *inMemoryQueue) RegisterHandler(cfg HandlerConfig, handlerFn HandlerFn) {
	q.config = cfg
	q.concurrencyQuota = newSemaphore(cfg.Limiter.MaxExecutions)
	q.handlerFn = handlerFn
}
