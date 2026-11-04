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

// Package main demonstrates how to install a2aplugin plugins on a single A2A
// [a2asrv.RequestHandler] (as opposed to the process-wide default used by the
// client example).
//
// Both plugins are bundled into one [a2aplugin.Sequential] manager and
// attached to the handler with [a2asrv.WithPlugins]. The handler dispatches
// every hook through that manager without touching [a2aplugin.SetGlobal], so
// two handlers in the same process can carry different plugin stacks.
//
//   - metricsPlugin implements [a2asrv.ServerPlugin]. It counts handler calls
//     per method, times every call and correlates it with the client's
//     X-Request-ID service parameter.
//   - lifecyclePlugin embeds [a2aplugin.Passthrough] and overrides the
//     [a2asrv.ExecutionPlugin] and [a2asrv.TaskObserverPlugin] hooks to log
//     the agent execution lifecycle and the resulting task state transitions.
//
// Note that [a2asrv.WithPlugins] *replaces* every hook-type slot that
// [a2asrv.NewHandler] would otherwise populate from [a2aplugin.Global]: a
// plugin that doesn't implement a given hook interface nulls that slot. If
// you want both a global default *and* an extra per-handler plugin, compose
// them together into one Sequential manager and pass the result to
// [a2asrv.WithPlugins], the way this example composes metrics and lifecycle.
//
// Run with: go run .
package main

import (
	"context"
	"flag"
	"fmt"
	"iter"
	"log"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aplugin"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

var port = flag.Int("port", 9001, "Port for the A2A server to listen on.")

const requestIDHeader = "X-Request-ID"

func main() {
	flag.Parse()

	// Bundle the plugins into a single Sequential manager and attach it to
	// this handler only. The manager implements every hook interface
	// (ServerPlugin, ExecutionPlugin, TaskObserverPlugin, ...), so a2asrv
	// routes each hook to the registered plugins that implement it. Nothing
	// is installed with a2aplugin.SetGlobal, so a second handler built in
	// the same process would run with no plugins (or its own set).
	plugins := a2aplugin.Sequential(
		newMetricsPlugin(),
		&lifecyclePlugin{},
	)

	agent := a2asrv.AgentExecutorFunc(func(ctx context.Context, ec *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
		return func(yield func(a2a.Event, error) bool) {
			if !yield(a2a.NewSubmittedTask(ec, ec.Message), nil) {
				return
			}
			if !yield(a2a.NewStatusUpdateEvent(ec, a2a.TaskStateWorking, nil), nil) {
				return
			}
			reply := a2a.NewMessage(a2a.MessageRoleAgent,
				a2a.NewTextPart("Hello from the plugin-enabled agent!"))
			yield(a2a.NewStatusUpdateEvent(ec, a2a.TaskStateCompleted, reply), nil)
		}
	})

	handler := a2asrv.NewHandler(agent, a2asrv.WithPlugins(plugins))

	addr := fmt.Sprintf("http://127.0.0.1:%d/invoke", *port)
	card := &a2a.AgentCard{
		Name:        "Plugin Demo Agent",
		Description: "Agent used to showcase a2aplugin hooks",
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(addr, a2a.TransportProtocolJSONRPC),
		},
		DefaultInputModes:  []string{"text/plain"},
		DefaultOutputModes: []string{"text/plain"},
		Capabilities:       a2a.AgentCapabilities{Streaming: true},
		Skills: []a2a.AgentSkill{{
			ID:          "plugin_demo",
			Name:        "Plugin demo",
			Description: "Returns a canned greeting while plugins observe the call.",
			Tags:        []string{"demo"},
			Examples:    []string{"hi"},
		}},
	}

	mux := http.NewServeMux()
	mux.Handle("/invoke", a2asrv.NewJSONRPCHandler(handler))
	mux.Handle(a2asrv.WellKnownAgentCardPath, a2asrv.NewStaticAgentCardHandler(card))

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("Failed to bind to a port: %v", err)
	}
	log.Printf("Plugin demo server listening on 127.0.0.1:%d", *port)
	if err := http.Serve(listener, mux); err != nil {
		log.Printf("Server stopped: %v", err)
	}
}

// metricsPlugin counts handler calls per RPC method and times every call.
// It demonstrates the [a2asrv.ServerPlugin] hook pair: the Before hook
// starts a timer and the After hook (invoked once with IsFinal=true) reports
// the elapsed time and the outcome.
type metricsPlugin struct {
	mu     sync.Mutex
	counts map[string]int
}

