package auth_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

func TestJWTAuthProvider(t *testing.T) {
	// Setup test data
	secret := []byte("test-secret-key-for-jwt-provider")
	audience := "test-audience"
	issuer := "test-issuer"
	userID := "user123"
	customClaims := map[string]interface{}{
		"role": "admin",
	}

	// Create provider
	provider := auth.NewJWTAuthProvider(secret, audience, issuer, 1*time.Hour)

	// Test token creation
	t.Run("CreateToken", func(t *testing.T) {
		token, err := provider.CreateToken(userID, customClaims)
		require.NoError(t, err)
		require.NotEmpty(t, token)
	})

	// Test authentication with valid token
	t.Run("Authenticate_ValidToken", func(t *testing.T) {
		// Create a token
		token, err := provider.CreateToken(userID, customClaims)
		require.NoError(t, err)

		// Create a request with the token
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set(auth.AuthHeaderName, "Bearer "+token)

		// Authenticate
		user, err := provider.Authenticate(req)
		require.NoError(t, err)
		assert.Equal(t, userID, user.ID)
		assert.Equal(t, "admin", user.Claims["role"])
	})

	// Test authentication with missing token
	t.Run("Authenticate_MissingToken", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)

		user, err := provider.Authenticate(req)
		assert.ErrorIs(t, err, auth.ErrMissingToken)
		assert.Nil(t, user)
	})

	// Test authentication with invalid header format
	t.Run("Authenticate_InvalidHeader", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set(auth.AuthHeaderName, "InvalidFormat token123")

		user, err := provider.Authenticate(req)
		assert.ErrorIs(t, err, auth.ErrInvalidAuthHeader)
		assert.Nil(t, user)
	})

	// Test authentication with expired token
	t.Run("Authenticate_ExpiredToken", func(t *testing.T) {
		// Create provider with very short token lifetime
		shortProvider := auth.NewJWTAuthProvider(secret, audience, issuer, 1*time.Millisecond)
		token, err := shortProvider.CreateToken(userID, nil)
		require.NoError(t, err)

		// Wait for token to expire
		time.Sleep(5 * time.Millisecond)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set(auth.AuthHeaderName, "Bearer "+token)

		user, err := shortProvider.Authenticate(req)
		assert.Error(t, err)
		assert.Nil(t, user)
		assert.Contains(t, err.Error(), "expired")
	})

	// Test client configuration
	t.Run("ConfigureClient", func(t *testing.T) {
		client := &http.Client{}
		configuredClient := provider.ConfigureClient(client)
		require.NotNil(t, configuredClient)
		require.NotNil(t, configuredClient.Transport)

		// Create a test server that validates auth header
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get(auth.AuthHeaderName)
			if authHeader == "" {
				t.Error("Expected Authorization header but got none")
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		// Make a request
		resp, err := configuredClient.Get(server.URL)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})
}

