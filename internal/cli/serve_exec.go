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

package cli

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"iter"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

func serveExec(ctx context.Context, cfg *globalConfig, sc serveConfig, listener net.Listener, addr string, proto a2a.TransportProtocol, command, chunk, name, desc, cardFile string, quiet bool) error {
	if name == "" {
		name = "Exec Agent"
	}
	if desc == "" {
		desc = fmt.Sprintf("Wraps command: %s", command)
	}

	card, err := loadOrBuildCard(cardFile, name, desc, addr, proto)
	if err != nil {
		return err
	}

	caps := &a2a.AgentCapabilities{Streaming: chunk != ""}
	executor := newExecExecutor(command, chunk)
	handler := a2asrv.NewHandler(executor, a2asrv.WithCapabilityChecks(caps))

	transport := cfg.transport
	if transport == "" {
		transport = "rest"
	}

	cfg.logf("exec mode, command=%q chunk=%q transport=%s protocol=%s", command, chunk, transport, sc.protocol)
	return startTransportServer(ctx, listener, handler, card, transport, sc, quiet)
}

type execExecutor struct {
	command string
	chunk   string

	mu        sync.Mutex
	processes map[a2a.TaskID]*os.Process
}

func newExecExecutor(command, chunk string) *execExecutor {
	return &execExecutor{
		command:   command,
		chunk:     chunk,
		processes: make(map[a2a.TaskID]*os.Process),
	}
}

func (e *execExecutor) trackProcess(id a2a.TaskID, p *os.Process) {
	e.mu.Lock()
	e.processes[id] = p
	e.mu.Unlock()
}

func (e *execExecutor) untrackProcess(id a2a.TaskID) {
	e.mu.Lock()
	delete(e.processes, id)
	e.mu.Unlock()
}

func (e *execExecutor) killProcess(id a2a.TaskID) error {
	e.mu.Lock()
	p := e.processes[id]
	e.mu.Unlock()
	if p != nil {
		return p.Kill()
	}
	return nil
}

func (e *execExecutor) Execute(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		if execCtx.StoredTask == nil {
			if !yield(a2a.NewSubmittedTask(execCtx, execCtx.Message), nil) {
				return
			}
		}
		if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateWorking, nil), nil) {
			return
		}
		input := messageText(execCtx.Message)

		if e.chunk == "" {
			e.executeBuffered(ctx, execCtx, input, yield)
		} else {
			e.executeChunked(ctx, execCtx, input, yield)
		}
	}
}

func (e *execExecutor) executeBuffered(ctx context.Context, execCtx *a2asrv.ExecutorContext, input string, yield func(a2a.Event, error) bool) {
	cmd := exec.CommandContext(ctx, "sh", "-c", e.command)
	cmd.Stdin = strings.NewReader(input)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed,
			a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart(err.Error()))), nil)
		return
	}
	e.trackProcess(execCtx.TaskID, cmd.Process)
	defer e.untrackProcess(execCtx.TaskID)

	if err := cmd.Wait(); err != nil {
		msg := stderr.String()
		if msg == "" {
			msg = err.Error()
		}
		yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed,
			a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart(msg))), nil)
		return
	}

	evt := a2a.NewArtifactEvent(execCtx, a2a.NewTextPart(stdout.String()))
	evt.LastChunk = true
	if !yield(evt, nil) {
		return
	}
	yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCompleted, nil), nil)
}

func (e *execExecutor) executeChunked(ctx context.Context, execCtx *a2asrv.ExecutorContext, input string, yield func(a2a.Event, error) bool) {
	cmd := exec.CommandContext(ctx, "sh", "-c", e.command)
	cmd.Stdin = strings.NewReader(input)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		yield(nil, fmt.Errorf("creating stdout pipe: %w", err))
		return
	}
	if err := cmd.Start(); err != nil {
		yield(nil, fmt.Errorf("starting command: %w", err))
		return
	}
	e.trackProcess(execCtx.TaskID, cmd.Process)
	defer e.untrackProcess(execCtx.TaskID)

	scanner := bufio.NewScanner(stdout)
	const maxScanCapacity = 10 * 1024 * 1024 // 10 MB
	scanner.Buffer(make([]byte, 64*1024), maxScanCapacity)
	scanner.Split(splitByDelimiter(e.chunk))

	// Buffer one chunk ahead so we can set LastChunk on the final one.
	var pending *string
	var artifactID a2a.ArtifactID

	for scanner.Scan() {
		text := scanner.Text()
		if pending != nil {
			if artifactID == "" {
				evt := a2a.NewArtifactEvent(execCtx, a2a.NewTextPart(*pending))
				artifactID = evt.Artifact.ID
				if !yield(evt, nil) {
					return
				}
			} else {
				evt := a2a.NewArtifactUpdateEvent(execCtx, artifactID, a2a.NewTextPart(*pending))
				evt.Append = true
				if !yield(evt, nil) {
					return
				}
			}
		}
		pending = &text
	}
	if err := scanner.Err(); err != nil {
		yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed,
			a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart(fmt.Sprintf("reading output: %v", err)))), nil)
		return
	}

	// Emit the final chunk with LastChunk=true.
	if pending != nil {
		if artifactID == "" {
			evt := a2a.NewArtifactEvent(execCtx, a2a.NewTextPart(*pending))
			evt.LastChunk = true
			if !yield(evt, nil) {
				return
			}
		} else {
			evt := a2a.NewArtifactUpdateEvent(execCtx, artifactID, a2a.NewTextPart(*pending))
			evt.Append = true
			evt.LastChunk = true
			if !yield(evt, nil) {
				return
			}
		}
	}

	if err := cmd.Wait(); err != nil {
		msg := stderr.String()
		if msg == "" {
			msg = err.Error()
		}
		statusMsg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart(msg))
		yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed, statusMsg), nil)
		return
	}
	yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCompleted, nil), nil)
}

func (e *execExecutor) Cancel(_ context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		if err := e.killProcess(execCtx.TaskID); err != nil {
			yield(nil, err)
			return
		}
		yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCanceled, nil), nil)
	}
}

func splitByDelimiter(delim string) bufio.SplitFunc {
	return func(data []byte, atEOF bool) (advance int, token []byte, err error) {
		if atEOF && len(data) == 0 {
			return 0, nil, nil
		}
		if i := bytes.Index(data, []byte(delim)); i >= 0 {
			return i + len(delim), data[:i], nil
		}
		if atEOF {
			return len(data), data, nil
		}
		return 0, nil, nil
	}
}
