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
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func mustMarshal(t *testing.T, data any) string {
	t.Helper()
	bytes, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("Marshal() failed with: %v", err)
	}
	return string(bytes)
}

func mustUnmarshal(t *testing.T, data []byte, out any) {
	t.Helper()
	if err := json.Unmarshal(data, out); err != nil {
		t.Fatalf("Unmarshal() failed with: %v", err)
	}
}

func TestFilePartJSONCodec(t *testing.T) {
	testCases := []struct {
		json string
		part FilePart
	}{
		{
			part: FilePart{File: FileURI{URI: "uri"}},
			json: `{"kind":"file","file":{"uri":"uri"}}`,
		},
		{
			part: FilePart{File: FileBytes{Bytes: "abc"}},
			json: `{"kind":"file","file":{"bytes":"abc"}}`,
		},
		{
			part: FilePart{File: FileBytes{Bytes: "abc", FileMeta: FileMeta{Name: "foo"}}},
			json: `{"kind":"file","file":{"name":"foo","bytes":"abc"}}`,
		},
		{
			part: FilePart{File: FileBytes{Bytes: "abc", FileMeta: FileMeta{Name: "foo", MimeType: "mime"}}},
			json: `{"kind":"file","file":{"mimeType":"mime","name":"foo","bytes":"abc"}}`,
		},
		{
			part: FilePart{File: FileURI{URI: "uri", FileMeta: FileMeta{Name: "foo", MimeType: "mime"}}},
			json: `{"kind":"file","file":{"mimeType":"mime","name":"foo","uri":"uri"}}`,
		},
	}
	for _, tc := range testCases {
		if got := mustMarshal(t, tc.part); got != tc.json {
			t.Fatalf("Marshal() failed:\nwant %v\ngot: %s", tc.json, got)
		}

		got := FilePart{}
		mustUnmarshal(t, []byte(tc.json), &got)
		if !reflect.DeepEqual(got, tc.part) {
			t.Fatalf("Unmarshal() failed for %s:\nwant %v\ngot: %s", tc.json, tc.part, got)
		}
	}
}

func TestFilePartJSONDecodingFailure(t *testing.T) {
	malformed := []string{
		`{"kind":"file"}`,
		`{"kind":"file","file":{}}`,
		`{"kind":"file","file":{"name":"foo","mimeType":"mime","uri":"uri","bytes":"abc"}}`,
		`{"kind":"file","file":{"name":"foo","mimeType":"mime"}}`,
	}
	for _, v := range malformed {
		got := FilePart{}
		if err := json.Unmarshal([]byte(v), &got); err == nil {
			t.Fatalf("Unmarshal() expected to fail for %s, got: %v", v, got)
		}
	}
}

func TestContentPartsJSONCodec(t *testing.T) {
	parts := ContentParts{
		TextPart{Text: "hello, world"},
		FilePart{File: FileBytes{Bytes: "abc", FileMeta: FileMeta{Name: "foo", MimeType: "mime"}}},
		DataPart{Data: map[string]any{"foo": "bar"}},
		TextPart{Text: "42", Metadata: map[string]any{"foo": "bar"}},
	}

	jsons := []string{
		`{"kind":"text","text":"hello, world"}`,
		`{"kind":"file","file":{"mimeType":"mime","name":"foo","bytes":"abc"}}`,
		`{"kind":"data","data":{"foo":"bar"}}`,
		`{"kind":"text","text":"42","metadata":{"foo":"bar"}}`,
	}

	wantJSON := fmt.Sprintf("[%s]", strings.Join(jsons, ","))
	if got := mustMarshal(t, parts); got != wantJSON {
		t.Fatalf("Marshal() failed:\nwant %v\ngot: %s", wantJSON, got)
	}

	var got ContentParts
	mustUnmarshal(t, []byte(wantJSON), &got)
	if !reflect.DeepEqual(got, parts) {
		t.Fatalf("Unmarshal() failed:\nwant %v\ngot: %s", parts, got)
	}
}

func TestSecuritySchemeJSONCodec(t *testing.T) {
	schemes := NamedSecuritySchemes{
		"name1": APIKeySecurityScheme{Name: "abc", In: APIKeySecuritySchemeInCookie},
		"name2": OpenIDConnectSecurityScheme{OpenIDConnectURL: "url"},
		"name3": MutualTLSSecurityScheme{Description: "optional"},
		"name4": HTTPAuthSecurityScheme{Scheme: "Bearer", BearerFormat: "JWT"},
		"name5": OAuth2SecurityScheme{
			Flows: OAuthFlows{
				Password: &PasswordOAuthFlow{
					TokenURL: "url",
					Scopes:   map[string]string{"email": "read user emails"},
				},
			},
		},
	}

	entriesJSON := []string{
		`"name1":{"type":"apiKey","in":"cookie","name":"abc"}`,
		`"name2":{"type":"openIdConnect","openIdConnectUrl":"url"}`,
		`"name3":{"type":"mutualTLS","description":"optional"}`,
		`"name4":{"type":"http","bearerFormat":"JWT","scheme":"Bearer"}`,
		`"name5":{"type":"oauth2","flows":{"password":{"scopes":{"email":"read user emails"},"tokenUrl":"url"}}}`,
	}
	wantJSON := fmt.Sprintf("{%s}", strings.Join(entriesJSON, ","))

	var decodedJSON NamedSecuritySchemes
	mustUnmarshal(t, []byte(wantJSON), &decodedJSON)
	if !reflect.DeepEqual(decodedJSON, schemes) {
		t.Fatalf("Unmarshal() failed:\nwant %v\ngot: %s", schemes, decodedJSON)
	}

	encodedSchemes := mustMarshal(t, schemes)
	var decodedBack NamedSecuritySchemes
	mustUnmarshal(t, []byte(encodedSchemes), &decodedBack)
	if !reflect.DeepEqual(decodedJSON, decodedBack) {
		t.Fatalf("Decoding back failed:\nwant %v\ngot: %s", decodedJSON, decodedBack)
	}
}

