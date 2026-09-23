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
	"context"
	"crypto"
)

// KeyResolver returns the public key that a Verifier uses to check an
// AgentCard's signature.
//
// A signature names its key with two fields in its header:
//   - kid: a short label identifying which key signed the card.
//   - jku: a URL where the signer claims its public keys live.
//
// A card is not trusted until its signature checks out, so both fields are
// attacker-controlled. The trust root (the set of keys the verifier is willing
// to accept) must therefore be decided by the verifier, not taken from the card.
//
// A typical implementation holds a fixed set of trusted keys and uses kid to pick
// among them, returning an error when no trusted key matches. jku is passed for information
// only: implementations MUST NOT fetch a key from it to establish trust, because
// a forger could then serve both a fake card and a key set that "verifies" it. A
// resolver that does fetch keys by URL MUST restrict the URL to a verifier-side
// allowlist rather than trust the jku.
type KeyResolver interface {
	// ResolveKey returns the public key for the given kid. untrustedJKU is the
	// signer-supplied JWK Set URL; it MUST NOT be blindly trusted to select or fetch the key.
	ResolveKey(ctx context.Context, kid, untrustedJKU string) (crypto.PublicKey, error)
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
