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

package a2av0

import (
	"testing"

	a2alegacy "github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/go-cmp/cmp"
)

func TestToV1Part(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   a2alegacy.Part
		want *a2a.Part
	}{
		{
			name: "nil part",
			in:   nil,
			want: nil,
		},
		{
			name: "text part",
			in:   a2alegacy.TextPart{Text: "hello"},
			want: &a2a.Part{Content: a2a.Text("hello")},
		},
		{
			name: "data part with compat metadata unwraps value",
			in: a2alegacy.DataPart{
				Data:     map[string]any{"value": "hello"},
				Metadata: map[string]any{"data_part_compat": true},
			},
			want: &a2a.Part{Content: a2a.Data{Value: "hello"}, Metadata: map[string]any{}},
		},
		{
			name: "data part without compat metadata keeps map",
			in: a2alegacy.DataPart{
				Data:     map[string]any{"key": "value"},
				Metadata: map[string]any{"other": "meta"},
			},
			want: &a2a.Part{
				Content:  a2a.Data{Value: map[string]any{"key": "value"}},
				Metadata: map[string]any{"other": "meta"},
			},
		},
		{
			name: "data part with nil metadata",
			in:   a2alegacy.DataPart{Data: map[string]any{"key": "value"}},
			want: &a2a.Part{Content: a2a.Data{Value: map[string]any{"key": "value"}}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ToV1Part(tc.in)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("ToV1Part() wrong result (-want +got) diff = %s", diff)
			}
		})
	}
}

func TestFromV1Part(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   *a2a.Part
		want a2alegacy.Part
	}{
		{
			name: "primitive data wraps with compat flag",
			in:   a2a.NewDataPart("hello"),
			want: a2alegacy.DataPart{
				Data:     map[string]any{"value": "hello"},
				Metadata: map[string]any{"data_part_compat": true},
			},
		},
		{
			name: "map data not wrapped",
			in:   a2a.NewDataPart(map[string]any{"key": "value"}),
			want: a2alegacy.DataPart{
				Data: map[string]any{"key": "value"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := FromV1Part(tc.in)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("FromV1Part() wrong result (-want +got) diff = %s", diff)
			}
		})
	}
}
