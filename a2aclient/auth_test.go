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

package a2aclient

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2agrpc"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
	"github.com/a2aproject/a2a-go/internal/testutil/testexecutor"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

func startGRPCTestServer(t *testing.T, handler a2asrv.RequestHandler, listener *bufconn.Listener) {
	s := grpc.NewServer()
	grpcHandler := a2agrpc.NewHandler(handler)
	grpcHandler.RegisterWith(s)
	if err := s.Serve(listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
		t.Logf("Server exited with error: %v", err)
	}
}

func withTestGRPCTransport(listener *bufconn.Listener) FactoryOption {
	return WithGRPCTransport(
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
}

func TestAuth_GRPC(t *testing.T) {
	ctx := t.Context()
	listener := bufconn.Listen(1024 * 1024)

	var capturedCallContext *a2asrv.CallContext
	executor := testexecutor.FromFunction(func(ctx context.Context, reqCtx *a2asrv.RequestContext, q eventqueue.Queue) error {
		capturedCallContext, _ = a2asrv.CallContextFrom(ctx)
		return q.Write(ctx, a2a.NewMessage(a2a.MessageRoleAgent))
	})
	handler := a2asrv.NewHandler(executor)
	go startGRPCTestServer(t, handler, listener)

	schemeName := a2a.SecuritySchemeName("oauth2")
	card := &a2a.AgentCard{
		PreferredTransport: a2a.TransportProtocolGRPC,
		URL:                "passthrough:///bufnet",
		Security:           []a2a.SecurityRequirements{{schemeName: []string{}}},
		SecuritySchemes: a2a.NamedSecuritySchemes{
			schemeName: a2a.OAuth2SecurityScheme{},
		},
	}

	credStore := NewInMemoryCredentialsStore()
	client, err := NewFromCard(
		ctx,
		card,
		withTestGRPCTransport(listener),
		WithCallInterceptors(&AuthInterceptor{Service: credStore}),
	)
	if err != nil {
		t.Fatalf("a2aclient.NewFromCard() error = %v", err)
	}

	token := "secret"
	sessionID := SessionID("abcd")
	credStore.Set(sessionID, schemeName, AuthCredential(token))

	ctx = WithSessionID(ctx, sessionID)
	_, err = client.SendMessage(ctx, &a2a.MessageSendParams{Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: "test"})})
	if err != nil {
		t.Fatalf("client.SendMessage() error = %v", err)
	}

	auth, _ := capturedCallContext.RequestMeta().Get("authorization")
	if diff := cmp.Diff([]string{"Bearer " + token}, auth); diff != "" {
		t.Fatalf("RequestMeta[authorization] wrong value = %v, want = %v", auth, []string{"Bearer " + token})
	}
}

