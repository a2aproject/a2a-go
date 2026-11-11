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

	"github.com/a2aproject/a2a-go/v2/a2a"
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

type sliceAlias []string

type nonJSONSlices struct {
	Empty  []byte
	Nil    []byte
	Nested [1][]int
}

type gobSliceCodec struct{ Values []string }

func (gobSliceCodec) GobEncode() ([]byte, error) { return []byte{1}, nil }
func (v *gobSliceCodec) GobDecode([]byte) error  { v.Values = nil; return nil }

type binarySliceCodec struct{ Values []string }

func (binarySliceCodec) MarshalBinary() ([]byte, error)  { return []byte{1}, nil }
func (v *binarySliceCodec) UnmarshalBinary([]byte) error { v.Values = nil; return nil }

type gobPriorityCodec struct{ Values []string }

func (gobPriorityCodec) GobEncode() ([]byte, error) { return []byte{1}, nil }
func (v *gobPriorityCodec) GobDecode([]byte) error  { v.Values = nil; return nil }
func (gobPriorityCodec) MarshalBinary() ([]byte, error) {
	return nil, errors.New("gob must select GobEncode")
}
func (*gobPriorityCodec) UnmarshalBinary([]byte) error {
	return errors.New("gob must select GobDecode")
}

type textSliceCodec struct{ Values []string }

func (textSliceCodec) MarshalText() ([]byte, error) {
	return nil, errors.New("gob must not select MarshalText")
}
func (*textSliceCodec) UnmarshalText([]byte) error {
	return errors.New("gob must not select UnmarshalText")
}

func deepCopyCase[T any](source T) func() (any, error) {
	return func() (any, error) {
		copied, err := DeepCopy(&source)
		if err != nil {
			return nil, err
		}
		return *copied, nil
	}
}

func TestDeepCopy(t *testing.T) {
	t.Parallel()
	metadata := map[string]any{"empty": []any{}, "null": nil, "nested": map[string]any{"items": []any{}, "body": []any{"正文"}}, "metadata": []any{map[string]any{"empty": []any{}}}}
	part := a2a.NewDataPart(map[string]any{"items": []any{}, "body": []any{"正文"}, "null": nil})
	part.Metadata = map[string]any{"items": []any{}}
	message := a2a.NewMessage(a2a.MessageRoleUser, part)
	task := a2a.NewSubmittedTask(message, message)
	task.Metadata = map[string]any{"items": []any{}, "null": nil}
	testCases := []struct {
		name           string
		copy           func() (any, error)
		want           any
		wantErr        bool
		checkIsolation func(*testing.T, any)
	}{
		{name: "metadata arrays and explicit null", copy: deepCopyCase(metadata), want: metadata,
			checkIsolation: func(t *testing.T, copied any) {
				copied.(map[string]any)["nested"].(map[string]any)["body"].([]any)[0] = "changed"
				if diff := cmp.Diff("正文", metadata["nested"].(map[string]any)["body"].([]any)[0]); diff != "" {
					t.Fatalf("DeepCopy() source changed (-want +got) diff = %s", diff)
				}
			}},
		{name: "task metadata and data part", copy: deepCopyCase(*task), want: *task,
			checkIsolation: func(t *testing.T, copied any) {
				got := copied.(a2a.Task)
				got.Metadata["items"] = []any{"changed"}
				got.History[0].Parts[0].Data().(map[string]any)["body"].([]any)[0] = "changed"
				if diff := cmp.Diff([]any{}, task.Metadata["items"]); diff != "" {
					t.Fatalf("DeepCopy() task metadata aliases source (-want +got) diff = %s", diff)
				}
				if diff := cmp.Diff("正文", part.Data().(map[string]any)["body"].([]any)[0]); diff != "" {
					t.Fatalf("DeepCopy() data part aliases source (-want +got) diff = %s", diff)
				}
			}},
		{name: "empty bytes nil and arrays", copy: deepCopyCase(nonJSONSlices{Empty: []byte{}, Nested: [1][]int{{}}}), want: nonJSONSlices{Empty: []byte{}, Nested: [1][]int{{}}}},
		{name: "named empty slice", copy: deepCopyCase(sliceAlias{}), want: sliceAlias{}},
		{name: "gob codec owns its result", copy: deepCopyCase(gobSliceCodec{Values: []string{}}), want: gobSliceCodec{}},
		{name: "binary codec owns its result", copy: deepCopyCase(binarySliceCodec{Values: []string{}}), want: binarySliceCodec{}},
		{name: "gob codec takes precedence over binary", copy: deepCopyCase(gobPriorityCodec{Values: []string{}}), want: gobPriorityCodec{}},
		{name: "text-only codec uses structural gob", copy: deepCopyCase(textSliceCodec{Values: []string{}}), want: textSliceCodec{Values: []string{}}},
		{name: "gob encoding errors are retained", copy: deepCopyCase(map[string]any{"unsupported": func() {}}), wantErr: true},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := tc.copy()
			if tc.wantErr {
				if err == nil {
					t.Fatal("DeepCopy() error = nil, want gob encoding error")
				}
				return
			}
			if err != nil {
				t.Fatalf("DeepCopy() error = %v, want nil", err)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("DeepCopy() wrong result (-want +got) diff = %s", diff)
			}
			if tc.checkIsolation != nil {
				tc.checkIsolation(t, got)
			}
		})
	}
}
