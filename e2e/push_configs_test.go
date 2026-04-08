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
	"errors"
	"net"
	"net/http/httptest"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2agrpc/v1"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/internal/testutil"
	"github.com/a2aproject/a2a-go/v2/internal/testutil/testexecutor"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

func TestPushConfigs_ClientServerRoundtrip(t *testing.T) {
	ctx := t.Context()
	task := &a2a.Task{
		ID: a2a.NewTaskID(),
	}
	config := a2a.PushConfig{
		ID: "config-123",
		Auth: &a2a.PushAuthInfo{
			Scheme:      "Bearer",
			Credentials: "token-123",
		},
		Token: "interop-token",
		URL:   "https://example.invalid/webhook",
	}
	want := &a2a.TaskPushConfig{
		TaskID: task.ID,
		Config: config,
	}

	for _, transport := range []a2a.TransportProtocol{a2a.TransportProtocolHTTPJSON, a2a.TransportProtocolJSONRPC, a2a.TransportProtocolGRPC} {
		t.Run(string(transport), func(t *testing.T) {
			client := startPushServer(t, task, transport)
			t.Run("CreateTaskPushConfig", func(t *testing.T) {
				createTaskPushConfigReq := &a2a.CreateTaskPushConfigRequest{
					TaskID: task.ID,
					Config: config,
				}
				result, err := client.CreateTaskPushConfig(ctx, createTaskPushConfigReq)
				if err != nil {
					t.Fatalf("client.CreateTaskPushConfig() error = %v", err)
				}
				if diff := cmp.Diff(want, result); diff != "" {
					t.Fatalf("client.CreateTaskPushConfig() returned diff (+got,-want): %s", diff)
				}
			})
			t.Run("GetTaskPushConfig", func(t *testing.T) {
				getTaskPushConfigReq := &a2a.GetTaskPushConfigRequest{
					TaskID: task.ID,
					ID:     config.ID,
				}
				result, err := client.GetTaskPushConfig(ctx, getTaskPushConfigReq)
				if err != nil {
					t.Fatalf("client.GetTaskPushConfig() error = %v", err)
				}
				if diff := cmp.Diff(want, result); diff != "" {
					t.Fatalf("client.GetTaskPushConfig() returned diff (+got,-want): %s", diff)
				}
			})
			t.Run("ListTaskPushConfigs", func(t *testing.T) {
				listTaskPushConfigsReq := &a2a.ListTaskPushConfigRequest{
					TaskID: task.ID,
				}
				wantListResult := []*a2a.TaskPushConfig{want}
				result, err := client.ListTaskPushConfigs(ctx, listTaskPushConfigsReq)
				if err != nil {
					t.Fatalf("client.ListTaskPushConfigs() error = %v", err)
				}
				if diff := cmp.Diff(wantListResult, result); diff != "" {
					t.Fatalf("client.ListTaskPushConfigs() returned diff (+got,-want): %s", diff)
				}
			})
			t.Run("DeleteTaskPushConfig", func(t *testing.T) {
				deleteTaskPushConfigReq := &a2a.DeleteTaskPushConfigRequest{
					TaskID: task.ID,
					ID:     config.ID,
				}
				err := client.DeleteTaskPushConfig(ctx, deleteTaskPushConfigReq)
				if err != nil {
					t.Fatalf("client.DeleteTaskPushConfig() error = %v", err)
				}
				listTaskPushConfigsReq := &a2a.ListTaskPushConfigRequest{
					TaskID: task.ID,
				}
				wantListResult := []*a2a.TaskPushConfig{}
				result, err := client.ListTaskPushConfigs(ctx, listTaskPushConfigsReq)
				if err != nil {
					t.Fatalf("client.ListTaskPushConfigs() error = %v", err)
				}
				if diff := cmp.Diff(wantListResult, result); diff != "" {
					t.Fatalf("client.ListTaskPushConfigs() returned diff (+got,-want): %s", diff)
				}
			})
		})
	}
}

func startPushServer(t *testing.T, task *a2a.Task, transport a2a.TransportProtocol) *a2aclient.Client {
	t.Helper()
	ctx := t.Context()
	pushstore := testutil.NewTestPushConfigStore()
	pushsender := testutil.NewTestPushSender(t).SetSendPushError(nil)
	store := testutil.NewTestTaskStore().WithTasks(t, task)
	reqHandler := a2asrv.NewHandler(&testexecutor.TestAgentExecutor{}, a2asrv.WithTaskStore(store), a2asrv.WithPushNotifications(pushstore, pushsender))

	switch transport {
	case a2a.TransportProtocolHTTPJSON:
		server := httptest.NewServer(a2asrv.NewRESTHandler(reqHandler))
		t.Cleanup(server.Close)
		card := &a2a.AgentCard{
			SupportedInterfaces: []*a2a.AgentInterface{
				a2a.NewAgentInterface(server.URL, a2a.TransportProtocolHTTPJSON),
			},
		}
		client, err := a2aclient.NewFromCard(ctx, card)
		if err != nil {
			t.Fatalf("a2aclient.NewFromCard() error = %v", err)
		}
		return client
	case a2a.TransportProtocolJSONRPC:
		server := httptest.NewServer(a2asrv.NewJSONRPCHandler(reqHandler))
		t.Cleanup(server.Close)
		card := &a2a.AgentCard{
			SupportedInterfaces: []*a2a.AgentInterface{
				a2a.NewAgentInterface(server.URL, a2a.TransportProtocolJSONRPC),
			},
		}
		client, err := a2aclient.NewFromCard(ctx, card)
		if err != nil {
			t.Fatalf("a2aclient.NewFromCard() error = %v", err)
		}
		return client
	case a2a.TransportProtocolGRPC:
		lis := bufconn.Listen(1024 * 1024)
		s := grpc.NewServer()
		a2agrpc.NewHandler(reqHandler).RegisterWith(s)
		go func() {
			if err := s.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
				t.Errorf("gRPC server error = %v", err)
			}
		}()
		t.Cleanup(func() { s.Stop() })
		card := &a2a.AgentCard{
			SupportedInterfaces: []*a2a.AgentInterface{
				a2a.NewAgentInterface("passthrough:///bufnet", a2a.TransportProtocolGRPC),
			},
		}
		client, err := a2aclient.NewFromCard(ctx, card, a2agrpc.WithGRPCTransport(
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
				return lis.Dial()
			}),
		))
		if err != nil {
			t.Fatalf("a2aclient.NewFromCard() error = %v", err)
		}
		return client
	}
	t.Fatalf("unsupported transport protocol: %v", transport)
	return nil
}
