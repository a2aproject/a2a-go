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
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

func TestSignAndVerifyES256(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	key := mustGenerateECDSAP256Key(t)
	signer := mustNewSigner(t, SignerConfig{PrivateKey: key, KeyID: "test-kid", Algorithm: "ES256"})

	card := mustMarshalCard(t, makeTestCard())
	sig := mustSign(t, signer, card)
	if sig.Protected == "" {
		t.Error("signature protected header is empty")
	}
	if sig.Signature == "" {
		t.Error("signature is empty")
	}

	verifier := staticVerifier(key.Public())
	if err := verifier.Verify(ctx, card, sig); err != nil {
		t.Fatalf("Verify() error = %v, want nil", err)
	}
}

func TestSignAndVerify_tampered_card_fails(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	key := mustGenerateECDSAP256Key(t)
	signer := mustNewSigner(t, SignerConfig{PrivateKey: key, KeyID: "kid"})

	sig := mustSign(t, signer, mustMarshalCard(t, makeTestCard()))

	tampered := makeTestCard()
	tampered.Name = "Evil Agent"

	verifier := staticVerifier(key.Public())
	if err := verifier.Verify(ctx, mustMarshalCard(t, tampered), sig); err == nil {
		t.Error("Verify() returned nil for tampered card, want error")
	}
}

func TestSignAlgorithmInference(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	key := mustGenerateECDSAP256Key(t)
	signer := mustNewSigner(t, SignerConfig{PrivateKey: key, KeyID: "kid"})

	card := mustMarshalCard(t, makeTestCard())
	sig := mustSign(t, signer, card)

	verifier := staticVerifier(key.Public())
	if err := verifier.Verify(ctx, card, sig); err != nil {
		t.Errorf("Verify() with inferred algorithm error = %v", err)
	}
}

func TestVerify_nil_signature(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	key := mustGenerateECDSAP256Key(t)
	verifier := staticVerifier(key.Public())

	if err := verifier.Verify(ctx, mustMarshalCard(t, makeTestCard()), nil); err == nil {
		t.Error("Verify(nil sig) returned nil, want error")
	}
}

func TestSignAndVerifyEd25519(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	signer := mustNewSigner(t, SignerConfig{PrivateKey: priv, KeyID: "ed25519-kid"})

	card := mustMarshalCard(t, makeTestCard())
	sig := mustSign(t, signer, card)

	verifier := staticVerifier(pub)
	if err := verifier.Verify(ctx, card, sig); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
}

func TestSign_excludes_signatures_from_payload(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	key := mustGenerateECDSAP256Key(t)
	signer := mustNewSigner(t, SignerConfig{PrivateKey: key, KeyID: "kid"})

	card := makeTestCard()
	card.Signatures = []a2a.AgentCardSignature{{Protected: "existing", Signature: "sig"}}
	raw := mustMarshalCard(t, card)

	sig := mustSign(t, signer, raw)

	verifier := staticVerifier(key.Public())
	if err := verifier.Verify(ctx, raw, sig); err != nil {
		t.Fatalf("Verify() with pre-existing signatures error = %v", err)
	}
}

func TestSign_protected_header_has_typ(t *testing.T) {
	t.Parallel()

	key := mustGenerateECDSAP256Key(t)
	signer := mustNewSigner(t, SignerConfig{PrivateKey: key, KeyID: "kid"})

	sig := mustSign(t, signer, mustMarshalCard(t, makeTestCard()))

	protectedJSON, err := base64.RawURLEncoding.DecodeString(sig.Protected)
	if err != nil {
		t.Fatalf("failed to decode protected header: %v", err)
	}
	var protected map[string]any
	if err := json.Unmarshal(protectedJSON, &protected); err != nil {
		t.Fatalf("failed to parse protected header: %v", err)
	}
	if typ, ok := protected["typ"].(string); !ok || typ != "JOSE" {
		t.Errorf("protected header typ = %v, want JOSE", protected["typ"])
	}
}

