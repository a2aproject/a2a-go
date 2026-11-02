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
	"crypto"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"fmt"
	"math/big"
)

// jwk is the subset of RFC 7517 JSON Web Key fields this package reads.
type jwk struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
	N   string `json:"n"`
	E   string `json:"e"`
}

func (k jwk) publicKey() (crypto.PublicKey, error) {
	switch k.Kty {
	case "OKP":
		if k.Crv != "Ed25519" {
			return nil, fmt.Errorf("unsupported OKP curve %q", k.Crv)
		}
		x, err := base64.RawURLEncoding.DecodeString(k.X)
		if err != nil {
			return nil, fmt.Errorf("invalid OKP x: %w", err)
		}
		if len(x) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("invalid Ed25519 key length %d", len(x))
		}
		return ed25519.PublicKey(x), nil
	case "EC":
		return ecPublicKey(k)
	case "RSA":
		return rsaPublicKey(k)
	default:
		return nil, fmt.Errorf("unsupported key type %q", k.Kty)
	}
}

func ecPublicKey(k jwk) (crypto.PublicKey, error) {
	var curve elliptic.Curve
	var ecdhCurve ecdh.Curve
	switch k.Crv {
	case "P-256":
		curve, ecdhCurve = elliptic.P256(), ecdh.P256()
	case "P-384":
		curve, ecdhCurve = elliptic.P384(), ecdh.P384()
	case "P-521":
		curve, ecdhCurve = elliptic.P521(), ecdh.P521()
	default:
		return nil, fmt.Errorf("unsupported EC curve %q", k.Crv)
	}

	xb, err := base64.RawURLEncoding.DecodeString(k.X)
	if err != nil {
		return nil, fmt.Errorf("invalid EC x: %w", err)
	}
	yb, err := base64.RawURLEncoding.DecodeString(k.Y)
	if err != nil {
		return nil, fmt.Errorf("invalid EC y: %w", err)
	}
	x := new(big.Int).SetBytes(xb)
	y := new(big.Int).SetBytes(yb)
	bits := curve.Params().BitSize
	if x.BitLen() > bits || y.BitLen() > bits {
		return nil, fmt.Errorf("EC coordinate out of range for %s", k.Crv)
	}

	// crypto/ecdh validates that the point lies on the curve, which guards
	// against invalid-curve attacks. The coordinates are reused for the ecdsa
	// key only after that check passes.
	size := (bits + 7) / 8
	point := make([]byte, 1+2*size)
	point[0] = 4
	x.FillBytes(point[1 : 1+size])
	y.FillBytes(point[1+size:])
	if _, err := ecdhCurve.NewPublicKey(point); err != nil {
		return nil, fmt.Errorf("invalid EC public key: %w", err)
	}
	return &ecdsa.PublicKey{Curve: curve, X: x, Y: y}, nil
}

func rsaPublicKey(k jwk) (crypto.PublicKey, error) {
	nb, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, fmt.Errorf("invalid RSA n: %w", err)
	}
	eb, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, fmt.Errorf("invalid RSA e: %w", err)
	}
	e := new(big.Int).SetBytes(eb)
	if e.BitLen() == 0 || e.BitLen() > 32 {
		return nil, fmt.Errorf("invalid RSA exponent")
	}
	return &rsa.PublicKey{N: new(big.Int).SetBytes(nb), E: int(e.Int64())}, nil
}
