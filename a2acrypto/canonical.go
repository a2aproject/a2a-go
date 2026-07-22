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

package a2acrypto

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

// canonicalPayload serializes an AgentCard to RFC 8785 JSON Canonicalization
// Scheme (JCS) format. It recursively sorts all object keys by UTF-16 code
// unit order and excludes the top-level signatures field, ensuring
// deterministic signing input.
func canonicalPayload(card *a2a.AgentCard) ([]byte, error) {
	payload, err := json.Marshal(card)
	if err != nil {
		return nil, err
	}
	var obj any
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.UseNumber()
	if err := dec.Decode(&obj); err != nil {
		return nil, err
	}
	sortObjectKeys(obj, true)
	return jcsMarshal(obj)
}

// jcsMarshal serializes v as RFC 8785 canonical JSON.
//
// Key requirements:
//   - Object keys sorted by UTF-16 code unit order (RFC 8785 §3.2.3)
//   - No whitespace outside string literals
//   - Strings: only escape '"', '\', and control characters (U+0000–U+001F)
//   - U+2028 LINE SEPARATOR and U+2029 PARAGRAPH SEPARATOR are literal
//     (Go's encoding/json escapes them even with SetEscapeHTML(false))
//   - Numbers: no leading zeros, no trailing zeros after decimal
//   - No escaping of &, <, > per RFC 8785 §3.2.2.2
func jcsMarshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	if err := writeJCS(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeJCS(w io.Writer, v any) error {
	switch val := v.(type) {
	case nil:
		_, err := w.Write([]byte("null"))
		return err
	case bool:
		if val {
			_, err := w.Write([]byte("true"))
			return err
		}
		_, err := w.Write([]byte("false"))
		return err
	case json.Number:
		_, err := w.Write([]byte(canonicalNumber(val)))
		return err
	case float64:
		_, err := w.Write([]byte(canonicalFloat(val)))
		return err
	case string:
		return writeJCSString(w, val)
	case map[string]any:
		return writeJCSObject(w, val)
	case []any:
		return writeJCSArray(w, val)
	default:
		return fmt.Errorf("unsupported JCS type: %T", v)
	}
}

func writeJCSObject(w io.Writer, obj map[string]any) error {
	if _, err := w.Write([]byte("{")); err != nil {
		return err
	}
	keys := sortedKeys(obj)
	for i, k := range keys {
		if i > 0 {
			if _, err := w.Write([]byte(",")); err != nil {
				return err
			}
		}
		if err := writeJCSString(w, k); err != nil {
			return err
		}
		if _, err := w.Write([]byte(":")); err != nil {
			return err
		}
		if err := writeJCS(w, obj[k]); err != nil {
			return err
		}
	}
	if _, err := w.Write([]byte("}")); err != nil {
		return err
	}
	return nil
}

func writeJCSArray(w io.Writer, arr []any) error {
	if _, err := w.Write([]byte("[")); err != nil {
		return err
	}
	for i, v := range arr {
		if i > 0 {
			if _, err := w.Write([]byte(",")); err != nil {
				return err
			}
		}
		if err := writeJCS(w, v); err != nil {
			return err
		}
	}
	if _, err := w.Write([]byte("]")); err != nil {
		return err
	}
	return nil
}

// writeJCSString writes a JSON string with RFC 8785 escaping.
// Only escapes: ", \, and control characters (U+0000–U+001F).
// U+2028, U+2029, &, <, > are written as literal UTF-8 bytes.
func writeJCSString(w io.Writer, s string) error {
	if _, err := w.Write([]byte("\"")); err != nil {
		return err
	}
	for _, r := range s {
		switch r {
		case '"':
			if _, err := w.Write([]byte("\\\"")); err != nil {
				return err
			}
		case '\\':
			if _, err := w.Write([]byte("\\\\")); err != nil {
				return err
			}
		case '\b':
			if _, err := w.Write([]byte("\\b")); err != nil {
				return err
			}
		case '\f':
			if _, err := w.Write([]byte("\\f")); err != nil {
				return err
			}
		case '\n':
			if _, err := w.Write([]byte("\\n")); err != nil {
				return err
			}
		case '\r':
			if _, err := w.Write([]byte("\\r")); err != nil {
				return err
			}
		case '	':
			if _, err := w.Write([]byte("\\t")); err != nil {
				return err
			}
		default:
			if r < 0x0020 {
				if _, err := fmt.Fprintf(w, "\\u%04x", r); err != nil {
					return err
				}
			} else {
				// U+2028, U+2029, U+0080+, and printable ASCII — literal bytes.
				// This is the key difference from Go's encoding/json, which
				// escapes U+2028 and U+2029 as \u2028 / \u2029.
				if _, err := w.Write([]byte(string(r))); err != nil {
					return err
				}
			}
		}
	}
	if _, err := w.Write([]byte("\"")); err != nil {
		return err
	}
	return nil
}

// sortedKeys returns object keys sorted by UTF-16 code unit order (RFC 8785 §3.2.3).
func sortedKeys(obj map[string]any) []string {
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return utf16Compare(keys[i], keys[j]) < 0
	})
	return keys
}

// utf16Compare compares two strings by UTF-16 code unit order.
// Returns -1, 0, or 1.
func utf16Compare(a, b string) int {
	ua := utf16.Encode([]rune(a))
	ub := utf16.Encode([]rune(b))
	n := len(ua)
	if len(ub) < n {
		n = len(ub)
	}
	for i := 0; i < n; i++ {
		if ua[i] < ub[i] {
			return -1
		}
		if ua[i] > ub[i] {
			return 1
		}
	}
	if len(ua) < len(ub) {
		return -1
	}
	if len(ua) > len(ub) {
		return 1
	}
	return 0
}

// canonicalNumber formats a json.Number without unnecessary trailing zeros.
func canonicalNumber(n json.Number) string {
	s := string(n)
	if strings.ContainsAny(s, ".eE") {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return s
		}
		return canonicalFloat(f)
	}
	i, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return s
	}
	return strconv.FormatInt(i, 10)
}

// canonicalFloat formats a float64 without unnecessary trailing zeros.
func canonicalFloat(f float64) string {
	s := strconv.FormatFloat(f, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		return s
	}
	if strings.Contains(s, "e") {
		return strings.ToLower(s)
	}
	return s
}

// sortObjectKeys recursively sorts object keys in lexicographic order and,
// when root is true, removes the top-level "signatures" key per A2A §8.4.1.
func sortObjectKeys(v any, root bool) {
	switch val := v.(type) {
	case map[string]any:
		if root {
			delete(val, "signatures")
		}
		for _, vv := range val {
			sortObjectKeys(vv, false)
		}
	case []any:
		for _, item := range val {
			sortObjectKeys(item, false)
		}
	}
}
