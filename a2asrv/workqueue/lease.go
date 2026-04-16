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
	"errors"
	"fmt"
	"sync"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

// ErrLeaseAlreadyTaken is returned by [LeaseManager.Acquire] when a conflicting lease
// is already held for the given TaskID.
var ErrLeaseAlreadyTaken = errors.New("lease for this type of job is already taken")

// Lease represents an acquired exclusive right to process a work item.
// The holder must call [Lease.Release] when the work is done.
//
// TaskID returns the task identifier associated with the lease. Implementations may
// return a TaskID different from the one in the [Payload] to support idempotency
// (e.g. dedup by message ID and map to an existing TaskID).
type Lease interface {
	// TaskID returns the task identifier associated with the lease.
	TaskID() a2a.TaskID
	// Release releases the lease. Must be safe to call multiple times.
	Release(context.Context)
}

// LeaseManager controls concurrent access to task execution and cancelation.
// Implementations must enforce the following conflict rules:
//
//	Held lease type | Requested lease type | Result
//	----------------|----------------------|---------------------------
//	execute         | execute              | blocked (ErrLeaseAlreadyTaken)
//	execute         | cancel               | allowed (cancel during active execution)
//	cancel          | execute              | blocked (ErrLeaseAlreadyTaken)
//	cancel          | cancel               | blocked (ErrLeaseAlreadyTaken)
//
// Acquire may inspect any field of [Payload] (e.g. for dedup by message ID).
// Release must be safe to call multiple times (idempotent).
type LeaseManager interface {
	// Acquire attempts to acquire a lease for the given payload.
	Acquire(context.Context, *Payload) (Lease, error)
}

type inMemoryLeaseManager struct {
	mu           sync.Mutex
	executions   map[a2a.TaskID]struct{}
	cancelations map[a2a.TaskID]struct{}
}

// NewInMemoryLeaseManager returns a [LeaseManager] that tracks leases in memory.
func NewInMemoryLeaseManager() LeaseManager {
	return &inMemoryLeaseManager{
		executions:   make(map[a2a.TaskID]struct{}),
		cancelations: make(map[a2a.TaskID]struct{}),
	}
}

func (q *inMemoryLeaseManager) Acquire(ctx context.Context, p *Payload) (Lease, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	tid := p.TaskID
	switch p.Type {
	case PayloadTypeExecute:
		if _, ok := q.executions[tid]; ok {
			return nil, ErrLeaseAlreadyTaken
		}
		if _, ok := q.cancelations[tid]; ok {
			return nil, fmt.Errorf("task is being canceled: %w", ErrLeaseAlreadyTaken)
		}
		q.executions[tid] = struct{}{}
	default:
		if _, ok := q.cancelations[tid]; ok {
			return nil, ErrLeaseAlreadyTaken
		}
		q.cancelations[tid] = struct{}{}
	}

	releaseFunc := func() {
		q.mu.Lock()
		defer q.mu.Unlock()
		if p.Type == PayloadTypeExecute {
			delete(q.executions, p.TaskID)
		} else {
			delete(q.cancelations, p.TaskID)
		}
	}

	return &inMemoryLease{tid: p.TaskID, releaseFunc: releaseFunc}, nil
}

type inMemoryLease struct {
	tid         a2a.TaskID
	releaseFunc func()
}

func (l *inMemoryLease) TaskID() a2a.TaskID {
	return l.tid
}

func (l *inMemoryLease) Release(context.Context) {
	l.releaseFunc()
}
