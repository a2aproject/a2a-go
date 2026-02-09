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

package e2e

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2aclient"
	"github.com/a2aproject/a2a-go/a2aext"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/a2aproject/a2a-go/internal/testutil/testexecutor"
)

// durationsExtension if requested instructs a server to time call execution duration.
var durationsExtension = a2a.AgentExtension{
	URI:         "https://example.com/a2aproject/durations/v1",
	Description: "Adds timing information to responses if activated",
}

type durationKeyType struct{}

// durationTracker implements the extension by providing an [a2asrv.CallInterceptor].
type durationTracker struct{}

func (s *durationTracker) Before(ctx context.Context, callCtx *a2asrv.CallContext, req *a2asrv.Request) (context.Context, any, error) {
	extensions, ok := a2asrv.ExtensionsFrom(ctx)
	if !ok {
		return ctx, nil, nil
	}
	if !extensions.Requested(&durationsExtension) {
		return ctx, nil, nil
	}
	extensions.Activate(&durationsExtension)
	return context.WithValue(ctx, durationKeyType{}, time.Now()), nil, nil
}

func (s *durationTracker) After(ctx context.Context, callCtx *a2asrv.CallContext, resp *a2asrv.Response) error {
	callStart, ok := ctx.Value(durationKeyType{}).(time.Time)
	if !ok {
		return nil
	}
	mc, ok := resp.Payload.(a2a.MetadataCarrier)
	if !ok {
		return nil
	}
	mc.SetMeta(durationsExtension.URI, map[string]any{
		"duration_ms": time.Since(callStart).Milliseconds(),
	})
	return nil
}

func TestDurationsExtension(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	testCases := []struct {
		name            string
		serverDeclares  []a2a.AgentExtension
		activatorConfig []string
		wantDuration    bool
	}{
		{
			name:            "server supports and client requests",
			serverDeclares:  []a2a.AgentExtension{durationsExtension},
			activatorConfig: []string{durationsExtension.URI},
			wantDuration:    true,
		},
		{
			name:            "server does not declare - client does not request",
			serverDeclares:  []a2a.AgentExtension{},
			activatorConfig: []string{durationsExtension.URI},
			wantDuration:    false,
		},
		{
			name:            "server declares - client does not want",
			serverDeclares:  []a2a.AgentExtension{durationsExtension},
			activatorConfig: []string{durationsExtension.URI + "++"},
			wantDuration:    false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			serverCard := &a2a.AgentCard{
				PreferredTransport: a2a.TransportProtocolJSONRPC,
				Capabilities:       a2a.AgentCapabilities{Extensions: tc.serverDeclares},
			}

			agentExecutor := testexecutor.FromEventGenerator(func(reqCtx *a2asrv.RequestContext) []a2a.Event {
				return []a2a.Event{a2a.NewMessage(a2a.MessageRoleAgent, reqCtx.Message.Parts...)}
			})
			handler := a2asrv.NewHandler(agentExecutor, a2asrv.WithCallInterceptors(&durationTracker{}))

			server := httptest.NewServer(a2asrv.NewJSONRPCHandler(handler))
			serverCard.URL = server.URL
			defer server.Close()

			client, err := a2aclient.NewFromCard(ctx, serverCard, a2aclient.WithInterceptors(
				a2aext.NewActivator(tc.activatorConfig...),
			))
			if err != nil {
				t.Fatalf("a2aclient.NewFromCard() error = %v", err)
			}
			result, err := client.SendMessage(ctx, &a2a.MessageSendParams{
				Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: "ping"}),
			})
			if err != nil {
				t.Fatalf("SendMessage failed: %v", err)
			}
			mc := result.Meta()
			_, hasMeta := mc[durationsExtension.URI]
			if tc.wantDuration != hasMeta {
				t.Fatalf("client.SendMessage() meta = %v, want duration %v", result.Meta(), tc.wantDuration)
			}
		})
	}
}
