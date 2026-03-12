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

package a2asrv_test

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

type echoExecutor struct {
	ExecuteFn func(context.Context, *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error]
}

func (e *echoExecutor) Execute(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	if e.ExecuteFn != nil {
		return e.ExecuteFn(ctx, execCtx)
	}
	return func(yield func(a2a.Event, error) bool) {
		yield(a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("echo")), nil)
	}
}

func (e *echoExecutor) Cancel(_ context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCanceled, nil), nil)
	}
}

type testInterceptor struct {
	BeforeFn func(ctx context.Context, callCtx *a2asrv.CallContext, req *a2asrv.Request) (context.Context, any, error)
	AfterFn  func(ctx context.Context, callCtx *a2asrv.CallContext, resp *a2asrv.Response) error
}

func (ti *testInterceptor) Before(ctx context.Context, callCtx *a2asrv.CallContext, req *a2asrv.Request) (context.Context, any, error) {
	if ti.BeforeFn != nil {
		return ti.BeforeFn(ctx, callCtx, req)
	}
	return ctx, nil, nil
}

func (ti *testInterceptor) After(ctx context.Context, callCtx *a2asrv.CallContext, resp *a2asrv.Response) error {
	if ti.AfterFn != nil {
		return ti.AfterFn(ctx, callCtx, resp)
	}
	return nil
}

func ExampleNewHandler() {
	executor := &echoExecutor{}
	handler := a2asrv.NewHandler(executor)

	fmt.Println("Handler created:", handler != nil)
	// Output:
	// Handler created: true
}

func ExampleNewHandler_withOptions() {
	executor := &echoExecutor{}
	handler := a2asrv.NewHandler(
		executor,
		a2asrv.WithExtendedAgentCard(&a2a.AgentCard{Name: "Extended Agent"}),
		a2asrv.WithCallInterceptors(&a2asrv.PassthroughCallInterceptor{}),
	)

	fmt.Println("Handler with options created:", handler != nil)
	// Output:
	// Handler with options created: true
}

func ExampleNewJSONRPCHandler() {
	executor := &echoExecutor{}
	handler := a2asrv.NewHandler(executor)
	jsonrpcHandler := a2asrv.NewJSONRPCHandler(handler)

	mux := http.NewServeMux()
	mux.Handle("/", jsonrpcHandler)

	fmt.Println("JSON-RPC handler registered:", jsonrpcHandler != nil)
	// Output:
	// JSON-RPC handler registered: true
}

func ExampleNewStaticAgentCardHandler() {
	card := &a2a.AgentCard{
		Name:    "Echo Agent",
		Version: "1.0.0",
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface("http://localhost:8080", a2a.TransportProtocolJSONRPC),
		},
	}
	handler := a2asrv.NewStaticAgentCardHandler(card)

	server := httptest.NewServer(handler)
	defer server.Close()

	resp, err := http.Get(server.URL)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		fmt.Println("Error:", err)
		return
	}

	fmt.Println("Name:", result["name"])
	fmt.Println("Version:", result["version"])
	fmt.Println("Content-Type:", resp.Header.Get("Content-Type"))
	// Output:
	// Name: Echo Agent
	// Version: 1.0.0
	// Content-Type: application/json
}

func ExampleNewAgentCardHandler() {
	producer := a2asrv.AgentCardProducerFn(func(_ context.Context) (*a2a.AgentCard, error) {
		return &a2a.AgentCard{
			Name:    "Dynamic Agent",
			Version: "2.0.0",
			SupportedInterfaces: []*a2a.AgentInterface{
				a2a.NewAgentInterface("http://localhost:8080", a2a.TransportProtocolJSONRPC),
			},
		}, nil
	})
	handler := a2asrv.NewAgentCardHandler(producer)

	server := httptest.NewServer(handler)
	defer server.Close()

	resp, err := http.Get(server.URL)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		fmt.Println("Error:", err)
		return
	}

	fmt.Println("Name:", result["name"])
	fmt.Println("Version:", result["version"])
	// Output:
	// Name: Dynamic Agent
	// Version: 2.0.0
}

func ExamplePassthroughCallInterceptor() {
	type myInterceptor struct {
		a2asrv.PassthroughCallInterceptor
	}

	handler := a2asrv.NewHandler(&echoExecutor{}, a2asrv.WithCallInterceptors(myInterceptor{}))
	fmt.Println("Handler created:", handler != nil)
	// Output:
	// Handler created: true
}

