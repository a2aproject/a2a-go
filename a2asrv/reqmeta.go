package a2asrv

import (
	"slices"
	"strings"
)

// RequestMeta holds the data like auth headers, signatures, etc.
// Custom transport implementations pass the values to WithCallContext to make it accessible
// in a transport-agnostic way.
type RequestMeta struct {
	keyVals map[string][]string
}

// NewRequestMeta creates a new immutable RequestMeta.
func NewRequestMeta(m map[string][]string) *RequestMeta {
	if m == nil {
		return &RequestMeta{keyVals: map[string][]string{}}
	}

	keyVals := make(map[string][]string, len(m))
	for k, v := range keyVals {
		m[strings.ToLower(k)] = slices.Clone(v)
	}
	return &RequestMeta{keyVals: keyVals}
}

// Get performs a case-insensitive lookup of values for the given key.
func (rm *RequestMeta) Get(key string) ([]string, bool) {
	if rm == nil {
		return nil, false
	}

	val, ok := rm.keyVals[strings.ToLower(key)]
	return val, ok
}
