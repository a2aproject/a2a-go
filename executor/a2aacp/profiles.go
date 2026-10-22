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
	"fmt"
	"maps"
	"slices"
)

// profiles are the commands which start known ACP agents, selected with [Config.Profile].
var profiles = map[string][]string{
	"opencode": {"opencode", "acp"},
	"claude":   {"npx", "-y", "@zed-industries/claude-agent-acp"},
	"codex":    {"npx", "-y", "@zed-industries/codex-acp"},
	"gemini":   {"gemini", "--experimental-acp"},
}

// agentCommand returns the command line of the agent from cfg.
func agentCommand(cfg Config) ([]string, error) {
	if len(cfg.Command) > 0 {
		return slices.Clone(cfg.Command), nil
	}
	if cfg.Profile == "" {
		return nil, fmt.Errorf("a2aacp: one of Config.Command or Config.Profile is required")
	}
	argv, ok := profiles[cfg.Profile]
	if !ok {
		return nil, fmt.Errorf("a2aacp: unknown profile %q, known profiles: %v", cfg.Profile, slices.Sorted(maps.Keys(profiles)))
	}
	return slices.Clone(argv), nil
}
