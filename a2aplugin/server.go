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
	"github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"
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

	// BeforeTaskWrite runs before every Create or Update call on the task store. The returned
	// context is passed to the store and to AfterTaskWrite. If it returns an error, the write
	// is not performed and the error is returned from the store call.
	BeforeTaskWrite func(ctx context.Context, w *TaskWrite) (context.Context, error)

	// AfterTaskWrite runs after every Create or Update call on the task store with the error
	// returned by the store (nil on success, or the error returned by BeforeTaskWrite when the
	// write was skipped).
	AfterTaskWrite func(ctx context.Context, w *TaskWrite, err error)

	// WrapTaskStore wraps the task store outside the task store hooks.
	WrapTaskStore func(next taskstore.Store) taskstore.Store
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
	var contexts *sync.Map
	if p.hooks.AfterExecute != nil {
		contexts = &sync.Map{}
	}
	if p.hooks.BeforeExecute != nil || p.hooks.OnExecuteEvent != nil || p.hooks.AfterExecute != nil {
		next = &hookExecutor{AgentExecutor: next, hooks: p.hooks, contexts: contexts}
	}
	if p.hooks.WrapExecutor != nil {
		next = p.hooks.WrapExecutor(next)
	}
	if p.hooks.AfterExecute != nil {
		next = &cleaningExecutor{AgentExecutor: next, hooks: p.hooks, contexts: contexts}
	}
	return next
}

type hookExecutor struct {
	a2asrv.AgentExecutor
	hooks    ServerHooks
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
// It is the outermost wrapper, so the handler finds its Cleanup even if WrapExecutor is set.
type cleaningExecutor struct {
	a2asrv.AgentExecutor
	hooks    ServerHooks
	contexts *sync.Map
}

func (e *cleaningExecutor) Cleanup(ctx context.Context, execCtx *a2asrv.ExecutorContext, result a2a.SendMessageResult, err error) {
	// A missing context means Execute was not called: the cleanup follows a cancelation, or an
	// ExecutorContextInterceptor failed.
	if hookCtx, ok := e.contexts.LoadAndDelete(execCtx); ok {
		e.hooks.AfterExecute(context.WithoutCancel(hookCtx.(context.Context)), execCtx, result, err)
	}
	if cleaner, ok := e.AgentExecutor.(a2asrv.AgentExecutionCleaner); ok {
		cleaner.Cleanup(ctx, execCtx, result, err)
	}
}
