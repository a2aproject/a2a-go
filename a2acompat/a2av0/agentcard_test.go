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
	"testing"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/google/go-cmp/cmp"
)

var newAgentCard = &a2a.AgentCard{
	Name:             "GeoSpatial Route Planner Agent",
	Version:          "1.2.0",
	Description:      "Provides advanced route planning.",
	IconURL:          "https://georoute-agent.example.com/icon.png",
	DocumentationURL: "https://docs.examplegeoservices.com/georoute-agent/api",
	Provider:         &a2a.AgentProvider{Org: "Example Geo Services Inc.", URL: "https://www.examplegeoservices.com"},
	SupportedInterfaces: []*a2a.AgentInterface{
		{
			URL:             "https://georoute-agent.example.com/a2a/v1",
			ProtocolBinding: a2a.TransportProtocolJSONRPC,
			ProtocolVersion: Version,
		},
		{
			URL:             "https://georoute-agent.example.com/a2a/grpc",
			ProtocolBinding: a2a.TransportProtocolGRPC,
			ProtocolVersion: Version,
		},
		{
			URL:             "https://georoute-agent.example.com/a2a/json",
			ProtocolBinding: a2a.TransportProtocolHTTPJSON,
			ProtocolVersion: Version,
		},
	},
	Capabilities: a2a.AgentCapabilities{
		Streaming:         true,
		PushNotifications: true,
		ExtendedAgentCard: true,
	},
	SecuritySchemes: a2a.NamedSecuritySchemes{
		"google": a2a.OpenIDConnectSecurityScheme{
			OpenIDConnectURL: "https://accounts.google.com/.well-known/openid-configuration",
		},
	},
	SecurityRequirements: a2a.SecurityRequirementsOptions{
		a2a.SecurityRequirements{"google": {"openid", "profile", "email"}},
	},
	DefaultInputModes:  []string{"application/json", "text/plain"},
	DefaultOutputModes: []string{"application/json", "image/png"},
	Skills: []a2a.AgentSkill{
		{
			ID:          "route-optimizer-traffic",
			Name:        "Traffic-Aware Route Optimizer",
			Description: "Calculates the optimal driving route between two or more locations, taking into account real-time traffic conditions, road closures, and user preferences (e.g., avoid tolls, prefer highways).",
			Tags:        []string{"maps", "routing", "navigation", "directions", "traffic"},
			Examples: []string{
				"Plan a route from '1600 Amphitheatre Parkway, Mountain View, CA' to 'San Francisco International Airport' avoiding tolls.",
				"{\"origin\": {\"lat\": 37.422, \"lng\": -122.084}, \"destination\": {\"lat\": 37.7749, \"lng\": -122.4194}, \"preferences\": [\"avoid_ferries\"]}",
			},
			InputModes:  []string{"application/json", "text/plain"},
			OutputModes: []string{"application/json", "application/vnd.geo+json", "text/html"},
			SecurityRequirements: a2a.SecurityRequirementsOptions{
				a2a.SecurityRequirements{"example": {}},
				a2a.SecurityRequirements{"google": {"openid", "profile", "email"}},
			},
		},
		{
			ID:          "custom-map-generator",
			Name:        "Personalized Map Generator",
			Description: "Creates custom map images or interactive map views based on user-defined points of interest, routes, and style preferences. Can overlay data layers.",
			Tags:        []string{"maps", "customization", "visualization", "cartography"},
			Examples: []string{
				"Generate a map of my upcoming road trip with all planned stops highlighted.",
				"Show me a map visualizing all coffee shops within a 1-mile radius of my current location.",
			},
			InputModes:  []string{"application/json"},
			OutputModes: []string{"image/png", "image/jpeg", "application/json", "text/html"},
		},
	},
	Signatures: []a2a.AgentCardSignature{
		{
			Protected: "eyJhbGciOiJFUzI1NiIsInR5cCI6IkpPU0UiLCJraWQiOiJrZXktMSIsImprdSI6Imh0dHBzOi8vZXhhbXBsZS5jb20vYWdlbnQvandrcy5qc29uIn0",
			Signature: "QFdkNLNszlGj3z3u0YQGt_T9LixY3qtdQpZmsTdDHDe3fXV9y9-B3m2-XgCpzuhiLt8E0tV6HXoZKHv4GtHgKQ",
		},
	},
}

func TestAgentCard_NewToNew(t *testing.T) {
	bytes, err := json.MarshalIndent(newAgentCard, "", "  ")
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	parser := NewAgentCardParser()
	gotCard, err := parser(bytes)
	if err != nil {
		t.Fatalf("parser.Parse() error = %v", err)
	}
	if diff := cmp.Diff(newAgentCard, gotCard); diff != "" {
		t.Errorf("parser.Parse() wrong result  (+got,-want) diff = %s", diff)
	}
}