func TestEventMarshalEmptyContentParts(t *testing.T) {
	event := &TaskArtifactUpdateEvent{Artifact: &Artifact{ID: "art-123"}}
	jsonBytes, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("Marshal() failed: %v", err)
	}
	var decoded TaskArtifactUpdateEvent
	if err := json.Unmarshal(jsonBytes, &decoded); err != nil {
		t.Fatalf("Unmarshal() failed: %v", err)
	}
	if !strings.Contains(string(jsonBytes), `"parts":[]`) {
		t.Fatalf("json.Marshal() = %q, want parts to be non-nil", string(jsonBytes))
	}
}

func TestAgentCardParsing(t *testing.T) {
	cardJSON := `
{
  "protocolVersion": "0.2.9",
  "name": "GeoSpatial Route Planner Agent",
  "description": "Provides advanced route planning, traffic analysis, and custom map generation services. This agent can calculate optimal routes, estimate travel times considering real-time traffic, and create personalized maps with points of interest.",
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
  "capabilities": {
    "streaming": true,
    "pushNotifications": true,
    "stateTransitionHistory": false
  },
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
      ]
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
  "supportsAuthenticatedExtendedCard": true,
  "signatures": [
    {
      "protected": "eyJhbGciOiJFUzI1NiIsInR5cCI6IkpPU0UiLCJraWQiOiJrZXktMSIsImprdSI6Imh0dHBzOi8vZXhhbXBsZS5jb20vYWdlbnQvandrcy5qc29uIn0",
      "signature": "QFdkNLNszlGj3z3u0YQGt_T9LixY3qtdQpZmsTdDHDe3fXV9y9-B3m2-XgCpzuhiLt8E0tV6HXoZKHv4GtHgKQ"
    }
  ]
}
`
	want := AgentCard{
		ProtocolVersion:    "0.2.9",
		Name:               "GeoSpatial Route Planner Agent",
		Description:        "Provides advanced route planning, traffic analysis, and custom map generation services. This agent can calculate optimal routes, estimate travel times considering real-time traffic, and create personalized maps with points of interest.",
		URL:                "https://georoute-agent.example.com/a2a/v1",
		PreferredTransport: TransportProtocolJSONRPC,
		AdditionalInterfaces: []AgentInterface{
			{URL: "https://georoute-agent.example.com/a2a/v1", Transport: TransportProtocolJSONRPC},
			{URL: "https://georoute-agent.example.com/a2a/grpc", Transport: TransportProtocolGRPC},
			{URL: "https://georoute-agent.example.com/a2a/json", Transport: TransportProtocolHTTPJSON},
		},
		Provider: &AgentProvider{
			Org: "Example Geo Services Inc.",
			URL: "https://www.examplegeoservices.com",
		},
		IconURL:          "https://georoute-agent.example.com/icon.png",
		Version:          "1.2.0",
		DocumentationURL: "https://docs.examplegeoservices.com/georoute-agent/api",
		Capabilities:     AgentCapabilities{Streaming: true, PushNotifications: true, StateTransitionHistory: false},
		SecuritySchemes: NamedSecuritySchemes{
			SecuritySchemeName("google"): OpenIDConnectSecurityScheme{
				OpenIDConnectURL: "https://accounts.google.com/.well-known/openid-configuration",
			},
		},
		Security:           []SecurityRequirements{{SecuritySchemeName("google"): []string{"openid", "profile", "email"}}},
		DefaultInputModes:  []string{"application/json", "text/plain"},
		DefaultOutputModes: []string{"application/json", "image/png"},
		Skills: []AgentSkill{
			{
				ID:          "route-optimizer-traffic",
				Name:        "Traffic-Aware Route Optimizer",
				Description: "Calculates the optimal driving route between two or more locations, taking into account real-time traffic conditions, road closures, and user preferences (e.g., avoid tolls, prefer highways).",
				Tags:        []string{"maps", "routing", "navigation", "directions", "traffic"},
				Examples: []string{
					"Plan a route from '1600 Amphitheatre Parkway, Mountain View, CA' to 'San Francisco International Airport' avoiding tolls.",
					`{"origin": {"lat": 37.422, "lng": -122.084}, "destination": {"lat": 37.7749, "lng": -122.4194}, "preferences": ["avoid_ferries"]}`,
				},
				InputModes:  []string{"application/json", "text/plain"},
				OutputModes: []string{"application/json", "application/vnd.geo+json", "text/html"},
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
		SupportsAuthenticatedExtendedCard: true,
		Signatures: []AgentCardSignature{
			{
				Protected: "eyJhbGciOiJFUzI1NiIsInR5cCI6IkpPU0UiLCJraWQiOiJrZXktMSIsImprdSI6Imh0dHBzOi8vZXhhbXBsZS5jb20vYWdlbnQvandrcy5qc29uIn0",
				Signature: "QFdkNLNszlGj3z3u0YQGt_T9LixY3qtdQpZmsTdDHDe3fXV9y9-B3m2-XgCpzuhiLt8E0tV6HXoZKHv4GtHgKQ",
			},
		},
	}

	var got AgentCard
	if err := json.Unmarshal([]byte(cardJSON), &got); err != nil {
		t.Fatalf("AgentCard parsing failed: %v", err)
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("AgentCard codec:\ngot %v\nwant %v\ndiff(-want +got):\n%v", got, want, diff)
	}
}
