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

// Package a2av0 contains 0.3-compatible agent card type definition.
package a2av0

import "github.com/a2aproject/a2a-go/v2/a2a"

// Version is the protocol version which SDK implements.
const Version a2a.ProtocolVersion = "0.3"

// RESTPathPrefix is the URL path segment prepended to all REST endpoints in
// the A2A v0.3 HTTP+JSON binding. For example, the v0.3 "message:send"
// endpoint is mounted at "/v1/message:send" (relative to the base URL from
// the agent card).
const RESTPathPrefix = "/v1"