func TestAPIKeyAuthProvider(t *testing.T) {
	// Setup test data
	keyMap := map[string]string{
		"api-key-1": "user1",
		"api-key-2": "user2",
	}
	headerName := "X-Custom-API-Key"

	// Test with custom header name
	t.Run("Authenticate_CustomHeader", func(t *testing.T) {
		provider := auth.NewAPIKeyAuthProvider(keyMap, headerName)

		// Valid request
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set(headerName, "api-key-1")

		user, err := provider.Authenticate(req)
		require.NoError(t, err)
		assert.Equal(t, "user1", user.ID)
	})

	// Test with default header name
	t.Run("Authenticate_DefaultHeader", func(t *testing.T) {
		provider := auth.NewAPIKeyAuthProvider(keyMap, "")

		// Valid request
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("X-API-Key", "api-key-2")

		user, err := provider.Authenticate(req)
		require.NoError(t, err)
		assert.Equal(t, "user2", user.ID)
	})

	// Test missing API key
	t.Run("Authenticate_MissingKey", func(t *testing.T) {
		provider := auth.NewAPIKeyAuthProvider(keyMap, headerName)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)

		user, err := provider.Authenticate(req)
		assert.ErrorIs(t, err, auth.ErrMissingToken)
		assert.Nil(t, user)
	})

	// Test invalid API key
	t.Run("Authenticate_InvalidKey", func(t *testing.T) {
		provider := auth.NewAPIKeyAuthProvider(keyMap, headerName)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set(headerName, "invalid-key")

		user, err := provider.Authenticate(req)
		assert.ErrorIs(t, err, auth.ErrInvalidToken)
		assert.Nil(t, user)
	})

	// Test client configuration
	t.Run("ConfigureClient_WithAPIKey", func(t *testing.T) {
		provider := auth.NewAPIKeyAuthProvider(keyMap, headerName)
		provider.SetClientAPIKey("api-key-1")

		client := &http.Client{}
		configuredClient := provider.ConfigureClient(client)
		require.NotNil(t, configuredClient)
		require.NotNil(t, configuredClient.Transport)

		// Create a test server that validates API key header
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			apiKey := r.Header.Get(headerName)
			if apiKey != "api-key-1" {
				t.Errorf("Expected API key header with value 'api-key-1', got: %s", apiKey)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		// Make a request
		resp, err := configuredClient.Get(server.URL)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	// Test no client configuration when no API key set
	t.Run("ConfigureClient_NoAPIKey", func(t *testing.T) {
		provider := auth.NewAPIKeyAuthProvider(keyMap, headerName)
		// No SetClientAPIKey call

		client := &http.Client{}
		configuredClient := provider.ConfigureClient(client)
		require.NotNil(t, configuredClient)
		// Should be same as original client
		assert.Equal(t, client, configuredClient)
	})
}

func TestOAuth2AuthProvider(t *testing.T) {
	// Setup a mock OAuth2 userinfo server
	mockUserInfo := map[string]interface{}{
		"sub":   "user123",
		"name":  "Test User",
		"email": "test@example.com",
		"scope": "profile email",
	}

	userInfoServer := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check for Authorization header
			authHeader := r.Header.Get("Authorization")
			if authHeader != "Bearer test-token" {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(mockUserInfo)
		}))
	defer userInfoServer.Close()

	// Test authenticate with user info
	t.Run("Authenticate_WithUserInfo", func(t *testing.T) {
		provider := auth.NewOAuth2AuthProviderWithConfig(
			&oauth2.Config{},
			userInfoServer.URL,
			"sub",
		)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set(auth.AuthHeaderName, "Bearer test-token")

		user, err := provider.Authenticate(req)
		require.NoError(t, err)
		assert.Equal(t, "user123", user.ID)
		assert.Equal(t, "Test User", user.Claims["name"])
		assert.NotNil(t, user.OAuth2Info)
	})

	// Test authenticate without user info
	t.Run("Authenticate_WithoutUserInfo", func(t *testing.T) {
		provider := auth.NewOAuth2AuthProviderWithConfig(
			&oauth2.Config{},
			"", // No userinfo URL
			"",
		)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set(auth.AuthHeaderName, "Bearer test-token")

		user, err := provider.Authenticate(req)
		require.NoError(t, err)
		assert.Equal(t, "oauth2-user", user.ID) // Generic ID
		assert.NotNil(t, user.OAuth2Info)
	})

	// Test missing token
	t.Run("Authenticate_MissingToken", func(t *testing.T) {
		provider := auth.NewOAuth2AuthProviderWithConfig(
			&oauth2.Config{},
			"",
			"",
		)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)

		user, err := provider.Authenticate(req)
		assert.ErrorIs(t, err, auth.ErrMissingToken)
		assert.Nil(t, user)
	})

	// Test invalid token format
	t.Run("Authenticate_InvalidTokenFormat", func(t *testing.T) {
		provider := auth.NewOAuth2AuthProviderWithConfig(
			&oauth2.Config{},
			"",
			"",
		)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set(auth.AuthHeaderName, "InvalidFormat test-token")

		user, err := provider.Authenticate(req)
		assert.ErrorIs(t, err, auth.ErrInvalidAuthHeader)
		assert.Nil(t, user)
	})

	// Test invalid user info response
	t.Run("Authenticate_InvalidUserInfoResponse", func(t *testing.T) {
		invalidUserInfoServer := httptest.NewServer(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Return an invalid JSON response
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte("{invalid json"))
			}))
		defer invalidUserInfoServer.Close()

		provider := auth.NewOAuth2AuthProviderWithConfig(
			&oauth2.Config{},
			invalidUserInfoServer.URL,
			"sub",
		)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set(auth.AuthHeaderName, "Bearer test-token")

		user, err := provider.Authenticate(req)
		assert.Error(t, err)
		assert.Nil(t, user)
	})

	// Test client credentials provider
	t.Run("OAuth2ClientCredentials", func(t *testing.T) {
		// This is more of a structural test since we can't easily test the actual OAuth2 flow in unit tests
		provider := auth.NewOAuth2ClientCredentialsProvider(
			"client-id",
			"client-secret",
			"https://example.com/oauth2/token",
			[]string{"scope1", "scope2"},
		)

		assert.NotNil(t, provider)

		// Test that ConfigureClient returns a client
		client := &http.Client{}
		configuredClient := provider.ConfigureClient(client)
		assert.NotNil(t, configuredClient)
	})

	// Test setting a token source
	t.Run("SetTokenSource", func(t *testing.T) {
		provider := auth.NewOAuth2AuthProviderWithConfig(
			&oauth2.Config{},
			"",
			"",
		)

		// Create a static token source
		token := &oauth2.Token{
			AccessToken: "static-token",
			TokenType:   "Bearer",
			Expiry:      time.Now().Add(time.Hour),
		}
		tokenSource := oauth2.StaticTokenSource(token)
		provider.SetTokenSource(tokenSource)

		// Ensure the client is configured with our token source
		client := &http.Client{}
		configuredClient := provider.ConfigureClient(client)
		assert.NotNil(t, configuredClient)
		assert.NotEqual(t, client, configuredClient) // Should be a new client
	})
}

