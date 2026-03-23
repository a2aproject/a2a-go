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
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aclient/agentcard"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

type echoExecutor struct{}

func (e *echoExecutor) Execute(_ context.Context, _ *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		yield(a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("hello")), nil)
	}
}

func (e *echoExecutor) Cancel(_ context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCanceled, nil), nil)
	}
}

func startEchoServer() *httptest.Server {
	handler := a2asrv.NewHandler(&echoExecutor{})
	return httptest.NewServer(a2asrv.NewJSONRPCHandler(handler))
}

func makeCard(serverURL string) *a2a.AgentCard {
	return &a2a.AgentCard{
		Name: "Test Agent",
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(serverURL, a2a.TransportProtocolJSONRPC),
		},
	}
}

func ExampleNewFromCard() {
	server := startEchoServer()
	defer server.Close()

	client, err := a2aclient.NewFromCard(context.Background(), makeCard(server.URL))
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hi"))
	result, err := client.SendMessage(context.Background(), &a2a.SendMessageRequest{Message: msg})
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	if resp, ok := result.(*a2a.Message); ok {
		fmt.Println("Response:", resp.Parts[0].Text())
	}
	// Output:
	// Response: hello
}

func ExampleNewFromEndpoints() {
	server := startEchoServer()
	defer server.Close()

	endpoints := []*a2a.AgentInterface{
		a2a.NewAgentInterface(server.URL, a2a.TransportProtocolJSONRPC),
	}

	client, err := a2aclient.NewFromEndpoints(context.Background(), endpoints)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hi"))
	result, err := client.SendMessage(context.Background(), &a2a.SendMessageRequest{Message: msg})
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	if resp, ok := result.(*a2a.Message); ok {
		fmt.Println("Response:", resp.Parts[0].Text())
	}
	// Output:
	// Response: hello
}

func ExampleNewFactory() {
	server := startEchoServer()
	defer server.Close()

	factory := a2aclient.NewFactory(
		a2aclient.WithJSONRPCTransport(&http.Client{Timeout: 30 * time.Second}),
	)

	client, err := factory.CreateFromCard(context.Background(), makeCard(server.URL))
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hi"))
	result, err := client.SendMessage(context.Background(), &a2a.SendMessageRequest{Message: msg})
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	if resp, ok := result.(*a2a.Message); ok {
		fmt.Println("Response:", resp.Parts[0].Text())
	}
	// Output:
	// Response: hello
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

func ExampleAuthInterceptor() {
	var capturedAuth string
	srvInterceptor := &srvAuthCapture{captureFn: func(auth string) { capturedAuth = auth }}
	srvHandler := a2asrv.NewHandler(&echoExecutor{}, a2asrv.WithCallInterceptors(srvInterceptor))
	server := httptest.NewServer(a2asrv.NewJSONRPCHandler(srvHandler))
	defer server.Close()

	credStore := a2aclient.NewInMemoryCredentialsStore()
	sessionID := a2aclient.SessionID("session-1")
	schemeName := a2a.SecuritySchemeName("bearer")
	credStore.Set(sessionID, schemeName, a2aclient.AuthCredential("my-secret-token"))

	card := &a2a.AgentCard{
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(server.URL, a2a.TransportProtocolJSONRPC),
		},
		SecurityRequirements: []a2a.SecurityRequirements{{schemeName: []string{}}},
		SecuritySchemes: a2a.NamedSecuritySchemes{
			schemeName: a2a.OAuth2SecurityScheme{},
		},
	}

	client, err := a2aclient.NewFromCard(
		context.Background(),
		card,
		a2aclient.WithCallInterceptors(&a2aclient.AuthInterceptor{Service: credStore}),
	)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	ctx := a2aclient.AttachSessionID(context.Background(), sessionID)
	msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hi"))
	_, err = client.SendMessage(ctx, &a2a.SendMessageRequest{Message: msg})
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	fmt.Println("Server received auth:", capturedAuth)
	// Output:
	// Server received auth: Bearer my-secret-token
}

type srvAuthCapture struct {
	a2asrv.PassthroughCallInterceptor
	captureFn func(string)
}

func (s *srvAuthCapture) Before(ctx context.Context, callCtx *a2asrv.CallContext, _ *a2asrv.Request) (context.Context, any, error) {
	if auth, ok := callCtx.ServiceParams().Get("authorization"); ok && len(auth) > 0 {
		s.captureFn(auth[0])
	}
	return ctx, nil, nil
}

func ExampleWithCallInterceptors() {
	server := startEchoServer()
	defer server.Close()

	var capturedMethod string
	interceptor := &clientLogInterceptor{
		beforeFn: func(req *a2aclient.Request) {
			capturedMethod = req.Method
		},
	}

	client, err := a2aclient.NewFromCard(
		context.Background(),
		makeCard(server.URL),
		a2aclient.WithCallInterceptors(interceptor),
	)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hi"))
	_, err = client.SendMessage(context.Background(), &a2a.SendMessageRequest{Message: msg})
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	fmt.Println("Intercepted method:", capturedMethod)
	// Output:
	// Intercepted method: SendMessage
}

type clientLogInterceptor struct {
	a2aclient.PassthroughInterceptor
	beforeFn func(*a2aclient.Request)
}

func (i *clientLogInterceptor) Before(ctx context.Context, req *a2aclient.Request) (context.Context, any, error) {
	if i.beforeFn != nil {
		i.beforeFn(req)
	}
	return ctx, nil, nil
}
