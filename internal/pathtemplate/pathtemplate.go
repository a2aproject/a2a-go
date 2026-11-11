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
	"net/url"
	"strings"
)

// Template represents a compiled path template.
type Template struct {
	prefix, capture, suffix []string
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
	return &Template{
		prefix:  splitSegments(raw[:captureStart]),
		capture: splitSegments(raw[captureStart+1 : captureEnd]),
		suffix:  splitSegments(raw[captureEnd+1:]),
	}, nil
}

// Match attempts to match the provided path against the template.
// If the path matches, it returns the MatchResult and true.
// Otherwise, it returns nil and false.
func (c *Template) Match(path string) (*MatchResult, bool) {
	return c.match(strings.Split(trimSlash(path), "/"), false, false)
}

// MatchEscaped matches an escaped URL path one segment at a time. Captured is
// decoded, while Rest retains its escaping for further HTTP routing.
// The capture group matches either a single segment which decodes to a value
// matching all of the group segments (e.g. "locations%2Fx" for "{locations/*}"),
// or one segment per group segment. The single segment form is tried first.
func (c *Template) MatchEscaped(path string) (*MatchResult, bool) {
	segments := strings.Split(trimSlash(path), "/")
	if result, ok := c.match(segments, true, true); ok {
		return result, true
	}
	return c.match(segments, true, false)
}

func (c *Template) match(segments []string, escaped, singleSegmentCapture bool) (*MatchResult, bool) {
	if _, ok := matchSegments(c.prefix, segments, escaped); !ok {
		return nil, false
	}
	segments = segments[len(c.prefix):]

	captured, consumed, ok := c.matchCapture(segments, escaped, singleSegmentCapture)
	if !ok {
		return nil, false
	}
	segments = segments[consumed:]

	if _, ok := matchSegments(c.suffix, segments, escaped); !ok {
		return nil, false
	}
	return &MatchResult{
		Captured: strings.Join(captured, "/"),
		Rest:     "/" + strings.Join(segments[len(c.suffix):], "/"),
	}, true
}

func (c *Template) matchCapture(segments []string, escaped, singleSegment bool) ([]string, int, bool) {
	if !singleSegment {
		captured, ok := matchSegments(c.capture, segments, escaped)
		return captured, len(c.capture), ok
	}
	if len(segments) == 0 {
		return nil, 0, false
	}
	decoded, err := url.PathUnescape(segments[0])
	if err != nil {
		return nil, 0, false
	}
	parts := strings.Split(decoded, "/")
	if len(parts) != len(c.capture) {
		return nil, 0, false
	}
	captured, ok := matchSegments(c.capture, parts, false)
	return captured, 1, ok
}

func matchSegments(template, segments []string, escaped bool) ([]string, bool) {
	if len(segments) < len(template) {
		return nil, false
	}
	matched := make([]string, len(template))
	for i, tSegment := range template {
		pSegment := segments[i]
		if escaped {
			var err error
			if pSegment, err = url.PathUnescape(pSegment); err != nil {
				return nil, false
			}
		}
		if tSegment != "*" && tSegment != pSegment {
			return nil, false
		}
		matched[i] = pSegment
	}
	return matched, true
}

func splitSegments(s string) []string {
	var segments []string
	for segment := range strings.SplitSeq(trimSlash(s), "/") {
		if segment != "" {
			segments = append(segments, segment)
		}
	}
	return segments
}

func trimSlash(s string) string {
	return strings.TrimSuffix(strings.TrimPrefix(s, "/"), "/")
}
