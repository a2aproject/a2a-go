// Copyright 2025 The A2A Authors
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

/*
Package a2aclient provides a transport-agnostic A2A client implementation. Under the hood it handles
transport protocol negotiation and connection establishment.

A [Client] can be configured with [CallInterceptor] middleware and custom
transports.

If a client is created in multiple places, a [Factory] can be used to share the common configuration options:

	factory := NewFactory(
		WithConfig(&a2aclient.Config{...}),
		WithInterceptors(loggingInterceptor),
		WithGRPCTransport(customGRPCOptions)
	)

A client can be created from an [a2a.AgentCard] or a list of known [a2a.AgentInterface] descriptions
using either package-level functions or factory methods.

	client, err := factory.CreateFromEndpoints(ctx, []a2a.AgentInterface{URL: url, Transport: a2a.TransportProtocolGRPC})

	// or

	card, err :=  agentcard.Fetch(ctx, url)
	if err != nil {
		log.Fatalf("Failed to resolve an AgentCard: %v", err)
	}
	client, err := a2aclient.NewFromCard(ctx, card, WithInterceptors(&customInterceptor{}))
*/
package a2aclient
