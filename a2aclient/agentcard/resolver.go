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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/log"
)

// ErrStatusNotOK is an error returned by Resolver when HTTP request returned a non-OK status.
type ErrStatusNotOK struct {
	StatusCode int
	Status     string
}

func (e *ErrStatusNotOK) Error() string {
	return fmt.Sprintf("card request failed, status: %s", e.Status)
}

const defaultAgentCardPath = "/.well-known/agent-card.json"

var defaultClient = &http.Client{Timeout: 30 * time.Second}

// Parser is used to parse the agent card from the response body.
type Parser func([]byte) (*a2a.AgentCard, error)

// DefaultCardParser is a default [Parser].
var DefaultCardParser Parser = func(body []byte) (*a2a.AgentCard, error) {
	var card a2a.AgentCard
	if err := json.Unmarshal(body, &card); err != nil {
		return nil, err
	}
	return &card, nil
}

// DefaultResolver is configured with an [http.Client] with a 30-second timeout.
var DefaultResolver = &Resolver{Client: defaultClient, CardParser: DefaultCardParser}

// Resolver is used to fetch an [a2a.AgentCard].
type Resolver struct {
	// Client can be used to configure appropriate timeout, retry policy, and connection pooling
	Client *http.Client
	// CardParser can be used to configure AgentCard parsing.
	CardParser Parser
}

// NewResolver is a [Resolver] constructor function.
func NewResolver(client *http.Client) *Resolver {
	return &Resolver{Client: client}
}

// ResolveOption is used to customize Resolve behavior.
type ResolveOption func(r *resolveRequest)

type resolveRequest struct {
	path    string
	headers map[string]string
}

// Resolve fetches an [a2a.AgentCard] from the provided URL (base URL or complete agent card URL).
// By default, if the provided URL has no path or a root path, the request is sent for the /.well-known/agent-card.json path.
// If the provided URL contains a non-root path, it is fetched directly as the complete agent card URL.
// A file:// URL is supported to load the card from the local file system, in which case
// the card is read directly from the referenced path and request headers are ignored.
func (r *Resolver) Resolve(ctx context.Context, baseURL string, opts ...ResolveOption) (*a2a.AgentCard, error) {
	reqSpec := &resolveRequest{headers: make(map[string]string)}
	for _, o := range opts {
		o(reqSpec)
	}

	reqURL, err := buildURL(baseURL, reqSpec.path)
	if err != nil {
		return nil, err
	}

	u, err := url.Parse(reqURL)
	if err != nil {
		return nil, fmt.Errorf("url parsing failed: %w", err)
	}

	if u.Scheme == "file" {
		body, err := readCardFile(u)
		if err != nil {
			return nil, err
		}
		return r.parseCard(body)
	}

	client := r.Client
	if client == nil {
		client = defaultClient
	}

	body, err := fetchCard(ctx, client, reqURL, reqSpec.headers)
	if err != nil {
		return nil, err
	}

	return r.parseCard(body)
}

func buildURL(baseURL, path string) (string, error) {
	if path != "" {
		reqURL, err := url.JoinPath(baseURL, path)
		if err != nil {
			return "", fmt.Errorf("url construction failed: %w", err)
		}
		return reqURL, nil
	}

	u, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("url construction failed: %w", err)
	}
	if u.Path == "" || u.Path == "/" {
		reqURL, err := url.JoinPath(baseURL, defaultAgentCardPath)
		if err != nil {
			return "", fmt.Errorf("url construction failed: %w", err)
		}
		return reqURL, nil
	}
	return baseURL, nil
}

func fetchCard(ctx context.Context, client *http.Client, reqURL string, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to construct a request: %w", err)
	}
	for h, val := range headers {
		req.Header.Add(h, val)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("card request failed: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Error(ctx, "failed to close response body", err, "from", reqURL)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, &ErrStatusNotOK{StatusCode: resp.StatusCode, Status: resp.Status}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read card response: %w", err)
	}
	return body, nil
}

func readCardFile(u *url.URL) ([]byte, error) {
	if u.Host != "" && u.Host != "localhost" {
		return nil, fmt.Errorf("unsupported file URL host %q, only an empty host or localhost is allowed", u.Host)
	}

	path := u.Path
	if path == "" {
		path = u.Opaque
	}
	if path == "" {
		return nil, fmt.Errorf("file URL %q has no path", u)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read card file: %w", err)
	}
	return body, nil
}

func (r *Resolver) parseCard(body []byte) (*a2a.AgentCard, error) {
	parseFn := r.CardParser
	if parseFn == nil {
		parseFn = DefaultCardParser
	}
	card, err := parseFn(body)
	if err != nil {
		return nil, fmt.Errorf("card parsing failed: %w", err)
	}
	return card, nil
}

// WithPath makes Resolve fetch from the provided path relative to base URL.
func WithPath(path string) ResolveOption {
	return func(r *resolveRequest) {
		r.path = path
	}
}

// WithRequestHeader makes Resolve perform fetch attaching the provided HTTP headers.
func WithRequestHeader(name string, val string) ResolveOption {
	return func(r *resolveRequest) {
		r.headers[name] = val
	}
}
