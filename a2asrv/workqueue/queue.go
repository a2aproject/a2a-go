package workqueue

import (
	"context"
	"errors"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
)

// ErrQueueClosed is returned when a queue is closed. This will stop execution backend.
var ErrQueueClosed = errors.New("queue closed")

// PayloadType defines the type of payload.
type PayloadType string

const (
	// PayloadTypeExecute defines the payload type for execution.
	PayloadTypeExecute = "execute"
	// PayloadTypeCancel defines the payload type for cancelation.
	PayloadTypeCancel = "cancel"
)

// Payload defines the payload for execution or cancelation.
type Payload struct {
	// Type defines the type of payload.
	Type PayloadType
	// TaskID is an ID of the task to execute or cancel.
	TaskID a2a.TaskID
	// CancelParams defines the cancelation parameters. It is only set for [PayloadTypeCancel].
	CancelParams *a2a.TaskIDParams
	// ExecuteParams defines the execution parameters. It is only set for [PayloadTypeExecute].
	ExecuteParams *a2a.MessageSendParams
}

// Message defines the message for execution or cancelation.
type Message interface {
	// Payload returns the payload of the message.
	Payload() *Payload
	// Complete marks the message as completed after it was handled by a worker.
	Complete(ctx context.Context, result a2a.SendMessageResult) error
	// Return returns the message to the queue after worker failed to handle it.
	Return(ctx context.Context, cause error) error
}

// Heartbeater defines an optional heartbeat policy for message handler. Heartbeats are sent according
// to it while worker is handling a message if the message implements this interface.
type Heartbeater interface {
	// HeartbeatInterval returns the interval between heartbeats.
	HeartbeatInterval() time.Duration
	// Heartbeat is called at heartbeat interval to mark the message as still being handled.
	Heartbeat(ctx context.Context) error
}

// Queue is an interface for the work distribution component.
type Queue interface {
	// Write puts a new payload into the queue. It can return a TaskID to handle idempotency.
	Write(context.Context, *Payload) (a2a.TaskID, error)
	// Read returns a new message from the queue.
	Read(context.Context) (Message, error)
}
