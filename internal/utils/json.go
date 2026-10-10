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
	"fmt"
	"io"
)

// ErrTrailingData is returned by [DecodeJSON] and [ExpectEOF] when the input has more data after the first JSON value.
var ErrTrailingData = errors.New("unexpected data after JSON value")

// DecodeJSON decodes exactly one JSON value from r into v. Unlike a single json.Decoder.Decode call,
// it fails when anything other than whitespace follows the value, as json.Unmarshal does.
func DecodeJSON(r io.Reader, v any) error {
	dec := json.NewDecoder(r)
	if err := dec.Decode(v); err != nil {
		return err
	}
	return ExpectEOF(dec)
}

// ExpectEOF reports [ErrTrailingData] if dec has anything other than whitespace left to read.
// It lets callers that configure their own decoder (e.g. with UseNumber) apply the same check as [DecodeJSON].
func ExpectEOF(dec *json.Decoder) error {
	tok, err := dec.Token()
	switch {
	case errors.Is(err, io.EOF):
		return nil
	case err != nil:
		return fmt.Errorf("%w: %w", ErrTrailingData, err)
	default:
		return fmt.Errorf("%w: %v", ErrTrailingData, tok)
	}
}
