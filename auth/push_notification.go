// Package auth provides authentication and authorization functionality for the A2A protocol.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/lestrrat-go/jwx/v2/jwk"
)

// TokenType represents the authentication token type.
type TokenType string

const (
	// TokenTypeBearer represents a bearer token.
	TokenTypeBearer TokenType = "Bearer"
)

// Authentication errors.
var (
	ErrMissingToken      = errors.New("missing authentication token")
	ErrInvalidAuthHeader = errors.New("invalid authorization header format")
	ErrInvalidToken      = errors.New("invalid authentication token")
	ErrTokenExpired      = errors.New("token has expired")
)

// PushNotificationAuthenticator handles authentication for push notifications.
type PushNotificationAuthenticator struct {
	// For sending notifications (agent side).
	privateKey *rsa.PrivateKey
	keySet     jwk.Set
	keyID      string

	// For verifying notifications (client side).
	jwksClient *JWKSClient
}

// NewPushNotificationAuthenticator creates a new push notification authenticator.
func NewPushNotificationAuthenticator() *PushNotificationAuthenticator {
	return &PushNotificationAuthenticator{
		keySet: jwk.NewSet(),
	}
}

// GenerateKeyPair generates a new RSA key pair for signing push notifications.
func (a *PushNotificationAuthenticator) GenerateKeyPair() error {
	// Generate a new RSA key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("failed to generate RSA key: %w", err)
	}

	a.privateKey = privateKey
	a.keyID = fmt.Sprintf("key-%d", time.Now().Unix())

	// Create a JWK from the private key
	key, err := jwk.FromRaw(privateKey.Public())
	if err != nil {
		return fmt.Errorf("failed to create JWK from public key: %w", err)
	}

	// Set key ID
	if err := key.Set(jwk.KeyIDKey, a.keyID); err != nil {
		return fmt.Errorf("failed to set key ID: %w", err)
	}

	// Set key usage
	if err := key.Set(jwk.KeyUsageKey, "sig"); err != nil {
		return fmt.Errorf("failed to set key usage: %w", err)
	}

	// Set algorithm
	if err := key.Set(jwk.AlgorithmKey, "RS256"); err != nil {
		return fmt.Errorf("failed to set key algorithm: %w", err)
	}

	// Add the key to the key set
	a.keySet.AddKey(key)

	return nil
}

// SignPayload signs a payload for push notification.
func (a *PushNotificationAuthenticator) SignPayload(payload []byte) (string, error) {
	if a.privateKey == nil {
		return "", errors.New("private key not initialized")
	}
	// Calculate SHA256 hash of payload.
	hash := sha256.Sum256(payload)
	payloadHash := fmt.Sprintf("%x", hash)
	// Create token with claims.
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iat":                 time.Now().Unix(),
		"request_body_sha256": payloadHash,
	})
	// Set key ID in token header.
	token.Header["kid"] = a.keyID
	// Sign the token.
	tokenString, err := token.SignedString(a.privateKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}
	return tokenString, nil
}

// HandleJWKS handles requests to the JWKS endpoint.
func (a *PushNotificationAuthenticator) HandleJWKS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Marshal the entire key set to JSON
	keySetJSON, err := json.Marshal(a.keySet)
	if err != nil {
		http.Error(w, "Failed to marshal key set", http.StatusInternalServerError)
		return
	}

	// Parse the key set JSON to extract keys properly
	var keySetMap map[string]interface{}
	if err := json.Unmarshal(keySetJSON, &keySetMap); err != nil {
		http.Error(w, "Failed to process key set", http.StatusInternalServerError)
		return
	}

	// Construct the proper response format
	response := map[string]interface{}{
		"keys": keySetMap["keys"],
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "Failed to encode JWKS", http.StatusInternalServerError)
	}
}

// JWKSClient retrieves and caches JWKs from a remote endpoint.
type JWKSClient struct {
	jwksURL   string
	keySet    jwk.Set
	lastFetch time.Time
	cacheTTL  time.Duration
}

// NewJWKSClient creates a new JWKS client for a specific URL.
func NewJWKSClient(jwksURL string, cacheTTL time.Duration) *JWKSClient {
	if cacheTTL == 0 {
		cacheTTL = 1 * time.Hour
	}
	return &JWKSClient{
		jwksURL:  jwksURL,
		keySet:   jwk.NewSet(),
		cacheTTL: cacheTTL,
	}
}

