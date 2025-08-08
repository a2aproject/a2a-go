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

package a2asrv

import (
	"context"
	"iter"

	"github.com/a2aproject/a2a-go/a2a"
)

// RequestHandler defines a transport-agnostic interface for handling incoming A2A requests.
type RequestHandler interface {
	// OnGetTask handles the 'tasks/get' protocol method.
	OnGetTask(ctx context.Context, query a2a.TaskQueryParams) (a2a.Task, error)

	// OnCancelTask handles the 'tasks/cancel' protocol method.
	OnCancelTask(ctx context.Context, id a2a.TaskIDParams) (a2a.Task, error)

	// OnSendMessage handles the 'message/send' protocol method (non-streaming).
	OnSendMessage(ctx context.Context, message a2a.MessageSendParams) (a2a.SendMessageResult, error)

	// OnResubscribeToTask handles the `tasks/resubscribe` protocol method.
	OnResubscribeToTask(ctx context.Context, id a2a.TaskIDParams) iter.Seq2[a2a.Event, error]

	// OnMessageSendStream handles the 'message/stream' protocol method (streaming).
	OnSendMessageStream(ctx context.Context, message a2a.MessageSendParams) iter.Seq2[a2a.Event, error]

	// OnGetTaskPushNotificationConfig handles the `tasks/pushNotificationConfig/get` protocol method.
	OnGetTaskPushConfig(ctx context.Context, params a2a.GetTaskPushConfigParams) (a2a.TaskPushConfig, error)

	// OnListTaskPushNotificationConfig handles the `tasks/pushNotificationConfig/list` protocol method.
	OnListTaskPushConfig(ctx context.Context, params a2a.ListTaskPushConfigParams) ([]a2a.TaskPushConfig, error)

	// OnSetTaskPushConfig handles the `tasks/pushNotificationConfig/set` protocol method.
	OnSetTaskPushConfig(ctx context.Context, params a2a.TaskPushConfig) (a2a.TaskPushConfig, error)

	// OnDeleteTaskPushNotificationConfig handles the `tasks/pushNotificationConfig/delete` protocol method.
	OnDeleteTaskPushConfig(ctx context.Context, params a2a.DeleteTaskPushConfigParams) error
}
