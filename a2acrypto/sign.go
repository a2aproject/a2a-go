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

// SignatureSpec describes one key with which to sign an AgentCard. An empty
// Algorithm is inferred from the key.
type SignatureSpec struct {
	PrivateKey crypto.Signer
	KeyID      string
	Algorithm  string
	JWKSURL    string
}

// SignatureSpecResolver returns the keys a Signer should sign with.
//
// Resolve is called on every Sign, so returning a different set over time rotates
// signing keys without restarting the server.
type SignatureSpecResolver interface {
	// Resolve returns the specs to sign with right now. An empty slice signs nothing.
	Resolve(ctx context.Context) ([]SignatureSpec, error)
}

// SignatureSpecResolverFunc adapts a function to a [SignatureSpecResolver].
type SignatureSpecResolverFunc func(ctx context.Context) ([]SignatureSpec, error)

// Resolve implements [SignatureSpecResolver].
func (f SignatureSpecResolverFunc) Resolve(ctx context.Context) ([]SignatureSpec, error) {
	return f(ctx)
}

// FixedSignatureSpec returns a [SignatureSpecResolver] that always resolves to the given specs.
func FixedSignatureSpec(specs ...SignatureSpec) SignatureSpecResolver {
	fixed := append([]SignatureSpec(nil), specs...)
	return SignatureSpecResolverFunc(func(context.Context) ([]SignatureSpec, error) {
		return fixed, nil
	})
}

// SignerConfig configures AgentCard signing.
type SignerConfig struct {
	KeyResolver SignatureSpecResolver
}

// Signer creates JWS signatures for AgentCards.
type Signer struct {
	pkr SignatureSpecResolver
}

// NewSigner creates a Signer using the provided configuration.
func NewSigner(config SignerConfig) *Signer {
	return &Signer{pkr: config.KeyResolver}
}

// Sign computes a JWS signature (RFC 7515) over an AgentCard's raw JSON for each
// key currently resolved by the Signer's [SignatureSpecResolver], returning one
// signature per key. The bytes are canonicalized as given (RFC 8785, excluding
// the top-level signatures field).
func (s *Signer) Sign(ctx context.Context, raw json.RawMessage) ([]*a2a.AgentCardSignature, error) {
	if s.pkr == nil {
		return nil, fmt.Errorf("no private key resolver configured")
	}
	specs, err := s.pkr.Resolve(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve signing keys: %w", err)
	}

	payload, err := canonicalizeJSON(raw)
	if err != nil {
		return nil, fmt.Errorf("failed to canonicalize agent card: %w", err)
	}

	sigs := make([]*a2a.AgentCardSignature, 0, len(specs))
	for i, spec := range specs {
		sig, err := signPayload(spec, payload)
		if err != nil {
			return nil, fmt.Errorf("failed to sign agent card with key %d: %w", i, err)
		}
		sigs = append(sigs, sig)
	}
	return sigs, nil
}

func signPayload(spec SignatureSpec, payload []byte) (*a2a.AgentCardSignature, error) {
	if spec.PrivateKey == nil {
		return nil, fmt.Errorf("nil private key")
	}
	alg := spec.Algorithm
	if alg == "" {
		alg = inferAlgorithm(spec.PrivateKey)
	}

	protected := map[string]any{
		"alg": alg,
		"kid": spec.KeyID,
		"typ": "JOSE",
	}
	if spec.JWKSURL != "" {
		protected["jku"] = spec.JWKSURL
	}

	protectedJSON, err := json.Marshal(protected)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal protected header: %w", err)
	}

	protectedB64 := base64.RawURLEncoding.EncodeToString(protectedJSON)
	payloadB64 := base64.RawURLEncoding.EncodeToString(payload)
	signingInput := protectedB64 + "." + payloadB64

	hash, err := algToHash(alg)
	if err != nil {
		return nil, err
	}

	var signature []byte
	if hash == 0 {
		signature, err = spec.PrivateKey.Sign(rand.Reader, []byte(signingInput), crypto.Hash(0))
	} else {
		h := hash.New()
		h.Write([]byte(signingInput))
		signature, err = spec.PrivateKey.Sign(rand.Reader, h.Sum(nil), hash)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to sign: %w", err)
	}

	// Convert ECDSA DER output to raw R||S for JWS compatibility.
	if pub, ok := spec.PrivateKey.Public().(*ecdsa.PublicKey); ok {
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
