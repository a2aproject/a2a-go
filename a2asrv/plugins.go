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

package a2asrv

import (
	"context"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aplugin/core"
	"github.com/a2aproject/a2a-go/v2/internal/taskupdate"
	"github.com/a2aproject/a2a-go/v2/log"
)

// ServerPlugin is invoked around every call processed by a [RequestHandler].
// A non-nil error from [ServerPlugin.BeforeHandlerRun] rejects the call.
// See [github.com/a2aproject/a2a-go/v2/a2aplugin] for composing and registering plugins.
type ServerPlugin interface {
	core.Plugin

	// BeforeHandlerRun is called before the handler processes a request. The
	// returned context is passed to the handler and to AfterHandlerRun.
	BeforeHandlerRun(ctx context.Context, req *ServerRequest) (context.Context, error)

	// AfterHandlerRun is called with the handler result. For streaming calls it
	// runs once per emitted event with IsStreamEvent=true, IsFinal=false, and
	// once more with IsFinal=true after the stream ends.
	AfterHandlerRun(ctx context.Context, resp *ServerResponse) (context.Context, error)
}

// ServerRequest is passed to [ServerPlugin.BeforeHandlerRun].
type ServerRequest struct {
	// Method is the A2A RPC name of the invoked handler method, e.g. "SendMessage".
	Method string
	// Payload is the request value. It is nil for methods without parameters,
	// otherwise one of a2a package core types.
	Payload any
	// CallContext holds the server-side request metadata.
	CallContext *CallContext
	// AgentCard is the agent card served by this handler, if available.
	AgentCard *a2a.AgentCard
}

// ServerResponse is passed to [ServerPlugin.AfterHandlerRun].
type ServerResponse struct {
	// Method is the A2A RPC name, matching [ServerRequest.Method].
	Method string
	// Payload is the handler result or stream event. It is nil when Err is non-nil
	// or on the final call that marks the end of a stream.
	Payload any
	// CallContext is the context passed in [ServerRequest.CallContext].
	CallContext *CallContext
	// Err is the error returned by the handler, if any.
	Err error
	// IsStreamEvent is true when Payload is a single event of a streaming response.
	IsStreamEvent bool
	// IsFinal is true for the last call of a request. For unary calls it is
	// always true; for streaming calls it is true only for the trailing invocation.
	IsFinal bool
}

// ExecutionPlugin is invoked around every [AgentExecutor.Execute] call. Executions run
// outside the request scope, so in cluster mode the hooks run on the node executing the agent.
// A non-nil error from [ExecutionPlugin.BeforeExecution] fails the execution without running the agent.
type ExecutionPlugin interface {
	core.Plugin

	// BeforeExecution is called before the agent runs. The returned context is passed
	// to the agent and to the other execution hooks.
	BeforeExecution(ctx context.Context, execCtx *ExecutorContext) (context.Context, error)

	// OnExecutionEvent is called for every event emitted by the agent, before it is processed.
	// It runs on the agent's path, so it should be cheap.
	OnExecutionEvent(ctx context.Context, execCtx *ExecutorContext, event a2a.Event) error

	// AfterExecution is called after the agent finishes, with the error that ended the execution, if any.
	AfterExecution(ctx context.Context, execCtx *ExecutorContext, cause error) error
}

// TaskObserverPlugin is notified after every successful task store write: task creation,
// every applied agent event, user messages appended to an existing task and cancelation.
type TaskObserverPlugin interface {
	core.Plugin

	// OnTaskEvent is called after a successful task write. Returned errors are logged.
	OnTaskEvent(ctx context.Context, info *TaskEventInfo) error
}

// TaskEventInfo is passed to [TaskObserverPlugin.OnTaskEvent].
type TaskEventInfo struct {
	// TaskID is the ID of the written task.
	TaskID a2a.TaskID
	// FromState is the task state before the write, or [a2a.TaskStateUnspecified] if the write created the task.
	FromState a2a.TaskState
	// ToState is the task state after the write. It equals FromState for writes which
	// don't change the state, such as artifact updates.
	ToState a2a.TaskState
	// Event is the event which caused the write.
	Event a2a.Event
	// Task is the task state passed to the store. It must not be modified. With a custom
	// materializer set using [WithUpdateMaterializer] it might contain only a part of the task.
	Task *a2a.Task
	// CallContext is the context of the request which caused the write, if available.
	CallContext *CallContext
}

// WithPlugins sets the plugin whose hooks run around every call and agent execution.
// The handler uses the hook interfaces the plugin implements: [ServerPlugin], [ExecutionPlugin]
// and [TaskObserverPlugin].
// If omitted, the handler uses the global plugin registered with
// [github.com/a2aproject/a2a-go/v2/a2aplugin.SetGlobal] at construction time.
// Passing nil is a no-op.
func WithPlugins(plugin core.Plugin) RequestHandlerOption {
	return func(ih *InterceptedHandler, h *defaultRequestHandler) {
		if plugin == nil {
			return
		}
		ih.Plugins, _ = plugin.(ServerPlugin)
		h.executionPlugin, _ = plugin.(ExecutionPlugin)
		h.taskObserver, _ = plugin.(TaskObserverPlugin)
	}
}

