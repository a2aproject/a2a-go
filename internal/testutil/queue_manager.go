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

package testutil

import (
	"context"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
)

// TestQueueManager is a mock of eventqueue.Manager
type TestQueueManager struct {
	eventqueue.Manager

	GetOrCreateFunc func(ctx context.Context, taskID a2a.TaskID) (eventqueue.Queue, error)
	GetFunc         func(ctx context.Context, taskID a2a.TaskID) (eventqueue.Queue, bool)
	DestroyFunc     func(ctx context.Context, taskID a2a.TaskID) error
}

func (m *TestQueueManager) GetOrCreate(ctx context.Context, taskID a2a.TaskID) (eventqueue.Queue, error) {
	if m.GetOrCreateFunc != nil {
		return m.GetOrCreateFunc(ctx, taskID)
	}
	return m.Manager.GetOrCreate(ctx, taskID)
}

func (m *TestQueueManager) Get(ctx context.Context, taskID a2a.TaskID) (eventqueue.Queue, bool) {
	if m.GetFunc != nil {
		return m.GetFunc(ctx, taskID)
	}
	return m.Manager.Get(ctx, taskID)
}

func (m *TestQueueManager) Destroy(ctx context.Context, taskID a2a.TaskID) error {
	if m.DestroyFunc != nil {
		return m.DestroyFunc(ctx, taskID)
	}
	return m.Manager.Destroy(ctx, taskID)
}

// SetGetOrCreateOverride overrides GetOrCreate execution
func (m *TestQueueManager) SetGetOrCreateOverride(queue eventqueue.Queue, err error) *TestQueueManager {
	m.GetOrCreateFunc = func(ctx context.Context, taskID a2a.TaskID) (eventqueue.Queue, error) {
		return queue, err
	}
	return m
}

// SetGetOverride overrides Get execution
func (m *TestQueueManager) SetGetOverride(queue eventqueue.Queue, ok bool) *TestQueueManager {
	m.GetFunc = func(ctx context.Context, taskID a2a.TaskID) (eventqueue.Queue, bool) {
		return queue, ok
	}
	return m
}

// SetDestroyError overrides Destroy execution with given error
func (m *TestQueueManager) SetDestroyError(err error) *TestQueueManager {
	m.DestroyFunc = func(ctx context.Context, taskID a2a.TaskID) error {
		return err
	}
	return m
}

// NewTestQueueManager allows to mock execution of manager operations.
// Without any overrides it defaults to in memory implementation.
func NewTestQueueManager() *TestQueueManager {
	return &TestQueueManager{
		Manager: eventqueue.NewInMemoryManager(),
	}
}
