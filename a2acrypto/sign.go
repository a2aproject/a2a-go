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

package a2acrypto

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

// Sign computes a JWS signature for the given AgentCard.
// It serializes the card using RFC 8785 JSON Canonicalization Scheme (JCS),
// which excludes the signatures field and sorts object keys, then builds the
// protected header and signs.
func (s *Signer) Sign(card *a2a.AgentCard) (*a2a.AgentCardSignature, error) {
	payload, err := canonicalPayload(card)
	if err != nil {
		return nil, fmt.Errorf("failed to canonicalize agent card: %w", err)
	}

	protected := map[string]any{
		"alg": s.algorithm,
		"kid": s.kid,
		"typ": "JOSE+JSON",
	}
	if s.jwksURL != "" {
		protected["jku"] = s.jwksURL
	}

	protectedJSON, err := json.Marshal(protected)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal protected header: %w", err)
	}

	protectedB64 := base64.RawURLEncoding.EncodeToString(protectedJSON)
	payloadB64 := base64.RawURLEncoding.EncodeToString(payload)
	signingInput := protectedB64 + "." + payloadB64

	hash, err := algToHash(s.algorithm)
	if err != nil {
		return nil, err
	}

	var signature []byte
	if hash == 0 {
		signature, err = s.key.Sign(rand.Reader, []byte(signingInput), crypto.Hash(0))
	} else {
		h := hash.New()
		h.Write([]byte(signingInput))
		signature, err = s.key.Sign(rand.Reader, h.Sum(nil), hash)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to sign: %w", err)
	}

	// Convert ECDSA DER output to raw R||S for JWS compatibility.
	if _, ok := s.key.Public().(*ecdsa.PublicKey); ok {
		signature, err = marshalECDSASignature(signature, s.key.Public().(*ecdsa.PublicKey).Curve)
		if err != nil {
			return nil, fmt.Errorf("failed to convert ECDSA signature: %w", err)
		}
	}

	return &a2a.AgentCardSignature{
		Protected: protectedB64,
		Signature: base64.RawURLEncoding.EncodeToString(signature),
	}, nil
}
