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

// Package main demonstrates how to implement authentication for an A2A server.
//
// This example shows three key A2A auth concepts:
//
//  1. Declaring security schemes and requirements in the AgentCard so that
//     clients know how to authenticate before sending requests.
//  2. Implementing a server-side CallInterceptor that validates Bearer tokens
//     from the Authorization header and populates the authenticated User on
//     the CallContext.
//  3. Using the authenticated user identity inside the AgentExecutor to
//     personalize responses.
//
// The token validation is intentionally simplified (static token check) to
// focus on the A2A SDK wiring. In production you would validate JWTs against
// a JWKS endpoint or use an OAuth2 middleware.
//
// Run with:
//
//	go run .
//
// Then in another terminal, test with a valid token:
//
//	curl -s http://localhost:9002/.well-known/agent-card.json | jq .securitySchemes
//
// Or send a message (requires a running client or curl with JSON-RPC payload).
package main

import (
	"context"
	"flag"
	"fmt"
	"iter"
	"log"
	"net"
	"net/http"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

// --- Authentication Interceptor ---

// bearerAuthInterceptor implements a2asrv.CallInterceptor.
// It extracts the Bearer token from the Authorization header, validates it,
// and sets the authenticated user on the CallContext.
type bearerAuthInterceptor struct {
	a2asrv.PassthroughCallInterceptor

	// validTokens is a simple lookup table for demo purposes.
	// In production, this would be JWT validation against a JWKS endpoint.
	validTokens map[string]string // token -> username
}

func (b *bearerAuthInterceptor) Before(ctx context.Context, callCtx *a2asrv.CallContext, req *a2asrv.Request) (context.Context, any, error) {
	// Extract the Authorization header from ServiceParams.
	// ServiceParams are populated by the transport layer from HTTP headers (JSON-RPC/REST)
	// or gRPC metadata, making this check transport-agnostic.
	authValues, ok := callCtx.ServiceParams().Get("authorization")
	if !ok || len(authValues) == 0 {
		// No auth header present. Return an A2A protocol error so the client
		// knows authentication is required.
		return ctx, nil, a2a.ErrUnauthenticated
	}

	token := authValues[0]
	if !strings.HasPrefix(token, "Bearer ") {
		return ctx, nil, a2a.ErrUnauthenticated
	}
	token = strings.TrimPrefix(token, "Bearer ")

	// Validate the token. In production this would verify a JWT signature,
	// check expiry, audience claims, etc.
	username, valid := b.validTokens[token]
	if !valid {
		return ctx, nil, a2a.ErrUnauthenticated
	}

	// Set the authenticated user on the call context.
	// This makes the user identity available to the AgentExecutor via
	// ExecutorContext.User and to the task store for access control.
	callCtx.User = a2asrv.NewAuthenticatedUser(username, map[string]any{
		"auth_method": "bearer",
	})

	log.Printf("Authenticated user: %s", username)
	return ctx, nil, nil
}

// --- Agent Executor ---

// secureAgentExecutor personalizes responses based on the authenticated user.
type secureAgentExecutor struct{}

var _ a2asrv.AgentExecutor = (*secureAgentExecutor)(nil)

func (*secureAgentExecutor) Execute(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		// Access the authenticated user set by the auth interceptor.
		// The User field is always non-nil (defaults to unauthenticated).
		username := "anonymous"
		if execCtx.User != nil && execCtx.User.Authenticated {
			username = execCtx.User.Name
		}

		// Extract the user's message text for the echo response.
		userText := ""
		if execCtx.Message != nil && len(execCtx.Message.Parts) > 0 {
			userText = execCtx.Message.Parts[0].Text()
		}

		greeting := fmt.Sprintf("Hello, %s! You said: %q", username, userText)
		response := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart(greeting))
		yield(response, nil)
	}
}

func (*secureAgentExecutor) Cancel(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCanceled, nil), nil)
	}
}

// --- Main ---

var port = flag.Int("port", 9002, "Port for the authenticated A2A server.")

func main() {
	flag.Parse()

	// 1. Configure the authentication interceptor with valid tokens.
	//    In production, you would validate JWTs against a JWKS endpoint instead.
	authInterceptor := &bearerAuthInterceptor{
		validTokens: map[string]string{
			"alice-secret-token": "alice",
			"bob-secret-token":   "bob",
		},
	}

	// 2. Create the request handler with the auth interceptor attached.
	//    The interceptor runs before every A2A method call (SendMessage, GetTask, etc.).
	requestHandler := a2asrv.NewHandler(
		&secureAgentExecutor{},
		a2asrv.WithCallInterceptors(authInterceptor),
	)

	// 3. Build the AgentCard with security scheme declarations.
	//    This tells clients what authentication is required before they attempt to connect.
	//    The SecuritySchemes map declares available auth methods, and SecurityRequirements
	//    specifies which schemes must be satisfied (OR of ANDs).
	addr := fmt.Sprintf("http://127.0.0.1:%d/invoke", *port)
	agentCard := &a2a.AgentCard{
		Name:        "Authenticated Agent",
		Description: "An agent that requires Bearer token authentication",
		Version:     "1.0.0",
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(addr, a2a.TransportProtocolJSONRPC),
		},
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
		Capabilities:       a2a.AgentCapabilities{Streaming: true},
		Skills: []a2a.AgentSkill{
			{
				ID:          "greet",
				Name:        "Personalized Greeting",
				Description: "Greets the authenticated user by name.",
				Tags:        []string{"greeting", "auth"},
				Examples:    []string{"hello", "hi there"},
			},
		},

		// Declare that this agent uses HTTP Bearer authentication.
		// Clients reading the AgentCard will know to include an Authorization header.
		SecuritySchemes: a2a.NamedSecuritySchemes{
			a2a.SecuritySchemeName("bearerAuth"): a2a.HTTPAuthSecurityScheme{
				Scheme:       "Bearer",
				BearerFormat: "opaque",
				Description:  "A static bearer token for demo purposes.",
			},
		},

		// Require the bearerAuth scheme for all interactions.
		// This is an OR-list of AND-sets: here we have a single set requiring bearerAuth.
		SecurityRequirements: a2a.SecurityRequirementsOptions{
			a2a.SecurityRequirements{
				a2a.SecuritySchemeName("bearerAuth"): a2a.SecuritySchemeScopes{},
			},
		},
	}

	// 4. Set up HTTP server with JSON-RPC transport and agent card endpoint.
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("Failed to bind to port: %v", err)
	}
	log.Printf("Starting authenticated A2A server on 127.0.0.1:%d", *port)
	log.Printf("Valid tokens: alice-secret-token, bob-secret-token")

	mux := http.NewServeMux()
	mux.Handle("/invoke", a2asrv.NewJSONRPCHandler(requestHandler))
	mux.Handle(a2asrv.WellKnownAgentCardPath, a2asrv.NewStaticAgentCardHandler(agentCard))

	if err := http.Serve(listener, mux); err != nil {
		log.Fatalf("Server stopped: %v", err)
	}
}
