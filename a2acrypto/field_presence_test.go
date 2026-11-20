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
	"encoding/json"
	"testing"
)

// A security scheme whose message is empty is still a declaration: the oneof member is
// selected, so the card says "mTLS is required" even though the scheme carries no fields.
// Canonicalizing it away would make the two cards below indistinguishable to a signature,
// and a signature over one would verify on the other.
//
// This is the case reported against a2a-python and the JS SDK (a2aproject/a2a-python#1278).
// Both canonicalize these cards to the same bytes and drop the declaration; this test pins
// the behaviour that must hold here.
const cardDeclaringEmptyMTLSScheme = `{
  "name": "presence-probe",
  "description": "Declares mTLS with an empty scheme message.",
  "version": "1.0.0",
  "url": "https://example.invalid/a2a",
  "capabilities": {},
  "defaultInputModes": ["text/plain"],
  "defaultOutputModes": ["text/plain"],
  "skills": [],
  "securitySchemes": {"mtls": {"mtlsSecurityScheme": {}}}
}`

// Identical to the card above except that it declares no security scheme at all.
const cardWithoutSecuritySchemes = `{
  "name": "presence-probe",
  "description": "Declares mTLS with an empty scheme message.",
  "version": "1.0.0",
  "url": "https://example.invalid/a2a",
  "capabilities": {},
  "defaultInputModes": ["text/plain"],
  "defaultOutputModes": ["text/plain"],
  "skills": []
}`

func TestSelectedEmptyOneofMemberIsKeptByCanonicalization(t *testing.T) {
	t.Parallel()

	declared, err := canonicalizeJSON(json.RawMessage(cardDeclaringEmptyMTLSScheme))
	if err != nil {
		t.Fatalf("canonicalize the card declaring mTLS: %v", err)
	}
	absent, err := canonicalizeJSON(json.RawMessage(cardWithoutSecuritySchemes))
	if err != nil {
		t.Fatalf("canonicalize the card without security schemes: %v", err)
	}

	if bytes.Equal(declared, absent) {
		t.Fatalf("two cards that differ in whether mTLS is declared canonicalized to the same bytes:\n%s", declared)
	}
	if !bytes.Contains(declared, []byte(`"securitySchemes":{"mtls":{"mtlsSecurityScheme":{}}}`)) {
		t.Errorf("the empty scheme message was not preserved verbatim, got:\n%s", declared)
	}
	if bytes.Contains(absent, []byte("securitySchemes")) {
		t.Errorf("a card with no security schemes gained one:\n%s", absent)
	}
}

func TestSignatureOverOneSideOfThePairDoesNotVerifyOnTheOther(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	key := mustGenerateECDSAP256Key(t)
	signer := mustNewSigner(t, SignerConfig{PrivateKey: key, KeyID: "presence-probe", Algorithm: "ES256"})
	verifier := staticVerifier(key.Public())

	declared := json.RawMessage(cardDeclaringEmptyMTLSScheme)
	absent := json.RawMessage(cardWithoutSecuritySchemes)

	sig := mustSign(t, signer, declared)

	if err := verifier.Verify(ctx, declared, sig); err != nil {
		t.Fatalf("signature does not verify on the card it was made for: %v", err)
	}
	if err := verifier.Verify(ctx, absent, sig); err == nil {
		t.Error("a signature made for the card declaring mTLS verified on the card that declares none")
	}

	// And the other direction, so the assertion cannot pass because of a one-sided bug.
	sigAbsent := mustSign(t, signer, absent)
	if err := verifier.Verify(ctx, absent, sigAbsent); err != nil {
		t.Fatalf("signature does not verify on the card it was made for: %v", err)
	}
	if err := verifier.Verify(ctx, declared, sigAbsent); err == nil {
		t.Error("a signature made for the card without security schemes verified on the card declaring mTLS")
	}
}
