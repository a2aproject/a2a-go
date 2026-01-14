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

package workqueue

import (
	"context"
	"errors"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv/limiter"
)

// ErrMalformedPayload is a non-retryable error which can be returned by [HandlerFn].
var ErrMalformedPayload = errors.New("malformed payload")

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

// HandlerFn starts agent execution for the provided payload.
type HandlerFn func(context.Context, *Payload) (a2a.SendMessageResult, error)

// Writer is used by executor frontend to submit work for execution.
type Writer interface {
	// Write puts a new payload into the queue. Paylod will contain a TaskID but a different value can be returned to handle idempotency.
	Write(context.Context, *Payload) (a2a.TaskID, error)
}

// Queue is an interface for the work distribution component.
// Executor backend registers itself using RegisterHandler when RequestHandler is created.
// HandlerFn can be used by work queue implementations to start execution when works is received.
type Queue interface {
	Writer

	// RegisterHandler registers an executor. This method is called by the SDK.
	RegisterHandler(limiter.ConcurrencyConfig, HandlerFn)
}
