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

package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2aclient"
	"github.com/a2aproject/a2a-go/a2aclient/agentcard"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: compat-test [client <address> | server]")
		os.Exit(1)
	}

	mode := os.Args[1]

	switch mode {
	case "client":
		if len(os.Args) < 3 {
			fmt.Println("Usage: compat-test client <address>")
			os.Exit(1)
		}
		runClient(os.Args[2])
	case "server":
		runServer()
	default:
		fmt.Printf("Unknown mode: %s\n", mode)
		os.Exit(1)
	}
}

// runClient implements the client mode of compat-test.
// It constructs an A2A Client from an AgentCard obtained via the provided address,
// sends a single "ping" message, and exits with 0 if it receives a "pong" response.
func runClient(address string) {
	ctx := context.Background()
	card, err := agentcard.DefaultResolver.Resolve(ctx, address)
	if err != nil {
		fmt.Printf("failed to resolve AgentCard: %v\n", err)
		os.Exit(1)
	}

	client, err := a2aclient.NewFromCard(ctx, card)
	if err != nil {
		fmt.Printf("failed to create client: %v\n", err)
		os.Exit(1)
	}

	msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: "ping"})
	req := &a2a.MessageSendParams{
		Message: msg,
	}
	resp, err := client.SendMessage(ctx, req)
	if err != nil {
		fmt.Printf("failed to send message: %v\n", err)
		os.Exit(1)
	}

	respMsg, ok := resp.(*a2a.Message)
	if !ok {
		fmt.Printf("expected Message response, got: %T\n", resp)
		os.Exit(1)
	}

	for _, p := range respMsg.Parts {
		if textPart, ok := p.(a2a.TextPart); ok {
			if textPart.Text == "pong" {
				os.Exit(0)
			}
		}
	}

	fmt.Printf("unexpected response: %+v\n", resp)
	os.Exit(1)
}

// agentExecutor implements the a2asrv.AgentExecutor interface for the server mode.
type agentExecutor struct{}

// Execute validates that the request contains a "ping" text part,
// and responds with a "pong" text part, or returns an error otherwise.
func (*agentExecutor) Execute(ctx context.Context, reqCtx *a2asrv.RequestContext, q eventqueue.Queue) error {
	for _, p := range reqCtx.Message.Parts {
		if textPart, ok := p.(a2a.TextPart); ok {
			if textPart.Text == "ping" {
				response := a2a.NewMessage(a2a.MessageRoleAgent, a2a.TextPart{Text: "pong"})
				return q.Write(ctx, response)
			}
		}
	}
	return fmt.Errorf("expected ping message")
}

// Cancel implements a2asrv.AgentExecutor.Cancel, which is a no-op here.
func (*agentExecutor) Cancel(ctx context.Context, reqCtx *a2asrv.RequestContext, q eventqueue.Queue) error {
	return nil
}

// runServer implements the server mode of compat-test.
// It starts an HTTP JSON-RPC A2A server on an ephemeral port, prints the port
// number to stdout, processes exactly one request, and then shuts down the server.
func runServer() {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Printf("failed to listen: %v\n", err)
		os.Exit(1)
	}

	port := listener.Addr().(*net.TCPAddr).Port
	fmt.Printf("%d\n", port)

	agentCard := &a2a.AgentCard{
		Name:               "Compat Test Agent",
		Description:        "Agent for compatibility tests",
		URL:                fmt.Sprintf("http://127.0.0.1:%d/invoke", port),
		PreferredTransport: a2a.TransportProtocolJSONRPC,
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
		Capabilities:       a2a.AgentCapabilities{Streaming: false},
	}

	requestHandler := a2asrv.NewHandler(&agentExecutor{})
	jsonRpcHandler := a2asrv.NewJSONRPCHandler(requestHandler)
	mux := http.NewServeMux()

	srv := &http.Server{Handler: mux}

	mux.Handle("/invoke", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonRpcHandler.ServeHTTP(w, r)
		// Initiate a graceful shutdown in a separate goroutine after the first request is handled
		go func() {
			srv.Shutdown(context.Background())
		}()
	}))
	mux.Handle(a2asrv.WellKnownAgentCardPath, a2asrv.NewStaticAgentCardHandler(agentCard))

	err = srv.Serve(listener)
	if err != nil && err != http.ErrServerClosed {
		fmt.Printf("server error: %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}
