// Copyright 2026 The A2A Authors
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

package a2av0

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2aclient/agentcard"
	"github.com/a2aproject/a2a-go/a2asrv"
)

// NewAgentCardParser returns a parser that can parse agent cards in v0.3 format into v1.0 format.
func NewAgentCardParser() agentcard.Parser {
	return func(b []byte) (*a2a.AgentCard, error) {
		var compatCard agentCard
		if err := json.Unmarshal(b, &compatCard); err != nil {
			return nil, err
		}

		if compatCard.SupportsAuthenticatedExtendedCard {
			compatCard.Capabilities.ExtendedAgentCard = true
		}

		return &a2a.AgentCard{
			Capabilities:       compatCard.Capabilities,
			DefaultInputModes:  compatCard.DefaultInputModes,
			DefaultOutputModes: compatCard.DefaultOutputModes,
			Description:        compatCard.Description,
			DocumentationURL:   compatCard.DocumentationURL,
			IconURL:            compatCard.IconURL,
			Name:               compatCard.Name,
			Provider:           compatCard.Provider,
			Version:            compatCard.Version,
			Signatures:         compatCard.Signatures,

			SecurityRequirements: mapFromCompatSecurity(compatCard),
			SupportedInterfaces:  mapFromCompatInterfaces(compatCard),
			SecuritySchemes:      mapFromCompatSecuritySchemes(compatCard.SecuritySchemes),
			Skills:               mapFromCompatSkills(compatCard.Skills),
		}, nil
	}
}

// NewStaticAgentCardProducer returns a new AgentCardProducer which will modify the agent card
// to be compatible with the v0.3 protocol clients. This is handled by implementing serialization
// into a union of v1.0 and v0.3 formats when converting to JSON.
func NewStaticAgentCardProducer(card *a2a.AgentCard) a2asrv.AgentCardProducer {
	return &compatProducer{AgentCardProducer: &staticProducer{card: card}}
}

// NewCompatCardProducer returns a new AgentCardProducer which will modify the agent card
// to be compatible with the v0.3 protocol clients. This is handled by implementing serialization
// into a union of v1.0 and v0.3 formats when converting to JSON.
func NewAgentCardProducer(producer a2asrv.AgentCardProducer) a2asrv.AgentCardProducer {
	return &compatProducer{AgentCardProducer: producer}
}

type staticProducer struct {
	card *a2a.AgentCard
}

func (p *staticProducer) Card(ctx context.Context) (*a2a.AgentCard, error) {
	return p.card, nil
}

type compatProducer struct {
	a2asrv.AgentCardProducer
}

func (p *compatProducer) CardJSON(ctx context.Context) ([]byte, error) {
	card, err := p.Card(ctx)
	if err != nil {
		return nil, err
	}
	compatCard, err := toCompatCard(card)
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(compatCard, "", "  ")
}

func toCompatCard(card *a2a.AgentCard) (*agentCard, error) {
	preferredInterface, additionalInterfaces, err := mapToCompatInterfaces(card)
	if err != nil {
		return nil, err
	}
	return &agentCard{
		DefaultInputModes:    card.DefaultInputModes,
		DefaultOutputModes:   card.DefaultOutputModes,
		Description:          card.Description,
		DocumentationURL:     card.DocumentationURL,
		IconURL:              card.IconURL,
		Name:                 card.Name,
		Provider:             card.Provider,
		Signatures:           card.Signatures,
		Version:              card.Version,
		SupportedInterfaces:  card.SupportedInterfaces,
		Capabilities:         card.Capabilities,
		SecurityRequirements: card.SecurityRequirements,

		ProtocolVersion:                   Version,
		URL:                               preferredInterface.URL,
		PreferredTransport:                preferredInterface.ProtocolBinding,
		AdditionalInterfaces:              additionalInterfaces,
		Skills:                            mapToCompatSkills(card.Skills),
		Security:                          mapToCompatSecurity(card.SecurityRequirements),
		SecuritySchemes:                   mapToCompatSecuritySchemes(card.SecuritySchemes),
		SupportsAuthenticatedExtendedCard: card.Capabilities.ExtendedAgentCard,
	}, nil
}

