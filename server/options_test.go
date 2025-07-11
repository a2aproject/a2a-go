package server

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/auth"
	"github.com/a2aproject/a2a-go/protocol/jsonprotocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithCORSEnabled(t *testing.T) {
	// Test with CORS enabled
	opt := WithCORSEnabled(true)
	s := &A2AServer{}
	opt(s)
	assert.True(t, s.corsEnabled)

	// Test with CORS disabled
	opt = WithCORSEnabled(false)
	opt(s)
	assert.False(t, s.corsEnabled)
}

func TestWithJSONRPCEndpoint(t *testing.T) {
	// Test with custom JSON-RPC path
	path := "/custom/path"
	opt := WithJSONRPCEndpoint(path)
	s := &A2AServer{}
	opt(s)
	assert.Equal(t, path, s.jsonRPCEndpoint)
}

func TestWithReadTimeout(t *testing.T) {
	// Test with custom read timeout
	timeout := 30 * time.Second
	opt := WithReadTimeout(timeout)
	s := &A2AServer{}
	opt(s)
	assert.Equal(t, timeout, s.readTimeout)
}

func TestWithWriteTimeout(t *testing.T) {
	// Test with custom write timeout
	timeout := 30 * time.Second
	opt := WithWriteTimeout(timeout)
	s := &A2AServer{}
	opt(s)
	assert.Equal(t, timeout, s.writeTimeout)
}

func TestWithIdleTimeout(t *testing.T) {
	// Test with custom idle timeout
	timeout := 120 * time.Second
	opt := WithIdleTimeout(timeout)
	s := &A2AServer{}
	opt(s)
	assert.Equal(t, timeout, s.idleTimeout)
}

func TestWithAuthProvider(t *testing.T) {
	// Create a mock auth provider
	provider := &mockAuthProvider{}

	// Test with auth provider
	opt := WithAuthProvider(provider)
	s := &A2AServer{}
	opt(s)
	assert.Equal(t, provider, s.authProvider)
}

func TestWithJWKSEndpoint(t *testing.T) {
	// Test with JWKS endpoint enabled and custom path
	customPath := "/custom/jwks.json"
	opt := WithJWKSEndpoint(true, customPath)
	s := &A2AServer{}
	opt(s)
	assert.True(t, s.jwksEnabled)
	assert.Equal(t, customPath, s.jwksEndpoint)

	// Test with JWKS endpoint disabled
	opt = WithJWKSEndpoint(false, "")
	opt(s)
	assert.False(t, s.jwksEnabled)
}

// Test for WithPushNotificationAuthenticator option
func TestWithPushNotificationAuthenticator(t *testing.T) {
	authenticator := auth.NewPushNotificationAuthenticator()
	require.NoError(t, authenticator.GenerateKeyPair())

	serverOptions := &A2AServer{}
	opt := WithPushNotificationAuthenticator(authenticator)
	opt(serverOptions)

	assert.Equal(t, authenticator, serverOptions.pushAuth)
}

func TestExtractBasePathFromURL(t *testing.T) {
	tests := []struct {
		name         string
		agentURL     string
		expectedPath string
	}{
		{
			name:         "Basic sub-path",
			agentURL:     "http://localhost:8080/agent",
			expectedPath: "/agent",
		},
		{
			name:         "Multi-level path",
			agentURL:     "http://localhost:8080/api/v2/agents/myagent",
			expectedPath: "/api/v2/agents/myagent",
		},
		{
			name:         "Path with trailing slash - should be normalized",
			agentURL:     "http://localhost:8080/agent/api/v2/",
			expectedPath: "/agent/api/v2",
		},
		{
			name:         "Root path - should return empty",
			agentURL:     "http://localhost:8080/",
			expectedPath: "",
		},
		{
			name:         "Root path without slash - should return empty",
			agentURL:     "http://localhost:8080",
			expectedPath: "",
		},
		{
			name:         "Empty URL - should return empty",
			agentURL:     "",
			expectedPath: "",
		},
		{
			name:         "HTTPS URL with port",
			agentURL:     "https://example.com:9090/my/agent/path",
			expectedPath: "/my/agent/path",
		},
		{
			name:         "URL with query parameters - query ignored",
			agentURL:     "http://localhost:8080/agent?param=value",
			expectedPath: "/agent",
		},
		{
			name:         "URL with fragment - fragment ignored",
			agentURL:     "http://localhost:8080/agent#section",
			expectedPath: "/agent",
		},
		{
			name:         "Invalid URL - should return empty",
			agentURL:     "not-a-valid-url",
			expectedPath: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractBasePathFromURL(tt.agentURL)
			assert.Equal(t, tt.expectedPath, result, "Base path extraction mismatch")
		})
	}
}

