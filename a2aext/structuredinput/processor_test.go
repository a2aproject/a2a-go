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

package structuredinput

import (
	"context"
	"errors"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/jsonschema-go/jsonschema"
)

type validatorFunc func(ctx context.Context, schema *jsonschema.Schema, data any) error

func (f validatorFunc) Validate(ctx context.Context, schema *jsonschema.Schema, data any) error {
	return f(ctx, schema, data)
}

func TestProcessor_Process(t *testing.T) {
	t.Parallel()

	validInput := map[string]any{"status": "ok", "approver_id": "alice"}
	invalidInput := map[string]any{"status": "ok"}
	textPart := a2a.NewTextPart("is it safe?")

	tests := []struct {
		name        string
		untagged    UntaggedPolicy
		requestID   string
		parts       []*a2a.Part
		wantOutcome Outcome
		wantInput   any
		wantErrors  bool
		wantContext []*a2a.Part
	}{
		{
			name:        "tagged valid input",
			requestID:   "req-1",
			parts:       []*a2a.Part{textPart, mustPart(t)(NewResponsePart("req-1", validInput))},
			wantOutcome: OutcomeAccepted,
			wantInput:   validInput,
			wantContext: []*a2a.Part{textPart},
		},
		{
			name:        "tagged input without id",
			requestID:   "req-1",
			parts:       []*a2a.Part{mustPart(t)(NewResponsePart("", validInput))},
			wantOutcome: OutcomeAccepted,
			wantInput:   validInput,
		},
		{
			name:        "tagged invalid input",
			requestID:   "req-1",
			parts:       []*a2a.Part{textPart, mustPart(t)(NewResponsePart("req-1", invalidInput))},
			wantOutcome: OutcomeInvalid,
			wantErrors:  true,
			wantContext: []*a2a.Part{textPart},
		},
		{
			name:        "tagged input with mismatched id",
			requestID:   "req-2",
			parts:       []*a2a.Part{mustPart(t)(NewResponsePart("req-1", validInput))},
			wantOutcome: OutcomeIDMismatch,
		},
		{
			name:      "multiple tagged inputs",
			requestID: "req-1",
			parts: []*a2a.Part{
				mustPart(t)(NewResponsePart("req-1", validInput)),
				mustPart(t)(NewResponsePart("req-1", validInput)),
			},
			wantOutcome: OutcomeMultipleInputs,
		},
		{
			name:        "tagged input ignores untagged data",
			parts:       []*a2a.Part{a2a.NewDataPart(validInput), mustPart(t)(NewResponsePart("", invalidInput))},
			wantOutcome: OutcomeInvalid,
			wantErrors:  true,
			wantContext: []*a2a.Part{a2a.NewDataPart(validInput)},
		},
		{
			name:        "no input",
			parts:       []*a2a.Part{textPart},
			wantOutcome: OutcomeMissing,
			wantContext: []*a2a.Part{textPart},
		},
		{
			name:        "untagged valid input",
			requestID:   "req-1",
			parts:       []*a2a.Part{textPart, a2a.NewDataPart(validInput)},
			wantOutcome: OutcomeAccepted,
			wantInput:   validInput,
			wantContext: []*a2a.Part{textPart},
		},
		{
			name:        "untagged invalid input",
			parts:       []*a2a.Part{a2a.NewDataPart(invalidInput)},
			wantOutcome: OutcomeMissing,
			wantErrors:  true,
			wantContext: []*a2a.Part{a2a.NewDataPart(invalidInput)},
		},
		{
			name:        "multiple untagged data parts",
			parts:       []*a2a.Part{a2a.NewDataPart(validInput), a2a.NewDataPart(validInput)},
			wantOutcome: OutcomeMissing,
			wantContext: []*a2a.Part{a2a.NewDataPart(validInput), a2a.NewDataPart(validInput)},
		},
		{
			name:        "untagged input for request without id accepted with UntaggedAcceptWithoutID",
			untagged:    UntaggedAcceptWithoutID,
			parts:       []*a2a.Part{a2a.NewDataPart(validInput)},
			wantOutcome: OutcomeAccepted,
			wantInput:   validInput,
		},
		{
			name:        "untagged input for request with id rejected with UntaggedAcceptWithoutID",
			untagged:    UntaggedAcceptWithoutID,
			requestID:   "req-1",
			parts:       []*a2a.Part{a2a.NewDataPart(validInput)},
			wantOutcome: OutcomeMissing,
			wantContext: []*a2a.Part{a2a.NewDataPart(validInput)},
		},
		{
			name:        "untagged input rejected with UntaggedReject",
			untagged:    UntaggedReject,
			parts:       []*a2a.Part{a2a.NewDataPart(validInput)},
			wantOutcome: OutcomeMissing,
			wantContext: []*a2a.Part{a2a.NewDataPart(validInput)},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := &Request{ID: tc.requestID, Schema: newTestSchema()}
			msg := jsonRoundTrip(t, a2a.NewMessage(a2a.MessageRoleUser, tc.parts...))
			processor := &Processor{Untagged: tc.untagged}

			got, err := processor.Process(t.Context(), req, msg)
			if err != nil {
				t.Fatalf("Processor.Process() error = %v", err)
			}

			if got.Outcome != tc.wantOutcome {
				t.Fatalf("Processor.Process() outcome = %v, want %v", got.Outcome, tc.wantOutcome)
			}
			if diff := cmp.Diff(tc.wantInput, got.Input); diff != "" {
				t.Fatalf("Processor.Process() wrong input (-want +got) diff = %s", diff)
			}
			if gotErrors := got.Errors != nil; gotErrors != tc.wantErrors {
				t.Fatalf("Processor.Process() errors = %+v, want errors %v", got.Errors, tc.wantErrors)
			}
			if diff := cmp.Diff(tc.wantContext, got.Context, cmpopts.EquateEmpty()); diff != "" {
				t.Fatalf("Processor.Process() wrong context (-want +got) diff = %s", diff)
			}
		})
	}
}

