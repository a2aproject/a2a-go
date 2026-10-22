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

// Package subprocess runs a child process and exposes its stdio as a stream,
// for line-based protocols such as ACP.
package subprocess

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/a2aproject/a2a-go/v2/log"
)

// Command describes a process to start.
type Command struct {
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

// Start runs cmd and returns its stdio: reads come from its stdout, writes go
// to its stdin. Reads report [io.EOF] once the process exits. Close closes
// stdin, which line-based servers treat as a request to exit, and terminates
// the process group if the process doesn't exit in time. ctx is only used for
// logging: the process outlives it.
func Start(ctx context.Context, cmd Command) (io.ReadWriteCloser, error) {
	if len(cmd.Argv) == 0 {
		return nil, errors.New("command is empty")
	}
	argv := cmd.Argv
	c := exec.Command(argv[0], argv[1:]...)
	c.Dir = cmd.Dir
	c.Env = append(os.Environ(), cmd.Env...)
	c.Stderr = cmd.Stderr
	c.WaitDelay = 5 * time.Second
	configureProcessGroup(c)
	stdin, err := c.StdinPipe()
	if err != nil {
		return nil, err
	}
	// Wait closes pipes made by StdoutPipe as soon as the process exits,
	// which could drop its last messages, so stdout is a pipe we own.
	stdout, stdoutW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	c.Stdout = stdoutW
	startErr := c.Start()
	if err := stdoutW.Close(); err != nil && startErr == nil {
		startErr = err
	}
	if startErr != nil {
		return nil, errors.Join(fmt.Errorf("starting %s: %w", argv[0], startErr), stdout.Close())
	}
	p := &process{cmd: c, stdin: stdin, stdout: stdout, exited: make(chan struct{})}
	go func() {
		p.waitErr = c.Wait()
		close(p.exited)
		// A child process which inherited stdout would keep it open after
		// the process exits and hide the exit from the reader.
		time.AfterFunc(outputGrace, func() {
			if err := p.closeStdout(); err != nil {
				log.Warn(ctx, "failed to close process stdout", "cause", err)
			}
		})
	}()
	return p, nil
}

const outputGrace = 2 * time.Second

type process struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  *os.File
	exited  chan struct{}
	waitErr error
}

// closeStdout closes the read end of stdout, tolerating a second close.
func (p *process) closeStdout() error {
	if err := p.stdout.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		return err
	}
	return nil
}

// Read reads stdout. Once stdout was closed, after the process exited or by
// Close, reads report [io.EOF].
func (p *process) Read(b []byte) (int, error) {
	n, err := p.stdout.Read(b)
	if errors.Is(err, os.ErrClosed) {
		err = io.EOF
	}
	return n, err
}

func (p *process) Write(b []byte) (int, error) { return p.stdin.Write(b) }

// Close closes stdin and terminates the process group if the process doesn't
// exit in time.
func (p *process) Close() error {
	// Wait closes stdin once the process exits, which is fine here.
	stdinErr := p.stdin.Close()
	if errors.Is(stdinErr, os.ErrClosed) {
		stdinErr = nil
	}
	select {
	case <-p.exited:
	case <-time.After(2 * time.Second):
		if err := terminateProcessGroup(p.cmd); err != nil {
			return errors.Join(stdinErr, err)
		}
		<-p.exited
	}
	stdoutErr := p.closeStdout()
	var exitErr *exec.ExitError
	if p.waitErr != nil && !errors.As(p.waitErr, &exitErr) {
		return errors.Join(stdinErr, stdoutErr, p.waitErr)
	}
	return errors.Join(stdinErr, stdoutErr)
}
