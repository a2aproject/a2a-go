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

import (
	"encoding/gob"
	"encoding/json"
	"fmt"
)

// SecurityRequirements describes a set of security requirements that must be present on a request.
// For example, to specify that mutual TLS AND an oauth2 token for specific scopes is required, the
// following requirements object needs to be created:
//
//	map[SecuritySchemeName]SecuritySchemeScopes{
//		SecuritySchemeName("oauth2"): SecuritySchemeScopes{"read", "write"},
//		SecuritySchemeName("mTLS"): {}
//	}
type SecurityRequirements map[SecuritySchemeName]SecuritySchemeScopes

// SecurityRequirementsOptions is a list of security requirement objects that apply to all agent interactions.
// Each object lists security schemes that can be used.
// Follows the OpenAPI 3.0 Security Requirement Object.
// This list can be seen as an OR of ANDs. Each object in the list describes one
// possible set of security requirements that must be present on a request.
// This allows specifying, for example, "callers must either use OAuth OR an API Key AND mTLS.":
//
//	SecurityRequirements: a2a.SecurityRequirementsOptions{
//		map[a2a.SecuritySchemeName]a2a.SecuritySchemeScopes{
//			a2a.SecuritySchemeName("apiKey"): {},
//			a2a.SecuritySchemeName("oauth2"): {"read"},
//		},
//	}
type SecurityRequirementsOptions []SecurityRequirements

// MarshalJSON implements json.Marshaler.
func (rs SecurityRequirementsOptions) MarshalJSON() ([]byte, error) {
	type wrapper struct {
		Schemes map[SecuritySchemeName]SecuritySchemeScopes `json:"schemes"`
	}
	var out []wrapper
	for _, req := range rs {
		out = append(out, wrapper{Schemes: req})
	}
	return json.Marshal(out)
}

// UnmarshalJSON implements json.Unmarshaler.
func (rs *SecurityRequirementsOptions) UnmarshalJSON(b []byte) error {
	type wrapper struct {
		Schemes map[SecuritySchemeName]SecuritySchemeScopes `json:"schemes"`
	}
	var wrapped []wrapper
	if err := json.Unmarshal(b, &wrapped); err != nil {
		return err
	}
	result := make(SecurityRequirementsOptions, 0, len(wrapped))
	for _, w := range wrapped {
		result = append(result, w.Schemes)
	}
	*rs = result
	return nil
}

// SecuritySchemeName is a string used to describe a security scheme in AgentCard.SecuritySchemes
// and reference it the AgentCard.Security requirements.
type SecuritySchemeName string

// SecuritySchemeScopes is a list of scopes a security credential must be covering.
type SecuritySchemeScopes []string

// NamedSecuritySchemes is a declaration of the security schemes available to authorize requests.
// The key is the scheme name. Follows the OpenAPI 3.0 Security Scheme Object.
type NamedSecuritySchemes map[SecuritySchemeName]SecurityScheme