func TestAgentCard_CompatToNew(t *testing.T) {
	compatProducer := compatProducer{&staticCardProducer{card: newAgentCard}}
	compatCardJSON, err := compatProducer.CardJSON(context.Background())
	if err != nil {
		t.Fatalf("compatProducer.CardJSON() error = %v", err)
	}

	parser := NewAgentCardParser()
	gotCard, err := parser(compatCardJSON)
	if err != nil {
		t.Fatalf("parser.Parse() error = %v", err)
	}
	if diff := cmp.Diff(newAgentCard, gotCard); diff != "" {
		t.Errorf("parser.Parse() wrong result  (+got,-want) diff = %s", diff)
	}
}

func TestAgentCard_OldToNew(t *testing.T) {
	oldCardJSON := `
{
  "protocolVersion": "0.3",
  "name": "GeoSpatial Route Planner Agent",
  "description": "Provides advanced route planning.",
  "url": "https://georoute-agent.example.com/a2a/v1",
  "preferredTransport": "JSONRPC",
  "additionalInterfaces" : [
    {"url": "https://georoute-agent.example.com/a2a/v1", "transport": "JSONRPC"},
    {"url": "https://georoute-agent.example.com/a2a/grpc", "transport": "GRPC"},
    {"url": "https://georoute-agent.example.com/a2a/json", "transport": "HTTP+JSON"}
  ],
  "provider": {
    "organization": "Example Geo Services Inc.",
    "url": "https://www.examplegeoservices.com"
  },
  "iconUrl": "https://georoute-agent.example.com/icon.png",
  "version": "1.2.0",
  "documentationUrl": "https://docs.examplegeoservices.com/georoute-agent/api",
	"supportsAuthenticatedExtendedCard": true,
  "capabilities": {"streaming": true, "pushNotifications": true, "stateTransitionHistory": false},
	"securitySchemes": {
    "google": {
      "type": "openIdConnect",
      "openIdConnectUrl": "https://accounts.google.com/.well-known/openid-configuration"
    }
  },
  "security": [{ "google": ["openid", "profile", "email"] }],
  "defaultInputModes": ["application/json", "text/plain"],
  "defaultOutputModes": ["application/json", "image/png"],
  "skills": [
    {
      "id": "route-optimizer-traffic",
      "name": "Traffic-Aware Route Optimizer",
      "description": "Calculates the optimal driving route between two or more locations, taking into account real-time traffic conditions, road closures, and user preferences (e.g., avoid tolls, prefer highways).",
      "tags": ["maps", "routing", "navigation", "directions", "traffic"],
      "examples": [
        "Plan a route from '1600 Amphitheatre Parkway, Mountain View, CA' to 'San Francisco International Airport' avoiding tolls.",
        "{\"origin\": {\"lat\": 37.422, \"lng\": -122.084}, \"destination\": {\"lat\": 37.7749, \"lng\": -122.4194}, \"preferences\": [\"avoid_ferries\"]}"
      ],
      "inputModes": ["application/json", "text/plain"],
      "outputModes": [
        "application/json",
        "application/vnd.geo+json",
        "text/html"
      ],
			"security": [{"example": []}, {"google": ["openid", "profile", "email"]}]
    },
    {
      "id": "custom-map-generator",
      "name": "Personalized Map Generator",
      "description": "Creates custom map images or interactive map views based on user-defined points of interest, routes, and style preferences. Can overlay data layers.",
      "tags": ["maps", "customization", "visualization", "cartography"],
      "examples": [
        "Generate a map of my upcoming road trip with all planned stops highlighted.",
        "Show me a map visualizing all coffee shops within a 1-mile radius of my current location."
      ],
      "inputModes": ["application/json"],
      "outputModes": [
        "image/png",
        "image/jpeg",
        "application/json",
        "text/html"
      ]
    }
  ],
  "signatures": [
    {
      "protected": "eyJhbGciOiJFUzI1NiIsInR5cCI6IkpPU0UiLCJraWQiOiJrZXktMSIsImprdSI6Imh0dHBzOi8vZXhhbXBsZS5jb20vYWdlbnQvandrcy5qc29uIn0",
      "signature": "QFdkNLNszlGj3z3u0YQGt_T9LixY3qtdQpZmsTdDHDe3fXV9y9-B3m2-XgCpzuhiLt8E0tV6HXoZKHv4GtHgKQ"
    }
  ]
}
`
	parser := NewAgentCardParser()
	gotCard, err := parser([]byte(oldCardJSON))
	if err != nil {
		t.Fatalf("parser.Parse() error = %v", err)
	}
	if diff := cmp.Diff(newAgentCard, gotCard); diff != "" {
		t.Errorf("parser.Parse() wrong result  (+got,-want) diff = %s", diff)
	}
}

type staticCardProducer struct {
	card *a2a.AgentCard
}

func (s *staticCardProducer) Card(ctx context.Context) (*a2a.AgentCard, error) {
	return s.card, nil
}
