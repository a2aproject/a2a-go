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
	"sync"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/log"
)

// ErrQueueClosed can be returned by Read implementation to stop the polling queue backend.
var ErrQueueClosed = errors.New("queue closed")

// Message defines the message for execution or cancelation.
type Message interface {
	// Payload returns the payload of the message which becomes the execution or cancelation input.
	Payload() *Payload
	// Complete marks the message as completed after it was handled by a worker.
	Complete(ctx context.Context) error
	// Return returns the message to the queue after worker failed to handle it.
	Return(ctx context.Context, cause error) error
}

// ReadWriter is the necessary pull-queue dependency.
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
	// BeforeExecutionCallback is called before execution is started. It can be used to modify the context or the message.
	// If an error is returned, execution will not be started and neither Complete nor Return will be called for the Message.
	BeforeExecutionCallback func(context.Context, Message) (context.Context, error)
	// AfterExecutionCallback is called after execution finishes. It can be used to decide how the message should be handled.
	// If an error is returned, neither Complete nor Return will be called for the Message.
	AfterExecutionCallback func(context.Context, Message, a2a.SendMessageResult, error) error
}

type pullQueue struct {
	ReadWriter

	config *PullQueueConfig

	// used for tests
	onShutdown func()
}

// NewPullQueue creates a [Queue] implementation which runs a work polling loop until
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

func (q *pullQueue) RegisterHandler(cfg HandlerConfig, handlerFn HandlerFn) {
	concurrencyQuota := newSemaphore(cfg.Limiter.MaxExecutions)

	var wg sync.WaitGroup
	go func() {
		readAttempt := 0
		ctx := context.Background()
		for {
			concurrencyQuota.acquire()

			msg, err := q.ReadWriter.Read(ctx)

			if errors.Is(err, ErrQueueClosed) {
				concurrencyQuota.release()
				log.Info(ctx, "cluster backend stopped because work queue was closed")
				wg.Wait() // drain
				if q.onShutdown != nil {
					q.onShutdown()
				}
				return
			}

			if err != nil {
				concurrencyQuota.release()
				retryIn := q.config.ReadRetry.NextDelay(readAttempt)
				log.Info(ctx, "work queue read failed", "error", err, "retry_in_s", retryIn.Seconds())
				time.Sleep(retryIn)
				readAttempt++
				continue
			}
			readAttempt = 0

			wg.Add(1)
			go func(ctx context.Context) {
				defer wg.Done()
				defer concurrencyQuota.release()
				q.handleMessage(ctx, msg, handlerFn)
			}(ctx)
		}
	}()
}

func (q *pullQueue) handleMessage(ctx context.Context, msg Message, handlerFn HandlerFn) {
	if q.config.BeforeExecutionCallback != nil {
		var err error
		ctx, err = q.config.BeforeExecutionCallback(ctx, msg)
		if err != nil {
			log.Debug(ctx, "before exec callback short-circuited execution", "cause", err)
			return
		}
	}

	if hb, ok := msg.(Heartbeater); ok {
		ctx = AttachHeartbeater(ctx, hb)
	}

	result, handleErr := handlerFn(ctx, msg.Payload())

	if q.config.AfterExecutionCallback != nil {
		if err := q.config.AfterExecutionCallback(ctx, msg, result, handleErr); err != nil {
			log.Debug(ctx, "after exec callback handled message", "cause", err)
			return
		}
	}

	if errors.Is(handleErr, ErrMalformedPayload) {
		// TODO: dead-letter queue Writer. If fails - return message, else - mark completed
		if completeErr := msg.Complete(ctx); completeErr != nil {
			log.Warn(ctx, "failed to mark malformed item as complete", "payload", msg.Payload(), "payload_error", handleErr, "error", completeErr)
		} else {
			log.Info(ctx, "malformed item marked as complete", "payload", msg.Payload(), "payload_error", handleErr)
		}
		return
	}

	if handleErr != nil {
		if returnErr := msg.Return(ctx, handleErr); returnErr != nil {
			log.Warn(ctx, "failed to return failed work item", "handle_err", handleErr, "return_err", returnErr)
		} else {
			log.Info(ctx, "failed to handle work item", "error", handleErr)
		}
		return
	}

	if err := msg.Complete(ctx); err != nil {
		log.Warn(ctx, "failed to mark work item as completed", "error", err)
	}
}