func TestCanonical_U2028_U2029_literal(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	// RFC 8785 requires U+2028 (LINE SEPARATOR) and U+2029 (PARAGRAPH SEPARATOR)
	// to be literal UTF-8 bytes, not escaped as \u2028/\u2029 the way
	// encoding/json emits them.
	card := makeTestCard()
	card.Description = "line1\xe2\x80\xa8line2\xe2\x80\xa9line3"
	raw := mustMarshalCard(t, card)

	payload, err := canonicalizeJSON(raw)
	if err != nil {
		t.Fatalf("canonicalizeJSON() error = %v", err)
	}
	if bytes.Contains(payload, []byte("\\u2028")) || bytes.Contains(payload, []byte("\\u2029")) {
		t.Errorf("canonical payload escaped U+2028/U+2029, want literal bytes: %s", payload)
	}
	if !bytes.Contains(payload, []byte{0xe2, 0x80, 0xa8}) || !bytes.Contains(payload, []byte{0xe2, 0x80, 0xa9}) {
		t.Errorf("canonical payload missing literal U+2028/U+2029 bytes: %s", payload)
	}

	key := mustGenerateECDSAP256Key(t)
	signer := mustNewSigner(t, SignerConfig{PrivateKey: key, KeyID: "kid"})
	sig := mustSign(t, signer, raw)
	verifier := staticVerifier(key.Public())
	if err := verifier.Verify(ctx, raw, sig); err != nil {
		t.Fatalf("Verify() with U+2028/U+2029 error = %v", err)
	}
}

func TestCanonicalNumber_integers_serialize_from_binary64(t *testing.T) {
	t.Parallel()

	// RFC 8785 §3.2.2.3: numbers serialize from their binary64 value, not the
	// exact decimal token. For integers >= 2^53 the two diverge.
	cases := []struct {
		name string
		in   json.Number
		want string
	}{
		{"small integer exact", json.Number("12345"), "12345"},
		{"negative integer exact", json.Number("-42"), "-42"},
		{"2^53 representable", json.Number("9007199254740992"), "9007199254740992"},
		{"2^53+2 representable", json.Number("9007199254740994"), "9007199254740994"},
		{"2^53+1 rounds down", json.Number("9007199254740993"), "9007199254740992"},
		{"2^60 rounds to binary64", json.Number("1152921504606846976"), "1152921504606847000"},
		{"2^68 above int64", json.Number("295147905179352825856"), "295147905179352830000"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := canonicalNumber(tc.in); got != tc.want {
				t.Errorf("canonicalNumber(%s) = %s, want %s", tc.in, got, tc.want)
			}
		})
	}
}

func TestCanonical_float_matches_rfc8785(t *testing.T) {
	t.Parallel()

	// RFC 8785 §3.2.2.2: decimal for 1e-6 <= |x| < 1e21, exponential otherwise
	// with no leading zeros in the exponent, and -0 as 0.
	cases := []struct {
		name string
		in   float64
		want string
	}{
		{"integer within float64 range", 123456789012345680000, "123456789012345680000"},
		{"one-e21 boundary", 1e21, "1e+21"},
		{"one-e-5 decimal", 1e-5, "0.00001"},
		{"one-e-6 decimal boundary", 1e-6, "0.000001"},
		{"one-e-7 exponential", 1e-7, "1e-7"},
		{"fractional decimal", 0.1, "0.1"},
		{"negative exponential", -1.5e-7, "-1.5e-7"},
		{"negative zero", math.Copysign(0, -1), "0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := canonicalFloat(tc.in); got != tc.want {
				t.Errorf("canonicalFloat(%v) = %s, want %s", tc.in, got, tc.want)
			}
		})
	}
}

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

func mustMarshalCard(t *testing.T, card *a2a.AgentCard) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(card)
	if err != nil {
		t.Fatalf("failed to marshal card: %v", err)
	}
	return raw
}

func staticVerifier(pub crypto.PublicKey) *Verifier {
	return NewVerifier(VerifierConfig{KeyResolver: KeyResolverFunc(func(_ context.Context, kid string) (crypto.PublicKey, error) {
		return pub, nil
	})})
}

func mustNewSigner(t *testing.T, cfg SignerConfig) *Signer {
	t.Helper()
	sig, err := NewSigner(cfg)
	if err != nil {
		t.Fatalf("NewSigner() error = %v, want nil", err)
	}
	return sig
}

func mustSign(t *testing.T, signer *Signer, raw json.RawMessage) *a2a.AgentCardSignature {
	t.Helper()
	sig, err := signer.Sign(t.Context(), raw)
	if err != nil {
		t.Fatalf("Sign() error = %v, want nil", err)
	}
	return sig
}

