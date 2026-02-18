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

package a2aext

import (
	"context"
	"fmt"
	"iter"
	"maps"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2aclient"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/a2aproject/a2a-go/internal/testutil/testexecutor"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

/**
 * Tests payload and request metadata propagation from client to server.
 * [a2aclient] -> [forwardProxy] -> [reverseProxy] -> [server]
 */
func TestTripleHopPropagation(t *testing.T) {
	tests := []struct {
		name                  string
		clientCfg             ClientPropagatorConfig
		serverCfg             ServerPropagatorConfig
		clientSvcParams       map[string]any
		clientReqHeaders      map[string][]string
		wantPropagatedMeta    map[string]any
		wantPropagatedHeaders map[string][]string
	}{
		{
			name: "default propagation affects extensions",
			clientSvcParams: map[string]any{
				"extension1.com": "bar",
				"extension2.com": map[string]string{"nested": "bar"},
				"not-extension":  "qux",
			},
			clientReqHeaders: map[string][]string{
				ServiceParamsKey: {"extension1.com", "extension2.com"},
				"x-ignore":       {"ignored"},
			},
			wantPropagatedMeta: map[string]any{
				"extension1.com": "bar",
				"extension2.com": map[string]any{"nested": "bar"},
			},
			wantPropagatedHeaders: map[string][]string{
				ServiceParamsKey: {"extension1.com", "extension2.com"},
			},
		},
		{
			name: "selective propagation",
			serverCfg: ServerPropagatorConfig{
				MetadataPredicate: func(ctx context.Context, key string) bool {
					return key == "keep-meta"
				},
				HeaderPredicate: func(ctx context.Context, key string) bool {
					return key == "keep-header"
				},
			},
			clientCfg: ClientPropagatorConfig{
				MetadataPredicate: func(ctx context.Context, card *a2a.AgentCard, key string) bool {
					return key == "keep-meta"
				},
				HeaderPredicate: func(ctx context.Context, card *a2a.AgentCard, key string, val string) bool {
					return key == "keep-header" && val != "dropval"
				},
			},
			clientSvcParams: map[string]any{
				"keep-meta": "value",
				"drop-meta": "value",
			},
			clientReqHeaders: map[string][]string{
				"keep-header": {"val", "dropval"},
				"drop-header": {"val"},
			},
			wantPropagatedMeta:    map[string]any{"keep-meta": "value"},
			wantPropagatedHeaders: map[string][]string{"keep-header": {"val"}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()

			serverInterceptor := NewServerPropagator(&tc.serverCfg)
			clientInterceptor := NewClientPropagator(&tc.clientCfg)

			var gotExecCtx *a2asrv.ExecutorContext
			gotHeaders := map[string][]string{}
			server := startServer(t, serverInterceptor, testexecutor.FromFunction(
				func(ctx context.Context, ec *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
					return func(yield func(a2a.Event, error) bool) {
						maps.Insert(gotHeaders, ec.ServiceParams.List())
						gotExecCtx = ec

						event := a2a.NewSubmittedTask(ec, ec.Message)
						event.Status = a2a.TaskStatus{State: a2a.TaskStateCompleted}
						yield(event, nil)
					}
				},
			))
			reverseProxy := startServer(t, serverInterceptor, newProxyExecutor(clientInterceptor, proxyTarget{ai: server}))
			forwardProxy := startServer(t, serverInterceptor, newProxyExecutor(clientInterceptor, proxyTarget{ai: reverseProxy}))

			reqHeaderInjector := a2aclient.NewServiceParamsInjector(tc.clientReqHeaders)
			client, err := a2aclient.NewFromEndpoints(
				ctx,
				[]a2a.AgentInterface{forwardProxy},
				a2aclient.WithCallInterceptors(reqHeaderInjector),
			)
			if err != nil {
				t.Fatalf("a2aclient.NewFromEndpoints() error = %v", err)
			}

			resp, err := client.SendMessage(ctx, &a2a.SendMessageRequest{
				Message:  a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("Hi!")),
				Metadata: tc.clientSvcParams,
			})
			if err != nil {
				t.Fatalf("client.SendMessage() error = %v", err)
			}
			if task, ok := resp.(*a2a.Task); !ok || task.Status.State != a2a.TaskStateCompleted {
				t.Fatalf("client.SendMessage() = %v, want completed task", resp)
			}
			if diff := cmp.Diff(tc.wantPropagatedMeta, gotExecCtx.Metadata); diff != "" {
				t.Fatalf("wrong end request meta (+got,-want), diff = %s", diff)
			}
			ignoreStdHeaders := cmpopts.IgnoreMapEntries(func(k string, v any) bool {
				return slices.Contains([]string{"accept-encoding", "content-length", "content-type", "keep-header", "user-agent"}, k)
			})
			if diff := cmp.Diff(tc.wantPropagatedHeaders, gotHeaders, ignoreStdHeaders); diff != "" {
				t.Fatalf("wrong end request headers (+got,-want), diff = %s", diff)
			}
		})
	}
}

