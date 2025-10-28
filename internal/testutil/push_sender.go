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
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/internal/push"
)

// TestPushSender is a mock of push.Sender.
// It includes a mock HTTP server to receive push notifications.
type TestPushSender struct {
	*push.HTTPPushSender
	Server         *httptest.Server
	ReceivedTasks  []a2a.Task
	ReceivedConfig *a2a.PushConfig
	SendPushFunc   func(ctx context.Context, config *a2a.PushConfig, task *a2a.Task) error
}

// SendPush calls the underlying SendPushFunc if it's set. If not, it calls the
// embedded HTTPPushSender's SendPush method, which will send the push to the mock server.
func (m *TestPushSender) SendPush(ctx context.Context, config *a2a.PushConfig, task *a2a.Task) error {
	m.ReceivedConfig = config
	m.ReceivedTasks = append(m.ReceivedTasks, *task)

	if m.SendPushFunc != nil {
		return m.SendPushFunc(ctx, config, task)
	}

	return m.HTTPPushSender.SendPush(ctx, config, task)
}

// SetSendPushError overrides SendPush execution with given error
func (m *TestPushSender) SetSendPushError(err error) *TestPushSender {
	m.SendPushFunc = func(ctx context.Context, config *a2a.PushConfig, task *a2a.Task) error {
		m.ReceivedConfig = config
		return err
	}
	return m
}

// NewTestPushSender creates a new TestPushSender.
// It also initializes a mock HTTP server to receive push notifications.
func NewTestPushSender(t *testing.T) *TestPushSender {
	t.Helper()
	sender := &TestPushSender{
		ReceivedTasks: make([]a2a.Task, 0),
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "can't read body", http.StatusBadRequest)
			return
		}
		var task a2a.Task
		if err := json.Unmarshal(body, &task); err != nil {
			http.Error(w, "can't unmarshal body", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	sender.Server = server
	sender.HTTPPushSender = push.NewHTTPPushSender(&push.HTTPSenderConfig{})
	return sender
}
