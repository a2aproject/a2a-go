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
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/internal/acp"
	"github.com/google/go-cmp/cmp"
)

func TestExecutor_StartBackoff(t *testing.T) {
	t.Parallel()
	var starts atomic.Int32
	h := &fakeHarness{onPrompt: func(s *promptScript) { s.end(acp.StopEndTurn) }}
	e := newTestExecutor(t, h, Config{})
	e.start = func(context.Context) (io.ReadWriteCloser, error) {
		starts.Add(1)
		return nil, errors.New("no sandbox")
	}
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

	_, err := NewExecutor(t.Context(), Config{Dir: t.TempDir(), Start: h.start, StartTimeout: 10 * time.Millisecond})

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
