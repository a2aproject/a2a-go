// Package server contains the A2A server implementation and related types.
package server

// SecurityScheme represents an authentication scheme supported by the agent.
// Based on A2A 0.2.2 specification.
type SecurityScheme struct {
	// Type is the type of security scheme.
	Type SecuritySchemeType `json:"type"`
	// Description is an optional description of the scheme.
	Description *string `json:"description,omitempty"`
	// Name is the name of the header/query parameter (for apiKey).
	Name *string `json:"name,omitempty"`
	// In specifies where to include the key (for apiKey).
	In *SecuritySchemeIn `json:"in,omitempty"`
	// Scheme is the HTTP authentication scheme (for http type).
	Scheme *string `json:"scheme,omitempty"`
	// BearerFormat is the hint for the bearer token format.
	BearerFormat *string `json:"bearerFormat,omitempty"`
	// Flows contains OAuth2 flow definitions.
	Flows *OAuthFlows `json:"flows,omitempty"`
	// OpenIDConnectURL is the OpenID Connect URL.
	OpenIDConnectURL *string `json:"openIdConnectUrl,omitempty"`
}

// SecuritySchemeType represents the type of security scheme.
type SecuritySchemeType string

const (
	// SecuritySchemeTypeAPIKey represents API key authentication.
	SecuritySchemeTypeAPIKey SecuritySchemeType = "apiKey"
	// SecuritySchemeTypeHTTP represents HTTP authentication.
	SecuritySchemeTypeHTTP SecuritySchemeType = "http"
	// SecuritySchemeTypeOAuth2 represents OAuth2 authentication.
	SecuritySchemeTypeOAuth2 SecuritySchemeType = "oauth2"
	// SecuritySchemeTypeOpenIDConnect represents OpenID Connect authentication.
	SecuritySchemeTypeOpenIDConnect SecuritySchemeType = "openIdConnect"
)

// SecuritySchemeIn represents where to include the security credentials.
type SecuritySchemeIn string

const (
	// SecuritySchemeInQuery indicates the credential should be in query parameters.
	SecuritySchemeInQuery SecuritySchemeIn = "query"
	// SecuritySchemeInHeader indicates the credential should be in HTTP headers.
	SecuritySchemeInHeader SecuritySchemeIn = "header"
	// SecuritySchemeInCookie indicates the credential should be in cookies.
	SecuritySchemeInCookie SecuritySchemeIn = "cookie"
)

// OAuthFlows represents OAuth2 flow configurations.
type OAuthFlows struct {
	// AuthorizationCode contains authorization code flow configuration.
	AuthorizationCode *OAuthFlow `json:"authorizationCode,omitempty"`
	// Implicit contains implicit flow configuration.
	Implicit *OAuthFlow `json:"implicit,omitempty"`
	// Password contains password flow configuration.
	Password *OAuthFlow `json:"password,omitempty"`
	// ClientCredentials contains client credentials flow configuration.
	ClientCredentials *OAuthFlow `json:"clientCredentials,omitempty"`
}

// OAuthFlow represents a single OAuth2 flow configuration.
type OAuthFlow struct {
	// AuthorizationURL is the authorization URL (for authorizationCode and implicit).
	AuthorizationURL *string `json:"authorizationUrl,omitempty"`
	// TokenURL is the token URL.
	TokenURL string `json:"tokenUrl"`
	// RefreshURL is the refresh URL.
	RefreshURL *string `json:"refreshUrl,omitempty"`
	// Scopes are the available scopes.
	Scopes map[string]string `json:"scopes,omitempty"`
}

// AgentExtension represents an agent extension.
type AgentExtension struct {
	// URI is the extension URI.
	URI string `json:"uri"`
	// Required indicates if the extension is required.
	Required *bool `json:"required,omitempty"`
	// Description is an optional description.
	Description *string `json:"description,omitempty"`
	// Params contains extension-specific parameters.
	Params map[string]interface{} `json:"params,omitempty"`
}

// AgentCapabilities defines the capabilities supported by an agent.
type AgentCapabilities struct {
	// Streaming is a flag indicating if the agent supports streaming responses.
	Streaming *bool `json:"streaming,omitempty"`
	// PushNotifications is a flag indicating if the agent can push notifications.
	PushNotifications *bool `json:"pushNotifications,omitempty"`
	// StateTransitionHistory is a flag indicating if the agent can provide task history.
	StateTransitionHistory *bool `json:"stateTransitionHistory,omitempty"`
	// Extensions are the supported agent extensions.
	Extensions []AgentExtension `json:"extensions,omitempty"`
}

// AgentSkill describes a specific capability or function of the agent.
type AgentSkill struct {
	// ID is the unique identifier for the skill.
	ID string `json:"id"`
	// Name is the human-readable name of the skill.
	Name string `json:"name"`
	// Description is an optional detailed description of the skill.
	Description *string `json:"description,omitempty"`
	// Tags are optional tags for categorization.
	Tags []string `json:"tags,omitempty"`
	// Examples are optional usage examples.
	Examples []string `json:"examples,omitempty"`
	// InputModes are the supported input data modes/types.
	InputModes []string `json:"inputModes,omitempty"`
	// OutputModes are the supported output data modes/types.
	OutputModes []string `json:"outputModes,omitempty"`
}

// AgentProvider contains information about the agent's provider or developer.
type AgentProvider struct {
	// Organization is the name of the provider.
	Organization string `json:"organization"`
	// URL is an optional URL for the provider.
	URL *string `json:"url,omitempty"`
}

// AgentAuthentication defines the authentication mechanism required by the agent.
type AgentAuthentication struct {
	// Type is the type of authentication (e.g., "none", "apiKey", "oauth").
	Type string `json:"type"`
	// Required is a flag indicating if authentication is mandatory.
	Required bool `json:"required"`
	// Config is an optional configuration details for the auth type.
	Config interface{} `json:"config,omitempty"`
}

// AgentCard is the metadata structure describing an A2A agent.
// Updated for A2A 0.2.2 specification compliance.
type AgentCard struct {
	// Name is the name of the agent.
	Name string `json:"name"`
	// Description is the description of the agent (required in 0.2.2).
	Description string `json:"description"`
	// URL is the endpoint URL where the agent is hosted.
	URL string `json:"url"`
	// Provider is an optional provider information.
	Provider *AgentProvider `json:"provider,omitempty"`
	// IconURL is an optional URL to the agent's icon.
	IconURL *string `json:"iconUrl,omitempty"`
	// Version is the agent version string.
	Version string `json:"version"`
	// DocumentationURL is an optional link to documentation.
	DocumentationURL *string `json:"documentationUrl,omitempty"`
	// Capabilities are the declared capabilities of the agent.
	Capabilities AgentCapabilities `json:"capabilities"`
	// SecuritySchemes define the security schemes supported by the agent.
	SecuritySchemes map[string]SecurityScheme `json:"securitySchemes,omitempty"`
	// Security defines the security requirements for the agent.
	Security []map[string][]string `json:"security,omitempty"`
	// DefaultInputModes are the default input modes (required in 0.2.2).
	DefaultInputModes []string `json:"defaultInputModes"`
	// DefaultOutputModes are the default output modes (required in 0.2.2).
	DefaultOutputModes []string `json:"defaultOutputModes"`
	// Skills are the list of specific skills (required in 0.2.2).
	Skills []AgentSkill `json:"skills"`
	// SupportsAuthenticatedExtendedCard indicates if the agent supports authenticated extended card.
	SupportsAuthenticatedExtendedCard *bool `json:"supportsAuthenticatedExtendedCard,omitempty"`
}
