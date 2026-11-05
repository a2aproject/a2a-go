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
// An agent can tell a client where to obtain an access token in two ways: its agent
// card's securitySchemes and securityRequirements, read before the first request,
// and a Bearer challenge on a 401 response, which leads to the agent's Protected
// Resource Metadata (RFC 9728) and on to the authorization server's metadata
// (RFC 8414 or OpenID Connect Discovery). Either way the result is a
// [Requirement]: the authorization server [Endpoints], the resource indicator
// (RFC 8707) and the scopes the token must cover. A [Registration] names the
// client ID to use with an authorization server.
package oauth
