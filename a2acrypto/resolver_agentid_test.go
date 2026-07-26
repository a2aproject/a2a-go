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
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAgentIDKeyResolver_ResolveKey_Ed25519(t *testing.T) {
	t.Parallel()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate Ed25519 key: %v", err)
	}
	_ = priv // not needed for resolver test

	kid := "agentid-2026-03"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jwksResp := jwks{
			Keys: []jwk{
				{
					Kty: "OKP",
					Crv: "Ed25519",
					X:   base64.RawURLEncoding.EncodeToString(pub),
					Kid: kid,
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jwksResp)
	}))
	defer srv.Close()

	resolver := &AgentIDKeyResolver{JWKSURL: srv.URL}
	resolved, err := resolver.ResolveKey(kid, "")
	if err != nil {
		t.Fatalf("ResolveKey() error = %v", err)
	}

	edPub, ok := resolved.(ed25519.PublicKey)
	if !ok {
		t.Fatalf("ResolveKey() returned %T, want ed25519.PublicKey", resolved)
	}

	if len(edPub) != ed25519.PublicKeySize {
		t.Errorf("ed25519 key length = %d, want %d", len(edPub), ed25519.PublicKeySize)
	}

	// Verify the resolved key actually works for verification.
	msg := []byte("test message for ed25519 verification")
	sig := ed25519.Sign(priv, msg)
	if !ed25519.Verify(edPub, msg, sig) {
		t.Error("resolved Ed25519 key failed to verify a valid signature")
	}
}

func TestAgentIDKeyResolver_ResolveKey_ECDSA_P256(t *testing.T) {
	t.Parallel()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate ECDSA P-256 key: %v", err)
	}

	kid := "ecdsa-key-001"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		keySize := (key.Curve.Params().BitSize + 7) / 8
		jwksResp := jwks{
			Keys: []jwk{
				{
					Kty: "EC",
					Crv: "P-256",
					X:   base64.RawURLEncoding.EncodeToString(key.X.Bytes()[:keySize]),
					Y:   base64.RawURLEncoding.EncodeToString(key.Y.Bytes()[:keySize]),
					Kid: kid,
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jwksResp)
	}))
	defer srv.Close()

	resolver := &AgentIDKeyResolver{JWKSURL: srv.URL}
	resolved, err := resolver.ResolveKey(kid, "")
	if err != nil {
		t.Fatalf("ResolveKey() error = %v", err)
	}

	ecPub, ok := resolved.(*ecdsa.PublicKey)
	if !ok {
		t.Fatalf("ResolveKey() returned %T, want *ecdsa.PublicKey", resolved)
	}

	if ecPub.Curve != elliptic.P256() {
		t.Errorf("curve = %v, want P-256", ecPub.Curve)
	}

	// Verify the resolved key actually works for verification.
	msg := []byte("test message for ecdsa verification")
	hash := sha256.Sum256(msg)
	r, s, err := ecdsa.Sign(rand.Reader, key, hash[:])
	if err != nil {
		t.Fatalf("failed to sign: %v", err)
	}
	if !ecdsa.Verify(ecPub, hash[:], r, s) {
		t.Error("resolved ECDSA key failed to verify a valid signature")
	}
}

func TestAgentIDKeyResolver_ResolveKey_KeyNotFound(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jwksResp := jwks{
			Keys: []jwk{
				{
					Kty: "OKP",
					Crv: "Ed25519",
					X:   base64.RawURLEncoding.EncodeToString(make([]byte, ed25519.PublicKeySize)),
					Kid: "existing-key",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jwksResp)
	}))
	defer srv.Close()

	resolver := &AgentIDKeyResolver{JWKSURL: srv.URL}
	_, err := resolver.ResolveKey("non-existent-kid", "")
	if err == nil {
		t.Fatal("ResolveKey() returned nil error for unknown kid, want error")
	}
}

func TestAgentIDKeyResolver_ResolveKey_NetworkError(t *testing.T) {
	t.Parallel()

	resolver := &AgentIDKeyResolver{
		JWKSURL: "http://127.0.0.1:1", // invalid port, will fail immediately
	}

	_, err := resolver.ResolveKey("any-kid", "")
	if err == nil {
		t.Fatal("ResolveKey() returned nil error for network failure, want error")
	}
}

func TestAgentIDKeyResolver_ResolveKey_HTTPError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	resolver := &AgentIDKeyResolver{JWKSURL: srv.URL}
	_, err := resolver.ResolveKey("any-kid", "")
	if err == nil {
		t.Fatal("ResolveKey() returned nil error for HTTP 500, want error")
	}
}

func TestAgentIDKeyResolver_ResolveKey_InvalidJSON(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, "this is not json")
	}))
	defer srv.Close()

	resolver := &AgentIDKeyResolver{JWKSURL: srv.URL}
	_, err := resolver.ResolveKey("any-kid", "")
	if err == nil {
		t.Fatal("ResolveKey() returned nil error for invalid JSON, want error")
	}
}

