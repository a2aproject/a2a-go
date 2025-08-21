// Copyright 2025 The A2A Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package events

import (
	"context"

	"github.com/a2aproject/a2a-go/a2a"
)

// EventReader defines the interface for reading events from a queue.
// A2A server stack reads events written by AgentExecutor.
type EventReader interface {
	// Read dequeues an event or blocks if the queue is empty.
	Read(ctx context.Context) (a2a.Event, error)
}

// EventWriter defines the interface for writing events to a queue.
// AgentExecutor translates agent responses to Messages, Tasks or Task update events.
type EventWriter interface {
	// Write enqueues an event or blocks if a bounded queue is full.
	Write(ctx context.Context, event a2a.Event) error
}

// EventQueue defines the interface for publishing and consuming
// events generated during agent execution.
type EventQueue interface {
	EventReader
	EventWriter

	// Close shuts down a connection to the queue.
	Close() error
}

// EventQueueManager manages event queues on a per-task basis.
// It provides lifecycle management for task-specific event queues,
// enabling multiple clients to attach to the same task's event stream.
type EventQueueManager interface {
	// GetOrCreate returns an existing queue if one exists, or creates a new one.
	GetOrCreate(ctx context.Context, taskId a2a.TaskID) (EventQueue, error)

	// Destroy closes the queue for the specified task and frees all associates resources.
	Destroy(ctx context.Context, taskId a2a.TaskID) error
}
