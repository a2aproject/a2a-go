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

package e2e_test

import (
	"context"
	"fmt"
	"iter"
	"net/http/httptest"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/internal/testutil/testexecutor"
)

func TestTenantPropagation(t *testing.T) {
	tests := []struct {
		name               string
		incomingTenant     string
		ifaceTenant        string
		explicitTenant     string
		disablePropagation bool
		wantTenant         string
	}{
		{
			name:           "explicit tenant wins over all",
			incomingTenant: "incomingTenant",
			ifaceTenant:    "ifaceTenant",
			explicitTenant: "explicitTenant",
			wantTenant:     "explicitTenant",
		},
		{
			name:           "iface tenant wins over ctx tenant",
			incomingTenant: "incomingTenant",
			ifaceTenant:    "ifaceTenant",
			wantTenant:     "ifaceTenant",
		},
		{
			name:           "context fallback",
			incomingTenant: "incomingTenant",
			wantTenant:     "incomingTenant",
		},
		{
			name:       "no tenant",
			wantTenant: "",
		},
		{
			name:               "propagation disabled",
			incomingTenant:     "incomingTenant",
			disablePropagation: true,
			wantTenant:         "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			var gotTenant string

			serverBIface := startServer(t, testexecutor.FromFunction(func(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
				return func(yield func(a2a.Event, error) bool) {
					gotTenant = execCtx.Tenant
					event := a2a.NewSubmittedTask(execCtx, execCtx.Message)
					event.Status.State = a2a.TaskStateCompleted
					yield(event, nil)
				}
			}))
			serverBIface.Tenant = tc.ifaceTenant

			serverAIface := startServer(t, newProxyExecutor(proxyTarget{ai: serverBIface}, tc.disablePropagation, tc.explicitTenant))
			serverAIface.Tenant = tc.incomingTenant
			client, err := a2aclient.NewFromEndpoints(ctx, []*a2a.AgentInterface{serverAIface})
			if err != nil {
				t.Fatalf("a2aclient.NewFromEndpoints() error = %v", err)
			}
			resp, err := client.SendMessage(ctx, &a2a.SendMessageRequest{
				Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("Hi!")),
			})
			if err != nil {
				t.Fatalf("client.SendMessage() error = %v", err)
			}
			if task, ok := resp.(*a2a.Task); !ok || task.Status.State != a2a.TaskStateCompleted {
				t.Fatalf("client.SendMessage() = %v, want completed task", resp)
			}
			if gotTenant != tc.wantTenant {
				t.Fatalf("gotTenant = %q, wantTenant = %q", gotTenant, tc.wantTenant)
			}
		})
	}
}

type proxyTarget struct {
	ai *a2a.AgentInterface
}

func (pt proxyTarget) newClient(ctx context.Context, disablePropagation bool) (*a2aclient.Client, error) {
	if pt.ai != nil {
		client, err := a2aclient.NewFromEndpoints(ctx, []*a2a.AgentInterface{pt.ai}, a2aclient.WithConfig(a2aclient.Config{
			DisableTenantPropagation: disablePropagation,
		}))
		if err != nil {
			return nil, err
		}
		return client, nil
	}
	return nil, fmt.Errorf("agent interface not provided")
}

func newProxyExecutor(target proxyTarget, disablePropagation bool, explicitTenant string) a2asrv.AgentExecutor {
	return testexecutor.FromFunction(func(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
		return func(yield func(a2a.Event, error) bool) {
			client, err := target.newClient(ctx, disablePropagation)
			if err != nil {
				yield(nil, err)
				return
			}

			resp, err := client.SendMessage(ctx, &a2a.SendMessageRequest{
				Message: a2a.NewMessage(a2a.MessageRoleUser, execCtx.Message.Parts...),
				Tenant:  explicitTenant,
			})
			if err != nil {
				yield(nil, err)
				return
			}

			task, ok := resp.(*a2a.Task)
			if !ok {
				yield(nil, fmt.Errorf("client.SendMessage() = %v, want completed task", resp))
				return
			}

			task.ID = execCtx.TaskID
			task.ContextID = execCtx.ContextID
			yield(task, nil)
		}
	})
}

func startServer(t *testing.T, executor a2asrv.AgentExecutor) *a2a.AgentInterface {
	t.Helper()
	reqHandler := a2asrv.NewHandler(executor)
	server := httptest.NewServer(a2asrv.NewJSONRPCHandler(reqHandler))
	t.Cleanup(server.Close)
	return a2a.NewAgentInterface(server.URL, a2a.TransportProtocolJSONRPC)
}
