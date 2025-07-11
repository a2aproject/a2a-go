// Package auth provides authentication and authorization functionality for the A2A protocol.
package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

// ContextKey type for context values.
type ContextKey string

// Context keys.
const (
	// AuthUserKey is the key used to store the authenticated user in the context.
	AuthUserKey ContextKey = "auth_user"
	// AuthHeaderName is the default name of the header containing the authentication token.
	AuthHeaderName = "Authorization"
)

// User represents an authenticated user.
type User struct {
	// ID is the unique identifier of the user.
	ID string
	// Claims contains any additional JWT claims.
	Claims jwt.MapClaims
	// Additional information for OAuth2 users
	OAuth2Info *OAuth2UserInfo
}

// OAuth2UserInfo contains additional user information from OAuth2 providers.
type OAuth2UserInfo struct {
	// AccessToken is the OAuth2 access token.
	AccessToken string
	// TokenType is the type of token (e.g., "Bearer").
	TokenType string
	// Expiry is when the token expires.
	Expiry time.Time
	// Scope contains the granted OAuth2 scopes.
	Scope string
}

// Provider is an interface for authentication providers.
type Provider interface {
	// Authenticate authenticates a request.
	Authenticate(r *http.Request) (*User, error)
}

// ClientProvider is an extension of Provider that also supports client configuration.
type ClientProvider interface {
	Provider
	// ConfigureClient configures an HTTP client with authentication.
	ConfigureClient(client *http.Client) *http.Client
}

// JWTAuthProvider authenticates requests using JWT.
type JWTAuthProvider struct {
	// Secret is the secret key for validating JWT tokens.
	Secret []byte
	// Audience is the expected audience of the JWT.
	Audience string
	// Issuer is the expected issuer of the JWT.
	Issuer string
	// TokenLifetime is the duration for which a token is valid.
	TokenLifetime time.Duration
}

// NewJWTAuthProvider creates a new JWT authentication provider.
func NewJWTAuthProvider(secret []byte, audience, issuer string, lifetime time.Duration) *JWTAuthProvider {
	if lifetime == 0 {
		lifetime = 24 * time.Hour
	}
	return &JWTAuthProvider{
		Secret:        secret,
		Audience:      audience,
		Issuer:        issuer,
		TokenLifetime: lifetime,
	}
}

// Authenticate validates a JWT from the request's Authorization header.
func (p *JWTAuthProvider) Authenticate(r *http.Request) (*User, error) {
	// Extract token from Authorization header.
	authHeader := r.Header.Get(AuthHeaderName)
	if authHeader == "" {
		return nil, ErrMissingToken
	}
	// Example: Authorization: Bearer <token>
	parts := strings.Split(authHeader, " ")
	if len(parts) != 2 || !strings.EqualFold(parts[0], string(TokenTypeBearer)) {
		return nil, ErrInvalidAuthHeader
	}
	tokenString := parts[1]
	// Parse and validate the token.
	claims := jwt.MapClaims{}
	token, err := jwt.ParseWithClaims(
		tokenString,
		claims,
		func(token *jwt.Token) (interface{}, error) {
			// Validate the token signing method.
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %T", token.Method)
			}
			return p.Secret, nil
		},
		jwt.WithAudience(p.Audience),
		jwt.WithIssuer(p.Issuer),
	)
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, ErrInvalidToken
	}
	// Extract user ID from claims.
	subject, err := token.Claims.GetSubject()
	if err != nil {
		return nil, fmt.Errorf("missing subject claim: %w", err)
	}
	// Create and return the authenticated user.
	user := &User{
		ID:     subject,
		Claims: claims,
	}
	return user, nil
}

