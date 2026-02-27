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
	"iter"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v1/a2a"
	"github.com/a2aproject/a2a-go/v1/a2aclient"
	"github.com/a2aproject/a2a-go/v1/a2aclient/agentcard"
	"github.com/a2aproject/a2a-go/v1/a2acompat/a2av0"
	"github.com/a2aproject/a2a-go/v1/a2asrv"

	legacya2a "github.com/a2aproject/a2a-go/a2a"
	legacyclient "github.com/a2aproject/a2a-go/a2aclient"
	legacysrv "github.com/a2aproject/a2a-go/a2asrv"
	legacyqueue "github.com/a2aproject/a2a-go/a2asrv/eventqueue"
)

func TestMigration_V1ServerLegacyBackends(t *testing.T) {
	t.Parallel()

	// 1. Initialize legacy components
	legacyExecutor := &testLegacyExecutor{
		executeFn: func(ctx context.Context, reqCtx *legacysrv.RequestContext, q legacyqueue.Queue) error {
			for _, p := range reqCtx.Message.Parts {
				if textPart, ok := p.(legacya2a.TextPart); ok {
					if textPart.Text == "ping" {
						response := &legacya2a.Message{
							Role:   legacya2a.MessageRoleAgent,
							Parts:  legacya2a.ContentParts{legacya2a.TextPart{Text: "pong"}},
							TaskID: reqCtx.TaskID,
						}
						return q.Write(ctx, response)
					}
				}
			}
			return fmt.Errorf("expected ping message")
		},
	}
	legacyStore := &mockLegacyTaskStore{t: t, tasks: make(map[legacya2a.TaskID]*legacya2a.Task)}

	// 2. Wrap them using migration adapters
	executor := a2av0.NewAgentExecutor(legacyExecutor)
	store := a2av0.NewTaskStore(legacyStore)

	// 3. Create v1 handler with adapted backends
	handler := a2asrv.NewHandler(executor, a2asrv.WithTaskStore(store))

	mux := http.NewServeMux()
	ts := httptest.NewServer(mux)
	defer ts.Close()

	card := &a2a.AgentCard{
		Name: "Migration Test Agent",
		SupportedInterfaces: []*a2a.AgentInterface{
			{
				URL:             ts.URL + "/invoke",
				ProtocolBinding: a2a.TransportProtocolJSONRPC,
				ProtocolVersion: a2av0.Version,
			},
		},
	}
	cardProducer := a2av0.NewStaticAgentCardProducer(card)

	mux.Handle("/invoke", a2av0.NewJSONRPCHandler(handler))
	mux.Handle(a2asrv.WellKnownAgentCardPath, a2asrv.NewAgentCardHandler(cardProducer))

	// 5. Use v1 client to call the server
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resolver := agentcard.Resolver{CardParser: a2av0.NewAgentCardParser()}
	resolvedCard, err := resolver.Resolve(ctx, ts.URL)
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
}

// modifyingLegacyInterceptor modifies request in Before and response in After.
type modifyingLegacyInterceptor struct {
	t *testing.T
}

func (i *modifyingLegacyInterceptor) Before(ctx context.Context, callCtx *legacysrv.CallContext, req *legacysrv.Request) (context.Context, error) {
	if sendParams, ok := req.Payload.(*legacya2a.MessageSendParams); ok {
		for i, p := range sendParams.Message.Parts {
			if textPart, ok := p.(legacya2a.TextPart); ok {
				if textPart.Text == "ping" {
					sendParams.Message.Parts[i] = legacya2a.TextPart{Text: "ping-modified"}
				}
			}
		}
	}
	return ctx, nil
}

func (i *modifyingLegacyInterceptor) After(ctx context.Context, callCtx *legacysrv.CallContext, resp *legacysrv.Response) error {
	if msg, ok := resp.Payload.(*legacya2a.Message); ok {
		for i, p := range msg.Parts {
			if textPart, ok := p.(legacya2a.TextPart); ok {
				if textPart.Text == "pong" {
					msg.Parts[i] = legacya2a.TextPart{Text: "pong-modified"}
				}
			}
		}
	}
	return ctx.Err()
}

