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
	"fmt"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
)

const (
	draft07SchemaURI        = "http://json-schema.org/draft-07/schema#"
	draft07SecureSchemaURI  = "https://json-schema.org/draft-07/schema#"
	draft202012SchemaURI    = "https://json-schema.org/draft/2020-12/schema"
	validationErrorsPreview = 3
)

// ValidationOutput is a validation result in the JSON Schema Basic output format.
type ValidationOutput struct {
	// Valid is true if the instance satisfies the schema.
	Valid bool `json:"valid"`
	// Errors is the flat list of validation errors.
	Errors []ValidationErrorItem `json:"errors,omitempty"`
}

// ValidationErrorItem is a single validation error in the JSON Schema Basic output format.
type ValidationErrorItem struct {
	// KeywordLocation is the JSON Pointer to the failed keyword relative to the schema root.
	KeywordLocation string `json:"keywordLocation"`
	// AbsoluteKeywordLocation is the absolute URI of the failed keyword.
	AbsoluteKeywordLocation string `json:"absoluteKeywordLocation,omitempty"`
	// InstanceLocation is the JSON Pointer to the invalid value relative to the instance root.
	InstanceLocation string `json:"instanceLocation"`
	// Error is a human-readable description of the error.
	Error string `json:"error"`
}

// ValidationError is returned by a [Validator] when the data does not satisfy the schema.
type ValidationError struct {
	// Output describes the validation failure.
	Output *ValidationOutput
}

// Error implements error.
func (e *ValidationError) Error() string {
	if e.Output == nil || len(e.Output.Errors) == 0 {
		return "structured input validation failed"
	}
	preview := min(len(e.Output.Errors), validationErrorsPreview)
	msgs := make([]string, 0, preview+1)
	for _, unit := range e.Output.Errors[:preview] {
		msgs = append(msgs, unit.Error)
	}
	if len(e.Output.Errors) > validationErrorsPreview {
		msgs = append(msgs, fmt.Sprintf("and %d more", len(e.Output.Errors)-validationErrorsPreview))
	}
	return "structured input validation failed: " + strings.Join(msgs, "; ")
}

// Validator validates structured input against an input request schema.
type Validator interface {
	// Validate returns nil if data satisfies the schema, a [*ValidationError] if it does not,
	// or another error if the validation could not be performed.
	Validate(ctx context.Context, schema *jsonschema.Schema, data any) error
}

// NewValidator returns a [Validator] which uses the provided draft for schemas which
// do not declare their dialect using the $schema keyword. Only [Draft202012] and [Draft07] are supported.
//
// The validator stops at the first error and reports it as a single [ValidationErrorItem] anchored at the schema
// and instance roots, with the error location included in the error message.
func NewValidator(draft Draft) Validator {
	if draft == "" {
		draft = DefaultDraft
	}
	return &jsonSchemaValidator{draft: draft}
}

type jsonSchemaValidator struct {
	draft Draft
}

func (v *jsonSchemaValidator) Validate(ctx context.Context, schema *jsonschema.Schema, data any) error {
	if schema == nil {
		return fmt.Errorf("schema is required")
	}
	switch schema.Schema {
	case draft07SchemaURI, draft07SecureSchemaURI, draft202012SchemaURI:
	case "":
		switch v.draft {
		case Draft202012:
		case Draft07:
			schema = schema.CloneSchemas()
			schema.Schema = draft07SchemaURI
		default:
			return fmt.Errorf("unsupported JSON Schema draft %q", v.draft)
		}
	default:
		return fmt.Errorf("unsupported JSON Schema dialect %q", schema.Schema)
	}

	resolved, err := schema.Resolve(nil)
	if err != nil {
		return fmt.Errorf("failed to resolve schema: %w", err)
	}
	if err := resolved.Validate(data); err != nil {
		return &ValidationError{Output: &ValidationOutput{
			Valid:  false,
			Errors: []ValidationErrorItem{{KeywordLocation: "", InstanceLocation: "", Error: err.Error()}},
		}}
	}
	return nil
}
