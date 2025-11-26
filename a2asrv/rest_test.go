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

package a2asrv

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2aclient"
)

func TestREST_handleGetTask(t *testing.T) {
	testCases := []struct {
		method string
		call   func(ctx context.Context, client *a2aclient.Client) (any, error)
	}{
		{
			method: "OnGetTask",
			call: func(ctx context.Context, client *a2aclient.Client) (any, error) {
				return client.GetTask(ctx, &a2a.TaskQueryParams{ID: "test-id"})
			},
		},
	}

	ctx := t.Context()
	lastCalledMethod := make(chan string, 1)
	interceptor := &mockInterceptor{
		beforeFn: func(ctx context.Context, callCtx *CallContext, req *Request) (context.Context, error) {
			lastCalledMethod <- callCtx.Method()
			return ctx, nil
		},
	}
	reqHandler := NewHandler(
		&mockAgentExecutor{},
		WithCallInterceptor(interceptor),
		WithExtendedAgentCard(&a2a.AgentCard{}),
	)

	server := httptest.NewServer(NewRESTHandler(reqHandler))

	client, err := a2aclient.NewFromEndpoints(ctx, []a2a.AgentInterface{
		{URL: server.URL, Transport: a2a.TransportProtocolHTTPJSON},
	})
	if err != nil {
		t.Fatalf("a2aclient.NewFromEndpoints() error = %v", err)
	}

	for _, tc := range testCases {
		t.Run(tc.method, func(t *testing.T) {
			_, _ = tc.call(ctx, client)
			calledMethod := <-lastCalledMethod
			if calledMethod != tc.method {
				t.Fatalf("wrong method called: got %q, want %q", calledMethod, tc.method)
			}
		})
	}
}
