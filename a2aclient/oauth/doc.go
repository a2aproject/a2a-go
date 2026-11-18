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
// A [Manager] holds the client registrations and a [Store] of grants. Install
// its [Manager.Plugin] on an A2A client:
//
//	m := &oauth.Manager{
//		Clients: []oauth.Registration{{Authority: "https://login.example.com", ClientID: "my-client-id"}},
//		Store:   store,
//	}
//	client, err := a2aclient.NewFromCard(ctx, card, a2aclient.WithPlugins(m.Plugin()))
//
// The plugin attaches a stored access token to calls to agents whose card asks
// for OAuth, and to calls to agents a stored grant covers, refreshing the token
// when it is about to expire. When the agent rejects a token with a Bearer
// challenge, the plugin refreshes it and repeats the call once. When a call
// needs a grant that is not stored, or more scopes than the stored one has, it
// fails with a [*SignInRequiredError] that names the authorization server, the
// resource and the scopes; when the agent answered, the error wraps the
// agent's, so errors.Is and errors.As still find it. The application then
// signs the user in with [Manager.SignIn], which runs the authorization code
// flow with a loopback redirect or the device authorization grant, or with
// [Manager.BeginSignIn] and [Manager.CompleteSignIn] when it receives the
// redirect itself, and repeats the call. When the agent asks for more scopes,
// the new grant replaces the narrower one.
//
// An agent card request is not an A2A call: fetch a protected card with a token
// from [Manager.Token], and use [Manager.Discover] without a card to find where
// to sign in for it. [Manager.Discover] also finds where to sign in for an agent
// before calling it, and [Manager.Revoke] signs the user out.
//
// The plugin reads the challenges of a refused call from the error's
// [a2a.Error] service parameters, which the JSON-RPC, REST and gRPC client
// transports fill. Over gRPC, stored tokens are attached whatever the address;
// the transport security of the gRPC connection is up to the application.
//
// The plugin leaves alone calls that already carry an Authorization service
// parameter. It runs before the client's call interceptors, so it doesn't see
// an Authorization an interceptor sets, such as an [a2aclient.AuthInterceptor]
// that replaces the grant: a 401 for that credential costs at most one refresh
// before the call fails with a [*SignInRequiredError]. Provide credentials that
// should win over OAuth with [a2aclient.AttachServiceParams], or in a plugin
// installed before the Manager's.
//
// Discovery follows only what an agent may point to: at most five
// authorization servers from its Protected Resource Metadata, and no metadata
// or authorization server on a loopback host unless the agent is on one too.
// Tokens are sent only over https or to loopback hosts, and only to URLs the
// grant's resource covers. The plugin doesn't see HTTP redirects, on which the
// HTTP client keeps the Authorization header within a host.
//
// A Manager and its Store hold the grants of one user; use one Manager per
// user. Its errors are a [*SignInRequiredError] when the user has to sign in, a
// [*NoClientError] or an [*AmbiguousClientError] when no single client
// registration can be used with an authorization server, and a [*FailureError]
// when discovery, metadata validation, the store, its lock or a refresh fails.
// Authorization servers report sign-in failures as an [*Error].
//
// An agent can tell a client where to obtain an access token in two ways: its
// agent card's securitySchemes and securityRequirements, read before the first
// request, and a Bearer challenge on a 401 response, which leads to the agent's
// Protected Resource Metadata (RFC 9728) and on to the authorization server's
// metadata (RFC 8414 or OpenID Connect Discovery). Either way the result is a
// [Requirement]: the authorization server [Endpoints], the resource indicator
// (RFC 8707) and the scopes the token must cover. The Manager obtains a [Grant]
// with the authorization code flow with PKCE (RFC 7636) or the device
// authorization grant (RFC 8628), refreshes it and revokes it (RFC 7009).
//
// A [Store] keeps grants between calls by [Grant.ID]; [MemoryStore] keeps them
// for the life of the process. The Manager refreshes and writes grants under
// its own lock and then its Locker, if it has one, so that users sharing a
// store spend a refresh token the server rotates only once. Give Managers in
// different processes that share a store a [Locker]; the context passed to
// Locker.Lock bounds only the wait for the lock, and the store's Get, Put and
// Delete are called with the lock held. Waiting for the lock ends when the
// caller's context does. Once the lock is held, refreshes and the writes that
// keep their result are not canceled with the caller's context, so a rotated
// refresh token is not lost.
package oauth
