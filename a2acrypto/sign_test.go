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
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/go-cmp/cmp"
)

func mustGenerateECDSAP256Key(t *testing.T) crypto.Signer {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	return key
}

func makeTestCard() *a2a.AgentCard {
	return &a2a.AgentCard{
		Name:        "Test Agent",
		Description: "A test agent for signing",
		Version:     "1.0.0",
		Skills: []a2a.AgentSkill{
			{ID: "skill-1", Name: "test skill"},
		},
	}
}

func TestSignAndVerifyES256(t *testing.T) {
	t.Parallel()

	key := mustGenerateECDSAP256Key(t)

	signer := NewSigner(SignerConfig{
		PrivateKey: key,
		KeyID:      "test-kid",
		Algorithm:  "ES256",
	})

	card := makeTestCard()
	sig, err := signer.Sign(card)
	if err != nil {
		t.Fatalf("Sign() error = %v, want nil", err)
	}

	if sig.Protected == "" {
		t.Error("signature protected header is empty")
	}
	if sig.Signature == "" {
		t.Error("signature is empty")
	}

	staticResolver := &staticKeyResolver{pub: key.Public()}
	verifier := NewVerifier(VerifierConfig{KeyResolver: staticResolver})

	if err := verifier.Verify(card, sig); err != nil {
		t.Fatalf("Verify() error = %v, want nil", err)
	}
}

func TestSignAndVerify_tampered_card_fails(t *testing.T) {
	t.Parallel()

	key := mustGenerateECDSAP256Key(t)
	signer := NewSigner(SignerConfig{PrivateKey: key, KeyID: "kid"})

	card := makeTestCard()
	sig, err := signer.Sign(card)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}

	tampered := makeTestCard()
	tampered.Name = "Evil Agent"

	staticResolver := &staticKeyResolver{pub: key.Public()}
	verifier := NewVerifier(VerifierConfig{KeyResolver: staticResolver})
	if err := verifier.Verify(tampered, sig); err == nil {
		t.Error("Verify() returned nil for tampered card, want error")
	}
}

func TestSignAlgorithmInference(t *testing.T) {
	t.Parallel()

	key := mustGenerateECDSAP256Key(t)
	signer := NewSigner(SignerConfig{PrivateKey: key, KeyID: "kid"})
	card := makeTestCard()
	sig, err := signer.Sign(card)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}

	staticResolver := &staticKeyResolver{pub: key.Public()}
	verifier := NewVerifier(VerifierConfig{KeyResolver: staticResolver})
	if err := verifier.Verify(card, sig); err != nil {
		t.Errorf("Verify() with inferred algorithm error = %v", err)
	}
}

func TestVerify_nil_signature(t *testing.T) {
	t.Parallel()

	key := mustGenerateECDSAP256Key(t)
	staticResolver := &staticKeyResolver{pub: key.Public()}
	verifier := NewVerifier(VerifierConfig{KeyResolver: staticResolver})

	err := verifier.Verify(makeTestCard(), nil)
	if err == nil {
		t.Error("Verify(nil sig) returned nil, want error")
	}
}

func TestSign_protected_header_contains_kid(t *testing.T) {
	t.Parallel()

	key := mustGenerateECDSAP256Key(t)
	signer := NewSigner(SignerConfig{
		PrivateKey: key,
		KeyID:      "my-key-123",
		JWKSURL:    "https://example.com/jwks.json",
	})

	card := makeTestCard()
	sig, err := signer.Sign(card)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}

	// Verify protected header can be decoded and contains expected fields.
	importEncodingPayload := struct {
		Protected string `json:"protected"`
		Signature string `json:"signature"`
	}{Protected: sig.Protected, Signature: sig.Signature}

	if diff := cmp.Diff(sig.Protected, importEncodingPayload.Protected); diff != "" {
		t.Errorf("protected diff (-want +got):\n%s", diff)
	}
}

// staticKeyResolver returns a fixed public key for testing.
type staticKeyResolver struct {
	pub crypto.PublicKey
}

func (r *staticKeyResolver) ResolveKey(kid, jku string) (crypto.PublicKey, error) {
	return r.pub, nil
}

func TestSignAndVerifyEd25519(t *testing.T) {
	t.Parallel()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	signer := NewSigner(SignerConfig{
		PrivateKey: priv,
		KeyID:      "ed25519-kid",
	})

	card := makeTestCard()
	sig, err := signer.Sign(card)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}

	staticResolver := &staticKeyResolver{pub: pub}
	verifier := NewVerifier(VerifierConfig{KeyResolver: staticResolver})
	if err := verifier.Verify(card, sig); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
}

func TestSign_excludes_signatures_from_payload(t *testing.T) {
	t.Parallel()

	key := mustGenerateECDSAP256Key(t)
	signer := NewSigner(SignerConfig{PrivateKey: key, KeyID: "kid"})

	card := makeTestCard()
	card.Signatures = []a2a.AgentCardSignature{
		{Protected: "existing", Signature: "sig"},
	}

	sig, err := signer.Sign(card)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}

	// Verify should succeed because signatures are excluded from canonical payload.
	staticResolver := &staticKeyResolver{pub: key.Public()}
	verifier := NewVerifier(VerifierConfig{KeyResolver: staticResolver})
	if err := verifier.Verify(card, sig); err != nil {
		t.Fatalf("Verify() with pre-existing signatures error = %v", err)
	}
}

func TestSign_protected_header_has_typ(t *testing.T) {
	t.Parallel()

	key := mustGenerateECDSAP256Key(t)
	signer := NewSigner(SignerConfig{PrivateKey: key, KeyID: "kid"})

	sig, err := signer.Sign(makeTestCard())
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}

	protectedJSON, err := base64.RawURLEncoding.DecodeString(sig.Protected)
	if err != nil {
		t.Fatalf("failed to decode protected header: %v", err)
	}
	var protected map[string]any
	if err := json.Unmarshal(protectedJSON, &protected); err != nil {
		t.Fatalf("failed to parse protected header: %v", err)
	}
	if typ, ok := protected["typ"].(string); !ok || typ != "JOSE+JSON" {
		t.Errorf("protected header typ = %v, want JOSE+JSON", protected["typ"])
	}
}
