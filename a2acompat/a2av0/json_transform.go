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
	"encoding/json"
	"strings"
	"unicode"
)

// marshalSnakeCase marshals v to JSON and then transforms all object keys from
// camelCase to snake_case. This allows reusing the existing legacy types (which
// have camelCase JSON tags) while producing the snake_case JSON that v0.3 REST
// clients expect.
func marshalSnakeCase(v any) ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return transformJSONKeys(data, camelToSnake)
}

// unmarshalSnakeCase takes snake_case JSON from a v0.3 REST client and
// transforms it to camelCase before unmarshalling into v (which has camelCase
// JSON tags).
func unmarshalSnakeCase(data []byte, v any) error {
	data, err := transformJSONKeys(data, snakeToCamel)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// transformJSONKeys walks a JSON value and applies fn to every object key.
func transformJSONKeys(data []byte, fn func(string) string) ([]byte, error) {
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	transformed := walkJSON(raw, fn)
	return json.Marshal(transformed)
}

// walkJSON recursively walks a decoded JSON value and applies fn to every
// map key.
func walkJSON(v any, fn func(string) string) any {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, v := range val {
			out[fn(k)] = walkJSON(v, fn)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, v := range val {
			out[i] = walkJSON(v, fn)
		}
		return out
	default:
		return v
	}
}

// camelToSnake converts a camelCase string to snake_case.
// e.g. "messageId" → "message_id", "contextId" → "context_id"
func camelToSnake(s string) string {
	var b strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) {
			if i > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(unicode.ToLower(r))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// snakeToCamel converts a snake_case string to camelCase.
// e.g. "message_id" → "messageId", "context_id" → "contextId"
func snakeToCamel(s string) string {
	parts := strings.Split(s, "_")
	if len(parts) <= 1 {
		return s
	}
	var b strings.Builder
	b.WriteString(parts[0])
	for _, p := range parts[1:] {
		if len(p) == 0 {
			continue
		}
		b.WriteRune(unicode.ToUpper(rune(p[0])))
		b.WriteString(p[1:])
	}
	return b.String()
}
