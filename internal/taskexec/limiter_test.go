// Copyright 2025 The A2A Authors
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

package taskexec

import (
	"fmt"
	"testing"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv/limiter"
)

func TestManager_ExecuteRateLimit(t *testing.T) {
	type executionEvent struct {
		scope   string // passed to limiter.WithScope(ctx)
		end     bool   // if true an execution ends in the provided scope
		wantErr bool   // true means the execution start is expected to fail
	}
	testCases := []struct {
		name   string
		config limiter.ConcurrencyConfig
		events []executionEvent
	}{
		{
			name: "no limit",
			events: []executionEvent{
				{scope: "u-1"},
				{scope: "u-1"},
				{scope: "u-2"},
				{scope: "u-3"},
			},
		},
		{
			name:   "global single execution limit",
			config: limiter.ConcurrencyConfig{MaxExecutions: 1},
			events: []executionEvent{
				{scope: "u-1"},
				{scope: "u-2", wantErr: true},
				{scope: "u-1", end: true},
				{scope: "u-2"},
			},
		},
		{
			name:   "scoped single execution limit",
			config: limiter.ConcurrencyConfig{GetMaxExecutions: func(scope string) int { return 1 }},
			events: []executionEvent{
				{scope: "u-1"},
				{scope: "u-2"},
				{scope: "u-2", wantErr: true},
				{scope: "u-2", end: true},
				{scope: "u-2"},
			},
		},
		{
			name:   "global multi execution limit",
			config: limiter.ConcurrencyConfig{MaxExecutions: 2},
			events: []executionEvent{
				{scope: "u-1"},
				{scope: "u-2"},
				{scope: "u-1", wantErr: true},
				{scope: "u-2", end: true},
				{scope: "u-1"},
			},
		},
		{
			name:   "scoped multi execution limit",
			config: limiter.ConcurrencyConfig{GetMaxExecutions: func(scope string) int { return 2 }},
			events: []executionEvent{
				{scope: "u-1"},
				{scope: "u-1"},
				{scope: "u-1", wantErr: true},
				{scope: "u-1", end: true},
				{scope: "u-1"},
			},
		},
		{
			name: "different scope limits",
			config: limiter.ConcurrencyConfig{GetMaxExecutions: func(scope string) int {
				if scope == "u-1" {
					return 2
				} else {
					return 1
				}
			}},
			events: []executionEvent{
				{scope: "u-1"},
				{scope: "u-1"},
				{scope: "u-1", wantErr: true},
				{scope: "u-2"},
				{scope: "u-2", wantErr: true},
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, tid := t.Context(), a2a.NewTaskID()
			manager := NewManager(Config{ConcurrencyConfig: tc.config})

			type runningExec struct {
				proceed  chan struct{} // execution blocks on channel until it is time to end
				err      chan error    // execution writes manager.Execute() error or closes the channel
				finished chan struct{} // execution closes the channel after finishing
			}
			executions := map[string][]runningExec{}

			for i, ev := range tc.events {
				if ev.end {
					running, ok := executions[ev.scope]
					if !ok || len(running) == 0 {
						t.Fatalf("no execution to end in scope %q at event %d", ev.scope, i)
					}
					toEnd, rest := running[0], running[1:]
					executions[ev.scope] = rest
					close(toEnd.proceed)
					<-toEnd.finished
					continue
				}

				exec := runningExec{
					proceed:  make(chan struct{}),
					err:      make(chan error),
					finished: make(chan struct{}),
				}
				go func() {
					scopedCtx := limiter.WithScope(ctx, ev.scope)
					executor := newExecutor()
					executor.nextEventTerminal = true
					execution, subscription, err := manager.Execute(scopedCtx, a2a.TaskID(fmt.Sprintf("task-%d", i)), executor)
					subscription.cancel()
					if err != nil {
						exec.err <- err
						return
					}
					close(exec.err)
					<-exec.proceed
					<-executor.executeCalled
					executor.mustWrite(t, &a2a.Task{ID: tid})
					if _, err := execution.Result(scopedCtx); err != nil {
						t.Errorf("execution.Result() error = %v", err)
					}
					close(exec.finished)
				}()
				err, closed := <-exec.err
				gotErr := err != nil && closed
				if gotErr != ev.wantErr {
					t.Fatalf("manager.Execute() error = %v at %d, want error %v", err, i, ev.wantErr)
				}
				if !ev.wantErr {
					executions[ev.scope] = append(executions[ev.scope], exec)
				}
			}
			for _, v := range executions {
				for _, exec := range v {
					close(exec.proceed)
					<-exec.finished
				}
			}
		})
	}
}
