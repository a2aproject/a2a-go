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

package compat_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v1/a2a"
	"github.com/a2aproject/a2a-go/v1/a2aclient"
	"github.com/a2aproject/a2a-go/v1/a2aclient/agentcard"
	"github.com/a2aproject/a2a-go/v1/a2acompat/a2av0"
	"github.com/a2aproject/a2a-go/v1/a2asrv"

	legacya2a "github.com/a2aproject/a2a-go/a2a"
	legacysrv "github.com/a2aproject/a2a-go/a2asrv"
	legacyqueue "github.com/a2aproject/a2a-go/a2asrv/eventqueue"
)

// mockLegacyAgentExecutor implements legacy legacysrv.AgentExecutor interface.
type mockLegacyAgentExecutor struct {
	t *testing.T
}

func (e *mockLegacyAgentExecutor) Execute(ctx context.Context, reqCtx *legacysrv.RequestContext, q legacyqueue.Queue) error {
	e.t.Logf("mockLegacyAgentExecutor.Execute called with message: %+v", reqCtx.Message)
	for i, p := range reqCtx.Message.Parts {
		e.t.Logf("Part %d: %T %+v", i, p, p)
		if textPart, ok := p.(legacya2a.TextPart); ok {
			e.t.Logf("TextPart: %q", textPart.Text)
			if textPart.Text == "ping" {
				response := &legacya2a.Message{
					Role: legacya2a.MessageRoleAgent,
					Parts: legacya2a.ContentParts{
						legacya2a.TextPart{Text: "pong"},
					},
					TaskID: reqCtx.TaskID,
				}
				e.t.Logf("Writing pong response")
				return q.Write(ctx, response)
			}
		}
	}
	e.t.Logf("No ping found in message parts")
	return fmt.Errorf("expected ping message")
}

func (e *mockLegacyAgentExecutor) Cancel(ctx context.Context, reqCtx *legacysrv.RequestContext, q legacyqueue.Queue) error {
	return nil
}

// mockLegacyTaskStore implements legacy legacysrv.TaskStore interface.
type mockLegacyTaskStore struct {
	t     *testing.T
	tasks map[legacya2a.TaskID]*legacya2a.Task
}

func (s *mockLegacyTaskStore) Save(ctx context.Context, task *legacya2a.Task, event legacya2a.Event, prev legacya2a.TaskVersion) (legacya2a.TaskVersion, error) {
	s.t.Logf("mockLegacyTaskStore.Save called for task %v", task.ID)
	if s.tasks == nil {
		s.tasks = make(map[legacya2a.TaskID]*legacya2a.Task)
	}
	s.tasks[task.ID] = task
	return 1, nil
}

func (s *mockLegacyTaskStore) Get(ctx context.Context, taskID legacya2a.TaskID) (*legacya2a.Task, legacya2a.TaskVersion, error) {
	s.t.Logf("mockLegacyTaskStore.Get called for task %v", taskID)
	task, ok := s.tasks[taskID]
	if !ok {
		return nil, 0, legacya2a.ErrTaskNotFound
	}
	return task, 1, nil
}

func (s *mockLegacyTaskStore) List(ctx context.Context, req *legacya2a.ListTasksRequest) (*legacya2a.ListTasksResponse, error) {
	return &legacya2a.ListTasksResponse{}, nil
}

// mockLegacyInterceptor implements legacy legacysrv.CallInterceptor interface.
type mockLegacyInterceptor struct {
	t      *testing.T
	called bool
}

func (i *mockLegacyInterceptor) Before(ctx context.Context, callCtx *legacysrv.CallContext, req *legacysrv.Request) (context.Context, error) {
	i.t.Logf("mockLegacyInterceptor.Before called")
	i.called = true
	return ctx, nil
}

func (i *mockLegacyInterceptor) After(ctx context.Context, callCtx *legacysrv.CallContext, resp *legacysrv.Response) error {
	i.t.Logf("mockLegacyInterceptor.After called")
	return nil
}

func TestMigration_V1ServerLegacyBackends(t *testing.T) {
	t.Parallel()

	// 1. Initialize legacy components
	legacyExecutor := &mockLegacyAgentExecutor{t: t}
	legacyStore := &mockLegacyTaskStore{t: t, tasks: make(map[legacya2a.TaskID]*legacya2a.Task)}

	// 2. Wrap them using migration adapters
	executor := a2av0.NewAgentExecutor(legacyExecutor)
	store := a2av0.NewTaskStore(legacyStore)

	// 3. Create v1 interceptor from legacy interceptor
	interceptor := &mockLegacyInterceptor{t: t}
	v1Interceptor := a2av0.NewServerInterceptor(interceptor)

	// 4. Create v1 handler with adapted backends
	handler := a2asrv.NewHandler(executor,
		a2asrv.WithTaskStore(store),
		a2asrv.WithCallInterceptors(v1Interceptor),
	)

	// 5. Start v1 server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port

	card := &a2a.AgentCard{
		Name: "Migration Test Agent",
		SupportedInterfaces: []*a2a.AgentInterface{
			{
				URL:             fmt.Sprintf("http://127.0.0.1:%d/invoke", port),
				ProtocolBinding: a2a.TransportProtocolJSONRPC,
				ProtocolVersion: a2av0.Version,
			},
		},
	}
	cardProducer := a2av0.NewStaticAgentCardProducer(card)

	mux := http.NewServeMux()
	mux.Handle("/invoke", a2av0.NewJSONRPCHandler(handler, a2av0.JSONRPCHandlerConfig{}))
	mux.Handle(a2asrv.WellKnownAgentCardPath, a2asrv.NewAgentCardHandler(cardProducer))

	srv := &http.Server{Handler: mux}
	go func() {
		_ = srv.Serve(listener)
	}()
	defer srv.Shutdown(context.Background())

	// 6. Use v1 client to call the server
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resolver := agentcard.Resolver{CardParser: a2av0.NewAgentCardParser()}
	resolvedCard, err := resolver.Resolve(ctx, fmt.Sprintf("http://127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("failed to resolve card: %v", err)
	}

	jsonCompatFactory := a2av0.NewJSONRPCTransportFactory(a2av0.JSONRPCTransportConfig{})
	factory := a2aclient.NewFactory(
		a2aclient.WithCompatTransport(a2av0.Version, a2a.TransportProtocolJSONRPC, jsonCompatFactory),
	)
	client, err := factory.CreateFromCard(ctx, resolvedCard)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	req := &a2a.SendMessageRequest{
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("ping")),
	}
	resp, err := client.SendMessage(ctx, req)
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}

	msg, ok := resp.(*a2a.Message)
	if !ok {
		t.Fatalf("expected message, got %T", resp)
	}

	foundPong := false
	for _, p := range msg.Parts {
		if p.Text() == "pong" {
			foundPong = true
			break
		}
	}
	if !foundPong {
		t.Errorf("wanted pong, got %v", msg.Parts)
	}

	if !interceptor.called {
		t.Errorf("interceptor was not called")
	}
}