func mapFromCompatSecuritySchemes(schemes namedSecuritySchemes) a2a.NamedSecuritySchemes {
	result := a2a.NamedSecuritySchemes{}
	for name, scheme := range schemes {
		switch v := scheme.(type) {
		case a2a.APIKeySecurityScheme:
			result[name] = v
		case a2a.HTTPAuthSecurityScheme:
			result[name] = v
		case a2a.OpenIDConnectSecurityScheme:
			result[name] = v
		case a2a.MutualTLSSecurityScheme:
			result[name] = v
		case a2a.OAuth2SecurityScheme:
			result[name] = v
		case apiKeySecurityScheme:
			if v.ApiKey != nil {
				result[name] = *v.ApiKey
			} else {
				result[name] = a2a.APIKeySecurityScheme{
					Description: v.Description,
					Location:    v.In,
					Name:        v.Name,
				}
			}
		case httpAuthSecurityScheme:
			if v.HTTPAuth != nil {
				result[name] = *v.HTTPAuth
			} else {
				result[name] = a2a.HTTPAuthSecurityScheme{
					Description:  v.Description,
					BearerFormat: v.BearerFormat,
					Scheme:       v.Scheme,
				}
			}
		case openIDConnectSecurityScheme:
			if v.OpenID != nil {
				result[name] = *v.OpenID
			} else {
				result[name] = a2a.OpenIDConnectSecurityScheme{
					Description:      v.Description,
					OpenIDConnectURL: v.OpenIDConnectURL,
				}
			}
		case mutualTLSSecurityScheme:
			if v.MutualTLS != nil {
				result[name] = *v.MutualTLS
			} else {
				result[name] = a2a.MutualTLSSecurityScheme{Description: v.Description}
			}
		case oauth2SecurityScheme:
			if v.OAuth2 != nil {
				result[name] = *v.OAuth2
			} else {
				result[name] = a2a.OAuth2SecurityScheme{Description: v.Description}
			}
		}
	}
	return result
}

func mapToCompatSecuritySchemes(schemes a2a.NamedSecuritySchemes) namedSecuritySchemes {
	result := make(namedSecuritySchemes)
	for k, v := range schemes {
		switch v := v.(type) {
		case a2a.APIKeySecurityScheme:
			result[k] = apiKeySecurityScheme{
				Type:        "apiKey",
				In:          v.Location,
				Name:        v.Name,
				Description: v.Description,
				ApiKey:      &v,
			}
		case a2a.HTTPAuthSecurityScheme:
			result[k] = httpAuthSecurityScheme{
				Type:         "http",
				Scheme:       v.Scheme,
				Description:  v.Description,
				BearerFormat: v.BearerFormat,
				HTTPAuth:     &v,
			}
		case a2a.OpenIDConnectSecurityScheme:
			result[k] = openIDConnectSecurityScheme{
				Type:             "openIdConnect",
				OpenIDConnectURL: v.OpenIDConnectURL,
				Description:      v.Description,
				OpenID:           &v,
			}
		case a2a.MutualTLSSecurityScheme:
			result[k] = mutualTLSSecurityScheme{
				Type:        "mutualTLS",
				Description: v.Description,
				MutualTLS:   &v,
			}
		case a2a.OAuth2SecurityScheme:
			result[k] = oauth2SecurityScheme{
				Type:              "oauth2",
				Description:       v.Description,
				Flows:             v.Flows,
				Oauth2MetadataURL: v.Oauth2MetadataURL,
				OAuth2:            &v,
			}
		}
	}
	return result
}

func mapToCompatSecurity(req a2a.SecurityRequirementsOptions) []securityRequirements {
	out := make([]securityRequirements, len(req))
	for i, r := range req {
		out[i] = securityRequirements(r)
	}
	return out
}

