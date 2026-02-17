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

package eventqueue

import (
	"context"
	"errors"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv/taskstore"
)

// ErrQueueClosed indicates that the event queue has been closed.
var ErrQueueClosed = errors.New("queue is closed")

// Message represents the data broadcasted to event subscribers through event queue.
type Message struct {
	// Event is the event which was applied to task store.
	Event a2a.Event
	// TaskVersion is the version of the task after event was applied.
	TaskVersion taskstore.TaskVersion
	// Protocol is the version of the protocol which emitting process running.
	Protocol a2a.ProtocolVersion
}

// Reader defines the interface for reading events from a queue.
// A2A server stack reads events written by [a2asrv.AgentExecutor].
type Reader interface {
	// Read dequeues an event or blocks if the queue is empty.
	// TaskVersion is expected to be the same as was provided to [Writer.WriteVersioned].
	Read(ctx context.Context) (*Message, error)

	// Close shuts down a connection to the queue.
	Close() error
}

// Writer defines the interface for writing events to a queue.
// [a2asrv.AgentExecutor] translates agent responses to Messages, Tasks or Task update events.
type Writer interface {
	// Write enqueues an event or blocks if a bounded queue is full.
	//
	// Kept to maintain AgentExecutor API until a breaking SDK release.
	// The code other than AgentExecutor must use WriteVersioned.
	Write(ctx context.Context, msg *Message) error

	// Close shuts down a connection to the queue.
	Close() error
}
