package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPushNotificationAuthenticator_GenerateKeyPair(t *testing.T) {
	auth := NewPushNotificationAuthenticator()
	err := auth.GenerateKeyPair()

	require.NoError(t, err)
	assert.NotNil(t, auth.privateKey)
	assert.NotEmpty(t, auth.keyID)

	// Verify the key set has one key
	assert.Equal(t, 1, auth.keySet.Len())

	// Verify the key has the required attributes
	key, found := auth.keySet.Key(0)
	require.True(t, found)

	// Test algorithm setting (line 89-90)
	algVal, ok := key.Get(jwk.AlgorithmKey)
	require.True(t, ok)
	assert.EqualValues(t, "RS256", algVal)

	// Test key ID and usage
	kidVal, ok := key.Get(jwk.KeyIDKey)
	require.True(t, ok)
	assert.Equal(t, auth.keyID, kidVal)

	usageVal, ok := key.Get(jwk.KeyUsageKey)
	require.True(t, ok)
	assert.Equal(t, "sig", usageVal)
}

func TestPushNotificationAuthenticator_SignPayload(t *testing.T) {
	auth := NewPushNotificationAuthenticator()
	err := auth.GenerateKeyPair()
	require.NoError(t, err)

	// Test with valid payload
	payload := []byte(`{"test":"data"}`)
	tokenString, err := auth.SignPayload(payload)

	require.NoError(t, err)
	assert.NotEmpty(t, tokenString)

	// Parse token and verify header contains kid
	token, _, err := new(jwt.Parser).ParseUnverified(tokenString, jwt.MapClaims{})
	require.NoError(t, err)
	assert.Equal(t, auth.keyID, token.Header["kid"])

	// Verify claims
	claims, ok := token.Claims.(jwt.MapClaims)
	require.True(t, ok)
	assert.Contains(t, claims, "iat")
	assert.Contains(t, claims, "request_body_sha256")

	// Test with error case - no private key
	authNoKey := NewPushNotificationAuthenticator()
	_, err = authNoKey.SignPayload(payload)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "private key not initialized")
}

func TestPushNotificationAuthenticator_HandleJWKS(t *testing.T) {
	auth := NewPushNotificationAuthenticator()
	err := auth.GenerateKeyPair()
	require.NoError(t, err)

	// Test successful GET request
	req := httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil)
	w := httptest.NewRecorder()

	auth.HandleJWKS(w, req)

	// Verify response
	resp := w.Result()
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

	// Parse response body
	var jwksResp map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&jwksResp)
	require.NoError(t, err)

	// Verify keys array exists
	keys, ok := jwksResp["keys"].([]interface{})
	require.True(t, ok)
	assert.Len(t, keys, 1)

	// Test non-GET method
	req = httptest.NewRequest(http.MethodPost, "/.well-known/jwks.json", nil)
	w = httptest.NewRecorder()

	auth.HandleJWKS(w, req)

	resp = w.Result()
	defer resp.Body.Close()

	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

func TestJWKSClient_FetchKeys(t *testing.T) {
	// Create a mock JWKS server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"keys":[{"kty":"RSA","kid":"test-key-id","alg":"RS256","use":"sig","n":"test","e":"AQAB"}]}`)
	}))
	defer ts.Close()

	// Create JWKS client
	client := NewJWKSClient(ts.URL, 10*time.Minute)

	// Test fetch keys
	err := client.FetchKeys(context.Background())
	require.NoError(t, err)

	// Verify keys were fetched
	assert.Equal(t, 1, client.keySet.Len())

	// Test cache behavior - should not fetch again
	prevFetch := client.lastFetch
	err = client.FetchKeys(context.Background())
	require.NoError(t, err)
	assert.Equal(t, prevFetch, client.lastFetch) // Should not have refreshed

	// Test error cases

	// Invalid URL
	clientErr := NewJWKSClient("invalid-url", 10*time.Minute)
	err = clientErr.FetchKeys(context.Background())
	assert.Error(t, err)

	// Non-200 response
	tsErr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer tsErr.Close()

	clientErrResp := NewJWKSClient(tsErr.URL, 10*time.Minute)
	err = clientErrResp.FetchKeys(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected status code")

	// Invalid JSON
	tsBadJSON := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"keys": not valid json}`)
	}))
	defer tsBadJSON.Close()

	clientBadJSON := NewJWKSClient(tsBadJSON.URL, 10*time.Minute)
	err = clientBadJSON.FetchKeys(context.Background())
	assert.Error(t, err)
}

func TestJWKSClient_GetKey(t *testing.T) {
	// Create a mock JWKS server with key
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"keys":[{"kty":"RSA","kid":"test-key-id","alg":"RS256","use":"sig","n":"test","e":"AQAB"}]}`)
	}))
	defer ts.Close()

	// Create JWKS client
	client := NewJWKSClient(ts.URL, 10*time.Minute)

	// Get key by ID
	key, err := client.GetKey(context.Background(), "test-key-id")
	require.NoError(t, err)
	assert.NotNil(t, key)

	// Get non-existent key
	_, err = client.GetKey(context.Background(), "non-existent-key")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// Test for lines 315-358 in auth/push_notification.go
