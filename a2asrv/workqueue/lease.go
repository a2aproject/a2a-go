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
	"errors"
	"fmt"
	"sync"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

// ErrLeaseAlreadyTaken is returned by [LeaseManager.Acquire] when a conflicting lease
// is already held for the given task id.
var ErrLeaseAlreadyTaken = errors.New("lease for this type of job is already taken")

// Lease represents an acquired exclusive right to process a work item.
// Lease extension is supported by implementing [Heartbeater].
type Lease interface {
	// TaskID returns the task identifier associated with the lease.
	// Implementations may return a TaskID different from the one in the [Payload] to
	// support idempotency (e.g. dedup by message ID and map to an existing TaskID).
	TaskID() a2a.TaskID
	// Release releases the lease.
	Release(context.Context)
}

// LeaseManager controls concurrent access to task execution and cancelation.
type LeaseManager interface {
	// Acquire attempts to acquire a lease for the given payload.
	Acquire(context.Context, *Payload) (Lease, error)
}

type inMemoryLeaseManager struct {
	mu           sync.Mutex
	executions   map[a2a.TaskID]*struct{}
	cancelations map[a2a.TaskID]*struct{}
}

// NewInMemoryLeaseManager returns a [LeaseManager] that tracks leases in memory.
func NewInMemoryLeaseManager() LeaseManager {
	return &inMemoryLeaseManager{
		executions:   make(map[a2a.TaskID]*struct{}),
		cancelations: make(map[a2a.TaskID]*struct{}),
	}
}

// Acquire implements [LeaseManager.Acquire].
func (q *inMemoryLeaseManager) Acquire(ctx context.Context, p *Payload) (Lease, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	var tokenMap map[a2a.TaskID]*struct{}
	leaseToken := &struct{}{}

	tid := p.TaskID
	switch p.Type {
	case PayloadTypeExecute:
		if _, ok := q.executions[tid]; ok {
			return nil, ErrLeaseAlreadyTaken
		}
		if _, ok := q.cancelations[tid]; ok {
			return nil, fmt.Errorf("task is being canceled: %w", ErrLeaseAlreadyTaken)
		}
		q.executions[tid] = leaseToken
		tokenMap = q.executions

	default:
		if _, ok := q.cancelations[tid]; ok {
			return nil, ErrLeaseAlreadyTaken
		}
		q.cancelations[tid] = leaseToken
		tokenMap = q.cancelations
	}

	releaseFunc := func() {
		q.mu.Lock()
		defer q.mu.Unlock()
		if token := tokenMap[tid]; token == leaseToken {
			delete(tokenMap, tid)
		}
	}

	return &inMemoryLease{tid: p.TaskID, releaseFunc: releaseFunc}, nil
}

type inMemoryLease struct {
	tid         a2a.TaskID
	releaseFunc func()
}

// TaskID implements [Lease.TaskID].
func (l *inMemoryLease) TaskID() a2a.TaskID {
	return l.tid
}

// Release implements [Lease.Release].
func (l *inMemoryLease) Release(context.Context) {
	l.releaseFunc()
}
