package a2a

import (
	"encoding/json"
	"testing"
)

func TestParseJavaJacksonAgentCard(t *testing.T) {
	// Exact raw JSON from #347 — Java server using Jackson serialization
	const javaJacksonJSON = `{
  "name": "A2AT-Test-Agent",
  "description": "Helps with event",
  "provider": null,
  "version": "1.0.0",
  "documentationUrl": null,
  "capabilities": {
    "streaming": true,
    "pushNotifications": true,
    "extendedAgentCard": false,
    "extensions": [
      {
        "description": "push notification extension",
        "params": null,
        "required": false,
        "uri": "https://projects.tmforum.org/a2aproject/telecommunication/extensions/Notification-T/v1"
      }
    ]
  },
  "defaultInputModes": ["text"],
  "defaultOutputModes": ["text"],
  "skills": [
    {
      "id": "event_publish",
      "name": "Event publish",
      "description": "Helps with event publish",
      "tags": ["event"],
      "examples": ["push current event"],
      "inputModes": null,
      "outputModes": null,
      "securityRequirements": null
    }
  ],
  "securitySchemes": {
    "oauth2SecurityScheme": {
      "flows": {
        "authorizationCode": null,
        "clientCredentials": {
          "refreshUrl": null,
          "scopes": {
            "profile": "profile",
            "openid": "openid"
          },
          "tokenUrl": "http://a2a-agent-backend:27561/auth"
        },
        "deviceCode": null
      },
      "description": "Enables client credentials flow for authentication and authorization",
      "oauth2MetadataUrl": null
    }
  },
  "securityRequirements": null,
  "iconUrl": null,
  "supportedInterfaces": [
    {
      "protocolBinding": "HTTP+JSON",
      "url": "http://a2a-agent-backend:27561",
      "tenant": "",
      "protocolVersion": "1.0"
    }
  ],
  "signatures": null
}`

	var card AgentCard
	if err := json.Unmarshal([]byte(javaJacksonJSON), &card); err != nil {
		t.Fatalf("failed to parse Java Jackson AgentCard: %v", err)
	}

	// Verify security scheme was parsed
	if len(card.SecuritySchemes) != 1 {
		t.Fatalf("expected 1 security scheme, got %d", len(card.SecuritySchemes))
	}
	scheme := card.SecuritySchemes["oauth2SecurityScheme"]
	oauth2, ok := scheme.(OAuth2SecurityScheme)
	if !ok {
		t.Fatalf("expected OAuth2SecurityScheme, got %T", scheme)
	}
	cc, ok := oauth2.Flows.(ClientCredentialsOAuthFlow)
	if !ok {
		t.Fatalf("expected ClientCredentialsOAuthFlow, got %T", oauth2.Flows)
	}
	if cc.TokenURL != "http://a2a-agent-backend:27561/auth" {
		t.Fatalf("tokenUrl mismatch: got %s", cc.TokenURL)
	}
	t.Logf("✓ parsed Java Jackson AgentCard successfully: name=%s, tokenUrl=%s", card.Name, cc.TokenURL)
}
