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

// Package main demonstrates how to configure observability for an A2A server.
//
// It shows:
//   - Setting up structured logging with slog at different levels
//   - Wrapping the slog handler with a custom A2A type formatter
//   - Redirecting logs to a file
//   - Attaching the logging interceptor for request/response visibility
//   - Using payload logging for development debugging
//
// Run with: go run . -level debug -log-file agent.log
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"iter"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	a2alog "github.com/a2aproject/a2a-go/v2/log"
)

type agentExecutor struct{}

func (*agentExecutor) Execute(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		yield(a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("Hello from the observable agent!")), nil)
	}
}

func (*agentExecutor) Cancel(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {}
}

var (
	port    = flag.Int("port", 9001, "Port for the server to listen on.")
	level   = flag.String("level", "info", "Log level: debug, info, warn, error.")
	logFile = flag.String("log-file", "", "Path to a log file. Logs go to stderr when empty.")
	payload = flag.Bool("payload", false, "Enable request/response payload logging.")
)

func main() {
	flag.Parse()

	// 1. Choose the log level.
	//    debug – development, shows every interceptor log including payloads.
	//    info  – default for production, shows request lifecycle.
	//    warn  – quiet, shows only errors and warnings.
	var slogLevel slog.Level
	switch *level {
	case "debug":
		slogLevel = slog.LevelDebug
	case "info":
		slogLevel = slog.LevelInfo
	case "warn":
		slogLevel = slog.LevelWarn
	case "error":
		slogLevel = slog.LevelError
	default:
		log.Fatalf("unknown log level %q, use debug|info|warn|error", *level)
	}

	// 2. Choose output destination.
	//    By default logs go to stderr. Pass -log-file to redirect to a file.
	var output io.Writer = os.Stderr
	if *logFile != "" {
		f, err := os.OpenFile(*logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			log.Fatalf("Failed to open log file: %v", err)
		}
		defer func() { _ = f.Close() }()
		output = io.MultiWriter(os.Stderr, f)
	}

	// 3. Create a structured logger.
	//    Use slog.NewJSONHandler for machine-readable logs (production).
	//    Use slog.NewTextHandler for human-readable logs (development).
	//
	//    Wrapping with a2alog.NewHandler applies the type formatter so that
	//    A2A types (Task, Message, events) are logged with only their key
	//    identifiers instead of the full object. Pass a custom TypeFormatter
	//    to control which fields appear.
	jsonHandler := slog.NewTextHandler(output, &slog.HandlerOptions{
		Level:     slogLevel,
		AddSource: slogLevel == slog.LevelDebug,
	})
	logger := slog.New(a2alog.AttachFormatter(jsonHandler, a2alog.DefaultA2ATypeFormatter))

	// 4. Configure the logging interceptor.
	//    It logs every incoming A2A method call and errors. Payloads are logged
	//    when LogPayload is true, useful during development for inspecting messages.
	loggingInterceptor := a2asrv.NewLoggingInterceptor(&a2asrv.LoggingConfig{
		LogPayload: *payload,
	})

	// 5. Wire everything together.
	//    WithLogger sets the base logger for request-scoped structured logs.
	//    WithCallInterceptors attaches the logging interceptor.
	requestHandler := a2asrv.NewHandler(&agentExecutor{},
		a2asrv.WithLogger(logger),
		a2asrv.WithCallInterceptors(loggingInterceptor),
	)

	addr := fmt.Sprintf("http://127.0.0.1:%d/invoke", *port)
	agentCard := &a2a.AgentCard{
		Name:        "Observable Agent",
		Description: "An agent with structured logging and observability",
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(addr, a2a.TransportProtocolJSONRPC),
		},
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
		Capabilities:       a2a.AgentCapabilities{Streaming: true},
		Skills: []a2a.AgentSkill{
			{
				ID:          "hello",
				Name:        "Hello",
				Description: "Responds with a greeting.",
				Tags:        []string{"hello"},
				Examples:    []string{"hi", "hello"},
			},
		},
	}

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("Failed to bind to a port: %v", err)
	}
	logger.Info("server starting", "port", *port, "level", *level)

	mux := http.NewServeMux()
	mux.Handle("/invoke", a2asrv.NewJSONRPCHandler(requestHandler))
	mux.Handle(a2asrv.WellKnownAgentCardPath, a2asrv.NewStaticAgentCardHandler(agentCard))

	if err := http.Serve(listener, mux); err != nil {
		logger.Error("server stopped", slog.String("error", err.Error()))
	}
}
