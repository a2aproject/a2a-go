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
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/internal/acp"
	"github.com/google/go-cmp/cmp"
)

func TestExecutor_StartBackoff(t *testing.T) {
	t.Parallel()
	var starts atomic.Int32
	h := &fakeHarness{onPrompt: func(s *promptScript) { s.end(acp.StopEndTurn) }}
	e := newTestExecutor(t, h, Config{})
	e.agentStarter = AgentFunc(func(context.Context, string) (io.ReadWriteCloser, error) {
		starts.Add(1)
		return nil, errors.New("no sandbox")
	})
	h.agent(0).crash()
	waitFor(t, func() bool { return agentExited(e) })

	first := run(t, execute(e, newExecCtx("ctx", a2a.NewTextPart("one"))))
	second := run(t, execute(e, newExecCtx("ctx", a2a.NewTextPart("two"))))

	if diff := cmp.Diff([]eventSummary{submitted, failedWith("starting acp agent: no sandbox")}, first); diff != "" {
		t.Fatalf("Executor.Execute() wrong result (-want +got) diff = %s", diff)
	}
	if !strings.Contains(second[1].Text, "the agent failed to start, retrying after") {
		t.Fatalf("Executor.Execute() second failure = %q, want a retry notice", second[1].Text)
	}
	if got := starts.Load(); got != 1 {
		t.Fatalf("starts after the crash = %d, want 1", got)
	}
}

func TestNewExecutor_StartTimeout(t *testing.T) {
	t.Parallel()
	h := &fakeHarness{t: t, hangInitialize: true}

	_, err := NewExecutor(t.Context(), AgentFunc(h.start), &Config{StartTimeout: 10 * time.Millisecond})

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("NewExecutor() error = %v, want %v", err, context.DeadlineExceeded)
	}
	waitFor(t, h.agent(0).stopped)
}

func TestExecutor_CancelTimeoutStopsAgent(t *testing.T) {
	t.Parallel()
	h := &fakeHarness{}
	h.onPrompt = func(s *promptScript) {
		if s.promptText() == "next" {
			s.end(acp.StopEndTurn)
			return
		}
		s.text("m1", "Working on it")
	}
	e := newTestExecutor(t, h, Config{CancelTimeout: 10 * time.Millisecond})
	execCtx := newExecCtx("ctx", a2a.NewTextPart("hi"))

	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	defer cancel()
	var got []eventSummary
	for ev, err := range e.Execute(ctx, execCtx) {
		if err != nil {
			t.Fatalf("Executor.Execute() error = %v", err)
		}
		got = append(got, summarize(ev))
		if _, ok := ev.(*a2a.TaskArtifactUpdateEvent); ok {
			run(t, cancelTask(e, execCtx))
		}
	}

	want := []eventSummary{submitted, working, {Kind: "artifact", Text: "Working on it"}, canceled}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("Executor.Execute() wrong result (-want +got) diff = %s", diff)
	}
	got = run(t, execute(e, newExecCtx("ctx", a2a.NewTextPart("next"))))
	if diff := cmp.Diff([]eventSummary{submitted, working, completed}, got); diff != "" {
		t.Fatalf("Executor.Execute() after cancel wrong result (-want +got) diff = %s", diff)
	}
	if got := h.agentCount(); got != 2 {
		t.Fatalf("started agents = %d, want 2", got)
	}
}

func keyByContext(_ context.Context, contextID string) (string, error) {
	return "key-" + contextID, nil
}

func promptTexts(f *fakeAgent) []string {
	_, _, _, prompts := f.snapshot()
	var texts []string
	for _, p := range prompts {
		texts = append(texts, p.Prompt[0].Text)
	}
	return texts
}

func TestExecutor_AgentPerKey(t *testing.T) {
	t.Parallel()
	h := &fakeHarness{onPrompt: func(s *promptScript) { s.end(acp.StopEndTurn) }}
	e := newTestExecutor(t, h, Config{Pool: &PoolConfig{Key: keyByContext}})

	run(t, execute(e, newExecCtx("a", a2a.NewTextPart("one"))))
	run(t, execute(e, newExecCtx("b", a2a.NewTextPart("two"))))
	run(t, execute(e, newExecCtx("a", a2a.NewTextPart("three"))))

	if got := h.agentCount(); got != 2 {
		t.Fatalf("started agents = %d, want 2", got)
	}
	got := map[string][]string{h.agent(0).key: promptTexts(h.agent(0)), h.agent(1).key: promptTexts(h.agent(1))}
	want := map[string][]string{"key-a": {"one", "three"}, "key-b": {"two"}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("prompts by agent key wrong result (-want +got) diff = %s", diff)
	}
}

