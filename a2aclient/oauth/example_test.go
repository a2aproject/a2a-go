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
	opts := oauth.SignInOptions{
		ShowCode: func(dc oauth.DeviceCode) {
			fmt.Printf("To authorize, open %s and enter %s\n", dc.VerificationURI, dc.UserCode)
		},
	}

	card, err := resolveCard(ctx, m, opts, "https://agent.example.com")
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	client, err := a2aclient.NewFromCard(ctx, card, a2aclient.WithPlugins(m.Plugin()))
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	req := &a2a.SendMessageRequest{Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hi"))}
	_, err = client.SendMessage(ctx, req)
	var sr *oauth.SignInRequiredError
	if errors.As(err, &sr) {
		if _, err := m.SignIn(ctx, sr.Requirement, opts); err != nil {
			fmt.Println("Error:", err)
			return
		}
		_, err = client.SendMessage(ctx, req)
	}
	if err != nil {
		fmt.Println("Error:", err)
	}
}

// resolveCard fetches an agent card, with a stored token when there is one,
// and signs the user in first when the card is protected.
func resolveCard(ctx context.Context, m *oauth.Manager, opts oauth.SignInOptions, agentURL string) (*a2a.AgentCard, error) {
	cardURL := agentURL + "/.well-known/agent-card.json"
	resolve := func() (*a2a.AgentCard, error) {
		token, err := m.Token(ctx, cardURL)
		if err != nil {
			return agentcard.DefaultResolver.Resolve(ctx, agentURL)
		}
		return agentcard.DefaultResolver.Resolve(ctx, agentURL, agentcard.WithRequestHeader("Authorization", "Bearer "+token))
	}
	card, err := resolve()
	if err == nil {
		return card, nil
	}
	req, derr := m.Discover(ctx, cardURL, nil)
	if derr != nil {
		return nil, derr
	}
	if req == nil {
		return nil, err
	}
	if _, err := m.SignIn(ctx, req, opts); err != nil {
		return nil, err
	}
	return resolve()
}
