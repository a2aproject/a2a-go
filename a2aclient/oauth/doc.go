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

// Package oauth adds OAuth 2.0 support to A2A clients.
//
// A [Manager] holds the client registrations and a [Store] of grants. Its
// [Manager.Interceptor] attaches a stored access token to calls to agents whose
// card asks for OAuth, and to calls to agents covered by a stored grant,
// refreshing the token when it is about to expire. Its [Manager.Transport]
// attaches the grants obtained in this process to the HTTP requests below them
// and handles 401 responses: it refreshes a token the agent rejected, or follows
// the Bearer challenge of an agent whose card did not mention OAuth to a stored
// grant, and retries once. Otherwise the 401 reaches the interceptor, which
// turns it into an [*InteractionRequiredError] when the agent uses OAuth. Install
// both, with the transport in every HTTP client, the agent card resolver's
// included:
//
//	m := &oauth.Manager{
//		Clients: []oauth.Registration{{Authority: "https://login.example.com", ClientID: "my-client-id"}},
//		Store:   store,
//	}
//	httpClient := &http.Client{Transport: m.Transport(nil)}
//	card, err := agentcard.NewResolver(httpClient).Resolve(ctx, agentURL)
//	...
//	client, err := a2aclient.NewFromCard(ctx, card,
//		a2aclient.WithJSONRPCTransport(httpClient),
//		a2aclient.WithRESTTransport(httpClient),
//		a2aclient.WithCallInterceptors(m.Interceptor()),
//	)
//
// When a call needs a grant that is not stored, it fails with an
// [*InteractionRequiredError] that names the authorization server, the resource
// and the scopes. When the agent answered 401, the error wraps the error of the
// response, so errors.Is and errors.As still find it. The interceptor learns of
// the 401 from the transport, so install both.
// The application then signs the user in with [Manager.NewAuthorizationRequest]
// and [Manager.ExchangeCode], or with [Manager.StartDeviceAuthorization] and
// [Manager.PollDeviceAuthorization], and repeats the call. When the agent asked
// for more scopes, the new grant replaces the narrower one. An agent card request
// is not an A2A call: pass its error to [Manager.RequirementForError] to find out
// whether the card is protected; the transport in the resolver's HTTP client
// keeps the challenge of the 401 for it. [Manager.RequirementForURL] finds where
// to sign in for an agent before calling it.
//
// Over gRPC only the interceptor's work before a call applies: stored tokens are
// attached and refreshed before they expire, but a token the agent rejects is
// not refreshed and a challenge is not followed, since gRPC errors carry no
// response headers. The interceptor sends tokens to gRPC addresses whatever
// their scheme; the transport security of the gRPC connection is up to the
// application.
//
// The interceptor leaves alone calls that already carry an Authorization
// service parameter. Placed after an [a2aclient.AuthInterceptor], it fills in
// for the calls that one has no credential for. Placed before, it attaches the
// OAuth grant, or fails the call when the card requires OAuth and no grant is
// stored, and the AuthInterceptor then replaces the grant with its own
// credential when it has one; a 401 for that credential is left as it is.
//
// The transport never adds credentials to the requests [http.Client] makes to
// follow a redirect, so an agent can't redirect a call to another agent the
// user has a grant for. Discovery follows only what an agent may point to: at
// most five authorization servers from its Protected Resource Metadata, and no
// metadata or authorization server on a loopback host unless the agent is on
// one too.
//
// A Manager and its Store hold the grants of one user; use one Manager per
// user. Its errors are a [*NoClientError] or an [*AmbiguousClientError] when no
// single client registration can be used with an authorization server, an
// [*InteractionRequiredError] when the user has to sign in, and a
// [*FailureError] when discovery, metadata validation, the store, its lock or a
// refresh fails. Authorization servers report sign-in failures as an [*Error].
//
// The parts are also usable on their own. An agent can tell a client where to
// obtain an access token in two ways: its agent card's securitySchemes and
// securityRequirements, read before the first request, and a Bearer challenge on
// a 401 response, which leads to the agent's Protected Resource Metadata
// (RFC 9728) and on to the authorization server's metadata (RFC 8414 or OpenID
// Connect Discovery). [Manager.RequirementForCard] and
// [Manager.RequirementForChallenge] resolve them into a [Requirement]: the
// authorization server endpoints, the resource indicator (RFC 8707) and the
// scopes the token must cover. The Manager obtains a [Grant] with the
// authorization code flow with PKCE (RFC 7636) or the device authorization grant
// (RFC 8628), refreshes it and revokes it (RFC 7009). It implements the protocol
// only: the application shows the user where to go, and for the authorization
// code flow receives the redirect, typically on a loopback address (RFC 8252).
//
// A [Store] keeps grants between calls; [MemoryStore] keeps them for the life of
// the process. The Manager refreshes and writes grants under its own lock and
// then the store's [Locker], if it has one, so that callers sharing a store
// spend a refresh token the server rotates only once. A store shared between
// processes should implement [Locker]; the context passed to Locker.Lock bounds
// only the wait for the lock, and the store's Get, Put and Delete are called
// with the lock held. Waiting for the lock ends when the caller's context does.
// Once the lock is held, refreshes and the writes that keep their result are
// not canceled with the caller's context, so a rotated refresh token is not
// lost.
package oauth
