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
	"iter"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/go-cmp/cmp"
)

type tenantCaptureInterceptor struct {
	PassthroughInterceptor
	tenants []string
}

func (i *tenantCaptureInterceptor) Before(ctx context.Context, req *Request) (context.Context, any, error) {
	if r, ok := req.Payload.(*a2a.GetTaskRequest); ok {
		i.tenants = append(i.tenants, r.Tenant)
	}
	return ctx, nil, nil
}

func TestClient_TenantRequestReuse(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name               string
		requestTenant      string
		interfaceTenant    string
		disablePropagation bool
		want               []string
	}{
		{name: "context", want: []string{"first", "second", ""}},
		{name: "explicit request", requestTenant: "explicit", want: []string{"explicit", "explicit", "explicit"}},
		{name: "interface", interfaceTenant: "interface", want: []string{"interface", "interface", "interface"}},
		{name: "disabled", disablePropagation: true, want: []string{"", "", ""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var got []string
			interceptor := &tenantCaptureInterceptor{}
			client := &Client{
				config: Config{DisableTenantPropagation: tc.disablePropagation},
				endpoint: a2a.AgentInterface{
					Tenant: tc.interfaceTenant,
				},
				interceptors: []CallInterceptor{interceptor},
				transport: &testTransport{GetTaskFn: func(ctx context.Context, params ServiceParams, req *a2a.GetTaskRequest) (*a2a.Task, error) {
					got = append(got, req.Tenant)
					return &a2a.Task{ID: req.ID}, nil
				}},
			}
			wantRequest := &a2a.GetTaskRequest{ID: "task-1", Tenant: tc.requestTenant}
			request := *wantRequest
			for _, ctx := range []context.Context{
				a2a.AttachTenant(t.Context(), "first"),
				a2a.AttachTenant(t.Context(), "second"),
				t.Context(),
			} {
				if _, err := client.GetTask(ctx, &request); err != nil {
					t.Fatalf("client.GetTask() error = %v, want nil", err)
				}
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("client.GetTask() transport tenants wrong result (-want +got) diff = %s", diff)
			}
			if diff := cmp.Diff(tc.want, interceptor.tenants); diff != "" {
				t.Fatalf("client.GetTask() interceptor tenants wrong result (-want +got) diff = %s", diff)
			}
			if diff := cmp.Diff(wantRequest, &request); diff != "" {
				t.Fatalf("client.GetTask() request changed (-want +got) diff = %s", diff)
			}
		})
	}
}

func TestClient_TenantStreamingRequestReuse(t *testing.T) {
	t.Parallel()

	var got []string
	client := &Client{
		transport: &testTransport{SendStreamingMessageFn: func(ctx context.Context, params ServiceParams, req *a2a.SendMessageRequest) iter.Seq2[a2a.Event, error] {
			return func(yield func(a2a.Event, error) bool) {
				got = append(got, req.Tenant)
				yield(a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("ok")), nil)
			}
		}},
	}
	request := &a2a.SendMessageRequest{Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hello"))}
	first := client.SendStreamingMessage(a2a.AttachTenant(t.Context(), "first"), request)
	second := client.SendStreamingMessage(a2a.AttachTenant(t.Context(), "second"), request)
	for _, stream := range []iter.Seq2[a2a.Event, error]{second, first} {
		for _, err := range stream {
			if err != nil {
				t.Fatalf("client.SendStreamingMessage() error = %v, want nil", err)
			}
		}
	}
	if diff := cmp.Diff([]string{"second", "first"}, got); diff != "" {
		t.Fatalf("client.SendStreamingMessage() tenants wrong result (-want +got) diff = %s", diff)
	}
	if request.Tenant != "" {
		t.Fatalf("client.SendStreamingMessage() request.Tenant = %q, want empty", request.Tenant)
	}
}
