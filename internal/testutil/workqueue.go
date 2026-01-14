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
	"github.com/a2aproject/a2a-go/a2asrv/limiter"
	"github.com/a2aproject/a2a-go/a2asrv/workqueue"
)

type TestWorkQueue struct {
	HandlerFn workqueue.HandlerFn
	Payloads  []*workqueue.Payload
	WriteErr  error

	WriteFunc           func(context.Context, *workqueue.Payload) (a2a.TaskID, error)
	RegisterHandlerFunc func(limiter.ConcurrencyConfig, workqueue.HandlerFn)
}

func (m *TestWorkQueue) Write(ctx context.Context, payload *workqueue.Payload) (a2a.TaskID, error) {
	if m.WriteFunc != nil {
		return m.WriteFunc(ctx, payload)
	}
	m.Payloads = append(m.Payloads, payload)
	return payload.TaskID, m.WriteErr
}

func (m *TestWorkQueue) RegisterHandler(cfg limiter.ConcurrencyConfig, fn workqueue.HandlerFn) {
	if m.RegisterHandlerFunc != nil {
		m.RegisterHandlerFunc(cfg, fn)
	}
	m.HandlerFn = fn
}

// NewTestWorkQueue allows to mock execution of work queue operations.
// Without any overrides it defaults to in memory implementation.
func NewTestWorkQueue() *TestWorkQueue {
	return &TestWorkQueue{
		Payloads: make([]*workqueue.Payload, 0),
	}
}
