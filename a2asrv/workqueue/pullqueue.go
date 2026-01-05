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
	"math/rand"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv/limiter"
	"github.com/a2aproject/a2a-go/log"
)

// Message defines the message for execution or cancelation.
type Message interface {
	// Payload returns the payload of the message.
	Payload() *Payload
	// Complete marks the message as completed after it was handled by a worker.
	Complete(ctx context.Context, result a2a.SendMessageResult) error
	// Return returns the message to the queue after worker failed to handle it.
	Return(ctx context.Context, cause error) error
}

type ReadWriter interface {
	Writer
	// Read dequeues a new payload from the queue.
	Read(context.Context) (Message, error)
}

type pullQueue struct {
	ReadWriter
}

func NewPullQueue(rw ReadWriter) Queue {
	return &pullQueue{ReadWriter: rw}
}

func (q *pullQueue) RegisterHandler(cfg limiter.ConcurrencyConfig, handlerFn HandlerFn) {
	go func() {
		ctx := context.Background()
		random := rand.New(rand.NewSource(rand.Int63()))
		backoff, jitter, maxBackoff := 1*time.Second, 2*time.Second, 30*time.Second
		for {
			// TODO: acquire quota
			msg, err := q.ReadWriter.Read(ctx)
			if errors.Is(err, ErrQueueClosed) {
				log.Info(ctx, "cluster backend stopped because work queue was closed")
				return
			}

			if err != nil { // TODO: circuit breaker?
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
				backoff += time.Duration(float32(jitter) * random.Float32())
				log.Info(ctx, "work queue read failed", "error", err, "retry_in_s", backoff.Seconds())
				continue
			}
			backoff = 1

			go func() {
				if hb, ok := msg.(Heartbeater); ok {
					ctx = WithHearbeater(ctx, hb)
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
