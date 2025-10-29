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

// TestPushSender is a mock of push.Sender.
type TestPushSender struct {
	*push.HTTPPushSender

	PushedTasks   []*a2a.Task
	PushedConfigs []*a2a.PushConfig

	SendPushFunc func(ctx context.Context, config *a2a.PushConfig, task *a2a.Task) error
}

// SendPush calls the underlying SendPushFunc if it's set. If not,
// it calls the embedded HTTPPushSender's SendPush method.
func (m *TestPushSender) SendPush(ctx context.Context, config *a2a.PushConfig, task *a2a.Task) error {
	m.PushedConfigs = append(m.PushedConfigs, config)
	m.PushedTasks = append(m.PushedTasks, task)

	if m.SendPushFunc != nil {
		return m.SendPushFunc(ctx, config, task)
	}

	return m.HTTPPushSender.SendPush(ctx, config, task)
}

// SetSendPushError overrides SendPush execution with given error
func (m *TestPushSender) SetSendPushError(err error) *TestPushSender {
	m.SendPushFunc = func(ctx context.Context, config *a2a.PushConfig, task *a2a.Task) error {
		return err
	}
	return m
}

// NewTestPushSender creates a new TestPushSender.
func NewTestPushSender(t *testing.T) *TestPushSender {
	return &TestPushSender{
		HTTPPushSender: push.NewHTTPPushSender(nil),

		PushedTasks:   make([]*a2a.Task, 0),
		PushedConfigs: make([]*a2a.PushConfig, 0),
	}
}
