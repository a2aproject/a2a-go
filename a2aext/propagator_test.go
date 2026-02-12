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
	"maps"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2aclient"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
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
		clientReqMeta         map[string]any
		clientReqHeaders      map[string][]string
		wantPropagatedMeta    map[string]any
		wantPropagatedHeaders map[string][]string
	}{
		{
			name: "default propagation affects extensions",
			clientReqMeta: map[string]any{
				"extension1.com": "bar",
				"extension2.com": map[string]string{"nested": "bar"},
				"not-extension":  "qux",
			},
			clientReqHeaders: map[string][]string{
				ServiceParamsKey: {"extension1.com", "extension2.com"},
				"x-ignore":  {"ignored"},
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
			clientReqMeta: map[string]any{
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

			var gotReqCtx *a2asrv.RequestContext
			gotHeaders := map[string][]string{}
			server := startServer(t, serverInterceptor, testexecutor.FromFunction(
				func(ctx context.Context, rc *a2asrv.RequestContext, q eventqueue.Queue) error {
					if callCtx, ok := a2asrv.CallContextFrom(ctx); ok {
						maps.Insert(gotHeaders, callCtx.RequestMeta().List())
					}
					gotReqCtx = rc

					event := a2a.NewStatusUpdateEvent(rc, a2a.TaskStateCompleted, nil)
					event.Final = true
					return q.Write(ctx, event)
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

			resp, err := client.SendMessage(ctx, &a2a.MessageSendParams{
				Message:  a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: "Hi!"}),
				Metadata: tc.clientReqMeta,
			})
			if err != nil {
				t.Fatalf("client.SendMessage() error = %v", err)
			}
			if task, ok := resp.(*a2a.Task); !ok || task.Status.State != a2a.TaskStateCompleted {
				t.Fatalf("client.SendMessage() = %v, want completed task", resp)
			}
			if diff := cmp.Diff(tc.wantPropagatedMeta, gotReqCtx.Metadata); diff != "" {
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
		clientReqMeta        map[string]any
		clientReqHeaders     map[string][]string
		serverASupports      []string
		serverBSupports      []string
		wantBReceivedMeta    map[string]any
		wantBReceivedHeaders map[string][]string
	}{
		{
			name: "serverB supports all extensions",
			clientReqMeta: map[string]any{
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
			clientReqMeta: map[string]any{
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

			var gotReqCtx *a2asrv.RequestContext
			gotHeaders := map[string][]string{}
			serverB := startServer(t, serverInterceptor, testexecutor.FromFunction(
				func(ctx context.Context, rc *a2asrv.RequestContext, q eventqueue.Queue) error {
					if callCtx, ok := a2asrv.CallContextFrom(ctx); ok {
						maps.Insert(gotHeaders, callCtx.RequestMeta().List())
					}
					gotReqCtx = rc

					event := a2a.NewStatusUpdateEvent(rc, a2a.TaskStateCompleted, nil)
					event.Final = true
					return q.Write(ctx, event)
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

			resp, err := client.SendMessage(ctx, &a2a.MessageSendParams{
				Message:  a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: "Hi!"}),
				Metadata: tc.clientReqMeta,
			})
			if err != nil {
				t.Fatalf("client.SendMessage() error = %v", err)
			}
			if task, ok := resp.(*a2a.Task); !ok || task.Status.State != a2a.TaskStateCompleted {
				t.Fatalf("client.SendMessage() = %v, want completed task", resp)
			}
			if diff := cmp.Diff(tc.wantBReceivedMeta, gotReqCtx.Metadata); diff != "" {
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
	reqHandler := a2asrv.NewHandler(executor, a2asrv.WithCallInterceptor(interceptor))
	server := httptest.NewServer(a2asrv.NewJSONRPCHandler(reqHandler))
	t.Cleanup(server.Close)
	return a2a.AgentInterface{URL: server.URL, Transport: a2a.TransportProtocolJSONRPC}
}

func newAgentCard(endpoint a2a.AgentInterface, extensionURIs []string) *a2a.AgentCard {
	extensions := make([]a2a.AgentExtension, len(extensionURIs))
	for i, uri := range extensionURIs {
		extensions[i] = a2a.AgentExtension{URI: uri}
	}
	return &a2a.AgentCard{
		URL:                endpoint.URL,
		PreferredTransport: endpoint.Transport,
		Capabilities: a2a.AgentCapabilities{
			Extensions: extensions,
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
	return testexecutor.FromFunction(func(ctx context.Context, reqCtx *a2asrv.RequestContext, q eventqueue.Queue) error {
		client, err := target.newClient(ctx, interceptor)
		if err != nil {
			return err
		}
		result, err := client.SendMessage(ctx, &a2a.MessageSendParams{
			Message: a2a.NewMessage(a2a.MessageRoleUser, reqCtx.Message.Parts...),
		})
		if err != nil {
			return err
		}
		task, ok := result.(*a2a.Task)
		if !ok {
			return fmt.Errorf("result was %T, want a2a.Task", task)
		}
		task.ID = reqCtx.TaskID
		task.ContextID = reqCtx.ContextID
		return q.Write(ctx, result)
	})
}
