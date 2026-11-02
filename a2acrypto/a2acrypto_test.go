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
	"crypto/ed25519"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

// testdata/golden.json is produced by the a2a-python reference SDK.
type golden struct {
	Kid          string          `json:"kid"`
	SeedHex      string          `json:"ed25519_seed_hex"`
	CardJSON     json.RawMessage `json:"card_json_unsigned"`
	ProtectedB64 string          `json:"protected_b64"`
	SignatureB64 string          `json:"signature_b64"`
}

//go:embed testdata/golden.json
var goldenJSON []byte

func TestGoldenSignMatchesReference(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	var g golden
	if err := json.Unmarshal(goldenJSON, &g); err != nil {
		t.Fatalf("parse golden.json: %v", err)
	}
	seed, err := hex.DecodeString(g.SeedHex)
	if err != nil {
		t.Fatalf("seed hex: %v", err)
	}
	key := ed25519.NewKeyFromSeed(seed)

	signer := mustNewSigner(t, SignerConfig{PrivateKey: key, KeyID: g.Kid, Algorithm: "EdDSA"})
	sig := mustSign(t, signer, g.CardJSON)
	if sig.Protected != g.ProtectedB64 {
		t.Errorf("Sign() protected = %s, want %s", sig.Protected, g.ProtectedB64)
	}
	if sig.Signature != g.SignatureB64 {
		t.Errorf("Sign() signature = %s, want %s", sig.Signature, g.SignatureB64)
	}

	verifier := staticVerifier(key.Public())
	if err := verifier.Verify(ctx, g.CardJSON, sig); err != nil {
		t.Errorf("Verify() error = %v, want nil", err)
	}
}

func TestGoldenServerCardMatchesReference(t *testing.T) {
	t.Parallel()

	var g golden
	if err := json.Unmarshal(goldenJSON, &g); err != nil {
		t.Fatalf("parse golden.json: %v", err)
	}
	seed, err := hex.DecodeString(g.SeedHex)
	if err != nil {
		t.Fatalf("seed hex: %v", err)
	}
	key := ed25519.NewKeyFromSeed(seed)

	var card a2a.AgentCard
	if err := json.Unmarshal(g.CardJSON, &card); err != nil {
		t.Fatalf("unmarshal card: %v", err)
	}
	raw, err := json.Marshal(&card)
	if err != nil {
		t.Fatalf("marshal card: %v", err)
	}

	signer := mustNewSigner(t, SignerConfig{PrivateKey: key, KeyID: g.Kid, Algorithm: "EdDSA"})
	sig := mustSign(t, signer, raw)
	if sig.Signature != g.SignatureB64 {
		t.Errorf("server card signature = %s, want %s", sig.Signature, g.SignatureB64)
	}
}
