package a2acrypto

import (
	"bytes"
	"encoding/json"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

// canonicalPayload serializes an AgentCard to RFC 8785 JSON Canonicalization
// Scheme (JCS) format. It recursively sorts all object keys and excludes the
// top-level signatures field, ensuring deterministic signing input.
//
// Note: Go's json.Marshal sorts map keys by UTF-8 byte order. RFC 8785 §3.2.3
// specifies UTF-16 code unit ordering. The orderings diverge when keys mix
// U+E000..U+FFFF characters with supplementary-plane characters. For ASCII-only
// keys (the common case for AgentCard field names), UTF-8 and UTF-16 ordering
// are identical.
func canonicalPayload(card *a2a.AgentCard) ([]byte, error) {
	payload, err := json.Marshal(card)
	if err != nil {
		return nil, err
	}
	var obj any
	if err := json.Unmarshal(payload, &obj); err != nil {
		return nil, err
	}
	sortObjectKeys(obj, true)
	return jcsMarshal(obj)
}

// jcsMarshal serializes v to JSON without HTML-escaping &, <, > characters,
// as required by RFC 8785.
func jcsMarshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	// Encode appends a trailing newline; strip it for canonical output.
	b := buf.Bytes()
	if len(b) > 0 && b[len(b)-1] == '\n' {
		b = b[:len(b)-1]
	}
	return b, nil
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
