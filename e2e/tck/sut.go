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
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	// "github.com/a2aproject/a2a-go/a2agrpc"
	"github.com/a2aproject/a2a-go/a2asrv"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
)

type intercepter struct {
	a2asrv.PassthroughCallInterceptor
}

func (i *intercepter) Before(ctx context.Context, callCtx *a2asrv.CallContext, req *a2asrv.Request) (context.Context, any, error) {
	if callCtx.Method() == "OnSendMessage" {
		sendParams := req.Payload.(*a2a.SendMessageRequest)
		if sendParams.Config == nil {
			sendParams.Config = &a2a.SendMessageConfig{}
		}
		if sendParams.Config.Blocking == nil {
			blocking := false
			sendParams.Config.Blocking = &blocking
		}
	}
	return ctx, nil, nil
}

func main() {
	mode := flag.String("mode", "http", "mode to run in: http(JSON-RPC/REST) or grpc")
	httpPort := flag.Int("http-port", 9999, "HTTP port")
	grpcPort := flag.Int("grpc-port", 9998, "gRPC port")
	flag.Parse()

	agentExecutor := newCustomAgentExecutor()

	var cardUrl string
	var preferredTransport a2a.TransportProtocol
	switch *mode {
	case "grpc":
		cardUrl = fmt.Sprintf("http://localhost:%d", *grpcPort)
		preferredTransport = a2a.TransportProtocolGRPC
	// TODO: handle REST case
	default:
		cardUrl = fmt.Sprintf("http://localhost:%d", *httpPort)
		preferredTransport = a2a.TransportProtocolJSONRPC
	}

	agentCard := &a2a.AgentCard{
		Name:        "TCK Core Agent",
		Description: "A complete A2A agent implementation designed specifically for testing with the A2A Technology Compatibility Kit (TCK)",
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(cardUrl, preferredTransport),
		},
		Version:            "1.0.0",
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
		Capabilities:       a2a.AgentCapabilities{Streaming: true},
		// security
		Skills: []a2a.AgentSkill{
			{
				ID:          "tck_core_agent",
				Name:        "TCK Core Agent",
				Description: "A complete A2A agent implementation designed for TCK testing",
				Tags:        []string{"hello world", "how are you", "goodbye", "hi"},
				Examples:    []string{"tck", "testing", "core", "complete"},
			},
		},
	}

	requestHandler := a2asrv.NewHandler(agentExecutor, a2asrv.WithExtendedAgentCard(agentCard), a2asrv.WithCallInterceptors(&intercepter{}))

	var group errgroup.Group
	group.Go(func() error {
		return startGRPCServer(*grpcPort, requestHandler)
	})
	group.Go(func() error {
		return startHTTPServer(*httpPort, agentCard, requestHandler)
	})
	if err := group.Wait(); err != nil {
		log.Fatalf("Server shutdown: %v", err)
	}

}

func startGRPCServer(port int, handler a2asrv.RequestHandler) error {
	grpcListener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("failed to bind gRPC port: %w", err)
	}
	log.Printf("Starting a gRPC server on 127.0.0.1:%d", port)

	// TODO: uncomment and fix after pbconv is implemented

	// grpcHandler := a2agrpc.NewHandler(handler)
	grpcServer := grpc.NewServer()
	// grpcHandler.RegisterWith(grpcServer)
	return grpcServer.Serve(grpcListener)
}

func startHTTPServer(port int, card *a2a.AgentCard, handler a2asrv.RequestHandler) error {
	httpListener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("failed to bind HTTP port: %w", err)
	}
	log.Printf("Starting an HTTP server on 127.0.0.1:%d", port)

	mux := http.NewServeMux()

	// serve public card
	mux.Handle(a2asrv.WellKnownAgentCardPath, a2asrv.NewStaticAgentCardHandler(card))

	mux.HandleFunc("/quit", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("shutting down")); err != nil {
			log.Printf("Error writing response: %v", err)
		}

		// Exit in a background goroutine to allow the response to flush
		go func() {
			time.Sleep(100 * time.Millisecond)
			os.Exit(0)
		}()
	})

	// serve JSON-RPC endpoint
	mux.Handle("/", a2asrv.NewJSONRPCHandler(handler))

	// TODO: serve REST endpoint

	return http.Serve(httpListener, mux)
}
