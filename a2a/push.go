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

// Defines parameters for fetching a specific push notification configuration for a task.
type GetTaskPushConfigParams struct {
	// The unique identifier of the task.
	TaskID TaskID

	// An optional ID of the push notification configuration to retrieve.
	ConfigID string
}

// Defines parameters for listing all push notification configurations associated with a task.
type ListTaskPushConfigParams struct {
	// The unique identifier of the task.
	TaskID TaskID
}

// Defines parameters for deleting a specific push notification configuration for a task.
type DeleteTaskPushConfigParams struct {
	// The unique identifier of the task.
	TaskID TaskID

	// The ID of the push notification configuration to delete.
	ConfigID string
}

// A container associating a push notification configuration with a specific task.
type TaskPushConfig struct {
	// The push notification configuration for this task.
	Config *PushConfig

	// The ID of the task.
	TaskID TaskID
}

// Defines the configuration for setting up push notifications for task updates.
type PushConfig struct {
	// An optional unique ID for the push notification configuration, set by the client
	// to support multiple notification callbacks.
	ID string

	// Optional authentication details for the agent to use when calling the
	// notification URL.
	Auth *PushAuthInfo

	// An optional unique token for this task or session to validate incoming push
	// notifications.
	Token string

	// The callback URL where the agent should send push notifications.
	URL string
}

// Defines authentication details for a push notification endpoint.
type PushAuthInfo struct {
	// Optional credentials required by the push notification endpoint.
	Credentials string

	// A list of supported authentication schemes (e.g., 'Basic', 'Bearer').
	Schemes []string
}