func TestExecutor_StoredAgentKeyWins(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	h := &fakeHarness{onPrompt: func(s *promptScript) { s.end(acp.StopEndTurn) }}
	e := newTestExecutor(t, h, Config{Pool: &PoolConfig{Key: func(context.Context, string) (string, error) {
		return fmt.Sprintf("key-%d", calls.Add(1)), nil
	}}})

	run(t, execute(e, newExecCtx("ctx", a2a.NewTextPart("one"))))
	run(t, execute(e, newExecCtx("ctx", a2a.NewTextPart("two"))))

	if got := calls.Load(); got != 1 {
		t.Fatalf("PoolConfig.Key calls = %d, want 1", got)
	}
	if diff := cmp.Diff([]string{"one", "two"}, promptTexts(h.agent(0))); diff != "" {
		t.Fatalf("prompts of the first agent wrong result (-want +got) diff = %s", diff)
	}
}

func TestExecutor_AgentsWithSameSessionIDs(t *testing.T) {
	t.Parallel()
	h := &fakeHarness{onPrompt: func(s *promptScript) {
		if s.askPermission(rmToolCall).Outcome == acp.OutcomeSelected {
			s.end(acp.StopEndTurn)
		} else {
			s.end(acp.StopCancelled)
		}
	}}
	e := newTestExecutor(t, h, Config{Pool: &PoolConfig{Key: keyByContext}})
	first := newExecCtx("a", a2a.NewTextPart("delete x.txt"))
	second := newExecCtx("b", a2a.NewTextPart("delete x.txt"))
	run(t, execute(e, first))
	run(t, execute(e, second))

	for _, execCtx := range []*a2asrv.ExecutorContext{first, second} {
		got := run(t, execute(e, followUp(execCtx, a2a.NewTextPart("once"))))
		if diff := cmp.Diff([]eventSummary{working, completed}, got); diff != "" {
			t.Fatalf("Executor.Execute(%s) wrong result (-want +got) diff = %s", execCtx.ContextID, diff)
		}
	}
	created0, _, _, _ := h.agent(0).snapshot()
	created1, _, _, _ := h.agent(1).snapshot()
	if diff := cmp.Diff(created0, created1); diff != "" {
		t.Fatalf("session IDs of agents differ (-first +second) diff = %s", diff)
	}
}

func TestExecutor_IdleTimeoutStopsAgent(t *testing.T) {
	t.Parallel()
	h := &fakeHarness{
		caps:     acp.AgentCapabilities{LoadSession: true},
		onPrompt: func(s *promptScript) { s.end(acp.StopEndTurn) },
	}
	e := newTestExecutor(t, h, Config{Pool: &PoolConfig{IdleTimeout: 10 * time.Millisecond}})
	run(t, execute(e, newExecCtx("ctx", a2a.NewTextPart("one"))))
	waitFor(t, h.agent(0).stopped)

	got := run(t, execute(e, newExecCtx("ctx", a2a.NewTextPart("two"))))

	if diff := cmp.Diff([]eventSummary{submitted, working, completed}, got); diff != "" {
		t.Fatalf("Executor.Execute() wrong result (-want +got) diff = %s", diff)
	}
	_, loaded, _, _ := h.agent(1).snapshot()
	if diff := cmp.Diff([]string{"sess-1"}, loaded); diff != "" {
		t.Fatalf("session/load wrong sessions (-want +got) diff = %s", diff)
	}
}

func TestExecutor_IdleTimeoutSparesTurnWaitingForAnswer(t *testing.T) {
	t.Parallel()
	h := &fakeHarness{onPrompt: func(s *promptScript) {
		s.askPermission(rmToolCall)
		s.end(acp.StopEndTurn)
	}}
	e := newTestExecutor(t, h, Config{Pool: &PoolConfig{IdleTimeout: 10 * time.Millisecond}})
	execCtx := newExecCtx("ctx", a2a.NewTextPart("delete x.txt"))
	run(t, execute(e, execCtx))

	time.Sleep(50 * time.Millisecond)
	got := run(t, execute(e, followUp(execCtx, a2a.NewTextPart("once"))))

	if diff := cmp.Diff([]eventSummary{working, completed}, got); diff != "" {
		t.Fatalf("Executor.Execute() wrong result (-want +got) diff = %s", diff)
	}
	if got := h.agentCount(); got != 1 {
		t.Fatalf("started agents = %d, want 1", got)
	}
}