func TestDefaultPropagation(t *testing.T) {
	tests := []struct {
		name                 string
		clientSvcParams      map[string]any
		clientReqHeaders     map[string][]string
		serverASupports      []string
		serverBSupports      []string
		wantBReceivedMeta    map[string]any
		wantBReceivedHeaders map[string][]string
	}{
		{
			name: "serverB supports all extensions",
			clientSvcParams: map[string]any{
				"extension1.com": "bar",
				"extension2.com": map[string]string{"nested": "bar"},
			},
			clientReqHeaders: map[string][]string{
				ServiceParamsKey: {"extension1.com", "extension2.com"},
			},
			serverASupports: []string{"extension1.com", "extension2.com"},
			serverBSupports: []string{"extension1.com", "extension2.com"},
			wantBReceivedMeta: map[string]any{
				"extension1.com": "bar",
				"extension2.com": map[string]any{"nested": "bar"},
			},
			wantBReceivedHeaders: map[string][]string{
				ServiceParamsKey: {"extension1.com", "extension2.com"},
			},
		},
		{
			name: "serverB supports some extensions",
			clientSvcParams: map[string]any{
				"extension1.com": "bar",
				"extension2.com": map[string]string{"nested": "bar"},
			},
			clientReqHeaders: map[string][]string{
				ServiceParamsKey: {"extension1.com", "extension2.com"},
			},
			serverASupports: []string{"extension1.com", "extension2.com"},
			serverBSupports: []string{"extension1.com"},
			wantBReceivedMeta: map[string]any{
				"extension1.com": "bar",
			},
			wantBReceivedHeaders: map[string][]string{
				ServiceParamsKey: {"extension1.com"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()

			serverInterceptor := NewServerPropagator(nil)
			clientInterceptor := NewClientPropagator(nil)

			var gotExecCtx *a2asrv.ExecutorContext
			gotHeaders := map[string][]string{}
			serverB := startServer(t, serverInterceptor, testexecutor.FromFunction(
				func(ctx context.Context, ec *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
					return func(yield func(a2a.Event, error) bool) {
						maps.Insert(gotHeaders, ec.ServiceParams.List())
						gotExecCtx = ec
						event := a2a.NewSubmittedTask(ec, ec.Message)
						event.Status = a2a.TaskStatus{State: a2a.TaskStateCompleted}
						yield(event, nil)
					}
				},
			))
			serverBCard := newAgentCard(serverB, tc.serverBSupports)
			serverA := startServer(t, serverInterceptor, newProxyExecutor(clientInterceptor, proxyTarget{ac: serverBCard}))

			client, err := a2aclient.NewFromCard(
				ctx,
				newAgentCard(serverA, tc.serverASupports),
				a2aclient.WithCallInterceptors(NewActivator(tc.serverASupports...)),
			)
			if err != nil {
				t.Fatalf("a2aclient.NewFromEndpoints() error = %v", err)
			}

			resp, err := client.SendMessage(ctx, &a2a.SendMessageRequest{
				Message:  a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("Hi!")),
				Metadata: tc.clientSvcParams,
			})
			if err != nil {
				t.Fatalf("client.SendMessage() error = %v", err)
			}
			if task, ok := resp.(*a2a.Task); !ok || task.Status.State != a2a.TaskStateCompleted {
				t.Fatalf("client.SendMessage() = %v, want completed task", resp)
			}
			if diff := cmp.Diff(tc.wantBReceivedMeta, gotExecCtx.Metadata); diff != "" {
				t.Fatalf("wrong end request meta (+got,-want), diff = %s", diff)
			}
			ignoreStdHeaders := cmpopts.IgnoreMapEntries(func(k string, v any) bool {
				return slices.Contains([]string{"accept-encoding", "content-length", "content-type", "keep-header", "user-agent"}, k)
			})
			if diff := cmp.Diff(tc.wantBReceivedHeaders, gotHeaders, ignoreStdHeaders); diff != "" {
				t.Fatalf("wrong end request headers (+got,-want), diff = %s", diff)
			}
		})
	}
}

