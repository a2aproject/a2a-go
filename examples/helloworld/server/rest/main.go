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
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
)

// agentExecutor implements [a2asrv.AgentExecutor], which is a required [a2asrv.RequestHandler] dependency.
type agentExecutor struct{}

func (*agentExecutor) Execute(ctx context.Context, reqCtx *a2asrv.ExecutorContext, q eventqueue.Queue) error {
	response := a2a.NewMessage(a2a.MessageRoleAgent, a2a.TextPart{Text: "Hello from REST server!"})
	return q.Write(ctx, response)
}

func (*agentExecutor) Cancel(ctx context.Context, reqCtx *a2asrv.ExecutorContext, q eventqueue.Queue) error {
	return nil
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			log.Printf("Error reading request body: %v", err)
			return
		}
		r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
		log.Printf("-> Request: [%s] %s", r.Method, r.URL.Path)
		if len(bodyBytes) > 0 {
			log.Printf("-> Data: %s", string(bodyBytes))
		}

		next.ServeHTTP(w, r)
	})
}

var (
	port = flag.Int("port", 9001, "Port for REST A2A server to listen on.")
)

func main() {
	flag.Parse()

	agentCard := &a2a.AgentCard{
		Name:               "REST Hello World Agent",
		Description:        "Just a rest hello world agent",
		URL:                fmt.Sprintf("http://127.0.0.1:%d", *port),
		PreferredTransport: a2a.TransportProtocolHTTPJSON,
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
		Capabilities:       a2a.AgentCapabilities{Streaming: true},
		Skills: []a2a.AgentSkill{
			{
				ID:          "",
				Name:        "REST Hello world!",
				Description: "Returns a 'Hello from REST server!'",
				Tags:        []string{"hello world"},
				Examples:    []string{"hi", "hello"},
			},
		},
	}

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("Failed to bind to a port: %v", err)
	}
	log.Printf("Starting a REST server on 127.0.0.1:%d", *port)

	// A transport-agnostic implementation of A2A protocol methods.
	// The behavior is configurable using option-arguments of form a2asrv.With*(), for example:
	// a2asrv.NewHandler(executor, a2asrv.WithTaskStore(customStore))
	requestHandler := a2asrv.NewHandler(&agentExecutor{})

	// Mount REST handler directly at root so /v1/... paths match its internal routes
	mux := http.NewServeMux()
	mux.Handle("/", a2asrv.NewRESTHandler(requestHandler))
	mux.Handle(a2asrv.WellKnownAgentCardPath, a2asrv.NewStaticAgentCardHandler(agentCard))

	loggedRouter := loggingMiddleware(mux)
	err = http.Serve(listener, loggedRouter)

	if err != nil {
		log.Fatal(err)
	}
}