func TestNewA2AServerWithAgentCardURL(t *testing.T) {
	tests := []struct {
		name              string
		agentCardURL      string
		expectedJSONRPC   string
		expectedAgentCard string
		expectedJWKS      string
	}{
		{
			name:              "Agent card with sub-path",
			agentCardURL:      "http://localhost:8080/agent",
			expectedJSONRPC:   "/agent/",
			expectedAgentCard: "/agent/.well-known/agent.json",
			expectedJWKS:      "/agent/.well-known/jwks.json",
		},
		{
			name:              "Agent card with multi-level path",
			agentCardURL:      "http://localhost:8080/api/v2/agents/myagent",
			expectedJSONRPC:   "/api/v2/agents/myagent/",
			expectedAgentCard: "/api/v2/agents/myagent/.well-known/agent.json",
			expectedJWKS:      "/api/v2/agents/myagent/.well-known/jwks.json",
		},
		{
			name:              "Agent card with root URL",
			agentCardURL:      "http://localhost:8080/",
			expectedJSONRPC:   "/",
			expectedAgentCard: "/.well-known/agent.json",
			expectedJWKS:      "/.well-known/jwks.json",
		},
		{
			name:              "Agent card with empty URL",
			agentCardURL:      "",
			expectedJSONRPC:   "/",
			expectedAgentCard: "/.well-known/agent.json",
			expectedJWKS:      "/.well-known/jwks.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a mock task manager
			taskMgr := &optionsTestTaskManager{}

			// Create agent card with the test URL
			agentCard := AgentCard{
				Name: "Test Agent",
				URL:  tt.agentCardURL,
			}

			// Create the server
			server, err := NewA2AServer(agentCard, taskMgr)
			require.NoError(t, err)

			// Verify all paths are correctly configured
			assert.Equal(t, tt.expectedJSONRPC, server.jsonRPCEndpoint, "JSON-RPC endpoint mismatch")
			assert.Equal(t, tt.expectedAgentCard, server.agentCardPath, "Agent card path mismatch")
			assert.Equal(t, tt.expectedJWKS, server.jwksEndpoint, "JWKS endpoint mismatch")
		})
	}
}

func TestComposeJWKSURL(t *testing.T) {
	tests := []struct {
		name         string
		agentCardURL string
		jwksEndpoint string
		expectedURL  string
	}{
		{
			name:         "Agent card with sub-path",
			agentCardURL: "http://localhost:8080/agent",
			jwksEndpoint: "/agent/.well-known/jwks.json",
			expectedURL:  "http://localhost:8080/agent/.well-known/jwks.json",
		},
		{
			name:         "Agent card with multi-level path",
			agentCardURL: "https://example.com:9090/api/v2/agents/myagent",
			jwksEndpoint: "/api/v2/agents/myagent/.well-known/jwks.json",
			expectedURL:  "https://example.com:9090/api/v2/agents/myagent/.well-known/jwks.json",
		},
		{
			name:         "Agent card with root path",
			agentCardURL: "http://localhost:8080/",
			jwksEndpoint: "/.well-known/jwks.json",
			expectedURL:  "http://localhost:8080/.well-known/jwks.json",
		},
		{
			name:         "Empty agent card URL",
			agentCardURL: "",
			jwksEndpoint: "/.well-known/jwks.json",
			expectedURL:  "/.well-known/jwks.json",
		},
		{
			name:         "Invalid agent card URL",
			agentCardURL: "not-a-valid-url",
			jwksEndpoint: "/.well-known/jwks.json",
			expectedURL:  "/.well-known/jwks.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create server instance
			server := &A2AServer{
				agentCard: AgentCard{
					URL: tt.agentCardURL,
				},
				jwksEndpoint: tt.jwksEndpoint,
			}

			// Call the function
			result := server.composeJWKSURL()

			// Verify the result
			assert.Equal(t, tt.expectedURL, result, "JWKS URL composition mismatch")
		})
	}
}

