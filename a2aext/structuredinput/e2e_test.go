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

package structuredinput_test

import (
	"context"
	"fmt"
	"iter"
	"net/http/httptest"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aext"
	"github.com/a2aproject/a2a-go/v2/a2aext/structuredinput"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/internal/testutil/testexecutor"
	"github.com/google/jsonschema-go/jsonschema"
)

type approval struct {
	Status     string `json:"status"`
	ApproverID string `json:"approver_id"`
}

func approvalExecutor() a2asrv.AgentExecutor {
	schema := &jsonschema.Schema{
		Type: "object",
		Properties: map[string]*jsonschema.Schema{
			"status":      {Type: "string"},
			"approver_id": {Type: "string"},
		},
		Required: []string{"status", "approver_id"},
	}
	return testexecutor.FromFunction(func(ctx context.Context, ec *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
		return func(yield func(a2a.Event, error) bool) {
			if ec.StoredTask == nil {
				if !yield(a2a.NewSubmittedTask(ec, ec.Message), nil) {
					return
				}
				req := &structuredinput.Request{ID: "approval-1", Schema: schema}
				yield(structuredinput.NewRequestEvent(ec, req, a2a.NewTextPart("Please approve the deployment")))
				return
			}

			req, err := structuredinput.PendingRequest(ec.StoredTask)
			if err != nil || req == nil {
				yield(nil, fmt.Errorf("no pending input request: %w", err))
				return
			}
			var processor structuredinput.Processor
			result, err := processor.Process(ctx, req, ec.Message)
			if err != nil {
				yield(nil, err)
				return
			}
			if !result.Accepted() {
				yield(result.NewRetryEvent(ec))
				return
			}
			var got approval
			if err := result.Decode(&got); err != nil {
				yield(nil, err)
				return
			}
			msg := a2a.NewMessageForTask(a2a.MessageRoleAgent, ec, a2a.NewTextPart("approved by "+got.ApproverID))
			yield(a2a.NewStatusUpdateEvent(ec, a2a.TaskStateCompleted, msg), nil)
		}
	})
}

func TestStructuredInputFlow(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	server := httptest.NewServer(a2asrv.NewJSONRPCHandler(a2asrv.NewHandler(approvalExecutor())))
	t.Cleanup(server.Close)
	card := &a2a.AgentCard{
		Capabilities: a2a.AgentCapabilities{
			Extensions: []a2a.AgentExtension{structuredinput.NewExtension(structuredinput.Draft202012)},
		},
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(server.URL, a2a.TransportProtocolJSONRPC),
		},
	}
	client, err := a2aclient.NewFromCard(ctx, card, a2aclient.WithCallInterceptors(a2aext.NewActivator(structuredinput.URI)))
	if err != nil {
		t.Fatalf("a2aclient.NewFromCard() error = %v", err)
	}

	sendMessage := func(msg *a2a.Message) *a2a.Task {
		t.Helper()
		result, err := client.SendMessage(ctx, &a2a.SendMessageRequest{Message: msg})
		if err != nil {
			t.Fatalf("client.SendMessage() error = %v", err)
		}
		task, ok := result.(*a2a.Task)
		if !ok {
			t.Fatalf("client.SendMessage() = %T, want *a2a.Task", result)
		}
		return task
	}

	task := sendMessage(a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("deploy")))
	req, err := structuredinput.PendingRequest(task)
	if err != nil || req == nil {
		t.Fatalf("PendingInputRequest() = (%v, %v), want input request", req, err)
	}
	if req.ID != "approval-1" {
		t.Fatalf("PendingInputRequest() ID = %q, want %q", req.ID, "approval-1")
	}

	msg, err := structuredinput.NewResponse(task, req, map[string]any{"status": "approved"})
	if err != nil {
		t.Fatalf("NewInputMessage() error = %v", err)
	}
	task = sendMessage(msg)
	output, err := structuredinput.ParseValidationOutput(task.Status.Message)
	if err != nil || output == nil {
		t.Fatalf("ParseValidationOutput() = (%v, %v), want validation errors", output, err)
	}
	if retryReq, err := structuredinput.PendingRequest(task); err != nil || retryReq == nil || retryReq.ID != req.ID {
		t.Fatalf("PendingInputRequest() = (%v, %v), want request with ID %q", retryReq, err, req.ID)
	}

	msg, err = structuredinput.NewResponse(task, req, approval{Status: "approved", ApproverID: "alice"})
	if err != nil {
		t.Fatalf("NewInputMessage() error = %v", err)
	}
	task = sendMessage(msg)
	if task.Status.State != a2a.TaskStateCompleted {
		t.Fatalf("client.SendMessage() state = %v, want %v", task.Status.State, a2a.TaskStateCompleted)
	}
	if got := task.Status.Message.Parts[0].Text(); got != "approved by alice" {
		t.Fatalf("client.SendMessage() status text = %q, want %q", got, "approved by alice")
	}
}
