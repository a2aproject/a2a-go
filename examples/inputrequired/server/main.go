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

type agentExecutor struct{}

var _ a2asrv.AgentExecutor = (*agentExecutor)(nil)

func (*agentExecutor) Execute(ctx context.Context, reqCtx *a2asrv.RequestContext, q eventqueue.Queue) error {
	log.Printf("Executing request. TaskID: %s", reqCtx.TaskID)

	if reqCtx.StoredTask == nil {
		// New task, ask for input
		log.Println("New task. Asking for input.")
		msg := a2a.NewMessageForTask(a2a.MessageRoleAgent, reqCtx, a2a.TextPart{Text: "Please provide more details."})
		return reply(ctx, q, reqCtx, a2a.TaskStateInputRequired, msg)
	}

	// Task exists, this must be the input we asked for
	log.Println("Task exists. Completing task.")
	msg := a2a.NewMessageForTask(a2a.MessageRoleAgent, reqCtx, a2a.TextPart{Text: "Thank you for the input! Task completed."})
	return reply(ctx, q, reqCtx, a2a.TaskStateCompleted, msg)
}

func reply(ctx context.Context, q eventqueue.Queue, reqCtx *a2asrv.RequestContext, state a2a.TaskState, msg *a2a.Message) error {
	// Set state and attach the message.
	// Note: We MUST wrap the message in StatusUpdateEvent, otherwise sending a raw Message
	// event will be interpreted as a stateless execution result and the task won't be persisted.
	// We set Final=true to indicate this turn is over, so the server stops processing
	// and the client receives the response.
	statusUpdate := a2a.NewStatusUpdateEvent(reqCtx, state, msg)
	statusUpdate.Final = true
	return q.Write(ctx, statusUpdate)
}

func (*agentExecutor) Cancel(ctx context.Context, reqCtx *a2asrv.RequestContext, q eventqueue.Queue) error {
	return nil
}

var port = flag.Int("port", 9002, "Port for the A2A server to listen on.")

func main() {
	flag.Parse()

	agentCard := &a2a.AgentCard{
		Name:               "Input Required Agent",
		Description:        "Agent that demonstrates input-required state",
		URL:                fmt.Sprintf("http://127.0.0.1:%d/invoke", *port),
		PreferredTransport: a2a.TransportProtocolJSONRPC,
		Capabilities:       a2a.AgentCapabilities{Streaming: true},
	}

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("Failed to bind to a port: %v", err)
	}
	log.Printf("Starting server on 127.0.0.1:%d", *port)

	requestHandler := a2asrv.NewHandler(&agentExecutor{})

	mux := http.NewServeMux()
	mux.Handle("/invoke", a2asrv.NewJSONRPCHandler(requestHandler))
	mux.Handle(a2asrv.WellKnownAgentCardPath, a2asrv.NewStaticAgentCardHandler(agentCard))

	err = http.Serve(listener, mux)
	log.Printf("Server stopped: %v", err)
}
