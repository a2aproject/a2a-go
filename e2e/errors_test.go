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

package e2e_test

import (
	"context"
	"errors"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/a2asrv/limiter"
	"github.com/a2aproject/a2a-go/v2/internal/testutil"
	"github.com/a2aproject/a2a-go/v2/internal/testutil/testexecutor"
)

func TestCallerMistakeErrors(t *testing.T) {
	t.Parallel()
	completedTask := &a2a.Task{ID: a2a.NewTaskID(), ContextID: a2a.NewContextID(), Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}}
	send := func(ctx context.Context, client *a2aclient.Client, taskID a2a.TaskID, contextID string) error {
		msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hi"))
		msg.TaskID, msg.ContextID = taskID, contextID
		_, err := client.SendMessage(ctx, &a2a.SendMessageRequest{Message: msg})
		return err
	}

	tests := []struct {
		name    string
		opts    []a2asrv.RequestHandlerOption
		call    func(ctx context.Context, client *a2aclient.Client, running *a2asrv.ExecutorContext) error
		wantErr error
		wantMsg string
	}{
		{
			name: "send to a completed task",
			call: func(ctx context.Context, client *a2aclient.Client, _ *a2asrv.ExecutorContext) error {
				return send(ctx, client, completedTask.ID, "")
			},
			wantErr: a2a.ErrUnsupportedOperation,
			wantMsg: "task is in a terminal state (TASK_STATE_COMPLETED) and accepts no more messages",
		},
		{
			name: "send to an unknown task",
			call: func(ctx context.Context, client *a2aclient.Client, _ *a2asrv.ExecutorContext) error {
				return send(ctx, client, a2a.NewTaskID(), "")
			},
			wantErr: a2a.ErrTaskNotFound,
			wantMsg: "task not found",
		},
		{
			name: "send with another context",
			call: func(ctx context.Context, client *a2aclient.Client, _ *a2asrv.ExecutorContext) error {
				return send(ctx, client, completedTask.ID, "other")
			},
			wantErr: a2a.ErrInvalidParams,
			wantMsg: "message contextId differs from the contextId of the task",
		},
		{
			name: "cancel an unknown task",
			call: func(ctx context.Context, client *a2aclient.Client, _ *a2asrv.ExecutorContext) error {
				_, err := client.CancelTask(ctx, &a2a.CancelTaskRequest{ID: a2a.NewTaskID()})
				return err
			},
			wantErr: a2a.ErrTaskNotFound,
			wantMsg: "task not found",
		},
		{
			name: "cancel a completed task",
			call: func(ctx context.Context, client *a2aclient.Client, _ *a2asrv.ExecutorContext) error {
				_, err := client.CancelTask(ctx, &a2a.CancelTaskRequest{ID: completedTask.ID})
				return err
			},
			wantErr: a2a.ErrTaskNotCancelable,
			wantMsg: "task is in a terminal state (TASK_STATE_COMPLETED) and can't be canceled",
		},
		{
			name: "get an unknown task",
			call: func(ctx context.Context, client *a2aclient.Client, _ *a2asrv.ExecutorContext) error {
				_, err := client.GetTask(ctx, &a2a.GetTaskRequest{ID: a2a.NewTaskID()})
				return err
			},
			wantErr: a2a.ErrTaskNotFound,
			wantMsg: "task not found",
		},
		{
			name: "send while the task is processing a message",
			call: func(ctx context.Context, client *a2aclient.Client, running *a2asrv.ExecutorContext) error {
				return send(ctx, client, running.TaskID, "")
			},
			wantErr: a2a.ErrUnsupportedOperation,
			wantMsg: "task execution is already in progress",
		},
		{
			name: "send over the concurrency limit",
			opts: []a2asrv.RequestHandlerOption{a2asrv.WithConcurrencyConfig(limiter.ConcurrencyConfig{MaxExecutions: 1})},
			call: func(ctx context.Context, client *a2aclient.Client, _ *a2asrv.ExecutorContext) error {
				return send(ctx, client, "", "")
			},
			wantErr: a2a.ErrInternalError,
			wantMsg: "too many tasks in progress, try again later",
		},
	}
	transports := []a2a.TransportProtocol{a2a.TransportProtocolJSONRPC, a2a.TransportProtocolHTTPJSON, a2a.TransportProtocolGRPC}
	for _, transport := range transports {
		for _, tc := range tests {
			t.Run(string(transport)+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				ctx := t.Context()
				store := testutil.NewTestTaskStore().WithTasks(t, completedTask)
				executor, execChannels := testexecutor.NewWithControlChannels()
				opts := append([]a2asrv.RequestHandlerOption{a2asrv.WithTaskStore(store)}, tc.opts...)
				client := startTransportServer(t, a2asrv.NewHandler(executor, opts...), transport)

				events, drain := sendMessageInBackground(t, client)
				running := <-execChannels.ReqCtx
				execChannels.ExecEvent <- a2a.NewSubmittedTask(running, running.Message)
				<-events
				defer func() {
					execChannels.ExecEvent <- a2a.NewStatusUpdateEvent(running, a2a.TaskStateCompleted, nil)
					drain()
				}()

				err := tc.call(ctx, client, running)

				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("client call error = %v, want %v", err, tc.wantErr)
				}
				if err.Error() != tc.wantMsg {
					t.Fatalf("client call error message = %q, want %q", err.Error(), tc.wantMsg)
				}
			})
		}
	}
}
