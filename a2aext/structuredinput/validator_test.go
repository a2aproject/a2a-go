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
	"errors"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

func TestValidator(t *testing.T) {
	t.Parallel()

	// In draft-07 keywords next to $ref are ignored, so the "type" constraint does not apply.
	refSchema := &jsonschema.Schema{
		Definitions: map[string]*jsonschema.Schema{"any": {}},
		Ref:         "#/definitions/any",
		Type:        "string",
	}

	tests := []struct {
		name               string
		draft              Draft
		schema             *jsonschema.Schema
		data               any
		wantValidationErr  bool
		wantValidatorError bool
	}{
		{
			name:   "valid input",
			schema: newTestSchema(),
			data:   map[string]any{"status": "ok", "approver_id": "alice"},
		},
		{
			name:              "missing required property",
			schema:            newTestSchema(),
			data:              map[string]any{"status": "ok"},
			wantValidationErr: true,
		},
		{
			name:              "wrong type",
			schema:            newTestSchema(),
			data:              "ok",
			wantValidationErr: true,
		},
		{
			name:              "2020-12 applies keywords next to $ref",
			draft:             Draft202012,
			schema:            refSchema,
			data:              42.0,
			wantValidationErr: true,
		},
		{
			name:   "draft-07 ignores keywords next to $ref",
			draft:  Draft07,
			schema: refSchema,
			data:   42.0,
		},
		{
			name:               "unsupported draft",
			draft:              "2019-09",
			schema:             newTestSchema(),
			data:               "ok",
			wantValidatorError: true,
		},
		{
			name:               "unsupported $schema",
			schema:             &jsonschema.Schema{Schema: "https://json-schema.org/draft/2019-09/schema"},
			data:               "ok",
			wantValidatorError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := NewValidator(tc.draft).Validate(t.Context(), tc.schema, tc.data)
			var validationErr *ValidationError
			isValidationErr := errors.As(err, &validationErr)
			if isValidationErr != tc.wantValidationErr {
				t.Fatalf("Validator.Validate() error = %v, want validation error %v", err, tc.wantValidationErr)
			}
			if gotValidatorErr := err != nil && !isValidationErr; gotValidatorErr != tc.wantValidatorError {
				t.Fatalf("Validator.Validate() error = %v, want validator error %v", err, tc.wantValidatorError)
			}
			if isValidationErr && (validationErr.Output.Valid || len(validationErr.Output.Errors) == 0) {
				t.Fatalf("Validator.Validate() output = %+v, want invalid output with errors", validationErr.Output)
			}
		})
	}
}
