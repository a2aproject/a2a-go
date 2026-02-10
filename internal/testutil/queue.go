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
	"github.com/a2aproject/a2a-go/internal/eventpipe"
)

// TestEventQueue is a mock of eventqueue.Queue
type TestEventQueue struct {
	pipe *eventpipe.Local

	ReadFunc  func(ctx context.Context) (*eventqueue.Message, error)
	WriteFunc func(ctx context.Context, msg *eventqueue.Message) error
	CloseFunc func() error
}

var _ eventqueue.Reader = (*TestEventQueue)(nil)
var _ eventqueue.Writer = (*TestEventQueue)(nil)

func (m *TestEventQueue) Read(ctx context.Context) (*eventqueue.Message, error) {
	if m.ReadFunc != nil {
		return m.ReadFunc(ctx)
	}
	event, err := m.pipe.Reader.Read(ctx)
	return &eventqueue.Message{Event: event, TaskVersion: a2a.TaskVersionMissing}, err
}

func (m *TestEventQueue) Write(ctx context.Context, msg *eventqueue.Message) error {
	if m.WriteFunc != nil {
		return m.WriteFunc(ctx, msg)
	}
	return m.pipe.Writer.Write(ctx, msg.Event)
}

func (m *TestEventQueue) Close() error {
	if m.CloseFunc != nil {
		return m.CloseFunc()
	}
	m.pipe.Close()
	return nil
}

// SetReadOverride overrides Read execution
func (m *TestEventQueue) SetReadOverride(event a2a.Event, err error) *TestEventQueue {
	m.ReadFunc = func(ctx context.Context) (*eventqueue.Message, error) {
		return &eventqueue.Message{Event: event, TaskVersion: a2a.TaskVersionMissing}, err
	}
	return m
}

// SetReadOverride overrides Read execution with an option to provide a version.
func (m *TestEventQueue) SetReadVersionedOverride(event a2a.Event, version a2a.TaskVersion, err error) *TestEventQueue {
	m.ReadFunc = func(ctx context.Context) (*eventqueue.Message, error) {
		return &eventqueue.Message{Event: event, TaskVersion: version}, err
	}
	return m
}

// SetWriteError overrides Write execution with given error
func (m *TestEventQueue) SetWriteError(err error) *TestEventQueue {
	m.WriteFunc = func(ctx context.Context, msg *eventqueue.Message) error {
		return err
	}
	return m
}

// SetCloseError overrides Close execution with given error
func (m *TestEventQueue) SetCloseError(err error) *TestEventQueue {
	m.CloseFunc = func() error {
		return err
	}
	return m
}

// NewTestEventQueue allows to mock execution of read, write and close.
// Without any overrides it defaults to in memory implementation.
func NewTestEventQueue() *TestEventQueue {
	return &TestEventQueue{
		pipe: eventpipe.NewLocal(),
	}
}
