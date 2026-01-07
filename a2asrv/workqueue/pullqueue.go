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
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv/limiter"
	"github.com/a2aproject/a2a-go/log"
)

// ErrQueueClosed can be returned by Read implementation to stop the polling queue backend.
var ErrQueueClosed = errors.New("queue closed")

// Message defines the message for execution or cancelation.
type Message interface {
	// Payload returns the payload of the message which becomes the execution or cancelation input.
	Payload() *Payload
	// Complete marks the message as completed after it was handled by a worker.
	Complete(ctx context.Context, result a2a.SendMessageResult) error
	// Return returns the message to the queue after worker failed to handle it.
	Return(ctx context.Context, cause error) error
}

// ReadWriter is the neccessary pull-queue dependency.
// Write is used by executor frontend to submit work when a message is received from a client.
// Read is called periodically from background goroutine to request work. Read blocks if no work is available.
// [ErrQueueClosed] will stop the polling loop.
type ReadWriter interface {
	Writer
	// Read dequeues a new message from the queue.
	Read(context.Context) (Message, error)
}

// PullQueueConfig provides a way to customize pull-queue behavior.
type PullQueueConfig struct {
	// ReadRetry configures the behavior of polling loop in case of workqueue Read errors.
	ReadRetry ReadRetryPolicy
}

type pullQueue struct {
	ReadWriter

	config *PullQueueConfig
}

// NewPullQueue creates a [Queue] implementation which starts runs a work polling loop until
// [ErrQueueClosed] is returned from Read.
func NewPullQueue(rw ReadWriter, cfg *PullQueueConfig) Queue {
	if cfg == nil {
		cfg = &PullQueueConfig{}
	}
	if cfg.ReadRetry == nil {
		cfg.ReadRetry = defaultExponentialBackoff
	}
	return &pullQueue{ReadWriter: rw, config: cfg}
}

func (q *pullQueue) RegisterHandler(cfg limiter.ConcurrencyConfig, handlerFn HandlerFn) {
	go func() {
		readAttempt := 0
		ctx := context.Background()
		for {
			// TODO: only call Read when concurrency quota permits
			msg, err := q.ReadWriter.Read(ctx)
			if errors.Is(err, ErrQueueClosed) {
				log.Info(ctx, "cluster backend stopped because work queue was closed")
				return
			}

			if err != nil {
				retryIn := q.config.ReadRetry.NextDelay(readAttempt)
				log.Info(ctx, "work queue read failed", "error", err, "retry_in_s", retryIn.Seconds())
				time.Sleep(retryIn)
				readAttempt++
				continue
			}
			readAttempt = 0

			go func() {
				if hb, ok := msg.(Heartbeater); ok {
					ctx = WithHeartbeater(ctx, hb)
				}

				result, handleErr := handlerFn(ctx, msg.Payload())
				if handleErr != nil {
					if returnErr := msg.Return(ctx, handleErr); returnErr != nil {
						log.Warn(ctx, "failed to return failed work item", "handle_err", handleErr, "return_err", returnErr)
					} else {
						log.Info(ctx, "failed to handle work item", "error", handleErr)
					}
					return
				}

				if err := msg.Complete(ctx, result); err != nil {
					log.Warn(ctx, "failed to mark work item as completed", "error", err)
				}
			}()
		}
	}()
}
