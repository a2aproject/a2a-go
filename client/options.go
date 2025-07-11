package client

import (
	"context"
	"net/http"
	"time"

	"github.com/a2aproject/a2a-go/auth"
	"golang.org/x/oauth2"
)

// Option is a functional option type for configuring the A2AClient.
type Option func(*A2AClient)

// HTTPReqHandler is a custom HTTP request handler for a2a client.
type HTTPReqHandler interface {
	Handle(ctx context.Context, client *http.Client, req *http.Request) (*http.Response, error)
}

// WithHTTPClient sets a custom http.Client for the A2AClient.
func WithHTTPClient(client *http.Client) Option {
	return func(c *A2AClient) {
		if client != nil {
			c.httpClient = client
		}
	}
}

// WithTimeout sets the timeout for the underlying http.Client.
// If a custom client was provided via WithHTTPClient, this modifies its timeout.
func WithTimeout(timeout time.Duration) Option {
	return func(c *A2AClient) {
		if timeout > 0 && c.httpClient != nil {
			c.httpClient.Timeout = timeout
		}
	}
}

// WithUserAgent sets a custom User-Agent header for requests.
func WithUserAgent(userAgent string) Option {
	return func(c *A2AClient) {
		c.userAgent = userAgent
	}
}

// Authentication options

// WithJWTAuth configures the client to use JWT authentication.
func WithJWTAuth(secret []byte, audience, issuer string, lifetime time.Duration) Option {
	return func(c *A2AClient) {
		provider := auth.NewJWTAuthProvider(secret, audience, issuer, lifetime)
		c.authProvider = provider
		c.httpClient = provider.ConfigureClient(c.httpClient)
	}
}

// WithAPIKeyAuth configures the client to use API key authentication.
func WithAPIKeyAuth(apiKey, headerName string) Option {
	return func(c *A2AClient) {
		provider := auth.NewAPIKeyAuthProvider(make(map[string]string), headerName)
		provider.SetClientAPIKey(apiKey)
		c.authProvider = provider
		c.httpClient = provider.ConfigureClient(c.httpClient)
	}
}

// WithOAuth2ClientCredentials configures the client to use OAuth2 client credentials flow.
func WithOAuth2ClientCredentials(clientID, clientSecret, tokenURL string, scopes []string) Option {
	return func(c *A2AClient) {
		provider := auth.NewOAuth2ClientCredentialsProvider(
			clientID,
			clientSecret,
			tokenURL,
			scopes,
		)
		c.authProvider = provider
		c.httpClient = provider.ConfigureClient(c.httpClient)
	}
}

// WithOAuth2TokenSource configures the client to use a custom OAuth2 token source.
func WithOAuth2TokenSource(config *oauth2.Config, tokenSource oauth2.TokenSource) Option {
	return func(c *A2AClient) {
		provider := auth.NewOAuth2AuthProviderWithConfig(config, "", "")
		provider.SetTokenSource(tokenSource)
		c.authProvider = provider
		c.httpClient = provider.ConfigureClient(c.httpClient)
	}
}

// WithAuthProvider allows using a custom auth provider that implements the ClientProvider interface.
func WithAuthProvider(provider auth.ClientProvider) Option {
	return func(c *A2AClient) {
		c.authProvider = provider
		c.httpClient = provider.ConfigureClient(c.httpClient)
	}
}

// WithHTTPReqHandler sets a custom HTTP request handler for the A2AClient.
func WithHTTPReqHandler(handler HTTPReqHandler) Option {
	return func(c *A2AClient) {
		c.httpReqHandler = handler
	}
}