func TestExecutor_MaxAgents(t *testing.T) {
	t.Parallel()
	asked := make(chan struct{})
	h := &fakeHarness{onPrompt: func(s *promptScript) {
		if s.promptText() == "ask" {
			defer close(asked)
			s.askPermission(rmToolCall)
		}
		s.end(acp.StopEndTurn)
	}}
	e := newTestExecutor(t, h, Config{Pool: &PoolConfig{Key: keyByContext, MaxAgents: 1}})
	run(t, execute(e, newExecCtx("a", a2a.NewTextPart("done"))))
	waitFor(t, func() bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		for _, s := range e.agents {
			if s.refs > 0 {
				return false
			}
		}
		return true
	})
	busy := newExecCtx("b", a2a.NewTextPart("ask"))

	got := run(t, execute(e, busy))

	if diff := cmp.Diff([]eventSummary{submitted, working, rmInputRequired("")}, got); diff != "" {
		t.Fatalf("Executor.Execute() wrong result (-want +got) diff = %s", diff)
	}
	waitFor(t, h.agent(0).stopped)

	got = run(t, execute(e, newExecCtx("c", a2a.NewTextPart("done"))))

	if diff := cmp.Diff([]eventSummary{submitted, failedWith("all 1 agents are busy, send the message again later")}, got); diff != "" {
		t.Fatalf("Executor.Execute() with all agents busy wrong result (-want +got) diff = %s", diff)
	}
	run(t, cancelTask(e, busy))
	<-asked
}

type countedAgentConn struct {
	io.ReadWriteCloser
	live *atomic.Int32
	once sync.Once
}

type blockedCloseConn struct {
	io.ReadWriteCloser
	started chan struct{}
	release <-chan struct{}
	once    sync.Once
}

func (c *blockedCloseConn) Close() error {
	c.once.Do(func() { close(c.started) })
	<-c.release
	return c.ReadWriteCloser.Close()
}

func TestExecutor_ConcurrentStartsDoNotWaitForUnrelatedAgentClose(t *testing.T) {
	t.Parallel()
	h := &fakeHarness{t: t, onPrompt: func(s *promptScript) { s.end(acp.StopEndTurn) }}
	closing := make(chan struct{})
	releaseClose := make(chan struct{})
	startedD := make(chan struct{})
	var releaseOnce sync.Once
	starter := AgentFunc(func(ctx context.Context, key string) (io.ReadWriteCloser, error) {
		conn, err := h.start(ctx, key)
		if err != nil {
			return nil, err
		}
		if key == "key-a" {
			return &blockedCloseConn{ReadWriteCloser: conn, started: closing, release: releaseClose}, nil
		}
		if key == "key-d" {
			close(startedD)
		}
		return conn, nil
	})
	e, err := NewExecutor(t.Context(), starter, &Config{Pool: &PoolConfig{Key: keyByContext, MaxAgents: 2}})
	if err != nil {
		t.Fatalf("NewExecutor() error = %v, want nil", err)
	}
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(releaseClose) })
		if err := e.Close(); err != nil {
			t.Errorf("Executor.Close() error = %v", err)
		}
	})
	for _, key := range []string{"a", "b"} {
		got := run(t, execute(e, newExecCtx(key, a2a.NewTextPart("done"))))
		if diff := cmp.Diff([]eventSummary{submitted, working, completed}, got); diff != "" {
			t.Fatalf("Executor.Execute() initial result (-want +got) diff = %s", diff)
		}
	}
	waitFor(t, func() bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		for _, slot := range e.agents {
			if slot.refs > 0 {
				return false
			}
		}
		return true
	})
	results := make(chan []eventSummary, 2)
	launch := func(key string) {
		go func() {
			var got []eventSummary
			for event, err := range e.Execute(t.Context(), newExecCtx(key, a2a.NewTextPart("done"))) {
				if err != nil {
					break
				}
				got = append(got, summarize(event))
			}
			results <- got
		}()
	}
	launch("c")
	select {
	case <-closing:
	case <-time.After(testTimeout):
		t.Fatal("idle agent close did not start")
	}
	launch("d")
	select {
	case <-startedD:
	case <-time.After(testTimeout):
		t.Fatal("second agent did not start while an unrelated agent close was blocked")
	}
	releaseOnce.Do(func() { close(releaseClose) })
	for range 2 {
		select {
		case got := <-results:
			if diff := cmp.Diff([]eventSummary{submitted, working, completed}, got); diff != "" {
				t.Fatalf("Executor.Execute() wrong result (-want +got) diff = %s", diff)
			}
		case <-time.After(testTimeout):
			t.Fatal("Executor.Execute() timed out")
		}
	}
}

