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

package e2e_test

import (
	"context"
	"errors"
	"iter"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

func firstOutputHTTPClient(t *testing.T, handler a2asrv.RequestHandler, protocol a2a.TransportProtocol) *a2aclient.Client {
	t.Helper()
	transport := a2asrv.NewJSONRPCHandler(handler)
	if protocol == a2a.TransportProtocolHTTPJSON {
		transport = a2asrv.NewRESTHandler(handler)
	}
	server := httptest.NewServer(transport)
	t.Cleanup(server.Close)
	card := &a2a.AgentCard{
		SupportedInterfaces: []*a2a.AgentInterface{a2a.NewAgentInterface(server.URL, protocol)},
		Capabilities:        a2a.AgentCapabilities{Streaming: true},
	}
	client, err := a2aclient.NewFromCard(t.Context(), card, a2aclient.WithJSONRPCTransport(server.Client()), a2aclient.WithRESTTransport(server.Client()))
	if err != nil {
		t.Fatalf("a2aclient.NewFromCard() error = %v, want nil", err)
	}
	return client
}

func TestAgentFirstOutputTimeout_HTTP(t *testing.T) {
	t.Parallel()
	for _, protocol := range []a2a.TransportProtocol{a2a.TransportProtocolJSONRPC, a2a.TransportProtocolHTTPJSON} {
		for _, output := range []bool{false, true} {
			name := string(protocol) + "/timeout"
			if output {
				name = string(protocol) + "/success"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				const budget = 250 * time.Millisecond
				causes := make(chan error, 1)
				executor := a2asrv.AgentExecutorFunc(func(ctx context.Context, ec *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
					return func(yield func(a2a.Event, error) bool) {
						defer func() { causes <- context.Cause(ctx) }()
						if !yield(a2a.NewSubmittedTask(ec, ec.Message), nil) {
							return
						}
						if output && !yield(a2a.NewArtifactEvent(ec, a2a.NewTextPart("hello")), nil) {
							return
						}
						finish := time.NewTimer(2 * budget)
						defer finish.Stop()
						ticker := time.NewTicker(budget / 10)
						defer ticker.Stop()
						for {
							select {
							case <-ctx.Done():
								yield(nil, context.Cause(ctx))
								return
							case <-finish.C:
								yield(a2a.NewStatusUpdateEvent(ec, a2a.TaskStateCompleted, nil), nil)
								return
							case <-ticker.C:
								if !yield(a2a.NewStatusUpdateEvent(ec, a2a.TaskStateWorking, nil), nil) {
									return
								}
							}
						}
					}
				})
				handler := a2asrv.NewHandler(executor, a2asrv.WithAgentFirstOutputTimeout(budget, matchesArtifact))
				client := firstOutputHTTPClient(t, handler, protocol)
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				var taskID a2a.TaskID
				var state a2a.TaskState
				for event, err := range client.SendStreamingMessage(ctx, &a2a.SendMessageRequest{Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hello"))}) {
					if err != nil {
						t.Fatalf("client.SendStreamingMessage() error = %v, want nil", err)
					}
					switch e := event.(type) {
					case *a2a.Task:
						taskID, state = e.ID, e.Status.State
					case *a2a.TaskStatusUpdateEvent:
						state = e.Status.State
					}
				}
				want := a2a.TaskStateFailed
				if output {
					want = a2a.TaskStateCompleted
				}
				if state != want {
					t.Fatalf("stream state = %v, want %v", state, want)
				}
				task, err := client.GetTask(ctx, &a2a.GetTaskRequest{ID: taskID})
				if err != nil || task.Status.State != want {
					t.Fatalf("client.GetTask() = %v, %v, want state %v", task, err, want)
				}
				if cause := <-causes; !output && !errors.Is(cause, a2asrv.ErrAgentFirstOutputTimeout) {
					t.Fatalf("context.Cause() = %v, want first-output timeout", cause)
				}
			})
		}
	}
}
