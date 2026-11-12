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

// Package main demonstrates server plugins.
//
// It installs two plugins:
//   - audit, built with [a2aplugin.Server] from hooks. It logs every call with the experiment ID set by
//     the client plugin, and every task state change.
//   - timing, a plugin which implements [a2asrv.Plugin] directly by embedding [a2asrv.PassthroughPlugin].
//     It wraps every call and logs its duration, including streams.
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
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aplugin"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

var port = flag.Int("port", 9001, "Port for the server to listen on.")

func newAudit() a2asrv.Plugin {
	return a2aplugin.Server("audit", a2aplugin.ServerHooks{
		AfterCall: func(ctx context.Context, callCtx *a2asrv.CallContext, resp *a2asrv.Response) error {
			experiment, _ := callCtx.ServiceParams().Get("x-experiment-id")
			log.Printf("[audit] %s experiment=%v err=%v", callCtx.Method(), experiment, resp.Err)
			return nil
		},
		AfterTaskWrite: func(ctx context.Context, w *a2aplugin.TaskWrite, err error) {
			if w.PrevTask == nil {
				log.Printf("[audit] task %s created in state %s, err=%v", w.Task.ID, w.Task.Status.State, err)
				return
			}
			log.Printf("[audit] task %s: %s -> %s, err=%v", w.Task.ID, w.PrevTask.Status.State, w.Task.Status.State, err)
		},
	})
}

type timing struct{ a2asrv.PassthroughPlugin }

func (timing) Name() string { return "timing" }

func (timing) WrapCall(next a2asrv.CallHandler) a2asrv.CallHandler {
	return func(ctx context.Context, callCtx *a2asrv.CallContext, req a2asrv.Request) iter.Seq2[any, error] {
		return func(yield func(any, error) bool) {
			start, values := time.Now(), 0
			defer func() {
				log.Printf("[timing] %s returned %d value(s) in %v", callCtx.Method(), values, time.Since(start))
			}()
			for v, err := range next(ctx, callCtx, req) {
				values++
				if !yield(v, err) {
					return
				}
			}
		}
	}
}

func main() {
	flag.Parse()

	agentExecutor := a2asrv.AgentExecutorFunc(func(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
		return func(yield func(a2a.Event, error) bool) {
			events := []a2a.Event{
				a2a.NewSubmittedTask(execCtx, execCtx.Message),
				a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateWorking, nil),
				a2a.NewArtifactEvent(execCtx, a2a.NewTextPart("Hello from the plugins example!")),
				a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCompleted, nil),
			}
			for _, event := range events {
				if !yield(event, nil) {
					return
				}
			}
		}
	})

	requestHandler := a2asrv.NewHandler(agentExecutor, a2asrv.WithPlugins(newAudit(), timing{}))

	addr := fmt.Sprintf("http://127.0.0.1:%d/invoke", *port)
	agentCard := &a2a.AgentCard{
		Name:        "Plugins Agent",
		Description: "An agent with server plugins installed",
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(addr, a2a.TransportProtocolJSONRPC),
		},
		DefaultInputModes:  []string{"text/plain"},
		DefaultOutputModes: []string{"text/plain"},
		Capabilities:       a2a.AgentCapabilities{Streaming: true},
		Skills: []a2a.AgentSkill{
			{
				ID:          "hello",
				Name:        "Hello",
				Description: "Responds with a greeting.",
				Tags:        []string{"hello"},
				Examples:    []string{"hi", "hello"},
			},
		},
	}

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("Failed to bind to a port: %v", err)
	}
	log.Printf("Starting a JSONRPC server on 127.0.0.1:%d", *port)

	mux := http.NewServeMux()
	mux.Handle("/invoke", a2asrv.NewJSONRPCHandler(requestHandler))
	mux.Handle(a2asrv.WellKnownAgentCardPath, a2asrv.NewStaticAgentCardHandler(agentCard))

	err = http.Serve(listener, mux)
	log.Printf("Server stopped: %v", err)
}
