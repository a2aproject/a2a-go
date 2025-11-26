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
	"github.com/a2aproject/a2a-go/internal/eventpipe"
)

// TestEventQueue is a mock of eventqueue.Queue
type TestEventQueue struct {
	pipe *eventpipe.Local

	ReadFunc  func(ctx context.Context) (a2a.Event, error)
	WriteFunc func(ctx context.Context, event a2a.Event) error
	CloseFunc func() error
}

func (m *TestEventQueue) Read(ctx context.Context) (a2a.Event, error) {
	if m.ReadFunc != nil {
		return m.ReadFunc(ctx)
	}
	return m.pipe.Reader.Read(ctx)
}

func (m *TestEventQueue) Write(ctx context.Context, event a2a.Event) error {
	if m.WriteFunc != nil {
		return m.WriteFunc(ctx, event)
	}
	return m.pipe.Writer.Write(ctx, event)
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
	m.ReadFunc = func(ctx context.Context) (a2a.Event, error) {
		return event, err
	}
	return m
}

// SetWriteError overrides Write execution with given error
func (m *TestEventQueue) SetWriteError(err error) *TestEventQueue {
	m.WriteFunc = func(ctx context.Context, event a2a.Event) error {
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
