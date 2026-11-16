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
	"fmt"
	"slices"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

// UntaggedPolicy defines when a data part without the extension metadata can be accepted as the structured input.
type UntaggedPolicy int

const (
	// UntaggedAccept accepts untagged input for all input requests. This allows extension-unaware clients
	// to participate, at the cost of not being able to tell a late answer to an earlier input request
	// with the same schema from an answer to the pending one.
	UntaggedAccept UntaggedPolicy = iota
	// UntaggedAcceptWithoutID accepts untagged input only for input requests which do not have an ID.
	UntaggedAcceptWithoutID
	// UntaggedReject never accepts untagged input.
	UntaggedReject
)

// Outcome is the result of processing a continuation request.
type Outcome int

const (
	// OutcomeAccepted means the request contained structured input satisfying the schema.
	OutcomeAccepted Outcome = iota + 1
	// OutcomeMissing means the request contained no structured input. [Result.Errors] can still be set
	// if the request contained a single untagged data part which failed validation.
	OutcomeMissing
	// OutcomeInvalid means the structured input failed schema validation.
	OutcomeInvalid
	// OutcomeIDMismatch means the structured input was correlated with a different input request.
	OutcomeIDMismatch
	// OutcomeMultipleInputs means the request contained more than one structured input part.
	OutcomeMultipleInputs
)

// String implements [fmt.Stringer].
func (o Outcome) String() string {
	switch o {
	case OutcomeAccepted:
		return "accepted"
	case OutcomeMissing:
		return "missing"
	case OutcomeInvalid:
		return "invalid"
	case OutcomeIDMismatch:
		return "id mismatch"
	case OutcomeMultipleInputs:
		return "multiple inputs"
	default:
		return fmt.Sprintf("Outcome(%d)", int(o))
	}
}

// ProcessorResult is the result of processing a continuation request.
type ProcessorResult struct {
	// Request is the input request the continuation was processed against.
	Request *Request
	// Outcome describes whether the input was accepted.
	Outcome Outcome
	// Input is the structured input. It is set only if Outcome is [OutcomeAccepted].
	Input any
	// Errors are the validation errors which should be reported to the client.
	Errors *ValidationOutput
	// Context contains the request parts which were not interpreted as the structured input,
	// for example clarifying questions the agent can respond to while the input request is pending.
	Context []*a2a.Part
}

// Accepted returns true if the continuation request contained a valid structured input.
func (r *ProcessorResult) Accepted() bool {
	return r.Outcome == OutcomeAccepted
}

// Decode decodes the accepted structured input into target using a JSON round-trip.
func (r *ProcessorResult) Decode(target any) error {
	if !r.Accepted() {
		return fmt.Errorf("structured input was not accepted: %s", r.Outcome)
	}
	if err := fromJSONValue(r.Input, target); err != nil {
		return fmt.Errorf("failed to decode structured input: %w", err)
	}
	return nil
}

// NewRetryEvent creates an input-required status update event repeating the input request.
// The event message consists of the provided parts followed by the input request schema part
// and a validation error part if [ProcessorResult.Errors] is set.
func (r *ProcessorResult) NewRetryEvent(info a2a.TaskInfoProvider, parts ...*a2a.Part) (*a2a.TaskStatusUpdateEvent, error) {
	parts = slices.Clone(parts)
	schemaPart, err := r.Request.Part()
	if err != nil {
		return nil, err
	}
	parts = append(parts, schemaPart)
	if r.Errors != nil {
		errPart, err := NewValidationErrorPart(r.Errors)
		if err != nil {
			return nil, err
		}
		parts = append(parts, errPart)
	}
	msg := a2a.NewMessageForTask(a2a.MessageRoleAgent, info, parts...)
	msg.Extensions = []string{URI}
	return a2a.NewStatusUpdateEvent(info, a2a.TaskStateInputRequired, msg), nil
}

// Processor validates continuation requests against pending input requests.
// The zero value is ready to use.
type Processor struct {
	// Validator is used for validating structured input. Defaults to [NewValidator] with [DefaultDraft].
	Validator Validator
	// Untagged defines when untagged data parts can be accepted as the structured input.
	Untagged UntaggedPolicy
}

// Process extracts and validates the structured input provided in msg in response to req.
// An error is returned only if the validation could not be performed. Invalid or missing input
// is reported using [ProcessorResult.Outcome].
func (p *Processor) Process(ctx context.Context, req *Request, msg *a2a.Message) (*ProcessorResult, error) {
	if req == nil {
		return nil, fmt.Errorf("input request is required")
	}
	if msg == nil {
		return nil, fmt.Errorf("message is required")
	}

	var tagged, rest []*a2a.Part
	for _, part := range msg.Parts {
		if meta, ok := GetPartMetadata(part); ok && meta.Type == PartTypeStructuredInput {
			tagged = append(tagged, part)
		} else {
			rest = append(rest, part)
		}
	}
	result := &ProcessorResult{Request: req, Context: rest}

	switch len(tagged) {
	case 0:
		return p.processUntagged(ctx, result)
	case 1:
		meta, _ := GetPartMetadata(tagged[0])
		if meta.ID != "" && meta.ID != req.ID {
			result.Outcome = OutcomeIDMismatch
			return result, nil
		}
		output, err := p.validate(ctx, req, tagged[0].Data())
		if err != nil {
			return nil, err
		}
		if output != nil {
			result.Outcome = OutcomeInvalid
			result.Errors = output
			return result, nil
		}
		result.Outcome = OutcomeAccepted
		result.Input = tagged[0].Data()
		return result, nil
	default:
		result.Outcome = OutcomeMultipleInputs
		return result, nil
	}
}

func (p *Processor) processUntagged(ctx context.Context, result *ProcessorResult) (*ProcessorResult, error) {
	result.Outcome = OutcomeMissing
	if !p.acceptsUntagged(result.Request) {
		return result, nil
	}

	candidateIdx := -1
	for i, part := range result.Context {
		if _, ok := part.Content.(a2a.Data); !ok {
			continue
		}
		if _, ok := GetPartMetadata(part); ok {
			continue
		}
		if candidateIdx >= 0 {
			return result, nil
		}
		candidateIdx = i
	}
	if candidateIdx < 0 {
		return result, nil
	}

	candidate := result.Context[candidateIdx]
	output, err := p.validate(ctx, result.Request, candidate.Data())
	if err != nil {
		return nil, err
	}
	if output != nil {
		result.Errors = output
		return result, nil
	}
	result.Outcome = OutcomeAccepted
	result.Input = candidate.Data()
	result.Context = slices.Delete(slices.Clone(result.Context), candidateIdx, candidateIdx+1)
	return result, nil
}

func (p *Processor) acceptsUntagged(req *Request) bool {
	switch p.Untagged {
	case UntaggedAccept:
		return true
	case UntaggedAcceptWithoutID:
		return req.ID == ""
	default:
		return false
	}
}

// validate returns a non-nil output if data failed validation.
func (p *Processor) validate(ctx context.Context, req *Request, data any) (*ValidationOutput, error) {
	validator := p.Validator
	if validator == nil {
		validator = NewValidator(DefaultDraft)
	}
	err := validator.Validate(ctx, req.Schema, data)
	if err == nil {
		return nil, nil
	}
	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		return nil, fmt.Errorf("structured input validation: %w", err)
	}
	if validationErr.Output == nil {
		return &ValidationOutput{Valid: false}, nil
	}
	return validationErr.Output, nil
}
