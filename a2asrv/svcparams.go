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

package a2asrv

import (
	"iter"
	"maps"
	"slices"
	"strings"
)

// ServiceParams holds the metadata associated with a request.
// Custom transport implementations can call [NewCallContext] to make it accessible during request processing.
type ServiceParams struct {
	kv map[string][]string
}

// NewServiceParams is a [ServiceParams] constructor function.
func NewServiceParams(src map[string][]string) *ServiceParams {
	if src == nil {
		return &ServiceParams{kv: map[string][]string{}}
	}

	kv := make(map[string][]string, len(src))
	for k, v := range src {
		kv[strings.ToLower(k)] = slices.Clone(v)
	}
	return &ServiceParams{kv: kv}
}

// Get performs a case-insensitive lookup of values for the given key.
func (sp *ServiceParams) Get(key string) ([]string, bool) {
	if sp == nil {
		return nil, false
	}

	val, ok := sp.kv[strings.ToLower(key)]
	return val, ok
}

// List allows to inspect all request meta values.
func (sp *ServiceParams) List() iter.Seq2[string, []string] {
	return func(yield func(string, []string) bool) {
		if sp == nil {
			return
		}
		for k, v := range sp.kv {
			if !yield(k, slices.Clone(v)) {
				return
			}
		}
	}
}

// With allows to create a ServiceParams instance holding the extended set of values.
func (sp *ServiceParams) With(additional map[string][]string) *ServiceParams {
	if len(additional) == 0 {
		return sp
	}

	merged := make(map[string][]string, len(additional)+len(sp.kv))
	maps.Copy(merged, sp.kv)
	for k, v := range additional {
		merged[strings.ToLower(k)] = slices.Clone(v)
	}
	return &ServiceParams{kv: merged}
}
