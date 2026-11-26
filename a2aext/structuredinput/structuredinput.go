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
	"encoding/json"
	"fmt"
	"slices"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/jsonschema-go/jsonschema"
)

const (
	// URI is the unique identifier of the extension.
	URI = "https://github.com/a2aproject/experimental-ext-structured-input/tree/main/v1"
	// SchemaMediaType is the media type set on input schema parts.
	SchemaMediaType = "application/schema+json"
	// ParamJSONSchemaDraft is the agent extension parameter declaring the JSON Schema draft used for validation.
	ParamJSONSchemaDraft = "jsonschema_draft"
)

// Draft is a JSON Schema draft version.
type Draft string

const (
	// Draft202012 is the JSON Schema draft 2020-12.
	Draft202012 Draft = "2020-12"
	// Draft07 is the JSON Schema draft-07.
	Draft07 Draft = "draft-07"
	// DefaultDraft is the draft assumed when the agent does not declare one.
	DefaultDraft = Draft202012
)

// PartType defines the role of a part in the structured input exchange.
type PartType string

const (
	// PartTypeInputSchema marks an agent part containing the JSON Schema of the requested input.
	PartTypeInputSchema PartType = "input_schema"
	// PartTypeValidationError marks an agent part containing validation errors in the JSON Schema Basic output format.
	PartTypeValidationError PartType = "validation_error"
	// PartTypeStructuredInput marks a client part containing the data satisfying the requested schema.
	PartTypeStructuredInput PartType = "structured_input"
)

// AnswerSource names who provides the structured input for an input request.
type AnswerSource string

const (
	// AnswerSourcePerson means a person gave or confirmed the input for the input request.
	AnswerSourcePerson AnswerSource = "person"
	// AnswerSourcePolicy means a rule configured by a person in advance produced the input.
	AnswerSourcePolicy AnswerSource = "policy"
	// AnswerSourceAgent means the client produced the input itself, for example with a model.
	AnswerSourceAgent AnswerSource = "agent"
)

// PartMetadata is the extension metadata attached to structured input parts.
type PartMetadata struct {
	// Type is the role of the part.
	Type PartType
	// ID is the optional identifier of the input request.
	ID string
	// AnswerFrom is set on input schema parts when the agent requires the input from a specific source.
	AnswerFrom AnswerSource
	// AskedBy is set on input schema parts relayed by an intermediary and names the agent which issued the request.
	AskedBy string
	// AnsweredBy is set on structured input parts and states who provided the input.
	// It is a claim made by the client and is not verified.
	AnsweredBy AnswerSource
}

// NewExtension creates an agent card extension declaration which advertises
// support for structured input requests validated using the provided draft.
func NewExtension(draft Draft) a2a.AgentExtension {
	ext := a2a.AgentExtension{
		URI:         URI,
		Description: "Standardizes schema-based input requests.",
	}
	if draft != "" {
		ext.Params = map[string]any{ParamJSONSchemaDraft: string(draft)}
	}
	return ext
}

// DraftFromCard returns the JSON Schema draft declared by the agent.
// Returns false if the agent does not support the extension.
func DraftFromCard(card *a2a.AgentCard) (Draft, bool) {
	if card == nil {
		return "", false
	}
	idx := slices.IndexFunc(card.Capabilities.Extensions, func(e a2a.AgentExtension) bool {
		return e.URI == URI
	})
	if idx < 0 {
		return "", false
	}
	if draft, ok := card.Capabilities.Extensions[idx].Params[ParamJSONSchemaDraft].(string); ok && draft != "" {
		return Draft(draft), true
	}
	return DefaultDraft, true
}

// GetPartMetadata returns the extension metadata of a data part.
// Returns false if the part is not a data part or does not carry valid extension metadata.
func GetPartMetadata(part *a2a.Part) (PartMetadata, bool) {
	if part == nil {
		return PartMetadata{}, false
	}
	if _, ok := part.Content.(a2a.Data); !ok {
		return PartMetadata{}, false
	}
	raw, ok := part.Metadata[URI].(map[string]any)
	if !ok {
		return PartMetadata{}, false
	}
	partType, ok := raw["type"].(string)
	if !ok || partType == "" {
		return PartMetadata{}, false
	}
	id, _ := raw["id"].(string)
	answerFrom, _ := raw["answer_from"].(string)
	askedBy, _ := raw["asked_by"].(string)
	answeredBy, _ := raw["answered_by"].(string)
	return PartMetadata{
		Type:       PartType(partType),
		ID:         id,
		AnswerFrom: AnswerSource(answerFrom),
		AskedBy:    askedBy,
		AnsweredBy: AnswerSource(answeredBy),
	}, true
}

// NewSchemaPart creates a part requesting input matching the provided schema.
func NewSchemaPart(id string, schema *jsonschema.Schema) (*a2a.Part, error) {
	req := &Request{ID: id, Schema: schema}
	return req.Part()
}

// NewValidationErrorPart creates a part reporting validation errors to the client.
func NewValidationErrorPart(output *ValidationOutput) (*a2a.Part, error) {
	if output == nil {
		return nil, fmt.Errorf("validation output is required")
	}
	part, err := newPart(PartMetadata{Type: PartTypeValidationError}, output)
	if err != nil {
		return nil, fmt.Errorf("failed to create validation error part: %w", err)
	}
	return part, nil
}

// NewResponsePart creates a part carrying data provided in response to the input request with the provided ID.
func NewResponsePart(id string, data any) (*a2a.Part, error) {
	return NewResponsePartAnsweredBy(id, "", data)
}

// NewResponsePartAnsweredBy creates a part carrying data provided by the given source in response
// to the input request with the provided ID.
func NewResponsePartAnsweredBy(id string, answeredBy AnswerSource, data any) (*a2a.Part, error) {
	part, err := newPart(PartMetadata{Type: PartTypeStructuredInput, ID: id, AnsweredBy: answeredBy}, data)
	if err != nil {
		return nil, fmt.Errorf("failed to create structured input part: %w", err)
	}
	return part, nil
}

// newPart converts value to a plain JSON value, so that the part has the same in-memory
// representation as a part received over the wire and can be deep-copied by task stores.
func newPart(meta PartMetadata, value any) (*a2a.Part, error) {
	data, err := toJSONValue(value)
	if err != nil {
		return nil, err
	}
	partMeta := map[string]any{"type": string(meta.Type)}
	if meta.ID != "" {
		partMeta["id"] = meta.ID
	}
	if meta.AnswerFrom != "" {
		partMeta["answer_from"] = string(meta.AnswerFrom)
	}
	if meta.AskedBy != "" {
		partMeta["asked_by"] = meta.AskedBy
	}
	if meta.AnsweredBy != "" {
		partMeta["answered_by"] = string(meta.AnsweredBy)
	}
	part := a2a.NewDataPart(data)
	part.SetMeta(URI, partMeta)
	return part, nil
}

func toJSONValue(value any) (any, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var result any
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func fromJSONValue(value any, target any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, target)
}
