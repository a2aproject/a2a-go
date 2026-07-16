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

// Package a2acrypto provides utilities for AgentCard JWS signing and verification.
package a2acrypto

import (
	"crypto"
)

// KeyResolver resolves a key ID and JWKS URL into a public key for verification.
type KeyResolver interface {
	// ResolveKey looks up the public key for the given key ID (kid) and JWKS endpoint (jku).
	ResolveKey(kid, jku string) (crypto.PublicKey, error)
}

// VerifierConfig configures signature verification.
type VerifierConfig struct {
	KeyResolver KeyResolver
}

// Verifier verifies AgentCard JWS signatures.
type Verifier struct {
	kr KeyResolver
}

// NewVerifier creates a Verifier using the provided configuration.
func NewVerifier(config VerifierConfig) *Verifier {
	return &Verifier{kr: config.KeyResolver}
}

// SignerConfig configures AgentCard signing.
type SignerConfig struct {
	PrivateKey crypto.Signer
	KeyID      string
	Algorithm  string
	JWKSURL    string
}

// Signer creates JWS signatures for AgentCards.
type Signer struct {
	key       crypto.Signer
	kid       string
	algorithm string
	jwksURL   string
}

// NewSigner creates a Signer using the provided configuration.
func NewSigner(config SignerConfig) *Signer {
	alg := config.Algorithm
	if alg == "" {
		alg = inferAlgorithm(config.PrivateKey)
	}
	return &Signer{
		key:       config.PrivateKey,
		kid:       config.KeyID,
		algorithm: alg,
		jwksURL:   config.JWKSURL,
	}
}
