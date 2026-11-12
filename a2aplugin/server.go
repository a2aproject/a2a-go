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

package a2aplugin

import (
	"context"
	"iter"
	"sync"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

// ServerHooks are the hooks of a server plugin. All fields are optional.
type ServerHooks struct {
	// BeforeExecute runs before the agent executes. The returned context is passed to the agent,
	// OnExecuteEvent and AfterExecute. If it returns an error, the agent is not executed.
	BeforeExecute func(ctx context.Context, execCtx *a2asrv.ExecutorContext) (context.Context, error)

	// OnExecuteEvent runs on every event emitted by the agent and returns the event to emit instead.
	// If it returns an error, the execution stops with the error.
	OnExecuteEvent func(ctx context.Context, execCtx *a2asrv.ExecutorContext, event a2a.Event) (a2a.Event, error)

	// AfterExecute runs after the execution finishes with its result or error. Its context carries
	// the values of the context returned by BeforeExecute but is not canceled when the execution ends.
	AfterExecute func(ctx context.Context, execCtx *a2asrv.ExecutorContext, result a2a.SendMessageResult, err error)

	// WrapExecutor wraps the agent executor outside the execution hooks.
	WrapExecutor func(next a2asrv.AgentExecutor) a2asrv.AgentExecutor
}

// Server returns an [a2asrv.Plugin] which runs the hooks. Execution hooks run for agent executions,
// not for cancelations.
func Server(name string, hooks ServerHooks) a2asrv.Plugin {
	return &serverPlugin{name: name, hooks: hooks}
}

type serverPlugin struct {
	a2asrv.PassthroughPlugin
	name  string
	hooks ServerHooks
}

func (p *serverPlugin) Name() string {
	return p.name
}

func (p *serverPlugin) WrapExecutor(next a2asrv.AgentExecutor) a2asrv.AgentExecutor {
	if p.hooks.AfterExecute != nil {
		next = &cleaningExecutor{hookExecutor: &hookExecutor{AgentExecutor: next, hooks: p.hooks, contexts: &sync.Map{}}}
	} else if p.hooks.BeforeExecute != nil || p.hooks.OnExecuteEvent != nil {
		next = &hookExecutor{AgentExecutor: next, hooks: p.hooks}
	}
	if p.hooks.WrapExecutor != nil {
		next = p.hooks.WrapExecutor(next)
	}
	return next
}

type hookExecutor struct {
	a2asrv.AgentExecutor
	hooks ServerHooks
	// contexts maps the ExecutorContext of every started execution to the context passed to AfterExecute.
	// It is nil if AfterExecute is not set.
	contexts *sync.Map
}

func (e *hookExecutor) Execute(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		ctx := ctx
		if e.hooks.BeforeExecute != nil {
			hookCtx, err := e.hooks.BeforeExecute(ctx, execCtx)
			if err != nil {
				e.started(ctx, execCtx)
				yield(nil, err)
				return
			}
			ctx = hookCtx
		}
		e.started(ctx, execCtx)
		for event, err := range e.AgentExecutor.Execute(ctx, execCtx) {
			if err == nil && e.hooks.OnExecuteEvent != nil {
				event, err = e.hooks.OnExecuteEvent(ctx, execCtx, event)
			}
			if err != nil {
				yield(nil, err)
				return
			}
			if !yield(event, nil) {
				return
			}
		}
	}
}

func (e *hookExecutor) started(ctx context.Context, execCtx *a2asrv.ExecutorContext) {
	if e.contexts != nil {
		e.contexts.Store(execCtx, ctx)
	}
}

// cleaningExecutor runs AfterExecute from Cleanup, which the handler calls after every execution and cancelation.
type cleaningExecutor struct {
	*hookExecutor
}

func (e *cleaningExecutor) Cleanup(_ context.Context, execCtx *a2asrv.ExecutorContext, result a2a.SendMessageResult, err error) {
	ctx, ok := e.contexts.LoadAndDelete(execCtx)
	if !ok {
		// Execute was not called: the cleanup follows a cancelation, or an ExecutorContextInterceptor failed.
		return
	}
	e.hooks.AfterExecute(context.WithoutCancel(ctx.(context.Context)), execCtx, result, err)
}
