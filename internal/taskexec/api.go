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
	"iter"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
)

// Manager provides an API for executing and canceling tasks.
type Manager interface {
	// Resubscribe is used to resubscribe to events of an active execution.
	Resubscribe(ctx context.Context, taskID a2a.TaskID) (Subscription, error)
	// Execute requests an execution for handling a received message.
	Execute(ctx context.Context, params *a2a.MessageSendParams) (Subscription, error)
	// Cancel requests a task cancelation.
	Cancel(ctx context.Context, params *a2a.TaskIDParams) (*a2a.Task, error)
}

// TaskStore is a dependency required for loading latest task snapshots.
type TaskStore interface {
	Get(context.Context, a2a.TaskID) (*a2a.Task, a2a.TaskVersion, error)
}

// Subscription encapsulates the logic of subscribing to execution events.
type Subscription interface {
	// TaskID is ID of the task to which this subscription is related.
	TaskID() a2a.TaskID
	// Events returns a sequence of events. If error is returned the sequence is terminated.
	// This method can only be called once.
	Events(ctx context.Context) iter.Seq2[a2a.Event, error]
}

// Factory is used to setup task execution or cancelation context.
type Factory interface {
	// CreateExecutor is used to create initialized Executor and Processor for a Task execution which will run in separate goroutines.
	CreateExecutor(context.Context, a2a.TaskID, *a2a.MessageSendParams) (Executor, Processor, error)

	// CreateCanceler is used to create initialized Canceler and Processor for a Task cancelation which will run in separate goroutines.
	CreateCanceler(context.Context, *a2a.TaskIDParams) (Canceler, Processor, error)
}

// Processor implementation handles events produced during AgentExecution.
type Processor interface {
	// Process is called for each event produced by the started Execution. Called in a separate goroutine.
	Process(context.Context, a2a.Event) (*ProcessorResult, error)

	// ProcessError is called when an execution error is encountered to try recovering from it.
	// If it returns a result, the returned value becomes the result of the execution. If an error can't be handled
	// either a modified error or the original error cause must be returned.
	ProcessError(context.Context, error) (a2a.SendMessageResult, error)
}

// ProcessorResult is returned by processor after an event was handled successfuly.
type ProcessorResult struct {
	// ExecutionResult becomes the result of the execution if a non-nil value is returned.
	ExecutionResult a2a.SendMessageResult
	// TaskVersion is the version of the task after the event was processed.
	TaskVersion a2a.TaskVersion
}

// Executor implementation starts an agent execution.
type Executor interface {
	// Start starts publishing events to the queue. Called in a separate goroutine.
	Execute(context.Context, eventqueue.Queue) error
}

// Canceler implementation sends a Task cancelation signal.
type Canceler interface {
	// Cancel attempts to cancel a Task.
	// Expected to produce a Task update event with canceled state.
	Cancel(context.Context, eventqueue.Queue) error
}
