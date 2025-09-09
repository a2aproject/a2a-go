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

package a2a

// Defines a security scheme that can be used to secure an agent's endpoints.
// This is a discriminated union type based on the OpenAPI 3.0 Security Scheme.
type SecurityScheme interface {
	isSecurityScheme()
}

func (APIKeySecurityScheme) isSecurityScheme()        {}
func (HTTPAuthSecurityScheme) isSecurityScheme()      {}
func (OpenIDConnectSecurityScheme) isSecurityScheme() {}
func (MutualTLSSecurityScheme) isSecurityScheme()     {}
func (OAuth2SecurityScheme) isSecurityScheme()        {}

// Defines a security scheme using an API key.
type APIKeySecurityScheme struct {
	// An optional description for the security scheme.
	Description string `json:"description,omitempty" yaml:"description,omitempty" mapstructure:"description,omitempty"`

	// The location of the API key.
	In APIKeySecuritySchemeIn `json:"in" yaml:"in" mapstructure:"in"`

	// The name of the header, query, or cookie parameter to be used.
	Name string `json:"name" yaml:"name" mapstructure:"name"`

	// The type of the security scheme. Must be 'apiKey'.
	Type string `json:"type" yaml:"type" mapstructure:"type"`
}

type APIKeySecuritySchemeIn string

const (
	APIKeySecuritySchemeInCookie APIKeySecuritySchemeIn = "cookie"
	APIKeySecuritySchemeInHeader APIKeySecuritySchemeIn = "header"
	APIKeySecuritySchemeInQuery  APIKeySecuritySchemeIn = "query"
)

// Defines configuration details for the OAuth 2.0 Authorization Code flow.
type AuthorizationCodeOAuthFlow struct {
	// The authorization URL to be used for this flow.
	// This MUST be a URL and use TLS.
	AuthorizationURL string `json:"authorizationUrl" yaml:"authorizationUrl" mapstructure:"authorizationUrl"`

	// An optional URL to be used for obtaining refresh tokens.
	// This MUST be a URL and use TLS.
	RefreshURL string `json:"refreshUrl,omitempty" yaml:"refreshUrl,omitempty" mapstructure:"refreshUrl,omitempty"`

	// The available scopes for the OAuth2 security scheme. A map between the scope
	// name and a short description for it.
	Scopes map[string]string `json:"scopes" yaml:"scopes" mapstructure:"scopes"`

	// The token URL to be used for this flow.
	// This MUST be a URL and use TLS.
	TokenURL string `json:"tokenUrl" yaml:"tokenUrl" mapstructure:"tokenUrl"`
}

// Defines configuration details for the OAuth 2.0 Client Credentials flow.
type ClientCredentialsOAuthFlow struct {
	// An optional URL to be used for obtaining refresh tokens. This MUST be a URL.
	RefreshURL string `json:"refreshUrl,omitempty" yaml:"refreshUrl,omitempty" mapstructure:"refreshUrl,omitempty"`

	// The available scopes for the OAuth2 security scheme. A map between the scope
	// name and a short description for it.
	Scopes map[string]string `json:"scopes" yaml:"scopes" mapstructure:"scopes"`

	// The token URL to be used for this flow. This MUST be a URL.
	TokenURL string `json:"tokenUrl" yaml:"tokenUrl" mapstructure:"tokenUrl"`
}

// Defines a security scheme using HTTP authentication.
type HTTPAuthSecurityScheme struct {
	// An optional hint to the client to identify how the bearer token is formatted (e.g.,
	// "JWT"). This is primarily for documentation purposes.
	BearerFormat string `json:"bearerFormat,omitempty" yaml:"bearerFormat,omitempty" mapstructure:"bearerFormat,omitempty"`

	// An optional description for the security scheme.
	Description string `json:"description,omitempty" yaml:"description,omitempty" mapstructure:"description,omitempty"`

	// The name of the HTTP Authentication scheme to be used in the Authorization
	// header,
	// as defined in RFC7235 (e.g., "Bearer").
	// This value should be registered in the IANA Authentication Scheme registry.
	Scheme string `json:"scheme" yaml:"scheme" mapstructure:"scheme"`

	// The type of the security scheme. Must be 'http'.
	Type string `json:"type" yaml:"type" mapstructure:"type"`
}

