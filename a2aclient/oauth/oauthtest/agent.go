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

package oauthtest

import (
	"context"
	"crypto/x509"
	"fmt"
	"iter"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

// CardSecurity selects what the agent card declares about authorization.
type CardSecurity int

const (
	// CardNone declares nothing; clients learn about OAuth from the 401 challenge.
	CardNone CardSecurity = iota
	// CardMetadataURL declares an oauth2 scheme with oauth2MetadataUrl.
	CardMetadataURL
	// CardAuthCode declares an oauth2 scheme with an inline authorizationCode flow.
	CardAuthCode
	// CardDeviceCode declares an oauth2 scheme with an inline deviceCode flow.
	CardDeviceCode
	// CardOIDC declares an openIdConnect scheme.
	CardOIDC
)

// AgentOptions configure a [ProtectedAgent].
type AgentOptions struct {
	// Card selects the security the agent card declares.
	Card CardSecurity
	// ProtectCard requires a token for the agent card too.
	ProtectCard bool
	// OmitResourceMetadata leaves resource_metadata out of the challenge, so
	// clients have to probe the well-known location.
	OmitResourceMetadata bool
	// Scopes are required by the agent and named in the card and the challenge.
	Scopes []string
	// JSONRPC serves the JSON-RPC binding instead of REST.
	JSONRPC bool
	// TLS serves https with the certificate returned by [ProtectedAgent.Certificate].
	TLS bool
	// BasePath mounts the agent below a path, as in "/a2a/v1". Its resource
	// identifier is the server URL with the path, and its Protected Resource
	// Metadata is served only at the RFC 9728 location with the path inserted.
	// The agent card stays at the origin.
	BasePath string
}

// ProtectedAgent is an A2A echo agent that requires access tokens from an [AuthServer].
type ProtectedAgent struct {
	// URL is the agent's base URL, which is also its resource identifier.
	URL string

	as          *AuthServer
	opts        AgentOptions
	srv         *httptest.Server
	metadataURL string

	mu      sync.Mutex
	headers []string
}

// NewProtectedAgent starts an agent protected by as that is stopped with the test.
func NewProtectedAgent(t testing.TB, as *AuthServer, opts AgentOptions) *ProtectedAgent {
	t.Helper()
	a := &ProtectedAgent{as: as, opts: opts}
	handler := a2asrv.NewHandler(echoExecutor{}, a2asrv.WithCapabilityChecks(&a2a.AgentCapabilities{Streaming: true}))
	mux := http.NewServeMux()
	a.srv = startServer(t, mux, opts.TLS)
	base := strings.TrimSuffix(opts.BasePath, "/")
	a.URL = a.srv.URL + base
	prmPath := "/.well-known/oauth-protected-resource" + base
	a.metadataURL = a.srv.URL + prmPath

	protocol := a2a.TransportProtocolHTTPJSON
	binding := a2asrv.NewRESTHandler(handler)
	if opts.JSONRPC {
		protocol = a2a.TransportProtocolJSONRPC
		binding = a2asrv.NewJSONRPCHandler(handler)
	}
	card := a.card(protocol)
	cardHandler := a2asrv.NewStaticAgentCardHandler(card)
	if opts.ProtectCard {
		cardHandler = a.protect(cardHandler)
	}
	mux.Handle(a2asrv.WellKnownAgentCardPath, cardHandler)
	mux.HandleFunc("GET "+prmPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(r.Context(), w, http.StatusOK, map[string]any{
			"resource":              a.URL,
			"authorization_servers": []string{as.URL},
			"scopes_supported":      opts.Scopes,
		})
	})
	if base == "" {
		mux.Handle("/", a.protect(binding))
		return a
	}
	mux.Handle(base, a.protect(http.StripPrefix(base, binding)))
	mux.Handle(base+"/", a.protect(http.StripPrefix(base, binding)))
	mux.Handle("/", a.protect(http.NotFoundHandler()))
	return a
}

