package workqueue

import (
	"context"
	"errors"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
)

var ErrQueueClosed = errors.New("queue closed")

type PayloadType string

const (
	PayloadTypeExecute = "execute"
	PayloadTypeCancel  = "cancel"
)

type Payload struct {
	Type          PayloadType
	TaskID        a2a.TaskID
	CancelParams  *a2a.TaskIDParams
	ExecuteParams *a2a.MessageSendParams
}

type Message interface {
	Payload() *Payload

	Complete(ctx context.Context, result a2a.SendMessageResult) error

	Return(ctx context.Context, cause error) error
}

type Hearbeater interface {
	HeartbeatInterval() time.Duration

	Hearbeat(ctx context.Context) error
}

type Queue interface {
	Write(context.Context, *Payload) error

	Read(context.Context) (Message, error)
}
