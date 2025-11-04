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
	"github.com/a2aproject/a2a-go/a2agrpc"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
)

// agentExecutor implements [a2asrv.AgentExecutor], which is a required [a2asrv.RequestHandler] dependency.
// It is responsible for invoking an agent, translating its outputs to a2a.Event object and writing them to the provided [eventqueue.Queue].
type agentExecutor struct{}

func (*agentExecutor) Execute(ctx context.Context, reqCtx *a2asrv.RequestContext, q eventqueue.Queue) error {
	response := a2a.NewMessage(a2a.MessageRoleAgent, a2a.TextPart{Text: "Hello, world!"})
	return q.Write(ctx, response)
}

func (*agentExecutor) Cancel(ctx context.Context, reqCtx *a2asrv.RequestContext, q eventqueue.Queue) error {
	return nil
}

func startGRPCServer(port int, card *a2a.AgentCard) error {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return err
	}
	log.Printf("Starting a gRPC server on 127.0.0.1:%d", port)

	// A transport-agnostic implementation of A2A protocol methods.
	// The behavior is configurable using option-arguments of form a2asrv.With*(), for example:
	// a2asrv.NewHandler(executor, a2asrv.WithTaskStore(customStore))
	requestHandler := a2asrv.NewHandler(&agentExecutor{}, a2asrv.WithExtendedAgentCard(card))

	// A gRPC-transport implementation for A2A.
	grpcHandler := a2agrpc.NewHandler(requestHandler)

	s := grpc.NewServer()
	grpcHandler.RegisterWith(s)
	return s.Serve(listener)
}

func servePublicCard(port int, card *a2a.AgentCard) error {
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return err
	}

	log.Printf("Starting a public AgentCard server on 127.0.0.1:%d", port)

	mux := http.NewServeMux()
	mux.Handle(a2asrv.WellKnownAgentCardPath, a2asrv.NewStaticAgentCardHandler(card))
	return http.Serve(listener, mux)
}

var (
	grpcPort = flag.Int("grpc-port", 9000, "Port for a gGRPC A2A server to listen on.")
	cardPort = flag.Int("card-port", 9001, "Port for a public A2A AgentCard server to listen on.")
)

func main() {
	flag.Parse()

	agentCard := &a2a.AgentCard{
		Name:               "Hello World Agent",
		Description:        "Just a hello world agent",
		URL:                fmt.Sprintf("127.0.0.1:%d", *grpcPort),
		PreferredTransport: a2a.TransportProtocolGRPC,
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

	var group errgroup.Group
	group.Go(func() error {
		return startGRPCServer(*grpcPort, agentCard)
	})
	group.Go(func() error {
		return servePublicCard(*cardPort, agentCard)
	})
	if err := group.Wait(); err != nil {
		log.Fatalf("Server shutdown: %v", err)
	}
}
