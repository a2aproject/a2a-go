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
	"slices"
	"sync"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aext"
	"github.com/a2aproject/a2a-go/v2/a2aext/structuredinput"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/internal/testutil/testexecutor"
	"github.com/google/go-cmp/cmp"
	"github.com/google/jsonschema-go/jsonschema"
)

func chargeSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Type: "object",
		Properties: map[string]*jsonschema.Schema{
			"status":      {Type: "string"},
			"approver_id": {Type: "string"},
		},
		Required: []string{"status", "approver_id"},
	}
}

type answerLog struct {
	mu      sync.Mutex
	answers []*a2a.Message
}

func (l *answerLog) add(msg *a2a.Message) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.answers = append(l.answers, msg)
}

func (l *answerLog) all() []*a2a.Message {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.answers)
}

func airlineExecutor(received *answerLog) a2asrv.AgentExecutor {
	return testexecutor.FromFunction(func(ctx context.Context, ec *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
		return func(yield func(a2a.Event, error) bool) {
			if ec.StoredTask == nil {
				if !yield(a2a.NewSubmittedTask(ec, ec.Message), nil) {
					return
				}
				req := &structuredinput.Request{
					ID:         "charge-1",
					Schema:     chargeSchema(),
					AnswerFrom: structuredinput.AnswerSourcePerson,
				}
				yield(structuredinput.NewRequestEvent(ec, req, a2a.NewTextPart("Approve the charge of 240 dollars?")))
				return
			}

			received.add(ec.Message)
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
				yield(result.NewRetryEvent(ec, a2a.NewTextPart("Approve the charge of 240 dollars?")))
				return
			}
			var got approval
			if err := result.Decode(&got); err != nil {
				yield(nil, err)
				return
			}
			msg := a2a.NewMessageForTask(a2a.MessageRoleAgent, ec, a2a.NewTextPart("charged, approved by "+got.ApproverID))
			yield(a2a.NewStatusUpdateEvent(ec, a2a.TaskStateCompleted, msg), nil)
		}
	})
}

func travelExecutor(airline *a2aclient.Client) a2asrv.AgentExecutor {
	var mu sync.Mutex
	airlineTasks := map[a2a.TaskID]*a2a.Task{}

	callAirline := func(ctx context.Context, msg *a2a.Message) (*a2a.Task, error) {
		result, err := airline.SendMessage(ctx, &a2a.SendMessageRequest{Message: msg})
		if err != nil {
			return nil, err
		}
		task, ok := result.(*a2a.Task)
		if !ok {
			return nil, fmt.Errorf("airline returned %T, want *a2a.Task", result)
		}
		return task, nil
	}

	return testexecutor.FromFunction(func(ctx context.Context, ec *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
		return func(yield func(a2a.Event, error) bool) {
			if ec.StoredTask == nil {
				if !yield(a2a.NewSubmittedTask(ec, ec.Message), nil) {
					return
				}
				airlineTask, err := callAirline(ctx, a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("book the flight")))
				if err != nil {
					yield(nil, err)
					return
				}
				received, err := structuredinput.PendingRequest(airlineTask)
				if err != nil || received == nil {
					yield(nil, fmt.Errorf("airline has no pending input request: %w", err))
					return
				}
				relayed, err := structuredinput.RelayRequest(received, "relay-1", "airline")
				if err != nil {
					yield(nil, err)
					return
				}
				mu.Lock()
				airlineTasks[ec.TaskID] = airlineTask
				mu.Unlock()
				yield(structuredinput.NewRequestEvent(ec, relayed, a2a.NewTextPart("The airline asks: Approve the charge of 240 dollars?")))
				return
			}

			relayed, err := structuredinput.PendingRequest(ec.StoredTask)
			if err != nil || relayed == nil {
				yield(nil, fmt.Errorf("no pending input request: %w", err))
				return
			}
			var processor structuredinput.Processor
			result, err := processor.Process(ctx, relayed, ec.Message)
			if err != nil {
				yield(nil, err)
				return
			}
			if !result.Accepted() {
				yield(result.NewRetryEvent(ec, a2a.NewTextPart("The airline needs an answer from a person.")))
				return
			}

			mu.Lock()
			airlineTask := airlineTasks[ec.TaskID]
			mu.Unlock()
			received, err := structuredinput.PendingRequest(airlineTask)
			if err != nil || received == nil {
				yield(nil, fmt.Errorf("airline has no pending input request: %w", err))
				return
			}
			answer, err := structuredinput.RelayResponse(airlineTask, received, result)
			if err != nil {
				yield(nil, err)
				return
			}
			airlineTask, err = callAirline(ctx, answer)
			if err != nil {
				yield(nil, err)
				return
			}
			if airlineTask.Status.State != a2a.TaskStateCompleted {
				yield(nil, fmt.Errorf("airline task state = %v, want %v", airlineTask.Status.State, a2a.TaskStateCompleted))
				return
			}
			msg := a2a.NewMessageForTask(a2a.MessageRoleAgent, ec, a2a.NewTextPart(airlineTask.Status.Message.Parts[0].Text()))
			yield(a2a.NewStatusUpdateEvent(ec, a2a.TaskStateCompleted, msg), nil)
		}
	})
}

