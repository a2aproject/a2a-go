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

package agentcard

import (
	"context"
	"fmt"

	"github.com/a2aproject/a2a-go/a2a"
)

const defaultAgentCardPath = "/.well-known/agent-card.json"

// Resolver is used to fetch an AgentCard from the provided URL.
type Resolver struct {
	BaseURL string
}

// ResolveOption is used to customize Resolve() behavior.
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

	return a2a.AgentCard{}, fmt.Errorf("not implemented")
}

// WithPath makes Resolve fetch from the provided path relative to BaseURL.
func WithPath(path string) ResolveOption {
	return func(r *resolveRequest) {
		r.path = path
	}
}

// WithRequestHeader makes Resolve perform fetch attaching the provided HTTP headers.
func WithRequestHeaders(headers map[string]string) ResolveOption {
	return func(r *resolveRequest) {
		for k, v := range headers {
			r.headers[k] = v
		}
	}
}
