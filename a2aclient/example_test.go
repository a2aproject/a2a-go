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

package a2aclient_test

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"net/http"
	"net/http/httptest"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2aclient"
	"github.com/a2aproject/a2a-go/a2aclient/agentcard"
	"github.com/a2aproject/a2a-go/a2asrv"
)

type testExecutor struct{}

func (e *testExecutor) Execute(_ context.Context, _ *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		yield(a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("hello")), nil)
	}
}

func (e *testExecutor) Cancel(_ context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCanceled, nil), nil)
	}
}

func startTestServer() *httptest.Server {
	handler := a2asrv.NewHandler(&testExecutor{})
	return httptest.NewServer(a2asrv.NewJSONRPCHandler(handler))
}

func ExampleNewFromCard() {
	server := startTestServer()
	defer server.Close()

	card := &a2a.AgentCard{
		Name: "Test Agent",
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(server.URL, a2a.TransportProtocolJSONRPC),
		},
	}

	client, err := a2aclient.NewFromCard(context.Background(), card)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	fmt.Println("Client created:", client != nil)
	// Output:
	// Client created: true
}

func ExampleNewFromEndpoints() {
	server := startTestServer()
	defer server.Close()

	endpoints := []*a2a.AgentInterface{
		a2a.NewAgentInterface(server.URL, a2a.TransportProtocolJSONRPC),
	}

	client, err := a2aclient.NewFromEndpoints(context.Background(), endpoints)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	fmt.Println("Client created:", client != nil)
	// Output:
	// Client created: true
}

func ExampleNewFactory() {
	factory := a2aclient.NewFactory(
		a2aclient.WithJSONRPCTransport(nil),
	)

	fmt.Println("Factory created:", factory != nil)
	// Output:
	// Factory created: true
}

func ExampleNewFactory_withConfig() {
	factory := a2aclient.NewFactory(
		a2aclient.WithConfig(a2aclient.Config{
			PreferredTransports: []a2a.TransportProtocol{
				a2a.TransportProtocolJSONRPC,
			},
		}),
	)

	fmt.Println("Factory with config:", factory != nil)
	// Output:
	// Factory with config: true
}

func ExampleFactory_CreateFromCard() {
	server := startTestServer()
	defer server.Close()

	factory := a2aclient.NewFactory(
		a2aclient.WithJSONRPCTransport(nil),
	)

	card := &a2a.AgentCard{
		Name: "Test Agent",
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(server.URL, a2a.TransportProtocolJSONRPC),
		},
	}

	client, err := factory.CreateFromCard(context.Background(), card)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	fmt.Println("Client from factory:", client != nil)
	// Output:
	// Client from factory: true
}

func ExampleFactory_CreateFromEndpoints() {
	server := startTestServer()
	defer server.Close()

	factory := a2aclient.NewFactory(
		a2aclient.WithJSONRPCTransport(nil),
	)

	endpoints := []*a2a.AgentInterface{
		a2a.NewAgentInterface(server.URL, a2a.TransportProtocolJSONRPC),
	}

	client, err := factory.CreateFromEndpoints(context.Background(), endpoints)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	fmt.Println("Client from endpoints:", client != nil)
	// Output:
	// Client from endpoints: true
}

func ExampleWithJSONRPCTransport() {
	customHTTPClient := &http.Client{}

	factory := a2aclient.NewFactory(
		a2aclient.WithJSONRPCTransport(customHTTPClient),
	)

	fmt.Println("Factory with custom transport:", factory != nil)
	// Output:
	// Factory with custom transport: true
}

func ExampleResolver_Resolve() {
	card := &a2a.AgentCard{
		Name:    "Discovered Agent",
		Version: "1.0.0",
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface("http://localhost:8080", a2a.TransportProtocolJSONRPC),
		},
	}
	cardBytes, err := json.Marshal(card)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(cardBytes)
	}))
	defer server.Close()

	resolved, err := agentcard.DefaultResolver.Resolve(context.Background(), server.URL)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	fmt.Println("Name:", resolved.Name)
	fmt.Println("Version:", resolved.Version)
	// Output:
	// Name: Discovered Agent
	// Version: 1.0.0
}

func ExampleNewResolver() {
	customClient := &http.Client{}
	resolver := agentcard.NewResolver(customClient)

	fmt.Println("Resolver created:", resolver != nil)
	// Output:
	// Resolver created: true
}

func ExampleAuthInterceptor() {
	credStore := a2aclient.NewInMemoryCredentialsStore()
	sessionID := a2aclient.SessionID("session-123")
	schemeName := a2a.SecuritySchemeName("bearer")
	credStore.Set(sessionID, schemeName, a2aclient.AuthCredential("secret-token"))

	interceptor := &a2aclient.AuthInterceptor{Service: credStore}

	factory := a2aclient.NewFactory(
		a2aclient.WithJSONRPCTransport(nil),
		a2aclient.WithCallInterceptors(interceptor),
	)

	fmt.Println("Factory with auth interceptor created:", factory != nil)
	// Output:
	// Factory with auth interceptor created: true
}

func ExampleWithAdditionalOptions() {
	baseFactory := a2aclient.NewFactory(
		a2aclient.WithJSONRPCTransport(nil),
	)

	extendedFactory := a2aclient.WithAdditionalOptions(
		baseFactory,
		a2aclient.WithConfig(a2aclient.Config{
			PreferredTransports: []a2a.TransportProtocol{
				a2a.TransportProtocolJSONRPC,
			},
		}),
	)

	fmt.Println("Base factory:", baseFactory != nil)
	fmt.Println("Extended factory:", extendedFactory != nil)
	// Output:
	// Base factory: true
	// Extended factory: true
}
