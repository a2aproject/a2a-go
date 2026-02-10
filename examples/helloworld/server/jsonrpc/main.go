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

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
)

// agentExecutor implements [a2asrv.AgentExecutor], which is a required [a2asrv.RequestHandler] dependency.
// It is responsible for invoking an agent, translating its outputs to a2a.Event object and writing them to the provided [eventqueue.Queue].
type agentExecutor struct{}

var _ a2asrv.AgentExecutor = (*agentExecutor)(nil)

func (*agentExecutor) Execute(ctx context.Context, execCtx *a2asrv.ExecutorContext, q eventqueue.Queue) error {
	response := a2a.NewMessage(a2a.MessageRoleAgent, a2a.TextPart{Text: "Hello, world!"})
	return q.Write(ctx, response)
}

func (*agentExecutor) Cancel(ctx context.Context, execCtx *a2asrv.ExecutorContext, q eventqueue.Queue) error {
	return nil
}

var port = flag.Int("port", 9001, "Port for a gGRPC A2A server to listen on.")

func main() {
	flag.Parse()

	agentCard := &a2a.AgentCard{
		Name:               "Hello World Agent",
		Description:        "Just a hello world agent",
		URL:                fmt.Sprintf("http://127.0.0.1:%d/invoke", *port),
		PreferredTransport: a2a.TransportProtocolJSONRPC,
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
		Capabilities:       a2a.AgentCapabilities{Streaming: true},
		Skills: []a2a.AgentSkill{
			{
				ID:          "hello_world",
				Name:        "Hello, world!",
				Description: "Returns a 'Hello, world!'",
				Tags:        []string{"hello world"},
				Examples:    []string{"hi", "hello"},
			},
		},
	}

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("Failed to bind to a port: %v", err)
	}
	log.Printf("Starting a JSONRPC server on 127.0.0.1:%d", *port)

	// A transport-agnostic implementation of A2A protocol methods.
	// The behavior is configurable using option-arguments of form a2asrv.With*(), for example:
	// a2asrv.NewHandler(executor, a2asrv.WithTaskStore(customStore))
	requestHandler := a2asrv.NewHandler(&agentExecutor{})

	mux := http.NewServeMux()
	mux.Handle("/invoke", a2asrv.NewJSONRPCHandler(requestHandler))
	mux.Handle(a2asrv.WellKnownAgentCardPath, a2asrv.NewStaticAgentCardHandler(agentCard))

	err = http.Serve(listener, mux)
	log.Printf("Server stopped: %v", err)
}
