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

package eventqueue

import (
	"context"
	"fmt"
	"sync"

	"github.com/a2aproject/a2a-go/a2a"
)

// Implements Manager interface
type inMemoryManager struct {
	mu     sync.Mutex
	queues map[a2a.TaskID]Queue
}

// NewInMemoryManager creates a new queue manager
func NewInMemoryManager() Manager {
	return &inMemoryManager{
		queues: make(map[a2a.TaskID]Queue),
	}
}

func (m *inMemoryManager) GetOrCreate(ctx context.Context, taskId a2a.TaskID) (Queue, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.queues[taskId]; !ok {
		queue := NewInMemoryQueue(defaultMaxQueueSize)
		m.queues[taskId] = queue
	}
	return m.queues[taskId], nil
}

func (m *inMemoryManager) Get(ctx context.Context, taskId a2a.TaskID) (Queue, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	queue, ok := m.queues[taskId]
	return queue, ok
}

func (m *inMemoryManager) Destroy(ctx context.Context, taskId a2a.TaskID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.queues[taskId]; !ok {
		// todo: consider not failing when it already has desired state
		return fmt.Errorf("queue cannot be destroyed as queue for taskId: %s does not exist", taskId)
	}
	queue := m.queues[taskId]
	_ = queue.Close() // in memory queue close never fails
	delete(m.queues, taskId)
	return nil
}
