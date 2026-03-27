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
	"log/slog"
	"runtime/debug"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv/eventqueue"
	"github.com/a2aproject/a2a-go/v2/a2asrv/workqueue"
	"github.com/a2aproject/a2a-go/v2/internal/eventpipe"
	"github.com/a2aproject/a2a-go/v2/log"
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
			return h.handleErrorFn(ctx, fmt.Errorf("context canceled: %w", context.Cause(ctx)))
		}

		if err != nil {
			return h.handleErrorFn(ctx, fmt.Errorf("queue read failed: %w", err))
		}

		if log.Enabled(ctx, slog.LevelDebug) {
			log.Debug(ctx, "processing event", "event_type", fmt.Sprintf("%T", event))
		}

		processResult, err := h.handleEventFn(ctx, event)
		if err != nil {
			return nil, fmt.Errorf("processor error: %w", err)
		}

		if h.handledEventQueue != nil {
			toEmit := event
			if processResult.EventOverride != nil {
				log.Debug(ctx, "event overridden by processor", "cause", processResult.ExecutionFailureCause)
				toEmit = processResult.EventOverride
			}

			msg := &eventqueue.Message{Event: toEmit, TaskVersion: processResult.TaskVersion}
			if err := h.handledEventQueue.Write(ctx, msg); err != nil {
				return h.handleErrorFn(ctx, fmt.Errorf("context canceled on event broadcast: %w", context.Cause(ctx)))
			}
		}

		if processResult.ExecutionResult != nil {
			// If ExecutionResult is not nil it will be received by blocking clients, not the failure cause.
			// The failure cause gets delivered to execution goroutine.
			return processResult.ExecutionResult, processResult.ExecutionFailureCause
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
	panicHandler PanicHandlerFn,
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
						return fmt.Errorf("heartbeat failure: %w", err)
					}
				}
			}
		})
	}

	group.Go(func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				if panicHandler != nil {
					err = panicHandler(r)
				} else {
					err = fmt.Errorf("event producer panic: %v\n%s", r, debug.Stack())
					log.Warn(ctx, "event producer panicked", "error", err)
				}
			}
		}()

		log.Debug(ctx, "event producer started")
		err = producer(ctx)
		log.Debug(ctx, "event producer stopped", "error", err)
		return
	})

	// The error is returned to cancel producer context when consumer decides to return a result and stop processing events.
	errConsumerStopped := errors.New("consumer stopped")

	var processorResult a2a.SendMessageResult
	group.Go(func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				if panicHandler != nil {
					err = panicHandler(r)
				} else {
					err = fmt.Errorf("event consumer panic: %v\n%s", r, debug.Stack())
					log.Warn(ctx, "event consumer panicked", "error", err)
				}
			}
		}()

		log.Debug(ctx, "event consumer started")
		localResult, err := consumer(ctx)
		log.Debug(ctx, "event consumer stopped", "has_result", localResult != nil, "error", err)

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