// MarshalJSON implements json.Marshaler.
func (s NamedSecuritySchemes) MarshalJSON() ([]byte, error) {
	out := make(map[SecuritySchemeName]any)
	for name, scheme := range s {
		var wrapped any
		switch v := scheme.(type) {
		case APIKeySecurityScheme:
			wrapped = map[string]any{"apiKey": v}
		case HTTPAuthSecurityScheme:
			wrapped = map[string]any{"http": v}
		case OpenIDConnectSecurityScheme:
			wrapped = map[string]any{"openIdConnect": v}
		case MutualTLSSecurityScheme:
			wrapped = map[string]any{"mutualTLS": v}
		case OAuth2SecurityScheme:
			wrapped = map[string]any{"oauth2": v}
		default:
			return nil, fmt.Errorf("unknown security scheme type %T", v)
		}
		out[name] = wrapped
	}
	return json.Marshal(out)
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *NamedSecuritySchemes) UnmarshalJSON(b []byte) error {
	var schemes map[SecuritySchemeName]json.RawMessage
	if err := json.Unmarshal(b, &schemes); err != nil {
		return err
	}

	result := make(map[SecuritySchemeName]SecurityScheme, len(schemes))
	for name, rawMessage := range schemes {
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(rawMessage, &raw); err != nil {
			return err
		}

		if v, ok := raw["apiKey"]; ok {
			var scheme APIKeySecurityScheme
			if err := json.Unmarshal(v, &scheme); err != nil {
				return err
			}
			result[name] = scheme
		} else if v, ok := raw["http"]; ok {
			var scheme HTTPAuthSecurityScheme
			if err := json.Unmarshal(v, &scheme); err != nil {
				return err
			}
			result[name] = scheme
		} else if v, ok := raw["mutualTLS"]; ok {
			var scheme MutualTLSSecurityScheme
			if err := json.Unmarshal(v, &scheme); err != nil {
				return err
			}
			result[name] = scheme
		} else if v, ok := raw["oauth2"]; ok {
			var scheme OAuth2SecurityScheme
			if err := json.Unmarshal(v, &scheme); err != nil {
				return err
			}
			result[name] = scheme
		} else if v, ok := raw["openIdConnect"]; ok {
			var scheme OpenIDConnectSecurityScheme
			if err := json.Unmarshal(v, &scheme); err != nil {
				return err
			}
			result[name] = scheme
		} else {
			keys := make([]string, 0, len(raw))
			for k := range raw {
				keys = append(keys, k)
			}
			return fmt.Errorf("unknown security scheme type for %q: found keys %v", name, keys)
		}
	}

	*s = result
	return nil
}

// SecurityScheme is a sealed discriminated type union for supported security schemes.
type SecurityScheme interface {
	isSecurityScheme()
}

func (APIKeySecurityScheme) isSecurityScheme()        {}
func (HTTPAuthSecurityScheme) isSecurityScheme()      {}
func (OpenIDConnectSecurityScheme) isSecurityScheme() {}
func (MutualTLSSecurityScheme) isSecurityScheme()     {}
func (OAuth2SecurityScheme) isSecurityScheme()        {}

func init() {
	gob.Register(APIKeySecurityScheme{})
	gob.Register(HTTPAuthSecurityScheme{})
	gob.Register(OpenIDConnectSecurityScheme{})
	gob.Register(MutualTLSSecurityScheme{})
	gob.Register(OAuth2SecurityScheme{})
}

// APIKeySecurityScheme defines a security scheme using an API key.
type APIKeySecurityScheme struct {
	// An optional description for the security scheme.
	Description string `json:"description,omitempty" yaml:"description,omitempty" mapstructure:"description,omitempty"`

	// The location of the API key. Valid values are "query", "header", or "cookie".
	Location APIKeySecuritySchemeLocation `json:"location" yaml:"location" mapstructure:"location"`

	// The name of the header, query, or cookie parameter to be used.
	Name string `json:"name" yaml:"name" mapstructure:"name"`
}

// APIKeySecuritySchemeLocation defines a set of permitted values for the expected API key location in APIKeySecurityScheme.
type APIKeySecuritySchemeLocation string

const (
	// APIKeySecuritySchemeLocationCookie indicates the API key is passed in a cookie.
	APIKeySecuritySchemeLocationCookie APIKeySecuritySchemeLocation = "cookie"
	// APIKeySecuritySchemeLocationHeader indicates the API key is passed in a header.
	APIKeySecuritySchemeLocationHeader APIKeySecuritySchemeLocation = "header"
	// APIKeySecuritySchemeLocationQuery indicates the API key is passed in a query parameter.
	APIKeySecuritySchemeLocationQuery APIKeySecuritySchemeLocation = "query"
)