func TestChainAuthProvider(t *testing.T) {
	// Setup JWT provider
	jwtProvider := auth.NewJWTAuthProvider(
		[]byte("test-secret"),
		"test-audience",
		"test-issuer",
		1*time.Hour,
	)

	// Setup API key provider
	keyMap := map[string]string{
		"test-api-key": "api-user",
	}
	apiKeyProvider := auth.NewAPIKeyAuthProvider(keyMap, "X-API-Key")

	// Setup OAuth2 provider
	oauth2Provider := auth.NewOAuth2AuthProviderWithConfig(
		&oauth2.Config{},
		"", // No userinfo URL for simplicity
		"",
	)

	// Create chain provider
	chainProvider := auth.NewChainAuthProvider(jwtProvider, apiKeyProvider, oauth2Provider)

	// Test JWT authentication success
	t.Run("Authenticate_JWT_Success", func(t *testing.T) {
		token, err := jwtProvider.CreateToken("jwt-user", nil)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set(auth.AuthHeaderName, "Bearer "+token)

		user, err := chainProvider.Authenticate(req)
		require.NoError(t, err)
		assert.Equal(t, "jwt-user", user.ID)
	})

	// Test API key authentication success
	t.Run("Authenticate_APIKey_Success", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("X-API-Key", "test-api-key")

		user, err := chainProvider.Authenticate(req)
		require.NoError(t, err)
		assert.Equal(t, "api-user", user.ID)
	})

	// Test authentication failure when no provider succeeds
	t.Run("Authenticate_AllFail", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)

		user, err := chainProvider.Authenticate(req)
		assert.Error(t, err)
		assert.Nil(t, user)
	})

	// Test client configuration uses first provider
	t.Run("ConfigureClient", func(t *testing.T) {
		// Set up a chain with API key first
		apiKeyProvider.SetClientAPIKey("test-api-key")
		apiKeyFirstChain := auth.NewChainAuthProvider(apiKeyProvider, jwtProvider)

		client := &http.Client{}
		configuredClient := apiKeyFirstChain.ConfigureClient(client)
		require.NotNil(t, configuredClient)
		require.NotEqual(t, client, configuredClient) // Should be a different client

		// Create a test server that validates the API key
		server := httptest.NewServer(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				apiKey := r.Header.Get("X-API-Key")
				if apiKey != "test-api-key" {
					t.Errorf("Expected 'X-API-Key: test-api-key', got: %s", apiKey)
					http.Error(w, "Unauthorized", http.StatusUnauthorized)
					return
				}
				w.WriteHeader(http.StatusOK)
			}))
		defer server.Close()

		// Make a request
		resp, err := configuredClient.Get(server.URL)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})
}

func TestAuthMiddleware(t *testing.T) {
	// Setup JWT provider for testing middleware
	provider := auth.NewJWTAuthProvider(
		[]byte("test-secret"),
		"",
		"",
		1*time.Hour,
	)

	// Create middleware
	middleware := auth.NewMiddleware(provider)

	// Create test handler that checks for authenticated user in context
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := r.Context().Value(auth.AuthUserKey).(*auth.User)
		if !ok || user == nil {
			t.Error("Expected authenticated user in context but found none")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		// Respond with user ID to verify
		w.Write([]byte(user.ID))
	})

	// Wrap the test handler with the middleware
	wrappedHandler := middleware.Wrap(testHandler)

	// Test with valid authentication
	t.Run("ValidAuthentication", func(t *testing.T) {
		token, err := provider.CreateToken("test-user", nil)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set(auth.AuthHeaderName, "Bearer "+token)
		rr := httptest.NewRecorder()

		wrappedHandler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
		assert.Equal(t, "test-user", rr.Body.String())
	})

	// Test with invalid authentication
	t.Run("InvalidAuthentication", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		// No auth header
		rr := httptest.NewRecorder()

		wrappedHandler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusUnauthorized, rr.Code)
	})
}