func (c *countedAgentConn) Close() error {
	err := c.ReadWriteCloser.Close()
	c.once.Do(func() { c.live.Add(-1) })
	return err
}

func TestExecutor_ConcurrentStartsEvictDistinctIdleAgents(t *testing.T) {
	t.Parallel()
	h := &fakeHarness{t: t, onPrompt: func(s *promptScript) { s.end(acp.StopEndTurn) }}
	var live atomic.Int32
	started := make(chan string, 2)
	release := make(chan struct{})
	var releaseOnce sync.Once
	starter := AgentFunc(func(ctx context.Context, key string) (io.ReadWriteCloser, error) {
		if live.Add(1) > 2 {
			live.Add(-1)
			return nil, errors.New("process capacity exhausted")
		}
		if key == "key-c" || key == "key-d" {
			started <- key
			select {
			case <-release:
			case <-ctx.Done():
				live.Add(-1)
				return nil, ctx.Err()
			}
		}
		conn, err := h.start(ctx, key)
		if err != nil {
			live.Add(-1)
			return nil, err
		}
		return &countedAgentConn{ReadWriteCloser: conn, live: &live}, nil
	})
	e, err := NewExecutor(t.Context(), starter, &Config{Pool: &PoolConfig{Key: keyByContext, MaxAgents: 2}})
	if err != nil {
		t.Fatalf("NewExecutor() error = %v, want nil", err)
	}
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(release) })
		if err := e.Close(); err != nil {
			t.Errorf("Executor.Close() error = %v", err)
		}
	})
	for _, key := range []string{"a", "b"} {
		got := run(t, execute(e, newExecCtx(key, a2a.NewTextPart("done"))))
		if diff := cmp.Diff([]eventSummary{submitted, working, completed}, got); diff != "" {
			t.Fatalf("Executor.Execute() initial result (-want +got) diff = %s", diff)
		}
	}
	waitFor(t, func() bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		for _, slot := range e.agents {
			if slot.refs > 0 {
				return false
			}
		}
		return true
	})
	type result struct {
		events []eventSummary
		err    error
	}
	results := make(chan result, 2)
	launch := func(key string) {
		go func() {
			var got result
			for event, err := range e.Execute(t.Context(), newExecCtx(key, a2a.NewTextPart("done"))) {
				if err != nil {
					got.err = err
					break
				}
				got.events = append(got.events, summarize(event))
			}
			results <- got
		}()
	}
	launch("c")
	select {
	case <-started:
	case <-time.After(testTimeout):
		t.Fatal("first agent did not start")
	}
	launch("d")
	select {
	case <-started:
	case got := <-results:
		t.Fatalf("Executor.Execute() completed before the second agent started: %+v", got)
	case <-time.After(testTimeout):
		t.Fatal("second agent did not start")
	}
	releaseOnce.Do(func() { close(release) })
	for range 2 {
		select {
		case got := <-results:
			if got.err != nil {
				t.Fatalf("Executor.Execute() error = %v, want nil", got.err)
			}
			if diff := cmp.Diff([]eventSummary{submitted, working, completed}, got.events); diff != "" {
				t.Fatalf("Executor.Execute() wrong result (-want +got) diff = %s", diff)
			}
		case <-time.After(testTimeout):
			t.Fatal("Executor.Execute() timed out")
		}
	}
}

func TestExecutor_StartingAgentConsumesCapacityUntilFailure(t *testing.T) {
	t.Parallel()
	h := &fakeHarness{t: t, onPrompt: func(s *promptScript) { s.end(acp.StopEndTurn) }}
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	starter := AgentFunc(func(ctx context.Context, key string) (io.ReadWriteCloser, error) {
		if key == "key-a" {
			close(started)
			select {
			case <-release:
				return nil, errors.New("agent start failed")
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return h.start(ctx, key)
	})
	e, err := NewExecutor(t.Context(), starter, &Config{Pool: &PoolConfig{Key: keyByContext, MaxAgents: 1}})
	if err != nil {
		t.Fatalf("NewExecutor() error = %v, want nil", err)
	}
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(release) })
		if err := e.Close(); err != nil {
			t.Errorf("Executor.Close() error = %v", err)
		}
	})
	firstDone := make(chan []eventSummary, 1)
	go func() {
		var got []eventSummary
		for event, err := range e.Execute(t.Context(), newExecCtx("a", a2a.NewTextPart("first"))) {
			if err != nil {
				break
			}
			got = append(got, summarize(event))
		}
		firstDone <- got
	}()
	select {
	case <-started:
	case <-time.After(testTimeout):
		t.Fatal("first agent did not start")
	}
	got := run(t, execute(e, newExecCtx("b", a2a.NewTextPart("busy"))))
	if diff := cmp.Diff([]eventSummary{submitted, failedWith("all 1 agents are busy, send the message again later")}, got); diff != "" {
		t.Fatalf("Executor.Execute() while starting wrong result (-want +got) diff = %s", diff)
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case got = <-firstDone:
		if diff := cmp.Diff([]eventSummary{submitted, failedWith("starting acp agent: agent start failed")}, got); diff != "" {
			t.Fatalf("Executor.Execute() failed start wrong result (-want +got) diff = %s", diff)
		}
	case <-time.After(testTimeout):
		t.Fatal("first execution did not finish")
	}
	got = run(t, execute(e, newExecCtx("b", a2a.NewTextPart("after failure"))))
	if diff := cmp.Diff([]eventSummary{submitted, working, completed}, got); diff != "" {
		t.Fatalf("Executor.Execute() after failed start wrong result (-want +got) diff = %s", diff)
	}
}

