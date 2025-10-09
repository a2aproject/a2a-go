package a2asrv

import (
	"slices"
	"strings"
)

// RequestMeta holds the data like auth headers, signatures, etc.
// Custom transport implementations pass the values to WithCallContext to make it accessible
// in a transport-agnostic way.
type RequestMeta struct {
	kv map[string][]string
}

// NewRequestMeta creates a new immutable RequestMeta.
func NewRequestMeta(src map[string][]string) *RequestMeta {
	if src == nil {
		return &RequestMeta{kv: map[string][]string{}}
	}

	kv := make(map[string][]string, len(src))
	for k, v := range src {
		kv[strings.ToLower(k)] = slices.Clone(v)
	}
	return &RequestMeta{kv: kv}
}

// Get performs a case-insensitive lookup of values for the given key.
func (rm *RequestMeta) Get(key string) ([]string, bool) {
	if rm == nil {
		return nil, false
	}

	val, ok := rm.kv[strings.ToLower(key)]
	return val, ok
}