// CreateToken creates a new JWT token.
func (p *JWTAuthProvider) CreateToken(userID string, customClaims map[string]interface{}) (string, error) {
	now := time.Now()
	expiresAt := now.Add(p.TokenLifetime)
	const (
		subKey = "sub"
		iatKey = "iat"
		expKey = "exp"
		audKey = "aud"
		issKey = "iss"
	)
	// Create standard claims.
	claims := jwt.MapClaims{
		subKey: userID,
		iatKey: now.Unix(),
		expKey: expiresAt.Unix(),
	}
	// Add audience and issuer if set.
	if p.Audience != "" {
		claims[audKey] = p.Audience
	}
	if p.Issuer != "" {
		claims[issKey] = p.Issuer
	}
	// Add custom claims.
	for k, v := range customClaims {
		claims[k] = v
	}
	// Create and sign the token.
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(p.Secret)
	if err != nil {
		return "", err
	}
	return tokenString, nil
}

// ConfigureClient implements ClientProvider interface.
func (p *JWTAuthProvider) ConfigureClient(client *http.Client) *http.Client {
	// Create a transport that adds the JWT token to requests
	transport := &jwtAuthTransport{
		base:     client.Transport,
		provider: p,
		userID:   "client", // Default user ID for client auth
	}

	// If the client transport is nil, initialize with http.DefaultTransport
	if transport.base == nil {
		transport.base = http.DefaultTransport
	}

	// Return a new client with our custom transport
	newClient := *client
	newClient.Transport = transport
	return &newClient
}

// Headers returns the header keys and values to be added to the request.
func (p *JWTAuthProvider) Headers() (map[string]string, error) {
	token, err := p.CreateToken("client", nil)
	if err != nil {
		return nil, err
	}
	return map[string]string{
		AuthHeaderName: fmt.Sprintf("%s %s", TokenTypeBearer, token),
	}, nil
}

// jwtAuthTransport is an http.RoundTripper that adds JWT authentication.
type jwtAuthTransport struct {
	base     http.RoundTripper
	provider *JWTAuthProvider
	userID   string
}

// RoundTrip implements http.RoundTripper.
func (t *jwtAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Create a new token for each request
	token, err := t.provider.CreateToken(t.userID, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create JWT token: %w", err)
	}

	// Clone the request to avoid modifying the original
	reqClone := req.Clone(req.Context())
	reqClone.Header.Set(AuthHeaderName, fmt.Sprintf("%s %s", TokenTypeBearer, token))

	// Continue with the base transport
	return t.base.RoundTrip(reqClone)
}

// APIKeyAuthProvider authenticates requests using API keys.
type APIKeyAuthProvider struct {
	// KeyMap maps API keys to user IDs.
	KeyMap map[string]string
	// HeaderName is the name of the header containing the API key.
	HeaderName string
	// APIKey to use for client configuration
	clientAPIKey string
}

// NewAPIKeyAuthProvider creates a new API key authentication provider.
func NewAPIKeyAuthProvider(keyMap map[string]string, headerName string) *APIKeyAuthProvider {
	const defaultHeaderName = "X-API-Key"
	if headerName == "" {
		headerName = defaultHeaderName
	}
	return &APIKeyAuthProvider{
		KeyMap:     keyMap,
		HeaderName: headerName,
	}
}

// SetClientAPIKey sets the API key to use when configuring clients.
func (p *APIKeyAuthProvider) SetClientAPIKey(apiKey string) {
	p.clientAPIKey = apiKey
}

// Authenticate validates an API key from the request's header.
func (p *APIKeyAuthProvider) Authenticate(r *http.Request) (*User, error) {
	apiKey := r.Header.Get(p.HeaderName)
	if apiKey == "" {
		return nil, ErrMissingToken
	}
	userID, ok := p.KeyMap[apiKey]
	if !ok {
		return nil, ErrInvalidToken
	}
	user := &User{
		ID:     userID,
		Claims: jwt.MapClaims{},
	}
	return user, nil
}