// optionsTestTaskManager is a simple mock implementing taskmanager.TaskManager interface
type optionsTestTaskManager struct{}

func (m *optionsTestTaskManager) OnSendMessage(ctx context.Context, params jsonprotocol.SendMessageParams) (*jsonprotocol.MessageResult, error) {
	return &jsonprotocol.MessageResult{
		Result: &jsonprotocol.Message{
			Kind:      "message",
			MessageID: "test-message-id",
			Role:      jsonprotocol.MessageRoleAgent,
			Parts:     []jsonprotocol.Part{jsonprotocol.NewTextPart("test response")},
		},
	}, nil
}

func (m *optionsTestTaskManager) OnSendMessageStream(ctx context.Context, params jsonprotocol.SendMessageParams) (<-chan jsonprotocol.StreamingMessageEvent, error) {
	return nil, nil
}

func (m *optionsTestTaskManager) OnGetTask(ctx context.Context, params jsonprotocol.TaskQueryParams) (*jsonprotocol.Task, error) {
	return nil, nil
}

func (m *optionsTestTaskManager) OnCancelTask(ctx context.Context, params jsonprotocol.TaskIDParams) (*jsonprotocol.Task, error) {
	return nil, nil
}

func (m *optionsTestTaskManager) OnResubscribe(ctx context.Context, params jsonprotocol.TaskIDParams) (<-chan jsonprotocol.StreamingMessageEvent, error) {
	return nil, nil
}

func (m *optionsTestTaskManager) OnPushNotificationSet(ctx context.Context, params jsonprotocol.TaskPushNotificationConfig) (*jsonprotocol.TaskPushNotificationConfig, error) {
	return &params, nil
}

func (m *optionsTestTaskManager) OnPushNotificationGet(ctx context.Context, params jsonprotocol.TaskIDParams) (*jsonprotocol.TaskPushNotificationConfig, error) {
	return &jsonprotocol.TaskPushNotificationConfig{
		TaskID: params.ID,
		PushNotificationConfig: jsonprotocol.PushNotificationConfig{
			URL: "http://test.example.com/webhook",
		},
	}, nil
}

// mockAuthProvider is a simple mock implementing auth.Provider interface
type mockAuthProvider struct{}

func (p *mockAuthProvider) Authenticate(r *http.Request) (*auth.User, error) {
	return &auth.User{ID: "test-user"}, nil
}

func TestWithBasePath(t *testing.T) {
	tests := []struct {
		name              string
		basePath          string
		expectedJSONRPC   string
		expectedAgentCard string
		expectedJWKS      string
	}{
		{
			name:              "Basic sub-path",
			basePath:          "/agent",
			expectedJSONRPC:   "/agent/",
			expectedAgentCard: "/agent/.well-known/agent.json",
			expectedJWKS:      "/agent/.well-known/jwks.json",
		},
		{
			name:              "Multi-level path",
			basePath:          "/api/v2/agents/myagent",
			expectedJSONRPC:   "/api/v2/agents/myagent/",
			expectedAgentCard: "/api/v2/agents/myagent/.well-known/agent.json",
			expectedJWKS:      "/api/v2/agents/myagent/.well-known/jwks.json",
		},
		{
			name:              "Path with trailing slash - should be normalized",
			basePath:          "/agent/api/v2/",
			expectedJSONRPC:   "/agent/api/v2/",
			expectedAgentCard: "/agent/api/v2/.well-known/agent.json",
			expectedJWKS:      "/agent/api/v2/.well-known/jwks.json",
		},
		{
			name:              "Path without leading slash - should be normalized",
			basePath:          "agent/api",
			expectedJSONRPC:   "/agent/api/",
			expectedAgentCard: "/agent/api/.well-known/agent.json",
			expectedJWKS:      "/agent/api/.well-known/jwks.json",
		},
		{
			name:              "Path without leading slash and with trailing slash",
			basePath:          "agent/api/",
			expectedJSONRPC:   "/agent/api/",
			expectedAgentCard: "/agent/api/.well-known/agent.json",
			expectedJWKS:      "/agent/api/.well-known/jwks.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create server instance with default paths
			s := &A2AServer{
				jsonRPCEndpoint: jsonprotocol.DefaultJSONRPCPath,
				agentCardPath:   jsonprotocol.AgentCardPath,
				jwksEndpoint:    jsonprotocol.JWKSPath,
			}

			// Apply WithBasePath option
			opt := WithBasePath(tt.basePath)
			opt(s)

			// Verify all paths are correctly configured
			assert.Equal(t, tt.expectedJSONRPC, s.jsonRPCEndpoint, "JSON-RPC endpoint mismatch")
			assert.Equal(t, tt.expectedAgentCard, s.agentCardPath, "Agent card path mismatch")
			assert.Equal(t, tt.expectedJWKS, s.jwksEndpoint, "JWKS endpoint mismatch")
		})
	}
}