var _ a2asrv.ServerPlugin = (*metricsPlugin)(nil)

func newMetricsPlugin() *metricsPlugin {
	return &metricsPlugin{counts: map[string]int{}}
}

// Name implements [a2aplugin.Plugin].
func (*metricsPlugin) Name() string { return "metrics" }

// startKey is a context key used to pass the start time from BeforeHandlerRun
// to AfterHandlerRun. Using an unexported struct type avoids collisions with
// other context values.
type startKey struct{}

// BeforeHandlerRun implements [a2asrv.ServerPlugin].
func (p *metricsPlugin) BeforeHandlerRun(ctx context.Context, req *a2asrv.ServerRequest) (context.Context, error) {
	p.mu.Lock()
	p.counts[req.Method]++
	perMethod := p.counts[req.Method]
	p.mu.Unlock()

	log.Printf("[metrics] -> %s request_id=%q calls=%d", req.Method, requestIDOf(req.CallContext), perMethod)
	return context.WithValue(ctx, startKey{}, time.Now()), nil
}

// AfterHandlerRun implements [a2asrv.ServerPlugin]. Streaming events are
// ignored here; only the final after call reports the outcome.
func (p *metricsPlugin) AfterHandlerRun(ctx context.Context, resp *a2asrv.ServerResponse) (context.Context, error) {
	if !resp.IsFinal {
		return ctx, nil
	}
	start, _ := ctx.Value(startKey{}).(time.Time)
	elapsed := time.Since(start)
	outcome := "ok"
	if resp.Err != nil {
		outcome = "error=" + resp.Err.Error()
	}
	log.Printf("[metrics] <- %s request_id=%q elapsed=%s %s",
		resp.Method, requestIDOf(resp.CallContext), elapsed, outcome)
	return ctx, nil
}

// requestIDOf returns the first X-Request-ID service parameter attached to
// the call, or "" if none was sent.
func requestIDOf(cc *a2asrv.CallContext) string {
	if cc == nil {
		return ""
	}
	values, ok := cc.ServiceParams().Get(requestIDHeader)
	if !ok || len(values) == 0 {
		return ""
	}
	return values[0]
}

// lifecyclePlugin observes agent executions and task writes. By embedding
// [a2aplugin.Passthrough] it only has to override the hooks it cares about;
// every other hook (ServerPlugin, ClientPlugin, push.Plugin) falls through to
// a no-op implementation.
type lifecyclePlugin struct {
	a2aplugin.Passthrough
	events atomic.Int64
}

// Compile-time check that lifecyclePlugin implements the two hook interfaces
// used by this example.
var (
	_ a2asrv.ExecutionPlugin    = (*lifecyclePlugin)(nil)
	_ a2asrv.TaskObserverPlugin = (*lifecyclePlugin)(nil)
)

// Name implements [a2aplugin.Plugin].
func (*lifecyclePlugin) Name() string { return "lifecycle" }

// BeforeExecution implements [a2asrv.ExecutionPlugin].
func (p *lifecyclePlugin) BeforeExecution(ctx context.Context, ec *a2asrv.ExecutorContext) (context.Context, error) {
	log.Printf("[lifecycle] agent starting task=%s context=%s", ec.TaskID, ec.ContextID)
	return ctx, nil
}

// OnExecutionEvent implements [a2asrv.ExecutionPlugin]. It runs on the
// agent's path, so it only increments a counter and emits a cheap log line.
func (p *lifecyclePlugin) OnExecutionEvent(_ context.Context, ec *a2asrv.ExecutorContext, event a2a.Event) error {
	n := p.events.Add(1)
	log.Printf("[lifecycle] execution event #%d task=%s type=%T", n, ec.TaskID, event)
	return nil
}

// AfterExecution implements [a2asrv.ExecutionPlugin].
func (p *lifecyclePlugin) AfterExecution(_ context.Context, ec *a2asrv.ExecutorContext, cause error) error {
	log.Printf("[lifecycle] agent finished task=%s cause=%v", ec.TaskID, cause)
	return nil
}

// OnTaskEvent implements [a2asrv.TaskObserverPlugin]. It fires after every
// successful write to the task store, which includes task creation, user
// messages, and status/artifact updates.
func (p *lifecyclePlugin) OnTaskEvent(_ context.Context, info *a2asrv.TaskEventInfo) error {
	log.Printf("[lifecycle] task %s state %s -> %s (%T)",
		info.TaskID, info.FromState, info.ToState, info.Event)
	return nil
}
