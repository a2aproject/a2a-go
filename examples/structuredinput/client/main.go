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

// Package main provides an interactive client example responding to structured input
// requests using the Structured Input extension.
//
// Lines which are valid JSON are sent as the structured input, other lines are sent as text.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aclient/agentcard"
	"github.com/a2aproject/a2a-go/v2/a2aext"
	"github.com/a2aproject/a2a-go/v2/a2aext/structuredinput"
)

var (
	cardURL = flag.String("card-url", "http://127.0.0.1:9001", "Base URL of AgentCard server.")
	prompt  = flag.String("prompt", "deploy payment-processing", "Initial message sent to the agent.")
)

func main() {
	flag.Parse()
	ctx := context.Background()

	card, err := agentcard.DefaultResolver.Resolve(ctx, *cardURL)
	if err != nil {
		log.Fatalf("Failed to resolve an AgentCard: %v", err)
	}
	if _, ok := structuredinput.DraftFromCard(card); !ok {
		log.Fatalf("Agent does not support the structured input extension")
	}

	// The activator adds the extension URI to the A2A-Extensions service parameter of every request.
	activator := a2aext.NewActivator(structuredinput.URI)
	client, err := a2aclient.NewFromCard(ctx, card, a2aclient.WithCallInterceptors(activator))
	if err != nil {
		log.Fatalf("Failed to create a client: %v", err)
	}

	task, err := sendMessage(ctx, client, a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart(*prompt)))
	if err != nil {
		log.Fatalf("Failed to send a message: %v", err)
	}

	stdin := bufio.NewScanner(os.Stdin)
	var shownRequest *structuredinput.Request
	for task.Status.State == a2a.TaskStateInputRequired {
		req, err := structuredinput.PendingRequest(task)
		if err != nil {
			log.Fatalf("Failed to parse the input request: %v", err)
		}
		if req == nil {
			log.Fatalf("Agent requested input without a schema")
		}
		// A repeated input request keeps its ID, so the schema is printed only for new requests.
		showSchema := shownRequest == nil || shownRequest.ID != req.ID
		if err := printInputRequest(task.Status.Message, req, showSchema); err != nil {
			log.Fatalf("Failed to print the input request: %v", err)
		}
		shownRequest = req

		fmt.Print("> ")
		if !stdin.Scan() {
			if err := stdin.Err(); err != nil {
				log.Fatalf("Failed to read input: %v", err)
			}
			return
		}

		msg, err := newContinuation(task, req, strings.TrimSpace(stdin.Text()))
		if err != nil {
			log.Fatalf("Failed to create a message: %v", err)
		}
		if task, err = sendMessage(ctx, client, msg); err != nil {
			log.Fatalf("Failed to send a message: %v", err)
		}
	}

	fmt.Printf("Task %s: %s\n", task.Status.State, messageText(task.Status.Message))
}

func sendMessage(ctx context.Context, client *a2aclient.Client, msg *a2a.Message) (*a2a.Task, error) {
	resp, err := client.SendMessage(ctx, &a2a.SendMessageRequest{Message: msg})
	if err != nil {
		return nil, err
	}
	task, ok := resp.(*a2a.Task)
	if !ok {
		return nil, fmt.Errorf("expected a task, got %T", resp)
	}
	return task, nil
}

// newContinuation sends lines which are valid JSON as the structured input and other lines as text,
// for example to ask the agent a clarifying question without leaving the validation loop.
func newContinuation(task *a2a.Task, req *structuredinput.Request, line string) (*a2a.Message, error) {
	var data any
	if err := json.Unmarshal([]byte(line), &data); err != nil {
		return a2a.NewMessageForTask(a2a.MessageRoleUser, task, a2a.NewTextPart(line)), nil
	}
	return structuredinput.NewResponse(task, req, data)
}

func printInputRequest(msg *a2a.Message, req *structuredinput.Request, showSchema bool) error {
	for _, part := range msg.Parts {
		if _, ok := structuredinput.GetPartMetadata(part); ok {
			continue
		}
		if text := part.Text(); text != "" {
			fmt.Println(text)
			continue
		}
		if data := part.Data(); data != nil {
			if err := printJSON(data); err != nil {
				return err
			}
		}
	}
	if showSchema {
		if err := printJSON(req.Schema); err != nil {
			return err
		}
	}

	output, err := structuredinput.ParseValidationOutput(msg)
	if err != nil {
		return err
	}
	if output == nil {
		return nil
	}
	fmt.Println("Validation errors:")
	for _, unit := range output.Errors {
		fmt.Printf("  - %s\n", unit.Error)
	}
	return nil
}

func printJSON(v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(raw))
	return nil
}

func messageText(msg *a2a.Message) string {
	if msg == nil {
		return ""
	}
	var texts []string
	for _, part := range msg.Parts {
		if text := part.Text(); text != "" {
			texts = append(texts, text)
		}
	}
	return strings.Join(texts, " ")
}
