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

// Package pathtemplate provides utilities for parsing and matching URI path templates
// containing capture groups.
package pathtemplate

import (
	"fmt"
	"strings"
)

// Template represents a compiled path template.
type Template struct {
	segments []string
}

// MatchResult contains the result of a successful path match.
type MatchResult struct {
	// Captured is the part of the path captured by the {} group.
	Captured string
	// Rest is the remaining part of the path after the template segments.
	Rest string
}

// New compiles a raw path template string into a Template.
// A template must contain exactly one capture group {} which can span
// multiple path segments or be part of a single segment.
func New(raw string) (*Template, error) {
	raw = trimSlash(raw)
	if raw == "" {
		return nil, fmt.Errorf("empty template")
	}
	captureStart, captureEnd := strings.IndexByte(raw, '{'), strings.IndexByte(raw, '}')
	if captureStart < 0 || captureEnd < 0 {
		return nil, fmt.Errorf("no capture group {} in %s", raw)
	}
	if captureStart > captureEnd {
		return nil, fmt.Errorf("invalid capture group in %s", raw)
	}
	anotherOpen, anotherClose := strings.LastIndexByte(raw, '{'), strings.LastIndexByte(raw, '}')
	if captureStart != anotherOpen || captureEnd != anotherClose {
		return nil, fmt.Errorf("duplicate { or } in %s", raw)
	}

	var segments []string
	for s := range strings.SplitSeq(trimSlash(raw[:captureStart]), "/") {
		if s != "" {
			segments = append(segments, s)
		}
	}
	segments = append(segments, "{")
	for s := range strings.SplitSeq(trimSlash(raw[captureStart+1:captureEnd]), "/") {
		if s != "" {
			segments = append(segments, s)
		}
	}
	segments = append(segments, "}")
	for s := range strings.SplitSeq(trimSlash(raw[captureEnd+1:]), "/") {
		if s != "" {
			segments = append(segments, s)
		}
	}
	return &Template{segments: segments}, nil
}

// Match attempts to match the provided path against the template.
// If the path matches, it returns the MatchResult and true.
// Otherwise, it returns nil and false.
func (c *Template) Match(path string) (*MatchResult, bool) {
	segments := strings.Split(trimSlash(path), "/")
	capturedParts, inCapture := []string{}, false
	pathIdx := 0
	for tplIdx := range c.segments {
		tSegment := c.segments[tplIdx]
		if tSegment == "{" || tSegment == "}" {
			inCapture = tSegment == "{"
			continue
		}
		if pathIdx >= len(segments) {
			return nil, false
		}
		pSegment := segments[pathIdx]
		if tSegment != "*" && tSegment != segments[pathIdx] {
			return nil, false
		}
		if inCapture {
			capturedParts = append(capturedParts, pSegment)
		}
		pathIdx++
	}
	return &MatchResult{
		Captured: strings.Join(capturedParts, "/"),
		Rest:     "/" + strings.Join(segments[pathIdx:], "/"),
	}, true
}

func trimSlash(s string) string {
	return strings.TrimSuffix(strings.TrimPrefix(s, "/"), "/")
}
