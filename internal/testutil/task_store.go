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
	"github.com/a2aproject/a2a-go/internal/taskstore"
)

// TestTaskStore is a mock of TaskStore
type TestTaskStore struct {
	*taskstore.Mem

	SaveFunc func(ctx context.Context, task *a2a.Task) error
	GetFunc  func(ctx context.Context, taskID a2a.TaskID) (*a2a.Task, error)
}

func (m *TestTaskStore) Save(ctx context.Context, task *a2a.Task) error {
	if m.SaveFunc != nil {
		return m.SaveFunc(ctx, task)
	}
	return m.Mem.Save(ctx, task)
}

func (m *TestTaskStore) Get(ctx context.Context, taskID a2a.TaskID) (*a2a.Task, error) {
	if m.GetFunc != nil {
		return m.GetFunc(ctx, taskID)
	}
	return m.Mem.Get(ctx, taskID)
}

// SetSaveError overrides Save execution with given error
func (m *TestTaskStore) SetSaveError(err error) *TestTaskStore {
	m.SaveFunc = func(ctx context.Context, task *a2a.Task) error {
		return err
	}
	return m
}

// SetGetOverride overrides Get execution
func (m *TestTaskStore) SetGetOverride(task *a2a.Task, err error) *TestTaskStore {
	m.GetFunc = func(ctx context.Context, taskID a2a.TaskID) (*a2a.Task, error) {
		return task, err
	}
	return m
}

// WithTasks seeds TaskStore with given tasks
func (m *TestTaskStore) WithTasks(t *testing.T, tasks ...*a2a.Task) *TestTaskStore {
	t.Helper()
	ctx := t.Context()
	for _, task := range tasks {
		err := m.Save(ctx, task)
		if err != nil {
			t.Errorf("failed to save task: %v", err)
		}
	}
	return m
}

// NewTestTaskStore allows to mock execution of task store operations.
// Without any overrides it defaults to in memory implementation.
func NewTestTaskStore() *TestTaskStore {
	return &TestTaskStore{
		Mem: taskstore.NewMem(),
	}
}
