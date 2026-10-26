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

package a2aacp

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

// AutoAnswerer answers questions without asking the caller. See [Config.AutoAnswer].
type AutoAnswerer interface {
	// AutoAnswer returns the answer to q, or false to ask the caller. An
	// answer which doesn't satisfy the request schema is logged and the caller
	// is asked. [OptionCancel] cancels any question.
	AutoAnswer(ctx context.Context, execCtx *a2asrv.ExecutorContext, q Question) (Answer, bool)
}

// AutoAnswerFunc is a function which implements [AutoAnswerer].
type AutoAnswerFunc func(ctx context.Context, execCtx *a2asrv.ExecutorContext, q Question) (Answer, bool)

// AutoAnswer implements [AutoAnswerer].
func (f AutoAnswerFunc) AutoAnswer(ctx context.Context, execCtx *a2asrv.ExecutorContext, q Question) (Answer, bool) {
	return f(ctx, execCtx, q)
}

// ToolRules is an [AutoAnswerer] which answers permission requests by the
// ACP tool kind: read, edit, delete, move, search, execute, think, fetch or
// other. In patterns, * matches any run of characters. Deny takes precedence.
// An allowed request is approved once, or always if the agent offers no other
// way to approve it. A denied request is rejected, or canceled if the agent
// offers no way to reject it, so it never reaches the caller.
type ToolRules struct {
	Allow []string
	Deny  []string
}

var _ AutoAnswerer = ToolRules{}

// AutoAnswer implements [AutoAnswerer].
func (r ToolRules) AutoAnswer(_ context.Context, _ *a2asrv.ExecutorContext, q Question) (Answer, bool) {
	if q.Tool == nil {
		return Answer{}, false
	}
	if matchAny(r.Deny, q.Tool.Kind) {
		if id := firstOffered(q, OptionReject, OptionRejectAlways); id != "" {
			return Answer{Option: id}, true
		}
		return Answer{Option: OptionCancel}, true
	}
	if matchAny(r.Allow, q.Tool.Kind) {
		if id := firstOffered(q, OptionOnce, OptionAlways); id != "" {
			return Answer{Option: id}, true
		}
	}
	return Answer{}, false
}

func matchAny(patterns []string, s string) bool {
	return slices.ContainsFunc(patterns, func(p string) bool { return matchGlob(p, s) })
}

// matchGlob reports whether s matches pattern, in which * matches any run of
// characters and every other character matches itself.
func matchGlob(pattern, s string) bool {
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return pattern == s
	}
	first, last := parts[0], parts[len(parts)-1]
	if !strings.HasPrefix(s, first) {
		return false
	}
	s = s[len(first):]
	for _, mid := range parts[1 : len(parts)-1] {
		i := strings.Index(s, mid)
		if i < 0 {
			return false
		}
		s = s[i+len(mid):]
	}
	return strings.HasSuffix(s, last)
}

// SettingsMetadataKey is the metadata key [MetadataSettings] reads session settings from.
const SettingsMetadataKey = "acpSettings"

// MetadataSettings returns a [Config.Settings] which takes session settings
// from {"acpSettings": {"<setting id>": <value>}} in the request and message
// metadata, the latter taking precedence. A message which changes a setting
// whose ID matches none of the allowed patterns (see [ToolRules]) fails.
//
// Settings can weaken the agent's safeguards: some agents have modes which
// skip permission requests. Only allow what every caller may change.
func MetadataSettings(allowed ...string) func(context.Context, *a2asrv.ExecutorContext) (map[string]any, error) {
	return func(_ context.Context, execCtx *a2asrv.ExecutorContext) (map[string]any, error) {
		settings := map[string]any{}
		maps.Copy(settings, metadataSettings(execCtx.Metadata))
		if execCtx.Message != nil {
			maps.Copy(settings, metadataSettings(execCtx.Message.Metadata))
		}
		for _, id := range slices.Sorted(maps.Keys(settings)) {
			if !matchAny(allowed, id) {
				return nil, fmt.Errorf("the session setting %q can't be changed in a message", id)
			}
		}
		return settings, nil
	}
}

func metadataSettings(md map[string]any) map[string]any {
	settings, _ := md[SettingsMetadataKey].(map[string]any)
	return settings
}