func mapFromCompatSecurity(card agentCard) a2a.SecurityRequirementsOptions {
	if len(card.SecurityRequirements) > 0 {
		return card.SecurityRequirements
	}
	var out a2a.SecurityRequirementsOptions
	for _, r := range card.Security {
		out = append(out, a2a.SecurityRequirements(r))
	}
	return out
}

func mapFromCompatInterfaces(card agentCard) []*a2a.AgentInterface {
	if len(card.SupportedInterfaces) > 0 {
		return card.SupportedInterfaces
	}
	version := Version
	if len(card.ProtocolVersion) > 0 {
		version = card.ProtocolVersion
	}
	out := make([]*a2a.AgentInterface, 0, 1+len(card.AdditionalInterfaces))
	if len(card.URL) > 0 && card.PreferredTransport != "" {
		out = append(out, &a2a.AgentInterface{URL: card.URL, ProtocolBinding: card.PreferredTransport, ProtocolVersion: version})
	}
	for _, r := range card.AdditionalInterfaces {
		if r.URL == card.URL {
			continue
		}
		out = append(out, &a2a.AgentInterface{URL: r.URL, ProtocolBinding: r.Transport, ProtocolVersion: version})
	}
	return out
}

func mapToCompatInterfaces(card *a2a.AgentCard) (*a2a.AgentInterface, []agentInterface, error) {
	agentInterfaceIdx := slices.IndexFunc(card.SupportedInterfaces, func(i *a2a.AgentInterface) bool {
		return i.ProtocolVersion == Version
	})
	if agentInterfaceIdx == -1 {
		return nil, nil, fmt.Errorf("at least 1 interface supporting %s must be listed", Version)
	}
	var additionalInterfaces []agentInterface
	for i, iface := range card.SupportedInterfaces {
		if i == agentInterfaceIdx || iface.ProtocolVersion != Version {
			continue
		}
		additionalInterfaces = append(additionalInterfaces, agentInterface{URL: iface.URL, Transport: iface.ProtocolBinding})
	}
	return card.SupportedInterfaces[agentInterfaceIdx], additionalInterfaces, nil
}

func mapFromCompatSkills(skills []agentSkill) []a2a.AgentSkill {
	result := make([]a2a.AgentSkill, len(skills))
	for i, s := range skills {
		skill := s.AgentSkill
		if len(skill.SecurityRequirements) == 0 {
			for _, r := range s.Security {
				skill.SecurityRequirements = append(skill.SecurityRequirements, a2a.SecurityRequirements(r))
			}
		}
		result[i] = skill
	}
	return result
}

func mapToCompatSkills(skills []a2a.AgentSkill) []agentSkill {
	result := make([]agentSkill, len(skills))
	for i, s := range skills {
		result[i] = agentSkill{
			AgentSkill: s,
			Security:   mapToCompatSecurity(s.SecurityRequirements),
		}
	}
	return result
}

type agentCard struct {
	// The same as v1.0
	DefaultInputModes  []string                 `json:"defaultInputModes"`
	DefaultOutputModes []string                 `json:"defaultOutputModes"`
	Description        string                   `json:"description"`
	DocumentationURL   string                   `json:"documentationUrl,omitempty"`
	IconURL            string                   `json:"iconUrl,omitempty"`
	Name               string                   `json:"name"`
	Provider           *a2a.AgentProvider       `json:"provider,omitempty"`
	Signatures         []a2a.AgentCardSignature `json:"signatures,omitempty"`
	Version            string                   `json:"version"`

	// Nested changes
	Skills          []agentSkill         `json:"skills"`
	SecuritySchemes namedSecuritySchemes `json:"securitySchemes,omitempty"`

	// Moved from root into capabilities
	Capabilities                      a2a.AgentCapabilities `json:"capabilities"`
	SupportsAuthenticatedExtendedCard bool                  `json:"supportsAuthenticatedExtendedCard,omitempty"`

	// Added in v1.0
	SupportedInterfaces  []*a2a.AgentInterface           `json:"supportedInterfaces"`
	SecurityRequirements a2a.SecurityRequirementsOptions `json:"securityRequirements,omitempty"`

	// Removed in v1.0
	AdditionalInterfaces []agentInterface       `json:"additionalInterfaces,omitempty"`
	URL                  string                 `json:"url"`
	ProtocolVersion      a2a.ProtocolVersion    `json:"protocolVersion"`
	PreferredTransport   a2a.TransportProtocol  `json:"preferredTransport,omitempty"`
	Security             []securityRequirements `json:"security,omitempty"`
}

