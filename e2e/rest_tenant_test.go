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
	"iter"
	"net/http/httptest"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/internal/testutil/testexecutor"
)

func TestRESTTenantTemplate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		template string
		basePath string
		tenant   string
	}{
		{
			name:     "single segment",
			template: "/{*}",
			tenant:   "my-tenant",
		},
		{
			name:     "single segment template with slashes in tenant",
			template: "/{*}",
			tenant:   "locations/x/projects/y",
		},
		{
			name:     "multi-segment template",
			template: "/{locations/*/projects/*}",
			tenant:   "locations/x/projects/y",
		},
		{
			name:     "multi-segment template starting with wildcard",
			template: "/{*/*}",
			tenant:   "x/y",
		},
		{
			name:     "literal prefix",
			template: "/locations/*/projects/{*}",
			basePath: "/locations/x/projects",
			tenant:   "my-project",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			var gotTenant string
			executor := testexecutor.FromFunction(func(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
				return func(yield func(a2a.Event, error) bool) {
					gotTenant = execCtx.Tenant
					event := a2a.NewSubmittedTask(execCtx, execCtx.Message)
					event.Status.State = a2a.TaskStateCompleted
					yield(event, nil)
				}
			})
			server := httptest.NewServer(a2asrv.NewTenantRESTHandler(tc.template, a2asrv.NewHandler(executor)))
			t.Cleanup(server.Close)
			iface := a2a.NewAgentInterface(server.URL+tc.basePath, a2a.TransportProtocolHTTPJSON)
			iface.Tenant = tc.tenant
			client, err := a2aclient.NewFromEndpoints(ctx, []*a2a.AgentInterface{iface})
			if err != nil {
				t.Fatalf("a2aclient.NewFromEndpoints() error = %v", err)
			}

			resp, err := client.SendMessage(ctx, &a2a.SendMessageRequest{
				Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("Hi!")),
			})
			if err != nil {
				t.Fatalf("client.SendMessage() error = %v", err)
			}
			task, ok := resp.(*a2a.Task)
			if !ok {
				t.Fatalf("client.SendMessage() = %v, want task", resp)
			}
			if gotTenant != tc.tenant {
				t.Fatalf("client.SendMessage() tenant = %q, want %q", gotTenant, tc.tenant)
			}
			if _, err := client.GetTask(ctx, &a2a.GetTaskRequest{ID: task.ID}); err != nil {
				t.Fatalf("client.GetTask() error = %v", err)
			}
		})
	}
}
