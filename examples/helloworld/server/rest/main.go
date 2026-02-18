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
	"iter"
	"log"
	"net"
	"net/http"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"
)

// agentExecutor implements [a2asrv.AgentExecutor], which is a required [a2asrv.RequestHandler] dependency.
type agentExecutor struct{}

func (*agentExecutor) Execute(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		response := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("Hello from REST server!"))
		yield(response, nil)
	}
}

func (*agentExecutor) Cancel(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {}
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

	addr := fmt.Sprintf("http://127.0.0.1:%d", *port)
	agentCard := &a2a.AgentCard{
		Name:        "REST Hello World Agent",
		Description: "Just a rest hello world agent",
		SupportedInterfaces: []a2a.AgentInterface{
			{URL: addr, ProtocolBinding: a2a.TransportProtocolHTTPJSON},
		},
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
		Capabilities:       a2a.AgentCapabilities{Streaming: true},
		Skills: []a2a.AgentSkill{
			{
				ID:          "hello_world",
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
