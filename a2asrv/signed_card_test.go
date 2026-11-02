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

func TestSignedCardProducerSignsWithEveryResolvedKey(t *testing.T) {
	t.Parallel()

	card := newTestAgentCard()
	currentSigner, currentVerifier := newSignerVerifierPair(t, "kid-current")
	nextSigner, nextVerifier := newSignerVerifierPair(t, "kid-next")
	resolver := newStaticSignerResolver(currentSigner, nextSigner)
	producer := NewSignedCardProducer(testCardProducer(card), resolver)

	signed, err := producer.Card(context.Background())
	if err != nil {
		t.Fatalf("Card() error = %v, want nil", err)
	}

	// Rotation works by publishing a signature per key, so every resolved key must be represented.
	if len(signed.Signatures) != 2 {
		t.Fatalf("Card() produced %d signatures, want 2", len(signed.Signatures))
	}
	signedRaw := mustMarshalCard(t, signed)
	for i, verifier := range []*a2acrypto.Verifier{currentVerifier, nextVerifier} {
		if err := verifier.Verify(context.Background(), signedRaw, &signed.Signatures[i]); err != nil {
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

	card := newTestAgentCard()
	existingSigner, existingVerifier := newSignerVerifierPair(t, "kid-existing")
	signature, err := existingSigner.Sign(t.Context(), mustMarshalCard(t, card))
	if err != nil {
		t.Fatalf("existingSigner.Sign() error = %v", err)
	}
	card.Signatures = []a2a.AgentCardSignature{*signature}

	addedSigner, addedVerifier := newSignerVerifierPair(t, "kid-added")
	producer := NewSignedCardProducer(testCardProducer(card), newStaticSignerResolver(addedSigner))

	signed, err := producer.Card(context.Background())
	if err != nil {
		t.Fatalf("Card() error = %v, want nil", err)
	}

	if len(signed.Signatures) != 2 {
		t.Fatalf("Card() produced %d signatures, want 2", len(signed.Signatures))
	}
	signedRaw := mustMarshalCard(t, signed)
	if err := existingVerifier.Verify(context.Background(), signedRaw, &signed.Signatures[0]); err != nil {
		t.Errorf("pre-existing signature did not verify: %v", err)
	}
	if err := addedVerifier.Verify(context.Background(), signedRaw, &signed.Signatures[1]); err != nil {
		t.Errorf("added signature did not verify: %v", err)
	}
}

func newSignerVerifierPair(t *testing.T, kid string) (*a2acrypto.Signer, *a2acrypto.Verifier) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	signer, err := a2acrypto.NewSigner(a2acrypto.SignerConfig{PrivateKey: key, KeyID: kid})
	if err != nil {
		t.Fatalf("a2acrypto.NewSigner() error = %v", err)
	}
	verifier := a2acrypto.NewVerifier(a2acrypto.VerifierConfig{
		KeyResolver: a2acrypto.KeyResolverFunc(func(context.Context, string) (crypto.PublicKey, error) {
			return key.Public(), nil
		}),
	})
	return signer, verifier
}

func newStaticSignerResolver(signers ...*a2acrypto.Signer) SignerResolverFunc {
	return SignerResolverFunc(func(ctx context.Context) ([]*a2acrypto.Signer, error) {
		return signers, nil
	})
}

func mustMarshalCard(t *testing.T, card *a2a.AgentCard) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(card)
	if err != nil {
		t.Fatalf("failed to marshal card: %v", err)
	}
	return raw
}

func testCardProducer(card *a2a.AgentCard) AgentCardProducer {
	return AgentCardProducerFn(func(context.Context) (*a2a.AgentCard, error) { return card, nil })
}

func newTestAgentCard() *a2a.AgentCard {
	return &a2a.AgentCard{
		Name:        "Test Agent",
		Description: "A test agent for signing",
		Version:     "1.0.0",
	}
}
