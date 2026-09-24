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
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

// SignerConfig configures AgentCard signing.
type SignerConfig struct {
	// PrivateKey is used to create a signature.
	PrivateKey crypto.Signer
	// KeyID is the kid in the signature header.
	KeyID string
	// Algorithm is alg in the signature header. Inferred from the private key if not provided.
	Algorithm string
	// JWKSURL is a jku in the signature header.
	JWKSURL string
}

// Signer creates JWS signatures for AgentCards.
type Signer struct {
	key       crypto.Signer
	hash      crypto.Hash
	kid       string
	algorithm string
	jwksURL   string
}

// NewSigner creates a Signer using the provided configuration.
func NewSigner(config SignerConfig) (*Signer, error) {
	if config.Algorithm == "" {
		alg, err := inferAlgorithm(config.PrivateKey)
		if err != nil {
			return nil, err
		}
		config.Algorithm = alg
	}
	hash, err := algToHash(config.Algorithm)
	if err != nil {
		return nil, err
	}
	return &Signer{
		key:       config.PrivateKey,
		kid:       config.KeyID,
		algorithm: config.Algorithm,
		jwksURL:   config.JWKSURL,
		hash:      hash,
	}, nil
}

// Sign computes a JWS signature (RFC 7515) over an AgentCard's raw JSON for each
// key currently resolved by the Signer's [SignatureSpecResolver], returning one
// signature per key. The bytes are canonicalized as given (RFC 8785, excluding
// the top-level signatures field).
func (s *Signer) Sign(ctx context.Context, raw json.RawMessage) (*a2a.AgentCardSignature, error) {
	if s.key == nil {
		return nil, fmt.Errorf("nil private key")
	}

	payload, err := canonicalizeJSON(raw)
	if err != nil {
		return nil, fmt.Errorf("failed to canonicalize agent card: %w", err)
	}

	protected := map[string]any{
		"alg": s.algorithm,
		"kid": s.kid,
		"typ": "JOSE",
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

	var signature []byte
	if s.hash == 0 {
		signature, err = s.key.Sign(rand.Reader, []byte(signingInput), crypto.Hash(0))
	} else {
		h := s.hash.New()
		h.Write([]byte(signingInput))
		signature, err = s.key.Sign(rand.Reader, h.Sum(nil), s.hash)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to sign: %w", err)
	}

	// Convert ECDSA DER output to raw R||S for JWS compatibility.
	if pub, ok := s.key.Public().(*ecdsa.PublicKey); ok {
		signature, err = marshalECDSASignature(signature, pub.Curve)
		if err != nil {
			return nil, fmt.Errorf("failed to convert ECDSA signature: %w", err)
		}
	}

	return &a2a.AgentCardSignature{
		Protected: protectedB64,
		Signature: base64.RawURLEncoding.EncodeToString(signature),
	}, nil
}
