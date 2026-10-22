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
	"os"
	"os/exec"
	"time"

	"github.com/a2aproject/a2a-go/v2/log"
)

// startFunc starts an ACP agent and returns its stdio stream: reads come from
// the agent's stdout, writes go to its stdin. Close must stop the agent.
// The stream must report [io.EOF] on read once the agent exits.
type startFunc func(ctx context.Context) (io.ReadWriteCloser, error)

// localStart returns a [startFunc] which runs argv in dir on the host. The
// agent's stderr goes to stderr, or is discarded if it is nil.
func localStart(argv []string, dir string, env []string, stderr io.Writer) startFunc {
	return func(ctx context.Context) (io.ReadWriteCloser, error) {
		if len(argv) == 0 {
			return nil, errors.New("acp agent command is empty")
		}
		c := exec.Command(argv[0], argv[1:]...)
		c.Dir = dir
		c.Env = append(os.Environ(), env...)
		c.Stderr = stderr
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
		p := &localProcess{cmd: c, stdin: stdin, stdout: stdout, exited: make(chan struct{})}
		go func() {
			p.waitErr = c.Wait()
			close(p.exited)
			// A child process which inherited stdout would keep it open after
			// the agent exits and hide the exit from the reader.
			time.AfterFunc(outputGrace, func() {
				if err := p.closeStdout(); err != nil {
					log.Warn(ctx, "failed to close acp agent stdout", "cause", err)
				}
			})
		}()
		return p, nil
	}
}

const outputGrace = 2 * time.Second

type localProcess struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  *os.File
	exited  chan struct{}
	waitErr error
}

// closeStdout closes the read end of stdout, tolerating a second close.
func (p *localProcess) closeStdout() error {
	if err := p.stdout.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		return err
	}
	return nil
}

func (p *localProcess) Read(b []byte) (int, error)  { return p.stdout.Read(b) }
func (p *localProcess) Write(b []byte) (int, error) { return p.stdin.Write(b) }

// Close closes stdin, which ACP agents treat as a request to exit, and
// terminates the process group if the agent doesn't exit in time.
func (p *localProcess) Close() error {
	stdinErr := p.stdin.Close()
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
