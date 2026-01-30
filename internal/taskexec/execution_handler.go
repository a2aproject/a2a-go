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

package taskexec

import (
	"context"
	"errors"
	"fmt"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
	"github.com/a2aproject/a2a-go/a2asrv/workqueue"
	"github.com/a2aproject/a2a-go/internal/eventpipe"
	"github.com/a2aproject/a2a-go/log"
)

type executionHandler struct {
	agentEvents       eventpipe.Reader
	handledEventQueue eventqueue.Writer
	handleEventFn     func(context.Context, a2a.Event) (*ProcessorResult, error)
	handleErrorFn     func(context.Context, error) (a2a.SendMessageResult, error)
}

func (h *executionHandler) processEvents(ctx context.Context) (a2a.SendMessageResult, error) {
	for {
		event, err := h.agentEvents.Read(ctx)
		if err != nil && ctx.Err() != nil {
			log.Info(ctx, "execution context canceled", "cause", context.Cause(ctx))
			return h.handleErrorFn(ctx, context.Cause(ctx))
		}

		if err != nil {
			log.Info(ctx, "error reading from queue", "error", err)
			return h.handleErrorFn(ctx, err)
		}

		processResult, err := h.handleEventFn(ctx, event)
		if err != nil {
			log.Info(ctx, "processor error", "error", err)
			return nil, err
		}

		if h.handledEventQueue != nil {
			toEmit := event
			if processResult.EventOverride != nil {
				toEmit = processResult.EventOverride
			}
			if err := h.handledEventQueue.WriteVersioned(ctx, toEmit, processResult.TaskVersion); err != nil {
				log.Info(ctx, "execution context canceled during subscriber notification attempt", "cause", context.Cause(ctx))
				return h.handleErrorFn(ctx, context.Cause(ctx))
			}
		}

		if processResult.ExecutionResult != nil {
			return processResult.ExecutionResult, nil
		}
	}
}

type eventProducerFn func(context.Context) error
type eventConsumerFn func(context.Context) (a2a.SendMessageResult, error)

// runProducerConsumer starts producer and consumer goroutines in an error group and waits
// for both of them to finish or one of them to fail. If both complete successfuly and consumer produces a result,
// the result is returned, otherwise an error is returned.
func runProducerConsumer(
	ctx context.Context,
	producer eventProducerFn,
	consumer eventConsumerFn,
	heartbeater workqueue.Heartbeater,
) (a2a.SendMessageResult, error) {
	group, ctx := errgroup.WithContext(ctx)

	if heartbeater != nil {
		group.Go(func() error {
			timer := time.NewTicker(heartbeater.HeartbeatInterval())
			for {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-timer.C:
					if err := heartbeater.Heartbeat(ctx); err != nil {
						return err
					}
				}
			}
		})
	}

	group.Go(func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("event producer panic: %v", r)
			}
		}()
		err = producer(ctx)
		return
	})

	// The error is returned to cancel producer context when consumer decides to return a result and stop processing events.
	errConsumerStopped := errors.New("consumer stopped")

	var processorResult a2a.SendMessageResult
	group.Go(func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("event consumer panic: %v", r)
			}
		}()

		localResult, err := consumer(ctx)
		processorResult = localResult
		if err == nil {
			// We do this to cancel producer context. There's no point for it to continue, as there will be no consumer to process events.
			err = errConsumerStopped
		}
		return
	})

	groupErr := group.Wait()

	// process the result first, because consumer can override an error with "failed" result
	if processorResult != nil {
		return processorResult, nil
	}

	// errConsumerStopped is just a way to cancel producer context
	if groupErr != nil && !errors.Is(groupErr, errConsumerStopped) {
		return nil, groupErr
	}

	return nil, fmt.Errorf("bug: consumer stopped, but result unset: %w", groupErr)
}
