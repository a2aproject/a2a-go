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
	"errors"
	"strings"
	"testing"
)

func TestDecodeJSON(t *testing.T) {
	testCases := []struct {
		name    string
		input   string
		wantErr error
		wantAny bool
	}{
		{name: "single object", input: `{"a":1}`},
		{name: "trailing whitespace", input: "{\"a\":1}\n \t\r\n"},
		{name: "trailing garbage", input: `{"a":1}TRAILING_GARBAGE`, wantErr: ErrTrailingData},
		{name: "concatenated object", input: `{"a":1}{"a":2}`, wantErr: ErrTrailingData},
		{name: "extra closing brace", input: `{"a":1}}`, wantErr: ErrTrailingData},
		{name: "empty input", input: ``, wantAny: true},
		{name: "invalid json", input: `{"a":`, wantAny: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var got struct {
				A int `json:"a"`
			}
			err := DecodeJSON(strings.NewReader(tc.input), &got)
			switch {
			case tc.wantErr != nil:
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("DecodeJSON() error = %v, want %v", err, tc.wantErr)
				}
			case tc.wantAny:
				if err == nil {
					t.Fatal("DecodeJSON() error = nil, want an error")
				}
			default:
				if err != nil {
					t.Fatalf("DecodeJSON() error = %v", err)
				}
				if got.A != 1 {
					t.Fatalf("DecodeJSON() decoded a = %d, want 1", got.A)
				}
			}
		})
	}
}
