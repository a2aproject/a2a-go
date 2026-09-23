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

package a2asrv

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2acrypto"
)

func mustMarshalCard(t *testing.T, card *a2a.AgentCard) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(card)
	if err != nil {
		t.Fatalf("failed to marshal card: %v", err)
	}
	return raw
}

// testSignerPair returns a signer and a verifier that trusts only this signer's key.
func testSignerPair(t *testing.T, kid string) (*a2acrypto.Signer, *a2acrypto.Verifier) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	signer := a2acrypto.NewSigner(a2acrypto.SignerConfig{PrivateKey: key, KeyID: kid})
	resolver := a2acrypto.NewStaticKeyResolver(map[string]crypto.PublicKey{kid: key.Public()})
	verifier := a2acrypto.NewVerifier(a2acrypto.VerifierConfig{KeyResolver: resolver})
	return signer, verifier
}

func testCardProducer(card *a2a.AgentCard) AgentCardProducer {
	return AgentCardProducerFn(func(context.Context) (*a2a.AgentCard, error) { return card, nil })
}

func testAgentCard() *a2a.AgentCard {
	return &a2a.AgentCard{
		Name:        "Test Agent",
		Description: "A test agent for signing",
		Version:     "1.0.0",
	}
}

func TestSignedCardProducerSignsWithEverySigner(t *testing.T) {
	t.Parallel()

	card := testAgentCard()
	current, currentVerifier := testSignerPair(t, "kid-current")
	next, nextVerifier := testSignerPair(t, "kid-next")

	producer := NewSignedCardProducer([]*a2acrypto.Signer{current, next}, testCardProducer(card))

	signed, err := producer.Card(context.Background())
	if err != nil {
		t.Fatalf("Card() error = %v, want nil", err)
	}

	// Rotation works by publishing a signature per key, so every signer must be represented.
	if len(signed.Signatures) != 2 {
		t.Fatalf("Card() produced %d signatures, want 2", len(signed.Signatures))
	}
	signedRaw := mustMarshalCard(t, signed)
	for i, verifier := range []*a2acrypto.Verifier{currentVerifier, nextVerifier} {
		if err := verifier.Verify(signedRaw, &signed.Signatures[i]); err != nil {
			t.Errorf("signature %d did not verify: %v", i, err)
		}
	}

	// The card owned by the wrapped producer must not be modified.
	if len(card.Signatures) != 0 {
		t.Errorf("wrapped producer card was modified, got %d signatures", len(card.Signatures))
	}
}

func TestSignedCardProducerKeepsExistingSignatures(t *testing.T) {
	t.Parallel()

	card := testAgentCard()
	existing, existingVerifier := testSignerPair(t, "kid-existing")
	existingSig, err := existing.Sign(mustMarshalCard(t, card))
	if err != nil {
		t.Fatalf("Sign() error = %v, want nil", err)
	}
	card.Signatures = []a2a.AgentCardSignature{*existingSig}

	added, addedVerifier := testSignerPair(t, "kid-added")
	producer := NewSignedCardProducer([]*a2acrypto.Signer{added}, testCardProducer(card))

	signed, err := producer.Card(context.Background())
	if err != nil {
		t.Fatalf("Card() error = %v, want nil", err)
	}

	if len(signed.Signatures) != 2 {
		t.Fatalf("Card() produced %d signatures, want 2", len(signed.Signatures))
	}
	signedRaw := mustMarshalCard(t, signed)
	if err := existingVerifier.Verify(signedRaw, &signed.Signatures[0]); err != nil {
		t.Errorf("pre-existing signature did not verify: %v", err)
	}
	if err := addedVerifier.Verify(signedRaw, &signed.Signatures[1]); err != nil {
		t.Errorf("added signature did not verify: %v", err)
	}
}

func TestSignedCardProducerRejectsNilSigner(t *testing.T) {
	t.Parallel()

	producer := NewSignedCardProducer([]*a2acrypto.Signer{nil}, testCardProducer(testAgentCard()))

	if _, err := producer.Card(context.Background()); err == nil {
		t.Error("Card() returned nil error for a nil signer, want error")
	}
}
