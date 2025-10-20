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

// MockEventQueue is a mock of eventqueue.Queue
type MockEventQueue struct {
	eventqueue.Queue

	ReadFunc  func(ctx context.Context) (a2a.Event, error)
	WriteFunc func(ctx context.Context, event a2a.Event) error
	CloseFunc func() error
}

func (m *MockEventQueue) Read(ctx context.Context) (a2a.Event, error) {
	if m.ReadFunc != nil {
		return m.ReadFunc(ctx)
	}
	return m.Queue.Read(ctx)
}

func (m *MockEventQueue) Write(ctx context.Context, event a2a.Event) error {
	if m.WriteFunc != nil {
		return m.WriteFunc(ctx, event)
	}
	return m.Queue.Write(ctx, event)
}

func (m *MockEventQueue) Close() error {
	if m.CloseFunc != nil {
		return m.CloseFunc()
	}
	return m.Queue.Close()
}

type MockEventQueueOption func(*MockEventQueue)

// WithReadMock overrides Read execution
func (m *MockEventQueue) WithReadMock(event a2a.Event, err error) *MockEventQueue {
	m.ReadFunc = func(ctx context.Context) (a2a.Event, error) {
		return event, err
	}
	return m
}

// ClearReadMock removes override for Read execution
func (m *MockEventQueue) ClearReadMock() *MockEventQueue {
	m.ReadFunc = nil
	return m
}

// WithWriteMock overrides Write execution
func (m *MockEventQueue) WithWriteMock(err error) *MockEventQueue {
	m.WriteFunc = func(ctx context.Context, event a2a.Event) error {
		return err
	}
	return m
}

// ClearWriteMock removes override for Write execution
func (m *MockEventQueue) ClearWriteMock() *MockEventQueue {
	m.WriteFunc = nil
	return m
}

// WithCloseMock overrides Close execution
func (m *MockEventQueue) WithCloseMock(err error) *MockEventQueue {
	m.CloseFunc = func() error {
		return err
	}
	return m
}

// ClearCloseMock removes override for Close execution
func (m *MockEventQueue) ClearCloseMock() *MockEventQueue {
	m.CloseFunc = nil
	return m
}

// NewEventQueueMock allows to mock execution of read, write and close.
// Without any overrides it defaults to in memory implementation.
func NewEventQueueMock(options ...MockEventQueueOption) *MockEventQueue {
	return &MockEventQueue{
		Queue: eventqueue.NewInMemoryQueue(512),
	}
}