// HTTPAuthSecurityScheme defines a security scheme using HTTP authentication.
type HTTPAuthSecurityScheme struct {
	// BearerFormat is an optional hint to the client to identify how the bearer token is formatted (e.g.,
	// "JWT"). This is primarily for documentation purposes.
	BearerFormat string `json:"bearerFormat,omitempty" yaml:"bearerFormat,omitempty" mapstructure:"bearerFormat,omitempty"`

	// Description is an optional description for the security scheme.
	Description string `json:"description,omitempty" yaml:"description,omitempty" mapstructure:"description,omitempty"`

	// Scheme is the name of the HTTP Authentication scheme to be used in the Authorization
	// header, as defined in RFC7235 (e.g., "Bearer").
	// This value should be registered in the IANA Authentication Scheme registry.
	Scheme string `json:"scheme" yaml:"scheme" mapstructure:"scheme"`
}

// OpenIDConnectSecurityScheme defines a security scheme using OpenID Connect.
type OpenIDConnectSecurityScheme struct {
	// Description is an optional description for the security scheme.
	Description string `json:"description,omitempty" yaml:"description,omitempty" mapstructure:"description,omitempty"`

	// OpenIDConnectURL is the OpenID Connect Discovery URL for the OIDC provider's metadata.
	OpenIDConnectURL string `json:"openIdConnectUrl" yaml:"openIdConnectUrl" mapstructure:"openIdConnectUrl"`
}

// MutualTLSSecurityScheme defines a security scheme using mTLS authentication.
type MutualTLSSecurityScheme struct {
	// Description is an optional description for the security scheme.
	Description string `json:"description,omitempty" yaml:"description,omitempty" mapstructure:"description,omitempty"`
}

// OAuth2SecurityScheme defines a security scheme using OAuth 2.0.
type OAuth2SecurityScheme struct {
	// Description is an optional description for the security scheme.
	Description string `json:"description,omitempty" yaml:"description,omitempty" mapstructure:"description,omitempty"`

	// Flows is an object containing configuration information for the supported OAuth 2.0 flows.
	Flows OAuthFlows `json:"flows" yaml:"flows" mapstructure:"flows"`

	// Oauth2MetadataURL is an optional URL to the oauth2 authorization server metadata
	// [RFC8414](https://datatracker.ietf.org/doc/html/rfc8414). TLS is required.
	Oauth2MetadataURL string `json:"oauth2MetadataUrl,omitempty" yaml:"oauth2MetadataUrl,omitempty" mapstructure:"oauth2MetadataUrl,omitempty"`
}

// OAuthFlowName defines the set of possible OAuth 2.0 flow names.
type OAuthFlowName string

const (
	// AuthorizationCodeOAuthFlowName is the name for the Authorization Code flow.
	AuthorizationCodeOAuthFlowName OAuthFlowName = "authorizationCode"
	// ClientCredentialsOAuthFlowName is the name for the Client Credentials flow.
	ClientCredentialsOAuthFlowName OAuthFlowName = "clientCredentials"
	// ImplicitOAuthFlowName is the name for the Implicit flow.
	ImplicitOAuthFlowName OAuthFlowName = "implicit"
	// PasswordOAuthFlowName is the name for the Resource Owner Password flow.
	PasswordOAuthFlowName OAuthFlowName = "password"
	// DeviceCodeOAuthFlowName is the name for the Device Code flow.
	DeviceCodeOAuthFlowName OAuthFlowName = "deviceCode"
)