// ConfigureClient implements ClientProvider interface.
func (p *APIKeyAuthProvider) ConfigureClient(client *http.Client) *http.Client {
	if p.clientAPIKey == "" {
		// If no API key is set, return the original client
		return client
	}

	// Create a transport that adds the API key to requests
	transport := &apiKeyAuthTransport{
		base:       client.Transport,
		headerName: p.HeaderName,
		apiKey:     p.clientAPIKey,
	}

	// If the client transport is nil, initialize with http.DefaultTransport
	if transport.base == nil {
		transport.base = http.DefaultTransport
	}

	// Return a new client with our custom transport
	newClient := *client
	newClient.Transport = transport
	return &newClient
}

// Headers returns the header keys and values to be added to the request.
func (p *APIKeyAuthProvider) Headers() (map[string]string, error) {
	if p.clientAPIKey == "" {
		return nil, nil
	}
	return map[string]string{
		p.HeaderName: p.clientAPIKey,
	}, nil
}

// apiKeyAuthTransport is an http.RoundTripper that adds API key authentication.
type apiKeyAuthTransport struct {
	base       http.RoundTripper
	headerName string
	apiKey     string
}

// RoundTrip implements http.RoundTripper.
func (t *apiKeyAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone the request to avoid modifying the original
	reqClone := req.Clone(req.Context())
	reqClone.Header.Set(t.headerName, t.apiKey)

	// Continue with the base transport
	return t.base.RoundTrip(reqClone)
}

// OAuth2AuthProvider authenticates requests using OAuth2.
type OAuth2AuthProvider struct {
	// OAuth2 configuration
	config *oauth2.Config
	// Client credentials for client_credentials grant
	clientCredentials *clientcredentials.Config
	// Token source for client configuration
	tokenSource oauth2.TokenSource
	// UserInfoURL for fetching user details after authentication
	userInfoURL string
	// UserIDField is the JSON field name that contains the user ID in the userinfo response
	userIDField string
}

// NewOAuth2AuthProviderWithConfig creates a new OAuth2 authentication provider with custom OAuth2 config.
func NewOAuth2AuthProviderWithConfig(config *oauth2.Config, userInfoURL, userIDField string) *OAuth2AuthProvider {
	if userIDField == "" {
		userIDField = "sub" // Default to OpenID Connect standard
	}

	return &OAuth2AuthProvider{
		config:      config,
		userInfoURL: userInfoURL,
		userIDField: userIDField,
	}
}

// NewOAuth2ClientCredentialsProvider creates a new OAuth2 provider for client credentials flow.
func NewOAuth2ClientCredentialsProvider(clientID, clientSecret, tokenURL string, scopes []string) *OAuth2AuthProvider {
	config := &clientcredentials.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		TokenURL:     tokenURL,
		Scopes:       scopes,
	}

	return &OAuth2AuthProvider{
		clientCredentials: config,
		tokenSource:       config.TokenSource(context.Background()),
	}
}

// Authenticate validates an OAuth2 token from the request's Authorization header.
func (p *OAuth2AuthProvider) Authenticate(r *http.Request) (*User, error) {
	// Extract token from Authorization header
	authHeader := r.Header.Get(AuthHeaderName)
	if authHeader == "" {
		return nil, ErrMissingToken
	}

	// Example: Authorization: Bearer <token>
	parts := strings.Split(authHeader, " ")
	if len(parts) != 2 || !strings.EqualFold(parts[0], string(TokenTypeBearer)) {
		return nil, ErrInvalidAuthHeader
	}
	tokenString := parts[1]

	// Create a token with just the access token (this doesn't validate, just wraps)
	token := &oauth2.Token{
		AccessToken: tokenString,
		TokenType:   "Bearer",
	}

	// If we have a userinfo URL, fetch user details
	if p.userInfoURL != "" {
		return p.getUserFromUserInfo(r.Context(), token)
	}

	// For simple validation, create a minimal user
	return &User{
		ID: "oauth2-user", // Generic user ID without userinfo
		OAuth2Info: &OAuth2UserInfo{
			AccessToken: tokenString,
			TokenType:   "Bearer",
		},
	}, nil
}