// FetchKeys fetches the JWKs from the remote endpoint.
func (c *JWKSClient) FetchKeys(ctx context.Context) error {
	// Check if we need to refresh the keys.
	if !c.lastFetch.IsZero() && time.Since(c.lastFetch) < c.cacheTTL {
		return nil
	}
	// Fetch the JWKs from the remote endpoint.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.jwksURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch JWKS: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}
	var jwksResponse struct {
		Keys []json.RawMessage `json:"keys"`
	}
	if err := json.Unmarshal(body, &jwksResponse); err != nil {
		return fmt.Errorf("failed to unmarshal JWKS response: %w", err)
	}
	// Create a new key set.
	newKeySet := jwk.NewSet()
	for _, rawKey := range jwksResponse.Keys {
		key, err := jwk.ParseKey(rawKey)
		if err != nil {
			return fmt.Errorf("failed to parse JWK: %w", err)
		}
		newKeySet.AddKey(key)
	}
	c.keySet = newKeySet
	c.lastFetch = time.Now()
	return nil
}

// GetKey returns a key with the specified ID.
func (c *JWKSClient) GetKey(ctx context.Context, keyID string) (jwk.Key, error) {
	if err := c.FetchKeys(ctx); err != nil {
		return nil, err
	}
	key, found := c.keySet.LookupKeyID(keyID)
	if !found {
		return nil, fmt.Errorf("key with ID %s not found", keyID)
	}
	return key, nil
}

// VerifyPushNotification verifies a push notification JWT and payload.
func (a *PushNotificationAuthenticator) VerifyPushNotification(r *http.Request, payload []byte) error {
	// Initialize the JWKS client if needed.
	if a.jwksClient == nil {
		return errors.New("JWKS client not initialized")
	}
	// Extract the JWT from the Authorization header.
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return ErrMissingToken
	}
	parts := strings.Split(authHeader, " ")
	if len(parts) != 2 || !strings.EqualFold(parts[0], string(TokenTypeBearer)) {
		return ErrInvalidAuthHeader
	}
	tokenString := parts[1]
	// Parse the JWT without verifying to extract the key ID.
	token, _, err := new(jwt.Parser).ParseUnverified(tokenString, jwt.MapClaims{})
	if err != nil {
		return fmt.Errorf("failed to parse token: %w", err)
	}
	// Extract the key ID from the token header.
	keyID, ok := token.Header["kid"].(string)
	if !ok {
		return errors.New("token missing key ID")
	}
	// Get the public key from the JWKS.
	key, err := a.jwksClient.GetKey(r.Context(), keyID)
	if err != nil {
		return fmt.Errorf("failed to get key: %w", err)
	}
	// Extract the public key.
	var publicKey interface{}
	if err := key.Raw(&publicKey); err != nil {
		return fmt.Errorf("failed to extract public key: %w", err)
	}
	// Parse and validate the token.
	claims := jwt.MapClaims{}
	parsedToken, err := jwt.ParseWithClaims(
		tokenString, claims, func(token *jwt.Token) (interface{}, error) {
			return publicKey, nil
		})
	if err != nil {
		return fmt.Errorf("failed to validate token: %w", err)
	}
	if !parsedToken.Valid {
		return ErrInvalidToken
	}
	// Verify the payload hash.
	hash := sha256.Sum256(payload)
	payloadHash := fmt.Sprintf("%x", hash)
	if claimHash, ok := claims["request_body_sha256"].(string); !ok || claimHash != payloadHash {
		return errors.New("payload hash mismatch")
	}
	// Verify the token age.
	if iat, ok := claims["iat"].(float64); ok {
		tokenAge := time.Since(time.Unix(int64(iat), 0))
		if tokenAge > 5*time.Minute {
			return ErrTokenExpired
		}
	} else {
		return errors.New("token missing issued at time")
	}
	return nil
}

// SetJWKSClient sets the JWKS client for verifying push notifications.
func (a *PushNotificationAuthenticator) SetJWKSClient(jwksURL string) {
	a.jwksClient = NewJWKSClient(jwksURL, 1*time.Hour)
}

// CreateAuthorizationHeader creates an Authorization header for push notifications.
func (a *PushNotificationAuthenticator) CreateAuthorizationHeader(payload []byte) (string, error) {
	token, err := a.SignPayload(payload)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s %s", TokenTypeBearer, token), nil
}

// SendPushNotification sends a push notification with JWT authentication.
// This is a convenience function for sending notifications from the server side.
func (a *PushNotificationAuthenticator) SendPushNotification(
	ctx context.Context,
	url string,
	payload interface{},
) error {
	// Validate inputs
	if url == "" {
		return errors.New("push notification URL is required")
	}

	// Make sure URL starts with http:// or https://
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		url = "http://" + url
	}

	// Marshal payload to JSON
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Create authorization header
	authHeader, err := a.CreateAuthorizationHeader(payloadBytes)
	if err != nil {
		return fmt.Errorf("failed to create authorization header: %w", err)
	}

	// Send push notification
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, url, strings.NewReader(string(payloadBytes)))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", authHeader)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send push notification: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("push notification failed with status: %s", resp.Status)
	}

	return nil
}