func startServer(t *testing.T, interceptor a2asrv.CallInterceptor, executor a2asrv.AgentExecutor) a2a.AgentInterface {
	reqHandler := a2asrv.NewHandler(executor, a2asrv.WithCallInterceptors(interceptor))
	server := httptest.NewServer(a2asrv.NewJSONRPCHandler(reqHandler))
	t.Cleanup(server.Close)
	return a2a.AgentInterface{URL: server.URL, ProtocolBinding: a2a.TransportProtocolJSONRPC}
}

func newAgentCard(endpoint a2a.AgentInterface, extensionURIs []string) *a2a.AgentCard {
	extensions := make([]a2a.AgentExtension, len(extensionURIs))
	for i, uri := range extensionURIs {
		extensions[i] = a2a.AgentExtension{URI: uri}
	}
	return &a2a.AgentCard{
		Capabilities: a2a.AgentCapabilities{Extensions: extensions},
		SupportedInterfaces: []a2a.AgentInterface{
			{URL: endpoint.URL, ProtocolBinding: endpoint.ProtocolBinding},
		},
	}
}

type proxyTarget struct {
	ac *a2a.AgentCard
	ai a2a.AgentInterface
}

func (pt proxyTarget) newClient(ctx context.Context, interceptor a2aclient.CallInterceptor) (*a2aclient.Client, error) {
	if pt.ac != nil {
		return a2aclient.NewFromCard(ctx, pt.ac, a2aclient.WithCallInterceptors(interceptor))
	}
	if pt.ai.URL != "" {
		return a2aclient.NewFromEndpoints(ctx, []a2a.AgentInterface{pt.ai}, a2aclient.WithCallInterceptors(interceptor))
	}
	return nil, fmt.Errorf("neither card nor agent interface provided")
}

func newProxyExecutor(interceptor a2aclient.CallInterceptor, target proxyTarget) a2asrv.AgentExecutor {
	return testexecutor.FromFunction(func(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
		return func(yield func(a2a.Event, error) bool) {
			client, err := target.newClient(ctx, interceptor)
			if err != nil {
				yield(nil, err)
				return
			}
			result, err := client.SendMessage(ctx, &a2a.SendMessageRequest{
				Message: a2a.NewMessage(a2a.MessageRoleUser, execCtx.Message.Parts...),
			})
			if err != nil {
				yield(nil, err)
				return
			}
			task, ok := result.(*a2a.Task)
			if !ok {
				yield(nil, fmt.Errorf("result was %T, want a2a.Task", task))
				return
			}
			task.ID = execCtx.TaskID
			task.ContextID = execCtx.ContextID
			yield(task, nil)
		}
	})
}
