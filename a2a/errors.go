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

package a2a

import "errors"

var (
	// ErrTaskNotFound indicates that a task with the provided ID was not found.
	ErrTaskNotFound = errors.New("task not found")

	// ErrTaskNotCancelable indicates that the task was in a state where it could not be canceled.
	ErrTaskNotCancelable = errors.New("task cannot be canceled")

	// ErrPushNotificationNotSupported indicates that the agent does not support push notifications.
	ErrPushNotificationNotSupported = errors.New("push notification not supported")

	// ErrUnsupportedOperation indicates that the requested operation is not supported by the agent.
	ErrUnsupportedOperation = errors.New("this operation is not supported")

	// ErrUnsupportedContentType indicates an incompatibility between the requested
	// content types and the agent's capabilities.
	ErrUnsupportedContentType = errors.New("incompatible content types")

	// ErrInvalidAgentResponse indicates that the agent returned a response that
	// does not conform to the specification for the current method.
	ErrInvalidAgentResponse = errors.New("invalid agent response")

	// ErrInvalidRequest indicates that the received request was invalid.
	ErrInvalidRequest = errors.New("invalid request")

	// ErrAuthenticatedExtendedCardNotConfigured indicates that the agent does not have an Authenticated
	// Extended Card configured.
	ErrAuthenticatedExtendedCardNotConfigured = errors.New("extended card not configured")
)
