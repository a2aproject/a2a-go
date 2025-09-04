package taskexec

import (
	"context"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
)

type Processor interface {
	// Process is called for each event produced by the started Execution.
	// Execution finishes when either a non-nil result or a non-nil error is returned.
	// the terminal value becomes the result of the execution.
	// Called in a separate goroutine.
	Process(context.Context, a2a.Event) (*a2a.SendMessageResult, error)
}

type Executor interface {
	Processor
	// Start starts publishing events to the queue. Called in a separate goroutine.
	Execute(context.Context, eventqueue.Queue) error
}

type Canceller interface {
	Processor
	// Cancel attempts to cancel a Task.
	// Expected to produce a Task update event with cancelled state.
	Cancel(context.Context, eventqueue.Queue) error
}
