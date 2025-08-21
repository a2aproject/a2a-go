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

package a2asrv

import (
	"context"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv/events"
)

// AgentExecutor implementations translate agent outputs to A2A events.
type AgentExecutor interface {
	// Execute invokes an agent with the provided context and translates agent outputs
	// into A2A events writing them to the provided event queue.
	//
	// Returns an error if agent invocation failed.
	Execute(ctx context.Context, reqCtx RequestContext, queue events.EventQueue) error

	// Cancel requests the agent to stop processing an ongoing task.
	//
	// The agent should attempt to gracefully stop the task identified by the
	// task ID in the request context and publish a TaskStatusUpdateEvent with
	// state TaskStateCanceled to the event queue.
	//
	// Returns an error if the cancellation request cannot be processed.
	Cancel(ctx context.Context, reqCtx RequestContext, queue events.EventQueue) error
}

// AgentCardProducer creates an AgentCard instances used for agent discovery and capability negotiation.
type AgentCardProducer interface {
	// Card returns a self-describing manifest for an agent. It provides essential
	// metadata including the agent's identity, capabilities, skills, supported
	// communication methods, and security requirements and is publicly available.
	Card() a2a.AgentCard
}

// ExtendedAgentCardProducer can create both public agent cards and cards available to authenticated users only.
type ExtendedAgentCardProducer interface {
	AgentCardProducer

	// ExtendedCard returns a manifest for an agent which is only available to authenticated users.
	ExtendedCard() a2a.AgentCard
}