type securityRequirements map[a2a.SecuritySchemeName]a2a.SecuritySchemeScopes

type agentInterface struct {
	Transport a2a.TransportProtocol `json:"transport"`
	URL       string                `json:"url"`
}

type agentSkill struct {
	a2a.AgentSkill
	Security []securityRequirements `json:"security,omitempty"`
}

type namedSecuritySchemes map[a2a.SecuritySchemeName]any

func (s *namedSecuritySchemes) UnmarshalJSON(b []byte) error {
	var schemes map[a2a.SecuritySchemeName]json.RawMessage
	if err := json.Unmarshal(b, &schemes); err != nil {
		return err
	}

	result := make(map[a2a.SecuritySchemeName]any, len(schemes))

	for name, rawMessage := range schemes {
		type schemeType struct {
			Type          string                           `json:"type"`
			ApiKey        *a2a.APIKeySecurityScheme        `json:"apiKey,omitempty"`
			HTTP          *a2a.HTTPAuthSecurityScheme      `json:"http,omitempty"`
			MutualTLS     *a2a.MutualTLSSecurityScheme     `json:"mutualTLS,omitempty"`
			OAuth2        *a2a.OAuth2SecurityScheme        `json:"oauth2,omitempty"`
			OpenIDConnect *a2a.OpenIDConnectSecurityScheme `json:"openIdConnect,omitempty"`
		}
		var st schemeType
		if err := json.Unmarshal(rawMessage, &st); err != nil {
			return err
		}

		// Compat or old
		if st.Type != "" {
			switch st.Type {
			case "apiKey":
				var scheme apiKeySecurityScheme
				if err := json.Unmarshal(rawMessage, &scheme); err != nil {
					return err
				}
				result[name] = scheme
			case "http":
				var scheme httpAuthSecurityScheme
				if err := json.Unmarshal(rawMessage, &scheme); err != nil {
					return err
				}
				result[name] = scheme
			case "mutualTLS":
				var scheme mutualTLSSecurityScheme
				if err := json.Unmarshal(rawMessage, &scheme); err != nil {
					return err
				}
				result[name] = scheme
			case "oauth2":
				var scheme oauth2SecurityScheme
				if err := json.Unmarshal(rawMessage, &scheme); err != nil {
					return err
				}
				result[name] = scheme
			case "openIdConnect":
				var scheme openIDConnectSecurityScheme
				if err := json.Unmarshal(rawMessage, &scheme); err != nil {
					return err
				}
				result[name] = scheme
			default:
				return fmt.Errorf("unknown security scheme type %q", st.Type)
			}
			continue
		}

		// New format
		if st.ApiKey != nil {
			result[name] = *st.ApiKey
		} else if st.HTTP != nil {
			result[name] = *st.HTTP
		} else if st.MutualTLS != nil {
			result[name] = *st.MutualTLS
		} else if st.OAuth2 != nil {
			result[name] = *st.OAuth2
		} else if st.OpenIDConnect != nil {
			result[name] = *st.OpenIDConnect
		} else {
			return fmt.Errorf("unknown security scheme: %q, parse result: %+v", string(rawMessage), st)
		}
	}
	*s = namedSecuritySchemes(result)
	return nil
}

type apiKeySecurityScheme struct {
	Type        string                           `json:"type"`
	Description string                           `json:"description,omitempty"`
	In          a2a.APIKeySecuritySchemeLocation `json:"in"`
	Name        string                           `json:"name"`
	ApiKey      *a2a.APIKeySecurityScheme        `json:"apiKey"`
}

type httpAuthSecurityScheme struct {
	Type         string                      `json:"type"`
	BearerFormat string                      `json:"bearerFormat,omitempty"`
	Description  string                      `json:"description,omitempty"`
	Scheme       string                      `json:"scheme"`
	HTTPAuth     *a2a.HTTPAuthSecurityScheme `json:"http"`
}

