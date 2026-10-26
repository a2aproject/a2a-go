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
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"sync"

	"github.com/a2aproject/a2a-go/v2/internal/subprocess"
)

// profiles are the commands which start known ACP agents, selected with [Config.Profile].
var profiles = map[string][]string{
	"opencode": {"opencode", "acp"},
	"claude":   {"npx", "-y", "@zed-industries/claude-agent-acp"},
	"codex":    {"npx", "-y", "@zed-industries/codex-acp"},
	"gemini":   {"gemini", "--experimental-acp"},
}

// ProfileCommand returns the command line which starts a known agent:
// "opencode", "claude", "codex" or "gemini". Use it to run a known agent in
// a sandbox from [Config.Start].
func ProfileCommand(profile string) ([]string, error) {
	argv, ok := profiles[profile]
	if !ok {
		return nil, fmt.Errorf("a2aacp: unknown profile %q, known profiles: %v", profile, slices.Sorted(maps.Keys(profiles)))
	}
	return slices.Clone(argv), nil
}

// agentCommand returns the command line of the agent from cfg.
func agentCommand(cfg Config) ([]string, error) {
	if len(cfg.Command) > 0 {
		return slices.Clone(cfg.Command), nil
	}
	if cfg.Profile == "" {
		return nil, errors.New("a2aacp: one of Config.Start, Config.Command or Config.Profile is required")
	}
	return ProfileCommand(cfg.Profile)
}

// Process describes a local agent process.
type Process struct {
	// Argv is the program and its arguments. The program is looked up in PATH
	// if it has no path separators.
	Argv []string
	// Dir is the working directory of the process.
	Dir string
	// Env is added to the environment of the current process.
	Env []string
	// Stderr receives the process's stderr. Discarded if nil.
	Stderr io.Writer
}

// StartProcess starts p and returns its stdio, as [Config.Start] does: reads
// come from its stdout, writes go to its stdin. Close closes stdin, which ACP
// agents treat as a request to exit, then terminates the process group if
// the process doesn't exit in time, so children such as MCP servers don't
// outlive it. The process outlives ctx.
func StartProcess(ctx context.Context, p Process) (io.ReadWriteCloser, error) {
	return subprocess.Start(ctx, subprocess.Command{Argv: p.Argv, Dir: p.Dir, Env: p.Env, Stderr: p.Stderr})
}

// WithCleanup returns rwc with a Close which also calls cleanup once rwc is
// closed, for example to remove the sandbox the agent ran in. cleanup runs
// once, even if Close is called again.
func WithCleanup(rwc io.ReadWriteCloser, cleanup func() error) io.ReadWriteCloser {
	return &cleanupRWC{ReadWriteCloser: rwc, cleanup: cleanup}
}

type cleanupRWC struct {
	io.ReadWriteCloser
	cleanup func() error

	once sync.Once
	err  error
}

func (c *cleanupRWC) Close() error {
	c.once.Do(func() {
		c.err = errors.Join(c.ReadWriteCloser.Close(), c.cleanup())
	})
	return c.err
}
