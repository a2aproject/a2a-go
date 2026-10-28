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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/go-cmp/cmp"
)

func apiKeyCard(scheme a2a.APIKeySecurityScheme) *a2a.AgentCard {
	schemeName := a2a.SecuritySchemeName("api-key")
	return &a2a.AgentCard{
		SecurityRequirements: []a2a.SecurityRequirements{{schemeName: []string{}}},
		SecuritySchemes: a2a.NamedSecuritySchemes{
			schemeName: scheme,
		},
	}
}

func TestAuthInterceptor_APIKeyLocation(t *testing.T) {
	sid := SessionID("123")

	testCases := []struct {
		name       string
		scheme     a2a.APIKeySecurityScheme
		wantParams ServiceParams
		wantQuery  url.Values
	}{
		{
			name:       "header location",
			scheme:     a2a.APIKeySecurityScheme{Name: "X-Custom-Auth", Location: a2a.APIKeySecuritySchemeLocationHeader},
			wantParams: ServiceParams{"X-Custom-Auth": []string{"secret"}},
		},
		{
			name:       "unspecified location defaults to header",
			scheme:     a2a.APIKeySecurityScheme{Name: "X-Custom-Auth"},
			wantParams: ServiceParams{"X-Custom-Auth": []string{"secret"}},
		},
		{
			name:       "cookie location",
			scheme:     a2a.APIKeySecurityScheme{Name: "session_key", Location: a2a.APIKeySecuritySchemeLocationCookie},
			wantParams: ServiceParams{"cookie": []string{"session_key=secret"}},
		},
		{
			name:       "query location",
			scheme:     a2a.APIKeySecurityScheme{Name: "api_key", Location: a2a.APIKeySecuritySchemeLocationQuery},
			wantParams: ServiceParams{},
			wantQuery:  url.Values{"api_key": []string{"secret"}},
		},
		{
			name:       "unknown location is not injected",
			scheme:     a2a.APIKeySecurityScheme{Name: "api_key", Location: a2a.APIKeySecuritySchemeLocation("body")},
			wantParams: ServiceParams{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			credStore := NewInMemoryCredentialsStore()
			credStore.Set(sid, a2a.SecuritySchemeName("api-key"), AuthCredential("secret"))

			interceptor := &AuthInterceptor{Service: credStore}
			params := ServiceParams{}
			ctx := AttachSessionID(t.Context(), sid)

			gotCtx, _, err := interceptor.Before(ctx, &Request{ServiceParams: params, Card: apiKeyCard(tc.scheme)})
			if err != nil {
				t.Fatalf("interceptor.Before() error = %v", err)
			}

			if diff := cmp.Diff(tc.wantParams, params); diff != "" {
				t.Errorf("wrong ServiceParams (-want +got) diff = %s", diff)
			}
			if diff := cmp.Diff(tc.wantQuery, httpQueryParamsFrom(gotCtx)); diff != "" {
				t.Errorf("wrong HTTP query params (-want +got) diff = %s", diff)
			}
		})
	}
}