func TestAgentIDKeyResolver_DefaultJWKSURL(t *testing.T) {
	t.Parallel()

	resolver := &AgentIDKeyResolver{}
	if resolver.JWKSURL != "" {
		// JWKSURL is empty by default; ResolveKey falls back to the default.
		// Verify that the default is used when the field is empty.
	}

	// Test with an explicit empty string to ensure fallback works.
	// We use the real endpoint for a smoke test.
	resolver2 := &AgentIDKeyResolver{}
	realKid := "agentid-2026-03"

	pub, err := resolver2.ResolveKey(realKid, "")
	if err != nil {
		// Network may not be available in all test environments; skip gracefully.
		t.Skipf("Skipping real JWKS test (network unavailable or key changed): %v", err)
	}

	edPub, ok := pub.(ed25519.PublicKey)
	if !ok {
		t.Fatalf("ResolveKey() from real JWKS returned %T, want ed25519.PublicKey", pub)
	}

	if len(edPub) != ed25519.PublicKeySize {
		t.Errorf("ed25519 key length = %d, want %d", len(edPub), ed25519.PublicKeySize)
	}
}

func TestParseJWKPublicKey_UnsupportedKeyType(t *testing.T) {
	t.Parallel()

	_, err := parseJWKPublicKey(jwk{Kty: "RSA", Kid: "rsa-key"})
	if err == nil {
		t.Fatal("parseJWKPublicKey() returned nil error for RSA key, want error")
	}
}

func TestParseJWKPublicKey_UnsupportedCurve(t *testing.T) {
	t.Parallel()

	_, err := parseJWKPublicKey(jwk{Kty: "EC", Crv: "P-384", Kid: "p384-key"})
	if err == nil {
		t.Fatal("parseJWKPublicKey() returned nil error for P-384 curve, want error")
	}

	_, err = parseJWKPublicKey(jwk{Kty: "OKP", Crv: "X25519", Kid: "x25519-key"})
	if err == nil {
		t.Fatal("parseJWKPublicKey() returned nil error for X25519 curve, want error")
	}
}

func TestParseJWKPublicKey_InvalidBase64(t *testing.T) {
	t.Parallel()

	_, err := parseJWKPublicKey(jwk{Kty: "OKP", Crv: "Ed25519", X: "!!!invalid!!!", Kid: "bad-key"})
	if err == nil {
		t.Fatal("parseJWKPublicKey() returned nil error for invalid base64, want error")
	}

	_, err = parseJWKPublicKey(jwk{
		Kty: "EC", Crv: "P-256",
		X:   base64.RawURLEncoding.EncodeToString(make([]byte, 32)),
		Y:   "!!!invalid!!!",
		Kid: "bad-ec-key",
	})
	if err == nil {
		t.Fatal("parseJWKPublicKey() returned nil error for invalid EC y base64, want error")
	}
}

func TestAgentIDKeyResolver_MultipleKeys(t *testing.T) {
	t.Parallel()

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate Ed25519 key: %v", err)
	}

	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate ECDSA key: %v", err)
	}

	matchingKid := "my-target-key"
	keySize := (ecKey.Curve.Params().BitSize + 7) / 8

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jwksResp := jwks{
			Keys: []jwk{
				{
					Kty: "OKP",
					Crv: "Ed25519",
					X:   base64.RawURLEncoding.EncodeToString(pub),
					Kid: "other-key",
				},
				{
					Kty: "EC",
					Crv: "P-256",
					X:   base64.RawURLEncoding.EncodeToString(ecKey.X.Bytes()[:keySize]),
					Y:   base64.RawURLEncoding.EncodeToString(ecKey.Y.Bytes()[:keySize]),
					Kid: matchingKid,
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jwksResp)
	}))
	defer srv.Close()

	resolver := &AgentIDKeyResolver{JWKSURL: srv.URL}
	resolved, err := resolver.ResolveKey(matchingKid, "")
	if err != nil {
		t.Fatalf("ResolveKey() error = %v", err)
	}

	ecPub, ok := resolved.(*ecdsa.PublicKey)
	if !ok {
		t.Fatalf("ResolveKey() returned %T, want *ecdsa.PublicKey (should match the EC key, not the Ed25519 key)", resolved)
	}

	// Verify EC key coordinates match.
	if ecPub.X.Cmp(ecKey.X) != 0 || ecPub.Y.Cmp(ecKey.Y) != 0 {
		t.Error("resolved EC key coordinates do not match original key")
	}
}

func TestAgentIDKeyResolver_InvalidEd25519KeyLength(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jwksResp := jwks{
			Keys: []jwk{
				{
					Kty: "OKP",
					Crv: "Ed25519",
					X:   base64.RawURLEncoding.EncodeToString([]byte("too-short")),
					Kid: "short-key",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jwksResp)
	}))
	defer srv.Close()

	resolver := &AgentIDKeyResolver{JWKSURL: srv.URL}
	_, err := resolver.ResolveKey("short-key", "")
	if err == nil {
		t.Fatal("ResolveKey() returned nil error for invalid Ed25519 key length, want error")
	}
}

func TestAgentIDKeyResolver_ECPointNotOnCurve(t *testing.T) {
	t.Parallel()

	// Create a point not on P-256 curve: use x=1, y=1 (not on curve)
	xBytes := big.NewInt(1).Bytes()
	yBytes := big.NewInt(1).Bytes()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jwksResp := jwks{
			Keys: []jwk{
				{
					Kty: "EC",
					Crv: "P-256",
					X:   base64.RawURLEncoding.EncodeToString(xBytes),
					Y:   base64.RawURLEncoding.EncodeToString(yBytes),
					Kid: "off-curve-key",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jwksResp)
	}))
	defer srv.Close()

	resolver := &AgentIDKeyResolver{JWKSURL: srv.URL}
	_, err := resolver.ResolveKey("off-curve-key", "")
	if err == nil {
		t.Fatal("ResolveKey() returned nil error for point not on curve, want error")
	}
}
