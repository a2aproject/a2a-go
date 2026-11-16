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
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/go-cmp/cmp"
	"github.com/google/jsonschema-go/jsonschema"
)

func newTestSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Type: "object",
		Properties: map[string]*jsonschema.Schema{
			"status":      {Type: "string"},
			"approver_id": {Type: "string"},
		},
		Required: []string{"status", "approver_id"},
	}
}

func jsonRoundTrip[T any](t *testing.T, v T) T {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var result T
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	return result
}

func mustPart(t *testing.T) func(*a2a.Part, error) *a2a.Part {
	t.Helper()
	return func(part *a2a.Part, err error) *a2a.Part {
		t.Helper()
		if err != nil {
			t.Fatalf("part creation error = %v", err)
		}
		return part
	}
}

func TestDraftFromCard(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		card      *a2a.AgentCard
		wantDraft Draft
		wantOK    bool
	}{
		{
			name:   "nil card",
			wantOK: false,
		},
		{
			name: "extension not supported",
			card: &a2a.AgentCard{Capabilities: a2a.AgentCapabilities{
				Extensions: []a2a.AgentExtension{{URI: "other"}},
			}},
			wantOK: false,
		},
		{
			name: "draft declared",
			card: &a2a.AgentCard{Capabilities: a2a.AgentCapabilities{
				Extensions: []a2a.AgentExtension{NewExtension(Draft07)},
			}},
			wantDraft: Draft07,
			wantOK:    true,
		},
		{
			name: "draft omitted",
			card: &a2a.AgentCard{Capabilities: a2a.AgentCapabilities{
				Extensions: []a2a.AgentExtension{{URI: URI}},
			}},
			wantDraft: DefaultDraft,
			wantOK:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			gotDraft, gotOK := DraftFromCard(tc.card)
			if gotDraft != tc.wantDraft || gotOK != tc.wantOK {
				t.Fatalf("DraftFromCard() = (%q, %v), want (%q, %v)", gotDraft, gotOK, tc.wantDraft, tc.wantOK)
			}
		})
	}
}

func TestPartsJSON(t *testing.T) {
	t.Parallel()

	schemaPart := mustPart(t)(NewSchemaPart("req-1", &jsonschema.Schema{Type: "string"}))
	errorPart := mustPart(t)(NewValidationErrorPart(&ValidationOutput{
		Errors: []ValidationErrorItem{{KeywordLocation: "/type", Error: "bad type"}},
	}))
	inputPart := mustPart(t)(NewResponsePart("", map[string]any{"foo": "bar"}))

	got, err := json.Marshal([]*a2a.Part{schemaPart, errorPart, inputPart})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	want := `[
		{"data":{"type":"string"},"mediaType":"application/schema+json","metadata":{"` + URI + `":{"id":"req-1","type":"input_schema"}}},
		{"data":{"errors":[{"error":"bad type","instanceLocation":"","keywordLocation":"/type"}],"valid":false},"metadata":{"` + URI + `":{"type":"validation_error"}}},
		{"data":{"foo":"bar"},"metadata":{"` + URI + `":{"type":"structured_input"}}}
	]`
	var wantValue, gotValue any
	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if diff := cmp.Diff(wantValue, gotValue); diff != "" {
		t.Fatalf("json.Marshal() wrong result (-want +got) diff = %s", diff)
	}
}

func TestGetPartMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		part     *a2a.Part
		wantMeta PartMetadata
		wantOK   bool
	}{
		{
			name:     "structured input with id",
			part:     mustPart(t)(NewResponsePart("req-1", "data")),
			wantMeta: PartMetadata{Type: PartTypeStructuredInput, ID: "req-1"},
			wantOK:   true,
		},
		{
			name:     "input schema without id",
			part:     mustPart(t)(NewSchemaPart("", newTestSchema())),
			wantMeta: PartMetadata{Type: PartTypeInputSchema},
			wantOK:   true,
		},
		{
			name: "untagged data part",
			part: a2a.NewDataPart(map[string]any{"foo": "bar"}),
		},
		{
			name: "text part with metadata",
			part: &a2a.Part{Content: a2a.Text("hi"), Metadata: map[string]any{URI: map[string]any{"type": "structured_input"}}},
		},
		{
			name: "metadata without type",
			part: &a2a.Part{Content: a2a.Data{Value: "hi"}, Metadata: map[string]any{URI: map[string]any{"id": "req-1"}}},
		},
		{
			name: "nil part",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var part *a2a.Part
			if tc.part != nil {
				part = jsonRoundTrip(t, tc.part)
			}
			gotMeta, gotOK := GetPartMetadata(part)
			if gotOK != tc.wantOK {
				t.Fatalf("GetPartMetadata() ok = %v, want %v", gotOK, tc.wantOK)
			}
			if diff := cmp.Diff(tc.wantMeta, gotMeta); diff != "" {
				t.Fatalf("GetPartMetadata() wrong result (-want +got) diff = %s", diff)
			}
		})
	}
}
