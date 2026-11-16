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

//go:build unix

package subprocess

import (
	"bufio"
	"io"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func start(t *testing.T, cmd Command) io.ReadWriteCloser {
	t.Helper()
	cmd.Dir = t.TempDir()
	p, err := Start(t.Context(), cmd)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	return p
}

func readAll(t *testing.T, r io.Reader) string {
	t.Helper()
	done := make(chan string, 1)
	go func() {
		b, err := io.ReadAll(r)
		if err != nil {
			t.Errorf("io.ReadAll() error = %v", err)
		}
		done <- string(b)
	}()
	select {
	case out := <-done:
		return out
	case <-time.After(10 * time.Second):
		t.Fatal("io.ReadAll() did not reach EOF")
		return ""
	}
}

func TestStart_KeepsOutputAfterExit(t *testing.T) {
	t.Parallel()
	p := start(t, Command{Argv: []string{"sh", "-c", "printf 'last words'"}})

	time.Sleep(100 * time.Millisecond)
	got := readAll(t, p)

	if got != "last words" {
		t.Fatalf("output = %q, want %q", got, "last words")
	}
	if err := p.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestStart_Stdio(t *testing.T) {
	t.Parallel()
	p := start(t, Command{Argv: []string{"cat"}})

	if _, err := io.WriteString(p, "ping\n"); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	line, err := bufio.NewReader(p).ReadString('\n')
	if err != nil {
		t.Fatalf("ReadString() error = %v", err)
	}

	if line != "ping\n" {
		t.Fatalf("ReadString() = %q, want %q", line, "ping\n")
	}
	if err := p.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestStart_Env(t *testing.T) {
	t.Parallel()
	p := start(t, Command{Argv: []string{"sh", "-c", "printf %s \"$GREETING\""}, Env: []string{"GREETING=hello"}})

	if got := readAll(t, p); got != "hello" {
		t.Fatalf("output = %q, want %q", got, "hello")
	}
	if err := p.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestStart_ReportsExitWhenChildHoldsStdout(t *testing.T) {
	t.Parallel()
	p := start(t, Command{Argv: []string{"sh", "-c", "sleep 30 & printf started"}})

	if got := readAll(t, p); got != "started" {
		t.Fatalf("output = %q, want %q", got, "started")
	}
	if err := p.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestClose_TerminatesProcessIgnoringStdin(t *testing.T) {
	t.Parallel()
	p := start(t, Command{Argv: []string{"sleep", "30"}})

	begin := time.Now()
	if err := p.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if elapsed := time.Since(begin); elapsed > 10*time.Second {
		t.Fatalf("Close() took %v, want the process terminated", elapsed)
	}
}

func TestClose_KillsProcessIgnoringSigterm(t *testing.T) {
	t.Parallel()
	p := start(t, Command{Argv: []string{"sh", "-c", `trap "" TERM; printf ready; while :; do sleep 0.1; done`}})
	if _, err := io.ReadFull(p, make([]byte, len("ready"))); err != nil {
		t.Fatalf("ReadFull() error = %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- p.Close() }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Close() did not return, want the process killed")
	}
}

func TestClose_TerminatesChildrenAfterCleanExit(t *testing.T) {
	t.Parallel()
	p := start(t, Command{Argv: []string{"sh", "-c", "sleep 30 >/dev/null & echo $!; cat >/dev/null"}})
	line, err := bufio.NewReader(p).ReadString('\n')
	if err != nil {
		t.Fatalf("ReadString() error = %v", err)
	}
	child, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatalf("strconv.Atoi(%q) error = %v", line, err)
	}

	if err := p.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for syscall.Kill(child, 0) == nil {
		if time.Now().After(deadline) {
			t.Fatalf("child process %d is alive after Close(), want it terminated", child)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestStart_Errors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
		want string
	}{
		{name: "empty", argv: nil, want: "command is empty"},
		{name: "missing binary", argv: []string{"a2a-no-such-binary"}, want: "starting a2a-no-such-binary"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := Start(t.Context(), Command{Argv: tc.argv})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Start() error = %v, want %q", err, tc.want)
			}
		})
	}
}
