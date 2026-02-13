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

	CreateWriterFunc func(ctx context.Context, taskID a2a.TaskID) (eventqueue.Writer, error)
	CreateReaderFunc func(ctx context.Context, taskID a2a.TaskID) (eventqueue.Reader, error)
	DestroyFunc      func(ctx context.Context, taskID a2a.TaskID) error
}

var _ eventqueue.Manager = (*TestQueueManager)(nil)

func (m *TestQueueManager) CreateWriter(ctx context.Context, taskID a2a.TaskID) (eventqueue.Writer, error) {
	if m.CreateWriterFunc != nil {
		return m.CreateWriterFunc(ctx, taskID)
	}
	return m.Manager.CreateWriter(ctx, taskID)
}

func (m *TestQueueManager) CreateReader(ctx context.Context, taskID a2a.TaskID) (eventqueue.Reader, error) {
	if m.CreateReaderFunc != nil {
		return m.CreateReaderFunc(ctx, taskID)
	}
	return m.Manager.CreateReader(ctx, taskID)
}

func (m *TestQueueManager) Destroy(ctx context.Context, taskID a2a.TaskID) error {
	if m.DestroyFunc != nil {
		return m.DestroyFunc(ctx, taskID)
	}
	return m.Manager.Destroy(ctx, taskID)
}

// SetQueue sets the queue to be returned by CreateWriter and CreateReader
func (m *TestQueueManager) SetQueue(queue *TestEventQueue) *TestQueueManager {
	m.CreateWriterFunc = func(ctx context.Context, taskID a2a.TaskID) (eventqueue.Writer, error) {
		return queue, nil
	}
	m.CreateReaderFunc = func(ctx context.Context, taskID a2a.TaskID) (eventqueue.Reader, error) {
		return queue, nil
	}
	return m
}

// SetError sets the error to be returned by CreateWriter and CreateReader
func (m *TestQueueManager) SetError(err error) *TestQueueManager {
	m.CreateWriterFunc = func(ctx context.Context, taskID a2a.TaskID) (eventqueue.Writer, error) {
		return nil, err
	}
	m.CreateReaderFunc = func(ctx context.Context, taskID a2a.TaskID) (eventqueue.Reader, error) {
		return nil, err
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
