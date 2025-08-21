package agentcard

import (
	"context"
	"errors"

	"github.com/a2aproject/a2a-go/a2a"
)

const defaultAgentCardPath = "/.well-known/agent-card.json"

// Resolver is used to fetch an AgentCard from the provided URL.
type Resolver struct {
	BaseURL string
}

// ResolveOption is used to customize Resolve behavior.
type ResolveOption func(r *resolveRequest)

type resolveRequest struct {
	path    string
	headers map[string]string
}

// Resolve fetches an AgentCard from the provided URL.
// By default fetches from the  /.well-known/agent-card.json path.
func (r *Resolver) Resolve(ctx context.Context, opts ...ResolveOption) (a2a.AgentCard, error) {
	req := &resolveRequest{path: defaultAgentCardPath}
	for _, o := range opts {
		o(req)
	}

	return a2a.AgentCard{}, errors.New("not implemented")
}

// WithPath makes Resolve fetch from the provided path relative to BaseURL.
func WithPath(path string) ResolveOption {
	return func(r *resolveRequest) {
		r.path = path
	}
}

// WithRequestHeader makes Resolve perform fetch attaching the provided HTTP header.
func WithRequestHeader(k string, v string) ResolveOption {
	return func(r *resolveRequest) {
		r.headers[k] = v
	}
}