func TestMigration_InterceptorModifications(t *testing.T) {
	t.Parallel()

	// 1. Initialize legacy components
	// The executor now expects "ping-modified"
	legacyExecutor := &testLegacyExecutor{
		executeFn: func(ctx context.Context, reqCtx *legacysrv.RequestContext, q legacyqueue.Queue) error {
			found := false
			for _, p := range reqCtx.Message.Parts {
				if tp, ok := p.(legacya2a.TextPart); ok && tp.Text == "ping-modified" {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("expected ping-modified, got %+v", reqCtx.Message.Parts)
			}

			return q.Write(ctx, &legacya2a.Message{
				Role:   legacya2a.MessageRoleAgent,
				Parts:  legacya2a.ContentParts{legacya2a.TextPart{Text: "pong"}},
				TaskID: reqCtx.TaskID,
			})
		},
	}
	executor := a2av0.NewAgentExecutor(legacyExecutor)

	// 2. Create v1 interceptor from modifying legacy interceptor
	interceptor := &modifyingLegacyInterceptor{t: t}
	v1Interceptor := a2av0.NewServerInterceptor(interceptor)

	// 3. Create v1 handler
	handler := a2asrv.NewHandler(executor,
		a2asrv.WithCallInterceptors(v1Interceptor),
	)

	mux := http.NewServeMux()
	mux.Handle("/invoke", a2av0.NewJSONRPCHandler(handler))
	ts := httptest.NewServer(mux)
	defer ts.Close()

	// 5. Use v1 client
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	jsonCompatFactory := a2av0.NewJSONRPCTransportFactory(a2av0.JSONRPCTransportConfig{})
	factory := a2aclient.NewFactory(
		a2aclient.WithCompatTransport(a2av0.Version, a2a.TransportProtocolJSONRPC, jsonCompatFactory),
	)
	client, err := factory.CreateFromEndpoints(ctx, []*a2a.AgentInterface{
		{
			URL:             ts.URL + "/invoke",
			ProtocolBinding: a2a.TransportProtocolJSONRPC,
			ProtocolVersion: a2av0.Version,
		},
	})
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

	msg := resp.(*a2a.Message)
	foundPongModified := false
	for _, p := range msg.Parts {
		if p.Text() == "pong-modified" {
			foundPongModified = true
			break
		}
	}
	if !foundPongModified {
		t.Errorf("wanted pong-modified, got %v", msg.Parts)
	}
}

type legacyModifyingClientInterceptor struct {
	t *testing.T
}

func (i *legacyModifyingClientInterceptor) Before(ctx context.Context, req *legacyclient.Request) (context.Context, error) {
	if req.Meta == nil {
		req.Meta = make(map[string][]string)
	}
	req.Meta["X-Modified"] = []string{"true"}
	if sendParams, ok := req.Payload.(*legacya2a.MessageSendParams); ok {
		for i, p := range sendParams.Message.Parts {
			if textPart, ok := p.(legacya2a.TextPart); ok {
				if textPart.Text == "ping" {
					sendParams.Message.Parts[i] = legacya2a.TextPart{Text: "ping-client-modified"}
				}
			}
		}
	}
	return ctx, nil
}

func (i *legacyModifyingClientInterceptor) After(ctx context.Context, resp *legacyclient.Response) error {
	if msg, ok := resp.Payload.(*legacya2a.Message); ok {
		for i, p := range msg.Parts {
			if textPart, ok := p.(legacya2a.TextPart); ok {
				if textPart.Text == "pong" {
					msg.Parts[i] = legacya2a.TextPart{Text: "pong-client-modified"}
				}
			}
		}
	}
	return nil
}

func TestMigration_ClientInterceptorModifications(t *testing.T) {
	t.Parallel()

	// 1. Setup server that checks for X-Modified
	executor := &testExecutor{
		executeFn: func(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
			return func(yield func(a2a.Event, error) bool) {
				callCtx, ok := a2asrv.CallContextFrom(ctx)
				if !ok {
					yield(nil, fmt.Errorf("no call context"))
					return
				}
				modified, ok := callCtx.ServiceParams().Get("X-Modified")
				if !ok || len(modified) == 0 || modified[0] != "true" {
					yield(nil, fmt.Errorf("X-Modified not set or not true: %v", modified))
					return
				}

				found := false
				for _, p := range execCtx.Message.Parts {
					if p.Text() == "ping-client-modified" {
						found = true
						break
					}
				}
				if !found {
					yield(nil, fmt.Errorf("expected ping-client-modified, got %+v", execCtx.Message.Parts))
					return
				}

				yield(a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("pong")), nil)
			}
		},
	}
	handler := a2asrv.NewHandler(executor)

	mux := http.NewServeMux()
	mux.Handle("/invoke", a2av0.NewJSONRPCHandler(handler))
	ts := httptest.NewServer(mux)
	defer ts.Close()

	// 2. Setup v1 client with legacy interceptor
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	legacyInterceptor := &legacyModifyingClientInterceptor{t: t}
	v1Interceptor := a2av0.NewClientInterceptor(legacyInterceptor)

	jsonCompatFactory := a2av0.NewJSONRPCTransportFactory(a2av0.JSONRPCTransportConfig{})
	factory := a2aclient.NewFactory(
		a2aclient.WithCompatTransport(a2av0.Version, a2a.TransportProtocolJSONRPC, jsonCompatFactory),
		a2aclient.WithCallInterceptors(v1Interceptor),
	)
	client, err := factory.CreateFromEndpoints(ctx, []*a2a.AgentInterface{
		{
			URL:             ts.URL + "/invoke",
			ProtocolBinding: a2a.TransportProtocolJSONRPC,
			ProtocolVersion: a2av0.Version,
		},
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	// 3. Send message
	req := &a2a.SendMessageRequest{
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("ping")),
	}
	resp, err := client.SendMessage(ctx, req)
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}

	msg := resp.(*a2a.Message)
	foundPongModified := false
	for _, p := range msg.Parts {
		if p.Text() == "pong-client-modified" {
			foundPongModified = true
			break
		}
	}
	if !foundPongModified {
		t.Errorf("wanted pong-client-modified, got %v", msg.Parts)
	}
}

