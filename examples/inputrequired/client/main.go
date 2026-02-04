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
	"flag"
	"fmt"
	"log"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2aclient"
	"github.com/a2aproject/a2a-go/a2aclient/agentcard"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var cardURL = flag.String("card-url", "http://127.0.0.1:9002", "Base URL of AgentCard server.")

func main() {
	flag.Parse()
	ctx := context.Background()

	// Resolve AgentCard
	card, err := agentcard.DefaultResolver.Resolve(ctx, *cardURL)
	if err != nil {
		log.Fatalf("Failed to resolve AgentCard: %v", err)
	}

	// Setup client
	withInsecureGRPC := a2aclient.WithGRPCTransport(grpc.WithTransportCredentials(insecure.NewCredentials()))
	client, err := a2aclient.NewFromCard(ctx, card, withInsecureGRPC)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	// 1. Send initial message
	fmt.Println("Sending initial message...")
	msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: "Start task"})
	resp, err := client.SendMessage(ctx, &a2a.MessageSendParams{Message: msg})
	if err != nil {
		log.Fatalf("Failed to send message: %v", err)
	}

	task, ok := resp.(*a2a.Task)
	if !ok {
		log.Fatalf("Expected *a2a.Task, got %T", resp)
	}

	log.Printf("Response state: %s", task.Status.State)

	if task.Status.State != a2a.TaskStateInputRequired {
		log.Printf("Task Output: %+v", task)
		log.Fatalf("Expected state %s, got %s", a2a.TaskStateInputRequired, task.Status.State)
	}

	// 2. Provide input
	fmt.Println("Providing input...")
	// We need to use the info from the returned task to reply
	inputMsg := a2a.NewMessageForTask(a2a.MessageRoleUser, task, a2a.TextPart{Text: "Here is the details"})
	resp2, err := client.SendMessage(ctx, &a2a.MessageSendParams{Message: inputMsg})
	if err != nil {
		log.Fatalf("Failed to send input: %v", err)
	}

	task2, ok := resp2.(*a2a.Task)
	if !ok {
		log.Fatalf("Expected *a2a.Task, got %T", resp2)
	}
	log.Printf("Final Response state: %s", task2.Status.State)

	if task2.Status.State != a2a.TaskStateCompleted {
		log.Printf("Task Output: %+v", task2)
		log.Fatalf("Expected state %s, got %s", a2a.TaskStateCompleted, task2.Status.State)
	}

	fmt.Println("Task completed successfully!")
}
