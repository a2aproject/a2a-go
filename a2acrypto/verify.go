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

// Verify checks the AgentCard signature against the card content.
func (v *Verifier) Verify(card *a2a.AgentCard, sig *a2a.AgentCardSignature) error {
	if sig == nil {
		return fmt.Errorf("%w: nil signature", ErrVerificationFailed)
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

	pubKey, err := v.kr.ResolveKey(kid, jku)
	if err != nil {
		return fmt.Errorf("%w: key resolution failed: %v", ErrVerificationFailed, err)
	}

	payload, err := canonicalPayload(card)
	if err != nil {
		return fmt.Errorf("%w: failed to canonicalize card for verification: %v", ErrVerificationFailed, err)
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