func TestSignerSignRejectsInvalidUnicode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		raw    json.RawMessage
		offset int
	}{
		{name: "high surrogate value", raw: json.RawMessage(`{"name":"\ud800"}`), offset: 9},
		{name: "low surrogate value", raw: json.RawMessage(`{"name":"\udc00"}`), offset: 9},
		{name: "high surrogate key", raw: json.RawMessage(`{"b\ud800key":1}`), offset: 3},
		{name: "low surrogate key", raw: json.RawMessage(`{"b\udc00key":1}`), offset: 3},
		{name: "consecutive high surrogates", raw: json.RawMessage(`{"name":"\ud800\ud800"}`), offset: 9},
		{name: "high surrogate followed by text", raw: json.RawMessage(`{"name":"\ud800x"}`), offset: 9},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			key := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
			signer := mustNewSigner(t, SignerConfig{PrivateKey: key, KeyID: "fixture-key"})
			_, err := signer.Sign(t.Context(), tt.raw)
			want := fmt.Sprintf("failed to canonicalize agent card: invalid Unicode surrogate escape at byte %d", tt.offset)
			if err == nil || err.Error() != want {
				t.Fatalf("Signer.Sign() error = %v, want %q", err, want)
			}
		})
	}
}

func TestVerifierRejectsInvalidUnicodeAlias(t *testing.T) {
	t.Parallel()
	key := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	signer := mustNewSigner(t, SignerConfig{PrivateKey: key, KeyID: "fixture-key"})
	valid := json.RawMessage(`{"name":"broken \ufffd value"}`)
	invalid := json.RawMessage(`{"name":"broken \udc00 value"}`)
	sig := mustSign(t, signer, valid)
	verifier := staticVerifier(key.Public())
	err := verifier.Verify(t.Context(), invalid, sig)
	want := "signature verification failed: failed to canonicalize card for verification: invalid Unicode surrogate escape at byte 16"
	if err == nil || err.Error() != want {
		t.Fatalf("Verifier.Verify() error = %v, want %q", err, want)
	}
}

func TestSignerSignPreservesUnicodeScalars(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		raw       json.RawMessage
		canonical string
	}{
		{name: "smallest surrogate pair", raw: json.RawMessage(`{"name":"\ud800\udc00"}`), canonical: "{\"name\":\"\U00010000\"}"},
		{name: "high surrogate boundary", raw: json.RawMessage(`{"name":"\udbff\udffd"}`), canonical: "{\"name\":\"\U0010fffd\"}"},
		{name: "low surrogate boundary", raw: json.RawMessage(`{"name":"\udbfe\udfff"}`), canonical: "{\"name\":\"\U0010fbff\"}"},
		{name: "astral key", raw: json.RawMessage(`{"\ud834\udd1e":1}`), canonical: "{\"\U0001d11e\":1}"},
		{name: "adjacent pairs", raw: json.RawMessage(`{"name":"\ud800\udc00\udbff\udffd"}`), canonical: "{\"name\":\"\U00010000\U0010fffd\"}"},
		{name: "literal replacement character", raw: json.RawMessage(`{"name":"\ufffd"}`), canonical: "{\"name\":\"\ufffd\"}"},
		{name: "escaped backslash", raw: json.RawMessage(`{"name":"\\ud800"}`), canonical: `{"name":"\\ud800"}`},
		{name: "escaped backslash and pair", raw: json.RawMessage(`{"name":"\\\ud800\udc00"}`), canonical: "{\"name\":\"\\\\\U00010000\"}"},
		{name: "BMP neighbors", raw: json.RawMessage(`{"name":"\ud7ff\ue000"}`), canonical: "{\"name\":\"\ud7ff\ue000\"}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			key := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
			signer := mustNewSigner(t, SignerConfig{PrivateKey: key, KeyID: "fixture-key"})
			sig := mustSign(t, signer, tt.raw)
			signed, err := base64.RawURLEncoding.DecodeString(sig.Signature)
			if err != nil {
				t.Fatalf("signature decode error = %v, want nil", err)
			}
			message := sig.Protected + "." + base64.RawURLEncoding.EncodeToString([]byte(tt.canonical))
			if !ed25519.Verify(key.Public().(ed25519.PublicKey), []byte(message), signed) {
				t.Fatalf("Signer.Sign() did not sign the expected Unicode scalar bytes")
			}
			if err := staticVerifier(key.Public()).Verify(t.Context(), tt.raw, sig); err != nil {
				t.Fatalf("Verifier.Verify() error = %v, want nil", err)
			}
		})
	}
}

func TestSignerSignRejectsInvalidUTF8(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  json.RawMessage
	}{
		{name: "value", raw: json.RawMessage("{\"name\":\"\xc0\x80\"}")},
		{name: "key", raw: json.RawMessage("{\"\xff\":1}")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			key := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
			signer := mustNewSigner(t, SignerConfig{PrivateKey: key, KeyID: "fixture-key"})
			_, err := signer.Sign(t.Context(), tt.raw)
			want := "failed to canonicalize agent card: invalid UTF-8 in JSON"
			if err == nil || err.Error() != want {
				t.Fatalf("Signer.Sign() error = %v, want %q", err, want)
			}
		})
	}
}
