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

// TaskPushConfig is a container associating a push notification configuration with a specific task.
type TaskPushConfig struct {
	// Tenant is an optional ID of the agent owner.
	Tenant string `json:"tenant,omitempty" yaml:"tenant,omitempty" mapstructure:"tenant,omitempty"`

	// Config is the push notification configuration for this task.
	Config PushConfig `json:"config" yaml:"config" mapstructure:"config"`

	// TaskID is the ID of the task.
	TaskID TaskID `json:"taskId" yaml:"taskId" mapstructure:"taskId"`

	// ID is the ID of the push notification configuration.
	ConfigID string `json:"id" yaml:"id" mapstructure:"id"`
}

// GetTaskPushConfigRequest defines request for fetching a specific push notification configuration for a task.
type GetTaskPushConfigRequest struct {
	// Tenant is an optional ID of the agent owner.
	Tenant string `json:"tenant,omitempty" yaml:"tenant,omitempty" mapstructure:"tenant,omitempty"`

	// TaskID is the unique identifier of the parent task.
	TaskID TaskID `json:"taskId" yaml:"taskId" mapstructure:"taskId"`

	// ConfigID is the ID of the push notification configuration to retrieve.
	ConfigID string `json:"id" yaml:"id" mapstructure:"id"`
}

// ListTaskPushConfigRequest defines the request for listing all push notification configurations associated with a task.
type ListTaskPushConfigRequest struct {
	// Tenant is an optional ID of the agent owner.
	Tenant string `json:"tenant,omitempty" yaml:"tenant,omitempty" mapstructure:"tenant,omitempty"`

	// TaskID is the unique identifier of the task.
	TaskID TaskID `json:"taskId" yaml:"taskId" mapstructure:"taskId"`

	// PageSize is the maximum number of push notification configurations to return.
	PageSize int `json:"pageSize,omitempty" yaml:"pageSize,omitempty" mapstructure:"pageSize,omitempty"`

	// PageToken is the token received from the previous ListTaskPushConfigRequest call.
	PageToken string `json:"pageToken,omitempty" yaml:"pageToken,omitempty" mapstructure:"pageToken,omitempty"`
}

type ListTaskPushConfigResponse struct {
	// Configs is a list of push notification configurations for the task.
	Configs []TaskPushConfig `json:"configs" yaml:"configs" mapstructure:"configs"`

	// NextPageToken is the token to use to retrieve the next page of push notification configurations.
	NextPageToken string `json:"nextPageToken,omitempty" yaml:"nextPageToken,omitempty" mapstructure:"nextPageToken,omitempty"`
}

// DeleteTaskPushConfigRequest defines parameters for deleting a specific push notification configuration for a task.
type DeleteTaskPushConfigRequest struct {
	// Tenant is an optional ID of the agent owner.
	Tenant string `json:"tenant,omitempty" yaml:"tenant,omitempty" mapstructure:"tenant,omitempty"`

	// TaskID is the unique identifier of the parent task.
	TaskID TaskID `json:"taskId" yaml:"taskId" mapstructure:"taskId"`

	// ConfigID is the ID of the push notification configuration to delete.
	ConfigID string `json:"id" yaml:"id" mapstructure:"id"`
}

// CreateTaskPushConfigRequest defines request for creating a push notification configuration for a task.
type CreateTaskPushConfigRequest struct {
	// Tenant is an optional ID of the agent owner.
	Tenant string `json:"tenant,omitempty" yaml:"tenant,omitempty" mapstructure:"tenant,omitempty"`

	// Config is the push notification configuration for this task.
	Config PushConfig `json:"config" yaml:"config" mapstructure:"config"`

	// TaskID is the ID of the task.
	TaskID TaskID `json:"taskId" yaml:"taskId" mapstructure:"taskId"`

	// ID is the ID of the push notification configuration.
	ConfigID string `json:"configId" yaml:"configId" mapstructure:"configId"`
}

// PushConfig defines the configuration for setting up push notifications for task updates.
type PushConfig struct {
	// ID is an optional unique ID for the push notification configuration, set by the client
	// to support multiple notification callbacks.
	ID string `json:"id,omitempty" yaml:"id,omitempty" mapstructure:"id,omitempty"`

	// Auth is an optional authentication details for the agent to use when calling the
	// notification URL.
	Auth *PushAuthInfo `json:"authentication,omitempty" yaml:"authentication,omitempty" mapstructure:"authentication,omitempty"`

	// Token is an optional unique token for this task or session to validate incoming push notifications.
	Token string `json:"token,omitempty" yaml:"token,omitempty" mapstructure:"token,omitempty"`

	// URL is the callback URL where the agent should send push notifications.
	URL string `json:"url" yaml:"url" mapstructure:"url"`
}

// PushAuthInfo defines authentication details for a push notification endpoint.
type PushAuthInfo struct {
	// Credentials is an optional credentials required by the push notification endpoint.
	Credentials string `json:"credentials,omitempty" yaml:"credentials,omitempty" mapstructure:"credentials,omitempty"`

	// Scheme is a supported authentication scheme (e.g., 'Basic', 'Bearer').
	Scheme string `json:"scheme" yaml:"scheme" mapstructure:"scheme"`
}
