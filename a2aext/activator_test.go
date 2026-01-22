package a2aext

import (
	"context"
	"maps"
	"net/http/httptest"
	"testing"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2aclient"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
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
				func(ctx context.Context, rc *a2asrv.RequestContext, q eventqueue.Queue) error {
					if callCtx, ok := a2asrv.CallContextFrom(ctx); ok {
						maps.Insert(gotHeaders, callCtx.RequestMeta().List())
					}
					event := a2a.NewStatusUpdateEvent(rc, a2a.TaskStateCompleted, nil)
					event.Final = true
					return q.Write(ctx, event)
				},
			)

			agentCard := startServerWithExtensions(t, captureExecutor, tc.serverSupports)
			activator := NewActivator(tc.clientWants...)
			client, err := a2aclient.NewFromCard(
				ctx,
				agentCard,
				a2aclient.WithInterceptors(activator),
			)
			if err != nil {
				t.Fatalf("a2aclient.NewFromEndpoints() error = %v", err)
			}

			_, err = client.SendMessage(ctx, &a2a.MessageSendParams{
				Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: "verify extensions"}),
			})
			if err != nil {
				t.Fatalf("client.SendMessage() error = %v", err)
			}

			var gotExtensions []string
			if vals, ok := gotHeaders[CallMetaKey]; ok {
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
	card := &a2a.AgentCard{
		Capabilities: a2a.AgentCapabilities{
			Extensions: extensions,
		},
	}
	reqHandler := a2asrv.NewHandler(executor)
	server := httptest.NewServer(a2asrv.NewJSONRPCHandler(reqHandler))
	card.URL = server.URL
	card.PreferredTransport = a2a.TransportProtocolJSONRPC
	t.Cleanup(server.Close)
	return card
}
