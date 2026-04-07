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

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"net"
	"net/http"
	"os"
	"os/signal"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"

	"github.com/a2aproject/a2a-go/v2/a2a"
	a2agrpc "github.com/a2aproject/a2a-go/v2/a2agrpc/v1"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

func newServeCmd(cfg *globalConfig) *cobra.Command {
	var (
		port     int
		host     string
		name     string
		desc     string
		cardFile string
		quiet    bool
		echo     bool
		proxyURL string
		execCmd  string
		chunk    string
	)

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start an A2A-compliant server",
		RunE: func(cmd *cobra.Command, args []string) error {
			modes := 0
			if echo {
				modes++
			}
			if proxyURL != "" {
				modes++
			}
			if execCmd != "" {
				modes++
			}
			if modes > 1 {
				return fmt.Errorf("--echo, --proxy, and --exec are mutually exclusive")
			}
			if modes == 0 {
				return fmt.Errorf("specify --echo, --proxy <url>, or --exec <cmd>")
			}

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
			defer stop()

			listener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", host, port))
			if err != nil {
				return fmt.Errorf("listen: %w", err)
			}

			addr := listener.Addr().String()
			serveTransport := cfg.transport
			if serveTransport == "" {
				serveTransport = "rest"
			}

			proto := a2a.TransportProtocolHTTPJSON
			switch serveTransport {
			case "jsonrpc":
				proto = a2a.TransportProtocolJSONRPC
			case "grpc":
				proto = a2a.TransportProtocolGRPC
			}

			switch {
			case echo:
				return serveEcho(ctx, cfg, listener, addr, proto, name, desc, cardFile, quiet)
			case proxyURL != "":
				return serveProxy(ctx, cfg, listener, addr, proto, proxyURL, cardFile, quiet)
			default:
				return serveExec(ctx, cfg, listener, addr, proto, execCmd, chunk, name, desc, cardFile, quiet)
			}
		},
	}

	f := cmd.Flags()
	f.IntVar(&port, "port", 8080, "Listen port")
	f.StringVar(&host, "host", "127.0.0.1", "Bind address")
	f.StringVar(&name, "name", "", "Agent name for the auto-generated card")
	f.StringVar(&desc, "description", "", "Agent description")
	f.StringVar(&cardFile, "card", "", "Serve a custom agent card JSON file")
	f.BoolVar(&quiet, "quiet", false, "Suppress traffic logging to stderr")
	f.BoolVar(&echo, "echo", false, "Echo mode: return the user's message as a response")
	f.StringVar(&proxyURL, "proxy", "", "Proxy mode: forward requests to an upstream agent URL")
	f.StringVar(&execCmd, "exec", "", "Exec mode: run a command as an A2A agent")
	f.StringVar(&chunk, "chunk", "", "Delimiter for streaming exec output (implies --exec)")

	return cmd
}

func loadOrBuildCard(cardFile, name, desc, addr string, proto a2a.TransportProtocol) (*a2a.AgentCard, error) {
	if cardFile != "" {
		data, err := os.ReadFile(cardFile)
		if err != nil {
			return nil, fmt.Errorf("reading card file: %w", err)
		}
		card := new(a2a.AgentCard)
		if err := json.Unmarshal(data, card); err != nil {
			return nil, fmt.Errorf("parsing card file: %w", err)
		}
		return card, nil
	}

	if name == "" {
		name = "a2a-cli"
	}
	url := "http://" + addr
	if proto == a2a.TransportProtocolGRPC {
		url = addr
	}
	return &a2a.AgentCard{
		Name:                name,
		Description:         desc,
		Version:             "1.0.0",
		SupportedInterfaces: []*a2a.AgentInterface{a2a.NewAgentInterface(url, proto)},
	}, nil
}

