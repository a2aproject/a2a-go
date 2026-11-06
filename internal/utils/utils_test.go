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
	"errors"
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

func TestCastResult(t *testing.T) {
	t.Parallel()

	errCall := errors.New("call failed")
	str := "value"

	testCases := []struct {
		name         string
		payload      any
		err          error
		want         *string
		wantErr      error
		wantMismatch bool
	}{
		{name: "matching type", payload: &str, want: &str},
		{name: "matching type with error", payload: &str, err: errCall, want: &str, wantErr: errCall},
		{name: "nil payload", payload: nil, err: errCall, wantErr: errCall},
		{name: "type mismatch", payload: 42, err: errCall, wantMismatch: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := CastResult[*string](tc.payload, tc.err)
			if tc.wantMismatch {
				if err == nil || errors.Is(err, errCall) {
					t.Fatalf("CastResult() error = %v, want type mismatch error", err)
				}
			} else if !errors.Is(err, tc.wantErr) {
				t.Fatalf("CastResult() error = %v, want %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("CastResult() = %v, want %v", got, tc.want)
			}
		})
	}
}
