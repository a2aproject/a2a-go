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
	"fmt"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
	"github.com/a2aproject/a2a-go/a2asrv/workqueue"
	"github.com/a2aproject/a2a-go/internal/eventpipe"
	"github.com/a2aproject/a2a-go/log"
)

type workQueueHandler struct {
	queueManager eventqueue.Manager
	taskStore    TaskStore
	factory      Factory
	panicHandler PanicHandlerFn
}

func newWorkQueueHandler(cfg *DistributedManagerConfig) *workQueueHandler {
	backend := &workQueueHandler{
		queueManager: cfg.QueueManager,
		taskStore:    cfg.TaskStore,
		factory:      cfg.Factory,
		panicHandler: cfg.PanicHandler,
	}
	cfg.WorkQueue.RegisterHandler(cfg.ConcurrencyConfig, backend.handle)
	return backend
}

func (b *workQueueHandler) handle(ctx context.Context, payload *workqueue.Payload) (a2a.SendMessageResult, error) {
	pipe := eventpipe.NewLocal()
	defer pipe.Close()

	var eventProducer eventProducerFn
	var eventProcessor Processor

	switch payload.Type {
	case workqueue.PayloadTypeExecute:
		if payload.ExecuteParams == nil {
			return nil, fmt.Errorf("execution params not set: %w", workqueue.ErrMalformedPayload)
		}
		executor, processor, err := b.factory.CreateExecutor(ctx, payload.TaskID, payload.ExecuteParams)
		if err != nil {
			return nil, fmt.Errorf("setup failed: %w", err)
		}
		eventProducer = func(ctx context.Context) error { return executor.Execute(ctx, pipe.Writer) }
		eventProcessor = processor

	case workqueue.PayloadTypeCancel:
		if payload.CancelParams == nil {
			return nil, fmt.Errorf("cancelation params not set: %w", workqueue.ErrMalformedPayload)
		}
		canceler, processor, err := b.factory.CreateCanceler(ctx, payload.CancelParams)
		if err != nil {
			return nil, fmt.Errorf("setup failed: %w", err)
		}
		eventProducer = func(ctx context.Context) error { return canceler.Cancel(ctx, pipe.Writer) }
		eventProcessor = processor

	default:
		// do not return non-retryable ErrMalformedPayload, the process might be running outdated code
		return nil, fmt.Errorf("unknown payload type: %q", payload.Type)
	}

	queue, err := b.queueManager.CreateWriter(ctx, payload.TaskID)
	if err != nil {
		return nil, fmt.Errorf("failed to create a queue: %w", err)
	}

	defer func() {
		if closeErr := queue.Close(); closeErr != nil {
			log.Warn(ctx, "queue close failed", "error", closeErr)
		}
	}()

	handler := &executionHandler{
		agentEvents:       pipe.Reader,
		handledEventQueue: queue,
		handleEventFn:     eventProcessor.Process,
		handleErrorFn:     eventProcessor.ProcessError,
	}

	var heartbeater workqueue.Heartbeater
	if hb, ok := workqueue.HeartbeaterFrom(ctx); ok {
		heartbeater = hb
	}

	return runProducerConsumer(ctx, eventProducer, handler.processEvents, heartbeater, b.panicHandler)
}
