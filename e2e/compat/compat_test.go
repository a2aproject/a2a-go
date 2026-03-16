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

package compat_test

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"net"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aclient/agentcard"
	"github.com/a2aproject/a2a-go/v2/a2acompat/a2av0"
	"github.com/a2aproject/a2a-go/v2/a2agrpc/v0"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	legacycore "github.com/a2aproject/a2a-go/a2a"
	legacyclient "github.com/a2aproject/a2a-go/a2aclient"
	legacyagentcard "github.com/a2aproject/a2a-go/a2aclient/agentcard"
	legacygrpc "github.com/a2aproject/a2a-go/a2agrpc"
	legacysrv "github.com/a2aproject/a2a-go/a2asrv"

	legacyeventqueue "github.com/a2aproject/a2a-go/a2asrv/eventqueue"
)

// compat_test.go refactored to use legacy SDK in-process.

func TestCompat_OldClientNewServer(t *testing.T) {
	transports := []a2a.TransportProtocol{a2a.TransportProtocolJSONRPC, a2a.TransportProtocolGRPC}
	for _, transport := range transports {
		t.Run(string(transport), func(t *testing.T) {
			port, stop := startNewServer(t, transport)
			defer stop()

			addr := fmt.Sprintf("http://127.0.0.1:%d", port)

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			card, err := legacyagentcard.DefaultResolver.Resolve(ctx, addr)
			if err != nil {
				t.Fatalf("failed to resolve AgentCard: %v", err)
			}

			var opts []legacyclient.FactoryOption
			if transport == a2a.TransportProtocolGRPC {
				opts = append(opts, legacyclient.WithGRPCTransport(grpc.WithTransportCredentials(insecure.NewCredentials())))
			}
			client, err := legacyclient.NewFromCard(ctx, card, opts...)
			if err != nil {
				t.Fatalf("failed to create client: %v", err)
			}

			msg := legacycore.NewMessage(legacycore.MessageRoleUser, legacycore.TextPart{Text: "ping"})
			req := &legacycore.MessageSendParams{
				Message: msg,
			}

			resp, err := client.SendMessage(ctx, req)
			if err != nil {
				t.Fatalf("Old client failed against new server: %v", err)
			}

			respMsg, ok := resp.(*legacycore.Message)
			if !ok {
				t.Fatalf("expected Message response, got: %T", resp)
			}

			found := false
			for _, p := range respMsg.Parts {
				if tp, ok := p.(legacycore.TextPart); ok && tp.Text == "pong" {
					found = true
					break
				}
			}

			if !found {
				t.Fatalf("unexpected response message parts: %+v", respMsg.Parts)
			}
		})
	}
}

func TestCompat_NewClientOldServer(t *testing.T) {
	transports := []legacycore.TransportProtocol{legacycore.TransportProtocolJSONRPC, legacycore.TransportProtocolGRPC}
	for _, legacyTransport := range transports {
		t.Run(string(legacyTransport), func(t *testing.T) {
			port, stop := startOldServer(t, legacyTransport)
			defer stop()

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			resolver := agentcard.Resolver{CardParser: a2av0.NewAgentCardParser()}
			card, err := resolver.Resolve(ctx, fmt.Sprintf("http://127.0.0.1:%d", port))
			if err != nil {
				t.Fatalf("failed to resolve AgentCard: %v", err)
			}

			var compatFactory a2aclient.TransportFactory
			var compatTransport a2a.TransportProtocol
			switch legacyTransport {
			case legacycore.TransportProtocolJSONRPC:
				compatFactory = a2av0.NewJSONRPCTransportFactory(a2av0.JSONRPCTransportConfig{})
				compatTransport = a2a.TransportProtocolJSONRPC
			case legacycore.TransportProtocolGRPC:
				compatFactory = a2agrpc.NewGRPCTransportFactory(grpc.WithTransportCredentials(insecure.NewCredentials()))
				compatTransport = a2a.TransportProtocolGRPC
			default:
				t.Fatalf("unsupported transport protocol: %v", legacyTransport)
			}

			factory := a2aclient.NewFactory(
				a2aclient.WithCompatTransport(a2av0.Version, compatTransport, compatFactory),
			)
			client, err := factory.CreateFromCard(ctx, card)
			if err != nil {
				t.Fatalf("failed to create client: %v", err)
			}

			msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("ping"))
			req := &a2a.SendMessageRequest{Message: msg}

			resp, err := client.SendMessage(ctx, req)
			if err != nil {
				t.Fatalf("failed to send message: %v", err)
			}

			respMsg, ok := resp.(*a2a.Message)
			if !ok {
				t.Fatalf("expected Message response, got: %T", resp)
			}

			gotPong := slices.ContainsFunc(respMsg.Parts, func(p *a2a.Part) bool { return p.Text() == "pong" })
			if !gotPong {
				t.Fatalf("unexpected response message parts: %+v", respMsg.Parts)
			}
		})
	}
}

