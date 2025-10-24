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
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
)

// AgentExecutor implementations translate agent outputs to A2A events.
type AgentExecutor interface {
	// Execute invokes an agent with the provided context and translates agent outputs
	// into A2A events writing them to the provided event queue.
	//
	// Returns an error if agent invocation failed.
	Execute(ctx context.Context, reqCtx *RequestContext, queue eventqueue.Queue) error

	// Cancel requests the agent to stop processing an ongoing task.
	//
	// The agent should attempt to gracefully stop the task identified by the
	// task ID in the request context and publish a TaskStatusUpdateEvent with
	// state TaskStateCanceled to the event queue.
	//
	// Returns an error if the cancelation request cannot be processed.
	Cancel(ctx context.Context, reqCtx *RequestContext, queue eventqueue.Queue) error
}

// AgentCardProducer creates an AgentCard instances used for agent discovery and capability negotiation.
type AgentCardProducer interface {
	// Card returns a self-describing manifest for an agent. It provides essential
	// metadata including the agent's identity, capabilities, skills, supported
	// communication methods, and security requirements and is publicly available.
	Card(ctx context.Context) *a2a.AgentCard
}

// AgentCardProducerFn is a function type which implements AgentCardProducer.
type AgentCardProducerFn func(ctx context.Context) *a2a.AgentCard

func (fn AgentCardProducerFn) Card(ctx context.Context) *a2a.AgentCard {
	return fn(ctx)
}

// ExtendedAgentCardProducer can create both public agent cards and cards available to authenticated users only.
type ExtendedAgentCardProducer interface {
	AgentCardProducer

	// ExtendedCard returns a manifest for an agent which is only available to authenticated users.
	ExtendedCard(ctx context.Context) *a2a.AgentCard
}

// StaticAgentCard represents a configuration of [AgentCardProducer] implementation which returns
// [a2a.AgentCard] objects which are configured once and remain static while the program running.
// It used as input to [NewStaticAgentCardProducer].
type StaticAgentCard struct {
	// Public is an agent card available to unauthenticated clients.
	Public *a2a.AgentCard
	// Extended is an agent card available to authorized clients.
	Extended *a2a.AgentCard
}

type staticAgentCardProducer struct {
	card *StaticAgentCard
}

// NewStaticAgentCardProducer creates an [AgentCardProducer] implementation which serves [a2a.AgentCard] objects
// objects which are configured once and remain static while the program running.
// NewStaticAgentCardProducer will automatically set SupportsAuthenticatedExtendedCard flag on Public card if Extended card is provided.
func NewStaticAgentCardProducer(card StaticAgentCard) ExtendedAgentCardProducer {
	if card.Extended != nil {
		publicCopy := *card.Public
		publicCopy.SupportsAuthenticatedExtendedCard = true
		card.Public = &publicCopy
	}
	return &staticAgentCardProducer{card: &card}
}

func (p *staticAgentCardProducer) Card(ctx context.Context) *a2a.AgentCard {
	return p.card.Public
}

func (p *staticAgentCardProducer) ExtendedCard(ctx context.Context) *a2a.AgentCard {
	if p.card.Extended != nil {
		return p.card.Extended
	}
	return p.card.Public
}
