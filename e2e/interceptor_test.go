package e2e

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2aclient"
	"github.com/a2aproject/a2a-go/a2aclient/agentcard"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/a2aproject/a2a-go/internal/testutil/testexecutor"
)

func TestClient_InterceptorReplacesPayload(t *testing.T) {
	ctx := t.Context()

	// 1. Setup Server
	// The executor will verify it received the REPLACED request.
	executor := testexecutor.FromEventGenerator(func(reqCtx *a2asrv.RequestContext) []a2a.Event {
		// Verify the message content received by the server
		textPart := reqCtx.Message.Parts[0].(a2a.TextPart)
		if textPart.Text != "Replaced Request" {
			// Instead of failing here (which might be hard to see), return an error message
			return []a2a.Event{
				a2a.NewMessage(a2a.MessageRoleAgent, a2a.TextPart{Text: fmt.Sprintf("Error: Expected 'Replaced Request', got '%s'", textPart.Text)}),
			}
		}

		// Return a response that will be replaced by the client interceptor
		return []a2a.Event{
			a2a.NewMessage(a2a.MessageRoleAgent, a2a.TextPart{Text: "Original Response"}),
		}
	})
	reqHandler := a2asrv.NewHandler(executor)
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	cardConfig := &a2a.AgentCard{
		URL:                fmt.Sprintf("%s/invoke", server.URL),
		PreferredTransport: a2a.TransportProtocolJSONRPC,
		Capabilities:       a2a.AgentCapabilities{Streaming: false},
	}
	mux.Handle(a2asrv.WellKnownAgentCardPath, a2asrv.NewStaticAgentCardHandler(cardConfig))
	mux.Handle("/invoke", a2asrv.NewJSONRPCHandler(reqHandler))

	// 2. Setup Client with Interceptor

	card, err := agentcard.DefaultResolver.Resolve(ctx, server.URL)
	if err != nil {
		t.Fatalf("resolver.Resolve() error = %v", err)
	}

	interceptor := &payloadReplacerInterceptor{
		replaceReq: func(req *a2aclient.Request) {
			// Identify the call we want to modify
			if _, ok := req.Payload.(*a2a.MessageSendParams); ok {
				// Replace the request payload with a new object
				req.Payload = &a2a.MessageSendParams{
					Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: "Replaced Request"}),
				}
			}
		},
		replaceResp: func(resp *a2aclient.Response) {
			// Identify the response we want to modify
			if msg, ok := resp.Payload.(*a2a.Message); ok {
				// Check if this is the "Original Response" from the server
				if len(msg.Parts) > 0 && msg.Parts[0].(a2a.TextPart).Text == "Original Response" {
					// Replace the response payload
					resp.Payload = a2a.NewMessage(a2a.MessageRoleAgent, a2a.TextPart{Text: "Replaced Response"})
				}
			}
		},
	}

	client, err := a2aclient.NewFromCard(ctx, card)
	if err != nil {
		t.Fatalf("a2aclient.NewFromCard() error = %v", err)
	}
	client.AddCallInterceptor(interceptor)

	// 3. Execute and Verify

	// Send "Original Request"
	originalParam := &a2a.MessageSendParams{
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: "Original Request"}),
	}

	resp, err := client.SendMessage(ctx, originalParam)
	if err != nil {
		t.Fatalf("client.SendMessage() error = %v", err)
	}

	// Verify the client received the "Replaced Response"
	respMsg := resp.(*a2a.Message)
	if len(respMsg.Parts) == 0 {
		t.Fatal("Response message is empty")
	}
	respText := respMsg.Parts[0].(a2a.TextPart).Text

	if respText != "Replaced Response" {
		t.Errorf("Client received wrong response. Got '%s', Want 'Replaced Response'. (Note: If got 'Original Response', header replacement failed. If got 'Error: ...', request replacement failed)", respText)
	}
}

type payloadReplacerInterceptor struct {
	replaceReq  func(*a2aclient.Request)
	replaceResp func(*a2aclient.Response)
}

func (i *payloadReplacerInterceptor) Before(ctx context.Context, req *a2aclient.Request) (context.Context, error) {
	if i.replaceReq != nil {
		i.replaceReq(req)
	}
	return ctx, nil
}

func (i *payloadReplacerInterceptor) After(ctx context.Context, resp *a2aclient.Response) error {
	if i.replaceResp != nil {
		i.replaceResp(resp)
	}
	return nil
}