func startAgent(t *testing.T, executor a2asrv.AgentExecutor) *a2aclient.Client {
	t.Helper()
	server := httptest.NewServer(a2asrv.NewJSONRPCHandler(a2asrv.NewHandler(executor)))
	t.Cleanup(server.Close)
	card := &a2a.AgentCard{
		Capabilities: a2a.AgentCapabilities{
			Extensions: []a2a.AgentExtension{structuredinput.NewExtension(structuredinput.Draft202012)},
		},
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(server.URL, a2a.TransportProtocolJSONRPC),
		},
	}
	client, err := a2aclient.NewFromCard(t.Context(), card, a2aclient.WithCallInterceptors(a2aext.NewActivator(structuredinput.URI)))
	if err != nil {
		t.Fatalf("a2aclient.NewFromCard() error = %v", err)
	}
	return client
}

func sendForTask(t *testing.T, client *a2aclient.Client, msg *a2a.Message) *a2a.Task {
	t.Helper()
	result, err := client.SendMessage(t.Context(), &a2a.SendMessageRequest{Message: msg})
	if err != nil {
		t.Fatalf("client.SendMessage() error = %v", err)
	}
	task, ok := result.(*a2a.Task)
	if !ok {
		t.Fatalf("client.SendMessage() = %T, want *a2a.Task", result)
	}
	return task
}

func TestPersonAnswerNotAcceptedFromAgent(t *testing.T) {
	t.Parallel()

	airline := startAgent(t, airlineExecutor(&answerLog{}))
	answer := approval{Status: "approved", ApproverID: "travel-agent"}

	tests := []struct {
		name       string
		answeredBy structuredinput.AnswerSource
	}{
		{name: "answered by agent", answeredBy: structuredinput.AnswerSourceAgent},
		{name: "answered by policy", answeredBy: structuredinput.AnswerSourcePolicy},
		{name: "answer source not stated"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			task := sendForTask(t, airline, a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("book the flight")))
			req, err := structuredinput.PendingRequest(task)
			if err != nil || req == nil {
				t.Fatalf("PendingRequest() = (%v, %v), want input request", req, err)
			}

			msg, err := structuredinput.NewResponseAnsweredBy(task, req, tc.answeredBy, answer)
			if err != nil {
				t.Fatalf("NewResponseAnsweredBy() error = %v", err)
			}
			task = sendForTask(t, airline, msg)

			if task.Status.State != a2a.TaskStateInputRequired {
				t.Fatalf("client.SendMessage() state = %v, want %v", task.Status.State, a2a.TaskStateInputRequired)
			}
			retry, err := structuredinput.PendingRequest(task)
			if err != nil || retry == nil || retry.ID != req.ID || retry.AnswerFrom != structuredinput.AnswerSourcePerson {
				t.Fatalf("PendingRequest() = (%+v, %v), want repeated request %q for a person", retry, err, req.ID)
			}
		})
	}
}

