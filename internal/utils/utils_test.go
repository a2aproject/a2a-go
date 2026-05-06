// Copyright 2025 The A2A Authors
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

func Test_MapStructCodec(t *testing.T) {
	type testStruct struct {
		Val    int         `json:"val"`
		Nested *testStruct `json:"nested,omitempty"`
	}
	val := testStruct{Val: 1, Nested: &testStruct{Val: 2}}

	gotMap, err := ToMapStruct(val)
	if err != nil {
		t.Fatalf("ToMapStruct(v) error = %v", err)
	}
	wantMap := map[string]any{"val": float64(1), "nested": map[string]any{"val": float64(2)}}
	if diff := cmp.Diff(wantMap, gotMap); diff != "" {
		t.Fatalf("ToMapStruct result different (-want,+got)\n%s", diff)
	}

	gotVal, err := FromMapStruct[testStruct](gotMap)
	if err != nil {
		t.Fatalf("FromMapStruct(v) error = %v", err)
	}
	if diff := cmp.Diff(val, *gotVal); diff != "" {
		t.Fatalf("FromMapStruct result different (-want,+got)\n%s", diff)
	}
}