// taskHooksReporter is implemented by plugin managers which can report
// that none of the registered plugins implement task observer hooks.
type taskHooksReporter interface {
	HasTaskHooks() bool
}

// observeTaskWrites reports the writes made by mgr to the task observer plugin, if one is configured.
func (f *factory) observeTaskWrites(mgr *taskupdate.Manager) {
	if f.taskObserver != nil {
		mgr.OnWrite = f.reportTaskWrite
	}
}

// reportTaskWrite calls [TaskObserverPlugin.OnTaskEvent], if a plugin is configured, and logs the error it returns.
func (f *factory) reportTaskWrite(ctx context.Context, w taskupdate.Write) {
	if f.taskObserver == nil {
		return
	}
	info := &TaskEventInfo{TaskID: w.TaskID, FromState: w.PrevState, ToState: w.State, Event: w.Event, Task: w.Task}
	info.CallContext, _ = CallContextFrom(ctx)
	if err := f.taskObserver.OnTaskEvent(ctx, info); err != nil {
		log.Error(ctx, "plugin OnTaskEvent failed", err, "plugin", f.taskObserver.Name())
	}
}

// executionHooksReporter is implemented by plugin managers which can report
// that none of the registered plugins implement execution hooks.
type executionHooksReporter interface {
	HasExecutionHooks() bool
}

// onExecutionEvent calls [ExecutionPlugin.OnExecutionEvent] and logs the error it returns.
func onExecutionEvent(ctx context.Context, p ExecutionPlugin, execCtx *ExecutorContext, event a2a.Event) {
	if err := p.OnExecutionEvent(ctx, execCtx, event); err != nil {
		log.Error(ctx, "plugin OnExecutionEvent failed", err, "plugin", p.Name())
	}
}

// afterExecution calls [ExecutionPlugin.AfterExecution] and logs the error it returns.
func afterExecution(ctx context.Context, p ExecutionPlugin, execCtx *ExecutorContext, cause error) {
	if err := p.AfterExecution(ctx, execCtx, cause); err != nil {
		log.Error(ctx, "plugin AfterExecution failed", err, "plugin", p.Name())
	}
}

// serverHooksReporter is implemented by plugin managers which can report
// that none of the registered plugins implement server hooks.
type serverHooksReporter interface {
	HasServerHooks() bool
}

// pluginCall carries the per-call plugin state across the Before and After
// (or stream-end) hooks of a single request.
type pluginCall struct {
	active    bool
	plugin    ServerPlugin
	ctx       context.Context
	req       *ServerRequest
	streamErr error
	finaled   bool
}

// beginPluginCall runs [ServerPlugin.BeforeHandlerRun] and returns a handle used
// to emit per-event and final AfterHandlerRun calls, together with the context to
// continue the call with. A non-nil error means the call must be rejected without
// further plugin dispatch. If the handler has no plugin hooks the returned handle
// is inactive and ctx is returned unchanged.
func beginPluginCall(ctx context.Context, h *InterceptedHandler, callCtx *CallContext, payload any) (*pluginCall, context.Context, error) {
	if !core.HasHooks(h.Plugins, serverHooksReporter.HasServerHooks) {
		return &pluginCall{}, ctx, nil
	}
	req := &ServerRequest{
		Method:      callCtx.Method(),
		Payload:     payload,
		CallContext: callCtx,
	}
	newCtx, err := h.Plugins.BeforeHandlerRun(ctx, req)
	if err != nil {
		return &pluginCall{}, ctx, err
	}
	return &pluginCall{active: true, plugin: h.Plugins, ctx: newCtx, req: req}, newCtx, nil
}

// finish fires the final [ServerPlugin.AfterHandlerRun] call at most once.
func (p *pluginCall) finish(payload any, err error) {
	if !p.active || p.finaled {
		return
	}
	p.finaled = true
	resp := &ServerResponse{
		Method:      p.req.Method,
		CallContext: p.req.CallContext,
		Err:         err,
		IsFinal:     true,
	}
	if err == nil {
		resp.Payload = payload
	}
	p.after(resp)
}

// finishStream fires the final [ServerPlugin.AfterHandlerRun] call of a stream with
// the last error yielded through the function returned by wrapStreamYield.
func (p *pluginCall) finishStream() {
	p.finish(nil, p.streamErr)
}

// wrapStreamYield returns a yield function that fires a per-event
// [ServerPlugin.AfterHandlerRun] call before each yield, recording any yielded
// error for finishStream.
func (p *pluginCall) wrapStreamYield(yield func(a2a.Event, error) bool) func(a2a.Event, error) bool {
	if !p.active {
		return yield
	}
	return func(ev a2a.Event, err error) bool {
		resp := &ServerResponse{
			Method:        p.req.Method,
			CallContext:   p.req.CallContext,
			Err:           err,
			IsStreamEvent: true,
		}
		if err != nil {
			p.streamErr = err
		} else {
			resp.Payload = ev
		}
		p.after(resp)
		return yield(ev, err)
	}
}

func (p *pluginCall) after(resp *ServerResponse) {
	if _, err := p.plugin.AfterHandlerRun(p.ctx, resp); err != nil {
		log.Error(p.ctx, "plugin AfterHandlerRun failed", err, "plugin", p.plugin.Name())
	}
}
