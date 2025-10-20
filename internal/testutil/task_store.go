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
	"github.com/a2aproject/a2a-go/internal/taskstore"
)

// MockTaskStore is a mock of TaskStore
type MockTaskStore struct {
	taskstore.Mem

	SaveFunc func(ctx context.Context, task *a2a.Task) error
	GetFunc  func(ctx context.Context, taskID a2a.TaskID) (*a2a.Task, error)
}

func (m *MockTaskStore) Save(ctx context.Context, task *a2a.Task) error {
	if m.SaveFunc != nil {
		return m.SaveFunc(ctx, task)
	}
	return m.Mem.Save(ctx, task)
}

func (m *MockTaskStore) Get(ctx context.Context, taskID a2a.TaskID) (*a2a.Task, error) {
	if m.GetFunc != nil {
		return m.GetFunc(ctx, taskID)
	}
	return m.Mem.Get(ctx, taskID)
}

// WithSaveMock overrides Save execution
func (m *MockTaskStore) WithSaveMock(err error) *MockTaskStore {
	m.SaveFunc = func(ctx context.Context, task *a2a.Task) error {
		return err
	}
	return m
}

// ClearSaveMock removes override for Save execution
func (m *MockTaskStore) ClearSaveMock() *MockTaskStore {
	m.SaveFunc = nil
	return m
}

// WithGetMock overrides Get execution
func (m *MockTaskStore) WithGetMock(task *a2a.Task, err error) *MockTaskStore {
	m.GetFunc = func(ctx context.Context, taskID a2a.TaskID) (*a2a.Task, error) {
		return task, err
	}
	return m
}

// ClearGetMock removes override for Get execution
func (m *MockTaskStore) ClearGetMock() *MockTaskStore {
	m.GetFunc = nil
	return m
}

// WithTask seeds TaskStore with given task
func (m *MockTaskStore) WithTask(ctx context.Context, task *a2a.Task) *MockTaskStore {
	_ = m.Mem.Save(ctx, task)
	return m
}

// NewTaskStoreMock allows to mock execution of task store operations.
// Without any overrides it defaults to in memory implementation.
func NewTaskStoreMock() *MockTaskStore {
	return &MockTaskStore{
		Mem: *taskstore.NewMem(),
	}
}
