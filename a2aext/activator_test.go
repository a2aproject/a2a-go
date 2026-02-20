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
	"iter"
	"maps"
	"net/http/httptest"
	"testing"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2aclient"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/a2aproject/a2a-go/internal/testutil/testexecutor"
	"github.com/google/go-cmp/cmp"
)

func TestActivator(t *testing.T) {
	tests := []struct {
		name           string
		clientWants    []string
		serverSupports []string
		clientSends    []string
	}{
		{
			name:           "no extensions configured",
			clientWants:    []string{},
			serverSupports: []string{"ext1"},
			clientSends:    nil,
		},
		{
			name:           "server does not support extensions",
			clientWants:    []string{"ext1"},
			serverSupports: []string{},
			clientSends:    nil,
		},
		{
			name:           "server supports extension",
			clientWants:    []string{"ext1"},
			serverSupports: []string{"ext1"},
			clientSends:    []string{"ext1"},
		},
		{
			name:           "server supports subset of extensions",
			clientWants:    []string{"ext1", "ext2"},
			serverSupports: []string{"ext1"},
			clientSends:    []string{"ext1"},
		},
		{
			name:           "server supports all extensions",
			clientWants:    []string{"ext1", "ext2"},
			serverSupports: []string{"ext1", "ext2"},
			clientSends:    []string{"ext1", "ext2"},
		},
		{
			name:           "not supported extensions ignored",
			clientWants:    []string{"ext1", "ext2"},
			serverSupports: []string{"ext3"},
			clientSends:    nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()

			gotHeaders := map[string][]string{}
			captureExecutor := testexecutor.FromFunction(
				func(ctx context.Context, ec *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
					return func(yield func(a2a.Event, error) bool) {
						maps.Insert(gotHeaders, ec.ServiceParams.List())
						event := a2a.NewSubmittedTask(ec, ec.Message)
						event.Status = a2a.TaskStatus{State: a2a.TaskStateCompleted}
						yield(event, nil)
					}
				},
			)

			agentCard := startServerWithExtensions(t, captureExecutor, tc.serverSupports)
			activator := NewActivator(tc.clientWants...)
			client, err := a2aclient.NewFromCard(
				ctx,
				agentCard,
				a2aclient.WithCallInterceptors(activator),
			)
			if err != nil {
				t.Fatalf("a2aclient.NewFromEndpoints() error = %v", err)
			}

			_, err = client.SendMessage(ctx, &a2a.SendMessageRequest{
				Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("verify extensions")),
			})
			if err != nil {
				t.Fatalf("client.SendMessage() error = %v", err)
			}

			var gotExtensions []string
			if vals, ok := gotHeaders[ServiceParamsKey]; ok {
				gotExtensions = vals
			}
			if diff := cmp.Diff(tc.clientSends, gotExtensions); diff != "" {
				t.Errorf("wrong extension headers (+got,-want), diff = %s", diff)
			}
		})
	}
}

func startServerWithExtensions(t *testing.T, executor a2asrv.AgentExecutor, extensionURIs []string) *a2a.AgentCard {
	var extensions []a2a.AgentExtension
	for _, uri := range extensionURIs {
		extensions = append(extensions, a2a.AgentExtension{URI: uri})
	}

	reqHandler := a2asrv.NewHandler(executor)
	server := httptest.NewServer(a2asrv.NewJSONRPCHandler(reqHandler))
	t.Cleanup(server.Close)

	card := &a2a.AgentCard{
		Capabilities: a2a.AgentCapabilities{Extensions: extensions},
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(server.URL, a2a.TransportProtocolJSONRPC),
		},
	}
	return card
}
