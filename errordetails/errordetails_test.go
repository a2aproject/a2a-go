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

package errordetails

import (
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestErrorDetailsJSONCodec(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    *Typed
		wantJSON string
		// For unmarshal-only cases (no round-trip)
		unmarshalOnly bool
		inputJSON     string
		wantTypeURL   string
		wantValue     map[string]any
	}{
		// Round-trip
		{
			name:     "ErrorInfo with data",
			input:    NewTyped(ErrorInfoType, map[string]any{"reason": "TASK_NOT_FOUND", "domain": "a2a-protocol.org"}),
			wantJSON: `{"@type":"type.googleapis.com/google.rpc.ErrorInfo","domain":"a2a-protocol.org","reason":"TASK_NOT_FOUND"}`,
		},
		{
			name:     "Struct with data",
			input:    NewTyped(StructType, map[string]any{"foo": "bar"}),
			wantJSON: `{"@type":"type.googleapis.com/google.protobuf.Struct","foo":"bar"}`,
		},
		{
			name:     "empty value",
			input:    NewTyped(ErrorInfoType, map[string]any{}),
			wantJSON: `{"@type":"type.googleapis.com/google.rpc.ErrorInfo"}`,
		},
		{
			name:          "missing @type defaults to Struct",
			unmarshalOnly: true,
			inputJSON:     `{"foo":"bar"}`,
			wantTypeURL:   StructType,
			wantValue:     map[string]any{"foo": "bar"},
		},
		{
			name:          "non-string @type defaults to Struct",
			unmarshalOnly: true,
			inputJSON:     `{"@type":123,"foo":"bar"}`,
			wantTypeURL:   StructType,
			wantValue:     map[string]any{"@type": float64(123), "foo": "bar"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.unmarshalOnly {
				var got Typed
				if err := json.Unmarshal([]byte(tc.inputJSON), &got); err != nil {
					t.Fatalf("Unmarshal() error = %v", err)
				}
				if got.TypeURL != tc.wantTypeURL {
					t.Fatalf("Unmarshal() TypeURL = %q, want %q", got.TypeURL, tc.wantTypeURL)
				}
				if diff := cmp.Diff(tc.wantValue, got.Value); diff != "" {
					t.Fatalf("Unmarshal() Value mismatch (+got,-want):\n%s", diff)
				}
				return
			}
			gotJSON, err := json.Marshal(tc.input)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			if string(gotJSON) != tc.wantJSON {
				t.Fatalf("Marshal() = %s, want %s", gotJSON, tc.wantJSON)
			}
			// Verify immutability
			if _, exists := tc.input.Value["@type"]; exists {
				t.Fatalf("Marshal() mutated original Value map with @type key")
			}
			var got Typed
			if err := json.Unmarshal(gotJSON, &got); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}
			if got.TypeURL != tc.input.TypeURL {
				t.Fatalf("Round-trip TypeURL = %q, want %q", got.TypeURL, tc.input.TypeURL)
			}
			if diff := cmp.Diff(tc.input.Value, got.Value); diff != "" {
				t.Fatalf("Round-trip Value mismatch (+got,-want):\n%s", diff)
			}
		})
	}
}
