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
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestDecodeJSON(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		input   string
		wantErr error
	}{
		{name: "single object", input: `{"a":1}`},
		{name: "trailing whitespace", input: "{\"a\":1}\n \t\r\n"},
		{name: "trailing garbage", input: `{"a":1}TRAILING_GARBAGE`, wantErr: ErrTrailingData},
		{name: "concatenated object", input: `{"a":1}{"a":2}`, wantErr: ErrTrailingData},
		{name: "extra closing brace", input: `{"a":1}}`, wantErr: ErrTrailingData},
		{name: "empty input", input: ``, wantErr: io.EOF},
		{name: "invalid json", input: `{"a":`, wantErr: io.ErrUnexpectedEOF},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var got struct {
				A int `json:"a"`
			}
			err := DecodeJSON(strings.NewReader(tc.input), &got)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("DecodeJSON() error = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("DecodeJSON() error = %v", err)
			}
			if got.A != 1 {
				t.Fatalf("DecodeJSON() decoded a = %d, want 1", got.A)
			}
		})
	}
}

func TestDecodeJSON_WrapsSyntaxError(t *testing.T) {
	t.Parallel()

	var got any
	err := DecodeJSON(strings.NewReader(`{"a":1}TRAILING_GARBAGE`), &got)

	var syntaxErr *json.SyntaxError
	if !errors.As(err, &syntaxErr) {
		t.Fatalf("DecodeJSON() error = %v, want it to wrap *json.SyntaxError", err)
	}
	if !errors.Is(err, ErrTrailingData) {
		t.Fatalf("DecodeJSON() error = %v, want %v", err, ErrTrailingData)
	}
}

func TestExpectEOF(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		input   string
		wantErr error
	}{
		{name: "number with trailing whitespace", input: "1 \n"},
		{name: "number followed by value", input: `1 2`, wantErr: ErrTrailingData},
		{name: "number followed by garbage", input: `1 x`, wantErr: ErrTrailingData},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dec := json.NewDecoder(strings.NewReader(tc.input))
			dec.UseNumber()
			var got json.Number
			if err := dec.Decode(&got); err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			if got != "1" {
				t.Fatalf("Decode() = %s, want 1", got)
			}

			err := ExpectEOF(dec)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("ExpectEOF() error = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ExpectEOF() error = %v", err)
			}
		})
	}
}
