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

package utils

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestDeepCopyPreservesEmptyCollections verifies empty arrays, explicit null, nested metadata, and write isolation.
func TestDeepCopyPreservesEmptyCollections(t *testing.T) {
	t.Parallel()
	source := map[string]any{"empty": []any{}, "null": nil, "nested": map[string]any{"items": []any{}, "body": []any{"正文"}}, "metadata": []any{map[string]any{"empty": []any{}}}}
	copied, err := DeepCopy(&source)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(source, *copied); diff != "" {
		t.Fatalf("DeepCopy() wrong result (-want +got) diff = %s", diff)
	}
	(*copied)["nested"].(map[string]any)["body"].([]any)[0] = "changed"
	if source["nested"].(map[string]any)["body"].([]any)[0] != "正文" {
		t.Fatal("copy shares source memory")
	}
}

// TestDeepCopyPreservesNonJSONTypes verifies arrays, pointers, bytes, and nil slices retain their semantics.
func TestDeepCopyPreservesNonJSONTypes(t *testing.T) {
	t.Parallel()
	type sample struct {
		Empty   []byte
		Nil     []byte
		Nested  [1][]int
		Pointer *[]string
		Integer uint64
	}
	empty := []string{"kept"}
	source := sample{Empty: []byte{}, Nested: [1][]int{{}}, Pointer: &empty, Integer: ^uint64(0)}
	copied, err := DeepCopy(&source)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(source, *copied); diff != "" {
		t.Fatalf("DeepCopy() wrong result (-want +got) diff = %s", diff)
	}
	if source.Pointer == copied.Pointer {
		t.Fatal("copy shares pointer")
	}
}

// emptySliceCodec exercises a custom gob type with its own empty-value representation.
type emptySliceCodec struct {
	Values []string
}

// GobEncode returns a fixed payload representing a custom encoding.
func (emptySliceCodec) GobEncode() ([]byte, error) {
	return []byte{1}, nil
}

// GobDecode intentionally normalizes empty slices to nil. DeepCopy must respect that choice.
func (v *emptySliceCodec) GobDecode([]byte) error {
	v.Values = nil
	return nil
}

// TestDeepCopyRespectsCustomGobCodec verifies reflection preserves custom codec results.
func TestDeepCopyRespectsCustomGobCodec(t *testing.T) {
	t.Parallel()
	source := emptySliceCodec{Values: []string{}}
	copied, err := DeepCopy(&source)
	if err != nil {
		t.Fatalf("DeepCopy() error = %v, want nil", err)
	}
	if diff := cmp.Diff(emptySliceCodec{}, *copied); diff != "" {
		t.Fatalf("DeepCopy() wrong result (-want +got) diff = %s", diff)
	}
}