type mutualTLSSecurityScheme struct {
	Type        string                       `json:"type"`
	Description string                       `json:"description,omitempty"`
	MutualTLS   *a2a.MutualTLSSecurityScheme `json:"mutualTLS"`
}

type openIDConnectSecurityScheme struct {
	Type             string                           `json:"type"`
	Description      string                           `json:"description,omitempty"`
	OpenIDConnectURL string                           `json:"openIdConnectUrl"`
	OpenID           *a2a.OpenIDConnectSecurityScheme `json:"openIdConnect"`
}

type oauth2SecurityScheme struct {
	Type              string                    `json:"type"`
	Description       string                    `json:"description,omitempty"`
	Flows             a2a.OAuthFlows            `json:"flows"`
	Oauth2MetadataURL string                    `json:"oauth2MetadataUrl,omitempty"`
	OAuth2            *a2a.OAuth2SecurityScheme `json:"oauth2"`
}

func (s *oauth2SecurityScheme) MarshalJSON() ([]byte, error) {
	type wrapper struct {
		Description       string                    `json:"description,omitempty"`
		Oauth2MetadataURL string                    `json:"oauth2MetadataUrl,omitempty"`
		Flows             map[a2a.OAuthFlowName]any `json:"flows,omitempty"`
	}
	wrapped := wrapper{Description: s.Description, Oauth2MetadataURL: s.Oauth2MetadataURL}
	switch v := s.Flows.(type) {
	case a2a.AuthorizationCodeOAuthFlow:
		wrapped.Flows = map[a2a.OAuthFlowName]any{a2a.AuthorizationCodeOAuthFlowName: v}
	case a2a.ClientCredentialsOAuthFlow:
		wrapped.Flows = map[a2a.OAuthFlowName]any{a2a.ClientCredentialsOAuthFlowName: v}
	case a2a.ImplicitOAuthFlow:
		wrapped.Flows = map[a2a.OAuthFlowName]any{a2a.ImplicitOAuthFlowName: v}
	case a2a.PasswordOAuthFlow:
		wrapped.Flows = map[a2a.OAuthFlowName]any{a2a.PasswordOAuthFlowName: v}
	case a2a.DeviceCodeOAuthFlow:
		wrapped.Flows = map[a2a.OAuthFlowName]any{a2a.DeviceCodeOAuthFlowName: v}
	default:
		return nil, fmt.Errorf("unknown OAuth flow type %T", v)
	}
	return json.Marshal(wrapped)
}

func (s *oauth2SecurityScheme) UnmarshalJSON(b []byte) error {
	type wrapper struct {
		Description       string                                `json:"description,omitempty"`
		Oauth2MetadataURL string                                `json:"oauth2MetadataUrl,omitempty"`
		Flows             map[a2a.OAuthFlowName]json.RawMessage `json:"flows,omitempty"`
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
		case a2a.AuthorizationCodeOAuthFlowName:
			var flow a2a.AuthorizationCodeOAuthFlow
			if err := json.Unmarshal(rawMessage, &flow); err != nil {
				return err
			}
			s.Flows = flow
		case a2a.ClientCredentialsOAuthFlowName:
			var flow a2a.ClientCredentialsOAuthFlow
			if err := json.Unmarshal(rawMessage, &flow); err != nil {
				return err
			}
			s.Flows = flow
		case a2a.ImplicitOAuthFlowName:
			var flow a2a.ImplicitOAuthFlow
			if err := json.Unmarshal(rawMessage, &flow); err != nil {
				return err
			}
			s.Flows = flow
		case a2a.PasswordOAuthFlowName:
			var flow a2a.PasswordOAuthFlow
			if err := json.Unmarshal(rawMessage, &flow); err != nil {
				return err
			}
			s.Flows = flow
		case a2a.DeviceCodeOAuthFlowName:
			var flow a2a.DeviceCodeOAuthFlow
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
