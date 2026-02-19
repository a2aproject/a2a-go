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
	"log/slog"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
	"github.com/a2aproject/a2a-go/a2asrv/limiter"
	"github.com/a2aproject/a2a-go/a2asrv/taskstore"
	"github.com/a2aproject/a2a-go/a2asrv/workqueue"
	"github.com/a2aproject/a2a-go/internal/taskupdate"
	"github.com/a2aproject/a2a-go/log"
)

// DistributedManagerConfig contains configuration for A2A task execution
// mode where work is distributed across an A2A cluster.
type DistributedManagerConfig struct {
	WorkQueue         workqueue.Queue
	QueueManager      eventqueue.Manager
	Factory           Factory
	TaskStore         taskstore.Store
	ConcurrencyConfig limiter.ConcurrencyConfig
	Logger            *slog.Logger
	PanicHandler      PanicHandlerFn
}

type distributedManager struct {
	workHandler  *workQueueHandler
	workQueue    workqueue.Queue
	queueManager eventqueue.Manager
	taskStore    taskstore.Store
}

var _ Manager = (*distributedManager)(nil)

// NewDistributedManager creates a new [Manager] instance which uses WorkQueue for work distribution across A2A cluster.
func NewDistributedManager(cfg *DistributedManagerConfig) Manager {
	frontend := &distributedManager{
		workHandler:  newWorkQueueHandler(cfg),
		queueManager: cfg.QueueManager,
		workQueue:    cfg.WorkQueue,
		taskStore:    cfg.TaskStore,
	}
	return frontend
}

func (m *distributedManager) Resubscribe(ctx context.Context, taskID a2a.TaskID) (Subscription, error) {
	if _, err := m.taskStore.Get(ctx, taskID); err != nil {
		return nil, a2a.ErrTaskNotFound
	}
	queue, err := m.queueManager.CreateReader(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("failed to get event queue: %w", err)
	}
	return newRemoteSubscription(queue, m.taskStore, taskID), nil
}

func (m *distributedManager) Execute(ctx context.Context, req *a2a.SendMessageRequest) (Subscription, error) {
	if req == nil || req.Message == nil {
		return nil, fmt.Errorf("message is required: %w", a2a.ErrInvalidParams)
	}

	var taskID a2a.TaskID
	if len(req.Message.TaskID) == 0 {
		taskID = a2a.NewTaskID()
	} else {
		taskID = req.Message.TaskID
	}

	msg := req.Message
	if msg.TaskID != "" {
		taskStoreTask, err := m.taskStore.Get(ctx, msg.TaskID)
		if err != nil {
			return nil, fmt.Errorf("task loading failed: %w", err)
		}
		storedTask := taskStoreTask.Task
		if storedTask == nil {
			return nil, a2a.ErrTaskNotFound
		}

		if msg.ContextID != "" && msg.ContextID != storedTask.ContextID {
			return nil, fmt.Errorf("message contextID different from task contextID: %w", a2a.ErrInvalidParams)
		}

		if storedTask.Status.State.Terminal() {
			return nil, fmt.Errorf("task in a terminal state %q: %w", storedTask.Status.State, a2a.ErrInvalidParams)
		}
	}

	queue, err := m.queueManager.CreateReader(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("failed to get or create queue: %w", err)
	}

	taskID, err = m.workQueue.Write(ctx, &workqueue.Payload{
		Type:           workqueue.PayloadTypeExecute,
		TaskID:         taskID,
		ExecuteRequest: req,
	})
	if err != nil {
		if closeErr := queue.Close(); closeErr != nil {
			log.Warn(ctx, "queue close failed", "error", closeErr)
		}
		return nil, fmt.Errorf("failed to create work item: %w", err)
	}

	return newRemoteSubscription(queue, m.taskStore, taskID), nil
}

func (m *distributedManager) Cancel(ctx context.Context, req *a2a.CancelTaskRequest) (*a2a.Task, error) {
	storedTask, err := m.taskStore.Get(ctx, req.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to load a task: %w", err)
	}

	task := storedTask.Task
	if task.Status.State == a2a.TaskStateCanceled {
		return task, nil
	}

	if task.Status.State.Terminal() {
		return nil, fmt.Errorf("task in non-cancelable state %q: %w", task.Status.State, a2a.ErrTaskNotCancelable)
	}

	queue, err := m.queueManager.CreateReader(ctx, req.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get or create queue: %w", err)
	}

	if _, err := m.workQueue.Write(ctx, &workqueue.Payload{
		Type:          workqueue.PayloadTypeCancel,
		TaskID:        req.ID,
		CancelRequest: req,
	}); err != nil {
		return nil, fmt.Errorf("failed to create work item: %w", err)
	}

	subscription := newRemoteSubscription(queue, m.taskStore, req.ID)
	var cancelationResult a2a.SendMessageResult
	var cancelationErr error
	for event, err := range subscription.Events(ctx) {
		if err != nil {
			cancelationErr = err
		}
		if taskupdate.IsFinal(event) {
			if result, ok := event.(a2a.SendMessageResult); ok {
				cancelationResult = result
			}
			break
		}
	}
	if cancelationResult == nil && cancelationErr != nil {
		storedTask, err := m.taskStore.Get(ctx, req.ID)
		if err != nil {
			cancelationErr = err
		} else {
			cancelationResult = storedTask.Task
		}
	}
	return convertToCancelationResult(ctx, cancelationResult, cancelationErr)
}
