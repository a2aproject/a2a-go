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

// Package main demonstrates client plugins.
//
// It installs retry, a plugin which implements [a2aclient.Plugin] directly by embedding [a2aclient.PassthroughPlugin].
// It retries GetTask calls which failed with a network error.
//
// Run the server first, then: go run .
package main

import (
	"context"
	"errors"
	"flag"
	"iter"
	"log"
	"net"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aclient/agentcard"
)

var cardURL = flag.String("card-url", "http://127.0.0.1:9001", "Base URL of AgentCard server.")

type retry struct {
	a2aclient.PassthroughPlugin
	attempts int
}

func (retry) Name() string { return "retry" }

func (r retry) WrapCall(next a2aclient.CallHandler) a2aclient.CallHandler {
	return func(ctx context.Context, req a2aclient.Request) iter.Seq2[any, error] {
		if req.Method != "GetTask" {
			return next(ctx, req)
		}
		return func(yield func(any, error) bool) {
			var result any
			var err error
			for attempt := 1; attempt <= r.attempts; attempt++ {
				for result, err = range next(ctx, req) {
				}
				if err == nil || !isTransient(ctx, err) || attempt == r.attempts {
					break
				}
				log.Printf("[retry] %s attempt %d failed: %v", req.Method, attempt, err)
				select {
				case <-time.After(time.Duration(attempt) * 100 * time.Millisecond):
				case <-ctx.Done():
					yield(nil, ctx.Err())
					return
				}
			}
			yield(result, err)
		}
	}
}

// isTransient reports whether a call failed with a network error, for example a refused connection or a timeout.
// Errors returned by the agent, such as a2a.ErrTaskNotFound, and the cancelation of the caller's context are not retried.
func isTransient(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return false
	}
	var netErr net.Error
	return errors.As(err, &netErr)
}

func main() {
	flag.Parse()
	ctx := context.Background()

	card, err := agentcard.DefaultResolver.Resolve(ctx, *cardURL)
	if err != nil {
		log.Fatalf("Failed to resolve an AgentCard: %v", err)
	}

	client, err := a2aclient.NewFromCard(ctx, card, a2aclient.WithPlugins(retry{attempts: 3}))
	if err != nil {
		log.Fatalf("Failed to create a client: %v", err)
	}

	req := &a2a.SendMessageRequest{Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("Hello, plugins"))}
	var taskID a2a.TaskID
	for event, err := range client.SendStreamingMessage(ctx, req) {
		if err != nil {
			log.Fatalf("Failed to send a message: %v", err)
		}
		log.Printf("Received %T", event)
		taskID = event.TaskInfo().TaskID
	}

	task, err := client.GetTask(ctx, &a2a.GetTaskRequest{ID: taskID})
	if err != nil {
		log.Fatalf("Failed to get a task: %v", err)
	}
	log.Printf("Task %s is in state %s", task.ID, task.Status.State)
}