func TestWithBasePathEmptyAndRoot(t *testing.T) {
	// Test with empty string - should not change paths
	s := &A2AServer{
		jsonRPCEndpoint: jsonprotocol.DefaultJSONRPCPath,
		agentCardPath:   jsonprotocol.AgentCardPath,
		jwksEndpoint:    jsonprotocol.JWKSPath,
	}

	opt := WithBasePath("")
	opt(s)

	// Paths should remain unchanged
	assert.Equal(t, jsonprotocol.DefaultJSONRPCPath, s.jsonRPCEndpoint)
	assert.Equal(t, jsonprotocol.AgentCardPath, s.agentCardPath)
	assert.Equal(t, jsonprotocol.JWKSPath, s.jwksEndpoint)

	// Test with root path "/" - should not change paths
	opt = WithBasePath("/")
	opt(s)

	// Paths should remain unchanged
	assert.Equal(t, jsonprotocol.DefaultJSONRPCPath, s.jsonRPCEndpoint)
	assert.Equal(t, jsonprotocol.AgentCardPath, s.agentCardPath)
	assert.Equal(t, jsonprotocol.JWKSPath, s.jwksEndpoint)
}

func TestWithBasePathPriority(t *testing.T) {
	tests := []struct {
		name              string
		agentCardURL      string
		basePath          string
		expectedJSONRPC   string
		expectedAgentCard string
		expectedJWKS      string
	}{
		{
			name:              "WithBasePath overrides agentCard.URL",
			agentCardURL:      "http://localhost:8080/from/url",
			basePath:          "/from/option",
			expectedJSONRPC:   "/from/option/",
			expectedAgentCard: "/from/option/.well-known/agent.json",
			expectedJWKS:      "/from/option/.well-known/jwks.json",
		},
		{
			name:              "agentCard.URL used when WithBasePath is empty",
			agentCardURL:      "http://localhost:8080/from/url",
			basePath:          "",
			expectedJSONRPC:   "/from/url/",
			expectedAgentCard: "/from/url/.well-known/agent.json",
			expectedJWKS:      "/from/url/.well-known/jwks.json",
		},
		{
			name:              "agentCard.URL used when WithBasePath is root path",
			agentCardURL:      "http://localhost:8080/from/url",
			basePath:          "/",
			expectedJSONRPC:   "/from/url/",
			expectedAgentCard: "/from/url/.well-known/agent.json",
			expectedJWKS:      "/from/url/.well-known/jwks.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a mock task manager
			taskMgr := &optionsTestTaskManager{}

			// Create agent card with the test URL
			agentCard := AgentCard{
				Name: "Test Agent",
				URL:  tt.agentCardURL,
			}

			// Create the server with WithBasePath option
			var opts []Option
			if tt.basePath != "" {
				opts = append(opts, WithBasePath(tt.basePath))
			}
			server, err := NewA2AServer(agentCard, taskMgr, opts...)
			require.NoError(t, err)

			// Verify all paths are correctly configured
			assert.Equal(t, tt.expectedJSONRPC, server.jsonRPCEndpoint, "JSON-RPC endpoint mismatch")
			assert.Equal(t, tt.expectedAgentCard, server.agentCardPath, "Agent card path mismatch")
			assert.Equal(t, tt.expectedJWKS, server.jwksEndpoint, "JWKS endpoint mismatch")
		})
	}
}