func TestRESTTransport_QueryParamsFromContext(t *testing.T) {
	var gotQuery url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"kind":"task","id":"task-123","contextId":"ctx-123","status":{"state":"TASK_STATE_COMPLETED"}}`))
	}))
	defer server.Close()

	transport := newRESTTransport(t, server)
	ctx := attachHTTPQueryParams(t.Context(), url.Values{"api_key": []string{"secret"}})

	_, err := transport.GetTask(ctx, ServiceParams{}, &a2a.GetTaskRequest{ID: "task-123"})
	if err != nil {
		t.Fatalf("GetTask failed: %v", err)
	}

	want := url.Values{"api_key": []string{"secret"}}
	if diff := cmp.Diff(want, gotQuery); diff != "" {
		t.Errorf("wrong query params (-want +got) diff = %s", diff)
	}
}

func TestJSONRPCTransport_QueryParamsFromContext(t *testing.T) {
	var gotQuery url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		req := mustDecodeJSONRPC(t, r, "GetTask")
		resp := newResponse(req, json.RawMessage(`{"kind":"task","id":"task-123"}`))
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	transport := NewJSONRPCTransport(server.URL, nil)
	ctx := attachHTTPQueryParams(t.Context(), url.Values{"api_key": []string{"secret"}})

	_, err := transport.GetTask(ctx, ServiceParams{}, &a2a.GetTaskRequest{ID: "task-123"})
	if err != nil {
		t.Fatalf("GetTask failed: %v", err)
	}

	want := url.Values{"api_key": []string{"secret"}}
	if diff := cmp.Diff(want, gotQuery); diff != "" {
		t.Errorf("wrong query params (-want +got) diff = %s", diff)
	}
}

func TestAuth_APIKeyQueryLocation_REST(t *testing.T) {
	ctx := t.Context()

	var gotQuery url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		if got := r.Header.Get("api_key"); got != "" {
			t.Errorf("credential leaked into header api_key = %q, want empty", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"task":{"id":"task-123","contextId":"ctx-123","status":{"state":"TASK_STATE_SUBMITTED"}}}`))
	}))
	defer server.Close()

	schemeName := a2a.SecuritySchemeName("api-key")
	card := &a2a.AgentCard{
		SupportedInterfaces: []*a2a.AgentInterface{
			{
				ProtocolBinding: a2a.TransportProtocolHTTPJSON,
				ProtocolVersion: a2a.Version,
				URL:             server.URL,
			},
		},
		SecurityRequirements: []a2a.SecurityRequirements{{schemeName: []string{}}},
		SecuritySchemes: a2a.NamedSecuritySchemes{
			schemeName: a2a.APIKeySecurityScheme{Name: "api_key", Location: a2a.APIKeySecuritySchemeLocationQuery},
		},
	}

	credStore := NewInMemoryCredentialsStore()
	client, err := NewFromCard(
		ctx,
		card,
		WithRESTTransport(nil),
		WithCallInterceptors(&AuthInterceptor{Service: credStore}),
	)
	if err != nil {
		t.Fatalf("a2aclient.NewFromCard() error = %v", err)
	}

	sessionID := SessionID("abcd")
	credStore.Set(sessionID, schemeName, AuthCredential("secret"))

	ctx = AttachSessionID(ctx, sessionID)
	_, err = client.SendMessage(ctx, &a2a.SendMessageRequest{Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("test"))})
	if err != nil {
		t.Fatalf("client.SendMessage() error = %v", err)
	}

	if got := gotQuery.Get("api_key"); got != "secret" {
		t.Errorf("query param api_key = %q, want %q", got, "secret")
	}
}

func TestAuth_APIKeyCookieLocation_JSONRPC(t *testing.T) {
	ctx := t.Context()

	var gotCookie string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCookie = r.Header.Get("Cookie")
		if got := r.Header.Get("session_key"); got != "" {
			t.Errorf("credential leaked into header session_key = %q, want empty", got)
		}
		req := mustDecodeJSONRPC(t, r, "SendMessage")
		resp := newResponse(req, json.RawMessage(`{"task":{"id":"task-123","contextId":"ctx-123","status":{"state":"TASK_STATE_SUBMITTED"}}}`))
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	schemeName := a2a.SecuritySchemeName("api-key")
	card := &a2a.AgentCard{
		SupportedInterfaces: []*a2a.AgentInterface{
			{
				ProtocolBinding: a2a.TransportProtocolJSONRPC,
				ProtocolVersion: a2a.Version,
				URL:             server.URL,
			},
		},
		SecurityRequirements: []a2a.SecurityRequirements{{schemeName: []string{}}},
		SecuritySchemes: a2a.NamedSecuritySchemes{
			schemeName: a2a.APIKeySecurityScheme{Name: "session_key", Location: a2a.APIKeySecuritySchemeLocationCookie},
		},
	}

	credStore := NewInMemoryCredentialsStore()
	client, err := NewFromCard(
		ctx,
		card,
		withTestJSONRPCTransport(),
		WithCallInterceptors(&AuthInterceptor{Service: credStore}),
	)
	if err != nil {
		t.Fatalf("a2aclient.NewFromCard() error = %v", err)
	}

	sessionID := SessionID("abcd")
	credStore.Set(sessionID, schemeName, AuthCredential("secret"))

	ctx = AttachSessionID(ctx, sessionID)
	_, err = client.SendMessage(ctx, &a2a.SendMessageRequest{Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("test"))})
	if err != nil {
		t.Fatalf("client.SendMessage() error = %v", err)
	}

	if gotCookie != "session_key=secret" {
		t.Errorf("Cookie header = %q, want %q", gotCookie, "session_key=secret")
	}
}
