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
	"encoding/json"
	"net/http"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/log"
)

// WellKnownAgentCardPath is the standard HTTP path for retrieving the agent card as defined in A2A spec.
const WellKnownAgentCardPath = "/.well-known/agent-card.json"

// AgentCardProducer creates public AgentCard instances used for agent discovery and capability negotiation.
type AgentCardProducer interface {
	// Card returns a self-describing manifest for an agent. It provides essential
	// metadata including the agent's identity, capabilities, skills, supported
	// communication methods, and security requirements.
	Card(ctx context.Context) (*a2a.AgentCard, error)
}

// AgentCardProducerFn is a function type which implements [AgentCardProducer].
type AgentCardProducerFn func(ctx context.Context) (*a2a.AgentCard, error)

// Card implements AgentCardProducer.
func (fn AgentCardProducerFn) Card(ctx context.Context) (*a2a.AgentCard, error) {
	return fn(ctx)
}

// ExtendedAgentCardProducer creates AgentCard instances used for communicating extended
// capabilities to authenticated clients.
type ExtendedAgentCardProducer interface {
	// ExtendedCard returns a self-describing manifest for an agent. It contains extended data
	// for authenticated clients.
	ExtendedCard(ctx context.Context, req *a2a.GetExtendedAgentCardRequest) (*a2a.AgentCard, error)
}

// ExtendedAgentCardProducerFn is a function type which implements [ExtendedAgentCardProducer].
type ExtendedAgentCardProducerFn func(ctx context.Context, req *a2a.GetExtendedAgentCardRequest) (*a2a.AgentCard, error)

// ExtendedCard implements [ExtendedAgentCardProducer].
func (fn ExtendedAgentCardProducerFn) ExtendedCard(ctx context.Context, req *a2a.GetExtendedAgentCardRequest) (*a2a.AgentCard, error) {
	return fn(ctx, req)
}

// WithExtendedAgentCard sets a static extended authenticated agent card.
func WithExtendedAgentCard(card *a2a.AgentCard) RequestHandlerOption {
	return func(ih *InterceptedHandler, h *defaultRequestHandler) {
		h.authenticatedCardProducer = ExtendedAgentCardProducerFn(func(ctx context.Context, req *a2a.GetExtendedAgentCardRequest) (*a2a.AgentCard, error) {
			return card, nil
		})
	}
}

// WithExtendedAgentCardProducer sets a dynamic extended authenticated agent card producer.
func WithExtendedAgentCardProducer(cardProducer ExtendedAgentCardProducer) RequestHandlerOption {
	return func(ih *InterceptedHandler, h *defaultRequestHandler) {
		h.authenticatedCardProducer = cardProducer
	}
}

// NewStaticAgentCardHandler creates an [http.Handler] implementation for serving a PUBLIC [a2a.AgentCard]
// which is not expected to change while the program is running.
// The information contained in this card can be queried from any origin.
// The method panics if the argument json marhsaling fails.
func NewStaticAgentCardHandler(card *a2a.AgentCard) http.Handler {
	bytes, err := json.Marshal(card)
	if err != nil {
		panic(err.Error())
	}
	return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		ctx := attachLogger(req)
		if req.Method == "OPTIONS" {
			writePublicCardHTTPOptions(rw, req)
			rw.WriteHeader(http.StatusOK)
			return
		}
		if req.Method != "GET" {
			rw.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writeAgentCardBytes(ctx, rw, req, bytes)
	})
}

// NewAgentCardHandler creates an [http.Handler] implementation for serving a PUBLIC [a2a.AgentCard].
// The information contained in this card can be queried from any origin.
func NewAgentCardHandler(producer AgentCardProducer) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		ctx := attachLogger(req)
		if req.Method == "OPTIONS" {
			writePublicCardHTTPOptions(rw, req)
			rw.WriteHeader(http.StatusOK)
			return
		}
		if req.Method != "GET" {
			rw.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		card, err := producer.Card(ctx)
		if err != nil {
			log.Error(ctx, "agent card producer failed", err)
			rw.WriteHeader(http.StatusInternalServerError)
			return
		}
		cardBytes, err := json.Marshal(card)
		if err != nil {
			log.Error(ctx, "agent card marshaling failed", err)
			rw.WriteHeader(http.StatusInternalServerError)
			return
		}
		writeAgentCardBytes(ctx, rw, req, cardBytes)
	})
}

func attachLogger(req *http.Request) context.Context {
	logger := log.LoggerFrom(req.Context())
	withAttrs := logger.With(
		"method", req.Method,
		"host", req.Host,
		"remote_addr", req.RemoteAddr,
	)
	return log.AttachLogger(req.Context(), withAttrs)
}

func writePublicCardHTTPOptions(rw http.ResponseWriter, req *http.Request) {
	writeCORSHeaders(rw, req)
	rw.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	rw.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	rw.Header().Set("Access-Control-Max-Age", "86400")
}

func writeAgentCardBytes(ctx context.Context, rw http.ResponseWriter, req *http.Request, bytes []byte) {
	writeCORSHeaders(rw, req)
	rw.Header().Set("Content-Type", "application/json")
	if _, err := rw.Write(bytes); err != nil {
		log.Error(ctx, "failed to write agent card response", err)
	}
}

func writeCORSHeaders(rw http.ResponseWriter, req *http.Request) {
	origin := req.Header.Get("Origin")
	if origin != "" {
		rw.Header().Set("Access-Control-Allow-Origin", origin)
		rw.Header().Set("Access-Control-Allow-Credentials", "true")
		rw.Header().Set("Vary", "Origin")
	} else {
		rw.Header().Set("Access-Control-Allow-Origin", "*")
	}
}
