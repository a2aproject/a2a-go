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

package a2acrypto

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
)

const defaultAgentIDJWKSURL = "https://getagentid.dev/.well-known/jwks.json"

// jwks represents a JSON Web Key Set as defined in RFC 7517.
type jwks struct {
	Keys []jwk `json:"keys"`
}

// jwk represents a JSON Web Key as defined in RFC 7517.
type jwk struct {
	Kty string `json:"kty"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
	Kid string `json:"kid"`
}

// AgentIDKeyResolver resolves did:agentid key IDs by fetching the JWKS from
// getagentid.dev and matching the kid against the keys in the set.
//
// It supports Ed25519 (OKP/crv=Ed25519) and ECDSA P-256 (EC/crv=P-256) keys.
type AgentIDKeyResolver struct {
	// JWKSURL is the JWKS endpoint to fetch keys from.
	// Defaults to https://getagentid.dev/.well-known/jwks.json.
	JWKSURL string

	// HTTPClient is the HTTP client used to fetch the JWKS.
	// If nil, http.DefaultClient is used.
	HTTPClient *http.Client
}

// ResolveKey implements KeyResolver by fetching the JWKS and looking up the
// public key matching the given kid. The jku parameter is ignored because
// did:agentid always resolves to a fixed well-known JWKS endpoint.
func (r *AgentIDKeyResolver) ResolveKey(kid, jku string) (crypto.PublicKey, error) {
	url := r.JWKSURL
	if url == "" {
		url = defaultAgentIDJWKSURL
	}

	client := r.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("agentid: failed to fetch JWKS from %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("agentid: JWKS request returned status %d", resp.StatusCode)
	}

	var set jwks
	if err := json.NewDecoder(resp.Body).Decode(&set); err != nil {
		return nil, fmt.Errorf("agentid: failed to decode JWKS: %w", err)
	}

	for _, key := range set.Keys {
		if key.Kid != kid {
			continue
		}
		return parseJWKPublicKey(key)
	}

	return nil, fmt.Errorf("agentid: key %q not found in JWKS", kid)
}

// parseJWKPublicKey converts a JWK into a crypto.PublicKey.
func parseJWKPublicKey(key jwk) (crypto.PublicKey, error) {
	switch key.Kty {
	case "OKP":
		return parseOKPPublicKey(key)
	case "EC":
		return parseECPublicKey(key)
	default:
		return nil, fmt.Errorf("agentid: unsupported key type %q", key.Kty)
	}
}

// parseOKPPublicKey parses an Octet Key Pair (RFC 8037) public key.
func parseOKPPublicKey(key jwk) (crypto.PublicKey, error) {
	if key.Crv != "Ed25519" {
		return nil, fmt.Errorf("agentid: unsupported OKP curve %q", key.Crv)
	}

	x, err := base64.RawURLEncoding.DecodeString(key.X)
	if err != nil {
		return nil, fmt.Errorf("agentid: failed to decode Ed25519 x: %w", err)
	}

	if len(x) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("agentid: invalid Ed25519 key length %d, want %d", len(x), ed25519.PublicKeySize)
	}

	pub := make(ed25519.PublicKey, ed25519.PublicKeySize)
	copy(pub, x)
	return pub, nil
}

// parseECPublicKey parses an Elliptic Curve (RFC 7518) public key.
func parseECPublicKey(key jwk) (crypto.PublicKey, error) {
	if key.Crv != "P-256" {
		return nil, fmt.Errorf("agentid: unsupported EC curve %q", key.Crv)
	}

	xBytes, err := base64.RawURLEncoding.DecodeString(key.X)
	if err != nil {
		return nil, fmt.Errorf("agentid: failed to decode EC x: %w", err)
	}

	yBytes, err := base64.RawURLEncoding.DecodeString(key.Y)
	if err != nil {
		return nil, fmt.Errorf("agentid: failed to decode EC y: %w", err)
	}

	pub := &ecdsa.PublicKey{
		Curve: elliptic.P256(),
		X:     new(big.Int).SetBytes(xBytes),
		Y:     new(big.Int).SetBytes(yBytes),
	}

	if !pub.Curve.IsOnCurve(pub.X, pub.Y) {
		return nil, fmt.Errorf("agentid: EC key point is not on P-256 curve")
	}

	return pub, nil
}

// Compile-time check that AgentIDKeyResolver implements KeyResolver.
var _ KeyResolver = (*AgentIDKeyResolver)(nil)
