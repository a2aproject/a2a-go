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

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"
)

type echoExecutor struct{}

func (e *echoExecutor) Execute(_ context.Context, _ *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		yield(a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("echo")), nil)
	}
}

func (e *echoExecutor) Cancel(_ context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCanceled, nil), nil)
	}
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

func ExampleWellKnownAgentCardPath() {
	fmt.Println("Path:", a2asrv.WellKnownAgentCardPath)
	// Output:
	// Path: /.well-known/agent-card.json
}

func ExampleWithCallInterceptors() {
	executor := &echoExecutor{}
	handler := a2asrv.NewHandler(executor, a2asrv.WithCallInterceptors(&a2asrv.PassthroughCallInterceptor{}))

	fmt.Println("Handler with interceptor:", handler != nil)
	// Output:
	// Handler with interceptor: true
}

func ExamplePassthroughCallInterceptor() {
	var interceptor a2asrv.PassthroughCallInterceptor

	ctx := context.Background()
	_, callCtx := a2asrv.NewCallContext(ctx, nil)

	newCtx, result, err := interceptor.Before(ctx, callCtx, &a2asrv.Request{})
	fmt.Println("Before - context preserved:", newCtx == ctx)
	fmt.Println("Before - result:", result)
	fmt.Println("Before - error:", err)

	afterErr := interceptor.After(ctx, callCtx, &a2asrv.Response{})
	fmt.Println("After - error:", afterErr)
	// Output:
	// Before - context preserved: true
	// Before - result: <nil>
	// Before - error: <nil>
	// After - error: <nil>
}

func ExampleNewCallContext() {
	params := a2asrv.NewServiceParams(map[string][]string{
		"Authorization": {"Bearer token123"},
	})

	_, callCtx := a2asrv.NewCallContext(context.Background(), params)

	auth, ok := callCtx.ServiceParams().Get("authorization")
	fmt.Println("Auth found:", ok)
	fmt.Println("Auth value:", auth[0])
	// Output:
	// Auth found: true
	// Auth value: Bearer token123
}

func ExampleNewServiceParams() {
	params := a2asrv.NewServiceParams(map[string][]string{
		"Content-Type":  {"application/json"},
		"Authorization": {"Bearer secret"},
	})

	ct, ok := params.Get("content-type")
	fmt.Println("Content-Type found:", ok)
	fmt.Println("Content-Type:", ct[0])

	auth, ok := params.Get("AUTHORIZATION")
	fmt.Println("Auth found:", ok)
	fmt.Println("Auth:", auth[0])
	// Output:
	// Content-Type found: true
	// Content-Type: application/json
	// Auth found: true
	// Auth: Bearer secret
}

func ExampleCallContext_Extensions() {
	params := a2asrv.NewServiceParams(map[string][]string{
		a2a.SvcParamExtensions: {"urn:example:ext1"},
	})

	_, callCtx := a2asrv.NewCallContext(context.Background(), params)
	extensions := callCtx.Extensions()

	ext1 := &a2a.AgentExtension{URI: "urn:example:ext1"}
	ext2 := &a2a.AgentExtension{URI: "urn:example:ext2"}

	fmt.Println("ext1 requested:", extensions.Requested(ext1))
	fmt.Println("ext2 requested:", extensions.Requested(ext2))

	extensions.Activate(ext1)
	fmt.Println("ext1 active:", extensions.Active(ext1))
	fmt.Println("Activated URIs:", extensions.ActivatedURIs())
	// Output:
	// ext1 requested: true
	// ext2 requested: false
	// ext1 active: true
	// Activated URIs: [urn:example:ext1]
}