func TestRelayThroughIntermediary(t *testing.T) {
	t.Parallel()

	airlineReceived := &answerLog{}
	airline := startAgent(t, airlineExecutor(airlineReceived))
	travel := startAgent(t, travelExecutor(airline))

	task := sendForTask(t, travel, a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("book a flight to Lisbon")))
	req, err := structuredinput.PendingRequest(task)
	if err != nil || req == nil {
		t.Fatalf("PendingRequest() = (%v, %v), want input request", req, err)
	}
	if req.ID != "relay-1" || req.AnswerFrom != structuredinput.AnswerSourcePerson || req.AskedBy != "airline" {
		t.Fatalf("PendingRequest() = %+v, want request relay-1 for a person asked by airline", req)
	}

	msg, err := structuredinput.NewResponseAnsweredBy(task, req, structuredinput.AnswerSourceAgent, approval{Status: "approved", ApproverID: "assistant"})
	if err != nil {
		t.Fatalf("NewResponseAnsweredBy() error = %v", err)
	}
	task = sendForTask(t, travel, msg)
	if task.Status.State != a2a.TaskStateInputRequired {
		t.Fatalf("client.SendMessage() state = %v, want %v", task.Status.State, a2a.TaskStateInputRequired)
	}
	req, err = structuredinput.PendingRequest(task)
	if err != nil || req == nil || req.ID != "relay-1" || req.AnswerFrom != structuredinput.AnswerSourcePerson {
		t.Fatalf("PendingRequest() = (%+v, %v), want repeated request relay-1 for a person", req, err)
	}
	if got := airlineReceived.all(); len(got) != 0 {
		t.Fatalf("airline received %d answers, want 0 before a person answered", len(got))
	}

	personAnswer := approval{Status: "approved", ApproverID: "alice"}
	msg, err = structuredinput.NewResponseAnsweredBy(task, req, structuredinput.AnswerSourcePerson, personAnswer)
	if err != nil {
		t.Fatalf("NewResponseAnsweredBy() error = %v", err)
	}
	task = sendForTask(t, travel, msg)
	if task.Status.State != a2a.TaskStateCompleted {
		t.Fatalf("client.SendMessage() state = %v, want %v", task.Status.State, a2a.TaskStateCompleted)
	}
	if got := task.Status.Message.Parts[0].Text(); got != "charged, approved by alice" {
		t.Fatalf("client.SendMessage() status text = %q, want %q", got, "charged, approved by alice")
	}

	answers := airlineReceived.all()
	if len(answers) != 1 || len(answers[0].Parts) != 1 {
		t.Fatalf("airline received %d answers, want 1 answer with 1 part", len(answers))
	}
	gotMeta, ok := structuredinput.GetPartMetadata(answers[0].Parts[0])
	if !ok {
		t.Fatalf("GetPartMetadata() ok = false, want extension metadata")
	}
	wantMeta := structuredinput.PartMetadata{
		Type:       structuredinput.PartTypeStructuredInput,
		ID:         "charge-1",
		AnsweredBy: structuredinput.AnswerSourcePerson,
	}
	if diff := cmp.Diff(wantMeta, gotMeta); diff != "" {
		t.Fatalf("GetPartMetadata() wrong result (-want +got) diff = %s", diff)
	}
	wantData := map[string]any{"status": "approved", "approver_id": "alice"}
	if diff := cmp.Diff(any(wantData), answers[0].Parts[0].Data()); diff != "" {
		t.Fatalf("airline received wrong input (-want +got) diff = %s", diff)
	}
}
