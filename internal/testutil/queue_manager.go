// Copyright 2025 The A2A Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
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

// MockQueueManager is a mock of eventqueue.Manager
type MockQueueManager struct {
	eventqueue.Manager

	GetOrCreateFunc func(ctx context.Context, taskID a2a.TaskID) (eventqueue.Queue, error)
	GetFunc         func(ctx context.Context, taskID a2a.TaskID) (eventqueue.Queue, bool)
	DestroyFunc     func(ctx context.Context, taskID a2a.TaskID) error
}

func (m *MockQueueManager) GetOrCreate(ctx context.Context, taskID a2a.TaskID) (eventqueue.Queue, error) {
	if m.GetOrCreateFunc != nil {
		return m.GetOrCreateFunc(ctx, taskID)
	}
	return m.Manager.GetOrCreate(ctx, taskID)
}

func (m *MockQueueManager) Get(ctx context.Context, taskID a2a.TaskID) (eventqueue.Queue, bool) {
	if m.GetFunc != nil {
		return m.GetFunc(ctx, taskID)
	}
	return m.Manager.Get(ctx, taskID)
}

func (m *MockQueueManager) Destroy(ctx context.Context, taskID a2a.TaskID) error {
	if m.DestroyFunc != nil {
		return m.DestroyFunc(ctx, taskID)
	}
	return m.Manager.Destroy(ctx, taskID)
}

// WithGetOrCreateMock overrides GetOrCreate execution
func (m *MockQueueManager) WithGetOrCreateMock(queue eventqueue.Queue, err error) *MockQueueManager {
	m.GetOrCreateFunc = func(ctx context.Context, taskID a2a.TaskID) (eventqueue.Queue, error) {
		return queue, err
	}
	return m
}

// ClearGetOrCreateMock removes override for GetOrCreate execution
func (m *MockQueueManager) ClearGetOrCreateMock() *MockQueueManager {
	m.GetOrCreateFunc = nil
	return m
}

// WithGetMock overrides Get execution
func (m *MockQueueManager) WithGetMock(queue eventqueue.Queue, ok bool) *MockQueueManager {
	m.GetFunc = func(ctx context.Context, taskID a2a.TaskID) (eventqueue.Queue, bool) {
		return queue, ok
	}
	return m
}

// ClearGetMock removes override for Get execution
func (m *MockQueueManager) ClearGetMock() *MockQueueManager {
	m.GetFunc = nil
	return m
}

// WithDestroyMock overrides Destroy execution
func (m *MockQueueManager) WithDestroyMock(err error) *MockQueueManager {
	m.DestroyFunc = func(ctx context.Context, taskID a2a.TaskID) error {
		return err
	}
	return m
}

// ClearDestroyMock removes override for Get execution
func (m *MockQueueManager) ClearDestroyMock() *MockQueueManager {
	m.DestroyFunc = nil
	return m
}

// NewQueueManagerMock allows to mock execution of manager operations.
// Without any overrides it defaults to in memory implementation.
func NewQueueManagerMock() *MockQueueManager {
	return &MockQueueManager{
		Manager: eventqueue.NewInMemoryManager(),
	}
}