// MarshalJSON implements json.Marshaler.
func (s OAuth2SecurityScheme) MarshalJSON() ([]byte, error) {
	type wrapper struct {
		Description       string                `json:"description,omitempty"`
		Oauth2MetadataURL string                `json:"oauth2MetadataUrl,omitempty"`
		Flows             map[OAuthFlowName]any `json:"flows,omitempty"`
	}
	wrapped := wrapper{Description: s.Description, Oauth2MetadataURL: s.Oauth2MetadataURL}
	switch v := s.Flows.(type) {
	case AuthorizationCodeOAuthFlow:
		wrapped.Flows = map[OAuthFlowName]any{"authorizationCode": v}
	case ClientCredentialsOAuthFlow:
		wrapped.Flows = map[OAuthFlowName]any{"clientCredentials": v}
	case ImplicitOAuthFlow:
		wrapped.Flows = map[OAuthFlowName]any{"implicit": v}
	case PasswordOAuthFlow:
		wrapped.Flows = map[OAuthFlowName]any{"password": v}
	case DeviceCodeOAuthFlow:
		wrapped.Flows = map[OAuthFlowName]any{"deviceCode": v}
	default:
		return nil, fmt.Errorf("unknown OAuth flow type %T", v)
	}
	return json.Marshal(wrapped)
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *OAuth2SecurityScheme) UnmarshalJSON(b []byte) error {
	type wrapper struct {
		Description       string                            `json:"description,omitempty"`
		Oauth2MetadataURL string                            `json:"oauth2MetadataUrl,omitempty"`
		Flows             map[OAuthFlowName]json.RawMessage `json:"flows,omitempty"`
	}
	var scheme wrapper
	if err := json.Unmarshal(b, &scheme); err != nil {
		return err
	}

	if len(scheme.Flows) != 1 {
		return fmt.Errorf("expected exactly one OAuth flow, got %d", len(scheme.Flows))
	}

	for name, rawMessage := range scheme.Flows {
		switch name {
		case "authorizationCode":
			var flow AuthorizationCodeOAuthFlow
			if err := json.Unmarshal(rawMessage, &flow); err != nil {
				return err
			}
			s.Flows = flow
		case "clientCredentials":
			var flow ClientCredentialsOAuthFlow
			if err := json.Unmarshal(rawMessage, &flow); err != nil {
				return err
			}
			s.Flows = flow
		case "implicit":
			var flow ImplicitOAuthFlow
			if err := json.Unmarshal(rawMessage, &flow); err != nil {
				return err
			}
			s.Flows = flow
		case "password":
			var flow PasswordOAuthFlow
			if err := json.Unmarshal(rawMessage, &flow); err != nil {
				return err
			}
			s.Flows = flow
		case "deviceCode":
			var flow DeviceCodeOAuthFlow
			if err := json.Unmarshal(rawMessage, &flow); err != nil {
				return err
			}
			s.Flows = flow
		default:
			keys := make([]string, 0, len(scheme.Flows))
			for k := range scheme.Flows {
				keys = append(keys, string(k))
			}
			return fmt.Errorf("unknown OAuth flow type: %s, available: %v", name, keys)
		}
	}
	return nil
}

// OAuthFlows defines the configuration for the supported OAuth 2.0 flows.
type OAuthFlows interface {
	isOAuthFlow()
}

func (AuthorizationCodeOAuthFlow) isOAuthFlow() {}
func (ClientCredentialsOAuthFlow) isOAuthFlow() {}
func (ImplicitOAuthFlow) isOAuthFlow()          {}
func (PasswordOAuthFlow) isOAuthFlow()          {}
func (DeviceCodeOAuthFlow) isOAuthFlow()        {}

func init() {
	gob.Register(AuthorizationCodeOAuthFlow{})
	gob.Register(ClientCredentialsOAuthFlow{})
	gob.Register(ImplicitOAuthFlow{})
	gob.Register(PasswordOAuthFlow{})
	gob.Register(DeviceCodeOAuthFlow{})
}

// AuthorizationCodeOAuthFlow defines configuration details for the OAuth 2.0 Authorization Code flow.
type AuthorizationCodeOAuthFlow struct {
	// AuthorizationURL is the authorization URL to be used for this flow.
	// This MUST be a URL and use TLS.
	AuthorizationURL string `json:"authorizationUrl" yaml:"authorizationUrl" mapstructure:"authorizationUrl"`

	// RefreshURL is an optional URL to be used for obtaining refresh tokens.
	// This MUST be a URL and use TLS.
	RefreshURL string `json:"refreshUrl,omitempty" yaml:"refreshUrl,omitempty" mapstructure:"refreshUrl,omitempty"`

	// Scopes are the available scopes for the OAuth2 security scheme. A map between the scope
	// name and a short description for it.
	Scopes map[string]string `json:"scopes" yaml:"scopes" mapstructure:"scopes"`

	// TokenURL is the URL to be used for this flow. This MUST be a URL and use TLS.
	TokenURL string `json:"tokenUrl" yaml:"tokenUrl" mapstructure:"tokenUrl"`

	// PKCERequired is an optional boolean indicating whether PKCE is required for this flow.
	// PKCE should always be used for public clients and is recommended for all clients.
	PKCERequired bool `json:"pkceRequired,omitempty" yaml:"pkceRequired,omitempty" mapstructure:"pkceRequired,omitempty"`
}