func TestAuthInterceptor(t *testing.T) {
	type storedCred struct {
		sid    SessionID
		scheme a2a.SecuritySchemeName
		cred   AuthCredential
	}

	toSchemeName := func(s string) a2a.SecuritySchemeName { return a2a.SecuritySchemeName(s) }

	testCases := []struct {
		name   string
		sid    SessionID
		stored []*storedCred
		card   *a2a.AgentCard
		want   ServiceParams
	}{
		{
			name: "http auth",
			sid:  SessionID("123"),
			stored: []*storedCred{{
				sid:    SessionID("123"),
				scheme: toSchemeName("test"),
				cred:   AuthCredential("secret"),
			}},
			card: &a2a.AgentCard{
				Security: []a2a.SecurityRequirements{{toSchemeName("test"): []string{}}},
				SecuritySchemes: a2a.NamedSecuritySchemes{
					toSchemeName("test"): a2a.HTTPAuthSecurityScheme{},
				},
			},
			want: ServiceParams{"Authorization": []string{"Bearer secret"}},
		},
		{
			name: "ouath2",
			sid:  SessionID("123"),
			stored: []*storedCred{{
				sid:    SessionID("123"),
				scheme: toSchemeName("test"),
				cred:   AuthCredential("secret"),
			}},
			card: &a2a.AgentCard{
				Security: []a2a.SecurityRequirements{{toSchemeName("test"): []string{}}},
				SecuritySchemes: a2a.NamedSecuritySchemes{
					toSchemeName("test"): a2a.OAuth2SecurityScheme{},
				},
			},
			want: ServiceParams{"Authorization": []string{"Bearer secret"}},
		},
		{
			name: "api key",
			sid:  SessionID("123"),
			stored: []*storedCred{{
				sid:    SessionID("123"),
				scheme: toSchemeName("test"),
				cred:   AuthCredential("secret"),
			}},
			card: &a2a.AgentCard{
				Security: []a2a.SecurityRequirements{{toSchemeName("test"): []string{}}},
				SecuritySchemes: a2a.NamedSecuritySchemes{
					toSchemeName("test"): a2a.APIKeySecurityScheme{Name: "X-Custom-Auth"},
				},
			},
			want: ServiceParams{"X-Custom-Auth": []string{"secret"}},
		},
		{
			name: "first credential chosen",
			sid:  SessionID("123"),
			stored: []*storedCred{
				{
					sid:    SessionID("123"),
					scheme: toSchemeName("test-2"),
					cred:   AuthCredential("secret-2"),
				},
				{
					sid:    SessionID("123"),
					scheme: toSchemeName("test-3"),
					cred:   AuthCredential("secret-3"),
				},
			},
			card: &a2a.AgentCard{
				Security: []a2a.SecurityRequirements{
					{toSchemeName("test"): []string{}},
					{toSchemeName("test-2"): []string{}},
					{toSchemeName("test-3"): []string{}},
				},
				SecuritySchemes: a2a.NamedSecuritySchemes{
					toSchemeName("test"):   a2a.OAuth2SecurityScheme{},
					toSchemeName("test-2"): a2a.HTTPAuthSecurityScheme{},
					toSchemeName("test-3"): a2a.APIKeySecurityScheme{Name: "X-Custom-Auth"},
				},
			},
			want: ServiceParams{"Authorization": []string{"Bearer secret-2"}},
		},
		{
			name: "no session",
			card: &a2a.AgentCard{
				Security: []a2a.SecurityRequirements{{toSchemeName("test"): []string{}}},
				SecuritySchemes: a2a.NamedSecuritySchemes{
					toSchemeName("test"): a2a.APIKeySecurityScheme{Name: "X-Custom-Auth"},
				},
			},
			want: ServiceParams{},
		},
		{
			name: "different session",
			sid:  SessionID("123"),
			stored: []*storedCred{{
				sid:    SessionID("321"),
				scheme: toSchemeName("test"),
				cred:   AuthCredential("secret"),
			}},
			card: &a2a.AgentCard{
				Security: []a2a.SecurityRequirements{{toSchemeName("test"): []string{}}},
				SecuritySchemes: a2a.NamedSecuritySchemes{
					toSchemeName("test"): a2a.APIKeySecurityScheme{Name: "X-Custom-Auth"},
				},
			},
			want: ServiceParams{},
		},
		{
			name: "no card",
			sid:  SessionID("123"),
			stored: []*storedCred{{
				sid:    SessionID("123"),
				scheme: toSchemeName("test"),
				cred:   AuthCredential("secret"),
			}},
			want: ServiceParams{},
		},
		{
			name: "no matching credential",
			sid:  SessionID("123"),
			stored: []*storedCred{{
				sid:    SessionID("123"),
				scheme: toSchemeName("test-2"),
				cred:   AuthCredential("secret"),
			}},
			card: &a2a.AgentCard{
				Security: []a2a.SecurityRequirements{{toSchemeName("test"): []string{}}},
				SecuritySchemes: a2a.NamedSecuritySchemes{
					toSchemeName("test"): a2a.OAuth2SecurityScheme{},
				},
			},
			want: ServiceParams{},
		},
		{
			name: "no security requirements",
			sid:  SessionID("123"),
			stored: []*storedCred{{
				sid:    SessionID("123"),
				scheme: toSchemeName("test"),
				cred:   AuthCredential("secret"),
			}},
			card: &a2a.AgentCard{
				SecuritySchemes: a2a.NamedSecuritySchemes{
					toSchemeName("test"): a2a.OAuth2SecurityScheme{},
				},
			},
			want: ServiceParams{},
		},
		{
			name: "no security schemes",
			sid:  SessionID("123"),
			stored: []*storedCred{{
				sid:    SessionID("123"),
				scheme: toSchemeName("test"),
				cred:   AuthCredential("secret"),
			}},
			card: &a2a.AgentCard{
				Security: []a2a.SecurityRequirements{{toSchemeName("test"): []string{}}},
			},
			want: ServiceParams{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			params := ServiceParams{}

			ctx := t.Context()
			if tc.sid != "" {
				ctx = WithSessionID(ctx, tc.sid)
			}

			credStore := NewInMemoryCredentialsStore()
			for _, stored := range tc.stored {
				credStore.Set(stored.sid, stored.scheme, stored.cred)
			}

			interceptor := &AuthInterceptor{Service: credStore}
			_, _, err := interceptor.Before(ctx, &Request{ServiceParams: params, Card: tc.card})
			if err != nil {
				t.Errorf("interceptor.Before() error = %v", err)
			}

			if diff := cmp.Diff(tc.want, params); diff != "" {
				t.Errorf("wrong ServiceParams (+got,-want) diff = %s", diff)
			}
		})
	}
}