// ConfigureClient implements ClientProvider interface.
func (p *OAuth2AuthProvider) ConfigureClient(client *http.Client) *http.Client {
	// If we have a client credentials config, create a client with that
	if p.clientCredentials != nil {
		return p.clientCredentials.Client(context.Background())
	}

	// If we have a token source already (from a previous auth), use that
	if p.tokenSource != nil {
		ctx := context.WithValue(context.Background(), oauth2.HTTPClient, client)
		return oauth2.NewClient(ctx, p.tokenSource)
	}

	// If we have a config but no token yet, we can't configure a client
	// (would need an authorization code or other grant type flow)
	return client
}

// getUserFromUserInfo fetches user info from the userinfo endpoint
func (p *OAuth2AuthProvider) getUserFromUserInfo(ctx context.Context, token *oauth2.Token) (*User, error) {
	client := oauth2.NewClient(ctx, oauth2.StaticTokenSource(token))

	// Make request to userinfo endpoint
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.userInfoURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("userinfo request failed with status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Parse the userinfo response
	var userInfo map[string]interface{}
	if err := json.Unmarshal(body, &userInfo); err != nil {
		return nil, err
	}

	// Extract user ID
	userIDValue, ok := userInfo[p.userIDField]
	if !ok {
		return nil, fmt.Errorf("userinfo response missing user ID field '%s'", p.userIDField)
	}

	userID, ok := userIDValue.(string)
	if !ok {
		return nil, fmt.Errorf("user ID field '%s' is not a string", p.userIDField)
	}

	// Create claims from userinfo data
	claims := jwt.MapClaims{}
	for k, v := range userInfo {
		claims[k] = v
	}

	return &User{
		ID:     userID,
		Claims: claims,
		OAuth2Info: &OAuth2UserInfo{
			AccessToken: token.AccessToken,
			TokenType:   token.TokenType,
			Expiry:      token.Expiry,
			Scope:       getScopeFromToken(token),
		},
	}, nil
}

// getScopeFromToken safely extracts the scope from a token
func getScopeFromToken(token *oauth2.Token) string {
	if scope := token.Extra("scope"); scope != nil {
		if scopeStr, ok := scope.(string); ok {
			return scopeStr
		}
	}
	return ""
}

// SetTokenSource allows setting a token source for client configuration.
func (p *OAuth2AuthProvider) SetTokenSource(tokenSource oauth2.TokenSource) {
	p.tokenSource = tokenSource
}

// ChainAuthProvider chains multiple authentication providers.
type ChainAuthProvider struct {
	providers []Provider
}

// NewChainAuthProvider creates a new chain authentication provider.
func NewChainAuthProvider(providers ...Provider) *ChainAuthProvider {
	return &ChainAuthProvider{
		providers: providers,
	}
}

// Authenticate tries each authentication provider in order, returning the first success.
func (p *ChainAuthProvider) Authenticate(r *http.Request) (*User, error) {
	var lastErr error
	for _, provider := range p.providers {
		user, err := provider.Authenticate(r)
		if err == nil {
			return user, nil
		}
		lastErr = err
	}

	return nil, lastErr
}

// ConfigureClient configures an HTTP client with the first ClientProvider in the chain.
// If no providers implement ClientProvider, returns the original client.
func (p *ChainAuthProvider) ConfigureClient(client *http.Client) *http.Client {
	for _, provider := range p.providers {
		if clientProvider, ok := provider.(ClientProvider); ok {
			return clientProvider.ConfigureClient(client)
		}
	}
	return client
}

// Middleware wraps an HTTP handler with authentication.
type Middleware struct {
	provider Provider
}

// NewMiddleware creates a new authentication middleware.
func NewMiddleware(provider Provider) *Middleware {
	return &Middleware{
		provider: provider,
	}
}

// Wrap adds authentication to an HTTP handler.
func (m *Middleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := m.provider.Authenticate(r)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		// Add the authenticated user to the request context.
		ctx := context.WithValue(r.Context(), AuthUserKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