// ClientCredentialsOAuthFlow defines configuration details for the OAuth 2.0 Client Credentials flow.
type ClientCredentialsOAuthFlow struct {
	// RefreshURL is an optional URL to be used for obtaining refresh tokens. This MUST be a URL.
	RefreshURL string `json:"refreshUrl,omitempty" yaml:"refreshUrl,omitempty" mapstructure:"refreshUrl,omitempty"`

	// Scopes are the available scopes for the OAuth2 security scheme. A map between the scope
	// name and a short description for it.
	Scopes map[string]string `json:"scopes" yaml:"scopes" mapstructure:"scopes"`

	// TokenURL is the token URL to be used for this flow. This MUST be a URL.
	TokenURL string `json:"tokenUrl" yaml:"tokenUrl" mapstructure:"tokenUrl"`
}

// ImplicitOAuthFlow defines configuration details for the OAuth 2.0 Implicit flow.
type ImplicitOAuthFlow struct {
	// AuthorizationURL is the authorization URL to be used for this flow. This MUST be a URL.
	AuthorizationURL string `json:"authorizationUrl" yaml:"authorizationUrl" mapstructure:"authorizationUrl"`

	// RefreshURL is an optional URL to be used for obtaining refresh tokens. This MUST be a URL.
	RefreshURL string `json:"refreshUrl,omitempty" yaml:"refreshUrl,omitempty" mapstructure:"refreshUrl,omitempty"`

	// Scopes are the available scopes for the OAuth2 security scheme. A map between the scope
	// name and a short description for it.
	Scopes map[string]string `json:"scopes" yaml:"scopes" mapstructure:"scopes"`
}

// PasswordOAuthFlow defines configuration details for the OAuth 2.0 Resource Owner Password flow.
type PasswordOAuthFlow struct {
	// RefreshURL is an optional URL to be used for obtaining refresh tokens. This MUST be a URL.
	RefreshURL string `json:"refreshUrl,omitempty" yaml:"refreshUrl,omitempty" mapstructure:"refreshUrl,omitempty"`

	// Scopes are the available scopes for the OAuth2 security scheme.
	// A map between the scope name and a short description for it.
	Scopes map[string]string `json:"scopes" yaml:"scopes" mapstructure:"scopes"`

	// TokenURL is the token URL to be used for this flow. This MUST be a URL.
	TokenURL string `json:"tokenUrl" yaml:"tokenUrl" mapstructure:"tokenUrl"`
}

// DeviceCodeOAuthFlow defines configuration details for the OAuth 2.0 Device Code flow.
type DeviceCodeOAuthFlow struct {
	// DeviceAuthorizationURL is the device authorization URL to be used for this flow. This MUST be a URL.
	DeviceAuthorizationURL string `json:"deviceAuthorizationUrl" yaml:"deviceAuthorizationUrl" mapstructure:"deviceAuthorizationUrl"`

	// RefreshURL is an optional URL to be used for obtaining refresh tokens. This MUST be a URL.
	RefreshURL string `json:"refreshUrl,omitempty" yaml:"refreshUrl,omitempty" mapstructure:"refreshUrl,omitempty"`

	// Scopes are the available scopes for the OAuth2 security scheme. A map between the scope
	// name and a short description for it.
	Scopes map[string]string `json:"scopes" yaml:"scopes" mapstructure:"scopes"`

	// TokenURL is the token URL to be used for this flow. This MUST be a URL.
	TokenURL string `json:"tokenUrl" yaml:"tokenUrl" mapstructure:"tokenUrl"`
}
