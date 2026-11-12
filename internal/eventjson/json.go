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

// Package eventjson provides JSON event classification shared by strict decoders and streaming clients.
package eventjson

import (
	"encoding/json"
	"errors"
	"slices"
)

// ErrUnknownEventType identifies a valid JSON event wrapper with no recognized payload.
// The public event decoder stays strict; streaming clients may inspect its fields.
var ErrUnknownEventType = errors.New("unknown event type")

// NonNullJSONFields returns the sorted names of the non-null fields in a JSON object.
func NonNullJSONFields(data []byte) ([]string, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	var names []string
	for name, value := range fields {
		if string(value) != "null" {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names, nil
}
