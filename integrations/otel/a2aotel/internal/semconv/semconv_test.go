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

package semconv

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

func TestMethodName(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		sdkMethod string
		want      string
	}{
		{sdkMethod: "SendMessage", want: MethodSendMessage},
		{sdkMethod: "SendStreamingMessage", want: MethodSendStreamingMessage},
		{sdkMethod: "GetTask", want: MethodGetTask},
		{sdkMethod: "ListTasks", want: MethodListTasks},
		{sdkMethod: "CancelTask", want: MethodCancelTask},
		{sdkMethod: "SubscribeToTask", want: MethodSubscribeToTask},
		{sdkMethod: "CreateTaskPushConfig", want: MethodCreateTaskPushNotificationConfig},
		{sdkMethod: "GetTaskPushConfig", want: MethodGetTaskPushNotificationConfig},
		{sdkMethod: "ListTaskPushConfigs", want: MethodListTaskPushNotificationConfigs},
		{sdkMethod: "DeleteTaskPushConfig", want: MethodDeleteTaskPushNotificationConfig},
		{sdkMethod: "GetExtendedAgentCard", want: MethodGetExtendedAgentCard},
		{sdkMethod: "CustomMethod", want: "CustomMethod"},
	}
	for _, tc := range testCases {
		t.Run(tc.sdkMethod, func(t *testing.T) {
			t.Parallel()
			if got := MethodName(tc.sdkMethod); got != tc.want {
				t.Fatalf("MethodName(%q) = %q, want %q", tc.sdkMethod, got, tc.want)
			}
		})
	}
}

func TestTaskState(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		state a2a.TaskState
		want  string
	}{
		{state: a2a.TaskStateWorking, want: "TASK_STATE_WORKING"},
		{state: a2a.TaskStateUnspecified, want: "TASK_STATE_UNSPECIFIED"},
	}
	for _, tc := range testCases {
		if got := TaskState(tc.state); got != tc.want {
			t.Fatalf("TaskState(%q) = %q, want %q", tc.state, got, tc.want)
		}
	}
}

func TestErrorType(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name       string
		err        error
		wantClient string
		wantServer string
	}{
		{
			name:       "sentinel",
			err:        a2a.ErrTaskNotFound,
			wantClient: "TASK_NOT_FOUND",
			wantServer: "TASK_NOT_FOUND",
		},
		{
			name:       "wrapped sentinel",
			err:        fmt.Errorf("loading task: %w", a2a.ErrTaskNotCancelable),
			wantClient: "TASK_NOT_CANCELABLE",
			wantServer: "TASK_NOT_CANCELABLE",
		},
		{
			name:       "a2a error",
			err:        a2a.NewError(a2a.ErrUnsupportedOperation, "not supported"),
			wantClient: "UNSUPPORTED_OPERATION",
			wantServer: "UNSUPPORTED_OPERATION",
		},
		{
			name:       "internal error",
			err:        a2a.ErrInternalError,
			wantClient: "INTERNAL_ERROR",
			wantServer: "INTERNAL_ERROR",
		},
		{
			name:       "a2a error without a known cause",
			err:        &a2a.Error{Message: "failed"},
			wantClient: "INTERNAL_ERROR",
			wantServer: "INTERNAL_ERROR",
		},
		{
			name:       "transport error",
			err:        &url.Error{Op: "Post", URL: "http://localhost", Err: errors.New("connection refused")},
			wantClient: "*url.Error",
			wantServer: "INTERNAL_ERROR",
		},
		{
			name:       "context error",
			err:        fmt.Errorf("sending request: %w", context.DeadlineExceeded),
			wantClient: "context.deadlineExceededError",
			wantServer: "INTERNAL_ERROR",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ClientErrorType(tc.err); got != tc.wantClient {
				t.Fatalf("ClientErrorType(%v) = %q, want %q", tc.err, got, tc.wantClient)
			}
			if got := ServerErrorType(tc.err); got != tc.wantServer {
				t.Fatalf("ServerErrorType(%v) = %q, want %q", tc.err, got, tc.wantServer)
			}
		})
	}
}
