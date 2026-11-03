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
	"fmt"
	"slices"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/jsonschema-go/jsonschema"
)

// Request is a request for structured input attached to an input-required task status.
type Request struct {
	// ID is an optional identifier distinguishing the request from other input requests within the task.
	// It must not be reused by a different input request within the task and should be kept when the
	// request is repeated after a failed validation.
	ID string
	// Schema defines the expected structure of the input.
	Schema *jsonschema.Schema
}

// Part creates a part carrying the input request.
func (r *Request) Part() (*a2a.Part, error) {
	return NewSchemaPart(r.ID, r.Schema)
}

// NewRequestEvent creates an input-required status update event with a message consisting
// of the provided parts followed by the input request schema part.
func NewRequestEvent(info a2a.TaskInfoProvider, req *Request, parts ...*a2a.Part) (*a2a.TaskStatusUpdateEvent, error) {
	schemaPart, err := req.Part()
	if err != nil {
		return nil, err
	}
	msg := a2a.NewMessageForTask(a2a.MessageRoleAgent, info, append(slices.Clone(parts), schemaPart)...)
	msg.Extensions = []string{URI}
	return a2a.NewStatusUpdateEvent(info, a2a.TaskStateInputRequired, msg), nil
}

// ParseRequest returns the input request carried by the message.
// Returns nil if the message does not contain an input schema part.
func ParseRequest(msg *a2a.Message) (*Request, error) {
	part, meta, err := findSinglePart(msg, PartTypeInputSchema)
	if err != nil || part == nil {
		return nil, err
	}
	var schema jsonschema.Schema
	if err := fromJSONValue(part.Data(), &schema); err != nil {
		return nil, fmt.Errorf("invalid input schema: %w", err)
	}
	return &Request{ID: meta.ID, Schema: &schema}, nil
}

// PendingRequest returns the input request of a task in the input-required state.
// Returns nil if the task is not waiting for structured input.
func PendingRequest(task *a2a.Task) (*Request, error) {
	if task == nil || task.Status.State != a2a.TaskStateInputRequired {
		return nil, nil
	}
	return ParseRequest(task.Status.Message)
}

// ParseValidationOutput returns validation errors reported by the agent in the message.
// Returns nil if the message does not contain a validation error part.
func ParseValidationOutput(msg *a2a.Message) (*ValidationOutput, error) {
	part, _, err := findSinglePart(msg, PartTypeValidationError)
	if err != nil || part == nil {
		return nil, err
	}
	var output ValidationOutput
	if err := fromJSONValue(part.Data(), &output); err != nil {
		return nil, fmt.Errorf("invalid validation output: %w", err)
	}
	return &output, nil
}

func findSinglePart(msg *a2a.Message, partType PartType) (*a2a.Part, PartMetadata, error) {
	if msg == nil {
		return nil, PartMetadata{}, nil
	}
	var found *a2a.Part
	var foundMeta PartMetadata
	for _, part := range msg.Parts {
		meta, ok := GetPartMetadata(part)
		if !ok || meta.Type != partType {
			continue
		}
		if found != nil {
			return nil, PartMetadata{}, fmt.Errorf("message contains more than one %s part", partType)
		}
		found, foundMeta = part, meta
	}
	return found, foundMeta, nil
}
