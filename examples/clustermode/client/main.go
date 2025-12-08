// Copyright 2025 The A2A Authors
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
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2aclient"
)

var (
	cmd    = flag.String("cmd", "", "Command to execute: send, cancel, subscribe")
	text   = flag.String("text", "", "Text payload for send command")
	taskID = flag.String("task-id", "", "Task ID for cancel and subscribe commands")
	server = flag.String("server", "http://localhost:8080", "Server URL")
)

func main() {
	flag.Parse()

	if *cmd == "" {
		flag.Usage()
		os.Exit(1)
	}

	ctx := context.Background()
	card := &a2a.AgentCard{
		URL:                fmt.Sprintf("%s/invoke", *server),
		PreferredTransport: a2a.TransportProtocolJSONRPC,
		Capabilities:       a2a.AgentCapabilities{Streaming: true},
	}

	httpClient := &http.Client{Timeout: 5 * time.Minute}
	client, err := a2aclient.NewFromCard(
		ctx,
		card,
		a2aclient.WithJSONRPCTransport(httpClient),
	)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	var cmdErr error
	switch *cmd {
	case "send":
		if *text == "" {
			log.Fatal("Text is required for send command")
		}
		cmdErr = send(ctx, client, *text)
	case "cancel":
		if *taskID == "" {
			log.Fatal("Task ID is required for cancel command")
		}
		cmdErr = cancel(ctx, client, *taskID)
	case "subscribe":
		if *taskID == "" {
			log.Fatal("Task ID is required for subscribe command")
		}
		cmdErr = subscribe(ctx, client, a2a.TaskID(*taskID))
	default:
		cmdErr = fmt.Errorf("unknown command: %s", *cmd)
	}
	if cmdErr != nil {
		log.Fatalf("Failed to execute command: %v", cmdErr)
	}
}

func send(ctx context.Context, client *a2aclient.Client, text string) error {
	msg := &a2a.MessageSendParams{
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: text}),
	}
	final := false
	taskID := a2a.TaskID("")
	for event, err := range client.SendStreamingMessage(ctx, msg) {
		if err != nil {
			return fmt.Errorf("error receiving event: %w", err)
		}
		if err := printEvent(event); err != nil {
			return fmt.Errorf("error printing event: %w", err)
		}
		if ev, ok := event.(*a2a.Task); ok {
			taskID = ev.ID
		}
		if ev, ok := event.(*a2a.TaskStatusUpdateEvent); ok {
			final = ev.Status.State.Terminal()
		}
	}
	if !final && taskID != "" {
		return subscribe(ctx, client, taskID)
	}
	return nil
}

func cancel(ctx context.Context, client *a2aclient.Client, id string) error {
	task, err := client.CancelTask(ctx, &a2a.TaskIDParams{ID: a2a.TaskID(id)})
	if err != nil {
		return fmt.Errorf("failed to cancel task: %w", err)
	}
	fmt.Printf("Task cancelled. New state: %s\n", task.Status.State)
	return nil
}

func subscribe(ctx context.Context, client *a2aclient.Client, id a2a.TaskID) error {
	final := false
	for !final {
		for event, err := range client.ResubscribeToTask(ctx, &a2a.TaskIDParams{ID: id}) {
			if err != nil {
				return fmt.Errorf("error receiving event: %w", err)
			}
			if err := printEvent(event); err != nil {
				return fmt.Errorf("error printing event: %w", err)
			}
			if ev, ok := event.(*a2a.TaskStatusUpdateEvent); ok {
				final = ev.Status.State.Terminal()
			}
		}
	}
	return nil
}

func printEvent(event a2a.Event) error {
	switch v := event.(type) {
	case *a2a.TaskArtifactUpdateEvent:
		fmt.Printf("[update]: %s\n", v.Artifact.Parts[0].(a2a.TextPart).Text)

	case *a2a.TaskStatusUpdateEvent:
		fmt.Printf("[state=%q]: %s\n", v.Status.State, v.Status.Message.Parts[0].(a2a.TextPart).Text)

	default:
		data, err := json.MarshalIndent(event, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal event: %w", err)
		}
		fmt.Println(string(data))
	}
	return nil
}
