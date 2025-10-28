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
	"github.com/a2aproject/a2a-go/a2asrv/push"
)

// TestPushConfigStore is a mock of push.PushConfigStore.
type TestPushConfigStore struct {
	*push.InMemoryPushConfigStore

	SaveFunc      func(ctx context.Context, taskID a2a.TaskID, config *a2a.PushConfig) (*a2a.PushConfig, error)
	GetFunc       func(ctx context.Context, taskID a2a.TaskID, configID string) (*a2a.PushConfig, error)
	ListFunc      func(ctx context.Context, taskID a2a.TaskID) ([]*a2a.PushConfig, error)
	DeleteFunc    func(ctx context.Context, taskID a2a.TaskID, configID string) error
	DeleteAllFunc func(ctx context.Context, taskID a2a.TaskID) error
}

func (m *TestPushConfigStore) Save(ctx context.Context, taskID a2a.TaskID, config *a2a.PushConfig) (*a2a.PushConfig, error) {
	if m.SaveFunc != nil {
		return m.SaveFunc(ctx, taskID, config)
	}
	return m.InMemoryPushConfigStore.Save(ctx, taskID, config)
}

func (m *TestPushConfigStore) Get(ctx context.Context, taskID a2a.TaskID, configID string) (*a2a.PushConfig, error) {
	if m.GetFunc != nil {
		return m.GetFunc(ctx, taskID, configID)
	}
	return m.InMemoryPushConfigStore.Get(ctx, taskID, configID)
}

func (m *TestPushConfigStore) List(ctx context.Context, taskID a2a.TaskID) ([]*a2a.PushConfig, error) {
	if m.ListFunc != nil {
		return m.ListFunc(ctx, taskID)
	}
	return m.InMemoryPushConfigStore.List(ctx, taskID)
}

func (m *TestPushConfigStore) Delete(ctx context.Context, taskID a2a.TaskID, configID string) error {
	if m.DeleteFunc != nil {
		return m.DeleteFunc(ctx, taskID, configID)
	}
	return m.InMemoryPushConfigStore.Delete(ctx, taskID, configID)
}

func (m *TestPushConfigStore) DeleteAll(ctx context.Context, taskID a2a.TaskID) error {
	if m.DeleteAllFunc != nil {
		return m.DeleteAllFunc(ctx, taskID)
	}
	return m.InMemoryPushConfigStore.DeleteAll(ctx, taskID)
}

// SetSaveOverride overrides Save execution
func (m *TestPushConfigStore) SetSaveOverride(config *a2a.PushConfig, err error) *TestPushConfigStore {
	m.SaveFunc = func(ctx context.Context, taskID a2a.TaskID, config *a2a.PushConfig) (*a2a.PushConfig, error) {
		return config, err
	}
	return m
}

// SetGetOverride overrides Get execution
func (m *TestPushConfigStore) SetGetOverride(config *a2a.PushConfig, err error) *TestPushConfigStore {
	m.GetFunc = func(ctx context.Context, taskID a2a.TaskID, configID string) (*a2a.PushConfig, error) {
		return config, err
	}
	return m
}

// SetListOverride overrides List execution
func (m *TestPushConfigStore) SetListOverride(configs []*a2a.PushConfig, err error) *TestPushConfigStore {
	m.ListFunc = func(ctx context.Context, taskID a2a.TaskID) ([]*a2a.PushConfig, error) {
		return configs, err
	}
	return m
}

// SetDeleteError overrides Delete execution with given error
func (m *TestPushConfigStore) SetDeleteError(err error) *TestPushConfigStore {
	m.DeleteFunc = func(ctx context.Context, taskID a2a.TaskID, configID string) error {
		return err
	}
	return m
}

// SetDeleteAllError overrides Delete execution with given error
func (m *TestPushConfigStore) SetDeleteAllError(err error) *TestPushConfigStore {
	m.DeleteAllFunc = func(ctx context.Context, taskID a2a.TaskID) error {
		return err
	}
	return m
}

// WithConfigs seeds PushConfigStore with given configs.
func (m *TestPushConfigStore) WithConfigs(t *testing.T, taskID a2a.TaskID, configs ...*a2a.PushConfig) *TestPushConfigStore {
	t.Helper()
	ctx := t.Context()
	for _, config := range configs {
		_, err := m.Save(ctx, taskID, config)
		if err != nil {
			t.Errorf("failed to save push config: %v", err)
		}
	}
	return m
}

// NewTestPushConfigStore allows to mock execution of push config store operations.
// Without any overrides it defaults to in memory implementation.
func NewTestPushConfigStore() *TestPushConfigStore {
	return &TestPushConfigStore{
		InMemoryPushConfigStore: push.NewInMemoryStore(),
	}
}
