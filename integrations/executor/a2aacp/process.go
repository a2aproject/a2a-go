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
	"io"
	"sync"

	"github.com/a2aproject/a2a-go/v2/internal/subprocess"
)

// Agent starts ACP agent processes for an [Executor].
type Agent interface {
	// Start starts the agent of a key and returns its stdio: reads come from
	// the agent's output, writes go to its input, and Close stops the agent.
	// key is the agent key of [PoolConfig.Key], or "" without a pool. The
	// agent must outlive ctx, which ends once the agent is initialized. Return
	// an error wrapping [ErrNotOwner] if another node runs the agent of the key.
	Start(ctx context.Context, key string) (io.ReadWriteCloser, error)
}

// AgentFunc is a function which implements [Agent], for example to start the
// agent in a sandbox of the key. See [Process] and [WithCleanup].
type AgentFunc func(ctx context.Context, key string) (io.ReadWriteCloser, error)

// Start implements [Agent].
func (f AgentFunc) Start(ctx context.Context, key string) (io.ReadWriteCloser, error) {
	return f(ctx, key)
}

// Process is an [Agent] which runs a local command, for example
// Process{Argv: []string{"opencode", "acp"}}. It runs the same command for
// every key.
type Process struct {
	// Argv is the program and its arguments. The program is looked up in PATH
	// if it has no path separators.
	Argv []string
	// Dir is the working directory of the process, and the default working
	// directory of its sessions. Defaults to the current directory.
	Dir string
	// Env is added to the environment of the current process.
	Env []string
	// Stderr receives the process's stderr. Discarded if nil.
	Stderr io.Writer
}

var _ Agent = Process{}

// Start implements [Agent]. Close closes stdin, which ACP agents treat as a
// request to exit, then terminates the process group if the process doesn't
// exit in time, so children such as MCP servers don't outlive it.
func (p Process) Start(ctx context.Context, _ string) (io.ReadWriteCloser, error) {
	if len(p.Argv) == 0 {
		return nil, errors.New("a2aacp: Process.Argv is empty")
	}
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
