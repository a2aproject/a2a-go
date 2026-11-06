// Copyright 2026 The A2A Authors
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
	"errors"
	"fmt"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

var (
	// ErrExecutionInProgress is returned when a caller attempts to start an execution for
	// a Task concurrently with another execution.
	ErrExecutionInProgress = a2a.NewError(a2a.ErrUnsupportedOperation, "task execution is already in progress")
	// ErrCancelationInProgress is returned when a caller attempts to start an execution for
	// a Task concurrently with its cancelation.
	ErrCancelationInProgress = a2a.NewError(a2a.ErrUnsupportedOperation, "task cancelation is in progress")
	// ErrQuotaExceeded is returned when a concurrency limit doesn't allow another execution.
	ErrQuotaExceeded = a2a.NewError(a2a.ErrInternalError, "too many tasks in progress, try again later")
)

// TaskLoadError returns the error of loading a task a caller referenced: a
// not found error without the store's details, or err with context otherwise.
func TaskLoadError(err error) error {
	if errors.Is(err, a2a.ErrTaskNotFound) {
		return a2a.NewError(a2a.ErrTaskNotFound, "task not found")
	}
	return fmt.Errorf("failed to load a task: %w", err)
}

// ErrContextMismatch is returned for a message whose context ID differs from the context ID of its task.
var ErrContextMismatch = a2a.NewError(a2a.ErrInvalidParams, "message contextId differs from the contextId of the task")

// TerminalTaskError returns the error of a message sent to a task in a terminal state.
func TerminalTaskError(state a2a.TaskState) error {
	return a2a.NewError(a2a.ErrUnsupportedOperation, fmt.Sprintf("task is in a terminal state (%s) and accepts no more messages", state))
}

// NotCancelableError returns the error of canceling a task in a terminal state.
func NotCancelableError(state a2a.TaskState) error {
	return a2a.NewError(a2a.ErrTaskNotCancelable, fmt.Sprintf("task is in a terminal state (%s) and can't be canceled", state))
}
