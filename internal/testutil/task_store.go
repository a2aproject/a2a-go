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
	"testing"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv/taskstore"
)

// TestTaskStore is a mock of TaskStore
type TestTaskStore struct {
	*taskstore.InMemory

	CreateFunc func(ctx context.Context, task *a2a.Task) (taskstore.TaskVersion, error)
	UpdateFunc func(ctx context.Context, req *taskstore.UpdateRequest) (taskstore.TaskVersion, error)
	GetFunc    func(ctx context.Context, taskID a2a.TaskID) (*taskstore.StoredTask, error)
}

func (m *TestTaskStore) Create(ctx context.Context, task *a2a.Task) (taskstore.TaskVersion, error) {
	if m.CreateFunc != nil {
		return m.CreateFunc(ctx, task)
	}
	return m.InMemory.Create(ctx, task)
}

func (m *TestTaskStore) Update(ctx context.Context, req *taskstore.UpdateRequest) (taskstore.TaskVersion, error) {
	if m.UpdateFunc != nil {
		return m.UpdateFunc(ctx, req)
	}
	return m.InMemory.Update(ctx, req)
}

func (m *TestTaskStore) Get(ctx context.Context, taskID a2a.TaskID) (*taskstore.StoredTask, error) {
	if m.GetFunc != nil {
		return m.GetFunc(ctx, taskID)
	}
	return m.InMemory.Get(ctx, taskID)
}

// SetSaveError overrides Save execution with given error
func (m *TestTaskStore) SetSaveError(err error) *TestTaskStore {
	m.CreateFunc = func(ctx context.Context, task *a2a.Task) (taskstore.TaskVersion, error) {
		return taskstore.TaskVersionMissing, err
	}
	m.UpdateFunc = func(ctx context.Context, req *taskstore.UpdateRequest) (taskstore.TaskVersion, error) {
		return taskstore.TaskVersionMissing, err
	}

	return m
}

// SetGetOverride overrides Get execution
func (m *TestTaskStore) SetGetOverride(task *taskstore.StoredTask, err error) *TestTaskStore {
	m.GetFunc = func(ctx context.Context, taskID a2a.TaskID) (*taskstore.StoredTask, error) {
		return task, err
	}
	return m
}

// WithTasks seeds TaskStore with given tasks
func (m *TestTaskStore) WithTasks(t *testing.T, tasks ...*a2a.Task) *TestTaskStore {
	t.Helper()
	ctx := t.Context()

	for _, task := range tasks {
		_, err := m.Create(ctx, task)
		if err != nil {
			t.Errorf("failed to save task: %v", err)
		}
	}
	return m
}

// NewTestTaskStore invokes NewTestTaskStoreWithConfig with nil to use the default config.
func NewTestTaskStore() *TestTaskStore {
	return NewTestTaskStoreWithConfig(nil)
}

// NewTestTaskStoreWithConfig allows to mock execution of task store operations.
// Without any overrides it defaults to in memory implementation with given config.
func NewTestTaskStoreWithConfig(config *taskstore.InMemoryStoreConfig) *TestTaskStore {
	return &TestTaskStore{
		InMemory: taskstore.NewInMemory(config),
	}
}
