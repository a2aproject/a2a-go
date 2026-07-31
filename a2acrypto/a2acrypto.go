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

// KeyResolver resolves a key identifier to a public key for verification.
//
// The trust root MUST be selected by verifier-side policy. Implementations
// MUST NOT use the signer-supplied jku to select or fetch the trust root:
// jku is carried in the artifact's own protected header, so honoring it lets
// a signer nominate its own key material. kid MAY be used to select among
// keys enrolled out-of-band through a verifier-controlled path (AgentIDKeyResolver
// does this: fixed endpoint, kid lookup, explicit error on miss).
//
// A resolver that fetches by URL MUST constrain the target to a verifier-side
// allowlist. The same constraint applies to x5u (X.509 URL, RFC 7515) if the
// interface is extended to carry it in the future.
type KeyResolver interface {
	// ResolveKey looks up the public key for the given key ID (kid).
	// The jku parameter is passed for informational purposes only; implementations
	// MUST NOT use it to select or fetch the trust root. See KeyResolver doc.
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
