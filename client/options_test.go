package client

import (
	"net/http"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

func TestWithHTTPClient(t *testing.T) {
	// Create a custom HTTP client with specific timeout
	customClient := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Create A2A client with the custom HTTP client
	client, err := NewA2AClient("http://localhost:8080", WithHTTPClient(customClient))
	require.NoError(t, err)

	// Verify the client is using our custom HTTP client
	assert.Equal(t, customClient, client.httpClient)
	assert.Equal(t, 30*time.Second, client.httpClient.Timeout)

	// Test with nil client (should use default)
	defaultClient, err := NewA2AClient("http://localhost:8080", WithHTTPClient(nil))
	require.NoError(t, err)
	assert.NotNil(t, defaultClient.httpClient)
	assert.Equal(t, defaultTimeout, defaultClient.httpClient.Timeout)
}

func TestWithTimeout(t *testing.T) {
	// Create client with custom timeout
	client, err := NewA2AClient("http://localhost:8080", WithTimeout(45*time.Second))
	require.NoError(t, err)

	// Verify timeout was set correctly
	assert.Equal(t, 45*time.Second, client.httpClient.Timeout)

	// Test with zero timeout (should use default)
	defaultClient, err := NewA2AClient("http://localhost:8080", WithTimeout(0))
	require.NoError(t, err)
	assert.Equal(t, defaultTimeout, defaultClient.httpClient.Timeout)
}

func TestWithUserAgent(t *testing.T) {
	// Create client with custom user agent
	customUA := "CustomUserAgent/1.0"
	client, err := NewA2AClient("http://localhost:8080", WithUserAgent(customUA))
	require.NoError(t, err)

	// Verify user agent was set correctly
	assert.Equal(t, customUA, client.userAgent)
}

func TestWithJWTAuth(t *testing.T) {
	secretKey := []byte("test-secret-key")
	audience := "test-audience"
	issuer := "test-issuer"
	lifetime := 1 * time.Hour

	// Create client with JWT auth
	client, err := NewA2AClient("http://localhost:8080",
		WithJWTAuth(secretKey, audience, issuer, lifetime))
	require.NoError(t, err)

	// Verify auth provider was set up correctly
	assert.NotNil(t, client.authProvider)

	// Type assertion to check it's the right type
	jwtProvider, ok := client.authProvider.(*auth.JWTAuthProvider)
	assert.True(t, ok, "Should be a JWTAuthProvider")
	assert.Equal(t, secretKey, jwtProvider.Secret)
	assert.Equal(t, audience, jwtProvider.Audience)
	assert.Equal(t, issuer, jwtProvider.Issuer)
	assert.Equal(t, lifetime, jwtProvider.TokenLifetime)
}

func TestWithAPIKeyAuth(t *testing.T) {
	apiKey := "test-api-key"
	headerName := "X-Custom-API-Key"

	// Create client with API key auth
	client, err := NewA2AClient("http://localhost:8080",
		WithAPIKeyAuth(apiKey, headerName))
	require.NoError(t, err)

	// Verify auth provider was set up correctly
	assert.NotNil(t, client.authProvider)

	// Type assertion to check it's the right type
	_, ok := client.authProvider.(*auth.APIKeyAuthProvider)
	assert.True(t, ok, "Should be an APIKeyAuthProvider")
}

func TestWithOAuth2ClientCredentials(t *testing.T) {
	clientID := "test-client-id"
	clientSecret := "test-client-secret-placeholder"
	tokenURL := "https://auth.example.com/token"
	scopes := []string{"profile", "email"}

	// Create client with OAuth2 client credentials
	client, err := NewA2AClient("http://localhost:8080",
		WithOAuth2ClientCredentials(clientID, clientSecret, tokenURL, scopes))
	require.NoError(t, err)

	// Verify auth provider was set up correctly
	assert.NotNil(t, client.authProvider)

	// Type assertion to check it's the right type
	_, ok := client.authProvider.(*auth.OAuth2AuthProvider)
	assert.True(t, ok, "Should be an OAuth2AuthProvider")
}

func TestWithOAuth2TokenSource(t *testing.T) {
	// Create a test OAuth2 config
	config := &oauth2.Config{
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret-placeholder",
		Endpoint: oauth2.Endpoint{
			TokenURL: "https://auth.example.com/token",
		},
	}

	// Create a static token source
	tokenSource := oauth2.StaticTokenSource(&oauth2.Token{
		AccessToken: "test-access-token-placeholder",
		TokenType:   "Bearer",
	})

	// Create client with OAuth2 token source
	client, err := NewA2AClient("http://localhost:8080",
		WithOAuth2TokenSource(config, tokenSource))
	require.NoError(t, err)

	// Verify auth provider was set up correctly
	assert.NotNil(t, client.authProvider)

	// Type assertion to check it's the right type
	_, ok := client.authProvider.(*auth.OAuth2AuthProvider)
	assert.True(t, ok, "Should be an OAuth2AuthProvider")
}

func TestWithAuthProvider(t *testing.T) {
	// Create a mock auth provider
	mockProvider := &mockClientProvider{}

	// Create client with custom auth provider
	client, err := NewA2AClient("http://localhost:8080",
		WithAuthProvider(mockProvider))
	require.NoError(t, err)

	// Verify auth provider was set correctly
	assert.Equal(t, mockProvider, client.authProvider)
}

// mockClientProvider implements auth.ClientProvider for testing
type mockClientProvider struct{}

func (p *mockClientProvider) Authenticate(r *http.Request) (*auth.User, error) {
	return &auth.User{ID: "mock-user"}, nil
}

func (p *mockClientProvider) ConfigureClient(client *http.Client) *http.Client {
	return client
}