func TestPushNotificationAuthenticator_SendPushNotification(t *testing.T) {
	auth := NewPushNotificationAuthenticator()
	err := auth.GenerateKeyPair()
	require.NoError(t, err)

	// Create a test server to receive the push notification
	var receivedRequest *http.Request
	var receivedBody []byte

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := io.ReadAll(r.Body)
		receivedRequest = r.Clone(context.Background())
		receivedBody = bodyBytes
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	// Test sending notification
	payload := map[string]interface{}{
		"task_id": "test-123",
		"status":  "completed",
	}

	ctx := context.Background()
	err = auth.SendPushNotification(ctx, ts.URL, payload)
	require.NoError(t, err)

	// Verify the request
	assert.NotNil(t, receivedRequest)
	assert.Equal(t, "application/json", receivedRequest.Header.Get("Content-Type"))
	assert.Contains(t, receivedRequest.Header.Get("Authorization"), "Bearer ")

	// Verify payload
	var receivedPayload map[string]interface{}
	err = json.Unmarshal(receivedBody, &receivedPayload)
	require.NoError(t, err)
	assert.Equal(t, "test-123", receivedPayload["task_id"])

	// Test implicit http:// prefix
	urlWithoutScheme := strings.TrimPrefix(ts.URL, "http://")
	err = auth.SendPushNotification(ctx, urlWithoutScheme, payload)
	require.NoError(t, err)
}

// Test error cases for SendPushNotification
func TestPushNotificationAuthenticator_SendPushNotification_Errors(t *testing.T) {
	auth := NewPushNotificationAuthenticator()
	err := auth.GenerateKeyPair()
	require.NoError(t, err)

	// Test with empty URL
	err = auth.SendPushNotification(context.Background(), "", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "URL is required")

	// Test with server that returns error
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	err = auth.SendPushNotification(context.Background(), ts.URL, map[string]string{"test": "data"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed with status")

	// Test with non-existent server to test HTTP error
	err = auth.SendPushNotification(context.Background(), "http://non-existent-server.local", map[string]string{"test": "data"})
	assert.Error(t, err)

	// Test with invalid payload that can't be marshaled
	invalidPayload := map[string]interface{}{
		"channel": make(chan int), // channels can't be marshaled to JSON
	}
	err = auth.SendPushNotification(context.Background(), ts.URL, invalidPayload)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to marshal payload")
}

func TestPushNotificationAuthenticator_CreateAuthorizationHeader(t *testing.T) {
	auth := NewPushNotificationAuthenticator()
	err := auth.GenerateKeyPair()
	require.NoError(t, err)

	payload := []byte(`{"test":"data"}`)
	header, err := auth.CreateAuthorizationHeader(payload)

	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(header, "Bearer "))

	// Test error case
	authNoKey := NewPushNotificationAuthenticator()
	_, err = authNoKey.CreateAuthorizationHeader(payload)
	assert.Error(t, err)
}

func TestPushNotificationAuthenticator_SetJWKSClient(t *testing.T) {
	auth := NewPushNotificationAuthenticator()

	// Initially jwksClient should be nil
	assert.Nil(t, auth.jwksClient)

	// Set JWKS client
	auth.SetJWKSClient("http://example.com/jwks.json")

	// Verify client was set
	assert.NotNil(t, auth.jwksClient)
	assert.Equal(t, "http://example.com/jwks.json", auth.jwksClient.jwksURL)
}

func TestPushNotificationAuthenticator_VerifyPushNotification(t *testing.T) {
	// This would require more complex setup with mocking
	// Skipping for now as it's complex to test due to dependencies
	t.Skip("Requires complex integration test setup")
}
