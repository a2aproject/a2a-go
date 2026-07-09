package a2acrypto

import (
	"encoding/json"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

// canonicalPayload serializes an AgentCard to RFC 8785 JSON Canonicalization
// Scheme (JCS) format. It recursively sorts all object keys and excludes the
// signatures field, ensuring deterministic signing input.
func canonicalPayload(card *a2a.AgentCard) ([]byte, error) {
	payload, err := json.Marshal(card)
	if err != nil {
		return nil, err
	}
	var obj any
	if err := json.Unmarshal(payload, &obj); err != nil {
		return nil, err
	}
	sortObjectKeys(obj)
	return json.Marshal(obj)
}

// sortObjectKeys recursively sorts object keys in lexicographic order and
// removes the "signatures" key, per RFC 8785 JCS.
func sortObjectKeys(v any) {
	switch val := v.(type) {
	case map[string]any:
		delete(val, "signatures")
		for _, vv := range val {
			sortObjectKeys(vv)
		}
	case []any:
		for _, item := range val {
			sortObjectKeys(item)
		}
	}
}