func ExampleUser() {
	authenticate := func(_ string) string { return "user" }

	interceptor := &testInterceptor{
		BeforeFn: func(ctx context.Context, callCtx *a2asrv.CallContext, req *a2asrv.Request) (context.Context, any, error) {
			if auth, ok := callCtx.ServiceParams().Get("authorization"); ok && len(auth) > 0 && strings.HasPrefix(auth[0], "Bearer ") {
				if name := authenticate(auth[0]); name != "" {
					callCtx.User = a2asrv.NewAuthenticatedUser(name, nil)
				}
			}
			return ctx, nil, nil
		},
	}

	executor := &echoExecutor{
		ExecuteFn: func(_ context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
			return func(yield func(a2a.Event, error) bool) {
				fmt.Println("Auth found:", execCtx.User.Name)
				yield(a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("echo")), nil)
			}
		},
	}

	ctx, _ := a2asrv.NewCallContext(context.Background(), a2asrv.NewServiceParams(map[string][]string{
		"Authorization": {"Bearer token"},
	}))
	handler := a2asrv.NewHandler(executor, a2asrv.WithCallInterceptors(interceptor))
	_, err := handler.SendMessage(ctx, &a2a.SendMessageRequest{
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("echo")),
	})
	fmt.Println("Error:", err)
	// Output:
	// Auth found: user
	// Error: <nil>
}

func ExampleAgentExecutor() {
	executor := &echoExecutor{
		ExecuteFn: func(_ context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
			return func(yield func(a2a.Event, error) bool) {
				if !yield(a2a.NewSubmittedTask(execCtx, execCtx.Message), nil) {
					return
				}

				if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateWorking, nil), nil) {
					return
				}

				if !yield(a2a.NewArtifactEvent(execCtx, a2a.NewTextPart("generated output")), nil) {
					return
				}

				yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCompleted, nil), nil)
			}
		},
	}

	handler := a2asrv.NewHandler(executor)

	msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("generate something"))
	result, err := handler.SendMessage(context.Background(), &a2a.SendMessageRequest{Message: msg})
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	task, ok := result.(*a2a.Task)
	if !ok {
		fmt.Println("Expected task result")
		return
	}

	fmt.Println("State:", task.Status.State)
	fmt.Println("Artifacts:", len(task.Artifacts))
	// Output:
	// State: TASK_STATE_COMPLETED
	// Artifacts: 1
}

func ExampleNewHandler_fullServer() {
	executor := &echoExecutor{}
	handler := a2asrv.NewHandler(executor)

	card := &a2a.AgentCard{
		Name:    "My Agent",
		Version: "1.0.0",
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface("http://localhost:8080", a2a.TransportProtocolJSONRPC),
		},
	}

	mux := http.NewServeMux()
	mux.Handle(a2asrv.WellKnownAgentCardPath, a2asrv.NewStaticAgentCardHandler(card))
	mux.Handle("/", a2asrv.NewJSONRPCHandler(handler))

	fmt.Println("Agent card path:", a2asrv.WellKnownAgentCardPath)
	fmt.Println("Server ready")
	// Output:
	// Agent card path: /.well-known/agent-card.json
	// Server ready
}

func ExampleServiceParams() {
	var capturedHeader string

	interceptor := &testInterceptor{
		BeforeFn: func(ctx context.Context, callCtx *a2asrv.CallContext, _ *a2asrv.Request) (context.Context, any, error) {
			if vals, ok := callCtx.ServiceParams().Get("x-custom-header"); ok && len(vals) > 0 {
				capturedHeader = vals[0]
			}
			return ctx, nil, nil
		},
	}

	executor := &echoExecutor{}
	handler := a2asrv.NewHandler(executor, a2asrv.WithCallInterceptors(interceptor))
	restHandler := a2asrv.NewRESTHandler(handler)

	server := httptest.NewServer(restHandler)
	defer server.Close()

	req, _ := http.NewRequest("GET", server.URL+"/tasks/task-123", nil)
	req.Header.Set("X-Custom-Header", "my-value")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	// The task won't be found, but the interceptor still captures the header.
	fmt.Println("Header in ServiceParams:", capturedHeader)
	// Output:
	// Header in ServiceParams: my-value
}
