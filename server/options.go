package server

import (
	"strings"
	"time"

	"github.com/a2aproject/a2a-go/auth"
	"github.com/a2aproject/a2a-go/protocol/jsonprotocol"
)

const (
	defaultReadTimeout  = 60 * time.Second
	defaultWriteTimeout = 60 * time.Second
	defaultIdleTimeout  = 300 * time.Second
)

// Option is a function that configures the A2AServer.
type Option func(*A2AServer)

// WithCORSEnabled enables CORS for the server.
func WithCORSEnabled(enabled bool) Option {
	return func(s *A2AServer) {
		s.corsEnabled = enabled
	}
}

// WithJSONRPCEndpoint sets the path for the JSON-RPC endpoint.
// Default is the root path ("/").
func WithJSONRPCEndpoint(path string) Option {
	return func(s *A2AServer) {
		s.jsonRPCEndpoint = path
	}
}

// WithReadTimeout sets the read timeout for the HTTP server.
func WithReadTimeout(timeout time.Duration) Option {
	return func(s *A2AServer) {
		s.readTimeout = timeout
	}
}

// WithWriteTimeout sets the write timeout for the HTTP server.
func WithWriteTimeout(timeout time.Duration) Option {
	return func(s *A2AServer) {
		s.writeTimeout = timeout
	}
}

// WithIdleTimeout sets the idle timeout for the HTTP server.
func WithIdleTimeout(timeout time.Duration) Option {
	return func(s *A2AServer) {
		s.idleTimeout = timeout
	}
}

// WithAuthProvider sets the authentication provider for the server.
// If not set, the server will not require authentication.
func WithAuthProvider(provider auth.Provider) Option {
	return func(s *A2AServer) {
		s.authProvider = provider
	}
}

// WithJWKSEndpoint enables the JWKS endpoint for push notification authentication.
// This is used for providing public keys for JWT verification.
// The path defaults to "/.well-known/jwks.json".
func WithJWKSEndpoint(enabled bool, path string) Option {
	return func(s *A2AServer) {
		s.jwksEnabled = enabled
		if path != "" {
			s.jwksEndpoint = path
		}
	}
}

// WithPushNotificationAuthenticator sets a custom authenticator for push notifications.
// This allows reusing the same authenticator instance throughout the application
// ensuring that the same keys are used for signing and verification.
func WithPushNotificationAuthenticator(authenticator *auth.PushNotificationAuthenticator) Option {
	return func(s *A2AServer) {
		s.pushAuth = authenticator
	}
}

// WithBasePath sets a base path for all A2A endpoints.
// This option has higher priority than agentCard.URL path extraction.
// When provided, it overrides any path extracted from agentCard.URL.
//
// This is useful when:
// - The agentCard.URL is for external/frontend use
// - Internal routing/forwarding maps to different backend paths
// - You need explicit control over the serving endpoints
//
// The base path will be automatically prepended to all standard A2A endpoints:
// - Agent card: basePath + "/.well-known/agent.json"
// - JSON-RPC: basePath + "/"
// - JWKS: basePath + "/.well-known/jwks.json"
//
// Example: WithBasePath("/api/v1/agent") creates endpoints:
// - /api/v1/agent/.well-known/agent.json
// - /api/v1/agent/
// - /api/v1/agent/.well-known/jwks.json
//
// The base path should start with "/" and not end with "/".
func WithBasePath(basePath string) Option {
	return func(s *A2AServer) {
		// Normalize the base path.
		if basePath == "" || basePath == "/" {
			// Empty or root path - use default paths.
			return
		}

		// Ensure the base path starts with "/".
		if !strings.HasPrefix(basePath, "/") {
			basePath = "/" + basePath
		}

		// Remove trailing slash.
		basePath = strings.TrimSuffix(basePath, "/")

		// Set all endpoint paths with the base path prefix.
		s.jsonRPCEndpoint = basePath + "/"
		s.agentCardPath = basePath + jsonprotocol.AgentCardPath
		s.jwksEndpoint = basePath + jsonprotocol.JWKSPath
	}
}