// testLegacyExecutor implements legacy legacysrv.AgentExecutor interface.
type testLegacyExecutor struct {
	executeFn func(ctx context.Context, reqCtx *legacysrv.RequestContext, q legacyqueue.Queue) error
}

func (e *testLegacyExecutor) Execute(ctx context.Context, reqCtx *legacysrv.RequestContext, q legacyqueue.Queue) error {
	return e.executeFn(ctx, reqCtx, q)
}

func (e *testLegacyExecutor) Cancel(ctx context.Context, reqCtx *legacysrv.RequestContext, q legacyqueue.Queue) error {
	return nil
}

// testExecutor implements a2asrv.AgentExecutor interface.
type testExecutor struct {
	executeFn func(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error]
}

func (e *testExecutor) Execute(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return e.executeFn(ctx, execCtx)
}

func (e *testExecutor) Cancel(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return nil
}

// mockLegacyTaskStore implements legacy legacysrv.TaskStore interface.
type mockLegacyTaskStore struct {
	t     *testing.T
	tasks map[legacya2a.TaskID]*legacya2a.Task
}

func (s *mockLegacyTaskStore) Save(ctx context.Context, task *legacya2a.Task, event legacya2a.Event, prev legacya2a.TaskVersion) (legacya2a.TaskVersion, error) {
	if s.tasks == nil {
		s.tasks = make(map[legacya2a.TaskID]*legacya2a.Task)
	}
	s.tasks[task.ID] = task
	return 1, nil
}

func (s *mockLegacyTaskStore) Get(ctx context.Context, taskID legacya2a.TaskID) (*legacya2a.Task, legacya2a.TaskVersion, error) {
	task, ok := s.tasks[taskID]
	if !ok {
		return nil, 0, legacya2a.ErrTaskNotFound
	}
	return task, 1, nil
}

func (s *mockLegacyTaskStore) List(ctx context.Context, req *legacya2a.ListTasksRequest) (*legacya2a.ListTasksResponse, error) {
	return &legacya2a.ListTasksResponse{}, nil
}