// startTransportServer starts the appropriate server (HTTP or gRPC) based on transport.
func startTransportServer(ctx context.Context, listener net.Listener, handler a2asrv.RequestHandler, card *a2a.AgentCard, transport string, quiet bool) error {
	if transport == "grpc" {
		return startGRPCServer(ctx, listener, handler, card, quiet)
	}
	mux := buildMux(handler, card, transport)
	return startHTTPServer(ctx, listener, mux, quiet)
}

func startHTTPServer(ctx context.Context, listener net.Listener, handler http.Handler, quiet bool) error {
	addr := listener.Addr().String()
	srv := &http.Server{Handler: handler}

	go func() {
		<-ctx.Done()
		if err := srv.Shutdown(context.Background()); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "HTTP server shutdown: %v\n", err)
		}
	}()

	if !quiet {
		fmt.Fprintf(os.Stderr, "Listening on %s\n", addr)
	}

	if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func startGRPCServer(ctx context.Context, listener net.Listener, handler a2asrv.RequestHandler, card *a2a.AgentCard, quiet bool) error {
	grpcHandler := a2agrpc.NewHandler(handler)
	s := grpc.NewServer()
	grpcHandler.RegisterWith(s)

	cardMux := http.NewServeMux()
	cardMux.Handle(a2asrv.WellKnownAgentCardPath, a2asrv.NewStaticAgentCardHandler(card))
	cardListener, err := net.Listen("tcp", ":0")
	if err != nil {
		return fmt.Errorf("creating agent card listener: %w", err)
	}

	if !quiet {
		fmt.Fprintf(os.Stderr, "gRPC listening on %s\n", listener.Addr())
		fmt.Fprintf(os.Stderr, "Agent card at http://%s%s\n", cardListener.Addr(), a2asrv.WellKnownAgentCardPath)
	}

	go func() {
		<-ctx.Done()
		s.GracefulStop()
		if err := cardListener.Close(); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Agent card listener close: %v\n", err)
		}
	}()

	go func() {
		if err := http.Serve(cardListener, cardMux); err != nil && !errors.Is(err, net.ErrClosed) {
			_, _ = fmt.Fprintf(os.Stderr, "Agent card server: %v\n", err)
		}
	}()

	if err := s.Serve(listener); err != nil {
		return fmt.Errorf("gRPC server: %w", err)
	}
	return nil
}

func buildMux(handler a2asrv.RequestHandler, card *a2a.AgentCard, transport string) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle(a2asrv.WellKnownAgentCardPath, a2asrv.NewStaticAgentCardHandler(card))

	switch transport {
	case "jsonrpc":
		mux.Handle("/", a2asrv.NewJSONRPCHandler(handler))
	default:
		mux.Handle("/", a2asrv.NewRESTHandler(handler))
	}
	return mux
}

func serveEcho(ctx context.Context, cfg *globalConfig, listener net.Listener, addr string, proto a2a.TransportProtocol, name, desc, cardFile string, quiet bool) error {
	if name == "" {
		name = "Echo Agent"
	}
	if desc == "" {
		desc = "Echoes the user's message back as a response"
	}

	card, err := loadOrBuildCard(cardFile, name, desc, addr, proto)
	if err != nil {
		return err
	}

	handler := a2asrv.NewHandler(&echoExecutor{})
	transport := cfg.transport
	if transport == "" {
		transport = "rest"
	}

	cfg.logf("echo mode, transport=%s", transport)
	return startTransportServer(ctx, listener, handler, card, transport, quiet)
}

type echoExecutor struct{}

func (e *echoExecutor) Execute(_ context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		if execCtx.StoredTask == nil {
			if !yield(a2a.NewSubmittedTask(execCtx, execCtx.Message), nil) {
				return
			}
		}
		if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateWorking, nil), nil) {
			return
		}
		text := messageText(execCtx.Message)
		evt := a2a.NewArtifactEvent(execCtx, a2a.NewTextPart(text))
		evt.LastChunk = true
		if !yield(evt, nil) {
			return
		}
		yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCompleted, nil), nil)
	}
}

func (e *echoExecutor) Cancel(_ context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCanceled, nil), nil)
	}
}