func TestExecutor_NotOwnerDoesNotDelayStart(t *testing.T) {
	t.Parallel()
	h := &fakeHarness{onPrompt: func(s *promptScript) { s.end(acp.StopEndTurn) }}
	e := newTestExecutor(t, h, Config{Pool: &PoolConfig{Key: keyByContext}})
	var starts atomic.Int32
	e.agentStarter = AgentFunc(func(ctx context.Context, key string) (io.ReadWriteCloser, error) {
		if starts.Add(1) == 1 {
			return nil, fmt.Errorf("%w: node-2", ErrNotOwner)
		}
		return h.start(ctx, key)
	})

	first := run(t, execute(e, newExecCtx("ctx", a2a.NewTextPart("one"))))
	second := run(t, execute(e, newExecCtx("ctx", a2a.NewTextPart("two"))))

	if diff := cmp.Diff([]eventSummary{submitted, failedWith("starting acp agent: the agent is owned by another node: node-2")}, first); diff != "" {
		t.Fatalf("Executor.Execute() wrong result (-want +got) diff = %s", diff)
	}
	if diff := cmp.Diff([]eventSummary{submitted, working, completed}, second); diff != "" {
		t.Fatalf("Executor.Execute() after ErrNotOwner wrong result (-want +got) diff = %s", diff)
	}
}

func TestExecutor_ShutdownWaitsForTasks(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	h := &fakeHarness{onPrompt: func(s *promptScript) {
		<-release
		s.end(acp.StopEndTurn)
	}}
	e := newTestExecutor(t, h, Config{})
	firstResult := make(chan []eventSummary, 1)
	go func() { firstResult <- run(t, execute(e, newExecCtx("a", a2a.NewTextPart("first")))) }()
	waitFor(t, func() bool { _, _, _, p := h.agent(0).snapshot(); return len(p) == 1 })
	drained := make(chan error, 1)
	go func() { drained <- e.Shutdown(t.Context()) }()
	waitFor(t, func() bool { e.mu.Lock(); defer e.mu.Unlock(); return e.draining })

	got := run(t, execute(e, newExecCtx("b", a2a.NewTextPart("second"))))

	if diff := cmp.Diff([]eventSummary{submitted, failedWith(errDraining.Error())}, got); diff != "" {
		t.Fatalf("Executor.Execute() while draining wrong result (-want +got) diff = %s", diff)
	}
	close(release)
	if err := <-drained; err != nil {
		t.Fatalf("Executor.Shutdown() error = %v, want nil", err)
	}
	if diff := cmp.Diff([]eventSummary{submitted, working, completed}, <-firstResult); diff != "" {
		t.Fatalf("Executor.Execute() first turn wrong result (-want +got) diff = %s", diff)
	}
	waitFor(t, h.agent(0).stopped)
}

func TestExecutor_ShutdownTimeoutCancelsTasks(t *testing.T) {
	t.Parallel()
	outcomes := make(chan acp.PermissionOutcome, 1)
	h := &fakeHarness{onPrompt: func(s *promptScript) { outcomes <- s.askPermission(rmToolCall) }}
	e := newTestExecutor(t, h, Config{})
	run(t, execute(e, newExecCtx("ctx", a2a.NewTextPart("delete x.txt"))))
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()

	err := e.Shutdown(ctx)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Executor.Shutdown() error = %v, want %v", err, context.DeadlineExceeded)
	}
	if diff := cmp.Diff(acp.PermissionOutcome{Outcome: acp.OutcomeCancelled}, <-outcomes); diff != "" {
		t.Fatalf("permission outcome wrong result (-want +got) diff = %s", diff)
	}
	waitFor(t, h.agent(0).stopped)
}
