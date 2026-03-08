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
	"fmt"
	"iter"
	"net"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v1/a2a"
	"github.com/a2aproject/a2a-go/v1/a2aclient"
	"github.com/a2aproject/a2a-go/v1/a2aclient/agentcard"
	"github.com/a2aproject/a2a-go/v1/a2acompat/a2av0"
	"github.com/a2aproject/a2a-go/v1/a2asrv"

	legacycore "github.com/a2aproject/a2a-go/a2a"
	legacyclient "github.com/a2aproject/a2a-go/a2aclient"
	legacyagentcard "github.com/a2aproject/a2a-go/a2aclient/agentcard"
	legacysrv "github.com/a2aproject/a2a-go/a2asrv"
	legacyeventqueue "github.com/a2aproject/a2a-go/a2asrv/eventqueue"
)

// compat_test.go refactored to use legacy SDK in-process.

func TestCompat_OldClientNewServer(t *testing.T) {
	port, stop := startNewServer(t)
	defer stop()

	addr := fmt.Sprintf("http://127.0.0.1:%d", port)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	card, err := legacyagentcard.DefaultResolver.Resolve(ctx, addr)
	if err != nil {
		t.Fatalf("failed to resolve AgentCard: %v", err)
	}

	client, err := legacyclient.NewFromCard(ctx, card)
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
}

func TestCompat_NewClientOldServer(t *testing.T) {
	port, stop := startOldServer(t)
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	jsonCompatFactory := a2av0.NewJSONRPCTransportFactory(a2av0.JSONRPCTransportConfig{})
	factory := a2aclient.NewFactory(
		a2aclient.WithCompatTransport(a2av0.Version, a2a.TransportProtocolJSONRPC, jsonCompatFactory),
	)

	resolver := agentcard.Resolver{CardParser: a2av0.NewAgentCardParser()}

	card, err := resolver.Resolve(ctx, fmt.Sprintf("http://127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("failed to resolve AgentCard: %v", err)
	}

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
}

func startOldServer(t *testing.T) (port int, stop func()) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	port = listener.Addr().(*net.TCPAddr).Port

	agentCard := &legacycore.AgentCard{
		Name:               "Legacy Test Agent",
		Description:        "Legacy Agent for compatibility tests",
		URL:                fmt.Sprintf("http://127.0.0.1:%d/invoke", port),
		PreferredTransport: legacycore.TransportProtocolJSONRPC,
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
		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			t.Errorf("legacy server error: %v", err)
		}
	}()

	return port, func() {
		_ = srv.Shutdown(context.Background())
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

func startNewServer(t *testing.T) (port int, stop func()) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	port = listener.Addr().(*net.TCPAddr).Port

	card := &a2a.AgentCard{
		Name:        "Compat Test Agent",
		Description: "Agent for compatibility tests",
		SupportedInterfaces: []*a2a.AgentInterface{
			{
				URL:             fmt.Sprintf("http://127.0.0.1:%d/invoke", port),
				ProtocolBinding: a2a.TransportProtocolJSONRPC,
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
		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			t.Errorf("server error: %v", err)
		}
	}()

	return port, func() {
		_ = srv.Shutdown(context.Background())
	}
}
