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

package oauth_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aclient/agentcard"
	"github.com/a2aproject/a2a-go/v2/a2aclient/oauth"
)

// A client that signs the user in when an agent asks for authorization. The
// example only compiles: it needs a real agent and authorization server, and
// the oauthtest ones require a testing.TB.
func ExampleManager() {
	ctx := context.Background()
	m := &oauth.Manager{
		Clients: []oauth.Registration{{Authority: "https://login.example.com", ClientID: "my-client-id"}},
	}
	httpClient := &http.Client{Transport: m.Transport(nil)}

	card, err := resolveCard(ctx, m, agentcard.NewResolver(httpClient), "https://agent.example.com")
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	client, err := a2aclient.NewFromCard(ctx, card,
		a2aclient.WithJSONRPCTransport(httpClient),
		a2aclient.WithRESTTransport(httpClient),
		a2aclient.WithCallInterceptors(m.Interceptor()),
	)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	req := &a2a.SendMessageRequest{Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hi"))}
	_, err = client.SendMessage(ctx, req)
	var ir *oauth.InteractionRequiredError
	if errors.As(err, &ir) {
		if err := signIn(ctx, m, ir.Requirement); err != nil {
			fmt.Println("Error:", err)
			return
		}
		_, err = client.SendMessage(ctx, req)
	}
	if err != nil {
		fmt.Println("Error:", err)
	}
}

// resolveCard fetches an agent card, signing the user in first when the card
// itself is protected.
func resolveCard(ctx context.Context, m *oauth.Manager, resolver *agentcard.Resolver, agentURL string) (*a2a.AgentCard, error) {
	card, err := resolver.Resolve(ctx, agentURL)
	if err == nil {
		return card, nil
	}
	cardURL, perr := url.Parse(agentURL + "/.well-known/agent-card.json")
	if perr != nil {
		return nil, perr
	}
	req, rerr := m.RequirementForError(ctx, cardURL, err)
	if rerr != nil {
		return nil, rerr
	}
	if req == nil {
		return nil, err
	}
	if err := signIn(ctx, m, req); err != nil {
		return nil, err
	}
	return resolver.Resolve(ctx, agentURL)
}

func signIn(ctx context.Context, m *oauth.Manager, req *oauth.Requirement) error {
	da, err := m.StartDeviceAuthorization(ctx, req, nil)
	if err != nil {
		return err
	}
	fmt.Printf("To authorize, open %s and enter %s\n", da.VerificationURI, da.UserCode)
	_, err = m.PollDeviceAuthorization(ctx, da)
	return err
}