func startOldServer(t *testing.T, transport legacycore.TransportProtocol) (port int, stop func()) {
	t.Helper()
	httpListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	httpPort := httpListener.Addr().(*net.TCPAddr).Port

	var grpcListener net.Listener
	var grpcPort int
	if transport == legacycore.TransportProtocolGRPC {
		grpcListener, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("failed to listen: %v", err)
		}
		grpcPort = grpcListener.Addr().(*net.TCPAddr).Port
	}

	var cardURL string
	switch transport {
	case legacycore.TransportProtocolJSONRPC:
		cardURL = fmt.Sprintf("http://127.0.0.1:%d/invoke", httpPort)
	case legacycore.TransportProtocolGRPC:
		cardURL = fmt.Sprintf("127.0.0.1:%d", grpcPort)
	default:
		t.Fatalf("unsupported transport protocol: %v", transport)
	}
	agentCard := &legacycore.AgentCard{
		Name:               "Legacy Test Agent",
		Description:        "Legacy Agent for compatibility tests",
		URL:                cardURL,
		PreferredTransport: transport,
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
		Capabilities:       legacycore.AgentCapabilities{Streaming: false},
	}

	executor := &legacyAgentExecutor{}
	requestHandler := legacysrv.NewHandler(executor)
	jsonRpcHandler := legacysrv.NewJSONRPCHandler(requestHandler)
	mux := http.NewServeMux()

	mux.Handle("/invoke", jsonRpcHandler)
	mux.Handle(legacysrv.WellKnownAgentCardPath, legacysrv.NewStaticAgentCardHandler(agentCard))

	srv := &http.Server{Handler: mux}

	go func() {
		if err := srv.Serve(httpListener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Errorf("legacy server error: %v", err)
		}
	}()

	var grpcServer *grpc.Server
	if transport == legacycore.TransportProtocolGRPC {
		grpcHandler := legacygrpc.NewHandler(requestHandler)
		grpcServer = grpc.NewServer()
		grpcHandler.RegisterWith(grpcServer)
		go func() {
			if err := grpcServer.Serve(grpcListener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
				t.Errorf("legacy server error: %v", err)
			}
		}()
	}

	return httpPort, func() {
		_ = srv.Shutdown(context.Background())
		if grpcServer != nil {
			grpcServer.GracefulStop()
		}
	}
}

type legacyAgentExecutor struct{}

func (*legacyAgentExecutor) Execute(ctx context.Context, reqCtx *legacysrv.RequestContext, q legacyeventqueue.Queue) error {
	for _, p := range reqCtx.Message.Parts {
		if textPart, ok := p.(legacycore.TextPart); ok {
			if textPart.Text == "ping" {
				response := legacycore.NewMessage(legacycore.MessageRoleAgent, legacycore.TextPart{Text: "pong"})
				return q.Write(ctx, response)
			}
		}
	}
	return fmt.Errorf("expected ping message")
}

func (*legacyAgentExecutor) Cancel(ctx context.Context, reqCtx *legacysrv.RequestContext, q legacyeventqueue.Queue) error {
	return nil
}

type testAgentExecutor struct {
	t *testing.T
}

func (e *testAgentExecutor) Execute(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		for _, p := range execCtx.Message.Parts {
			if text, ok := p.Content.(a2a.Text); ok {
				if string(text) == "ping" {
					response := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("pong"))
					yield(response, nil)
					return
				}
			}
		}
		yield(nil, fmt.Errorf("expected ping message"))
	}
}

func (e *testAgentExecutor) Cancel(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {}
}

func startNewServer(t *testing.T, preferredTransport a2a.TransportProtocol) (port int, stop func()) {
	t.Helper()
	httpListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	httpPort := httpListener.Addr().(*net.TCPAddr).Port

	var grpcListener net.Listener
	var grpcPort int
	if preferredTransport == a2a.TransportProtocolGRPC {
		grpcListener, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("failed to listen: %v", err)
		}
		grpcPort = grpcListener.Addr().(*net.TCPAddr).Port
	}

	var cardURL string
	switch preferredTransport {
	case a2a.TransportProtocolJSONRPC:
		cardURL = fmt.Sprintf("http://127.0.0.1:%d/invoke", httpPort)
	case a2a.TransportProtocolGRPC:
		cardURL = fmt.Sprintf("127.0.0.1:%d", grpcPort)
	default:
		t.Fatalf("unsupported transport protocol: %v", preferredTransport)
	}
	card := &a2a.AgentCard{
		Name:        "Compat Test Agent",
		Description: "Agent for compatibility tests",
		SupportedInterfaces: []*a2a.AgentInterface{
			{
				URL:             cardURL,
				ProtocolBinding: preferredTransport,
				ProtocolVersion: a2av0.Version,
			},
		},
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
		Capabilities:       a2a.AgentCapabilities{Streaming: false},
	}
	cardProducer := a2av0.NewStaticAgentCardProducer(card)

	executor := &testAgentExecutor{t: t}
	requestHandler := a2asrv.NewHandler(executor)
	jsonRpcHandler := a2av0.NewJSONRPCHandler(requestHandler)

	mux := http.NewServeMux()
	mux.Handle("/invoke", jsonRpcHandler)
	mux.Handle(a2asrv.WellKnownAgentCardPath, a2asrv.NewAgentCardHandler(cardProducer))

	srv := &http.Server{Handler: mux}

	go func() {
		if err := srv.Serve(httpListener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Errorf("server error: %v", err)
		}
	}()

	var grpcServer *grpc.Server
	if preferredTransport == a2a.TransportProtocolGRPC {
		grpcHandler := a2agrpc.NewHandler(requestHandler)
		grpcServer = grpc.NewServer()
		grpcHandler.RegisterWith(grpcServer)
		go func() {
			if err := grpcServer.Serve(grpcListener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
				t.Errorf("server error: %v", err)
			}
		}()
	}

	return httpPort, func() {
		_ = srv.Shutdown(context.Background())
		if grpcServer != nil {
			grpcServer.GracefulStop()
		}
	}
}