func (a *ProtectedAgent) card(protocol a2a.TransportProtocol) *a2a.AgentCard {
	card := &a2a.AgentCard{
		Name:                "Protected Echo",
		Version:             "1.0.0",
		Capabilities:        a2a.AgentCapabilities{Streaming: true},
		SupportedInterfaces: []*a2a.AgentInterface{a2a.NewAgentInterface(a.URL, protocol)},
		DefaultInputModes:   []string{"text"},
		DefaultOutputModes:  []string{"text"},
		Skills:              []a2a.AgentSkill{},
	}
	scopes := map[string]string{}
	for _, s := range a.opts.Scopes {
		scopes[s] = s
	}
	var scheme a2a.SecurityScheme
	switch a.opts.Card {
	case CardMetadataURL:
		scheme = a2a.OAuth2SecurityScheme{
			Oauth2MetadataURL: a.as.MetadataURL(),
			Flows:             a2a.AuthorizationCodeOAuthFlow{AuthorizationURL: a.as.AuthorizationURL(), TokenURL: a.as.URL + "/token", Scopes: scopes},
		}
	case CardAuthCode:
		scheme = a2a.OAuth2SecurityScheme{
			Flows: a2a.AuthorizationCodeOAuthFlow{AuthorizationURL: a.as.AuthorizationURL(), TokenURL: a.as.URL + "/token", Scopes: scopes, PKCERequired: true},
		}
	case CardDeviceCode:
		scheme = a2a.OAuth2SecurityScheme{
			Flows: a2a.DeviceCodeOAuthFlow{DeviceAuthorizationURL: a.as.URL + "/device", TokenURL: a.as.URL + "/token", Scopes: scopes},
		}
	case CardOIDC:
		scheme = a2a.OpenIDConnectSecurityScheme{OpenIDConnectURL: a.as.OpenIDConfigurationURL()}
	default:
		return card
	}
	card.SecuritySchemes = a2a.NamedSecuritySchemes{"oauth": scheme}
	card.SecurityRequirements = a2a.SecurityRequirementsOptions{{"oauth": a2a.SecuritySchemeScopes(a.opts.Scopes)}}
	return card
}

func (a *ProtectedAgent) protect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		a.mu.Lock()
		a.headers = append(a.headers, auth)
		a.mu.Unlock()
		token, ok := strings.CutPrefix(auth, "Bearer ")
		if ok && a.as.Valid(token, a.URL, a.opts.Scopes) {
			next.ServeHTTP(w, r)
			return
		}
		var params []string
		if !a.opts.OmitResourceMetadata {
			params = append(params, fmt.Sprintf("resource_metadata=%q", a.metadataURL))
		}
		if len(a.opts.Scopes) > 0 {
			params = append(params, fmt.Sprintf("scope=%q", strings.Join(a.opts.Scopes, " ")))
		}
		if auth != "" {
			params = append(params, `error="invalid_token"`)
		}
		challenge := "Bearer"
		if len(params) > 0 {
			challenge += " " + strings.Join(params, ", ")
		}
		w.Header().Set("WWW-Authenticate", challenge)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}

// AuthorizationHeaders returns the Authorization header of every request the
// agent received, empty for requests without one.
func (a *ProtectedAgent) AuthorizationHeaders() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.headers...)
}

// Certificate returns the agent's TLS certificate, or nil without
// [AgentOptions.TLS].
func (a *ProtectedAgent) Certificate() *x509.Certificate { return a.srv.Certificate() }

type echoExecutor struct{}

func (echoExecutor) Execute(_ context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		if execCtx.StoredTask == nil {
			if !yield(a2a.NewSubmittedTask(execCtx, execCtx.Message), nil) {
				return
			}
		}
		var text []string
		for _, p := range execCtx.Message.Parts {
			text = append(text, p.Text())
		}
		evt := a2a.NewArtifactEvent(execCtx, a2a.NewTextPart(strings.Join(text, " ")))
		evt.LastChunk = true
		if !yield(evt, nil) {
			return
		}
		yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCompleted, nil), nil)
	}
}

func (echoExecutor) Cancel(_ context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCanceled, nil), nil)
	}
}
