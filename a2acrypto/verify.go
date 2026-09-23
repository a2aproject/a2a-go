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
	"crypto/ed25519"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

// ErrVerificationFailed indicates the signature did not verify.
var ErrVerificationFailed = errors.New("signature verification failed")

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

// Verify checks sig against an AgentCard's raw JSON. The bytes are canonicalized
// as given (RFC 8785, excluding the top-level signatures field); see Signer.Sign.
func (v *Verifier) Verify(ctx context.Context, raw json.RawMessage, sig *a2a.AgentCardSignature) error {
	if sig == nil {
		return fmt.Errorf("%w: nil signature", ErrVerificationFailed)
	}

	payload, err := canonicalizeJSON(raw)
	if err != nil {
		return fmt.Errorf("%w: failed to canonicalize card for verification: %v", ErrVerificationFailed, err)
	}

	protectedJSON, err := base64.RawURLEncoding.DecodeString(sig.Protected)
	if err != nil {
		return fmt.Errorf("%w: invalid protected header encoding: %v", ErrVerificationFailed, err)
	}

	var protected map[string]any
	if err := json.Unmarshal(protectedJSON, &protected); err != nil {
		return fmt.Errorf("%w: invalid protected header JSON: %v", ErrVerificationFailed, err)
	}

	alg, _ := protected["alg"].(string)
	kid, _ := protected["kid"].(string)
	jku, _ := protected["jku"].(string)

	if alg == "" {
		return fmt.Errorf("%w: missing or invalid 'alg' in protected header", ErrVerificationFailed)
	}
	if kid == "" {
		return fmt.Errorf("%w: missing or invalid 'kid' in protected header", ErrVerificationFailed)
	}

	if v.kr == nil {
		return fmt.Errorf("%w: no key resolver configured", ErrVerificationFailed)
	}

	pubKey, err := v.kr.ResolveKey(ctx, kid, jku)
	if err != nil {
		return fmt.Errorf("%w: key resolution failed: %v", ErrVerificationFailed, err)
	}

	payloadB64 := base64.RawURLEncoding.EncodeToString(payload)
	signingInput := sig.Protected + "." + payloadB64

	sigBytes, err := base64.RawURLEncoding.DecodeString(sig.Signature)
	if err != nil {
		return fmt.Errorf("%w: invalid signature encoding: %v", ErrVerificationFailed, err)
	}

	hash, err := algToHash(alg)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrVerificationFailed, err)
	}

	return verifySignature(pubKey, []byte(signingInput), sigBytes, hash)
}

func verifySignature(pubKey crypto.PublicKey, message, sig []byte, hash crypto.Hash) error {
	switch key := pubKey.(type) {
	case *ecdsa.PublicKey:
		if hash == 0 {
			return fmt.Errorf("%w: ECDSA requires a hash", ErrVerificationFailed)
		}
		h := hash.New()
		h.Write(message)
		digest := h.Sum(nil)
		r, s, err := unmarshalECDSASignature(sig, key.Curve)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrVerificationFailed, err)
		}
		if !ecdsa.Verify(key, digest, r, s) {
			return ErrVerificationFailed
		}
		return nil

	case ed25519.PublicKey:
		if !ed25519.Verify(key, message, sig) {
			return ErrVerificationFailed
		}
		return nil

	case *rsa.PublicKey:
		if hash == 0 {
			return fmt.Errorf("%w: RSA requires a hash", ErrVerificationFailed)
		}
		h := hash.New()
		h.Write(message)
		digest := h.Sum(nil)
		if err := rsa.VerifyPKCS1v15(key, hash, digest, sig); err != nil {
			return fmt.Errorf("%w: %v", ErrVerificationFailed, err)
		}
		return nil

	default:
		return fmt.Errorf("%w: unsupported key type %T", ErrVerificationFailed, pubKey)
	}
}
