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

package compat

import (
	"context"
	"fmt"
	"iter"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2aclient"
	"github.com/a2aproject/a2a-go/a2aclient/agentcard"
	"github.com/a2aproject/a2a-go/a2acompat/a2av0"
	"github.com/a2aproject/a2a-go/a2asrv"
)

var compatTestBinPath string

func TestMain(m *testing.M) {
	os.Exit(compileSUTAndRun(m))
}

func TestCompat_OldClientNewServer(t *testing.T) {
	binPath := compatTestBinPath

	port, stop := startNewServer(t)
	defer stop()

	addr := fmt.Sprintf("http://127.0.0.1:%d", port)

	cmd := exec.Command(binPath, "client", addr)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Old client failed against new server: %v\nOutput: %s", err, out)
	}
}

func TestCompat_NewClientOldServer(t *testing.T) {
	binPath := compatTestBinPath

	cmd := exec.Command(binPath, "server")

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}

	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	var port int
	if _, err := fmt.Fscanf(stdout, "%d\n", &port); err != nil {
		t.Fatalf("failed to read port from server: %v", err)
	}

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

func compileSUTAndRun(m *testing.M) int {
	tmpDir, err := os.MkdirTemp(os.TempDir(), "a2a-compat-test-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp dir: %v\n", err)
		return 1
	}
	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()

	compatTestBinPath = filepath.Join(tmpDir, "compat-test-bin")

	wd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to get working directory: %v\n", err)
		return 1
	}

	cmd := exec.Command("go", "build", "-o", compatTestBinPath, ".")
	cmd.Dir = filepath.Join(wd, "v0_3")
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to build compat-test binary: %v\n%s\n", err, out)
		return 1
	}
	return m.Run()
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
	jsonRpcHandler := a2av0.NewJSONRPCHandler(requestHandler, a2av0.JSONRPCHandlerConfig{})

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
