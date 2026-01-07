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
	"github.com/a2aproject/a2a-go/a2asrv/workqueue"
	"github.com/a2aproject/a2a-go/log"
)

type TaskStore interface {
	Get(context.Context, a2a.TaskID) (*a2a.Task, a2a.TaskVersion, error)
}

// ClusterConfig contains configuration for A2A task execution mode where work is distributed across A2A cluster.
type ClusterConfig struct {
	WorkQueue         workqueue.Queue
	QueueManager      eventqueue.Manager
	Factory           Factory
	TaskStore         TaskStore
	ConcurrencyConfig limiter.ConcurrencyConfig
	Logger            *slog.Logger
}

type clusterFrontend struct {
	backend      *clusterBackend
	workQueue    workqueue.Queue
	queueManager eventqueue.Manager
	taskStore    TaskStore
}

var _ Manager = (*clusterFrontend)(nil)

// NewClusterFrontend creates a new [Manager] instance which uses WorkQueue for work distribution across A2A cluster.
func NewClusterFrontend(cfg *ClusterConfig) Manager {
	frontend := &clusterFrontend{
		backend:      newClusterBackend(cfg),
		queueManager: cfg.QueueManager,
		workQueue:    cfg.WorkQueue,
		taskStore:    cfg.TaskStore,
	}
	return frontend
}

func (m *clusterFrontend) GetExecution(ctx context.Context, taskID a2a.TaskID) (Execution, bool) {
	if _, _, err := m.taskStore.Get(ctx, taskID); err != nil {
		return nil, false
	}
	return newRemoteExecution(m.queueManager, m.taskStore, taskID), true
}

func (m *clusterFrontend) Execute(ctx context.Context, params *a2a.MessageSendParams) (Execution, Subscription, error) {
	if params == nil || params.Message == nil {
		return nil, nil, fmt.Errorf("message is required: %w", a2a.ErrInvalidParams)
	}

	var taskID a2a.TaskID
	if len(params.Message.TaskID) == 0 {
		taskID = a2a.NewTaskID()
	} else {
		taskID = params.Message.TaskID
	}

	msg := params.Message
	if msg.TaskID != "" {
		storedTask, _, err := m.taskStore.Get(ctx, msg.TaskID)
		if err != nil {
			return nil, nil, fmt.Errorf("task loading failed: %w", err)
		}
		if storedTask == nil {
			return nil, nil, a2a.ErrTaskNotFound
		}

		if msg.ContextID != "" && msg.ContextID != storedTask.ContextID {
			return nil, nil, fmt.Errorf("message contextID different from task contextID: %w", a2a.ErrInvalidParams)
		}

		if storedTask.Status.State.Terminal() {
			return nil, nil, fmt.Errorf("task in a terminal state %q: %w", storedTask.Status.State, a2a.ErrInvalidParams)
		}
	}

	queue, err := m.queueManager.GetOrCreate(ctx, taskID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get or create queue: %w", err)
	}

	taskID, err = m.workQueue.Write(ctx, &workqueue.Payload{
		Type:          workqueue.PayloadTypeExecute,
		TaskID:        taskID,
		ExecuteParams: params,
	})
	if err != nil {
		if closeErr := queue.Close(); closeErr != nil {
			log.Warn(ctx, "queue close failed", "error", closeErr)
		}
		return nil, nil, fmt.Errorf("failed to create work item: %w", err)
	}

	execution := newRemoteExecution(m.queueManager, m.taskStore, taskID)
	subscription := newRemoteSubscription(queue, m.taskStore, taskID)
	return execution, subscription, nil
}

func (m *clusterFrontend) Cancel(ctx context.Context, params *a2a.TaskIDParams) (*a2a.Task, error) {
	task, _, err := m.taskStore.Get(ctx, params.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to load a task: %w", err)
	}

	if task.Status.State == a2a.TaskStateCanceled {
		return task, nil
	}

	if task.Status.State.Terminal() {
		return nil, fmt.Errorf("task in non-cancelable state %q: %w", task.Status.State, a2a.ErrTaskNotCancelable)
	}

	if _, err := m.workQueue.Write(ctx, &workqueue.Payload{
		Type:         workqueue.PayloadTypeCancel,
		TaskID:       params.ID,
		CancelParams: params,
	}); err != nil {
		return nil, fmt.Errorf("failed to create work item: %w", err)
	}

	execution := newRemoteExecution(m.queueManager, m.taskStore, params.ID)
	result, err := execution.Result(ctx)
	return convertToCancelationResult(ctx, result, err)
}