func TestProcessor_ProcessValidatorError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("validator failed")
	processor := &Processor{Validator: validatorFunc(func(context.Context, *jsonschema.Schema, any) error {
		return wantErr
	})}
	msg := a2a.NewMessage(a2a.MessageRoleUser, mustPart(t)(NewResponsePart("", "data")))

	_, err := processor.Process(t.Context(), &Request{Schema: newTestSchema()}, msg)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Processor.Process() error = %v, want %v", err, wantErr)
	}
}

func TestResult_Decode(t *testing.T) {
	t.Parallel()

	type approval struct {
		Status     string `json:"status"`
		ApproverID string `json:"approver_id"`
	}

	accepted := &ProcessorResult{Outcome: OutcomeAccepted, Input: map[string]any{"status": "ok", "approver_id": "alice"}}
	var got approval
	if err := accepted.Decode(&got); err != nil {
		t.Fatalf("Result.Decode() error = %v", err)
	}
	if diff := cmp.Diff(approval{Status: "ok", ApproverID: "alice"}, got); diff != "" {
		t.Fatalf("Result.Decode() wrong result (-want +got) diff = %s", diff)
	}

	rejected := &ProcessorResult{Outcome: OutcomeInvalid}
	if err := rejected.Decode(&got); err == nil {
		t.Fatalf("Result.Decode() error = nil, want error for rejected input")
	}
}

func TestResult_NewRetryEvent(t *testing.T) {
	t.Parallel()

	taskInfo := a2a.TaskInfo{TaskID: "task-1", ContextID: "ctx-1"}
	req := &Request{ID: "req-1", Schema: newTestSchema()}
	output := &ValidationOutput{Errors: []ValidationErrorItem{{Error: "missing approver_id"}}}

	tests := []struct {
		name       string
		result     *ProcessorResult
		wantErrors *ValidationOutput
		wantParts  int
	}{
		{
			name:      "missing input",
			result:    &ProcessorResult{Request: req, Outcome: OutcomeMissing},
			wantParts: 2,
		},
		{
			name:       "invalid input",
			result:     &ProcessorResult{Request: req, Outcome: OutcomeInvalid, Errors: output},
			wantErrors: output,
			wantParts:  3,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			event, err := tc.result.NewRetryEvent(taskInfo, a2a.NewTextPart("try again"))
			if err != nil {
				t.Fatalf("Result.NewRetryEvent() error = %v", err)
			}

			if event.Status.State != a2a.TaskStateInputRequired {
				t.Fatalf("Result.NewRetryEvent() state = %v, want %v", event.Status.State, a2a.TaskStateInputRequired)
			}
			msg := jsonRoundTrip(t, event.Status.Message)
			if len(msg.Parts) != tc.wantParts {
				t.Fatalf("Result.NewRetryEvent() parts = %d, want %d", len(msg.Parts), tc.wantParts)
			}
			gotReq, err := ParseRequest(msg)
			if err != nil {
				t.Fatalf("ParseInputRequest() error = %v", err)
			}
			if diff := cmp.Diff(req, gotReq, cmpopts.IgnoreUnexported(jsonschema.Schema{})); diff != "" {
				t.Fatalf("ParseInputRequest() wrong result (-want +got) diff = %s", diff)
			}
			gotErrors, err := ParseValidationOutput(msg)
			if err != nil {
				t.Fatalf("ParseValidationOutput() error = %v", err)
			}
			if diff := cmp.Diff(tc.wantErrors, gotErrors); diff != "" {
				t.Fatalf("ParseValidationOutput() wrong result (-want +got) diff = %s", diff)
			}
		})
	}
}
