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
	"os"
	"os/signal"
	"syscall"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2aclient"
	"github.com/a2aproject/a2a-go/a2aclient/agentcard"
)

var cardURL = flag.String("card-url", "http://127.0.0.1:9003", "Base URL of AgentCard server.")

func main() {
	flag.Parse()

	// 1. Handle graceful shutdown
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// 2. Resolve AgentCard
	card, err := agentcard.DefaultResolver.Resolve(ctx, *cardURL)
	if err != nil {
		log.Fatalf("Failed to resolve AgentCard: %v", err)
	}

	// 3. Create Client
	client, err := a2aclient.NewFromCard(ctx, card)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	// 4. Send streaming message
	fmt.Println("Sending request and listening for stream...")
	msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: "Start streaming"})
	params := &a2a.MessageSendParams{Message: msg}

	// We'll track if we've printed a trailing newline to avoid messy output
	newLineNeeded := false

	for event, err := range client.SendStreamingMessage(ctx, params) {
		if err != nil {
			if newLineNeeded {
				fmt.Println()
			}
			log.Fatalf("Stream error: %v", err)
		}

		switch e := event.(type) {
		case *a2a.TaskStatusUpdateEvent:
			if newLineNeeded {
				fmt.Println()
				newLineNeeded = false
			}
			fmt.Printf("[Status Update]: %s", e.Status.State)
			if e.Status.Message != nil {
				for _, p := range e.Status.Message.Parts {
					if tp, ok := p.(a2a.TextPart); ok {
						fmt.Printf(" (%s)", tp.Text)
					}
				}
			}
			fmt.Println()

		case *a2a.TaskArtifactUpdateEvent:
			if e.Artifact != nil {
				for _, p := range e.Artifact.Parts {
					if tp, ok := p.(a2a.TextPart); ok {
						fmt.Print(tp.Text)
						newLineNeeded = true
						_ = os.Stdout.Sync() // Ensure it's printed immediately
					}
				}
			}

		case *a2a.Task:
			if newLineNeeded {
				fmt.Println()
				newLineNeeded = false
			}
			fmt.Printf("[Task Initialized]: %s\n", e.ID)

		default:
			// Ignore other events for this example
		}
	}

	if newLineNeeded {
		fmt.Println()
	}
	fmt.Println("Execution complete.")
}
