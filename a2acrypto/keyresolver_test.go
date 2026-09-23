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
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestKeyResolverFunc_Found(t *testing.T) {
	t.Parallel()

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	resolver := KeyResolverFunc(func(_ context.Context, kid string) (crypto.PublicKey, error) {
		return pub, nil
	})

	got, err := resolver.ResolveKey(t.Context(), "kid-1", "https://attacker.example/jwks.json")
	if err != nil {
		t.Fatalf("ResolveKey() error = %v, want nil", err)
	}
	if !pub.Equal(got) {
		t.Errorf("ResolveKey() returned a different key than the one enrolled")
	}
}

func TestKeyResolverFunc_NotFound(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("not found")
	resolver := KeyResolverFunc(func(_ context.Context, kid string) (crypto.PublicKey, error) {
		return nil, wantErr
	})
	if _, err := resolver.ResolveKey(t.Context(), "kid-2", "jku"); !errors.Is(err, wantErr) {
		t.Error("ResolveKey() returned nil error for an unknown kid, want error")
	}
}

func TestJWKSKeyResolverVerifiesSignedCard(t *testing.T) {
	t.Parallel()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	srv := jwksServer(t, ed25519JWKS("k1", pub))

	raw := mustMarshalCard(t, makeTestCard())
	sig, err := NewSigner(SignerConfig{PrivateKey: priv, KeyID: "k1", JWKSURL: srv.URL}).Sign(raw)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}

	resolver := NewJWKSKeyResolver(srv.Client(), []string{srv.URL})
	if err := NewVerifier(VerifierConfig{KeyResolver: resolver}).Verify(t.Context(), raw, sig); err != nil {
		t.Errorf("Verify() error = %v, want nil", err)
	}
}

type equalKey interface {
	Equal(crypto.PublicKey) bool
}

func TestJWKSKeyResolverResolveKey(t *testing.T) {
	t.Parallel()

	edPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	tests := []struct {
		name         string
		jwks         string
		failServer   bool
		kid          string
		untrustedJKU bool
		wantKey      crypto.PublicKey
		wantErr      bool
	}{
		{
			name:    "parses EC key",
			jwks:    ecP256JWKS("ec-1", &ecKey.PublicKey),
			kid:     "ec-1",
			wantKey: &ecKey.PublicKey,
		},
		{
			name:         "rejects jku off the allowlist even with a known kid",
			jwks:         ed25519JWKS("k1", edPub),
			kid:          "k1",
			untrustedJKU: true,
			wantErr:      true,
		},
		{
			name:    "rejects unknown kid",
			jwks:    ed25519JWKS("k1", edPub),
			kid:     "k2",
			wantErr: true,
		},
		{
			name:       "rejects when the endpoint fails",
			failServer: true,
			kid:        "k1",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv := jwksServer(t, tt.jwks)
			if tt.failServer {
				srv = failingJWKSServer(t)
			}

			jku := srv.URL
			if tt.untrustedJKU {
				jku = "https://attacker.example/jwks.json"
			}

			resolver := NewJWKSKeyResolver(srv.Client(), []string{srv.URL})
			got, err := resolver.ResolveKey(t.Context(), tt.kid, jku)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ResolveKey() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveKey() error = %v, want nil", err)
			}
			if !tt.wantKey.(equalKey).Equal(got) {
				t.Errorf("ResolveKey() returned a key that does not match the served key")
			}
		})
	}
}

func failingJWKSServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func ed25519JWKS(kid string, pub ed25519.PublicKey) string {
	return fmt.Sprintf(`{"keys":[{"kty":"OKP","crv":"Ed25519","kid":%q,"x":%q}]}`,
		kid, base64.RawURLEncoding.EncodeToString(pub))
}

func ecP256JWKS(kid string, pub *ecdsa.PublicKey) string {
	size := (pub.Curve.Params().BitSize + 7) / 8
	x := make([]byte, size)
	y := make([]byte, size)
	pub.X.FillBytes(x)
	pub.Y.FillBytes(y)
	return fmt.Sprintf(`{"keys":[{"kty":"EC","crv":"P-256","kid":%q,"x":%q,"y":%q}]}`,
		kid, base64.RawURLEncoding.EncodeToString(x), base64.RawURLEncoding.EncodeToString(y))
}

func jwksServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := io.WriteString(w, body); err != nil {
			t.Errorf("failed to write JWKS: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}
