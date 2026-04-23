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

/*
Package errordetails provides utilities for working with error details.
*/
package errordetails

import (
	"encoding/json"
	"maps"
)

// Typed is a wrapper around a value that is marshaled with a type URL.
type Typed struct {
	TypeURL string
	Value   map[string]any
}

// NewTyped creates a new Typed error detail.
func NewTyped(t string, v map[string]any) *Typed {
	return &Typed{TypeURL: t, Value: v}
}

// MarshalJSON implements json.Marshaler.
func (w *Typed) MarshalJSON() ([]byte, error) {
	typed := maps.Clone(w.Value)
	typed["@type"] = w.TypeURL
	return json.Marshal(typed)
}

// UnmarshalJSON implements json.Unmarshaler.
func (w *Typed) UnmarshalJSON(data []byte) error {
	var v map[string]any
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	if t, ok := v["@type"].(string); ok {
		w.TypeURL = t
		delete(v, "@type")
	} else {
		w.TypeURL = "google.protobuf.Struct"
	}
	w.Value = v
	return nil
}
