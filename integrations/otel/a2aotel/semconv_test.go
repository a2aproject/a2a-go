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

package a2aotel

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
		{sdkMethod: "SendMessage", want: "SendMessage"},
		{sdkMethod: "CreateTaskPushConfig", want: "CreateTaskPushNotificationConfig"},
		{sdkMethod: "GetTaskPushConfig", want: "GetTaskPushNotificationConfig"},
		{sdkMethod: "ListTaskPushConfigs", want: "ListTaskPushNotificationConfigs"},
		{sdkMethod: "DeleteTaskPushConfig", want: "DeleteTaskPushNotificationConfig"},
		{sdkMethod: "CustomMethod", want: "CustomMethod"},
	}
	for _, tc := range testCases {
		t.Run(tc.sdkMethod, func(t *testing.T) {
			t.Parallel()
			if got := methodName(tc.sdkMethod); got != tc.want {
				t.Fatalf("methodName(%q) = %q, want %q", tc.sdkMethod, got, tc.want)
			}
		})
	}
}

func TestClientErrorType(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "sentinel",
			err:  a2a.ErrTaskNotFound,
			want: "TASK_NOT_FOUND",
		},
		{
			name: "wrapped sentinel",
			err:  fmt.Errorf("loading task: %w", a2a.ErrTaskNotCancelable),
			want: "TASK_NOT_CANCELABLE",
		},
		{
			name: "a2a error",
			err:  a2a.NewError(a2a.ErrUnsupportedOperation, "not supported"),
			want: "UNSUPPORTED_OPERATION",
		},
		{
			name: "internal error",
			err:  a2a.ErrInternalError,
			want: "INTERNAL_ERROR",
		},
		{
			name: "a2a error without a known cause",
			err:  &a2a.Error{Message: "failed"},
			want: "INTERNAL_ERROR",
		},
		{
			name: "transport error",
			err:  &url.Error{Op: "Post", URL: "http://localhost", Err: errors.New("connection refused")},
			want: "*url.Error",
		},
		{
			name: "context error",
			err:  fmt.Errorf("sending request: %w", context.DeadlineExceeded),
			want: "context.deadlineExceededError",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := clientErrorType(tc.err); got != tc.want {
				t.Fatalf("clientErrorType(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}
