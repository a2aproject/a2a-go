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

// PublicKeyResolver returns the public key that a Verifier uses to check an
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
type PublicKeyResolver interface {
	// ResolveKey returns the public key for the given kid. untrustedJKU is the
	// signer-supplied JWK Set URL; it MUST NOT be blindly trusted to select or fetch the key.
	ResolveKey(ctx context.Context, kid, untrustedJKU string) (crypto.PublicKey, error)
}

// VerifierConfig configures signature verification.
type VerifierConfig struct {
	KeyResolver PublicKeyResolver
}

// Verifier verifies AgentCard JWS signatures.
type Verifier struct {
	kr PublicKeyResolver
}

// NewVerifier creates a Verifier using the provided configuration.
func NewVerifier(config VerifierConfig) *Verifier {
	return &Verifier{kr: config.KeyResolver}
}

// SignatureSpec describes one key with which to sign an AgentCard. An empty
// Algorithm is inferred from the key.
type SignatureSpec struct {
	PrivateKey crypto.Signer
	KeyID      string
	Algorithm  string
	JWKSURL    string
}

// PrivateKeyResolver returns the keys a Signer should sign with.
//
// Resolve is called on every Sign, so returning a different set over time rotates
// signing keys without restarting the server.
type PrivateKeyResolver interface {
	// Resolve returns the specs to sign with right now. An empty slice signs nothing.
	Resolve(ctx context.Context) ([]SignatureSpec, error)
}

// PrivateKeyResolverFunc adapts a function to a [PrivateKeyResolver].
type PrivateKeyResolverFunc func(ctx context.Context) ([]SignatureSpec, error)

// Resolve implements [PrivateKeyResolver].
func (f PrivateKeyResolverFunc) Resolve(ctx context.Context) ([]SignatureSpec, error) {
	return f(ctx)
}

// StaticPrivateKeyResolver returns a [PrivateKeyResolver] that always resolves to
// the given specs. Use it for a fixed signing key; for rotation supply a resolver
// that returns the current set on each call.
func StaticPrivateKeyResolver(specs ...SignatureSpec) PrivateKeyResolver {
	fixed := append([]SignatureSpec(nil), specs...)
	return PrivateKeyResolverFunc(func(context.Context) ([]SignatureSpec, error) {
		return fixed, nil
	})
}

// SignerConfig configures AgentCard signing.
type SignerConfig struct {
	KeyResolver PrivateKeyResolver
}

// Signer creates JWS signatures for AgentCards.
type Signer struct {
	pkr PrivateKeyResolver
}

// NewSigner creates a Signer using the provided configuration.
func NewSigner(config SignerConfig) *Signer {
	return &Signer{pkr: config.KeyResolver}
}