// Defines configuration details for the OAuth 2.0 Implicit flow.
type ImplicitOAuthFlow struct {
	// The authorization URL to be used for this flow. This MUST be a URL.
	AuthorizationURL string `json:"authorizationUrl" yaml:"authorizationUrl" mapstructure:"authorizationUrl"`

	// An optional URL to be used for obtaining refresh tokens. This MUST be a URL.
	RefreshURL string `json:"refreshUrl,omitempty" yaml:"refreshUrl,omitempty" mapstructure:"refreshUrl,omitempty"`

	// The available scopes for the OAuth2 security scheme. A map between the scope
	// name and a short description for it.
	Scopes map[string]string `json:"scopes" yaml:"scopes" mapstructure:"scopes"`
}

// Defines a security scheme using mTLS authentication.
type MutualTLSSecurityScheme struct {
	// An optional description for the security scheme.
	Description string `json:"description,omitempty" yaml:"description,omitempty" mapstructure:"description,omitempty"`

	// The type of the security scheme. Must be 'mutualTLS'.
	Type string `json:"type" yaml:"type" mapstructure:"type"`
}

// Defines a security scheme using OAuth 2.0.
type OAuth2SecurityScheme struct {
	// An optional description for the security scheme.
	Description string `json:"description,omitempty" yaml:"description,omitempty" mapstructure:"description,omitempty"`

	// An object containing configuration information for the supported OAuth 2.0
	// flows.
	Flows OAuthFlows `json:"flows" yaml:"flows" mapstructure:"flows"`

	// An optional URL to the oauth2 authorization server metadata
	// [RFC8414](https://datatracker.ietf.org/doc/html/rfc8414). TLS is required.
	Oauth2MetadataURL string `json:"oauth2MetadataUrl,omitempty" yaml:"oauth2MetadataUrl,omitempty" mapstructure:"oauth2MetadataUrl,omitempty"`

	// The type of the security scheme. Must be 'oauth2'.
	Type string `json:"type" yaml:"type" mapstructure:"type"`
}

// Defines the configuration for the supported OAuth 2.0 flows.
type OAuthFlows struct {
	// Configuration for the OAuth Authorization Code flow. Previously called
	// accessCode in OpenAPI 2.0.
	AuthorizationCode *AuthorizationCodeOAuthFlow `json:"authorizationCode,omitempty" yaml:"authorizationCode,omitempty" mapstructure:"authorizationCode,omitempty"`

	// Configuration for the OAuth Client Credentials flow. Previously called
	// application in OpenAPI 2.0.
	ClientCredentials *ClientCredentialsOAuthFlow `json:"clientCredentials,omitempty" yaml:"clientCredentials,omitempty" mapstructure:"clientCredentials,omitempty"`

	// Configuration for the OAuth Implicit flow.
	Implicit *ImplicitOAuthFlow `json:"implicit,omitempty" yaml:"implicit,omitempty" mapstructure:"implicit,omitempty"`

	// Configuration for the OAuth Resource Owner Password flow.
	Password *PasswordOAuthFlow `json:"password,omitempty" yaml:"password,omitempty" mapstructure:"password,omitempty"`
}

// Defines a security scheme using OpenID Connect.
type OpenIDConnectSecurityScheme struct {
	// An optional description for the security scheme.
	Description string `json:"description,omitempty" yaml:"description,omitempty" mapstructure:"description,omitempty"`

	// The OpenID Connect Discovery URL for the OIDC provider's metadata.
	OpenIDConnectURL string `json:"openIdConnectUrl" yaml:"openIdConnectUrl" mapstructure:"openIdConnectUrl"`

	// The type of the security scheme. Must be 'openIDConnect'.
	Type string `json:"type" yaml:"type" mapstructure:"type"`
}

// Defines configuration details for the OAuth 2.0 Resource Owner Password flow.
type PasswordOAuthFlow struct {
	// An optional URL to be used for obtaining refresh tokens. This MUST be a URL.
	RefreshURL string `json:"refreshUrl,omitempty" yaml:"refreshUrl,omitempty" mapstructure:"refreshUrl,omitempty"`

	// The available scopes for the OAuth2 security scheme. A map between the scope
	// name and a short description for it.
	Scopes map[string]string `json:"scopes" yaml:"scopes" mapstructure:"scopes"`

	// The token URL to be used for this flow. This MUST be a URL.
	TokenURL string `json:"tokenUrl" yaml:"tokenUrl" mapstructure:"tokenUrl"`
}
