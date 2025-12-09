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

package workqueue

import (
	"context"
	"errors"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv/limiter"
)

// ErrConcurrencyLimitExceeded is returned new work acceptance violates the configured concurrency limit.
var ErrConcurrencyLimitExceeded = errors.New("concurrency limit exceeded")

type pushQueue struct {
	Writer
	concurrencyConfig limiter.ConcurrencyConfig
	handlerFn         HandlerFn
}

// NewPushQueue creates a Queue implementation through which SDK submits work to the queue backend.
// The returned handler function is expected to be invoked in a separate goroutine by the caller when
// work is assigned to the node.
func NewPushQueue(writer Writer) (Queue, HandlerFn) {
	queue := &pushQueue{Writer: writer}
	handler := HandlerFn(func(ctx context.Context, p *Payload) (a2a.SendMessageResult, error) {
		// TODO: acquire concurrency quota
		return queue.handlerFn(ctx, p)
	})
	return queue, handler
}

func (q *pushQueue) RegisterHandler(cfg limiter.ConcurrencyConfig, handlerFn HandlerFn) {
	q.concurrencyConfig = cfg
	q.handlerFn = handlerFn
}
