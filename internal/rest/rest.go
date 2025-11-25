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

package rest

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/a2aproject/a2a-go/a2a"
)

type Error struct {
	Type 	string 	`json:"type"`
	Title 	string 	`json:"title"`
	Status 	int  	`json:"status"`
	Detail 	string 	`json:"detail"`
}

// ToA2AError returns A2A error  based on HTTP status codes and messages
func ToA2AError(resp *http.Response) error {
	contentType := resp.Header.Get("Content-Type")
	if contentType != "application/problem+json" {
		return a2a.ErrServerError
	}
	
	var e Error
	if err := json.NewDecoder(resp.Body).Decode(&e); err != nil {
		return fmt.Errorf("failed to decode error details: %w", err)
	}

	var A2AError error

	switch e.Type {
	case "https://a2a-protocol.org/errors/task-not-found":
		A2AError = a2a.ErrTaskNotFound
	case "https://a2a-protocol.org/errors/task-not-cancelable":
		A2AError = a2a.ErrTaskNotCancelable
	case "https://a2a-protocol.org/errors/push-notification-not-supported":
		A2AError = a2a.ErrPushNotificationNotSupported
	case "https://a2a-protocol.org/errors/unsupported-operation":
		A2AError = a2a.ErrUnsupportedOperation
	case "https://a2a-protocol.org/errors/content-type-not-supported":
		A2AError = a2a.ErrUnsupportedContentType
	case "https://a2a-protocol.org/errors/invalid-agent-response":
		A2AError = a2a.ErrInvalidAgentResponse
	case "https://a2a-protocol.org/errors/extended-agent-card-not-configured":
		A2AError = a2a.ErrAuthenticatedExtendedCardNotConfigured
	case "https://a2a-protocol.org/errors/extension-support-required":
		A2AError = a2a.ErrExtensionSupportRequired
	case "https://a2a-protocol.org/errors/version-not-supported":
		A2AError = a2a.ErrVersionNotSupported
	default:
		return a2a.ErrServerError
	}
	
	return fmt.Errorf("%s: %w", e.Detail, A2AError)
}



