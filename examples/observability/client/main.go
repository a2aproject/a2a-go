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

// Package main demonstrates how to configure observability for an A2A client.
//
// It shows:
//   - Setting up a structured logger with a custom A2A type formatter
//   - Attaching the logging interceptor for outgoing call visibility
//   - Attaching the logger to context so the SDK logs use it
//
// Run with: go run . -card-url http://127.0.0.1:9001
package main

import (
	"context"
	"flag"
	"log"
	"log/slog"
	"os"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aclient/agentcard"
	a2alog "github.com/a2aproject/a2a-go/v2/log"
)

var cardURL = flag.String("card-url", "http://127.0.0.1:9001", "Base URL of AgentCard server.")

func main() {
	flag.Parse()

	// 1. Create a structured logger for the client.
	//    The a2alog.Handler wrapper applies DefaultA2ATypeFormatter so that
	//    A2A types logged via slog.Any are shown with only key identifiers.
	jsonHandler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	logger := slog.New(a2alog.AttachFormatter(jsonHandler, a2alog.DefaultA2ATypeFormatter))

	// 2. Attach the logger to context so the SDK log calls pick it up.
	//    All internal SDK operations (transport failures, retries, etc.)
	//    will use this logger when logging via the log package.
	ctx := a2alog.AttachLogger(context.Background(), logger)

	// 3. Configure the client logging interceptor.
	//    It logs the start of every outgoing call and any errors returned.
	loggingInterceptor := a2aclient.NewLoggingInterceptor(&a2aclient.LoggingConfig{
		LogPayload: true,
	})

	card, err := agentcard.DefaultResolver.Resolve(ctx, *cardURL)
	if err != nil {
		log.Fatalf("Failed to resolve an AgentCard: %v", err)
	}

	// 4. Create the client with the logging interceptor attached.
	client, err := a2aclient.NewFromCard(ctx, card,
		a2aclient.WithCallInterceptors(loggingInterceptor),
	)
	if err != nil {
		log.Fatalf("Failed to create a client: %v", err)
	}

	msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("Hello, world"))
	resp, err := client.SendMessage(ctx, &a2a.SendMessageRequest{Message: msg})
	if err != nil {
		log.Fatalf("Failed to send a message: %v", err)
	}

	logger.Info("server responded", slog.Any("response", resp))
}
